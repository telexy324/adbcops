package rca

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"aiops-platform/backend/internal/auditutil"
	datasourcesvc "aiops-platform/backend/internal/datasource"
	evidencesvc "aiops-platform/backend/internal/evidence"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/observability"
	"aiops-platform/backend/internal/repository"
	"aiops-platform/backend/internal/resourcelimit"
	"aiops-platform/backend/internal/skillframework"
)

var (
	ErrInvalidInput        = errors.New("invalid input")
	ErrForbidden           = errors.New("rca access forbidden")
	ErrInvalidTransition   = errors.New("invalid rca state transition")
	ErrEvidenceRequired    = errors.New("root cause candidate requires evidence")
	ErrRoundLimit          = errors.New("rca round limit reached")
	ErrOrchestratorActive  = errors.New("rca orchestrator already active")
	ErrOrchestratorLimited = errors.New("rca orchestrator concurrency limit exceeded")
)

const (
	defaultMaxRounds = 3
	defaultTimeout   = 15 * time.Minute
	maxTimeout       = 2 * time.Hour
)

type Repository interface {
	repository.RCARepository
}

type EvidenceCreator interface {
	Create(ctx context.Context, input evidencesvc.CreateInput) (*model.EvidenceRecord, error)
}

type DataSourceLister interface {
	List(ctx context.Context, actor *model.AppUser) ([]datasourcesvc.DataSourceView, error)
}

type Service struct {
	repository                 Repository
	evidence                   EvidenceCreator
	dataSources                DataSourceLister
	skills                     RoundOneSkillExecutor
	skillCatalog               PlannerSkillCatalog
	plannerModel               PlannerModel
	databaseDiagnosisProviders []DatabaseDiagnosisProvider
	reportAgentRuns            ReportAgentRunLister
	reportSkillRuns            ReportSkillRunLister
	now                        func() time.Time
	orchestratorMu             sync.Mutex
	activeOrchestrators        map[int64]context.CancelFunc
	userOrchestratorLimiter    *resourcelimit.KeyedLimiter
	globalOrchestratorLimiter  *resourcelimit.Limiter
}

type RoundOneSkillExecutor interface {
	Execute(ctx context.Context, input skillframework.ExecuteInput) (*skillframework.ExecuteResult, error)
}

type CreateRunInput struct {
	Query          string          `json:"query"`
	Scope          json.RawMessage `json:"scope"`
	ConversationID *int64          `json:"conversationId"`
	IncidentID     *int64          `json:"incidentId"`
	WorkflowRunID  *int64          `json:"workflowRunId"`
	MaxRounds      int             `json:"maxRounds"`
	TimeoutSeconds int             `json:"timeoutSeconds"`
}

type Hypothesis struct {
	ID          string  `json:"id,omitempty"`
	Summary     string  `json:"summary"`
	Confidence  float64 `json:"confidence"`
	EvidenceIDs []int64 `json:"evidenceIds"`
}

type NextAction struct {
	ActionKey string          `json:"actionKey"`
	SkillName string          `json:"skillName"`
	Input     json.RawMessage `json:"input"`
}

type StartRoundInput struct {
	InputHypotheses []Hypothesis `json:"inputHypotheses"`
}

type CompleteRoundInput struct {
	Status             string       `json:"status"`
	NewEvidenceIDs     []int64      `json:"newEvidenceIds"`
	RejectedHypotheses []Hypothesis `json:"rejectedHypotheses"`
	NextActions        []NextAction `json:"nextActions"`
	ErrorCode          string       `json:"errorCode"`
}

type CreateActionInput struct {
	RoundID       int64           `json:"roundId"`
	ActionKey     string          `json:"actionKey"`
	SkillName     string          `json:"skillName"`
	Input         json.RawMessage `json:"input"`
	SensitiveRead bool            `json:"sensitiveRead"`
}

type CompleteActionInput struct {
	Status       string          `json:"status"`
	Output       json.RawMessage `json:"output"`
	EvidenceIDs  []int64         `json:"evidenceIds"`
	ErrorCode    string          `json:"errorCode"`
	ErrorMessage string          `json:"errorMessage"`
}

type CreateEvidenceInput struct {
	RoundID      int64           `json:"roundId"`
	ActionID     *int64          `json:"actionId"`
	EvidenceKey  string          `json:"evidenceKey"`
	SourceType   string          `json:"sourceType"`
	SourceRef    json.RawMessage `json:"sourceRef"`
	ObservedAt   *time.Time      `json:"observedAt"`
	Title        *string         `json:"title"`
	Summary      string          `json:"summary"`
	Content      json.RawMessage `json:"content"`
	Confidence   *float64        `json:"confidence"`
	Sensitivity  string          `json:"sensitivity"`
	EvidenceKind string          `json:"evidenceKind"`
	Entity       json.RawMessage `json:"entity"`
	WindowStart  *time.Time      `json:"windowStart"`
	WindowEnd    *time.Time      `json:"windowEnd"`
	SourceSkill  string          `json:"sourceSkill"`
	DataSourceID *int64          `json:"dataSourceId"`
}

type CreateCandidateInput struct {
	RoundID     int64   `json:"roundId"`
	Summary     string  `json:"summary"`
	Confidence  float64 `json:"confidence"`
	EvidenceIDs []int64 `json:"evidenceIds"`
	Rejected    bool    `json:"rejected"`
}

type CompleteRunInput struct {
	Status       string `json:"status"`
	ErrorCode    string `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`
	StopReason   string `json:"stopReason"`
}

type Detail struct {
	Run        *model.RCARun                 `json:"run"`
	Rounds     []model.RCARound              `json:"rounds"`
	Actions    []model.RCAAction             `json:"actions"`
	Evidence   []model.EvidenceRecord        `json:"evidence"`
	Candidates []model.RCARootCauseCandidate `json:"rootCauseCandidates"`
}

type RecoveryPlan struct {
	Run                *model.RCARun `json:"run"`
	SkippedActionIDs   []int64       `json:"skippedActionIds"`
	RetryableActionIDs []int64       `json:"retryableActionIds"`
}

func NewService(repository Repository, evidence EvidenceCreator, dataSources DataSourceLister) *Service {
	return &Service{
		repository: repository, evidence: evidence, dataSources: dataSources,
		databaseDiagnosisProviders: []DatabaseDiagnosisProvider{NewTiDBDatabaseDiagnosisProvider()},
		now:                        func() time.Time { return time.Now().UTC() }, activeOrchestrators: map[int64]context.CancelFunc{},
		userOrchestratorLimiter:   resourcelimit.NewKeyedLimiter(2),
		globalOrchestratorLimiter: resourcelimit.NewLimiter(8),
	}
}

func (s *Service) WithOrchestratorLimits(perUser, global int) *Service {
	s.userOrchestratorLimiter = resourcelimit.NewKeyedLimiter(perUser)
	s.globalOrchestratorLimiter = resourcelimit.NewLimiter(global)
	return s
}

func (s *Service) WithSkillExecutor(skills RoundOneSkillExecutor) *Service {
	s.skills = skills
	return s
}

func (s *Service) CreateRun(ctx context.Context, actor *model.AppUser, input CreateRunInput) (*model.RCARun, error) {
	if actor == nil || strings.TrimSpace(input.Query) == "" {
		return nil, ErrInvalidInput
	}
	maxRounds := input.MaxRounds
	if maxRounds == 0 {
		maxRounds = defaultMaxRounds
	}
	if maxRounds < 1 || maxRounds > 10 {
		return nil, ErrInvalidInput
	}
	timeout := defaultTimeout
	if input.TimeoutSeconds > 0 {
		timeout = time.Duration(input.TimeoutSeconds) * time.Second
	}
	if timeout <= 0 || timeout > maxTimeout {
		return nil, ErrInvalidInput
	}
	scope := sanitizeJSON(input.Scope, 16<<10, []byte(`{}`))
	if scope == nil {
		return nil, ErrInvalidInput
	}
	if err := s.authorizeScopeDataSources(ctx, actor, scope); err != nil {
		return nil, err
	}
	now := s.now()
	timeoutAt := now.Add(timeout)
	run := &model.RCARun{
		UserID: actor.ID, ConversationID: input.ConversationID, IncidentID: input.IncidentID,
		WorkflowRunID: input.WorkflowRunID, Status: model.RCARunStatusPending,
		Query: truncateText(auditutil.SanitizeText(strings.TrimSpace(input.Query)), 4096),
		Scope: scope, MaxRounds: maxRounds, TimeoutAt: &timeoutAt,
	}
	if err := s.repository.CreateRCARun(ctx, run); err != nil {
		return nil, err
	}
	observability.ObserveRCARunCreated()
	slog.InfoContext(ctx, "rca run created",
		"rca_run_id", run.ID, "user_id", actor.ID, "max_rounds", maxRounds,
		"timeout_seconds", int(timeout/time.Second),
	)
	return run, nil
}

func (s *Service) ListRuns(ctx context.Context, actor *model.AppUser, limit int, status string) ([]model.RCARun, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	if err := s.repository.MarkTimedOutRCARuns(ctx, s.now()); err != nil {
		return nil, err
	}
	filters := repository.RCARunFilters{Limit: limit, Status: strings.TrimSpace(status)}
	if actor.Role != model.RoleAdmin {
		filters.UserID = actor.ID
	}
	return s.repository.ListRCARuns(ctx, filters)
}

func (s *Service) GetRun(ctx context.Context, actor *model.AppUser, runID int64) (*model.RCARun, error) {
	if actor == nil || runID <= 0 {
		return nil, ErrInvalidInput
	}
	if err := s.repository.MarkTimedOutRCARuns(ctx, s.now()); err != nil {
		return nil, err
	}
	run, err := s.repository.FindRCARunByID(ctx, runID)
	if err != nil {
		return nil, err
	}
	if err := authorizeRun(actor, run); err != nil {
		return nil, err
	}
	return run, nil
}

func (s *Service) GetDetail(ctx context.Context, actor *model.AppUser, runID int64) (*Detail, error) {
	run, err := s.GetRun(ctx, actor, runID)
	if err != nil {
		return nil, err
	}
	rounds, err := s.repository.ListRCARounds(ctx, runID)
	if err != nil {
		return nil, err
	}
	actions, err := s.repository.ListRCAActions(ctx, runID)
	if err != nil {
		return nil, err
	}
	evidence, err := s.listAccessibleEvidence(ctx, actor, runID)
	if err != nil {
		return nil, err
	}
	candidates, err := s.repository.ListRCARootCauseCandidates(ctx, runID)
	if err != nil {
		return nil, err
	}
	return &Detail{Run: run, Rounds: rounds, Actions: actions, Evidence: evidence, Candidates: candidates}, nil
}

func (s *Service) ListRounds(ctx context.Context, actor *model.AppUser, runID int64) ([]model.RCARound, error) {
	if _, err := s.GetRun(ctx, actor, runID); err != nil {
		return nil, err
	}
	return s.repository.ListRCARounds(ctx, runID)
}

func (s *Service) ListActions(ctx context.Context, actor *model.AppUser, runID int64) ([]model.RCAAction, error) {
	if _, err := s.GetRun(ctx, actor, runID); err != nil {
		return nil, err
	}
	return s.repository.ListRCAActions(ctx, runID)
}

func (s *Service) ListEvidence(ctx context.Context, actor *model.AppUser, runID int64) ([]model.EvidenceRecord, error) {
	if _, err := s.GetRun(ctx, actor, runID); err != nil {
		return nil, err
	}
	return s.listAccessibleEvidence(ctx, actor, runID)
}

func (s *Service) StartRound(ctx context.Context, actor *model.AppUser, runID int64, input StartRoundInput) (*model.RCARound, error) {
	run, err := s.GetRun(ctx, actor, runID)
	if err != nil {
		return nil, err
	}
	if run.Status != model.RCARunStatusPending && run.Status != model.RCARunStatusRunning && run.Status != model.RCARunStatusPartialSuccess {
		return nil, ErrInvalidTransition
	}
	if run.FinishedAt != nil {
		return nil, ErrInvalidTransition
	}
	if run.CurrentRound >= run.MaxRounds {
		return nil, ErrRoundLimit
	}
	hypotheses, err := normalizedHypotheses(input.InputHypotheses, false)
	if err != nil {
		return nil, err
	}
	if err := s.validateHypothesisEvidence(ctx, runID, hypotheses); err != nil {
		return nil, err
	}
	rawHypotheses, _ := json.Marshal(hypotheses)
	now := s.now()
	round := &model.RCARound{
		RunID: run.ID, RoundNumber: run.CurrentRound + 1, Status: model.RCARoundStatusRunning,
		InputHypotheses: rawHypotheses, NewEvidenceIDs: []byte(`[]`),
		RejectedHypotheses: []byte(`[]`), NextActions: []byte(`[]`), StartedAt: &now,
	}
	if err := s.repository.CreateRCARound(ctx, round); err != nil {
		return nil, err
	}
	currentRound := round.RoundNumber
	updates := repository.RCARunUpdates{Status: model.RCARunStatusRunning, CurrentRound: &currentRound}
	if run.StartedAt == nil {
		updates.StartedAt = &now
	}
	if _, err := s.repository.UpdateRCARun(ctx, run.ID, updates); err != nil {
		return nil, err
	}
	return round, nil
}

func (s *Service) CreateAction(ctx context.Context, actor *model.AppUser, runID int64, input CreateActionInput) (*model.RCAAction, error) {
	if strings.TrimSpace(input.ActionKey) == "" || strings.TrimSpace(input.SkillName) == "" || input.RoundID <= 0 {
		return nil, ErrInvalidInput
	}
	run, err := s.GetRun(ctx, actor, runID)
	if err != nil {
		return nil, err
	}
	if run.Status != model.RCARunStatusRunning {
		return nil, ErrInvalidTransition
	}
	round, err := s.repository.FindRCARoundByID(ctx, input.RoundID)
	if err != nil {
		return nil, err
	}
	if round.RunID != runID || round.Status != model.RCARoundStatusRunning {
		return nil, ErrInvalidTransition
	}
	payload := sanitizeJSON(input.Input, 16<<10, []byte(`{}`))
	if payload == nil {
		return nil, ErrInvalidInput
	}
	action := &model.RCAAction{
		RunID: runID, RoundID: round.ID, ActionKey: truncateText(strings.TrimSpace(input.ActionKey), 160),
		SkillName: truncateText(strings.TrimSpace(input.SkillName), 120), Status: model.RCAActionStatusPending,
		Input: payload, EvidenceIDs: []byte(`[]`), SensitiveRead: input.SensitiveRead, Attempt: 1,
	}
	return s.repository.CreateOrGetRCAAction(ctx, action)
}

func (s *Service) StartAction(ctx context.Context, actor *model.AppUser, runID, actionID int64) (*model.RCAAction, error) {
	run, err := s.GetRun(ctx, actor, runID)
	if err != nil {
		return nil, err
	}
	if run.Status != model.RCARunStatusRunning {
		return nil, ErrInvalidTransition
	}
	action, err := s.repository.FindRCAActionByID(ctx, actionID)
	if err != nil {
		return nil, err
	}
	if action.RunID != runID || action.Status != model.RCAActionStatusPending {
		return nil, ErrInvalidTransition
	}
	now := s.now()
	return s.repository.UpdateRCAAction(ctx, actionID, repository.RCAActionUpdates{
		Status: model.RCAActionStatusRunning, StartedAt: &now,
	})
}

func (s *Service) CompleteAction(ctx context.Context, actor *model.AppUser, runID, actionID int64, input CompleteActionInput) (*model.RCAAction, error) {
	if _, err := s.GetRun(ctx, actor, runID); err != nil {
		return nil, err
	}
	action, err := s.repository.FindRCAActionByID(ctx, actionID)
	if err != nil {
		return nil, err
	}
	if action.RunID != runID || (action.Status != model.RCAActionStatusPending && action.Status != model.RCAActionStatusRunning) {
		return nil, ErrInvalidTransition
	}
	if !validActionTerminalStatus(input.Status) {
		return nil, ErrInvalidInput
	}
	if len(input.EvidenceIDs) > 0 {
		ok, err := s.repository.EvidenceIDsBelongToRCARun(ctx, runID, input.EvidenceIDs)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrInvalidInput
		}
	}
	output := sanitizeJSON(input.Output, 64<<10, []byte(`{}`))
	if output == nil {
		return nil, ErrInvalidInput
	}
	evidenceIDs, _ := json.Marshal(uniqueIDs(input.EvidenceIDs))
	now := s.now()
	code, message := safeFailure(input.ErrorCode, input.ErrorMessage)
	updated, err := s.repository.UpdateRCAAction(ctx, actionID, repository.RCAActionUpdates{
		Status: input.Status, Output: output, EvidenceIDs: evidenceIDs,
		ErrorCode: code, ErrorMessage: message, FinishedAt: &now,
	})
	if err != nil {
		return nil, err
	}
	observability.ObserveRCAAction(action.SkillName, input.Status, input.ErrorCode)
	slog.InfoContext(ctx, "rca action completed",
		"rca_run_id", runID, "rca_round_id", action.RoundID, "rca_action_id", actionID,
		"skill", action.SkillName, "status", input.Status, "error_code", input.ErrorCode,
		"evidence_count", len(input.EvidenceIDs),
	)
	return updated, nil
}

func (s *Service) AddEvidence(ctx context.Context, actor *model.AppUser, runID int64, input CreateEvidenceInput) (*model.EvidenceRecord, error) {
	if s.evidence == nil || input.RoundID <= 0 || strings.TrimSpace(input.EvidenceKind) == "" || strings.TrimSpace(input.SourceSkill) == "" {
		return nil, ErrInvalidInput
	}
	if _, err := s.GetRun(ctx, actor, runID); err != nil {
		return nil, err
	}
	round, err := s.repository.FindRCARoundByID(ctx, input.RoundID)
	if err != nil {
		return nil, err
	}
	if round.RunID != runID {
		return nil, ErrInvalidInput
	}
	if input.ActionID != nil {
		action, err := s.repository.FindRCAActionByID(ctx, *input.ActionID)
		if err != nil {
			return nil, err
		}
		if action.RunID != runID || action.RoundID != round.ID {
			return nil, ErrInvalidInput
		}
	}
	if input.DataSourceID != nil {
		allowed, err := s.dataSourceAllowed(ctx, actor, *input.DataSourceID)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, ErrForbidden
		}
	}
	ownerID := actor.ID
	record, err := s.evidence.Create(ctx, evidencesvc.CreateInput{
		EvidenceKey: input.EvidenceKey, SourceType: input.SourceType, SourceRef: input.SourceRef,
		ObservedAt: input.ObservedAt, Title: input.Title, Summary: input.Summary, Content: input.Content,
		Confidence: input.Confidence, Sensitivity: input.Sensitivity,
		RCARunID: &runID, RCARoundID: &input.RoundID, RCAActionID: input.ActionID,
		EvidenceKind: input.EvidenceKind, Entity: input.Entity, WindowStart: input.WindowStart,
		WindowEnd: input.WindowEnd, SourceSkill: input.SourceSkill, DataSourceID: input.DataSourceID,
		OwnerUserID: &ownerID,
	})
	if err != nil {
		return nil, err
	}
	observability.ObserveRCAEvidence(input.EvidenceKind, input.SourceType)
	slog.InfoContext(ctx, "rca evidence persisted",
		"rca_run_id", runID, "rca_round_id", input.RoundID, "evidence_id", record.ID,
		"evidence_kind", input.EvidenceKind, "source_type", input.SourceType,
		"source_skill", input.SourceSkill,
	)
	return record, nil
}

func (s *Service) AddRootCauseCandidate(ctx context.Context, actor *model.AppUser, runID int64, input CreateCandidateInput) (*model.RCARootCauseCandidate, error) {
	if strings.TrimSpace(input.Summary) == "" || input.RoundID <= 0 || input.Confidence < 0 || input.Confidence > 1 {
		return nil, ErrInvalidInput
	}
	if len(uniqueIDs(input.EvidenceIDs)) == 0 {
		return nil, ErrEvidenceRequired
	}
	if _, err := s.GetRun(ctx, actor, runID); err != nil {
		return nil, err
	}
	round, err := s.repository.FindRCARoundByID(ctx, input.RoundID)
	if err != nil {
		return nil, err
	}
	if round.RunID != runID {
		return nil, ErrInvalidInput
	}
	ok, err := s.repository.EvidenceIDsBelongToRCARun(ctx, runID, input.EvidenceIDs)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrEvidenceRequired
	}
	evidenceIDs, _ := json.Marshal(uniqueIDs(input.EvidenceIDs))
	candidate := &model.RCARootCauseCandidate{
		RunID: runID, RoundID: input.RoundID,
		Summary:    truncateText(auditutil.SanitizeText(strings.TrimSpace(input.Summary)), 4096),
		Confidence: input.Confidence, EvidenceIDs: evidenceIDs, Rejected: input.Rejected,
	}
	if err := s.repository.CreateRCARootCauseCandidate(ctx, candidate); err != nil {
		return nil, err
	}
	return candidate, nil
}

func (s *Service) CompleteRound(ctx context.Context, actor *model.AppUser, runID, roundID int64, input CompleteRoundInput) (*model.RCARound, error) {
	if _, err := s.GetRun(ctx, actor, runID); err != nil {
		return nil, err
	}
	round, err := s.repository.FindRCARoundByID(ctx, roundID)
	if err != nil {
		return nil, err
	}
	if round.RunID != runID || round.Status != model.RCARoundStatusRunning || !validRoundTerminalStatus(input.Status) {
		return nil, ErrInvalidTransition
	}
	if len(input.NewEvidenceIDs) > 0 {
		ok, err := s.repository.EvidenceIDsBelongToRCARun(ctx, runID, input.NewEvidenceIDs)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrInvalidInput
		}
	}
	rejected, err := normalizedHypotheses(input.RejectedHypotheses, true)
	if err != nil {
		return nil, err
	}
	if err := s.validateHypothesisEvidence(ctx, runID, rejected); err != nil {
		return nil, err
	}
	actions, err := normalizedNextActions(input.NextActions)
	if err != nil {
		return nil, err
	}
	evidenceIDs, _ := json.Marshal(uniqueIDs(input.NewEvidenceIDs))
	rejectedRaw, _ := json.Marshal(rejected)
	actionsRaw, _ := json.Marshal(actions)
	now := s.now()
	code, _ := safeFailure(input.ErrorCode, "")
	updated, err := s.repository.UpdateRCARound(ctx, roundID, repository.RCARoundUpdates{
		Status: input.Status, NewEvidenceIDs: evidenceIDs, RejectedHypotheses: rejectedRaw,
		NextActions: actionsRaw, ErrorCode: code, FinishedAt: &now,
	})
	if err != nil {
		return nil, err
	}
	if input.Status == model.RCARoundStatusPartialSuccess {
		_, _ = s.repository.UpdateRCARun(ctx, runID, repository.RCARunUpdates{Status: model.RCARunStatusPartialSuccess})
	}
	duration := time.Duration(0)
	if round.StartedAt != nil {
		duration = now.Sub(*round.StartedAt)
	}
	observability.ObserveRCARound(round.RoundNumber, input.Status, duration)
	slog.InfoContext(ctx, "rca round completed",
		"rca_run_id", runID, "rca_round_id", roundID, "round", round.RoundNumber,
		"status", input.Status, "error_code", input.ErrorCode,
		"evidence_count", len(input.NewEvidenceIDs), "next_action_count", len(input.NextActions),
	)
	return updated, nil
}

func (s *Service) CompleteRun(ctx context.Context, actor *model.AppUser, runID int64, input CompleteRunInput) (*model.RCARun, error) {
	run, err := s.GetRun(ctx, actor, runID)
	if err != nil {
		return nil, err
	}
	if !validRunTerminalStatus(input.Status) || terminalRunStatus(run.Status) {
		return nil, ErrInvalidTransition
	}
	if input.Status == model.RCARunStatusSuccess {
		candidates, err := s.repository.ListRCARootCauseCandidates(ctx, runID)
		if err != nil {
			return nil, err
		}
		hasActiveCandidate := false
		for _, candidate := range candidates {
			if !candidate.Rejected && len(candidate.EvidenceIDs) > 0 {
				hasActiveCandidate = true
				break
			}
		}
		if !hasActiveCandidate {
			return nil, ErrEvidenceRequired
		}
	}
	now := s.now()
	code, message := safeFailure(input.ErrorCode, input.ErrorMessage)
	stopReason := truncateText(auditutil.SanitizeText(strings.TrimSpace(input.StopReason)), 80)
	var stopReasonPointer *string
	if stopReason != "" {
		stopReasonPointer = &stopReason
	}
	return s.repository.UpdateRCARun(ctx, runID, repository.RCARunUpdates{
		Status: input.Status, ErrorCode: code, ErrorMessage: message, StopReason: stopReasonPointer, FinishedAt: &now,
	})
}

func (s *Service) Cancel(ctx context.Context, actor *model.AppUser, runID int64) (*model.RCARun, error) {
	run, err := s.GetRun(ctx, actor, runID)
	if err != nil {
		return nil, err
	}
	if terminalRunStatus(run.Status) {
		return nil, ErrInvalidTransition
	}
	s.orchestratorMu.Lock()
	cancel := s.activeOrchestrators[runID]
	s.orchestratorMu.Unlock()
	if cancel != nil {
		cancel()
	}
	now := s.now()
	rounds, err := s.repository.ListRCARounds(ctx, runID)
	if err != nil {
		return nil, err
	}
	for _, round := range rounds {
		if round.Status == model.RCARoundStatusRunning || round.Status == model.RCARoundStatusPending {
			if _, err := s.repository.UpdateRCARound(ctx, round.ID, repository.RCARoundUpdates{
				Status: model.RCARoundStatusCancelled, FinishedAt: &now,
			}); err != nil {
				return nil, err
			}
		}
	}
	actions, err := s.repository.ListRCAActions(ctx, runID)
	if err != nil {
		return nil, err
	}
	for _, action := range actions {
		if !terminalActionStatus(action.Status) {
			if _, err := s.repository.UpdateRCAAction(ctx, action.ID, repository.RCAActionUpdates{
				Status: model.RCAActionStatusCancelled, FinishedAt: &now,
			}); err != nil {
				return nil, err
			}
		}
	}
	code := "cancelled"
	message := "RCA run was cancelled"
	stopReason := "user_cancelled"
	return s.repository.UpdateRCARun(ctx, runID, repository.RCARunUpdates{
		Status: model.RCARunStatusCancelled, CancelRequestedAt: &now,
		ErrorCode: &code, ErrorMessage: &message, StopReason: &stopReason, FinishedAt: &now,
	})
}

func (s *Service) Recover(ctx context.Context, actor *model.AppUser, runID int64) (*RecoveryPlan, error) {
	run, err := s.GetRun(ctx, actor, runID)
	if err != nil {
		return nil, err
	}
	if run.Status != model.RCARunStatusFailed && run.Status != model.RCARunStatusTimedOut && run.Status != model.RCARunStatusPartialSuccess {
		return nil, ErrInvalidTransition
	}
	actions, err := s.repository.ListRCAActions(ctx, runID)
	if err != nil {
		return nil, err
	}
	plan := &RecoveryPlan{}
	for _, action := range actions {
		if action.Status == model.RCAActionStatusSuccess || action.Status == model.RCAActionStatusSkipped {
			plan.SkippedActionIDs = append(plan.SkippedActionIDs, action.ID)
			continue
		}
		if action.Status == model.RCAActionStatusFailed || action.Status == model.RCAActionStatusTimedOut || action.Status == model.RCAActionStatusPartialSuccess {
			attempt := action.Attempt + 1
			_, err := s.repository.UpdateRCAAction(ctx, action.ID, repository.RCAActionUpdates{
				Status: model.RCAActionStatusPending, Attempt: &attempt, ClearError: true, ClearFinishedAt: true,
			})
			if err != nil {
				return nil, err
			}
			plan.RetryableActionIDs = append(plan.RetryableActionIDs, action.ID)
		}
	}
	rounds, err := s.repository.ListRCARounds(ctx, runID)
	if err != nil {
		return nil, err
	}
	for _, round := range rounds {
		if round.RoundNumber == run.CurrentRound &&
			(round.Status == model.RCARoundStatusFailed || round.Status == model.RCARoundStatusTimedOut || round.Status == model.RCARoundStatusPartialSuccess) {
			if _, err := s.repository.UpdateRCARound(ctx, round.ID, repository.RCARoundUpdates{
				Status: model.RCARoundStatusRunning, ClearError: true, ClearFinishedAt: true,
			}); err != nil {
				return nil, err
			}
		}
	}
	updated, err := s.repository.UpdateRCARun(ctx, runID, repository.RCARunUpdates{
		Status: model.RCARunStatusRunning, ClearError: true, ClearStopReason: true, ClearFinishedAt: true,
	})
	if err != nil {
		return nil, err
	}
	plan.Run = updated
	return plan, nil
}

func (s *Service) listAccessibleEvidence(ctx context.Context, actor *model.AppUser, runID int64) ([]model.EvidenceRecord, error) {
	records, err := s.repository.ListRCAEvidence(ctx, runID)
	if err != nil {
		return nil, err
	}
	if actor.Role == model.RoleAdmin {
		return records, nil
	}
	allowed, err := s.allowedDataSourceSet(ctx, actor)
	if err != nil {
		return nil, err
	}
	result := make([]model.EvidenceRecord, 0, len(records))
	for _, record := range records {
		if record.OwnerUserID == nil || *record.OwnerUserID != actor.ID {
			continue
		}
		if record.DataSourceID != nil {
			if _, ok := allowed[*record.DataSourceID]; !ok {
				continue
			}
		}
		result = append(result, record)
	}
	return result, nil
}

func (s *Service) dataSourceAllowed(ctx context.Context, actor *model.AppUser, id int64) (bool, error) {
	if actor.Role == model.RoleAdmin {
		return true, nil
	}
	allowed, err := s.allowedDataSourceSet(ctx, actor)
	if err != nil {
		return false, err
	}
	_, ok := allowed[id]
	return ok, nil
}

func (s *Service) allowedDataSourceSet(ctx context.Context, actor *model.AppUser) (map[int64]struct{}, error) {
	result := map[int64]struct{}{}
	if s.dataSources == nil {
		return result, nil
	}
	views, err := s.dataSources.List(ctx, actor)
	if err != nil {
		return nil, err
	}
	for _, view := range views {
		if view.Enabled && view.ReadOnly {
			result[view.ID] = struct{}{}
		}
	}
	return result, nil
}

func (s *Service) authorizeScopeDataSources(ctx context.Context, actor *model.AppUser, scope json.RawMessage) error {
	ids := explicitDataSourceIDs(scope)
	if len(ids) == 0 {
		return nil
	}
	allowed, err := s.allowedDataSourceSet(ctx, actor)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, ok := allowed[id]; !ok {
			return ErrForbidden
		}
	}
	return nil
}

func explicitDataSourceIDs(raw json.RawMessage) []int64 {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	ids := []int64{}
	var walk func(any, string)
	walk = func(current any, key string) {
		switch typed := current.(type) {
		case map[string]any:
			for childKey, child := range typed {
				walk(child, childKey)
			}
		case []any:
			for _, child := range typed {
				walk(child, key)
			}
		case float64:
			normalized := strings.ToLower(key)
			if typed > 0 && (strings.HasSuffix(normalized, "datasourceid") || strings.HasSuffix(normalized, "datasourceids")) {
				ids = append(ids, int64(typed))
			}
		}
	}
	walk(value, "")
	return uniqueIDs(ids)
}

func (s *Service) executeRCASkill(ctx context.Context, actor *model.AppUser, name string, payload json.RawMessage, workflowRunID *int64) (*skillframework.ExecuteResult, error) {
	if actor == nil || s.skills == nil {
		return nil, ErrInvalidInput
	}
	sources, err := s.accessiblePlannerDataSources(ctx, actor)
	if err != nil {
		return nil, err
	}
	if !plannerDataSourceAllowed(name, payload, sources) {
		return nil, ErrForbidden
	}
	if s.skillCatalog != nil {
		definition, getErr := s.skillCatalog.Get(name)
		if getErr != nil || !definition.Enabled || !definition.ReadOnly || !plannerSkillAuthorized(actor, definition) {
			return nil, ErrForbidden
		}
		if skillframework.ValidateJSONSchema(definition.InputSchema, payload) != nil {
			return nil, ErrInvalidInput
		}
	}
	return s.skills.Execute(ctx, skillframework.ExecuteInput{
		Actor: actor, Name: name, Payload: payload, WorkflowRunID: workflowRunID,
	})
}

func rcaUserLimiterKey(userID int64) string {
	return strconv.FormatInt(userID, 10)
}

func (s *Service) validateHypothesisEvidence(ctx context.Context, runID int64, hypotheses []Hypothesis) error {
	ids := []int64{}
	for _, hypothesis := range hypotheses {
		ids = append(ids, hypothesis.EvidenceIDs...)
	}
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return nil
	}
	ok, err := s.repository.EvidenceIDsBelongToRCARun(ctx, runID, ids)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInvalidInput
	}
	return nil
}

func authorizeRun(actor *model.AppUser, run *model.RCARun) error {
	if actor == nil || run == nil {
		return ErrForbidden
	}
	if actor.Role != model.RoleAdmin && run.UserID != actor.ID {
		return ErrForbidden
	}
	return nil
}

func normalizedHypotheses(values []Hypothesis, requireEvidence bool) ([]Hypothesis, error) {
	result := make([]Hypothesis, 0, len(values))
	for _, value := range values {
		value.Summary = truncateText(auditutil.SanitizeText(strings.TrimSpace(value.Summary)), 2048)
		value.EvidenceIDs = uniqueIDs(value.EvidenceIDs)
		if value.Summary == "" || value.Confidence < 0 || value.Confidence > 1 || (requireEvidence && len(value.EvidenceIDs) == 0) {
			return nil, ErrInvalidInput
		}
		result = append(result, value)
	}
	return result, nil
}

func normalizedNextActions(values []NextAction) ([]NextAction, error) {
	result := make([]NextAction, 0, len(values))
	for _, value := range values {
		value.ActionKey = truncateText(strings.TrimSpace(value.ActionKey), 160)
		value.SkillName = truncateText(strings.TrimSpace(value.SkillName), 120)
		value.Input = sanitizeJSON(value.Input, 16<<10, []byte(`{}`))
		if value.ActionKey == "" || value.SkillName == "" || value.Input == nil {
			return nil, ErrInvalidInput
		}
		result = append(result, value)
	}
	return result, nil
}

func sanitizeJSON(raw json.RawMessage, maximum int, fallback []byte) []byte {
	if len(raw) == 0 {
		raw = fallback
	}
	return auditutil.SanitizeJSON(raw, maximum)
}

func safeFailure(code, message string) (*string, *string) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, nil
	}
	safeMessages := map[string]string{
		"upstream_timeout":     "upstream query timed out",
		"upstream_unavailable": "upstream data source is unavailable",
		"permission_denied":    "upstream query permission was denied",
		"invalid_response":     "upstream returned an invalid response",
		"resource_limit":       "query resource limit was reached",
		"run_timeout":          "RCA run exceeded its configured deadline",
		"cancelled":            "RCA run was cancelled",
		"internal_error":       "internal RCA operation failed",
	}
	switch code {
	case "upstream_timeout", "upstream_unavailable", "permission_denied", "invalid_response",
		"resource_limit", "run_timeout", "cancelled", "internal_error":
	default:
		code = "internal_error"
	}
	cleanMessage := safeMessages[code]
	return &code, &cleanMessage
}

func validActionTerminalStatus(status string) bool {
	switch status {
	case model.RCAActionStatusSuccess, model.RCAActionStatusPartialSuccess, model.RCAActionStatusFailed, model.RCAActionStatusTimedOut:
		return true
	default:
		return false
	}
}

func validRoundTerminalStatus(status string) bool {
	switch status {
	case model.RCARoundStatusSuccess, model.RCARoundStatusPartialSuccess, model.RCARoundStatusFailed, model.RCARoundStatusTimedOut:
		return true
	default:
		return false
	}
}

func validRunTerminalStatus(status string) bool {
	switch status {
	case model.RCARunStatusSuccess, model.RCARunStatusPartialSuccess, model.RCARunStatusFailed:
		return true
	default:
		return false
	}
}

func terminalRunStatus(status string) bool {
	switch status {
	case model.RCARunStatusSuccess, model.RCARunStatusFailed, model.RCARunStatusCancelled, model.RCARunStatusTimedOut:
		return true
	default:
		return false
	}
}

func terminalActionStatus(status string) bool {
	switch status {
	case model.RCAActionStatusSuccess, model.RCAActionStatusPartialSuccess, model.RCAActionStatusFailed,
		model.RCAActionStatusSkipped, model.RCAActionStatusCancelled, model.RCAActionStatusTimedOut:
		return true
	default:
		return false
	}
}

func truncateText(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum])
}

func uniqueIDs(values []int64) []int64 {
	seen := map[int64]struct{}{}
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
