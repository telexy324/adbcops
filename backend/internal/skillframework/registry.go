package skillframework

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"aiops-platform/backend/internal/auditutil"
	appmiddleware "aiops-platform/backend/internal/middleware"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
	"aiops-platform/backend/internal/toolregistry"
)

var (
	ErrInvalidInput     = errors.New("invalid input")
	ErrSkillNotFound    = errors.New("skill not found")
	ErrSkillDisabled    = errors.New("skill disabled")
	ErrRiskNotAllowed   = errors.New("skill risk not allowed")
	ErrPermissionDenied = errors.New("permission denied")
	ErrToolUnavailable  = errors.New("required tool unavailable")
)

type SkillDefinition struct {
	Name          string          `json:"name"`
	Version       string          `json:"version"`
	Description   string          `json:"description"`
	InputSchema   json.RawMessage `json:"inputSchema"`
	OutputSchema  json.RawMessage `json:"outputSchema,omitempty"`
	RiskLevel     string          `json:"riskLevel"`
	ReadOnly      bool            `json:"readOnly"`
	Enabled       bool            `json:"enabled"`
	TimeoutSecond int             `json:"timeoutSecond"`
	RequiredTools []string        `json:"requiredTools"`
}

type Skill interface {
	Definition() SkillDefinition
	Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error)
}

type Registry struct {
	mu      sync.RWMutex
	entries map[string]*entry
	tools   *toolregistry.Registry
	audit   AuditRepository
	now     func() time.Time
}

type AuditRepository interface {
	CreateSkillRun(ctx context.Context, run *model.SkillRun) error
	UpdateSkillRun(ctx context.Context, id int64, updates repository.SkillRunUpdates) (*model.SkillRun, error)
	ListSkillRuns(ctx context.Context, limit int) ([]model.SkillRun, error)
}

type entry struct {
	skill   Skill
	enabled bool
}

type ExecuteInput struct {
	Actor         *model.AppUser
	Name          string
	Payload       json.RawMessage
	WorkflowRunID *int64
	NodeRunID     *int64
}

type ExecuteResult struct {
	SkillName string          `json:"skillName"`
	RunID     int64           `json:"runId,omitempty"`
	Output    json.RawMessage `json:"output"`
}

func NewRegistry(tools *toolregistry.Registry, audit AuditRepository, skills ...Skill) (*Registry, error) {
	registry := &Registry{
		entries: map[string]*entry{},
		tools:   tools,
		audit:   audit,
		now:     func() time.Time { return time.Now().UTC() },
	}
	for _, skill := range skills {
		if err := registry.Register(skill); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *Registry) Register(skill Skill) error {
	if skill == nil {
		return ErrInvalidInput
	}
	definition := normalizeDefinition(skill.Definition())
	if err := validateDefinition(definition); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.entries[definition.Name]; exists {
		return ErrInvalidInput
	}
	r.entries[definition.Name] = &entry{skill: skill, enabled: true}
	return nil
}

func (r *Registry) List() []SkillDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	definitions := make([]SkillDefinition, 0, len(r.entries))
	for name, entry := range r.entries {
		definition := normalizeDefinition(entry.skill.Definition())
		definition.Name = name
		definition.Enabled = entry.enabled
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Name < definitions[j].Name
	})
	return definitions
}

func (r *Registry) Get(name string) (SkillDefinition, error) {
	entry, normalized, err := r.lookup(name)
	if err != nil {
		return SkillDefinition{}, err
	}
	definition := normalizeDefinition(entry.skill.Definition())
	definition.Name = normalized
	definition.Enabled = entry.enabled
	return definition, nil
}

func (r *Registry) Enable(name string) (SkillDefinition, error) {
	return r.setEnabled(name, true)
}

func (r *Registry) Disable(name string) (SkillDefinition, error) {
	return r.setEnabled(name, false)
}

func (r *Registry) Execute(ctx context.Context, input ExecuteInput) (*ExecuteResult, error) {
	if input.Actor == nil {
		return nil, ErrPermissionDenied
	}
	entry, normalized, err := r.lookup(input.Name)
	if err != nil {
		return nil, err
	}
	if !entry.enabled {
		return nil, ErrSkillDisabled
	}
	definition := normalizeDefinition(entry.skill.Definition())
	if err := authorize(input.Actor, definition); err != nil {
		return nil, err
	}
	if err := ValidateJSONSchema(definition.InputSchema, input.Payload); err != nil {
		return nil, err
	}
	if r.tools != nil {
		if err := r.tools.SkillCanExecute(definition.RequiredTools); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrToolUnavailable, err)
		}
	}
	startedAt := r.now()
	requestID := appmiddleware.GetRequestIDFromContext(ctx)
	run := &model.SkillRun{
		WorkflowRunID: input.WorkflowRunID,
		NodeRunID:     input.NodeRunID,
		RequestID:     stringPtrOrNil(requestID),
		SkillName:     normalized,
		ToolName:      firstTool(definition.RequiredTools),
		InputSummary:  summarizeJSON(input.Payload),
		Status:        model.SkillRunStatusRunning,
		StartedAt:     &startedAt,
	}
	if r.audit != nil {
		if err := r.audit.CreateSkillRun(ctx, run); err != nil {
			return nil, err
		}
	}
	timeout := time.Duration(definition.TimeoutSecond) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	executeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	executeCtx = ContextWithActor(executeCtx, input.Actor)
	output, executeErr := entry.skill.Execute(executeCtx, input.Payload)
	finishedAt := r.now()
	status := model.SkillRunStatusSuccess
	var errorMessage *string
	if executeErr != nil {
		status = model.SkillRunStatusFailed
		message := executeErr.Error()
		errorMessage = &message
	}
	if r.audit != nil && run.ID > 0 {
		_, _ = r.audit.UpdateSkillRun(ctx, run.ID, repository.SkillRunUpdates{
			Status:        status,
			OutputSummary: summarizeJSON(output),
			ErrorMessage:  errorMessage,
			FinishedAt:    &finishedAt,
		})
	}
	if executeErr != nil {
		return nil, executeErr
	}
	return &ExecuteResult{SkillName: normalized, RunID: run.ID, Output: output}, nil
}

func (r *Registry) ListRuns(ctx context.Context, limit int) ([]model.SkillRun, error) {
	if r.audit == nil {
		return []model.SkillRun{}, nil
	}
	return r.audit.ListSkillRuns(ctx, limit)
}

func (r *Registry) setEnabled(name string, enabled bool) (SkillDefinition, error) {
	normalized := normalizeName(name)
	if normalized == "" {
		return SkillDefinition{}, ErrInvalidInput
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[normalized]
	if !ok {
		return SkillDefinition{}, ErrSkillNotFound
	}
	entry.enabled = enabled
	definition := normalizeDefinition(entry.skill.Definition())
	definition.Name = normalized
	definition.Enabled = entry.enabled
	return definition, nil
}

func (r *Registry) lookup(name string) (*entry, string, error) {
	normalized := normalizeName(name)
	if normalized == "" {
		return nil, "", ErrInvalidInput
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.entries[normalized]
	if !ok {
		return nil, "", ErrSkillNotFound
	}
	return entry, normalized, nil
}

func authorize(actor *model.AppUser, definition SkillDefinition) error {
	if !definition.ReadOnly {
		return ErrRiskNotAllowed
	}
	switch definition.RiskLevel {
	case model.SkillRiskSafeRead:
		return nil
	case model.SkillRiskSensitiveRead:
		if actor.Role != model.RoleAdmin {
			return ErrPermissionDenied
		}
		return nil
	default:
		return ErrRiskNotAllowed
	}
}

func validateDefinition(definition SkillDefinition) error {
	if definition.Name == "" || definition.Version == "" || !definition.ReadOnly {
		return ErrInvalidInput
	}
	if definition.RiskLevel != model.SkillRiskSafeRead && definition.RiskLevel != model.SkillRiskSensitiveRead {
		return ErrRiskNotAllowed
	}
	if len(definition.InputSchema) == 0 || !json.Valid(definition.InputSchema) {
		return ErrInvalidInput
	}
	return nil
}

func normalizeDefinition(definition SkillDefinition) SkillDefinition {
	definition.Name = normalizeName(definition.Name)
	definition.RiskLevel = strings.TrimSpace(definition.RiskLevel)
	definition.Version = strings.TrimSpace(definition.Version)
	definition.RequiredTools = normalizeTools(definition.RequiredTools)
	return definition
}

func normalizeTools(tools []string) []string {
	normalized := make([]string, 0, len(tools))
	seen := map[string]struct{}{}
	for _, tool := range tools {
		name := normalizeName(tool)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		normalized = append(normalized, name)
	}
	return normalized
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func firstTool(tools []string) *string {
	if len(tools) == 0 {
		return nil
	}
	return &tools[0]
}

func stringPtrOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func summarizeJSON(value json.RawMessage) []byte {
	const maxBytes = 4096
	return auditutil.SanitizeJSON(value, maxBytes)
}
