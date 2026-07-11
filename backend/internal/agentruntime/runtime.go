package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
	"aiops-platform/backend/internal/skillframework"
)

const (
	defaultMaxSteps        = 12
	defaultMaxSkillCalls   = 20
	defaultTimeout         = 180 * time.Second
	defaultMaxContextBytes = 1048576
)

var (
	ErrInvalidInput       = errors.New("invalid input")
	ErrAgentNotFound      = errors.New("agent not found")
	ErrStepLimitExceeded  = errors.New("agent step limit exceeded")
	ErrSkillLimitExceeded = errors.New("agent skill call limit exceeded")
	ErrContextTooLarge    = errors.New("agent context too large")
	ErrEvidenceRefMissing = errors.New("agent evidence reference missing")
)

type AgentDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

type AgentContext struct {
	UserID         int64          `json:"userId"`
	ConversationID *int64         `json:"conversationId,omitempty"`
	IncidentID     *int64         `json:"incidentId,omitempty"`
	Query          string         `json:"query"`
	Scope          map[string]any `json:"scope,omitempty"`
	Evidence       []Evidence     `json:"evidence,omitempty"`
	Variables      map[string]any `json:"variables,omitempty"`
}

type Evidence struct {
	Key     string `json:"key"`
	Summary string `json:"summary"`
	Source  string `json:"source,omitempty"`
}

type Fact struct {
	Summary     string `json:"summary"`
	EvidenceKey string `json:"evidenceKey,omitempty"`
}

type Hypothesis struct {
	Summary    string  `json:"summary"`
	Confidence float64 `json:"confidence"`
}

type SkillRequest struct {
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type AgentResult struct {
	Summary       string          `json:"summary"`
	Facts         []Fact          `json:"facts,omitempty"`
	Hypotheses    []Hypothesis    `json:"hypotheses,omitempty"`
	EvidenceRefs  []string        `json:"evidenceRefs,omitempty"`
	SuggestedNext []SkillRequest  `json:"suggestedNext,omitempty"`
	Structured    json.RawMessage `json:"structured,omitempty"`
	Confidence    float64         `json:"confidence"`
}

type Agent interface {
	Name() string
	Description() string
	Analyze(ctx context.Context, input AgentContext, runtime *RunContext) (*AgentResult, error)
}

type AuditRepository interface {
	CreateAgentRun(ctx context.Context, run *model.AgentRun) error
	UpdateAgentRun(ctx context.Context, id int64, updates repository.AgentRunUpdates) (*model.AgentRun, error)
	ListAgentRuns(ctx context.Context, limit int) ([]model.AgentRun, error)
	FindAgentRunByID(ctx context.Context, id int64) (*model.AgentRun, error)
}

type Runtime struct {
	mu     sync.RWMutex
	agents map[string]*agentEntry
	skills *skillframework.Registry
	audit  AuditRepository
	limits Limits
	now    func() time.Time
}

type Limits struct {
	MaxSteps        int
	MaxSkillCalls   int
	Timeout         time.Duration
	MaxContextBytes int
}

type agentEntry struct {
	agent   Agent
	enabled bool
}

type RunInput struct {
	Actor         *model.AppUser
	Name          string
	Context       AgentContext
	WorkflowRunID *int64
}

type RunOutput struct {
	RunID  int64        `json:"runId,omitempty"`
	Result *AgentResult `json:"result"`
	Steps  int          `json:"steps"`
	Skills int          `json:"skills"`
}

type RunContext struct {
	actor      *model.AppUser
	skills     *skillframework.Registry
	steps      int
	skillCalls int
	maxSteps   int
	maxSkills  int
}

func NewRuntime(skills *skillframework.Registry, audit AuditRepository, limits Limits, agents ...Agent) (*Runtime, error) {
	limits = normalizeLimits(limits)
	runtime := &Runtime{
		agents: map[string]*agentEntry{},
		skills: skills,
		audit:  audit,
		limits: limits,
		now:    func() time.Time { return time.Now().UTC() },
	}
	for _, agent := range agents {
		if err := runtime.Register(agent); err != nil {
			return nil, err
		}
	}
	return runtime, nil
}

func BuiltinAgents() []Agent {
	return []Agent{
		CoordinatorAgent{},
		EchoAgent{},
		KnowledgeAgent{},
		LogAgent{},
		MetricsAgent{},
		KubernetesAgent{},
	}
}

func (r *Runtime) Register(agent Agent) error {
	if agent == nil || normalizeName(agent.Name()) == "" {
		return ErrInvalidInput
	}
	name := normalizeName(agent.Name())
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.agents[name]; ok {
		return ErrInvalidInput
	}
	r.agents[name] = &agentEntry{agent: agent, enabled: true}
	return nil
}

func (r *Runtime) List() []AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	definitions := make([]AgentDefinition, 0, len(r.agents))
	for name, entry := range r.agents {
		definitions = append(definitions, AgentDefinition{Name: name, Description: entry.agent.Description(), Enabled: entry.enabled})
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Name < definitions[j].Name })
	return definitions
}

func (r *Runtime) Get(name string) (AgentDefinition, error) {
	entry, normalized, err := r.lookup(name)
	if err != nil {
		return AgentDefinition{}, err
	}
	return AgentDefinition{Name: normalized, Description: entry.agent.Description(), Enabled: entry.enabled}, nil
}

func (r *Runtime) Run(ctx context.Context, input RunInput) (*RunOutput, error) {
	if input.Actor == nil || input.Context.UserID <= 0 {
		return nil, ErrInvalidInput
	}
	entry, name, err := r.lookup(input.Name)
	if err != nil {
		return nil, err
	}
	if !entry.enabled {
		return nil, ErrAgentNotFound
	}
	if err := validateContextSize(input.Context, r.limits.MaxContextBytes); err != nil {
		return nil, err
	}
	startedAt := r.now()
	inputSummary := summarizeContext(input.Context)
	run := &model.AgentRun{
		WorkflowRunID: input.WorkflowRunID,
		AgentName:     name,
		InputSummary:  &inputSummary,
		Status:        model.AgentRunStatusRunning,
		StartedAt:     &startedAt,
	}
	if r.audit != nil {
		if err := r.audit.CreateAgentRun(ctx, run); err != nil {
			return nil, err
		}
	}
	runCtx := &RunContext{
		actor:     input.Actor,
		skills:    r.skills,
		maxSteps:  r.limits.MaxSteps,
		maxSkills: r.limits.MaxSkillCalls,
	}
	executeCtx, cancel := context.WithTimeout(ctx, r.limits.Timeout)
	defer cancel()
	result, runErr := entry.agent.Analyze(executeCtx, input.Context, runCtx)
	if runErr == nil {
		runErr = validateAgentResult(input.Context, result)
	}
	finishedAt := r.now()
	status := model.AgentRunStatusSuccess
	var errorMessage *string
	var output []byte
	if runErr != nil {
		status = model.AgentRunStatusFailed
		message := runErr.Error()
		errorMessage = &message
	} else {
		output, _ = json.Marshal(result)
	}
	if r.audit != nil && run.ID > 0 {
		_, _ = r.audit.UpdateAgentRun(ctx, run.ID, repository.AgentRunUpdates{
			Status:       status,
			Output:       output,
			ErrorMessage: errorMessage,
			FinishedAt:   &finishedAt,
		})
	}
	if runErr != nil {
		return nil, runErr
	}
	return &RunOutput{RunID: run.ID, Result: result, Steps: runCtx.steps, Skills: runCtx.skillCalls}, nil
}

func (r *Runtime) ListRuns(ctx context.Context, limit int) ([]model.AgentRun, error) {
	if r.audit == nil {
		return []model.AgentRun{}, nil
	}
	return r.audit.ListAgentRuns(ctx, limit)
}

func (r *Runtime) GetRun(ctx context.Context, id int64) (*model.AgentRun, error) {
	if r.audit == nil {
		return nil, ErrAgentNotFound
	}
	return r.audit.FindAgentRunByID(ctx, id)
}

func (c *RunContext) Step(_ string) error {
	c.steps++
	if c.steps > c.maxSteps {
		return ErrStepLimitExceeded
	}
	return nil
}

func (c *RunContext) ExecuteSkill(ctx context.Context, name string, input json.RawMessage) (*skillframework.ExecuteResult, error) {
	c.skillCalls++
	if c.skillCalls > c.maxSkills {
		return nil, ErrSkillLimitExceeded
	}
	if c.skills == nil {
		return nil, ErrInvalidInput
	}
	return c.skills.Execute(ctx, skillframework.ExecuteInput{Actor: c.actor, Name: name, Payload: input})
}

func (r *Runtime) lookup(name string) (*agentEntry, string, error) {
	normalized := normalizeName(name)
	if normalized == "" {
		return nil, "", ErrInvalidInput
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.agents[normalized]
	if !ok {
		return nil, "", ErrAgentNotFound
	}
	return entry, normalized, nil
}

func normalizeLimits(limits Limits) Limits {
	if limits.MaxSteps <= 0 {
		limits.MaxSteps = defaultMaxSteps
	}
	if limits.MaxSkillCalls <= 0 {
		limits.MaxSkillCalls = defaultMaxSkillCalls
	}
	if limits.Timeout <= 0 {
		limits.Timeout = defaultTimeout
	}
	if limits.MaxContextBytes <= 0 {
		limits.MaxContextBytes = defaultMaxContextBytes
	}
	return limits
}

func validateContextSize(input AgentContext, maxBytes int) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return ErrInvalidInput
	}
	if len(raw) > maxBytes {
		return ErrContextTooLarge
	}
	return nil
}

func validateAgentResult(input AgentContext, result *AgentResult) error {
	if result == nil {
		return ErrInvalidInput
	}
	knownRefs := map[string]struct{}{}
	for _, evidence := range input.Evidence {
		key := strings.TrimSpace(evidence.Key)
		if key != "" {
			knownRefs[key] = struct{}{}
		}
	}
	for _, ref := range result.EvidenceRefs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return ErrEvidenceRefMissing
		}
		knownRefs[ref] = struct{}{}
	}
	for _, fact := range result.Facts {
		if fact.EvidenceKey == "" {
			continue
		}
		if _, ok := knownRefs[fact.EvidenceKey]; !ok {
			return fmt.Errorf("%w: %s", ErrEvidenceRefMissing, fact.EvidenceKey)
		}
	}
	return nil
}

func summarizeContext(input AgentContext) string {
	query := strings.TrimSpace(input.Query)
	if len([]rune(query)) > 200 {
		query = string([]rune(query)[:200])
	}
	return query
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

type EchoAgent struct{}

func (EchoAgent) Name() string {
	return "echo_agent"
}

func (EchoAgent) Description() string {
	return "Runtime smoke-test agent that returns the query and evidence count."
}

func (EchoAgent) Analyze(_ context.Context, input AgentContext, runtime *RunContext) (*AgentResult, error) {
	if err := runtime.Step("summarize input"); err != nil {
		return nil, err
	}
	return &AgentResult{
		Summary:    input.Query,
		Facts:      []Fact{{Summary: fmt.Sprintf("received %d evidence items", len(input.Evidence))}},
		Confidence: 0.5,
	}, nil
}
