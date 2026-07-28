package rca

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/observability"
	"aiops-platform/backend/internal/repository"
	"aiops-platform/backend/internal/resourcelimit"
	"aiops-platform/backend/internal/skillframework"
)

const (
	OrchestratorVersion = "rca-orchestrator-v1"

	defaultOrchestratorRounds          = 3
	defaultOrchestratorRoundSkillCalls = 12
	defaultOrchestratorSkillCalls      = 24
	defaultOrchestratorConcurrency     = 4
	defaultOrchestratorTokens          = 16000
	defaultOrchestratorContextBytes    = 64 << 10
	defaultOrchestratorWallTime        = 5 * time.Minute
	defaultOrchestratorConfidence      = .85
)

const (
	StopReasonConfirmed       = "confirmed_by_multi_source_evidence"
	StopReasonNoNewActions    = "no_new_evidence_or_actions"
	StopReasonMaxRounds       = "max_rounds_reached"
	StopReasonSkillBudget     = "skill_call_budget_exhausted"
	StopReasonTokenBudget     = "token_budget_exhausted"
	StopReasonContextBudget   = "context_budget_exhausted"
	StopReasonWallTime        = "wall_time_budget_exhausted"
	StopReasonUserCancelled   = "user_cancelled"
	StopReasonScopeUnresolved = "critical_scope_unresolved"
	StopReasonRoundOneFailed  = "round_one_failed"
)

type OrchestratorBudget struct {
	MaxRounds             int     `json:"maxRounds"`
	MaxSkillCallsPerRound int     `json:"maxSkillCallsPerRound"`
	MaxSkillCalls         int     `json:"maxSkillCalls"`
	MaxConcurrentSkills   int     `json:"maxConcurrentSkills"`
	MaxTokens             int     `json:"maxTokens"`
	MaxContextBytes       int     `json:"maxContextBytes"`
	MaxWallTimeSeconds    int     `json:"maxWallTimeSeconds"`
	ConfidenceThreshold   float64 `json:"confidenceThreshold"`
}

type OrchestrateInput struct {
	RoundOne RoundOneCollectionInput `json:"roundOne"`
	Budget   OrchestratorBudget      `json:"budget"`
	UseLLM   *bool                   `json:"useLlm,omitempty"`
}

type OrchestratorUsage struct {
	RoundsCompleted int `json:"roundsCompleted"`
	SkillCalls      int `json:"skillCalls"`
	EstimatedTokens int `json:"estimatedTokens"`
	ContextBytes    int `json:"contextBytes"`
}

type OrchestratorRoundResult struct {
	RoundNumber int                          `json:"roundNumber"`
	Status      string                       `json:"status"`
	Plan        *PlannerResult               `json:"plan,omitempty"`
	Topology    *TopologyInvestigationResult `json:"topologyInvestigation,omitempty"`
	Database    *DatabaseDiagnosisPlan       `json:"databaseDiagnosis,omitempty"`
	ActionIDs   []int64                      `json:"actionIds"`
	EvidenceIDs []int64                      `json:"evidenceIds"`
	Errors      []string                     `json:"errors,omitempty"`
}

type OrchestratorResult struct {
	Version    string                    `json:"version"`
	Run        *model.RCARun             `json:"run"`
	Report     *RCAReport                `json:"report,omitempty"`
	StopReason string                    `json:"stopReason"`
	Usage      OrchestratorUsage         `json:"usage"`
	Rounds     []OrchestratorRoundResult `json:"rounds"`
	Degraded   bool                      `json:"degraded"`
}

type plannedExecution struct {
	plan     PlannerAction
	action   *model.RCAAction
	result   *skillframework.ExecuteResult
	err      error
	partial  bool
	evidence []int64
}

func (s *Service) Orchestrate(ctx context.Context, actor *model.AppUser, runID int64, input OrchestrateInput) (result *OrchestratorResult, resultErr error) {
	startedAt := time.Now()
	defer func() {
		status, stopReason := "error", ""
		if result != nil {
			stopReason = result.StopReason
			if result.Run != nil {
				status = result.Run.Status
			}
		}
		observability.ObserveRCAOrchestration(status, stopReason, time.Since(startedAt))
		slog.InfoContext(ctx, "rca orchestration completed",
			"rca_run_id", runID, "status", status, "stop_reason", stopReason,
			"duration_ms", time.Since(startedAt).Milliseconds(), "error", safeRCAErrorCode(resultErr),
		)
	}()
	if actor == nil || runID <= 0 || s.skills == nil || s.skillCatalog == nil {
		return nil, ErrInvalidInput
	}
	run, err := s.GetRun(ctx, actor, runID)
	if err != nil {
		return nil, err
	}
	if terminalRunStatus(run.Status) || run.FinishedAt != nil {
		return nil, ErrInvalidTransition
	}
	releaseUser, err := s.userOrchestratorLimiter.Acquire(ctx, rcaUserLimiterKey(actor.ID))
	if err != nil {
		if errors.Is(err, resourcelimit.ErrLimitExceeded) {
			observability.ObserveRCALimit("user")
			return nil, ErrOrchestratorLimited
		}
		return nil, err
	}
	defer releaseUser()
	releaseGlobal, err := s.globalOrchestratorLimiter.Acquire(ctx)
	if err != nil {
		if errors.Is(err, resourcelimit.ErrLimitExceeded) {
			observability.ObserveRCALimit("global")
			return nil, ErrOrchestratorLimited
		}
		return nil, err
	}
	defer releaseGlobal()
	observability.AddActiveRCA("global", 1)
	defer observability.AddActiveRCA("global", -1)
	slog.InfoContext(ctx, "rca orchestration started",
		"rca_run_id", runID, "user_id", actor.ID, "current_round", run.CurrentRound,
		"max_rounds", run.MaxRounds,
	)
	budget, err := normalizeOrchestratorBudget(input.Budget, run.MaxRounds)
	if err != nil {
		return nil, err
	}
	if !criticalScopeResolved(run.Scope) {
		return s.finishOrchestration(ctx, actor, runID, &OrchestratorResult{
			Version: OrchestratorVersion, Run: run, StopReason: StopReasonScopeUnresolved,
		}, model.RCARunStatusFailed, StopReasonScopeUnresolved)
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(budget.MaxWallTimeSeconds)*time.Second)
	if run.TimeoutAt != nil {
		if remaining := run.TimeoutAt.Sub(s.now()); remaining > 0 && remaining < time.Duration(budget.MaxWallTimeSeconds)*time.Second {
			cancel()
			runCtx, cancel = context.WithTimeout(ctx, remaining)
		}
	}
	defer cancel()
	if !s.registerOrchestrator(runID, cancel) {
		return nil, ErrOrchestratorActive
	}
	defer s.unregisterOrchestrator(runID)

	outcome := &OrchestratorResult{Version: OrchestratorVersion, Run: run, Rounds: []OrchestratorRoundResult{}}
	if resumed, resumeErr := s.resumeRunningRound(runCtx, actor, runID, budget, &outcome.Usage); resumeErr != nil {
		return s.finishAfterExecutionError(ctx, actor, runID, outcome, resumeErr)
	} else if resumed != nil {
		outcome.Rounds = append(outcome.Rounds, *resumed)
	}
	run, err = s.GetRun(runCtx, actor, runID)
	if err != nil {
		return nil, err
	}
	if run.CurrentRound == 0 {
		roundOneInput := input.RoundOne
		roundOneInput.MaxConcurrency = budget.MaxConcurrentSkills
		roundOneInput.MaxSkillCalls = minIntRCA(budget.MaxSkillCalls, budget.MaxSkillCallsPerRound)
		first, collectionErr := s.CollectRoundOne(runCtx, actor, runID, roundOneInput)
		if collectionErr != nil {
			return s.finishAfterExecutionError(ctx, actor, runID, outcome, collectionErr)
		}
		firstRound := OrchestratorRoundResult{RoundNumber: 1, Status: first.Status}
		for _, action := range first.Actions {
			firstRound.ActionIDs = append(firstRound.ActionIDs, action.ID)
		}
		for _, item := range first.Evidence {
			firstRound.EvidenceIDs = append(firstRound.EvidenceIDs, item.ID)
		}
		outcome.Rounds = append(outcome.Rounds, firstRound)
		outcome.Usage.RoundsCompleted++
		outcome.Usage.SkillCalls += len(first.Attempts)
		if first.Status == model.RCARoundStatusFailed {
			outcome.StopReason = StopReasonRoundOneFailed
			reason := StopReasonRoundOneFailed
			current, _ := s.repository.UpdateRCARun(ctx, runID, repository.RCARunUpdates{StopReason: &reason})
			outcome.Run = current
			return outcome, nil
		}
	}

	var lastPlan *PlannerResult
	for {
		if reason := s.orchestratorBudgetStop(runCtx, actor, runID, budget, &outcome.Usage); reason != "" {
			outcome.StopReason = reason
			break
		}
		run, err = s.GetRun(runCtx, actor, runID)
		if err != nil {
			return s.finishAfterExecutionError(ctx, actor, runID, outcome, err)
		}
		if run.CurrentRound >= budget.MaxRounds {
			outcome.StopReason = StopReasonMaxRounds
			break
		}
		contextBytes, contextErr := s.plannerContextBytes(runCtx, actor, runID)
		if contextErr != nil {
			return s.finishAfterExecutionError(ctx, actor, runID, outcome, contextErr)
		}
		outcome.Usage.ContextBytes = contextBytes
		if contextBytes > budget.MaxContextBytes {
			outcome.StopReason = StopReasonContextBudget
			break
		}
		estimated := maxIntRCA(1, contextBytes/4)
		if outcome.Usage.EstimatedTokens+estimated > budget.MaxTokens {
			outcome.StopReason = StopReasonTokenBudget
			break
		}
		outcome.Usage.EstimatedTokens += estimated
		remainingCalls := minIntRCA(budget.MaxSkillCalls-outcome.Usage.SkillCalls, budget.MaxSkillCallsPerRound)
		remainingRounds := budget.MaxRounds - run.CurrentRound
		plan, planErr := s.PlanNext(runCtx, actor, runID, PlanRequest{
			Budget: PlannerBudget{
				RemainingRounds: remainingRounds, RemainingSkillCalls: remainingCalls,
				RemainingWallTimeSeconds: budget.MaxWallTimeSeconds,
			},
			UseLLM: input.UseLLM,
		})
		if planErr != nil {
			return s.finishAfterExecutionError(ctx, actor, runID, outcome, planErr)
		}
		lastPlan = plan
		if supportedByMultipleSources(plan.Hypotheses, mustDetail(s.GetDetail(runCtx, actor, runID))) && topConfidence(plan.Hypotheses) >= budget.ConfidenceThreshold {
			outcome.StopReason = StopReasonConfirmed
			break
		}
		if run.CurrentRound == 1 {
			roundResult, executeErr := s.executeTopologyGuidedRound(runCtx, actor, runID, plan, remainingCalls)
			if roundResult != nil {
				outcome.Rounds = append(outcome.Rounds, *roundResult)
				outcome.Usage.RoundsCompleted++
				outcome.Usage.SkillCalls += len(roundResult.ActionIDs)
				if len(roundResult.Errors) > 0 || roundResult.Status != model.RCARoundStatusSuccess {
					outcome.Degraded = true
				}
			}
			if executeErr != nil && !errors.Is(executeErr, ErrRoundPartial) {
				return s.finishAfterExecutionError(ctx, actor, runID, outcome, executeErr)
			}
			if executeErr != nil {
				outcome.Degraded = true
			}
			continue
		}
		actions := plan.NextActions
		var databasePlan *DatabaseDiagnosisPlan
		if run.CurrentRound == 2 {
			detail := mustDetail(s.GetDetail(runCtx, actor, runID))
			databasePlan, actions = s.buildDatabaseDeepDiagnosisPlan(runCtx, actor, detail, plan, remainingCalls)
			if len(actions) == 0 {
				actions = highestPriorityActions(plan.Hypotheses, plan.NextActions)
				if len(actions) == 0 {
					actions = s.validatedDeepeningActions(runCtx, actor, buildPlannerInput(detail, PlanRequest{
						Budget: PlannerBudget{RemainingRounds: remainingRounds, RemainingSkillCalls: remainingCalls, RemainingWallTimeSeconds: budget.MaxWallTimeSeconds},
					}), plan)
				}
			}
		}
		if len(actions) == 0 {
			outcome.StopReason = StopReasonNoNewActions
			break
		}
		if len(actions) > remainingCalls {
			actions = actions[:remainingCalls]
		}
		roundResult, executeErr := s.executePlannerRound(runCtx, actor, runID, plan, actions, budget.MaxConcurrentSkills)
		if roundResult != nil {
			if databasePlan != nil {
				databasePlan.Assessment = s.assessDatabaseDiagnosisRound(runCtx, actor, runID, roundResult.ActionIDs)
			}
			roundResult.Database = databasePlan
			outcome.Rounds = append(outcome.Rounds, *roundResult)
			outcome.Usage.RoundsCompleted++
			outcome.Usage.SkillCalls += len(roundResult.ActionIDs)
			if len(roundResult.Errors) > 0 {
				outcome.Degraded = true
			}
		}
		if executeErr != nil && !errors.Is(executeErr, ErrRoundPartial) {
			return s.finishAfterExecutionError(ctx, actor, runID, outcome, executeErr)
		}
		if executeErr != nil {
			outcome.Degraded = true
		}
	}
	if outcome.StopReason == "" {
		outcome.StopReason = StopReasonNoNewActions
	}
	_ = lastPlan
	return s.finishOrchestration(ctx, actor, runID, outcome, orchestratorFinalStatus(outcome), outcome.StopReason)
}

var ErrRoundPartial = errors.New("RCA round completed with partial evidence")

func normalizeOrchestratorBudget(input OrchestratorBudget, runMaxRounds int) (OrchestratorBudget, error) {
	if input.MaxRounds == 0 {
		input.MaxRounds = defaultOrchestratorRounds
	}
	if input.MaxSkillCallsPerRound == 0 {
		input.MaxSkillCallsPerRound = defaultOrchestratorRoundSkillCalls
	}
	if input.MaxSkillCalls == 0 {
		input.MaxSkillCalls = defaultOrchestratorSkillCalls
	}
	if input.MaxConcurrentSkills == 0 {
		input.MaxConcurrentSkills = defaultOrchestratorConcurrency
	}
	if input.MaxTokens == 0 {
		input.MaxTokens = defaultOrchestratorTokens
	}
	if input.MaxContextBytes == 0 {
		input.MaxContextBytes = defaultOrchestratorContextBytes
	}
	if input.MaxWallTimeSeconds == 0 {
		input.MaxWallTimeSeconds = int(defaultOrchestratorWallTime / time.Second)
	}
	if input.ConfidenceThreshold == 0 {
		input.ConfidenceThreshold = defaultOrchestratorConfidence
	}
	input.MaxRounds = minIntRCA(input.MaxRounds, minIntRCA(runMaxRounds, defaultOrchestratorRounds))
	if input.MaxRounds < 1 || input.MaxSkillCallsPerRound < 1 || input.MaxSkillCallsPerRound > 50 ||
		input.MaxSkillCalls < 1 || input.MaxSkillCalls > 200 || input.MaxConcurrentSkills < 1 || input.MaxConcurrentSkills > 16 ||
		input.MaxTokens < 1 || input.MaxTokens > 1_000_000 || input.MaxContextBytes < 1024 || input.MaxContextBytes > 4<<20 ||
		input.MaxWallTimeSeconds < 1 || time.Duration(input.MaxWallTimeSeconds)*time.Second > maxTimeout ||
		input.ConfidenceThreshold < 0 || input.ConfidenceThreshold > 1 {
		return OrchestratorBudget{}, ErrInvalidInput
	}
	return input, nil
}

func criticalScopeResolved(raw json.RawMessage) bool {
	scope := decodePlannerScope(raw)
	return scope.string("serviceName", "component", "serviceQuery", "topologyNodeKey", "nodeKey") != ""
}

func (s *Service) registerOrchestrator(runID int64, cancel context.CancelFunc) bool {
	s.orchestratorMu.Lock()
	defer s.orchestratorMu.Unlock()
	if _, exists := s.activeOrchestrators[runID]; exists {
		return false
	}
	s.activeOrchestrators[runID] = cancel
	return true
}

func (s *Service) unregisterOrchestrator(runID int64) {
	s.orchestratorMu.Lock()
	delete(s.activeOrchestrators, runID)
	s.orchestratorMu.Unlock()
}

func (s *Service) orchestratorBudgetStop(ctx context.Context, actor *model.AppUser, runID int64, budget OrchestratorBudget, usage *OrchestratorUsage) string {
	if ctx.Err() != nil {
		run, _ := s.repository.FindRCARunByID(context.Background(), runID)
		if run != nil && run.CancelRequestedAt != nil {
			return StopReasonUserCancelled
		}
		return StopReasonWallTime
	}
	run, err := s.GetRun(ctx, actor, runID)
	if err == nil && run.Status == model.RCARunStatusCancelled {
		return StopReasonUserCancelled
	}
	if usage.SkillCalls >= budget.MaxSkillCalls {
		return StopReasonSkillBudget
	}
	return ""
}

func (s *Service) plannerContextBytes(ctx context.Context, actor *model.AppUser, runID int64) (int, error) {
	detail, err := s.GetDetail(ctx, actor, runID)
	if err != nil {
		return 0, err
	}
	total := len(detail.Run.Scope) + len(detail.Run.Query)
	for _, item := range detail.Evidence {
		total += len(item.Summary) + len(item.Content) + len(item.Entity) + len(item.SourceRef)
	}
	for _, item := range detail.Rounds {
		total += len(item.InputHypotheses) + len(item.NewEvidenceIDs) + len(item.NextActions)
	}
	return total, nil
}

func (s *Service) executePlannerRound(ctx context.Context, actor *model.AppUser, runID int64, plan *PlannerResult, actions []PlannerAction, concurrency int) (*OrchestratorRoundResult, error) {
	hypotheses := make([]Hypothesis, 0, len(plan.Hypotheses))
	for _, item := range plan.Hypotheses {
		hypotheses = append(hypotheses, Hypothesis{ID: item.ID, Summary: item.Summary, Confidence: item.Confidence, EvidenceIDs: item.SupportingEvidenceIDs})
	}
	round, err := s.StartRound(ctx, actor, runID, StartRoundInput{InputHypotheses: hypotheses})
	if err != nil {
		return nil, err
	}
	executions := make([]plannedExecution, 0, len(actions))
	for _, actionPlan := range actions {
		definition, getErr := s.skillCatalog.Get(actionPlan.SkillName)
		if getErr != nil {
			return nil, getErr
		}
		action, createErr := s.CreateAction(ctx, actor, runID, CreateActionInput{
			RoundID: round.ID, ActionKey: actionPlan.ActionKey, SkillName: actionPlan.SkillName,
			Input: actionPlan.Input, SensitiveRead: definition.RiskLevel == model.SkillRiskSensitiveRead,
		})
		if createErr != nil {
			return nil, createErr
		}
		if action.Status == model.RCAActionStatusSuccess || action.Status == model.RCAActionStatusSkipped {
			var ids []int64
			_ = json.Unmarshal(action.EvidenceIDs, &ids)
			executions = append(executions, plannedExecution{plan: actionPlan, action: action, evidence: ids})
			continue
		}
		if action.Status == model.RCAActionStatusRunning {
			attempt := action.Attempt + 1
			action, createErr = s.repository.UpdateRCAAction(ctx, action.ID, repository.RCAActionUpdates{
				Status: model.RCAActionStatusPending, Attempt: &attempt, ClearError: true, ClearFinishedAt: true,
			})
			if createErr != nil {
				return nil, createErr
			}
		}
		action, createErr = s.StartAction(ctx, actor, runID, action.ID)
		if createErr != nil {
			return nil, createErr
		}
		executions = append(executions, plannedExecution{plan: actionPlan, action: action})
	}
	sem := make(chan struct{}, maxIntRCA(1, concurrency))
	var wait sync.WaitGroup
	for index := range executions {
		if len(executions[index].evidence) > 0 {
			continue
		}
		wait.Add(1)
		go func(execution *plannedExecution) {
			defer wait.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				execution.err = ctx.Err()
				return
			}
			execution.result, execution.err = s.executeRCASkill(
				ctx, actor, execution.plan.SkillName, execution.plan.Input,
				mustRunWorkflowID(s.repository.FindRCARunByID(ctx, runID)),
			)
			if execution.err == nil && execution.result == nil {
				execution.err = errors.New("invalid skill response")
			}
			if execution.err == nil {
				execution.partial = outputIsPartial(execution.result.Output)
			}
		}(&executions[index])
	}
	wait.Wait()
	return s.completePlannerRound(ctx, actor, runID, round, plan, executions)
}

func (s *Service) completePlannerRound(ctx context.Context, actor *model.AppUser, runID int64, round *model.RCARound, plan *PlannerResult, executions []plannedExecution) (*OrchestratorRoundResult, error) {
	result := &OrchestratorRoundResult{RoundNumber: round.RoundNumber, Plan: plan, Status: model.RCARoundStatusSuccess}
	degraded := false
	for index := range executions {
		execution := &executions[index]
		result.ActionIDs = append(result.ActionIDs, execution.action.ID)
		if len(execution.evidence) > 0 {
			result.EvidenceIDs = append(result.EvidenceIDs, execution.evidence...)
			continue
		}
		status, errorCode := model.RCAActionStatusSuccess, ""
		output := json.RawMessage(`{}`)
		if execution.result != nil {
			output = execution.result.Output
		}
		if execution.err != nil {
			degraded = true
			status, errorCode = model.RCAActionStatusFailed, classifyRoundOneError(execution.err)
			result.Errors = append(result.Errors, execution.plan.SkillName+":"+errorCode)
		} else {
			record, evidenceErr := s.addPlannerActionEvidence(ctx, actor, runID, round, execution)
			if evidenceErr != nil {
				degraded = true
				status, errorCode = model.RCAActionStatusFailed, "evidence_persist_failed"
				result.Errors = append(result.Errors, execution.plan.SkillName+":"+errorCode)
			} else if record != nil {
				execution.evidence = []int64{record.ID}
				result.EvidenceIDs = append(result.EvidenceIDs, record.ID)
			}
			if execution.partial {
				degraded = true
				status, errorCode = model.RCAActionStatusPartialSuccess, "upstream_unavailable"
				result.Errors = append(result.Errors, execution.plan.SkillName+":"+errorCode)
			}
		}
		if _, err := s.CompleteAction(ctx, actor, runID, execution.action.ID, CompleteActionInput{
			Status: status, Output: output, EvidenceIDs: execution.evidence, ErrorCode: errorCode,
		}); err != nil {
			return result, err
		}
	}
	result.EvidenceIDs = uniqueIDs(result.EvidenceIDs)
	if len(result.EvidenceIDs) == 0 {
		result.Status = model.RCARoundStatusFailed
	} else if degraded {
		result.Status = model.RCARoundStatusPartialSuccess
	}
	errorCode := ""
	if result.Status != model.RCARoundStatusSuccess {
		errorCode = "partial_evidence"
	}
	nextActions := []NextAction{}
	for _, action := range plan.NextActions {
		nextActions = append(nextActions, NextAction{ActionKey: action.ActionKey, SkillName: action.SkillName, Input: action.Input})
	}
	if _, err := s.CompleteRound(ctx, actor, runID, round.ID, CompleteRoundInput{
		Status: result.Status, NewEvidenceIDs: result.EvidenceIDs, NextActions: nextActions, ErrorCode: errorCode,
	}); err != nil {
		return result, err
	}
	if degraded {
		return result, ErrRoundPartial
	}
	return result, nil
}

func (s *Service) addPlannerActionEvidence(ctx context.Context, actor *model.AppUser, runID int64, round *model.RCARound, execution *plannedExecution) (*model.EvidenceRecord, error) {
	key := fmt.Sprintf("rca_%d_round%d_%s", runID, round.RoundNumber, execution.action.ActionKey)
	existing, err := s.repository.ListRCAEvidence(ctx, runID)
	if err != nil {
		return nil, err
	}
	for _, record := range existing {
		if record.EvidenceKey == key {
			return &record, nil
		}
	}
	dataSourceID := plannerActionDataSourceID(execution.plan.Input)
	now := s.now()
	confidence := .8
	scope, _ := s.repository.FindRCARunByID(ctx, runID)
	entity := json.RawMessage(`{}`)
	if scope != nil {
		entity = scope.Scope
	}
	return s.AddEvidence(ctx, actor, runID, CreateEvidenceInput{
		RoundID: round.ID, ActionID: &execution.action.ID, EvidenceKey: key,
		SourceType: plannerEvidenceSource(execution.plan.SkillName), SourceRef: mustJSON(map[string]any{
			"skillRunId": execution.result.RunID, "actionKey": execution.plan.ActionKey,
			"inputEvidenceIds": execution.plan.EvidenceIDs,
		}),
		ObservedAt: &now, Summary: plannerEvidenceSummary(execution.plan.SkillName, execution.result.Output),
		Content: execution.result.Output, Confidence: &confidence, Sensitivity: model.EvidenceSensitivityInternal,
		EvidenceKind: plannerEvidenceKind(execution.plan.SkillName), Entity: entity,
		SourceSkill: execution.plan.SkillName, DataSourceID: dataSourceID,
	})
}

func plannerEvidenceSource(skill string) string {
	switch {
	case strings.Contains(skill, "topology") || strings.Contains(skill, "dependencies"):
		return "topology"
	case strings.Contains(skill, "tidb"):
		return model.DataSourceTypeTiDB
	case strings.Contains(skill, "redis"):
		return model.DataSourceTypeRedis
	case strings.Contains(skill, "nginx"):
		return model.DataSourceTypeNginx
	case strings.Contains(skill, "k8s") || strings.Contains(skill, "pod"):
		return model.DataSourceTypeKubernetes
	case strings.Contains(skill, "linux"):
		return model.DataSourceTypeLinuxServer
	default:
		return "skill"
	}
}

func plannerEvidenceKind(skill string) string {
	if strings.HasPrefix(skill, "diagnose_") || strings.HasPrefix(skill, "run_") {
		return model.EvidenceKindRule
	}
	return model.EvidenceKindFact
}

func plannerEvidenceSummary(skill string, output json.RawMessage) string {
	summary := strings.ReplaceAll(skill, "_", " ") + " returned structured evidence"
	var body map[string]any
	if json.Unmarshal(output, &body) == nil {
		if value, ok := body["summary"].(string); ok && strings.TrimSpace(value) != "" {
			summary += ": " + value
		}
	}
	return truncateText(summary, 1024)
}

func plannerActionDataSourceID(input json.RawMessage) *int64 {
	var body map[string]any
	if json.Unmarshal(input, &body) != nil {
		return nil
	}
	if value, ok := body["dataSourceId"].(float64); ok && value > 0 {
		id := int64(value)
		return &id
	}
	return nil
}

func prioritizeTopologyActions(actions []PlannerAction) []PlannerAction {
	result := append([]PlannerAction{}, actions...)
	sort.SliceStable(result, func(i, j int) bool {
		return plannerEvidenceSource(result[i].SkillName) == "topology" && plannerEvidenceSource(result[j].SkillName) != "topology"
	})
	return result
}

func highestPriorityActions(hypotheses []PlannerHypothesis, actions []PlannerAction) []PlannerAction {
	if len(hypotheses) == 0 {
		return nil
	}
	top := hypotheses[0]
	result := []PlannerAction{}
	for _, action := range actions {
		if actionMatchesHypothesis(action.SkillName, top.ID) {
			result = append(result, action)
		}
	}
	return result
}

func actionMatchesHypothesis(skill, hypothesis string) bool {
	switch {
	case strings.Contains(hypothesis, "database"):
		return strings.Contains(skill, "tidb")
	case strings.Contains(hypothesis, "redis"):
		return strings.Contains(skill, "redis")
	case strings.Contains(hypothesis, "nginx"):
		return strings.Contains(skill, "nginx")
	case strings.Contains(hypothesis, "k8s"):
		return strings.Contains(skill, "k8s") || strings.Contains(skill, "pod")
	case strings.Contains(hypothesis, "linux"):
		return strings.Contains(skill, "linux")
	default:
		return strings.Contains(skill, "topology") || strings.Contains(skill, "dependencies")
	}
}

func (s *Service) validatedDeepeningActions(ctx context.Context, actor *model.AppUser, input PlannerInput, plan *PlannerResult) []PlannerAction {
	if len(plan.Hypotheses) == 0 {
		return nil
	}
	scope := decodePlannerScope(input.Scope)
	var candidate *PlannerAction
	switch {
	case strings.Contains(plan.Hypotheses[0].ID, "database"):
		if id := scope.int64("tidbDataSourceId", "databaseDataSourceId", "dataSourceId"); id > 0 {
			action := plannerAction("query_tidb_processlist", map[string]any{"dataSourceId": id, "limit": 20}, "针对最高优先级数据库假设继续收集连接与执行状态", "database")
			candidate = &action
		}
	case strings.Contains(plan.Hypotheses[0].ID, "redis"):
		if id := scope.int64("redisDataSourceId", "dataSourceId"); id > 0 {
			action := plannerAction("query_redis_slowlog", map[string]any{"dataSourceId": id, "limit": 20}, "针对最高优先级 Redis 假设继续收集慢命令证据", "redis")
			candidate = &action
		}
	case strings.Contains(plan.Hypotheses[0].ID, "k8s"):
		actions, _ := k8sPlannerActions(scope)
		if len(actions) > 0 {
			action := actions[0]
			action.SkillName = "get_pod_context"
			action.ActionKey = plannerAction("get_pod_context", mustObject(action.Input), action.Reason, action.TargetEntity).ActionKey
			candidate = &action
		}
	}
	if candidate == nil {
		return nil
	}
	sources, err := s.accessiblePlannerDataSources(ctx, actor)
	if err != nil {
		return nil
	}
	checked := s.validatePlannerResult(actor, input, PlannerResult{NextActions: []PlannerAction{*candidate}}, sources)
	return checked.NextActions
}

func supportedByMultipleSources(hypotheses []PlannerHypothesis, detail *Detail) bool {
	if detail == nil || len(hypotheses) == 0 {
		return false
	}
	sourceByID := map[int64]string{}
	for _, item := range detail.Evidence {
		sourceByID[item.ID] = item.SourceType
	}
	sources := map[string]struct{}{}
	for _, id := range hypotheses[0].SupportingEvidenceIDs {
		if source := sourceByID[id]; source != "" {
			sources[source] = struct{}{}
		}
	}
	return len(sources) >= 2
}

func topConfidence(hypotheses []PlannerHypothesis) float64 {
	if len(hypotheses) == 0 {
		return 0
	}
	return hypotheses[0].Confidence
}

func (s *Service) resumeRunningRound(ctx context.Context, actor *model.AppUser, runID int64, budget OrchestratorBudget, usage *OrchestratorUsage) (*OrchestratorRoundResult, error) {
	detail, err := s.GetDetail(ctx, actor, runID)
	if err != nil || len(detail.Rounds) == 0 {
		return nil, err
	}
	latest := detail.Rounds[len(detail.Rounds)-1]
	if latest.Status != model.RCARoundStatusRunning {
		return nil, nil
	}
	actions := []PlannerAction{}
	successful := []plannedExecution{}
	for _, action := range detail.Actions {
		if action.RoundID != latest.ID {
			continue
		}
		if action.Status == model.RCAActionStatusSuccess || action.Status == model.RCAActionStatusSkipped {
			var ids []int64
			_ = json.Unmarshal(action.EvidenceIDs, &ids)
			copied := action
			successful = append(successful, plannedExecution{
				plan:   PlannerAction{ActionKey: action.ActionKey, SkillName: action.SkillName, Input: action.Input, Reason: "reuse successful read-only action"},
				action: &copied, evidence: ids,
			})
			continue
		}
		if action.Status == model.RCAActionStatusRunning {
			attempt := action.Attempt + 1
			if _, err := s.repository.UpdateRCAAction(ctx, action.ID, repository.RCAActionUpdates{
				Status: model.RCAActionStatusPending, Attempt: &attempt, ClearError: true, ClearFinishedAt: true,
			}); err != nil {
				return nil, err
			}
		}
		actions = append(actions, PlannerAction{ActionKey: action.ActionKey, SkillName: action.SkillName, Input: action.Input, Reason: "resume interrupted read-only action"})
	}
	if len(actions) == 0 {
		result := &OrchestratorRoundResult{RoundNumber: latest.RoundNumber, Status: model.RCARoundStatusFailed}
		for _, execution := range successful {
			result.ActionIDs = append(result.ActionIDs, execution.action.ID)
			result.EvidenceIDs = append(result.EvidenceIDs, execution.evidence...)
		}
		if len(result.EvidenceIDs) > 0 {
			result.Status = model.RCARoundStatusSuccess
		}
		if _, completeErr := s.CompleteRound(ctx, actor, runID, latest.ID, CompleteRoundInput{
			Status: result.Status, NewEvidenceIDs: uniqueIDs(result.EvidenceIDs),
		}); completeErr != nil {
			return result, completeErr
		}
		usage.RoundsCompleted++
		if result.Status == model.RCARoundStatusFailed {
			return result, ErrRoundPartial
		}
		return result, nil
	}
	if len(actions) > budget.MaxSkillCallsPerRound {
		actions = actions[:budget.MaxSkillCallsPerRound]
	}
	plan := &PlannerResult{Version: PlannerVersion, Round: latest.RoundNumber, NextActions: actions}
	executions := append([]plannedExecution{}, successful...)
	for _, actionPlan := range actions {
		for index := range detail.Actions {
			action := detail.Actions[index]
			if action.ActionKey != actionPlan.ActionKey {
				continue
			}
			started, startErr := s.StartAction(ctx, actor, runID, action.ID)
			if startErr != nil {
				return nil, startErr
			}
			executions = append(executions, plannedExecution{plan: actionPlan, action: started})
			break
		}
	}
	sem := make(chan struct{}, maxIntRCA(1, budget.MaxConcurrentSkills))
	var wait sync.WaitGroup
	for index := range executions {
		wait.Add(1)
		go func(execution *plannedExecution) {
			defer wait.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				execution.err = ctx.Err()
				return
			}
			execution.result, execution.err = s.executeRCASkill(ctx, actor, execution.plan.SkillName, execution.plan.Input, nil)
		}(&executions[index])
	}
	wait.Wait()
	result, completeErr := s.completePlannerRound(ctx, actor, runID, &latest, plan, executions)
	if result != nil {
		usage.SkillCalls += len(actions)
		usage.RoundsCompleted++
	}
	return result, completeErr
}

func safeRCAErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrForbidden):
		return "forbidden"
	case errors.Is(err, ErrOrchestratorLimited):
		return "concurrency_limited"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "internal_error"
	}
}

func (s *Service) finishAfterExecutionError(ctx context.Context, actor *model.AppUser, runID int64, outcome *OrchestratorResult, err error) (*OrchestratorResult, error) {
	reason := StopReasonNoNewActions
	stored, _ := s.repository.FindRCARunByID(ctx, runID)
	switch {
	case stored != nil && stored.CancelRequestedAt != nil:
		reason = StopReasonUserCancelled
	case errors.Is(err, context.Canceled):
		reason = StopReasonWallTime
	case errors.Is(err, context.DeadlineExceeded):
		reason = StopReasonWallTime
	}
	outcome.StopReason = reason
	finished, finishErr := s.finishOrchestration(ctx, actor, runID, outcome, model.RCARunStatusPartialSuccess, reason)
	if finishErr != nil {
		return nil, finishErr
	}
	if reason == StopReasonUserCancelled {
		return finished, nil
	}
	return finished, err
}

func (s *Service) finishOrchestration(ctx context.Context, actor *model.AppUser, runID int64, outcome *OrchestratorResult, status, reason string) (*OrchestratorResult, error) {
	run, err := s.repository.FindRCARunByID(ctx, runID)
	if err != nil {
		return nil, err
	}
	if terminalRunStatus(run.Status) {
		if run.StopReason == nil || strings.TrimSpace(*run.StopReason) == "" {
			run, err = s.repository.UpdateRCARun(ctx, runID, repository.RCARunUpdates{StopReason: &reason})
			if err != nil {
				return nil, err
			}
		}
		outcome.Run, outcome.StopReason = run, reason
		outcome.Report, _ = s.BuildReport(ctx, actor, runID)
		return outcome, nil
	}
	if status == model.RCARunStatusSuccess {
		candidates, _ := s.repository.ListRCARootCauseCandidates(ctx, runID)
		if len(candidates) == 0 {
			status = model.RCARunStatusPartialSuccess
		}
	}
	updated, err := s.CompleteRun(ctx, actor, runID, CompleteRunInput{Status: status, StopReason: reason})
	if err != nil {
		return nil, err
	}
	outcome.Run, outcome.StopReason = updated, reason
	outcome.Report, _ = s.BuildReport(ctx, actor, runID)
	return outcome, nil
}

func orchestratorFinalStatus(outcome *OrchestratorResult) string {
	if outcome.StopReason == StopReasonScopeUnresolved || outcome.StopReason == StopReasonRoundOneFailed {
		return model.RCARunStatusFailed
	}
	if len(outcome.Rounds) == 0 {
		return model.RCARunStatusPartialSuccess
	}
	if outcome.Degraded {
		return model.RCARunStatusPartialSuccess
	}
	return model.RCARunStatusSuccess
}

func mustDetail(detail *Detail, err error) *Detail {
	if err != nil {
		return nil
	}
	return detail
}

func mustRunWorkflowID(run *model.RCARun, err error) *int64 {
	if err != nil || run == nil {
		return nil
	}
	return run.WorkflowRunID
}

func mustObject(raw json.RawMessage) map[string]any {
	var value map[string]any
	_ = json.Unmarshal(raw, &value)
	return value
}

func maxIntRCA(left, right int) int {
	if left > right {
		return left
	}
	return right
}
