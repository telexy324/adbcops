package rca

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	datasourcesvc "aiops-platform/backend/internal/datasource"
	evidencesvc "aiops-platform/backend/internal/evidence"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

func TestRCAStateTracksRoundEvidenceActionsAndPartialSuccess(t *testing.T) {
	repo := newMemoryRCARepository()
	evidence := &memoryEvidenceCreator{repository: repo}
	service := NewService(repo, evidence, fakeRCADataSources{views: accessibleViews(1)})
	service.now = fixedRCATime
	actor := rcaUser(11)

	run, err := service.CreateRun(context.Background(), actor, CreateRunInput{
		Query: "订单服务变慢，请查询可能原因", Scope: json.RawMessage(`{"environment":"prod"}`),
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	round, err := service.StartRound(context.Background(), actor, run.ID, StartRoundInput{
		InputHypotheses: []Hypothesis{{ID: "h1", Summary: "数据库调用可能变慢", Confidence: 0.4}},
	})
	if err != nil {
		t.Fatalf("start round: %v", err)
	}
	action, err := service.CreateAction(context.Background(), actor, run.ID, CreateActionInput{
		RoundID: round.ID, ActionKey: "round1:query_logs", SkillName: "query_logs",
		Input: json.RawMessage(`{"query":"timeout"}`), SensitiveRead: true,
	})
	if err != nil {
		t.Fatalf("create action: %v", err)
	}
	action, err = service.StartAction(context.Background(), actor, run.ID, action.ID)
	if err != nil || action.Status != model.RCAActionStatusRunning || action.StartedAt == nil {
		t.Fatalf("start action: action=%+v err=%v", action, err)
	}
	record, err := service.AddEvidence(context.Background(), actor, run.ID, CreateEvidenceInput{
		RoundID: round.ID, ActionID: &action.ID, SourceType: "log", Summary: "database call took 2.4s",
		Content: json.RawMessage(`{"durationMs":2400}`), EvidenceKind: model.EvidenceKindFact,
		SourceSkill: "query_logs", DataSourceID: int64TestPointer(1),
	})
	if err != nil {
		t.Fatalf("add evidence: %v", err)
	}
	completedAction, err := service.CompleteAction(context.Background(), actor, run.ID, action.ID, CompleteActionInput{
		Status: model.RCAActionStatusPartialSuccess, EvidenceIDs: []int64{record.ID},
		ErrorCode: "upstream_timeout", ErrorMessage: "secondary shard timed out password=secret",
	})
	if err != nil {
		t.Fatalf("complete action: %v", err)
	}
	if completedAction.ErrorMessage == nil || strings.Contains(*completedAction.ErrorMessage, "secret") {
		t.Fatalf("external error detail was persisted: %+v", completedAction)
	}
	completedRound, err := service.CompleteRound(context.Background(), actor, run.ID, round.ID, CompleteRoundInput{
		Status: model.RCARoundStatusPartialSuccess, NewEvidenceIDs: []int64{record.ID},
		RejectedHypotheses: []Hypothesis{{
			ID: "h0", Summary: "CPU 饱和", Confidence: 0.1, EvidenceIDs: []int64{record.ID},
		}},
		NextActions: []NextAction{{
			ActionKey: "round2:expand_topology", SkillName: "expand_topology",
			Input: json.RawMessage(`{"nodeKey":"svc:order"}`),
		}},
		ErrorCode: "upstream_timeout",
	})
	if err != nil {
		t.Fatalf("complete round: %v", err)
	}
	if completedRound.Status != model.RCARoundStatusPartialSuccess {
		t.Fatalf("partial round was presented as success: %+v", completedRound)
	}
	run, err = service.GetRun(context.Background(), actor, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Status != model.RCARunStatusPartialSuccess {
		t.Fatalf("partial run was presented as success: %+v", run)
	}
	candidate, err := service.AddRootCauseCandidate(context.Background(), actor, run.ID, CreateCandidateInput{
		RoundID: round.ID, Summary: "订单数据库存在慢调用", Confidence: 0.82, EvidenceIDs: []int64{record.ID},
	})
	if err != nil {
		t.Fatalf("add root cause: %v", err)
	}
	if len(candidate.EvidenceIDs) == 0 {
		t.Fatal("root cause candidate does not reference evidence")
	}
	if _, err := service.CompleteRun(context.Background(), actor, run.ID, CompleteRunInput{Status: model.RCARunStatusSuccess}); err != nil {
		t.Fatalf("complete run: %v", err)
	}
	detail, err := service.GetDetail(context.Background(), actor, run.ID)
	if err != nil {
		t.Fatalf("get detail: %v", err)
	}
	if len(detail.Rounds) != 1 || len(detail.Actions) != 1 || len(detail.Evidence) != 1 || len(detail.Candidates) != 1 {
		t.Fatalf("incomplete RCA detail: %+v", detail)
	}
}

func TestRootCauseCandidateRequiresEvidenceFromSameRun(t *testing.T) {
	repo := newMemoryRCARepository()
	service := NewService(repo, &memoryEvidenceCreator{repository: repo}, fakeRCADataSources{})
	actor := rcaUser(11)
	run, _ := service.CreateRun(context.Background(), actor, CreateRunInput{Query: "slow"})
	round, _ := service.StartRound(context.Background(), actor, run.ID, StartRoundInput{})

	_, err := service.AddRootCauseCandidate(context.Background(), actor, run.ID, CreateCandidateInput{
		RoundID: round.ID, Summary: "unsupported candidate", Confidence: 0.5,
	})
	if !errors.Is(err, ErrEvidenceRequired) {
		t.Fatalf("expected evidence required, got %v", err)
	}
	_, err = service.AddRootCauseCandidate(context.Background(), actor, run.ID, CreateCandidateInput{
		RoundID: round.ID, Summary: "foreign evidence", Confidence: 0.5, EvidenceIDs: []int64{999},
	})
	if !errors.Is(err, ErrEvidenceRequired) {
		t.Fatalf("expected same-run evidence requirement, got %v", err)
	}
}

func TestRCAStartsNextRoundAfterPartialRoundWithinLimit(t *testing.T) {
	repo := newMemoryRCARepository()
	service := NewService(repo, &memoryEvidenceCreator{repository: repo}, fakeRCADataSources{})
	actor := rcaUser(11)
	run, _ := service.CreateRun(context.Background(), actor, CreateRunInput{Query: "slow", MaxRounds: 2})
	first, _ := service.StartRound(context.Background(), actor, run.ID, StartRoundInput{})
	if _, err := service.CompleteRound(context.Background(), actor, run.ID, first.ID, CompleteRoundInput{
		Status: model.RCARoundStatusPartialSuccess,
	}); err != nil {
		t.Fatalf("complete first round: %v", err)
	}
	second, err := service.StartRound(context.Background(), actor, run.ID, StartRoundInput{})
	if err != nil || second.RoundNumber != 2 {
		t.Fatalf("start second round: round=%+v err=%v", second, err)
	}
	if _, err := service.StartRound(context.Background(), actor, run.ID, StartRoundInput{}); !errors.Is(err, ErrRoundLimit) {
		t.Fatalf("expected round limit, got %v", err)
	}
}

func TestRCAEvidenceEnforcesOwnerAndDataSourcePermissions(t *testing.T) {
	repo := newMemoryRCARepository()
	service := NewService(repo, &memoryEvidenceCreator{repository: repo}, fakeRCADataSources{views: accessibleViews(1)})
	owner := rcaUser(11)
	run, _ := service.CreateRun(context.Background(), owner, CreateRunInput{Query: "slow"})
	round, _ := service.StartRound(context.Background(), owner, run.ID, StartRoundInput{})

	_, err := service.AddEvidence(context.Background(), owner, run.ID, CreateEvidenceInput{
		RoundID: round.ID, SourceType: "metric", Summary: "forbidden source",
		Content: json.RawMessage(`{}`), EvidenceKind: model.EvidenceKindFact,
		SourceSkill: "query_metrics", DataSourceID: int64TestPointer(2),
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected forbidden data source, got %v", err)
	}
	record, err := service.AddEvidence(context.Background(), owner, run.ID, CreateEvidenceInput{
		RoundID: round.ID, SourceType: "metric", Summary: "allowed source",
		Content: json.RawMessage(`{}`), EvidenceKind: model.EvidenceKindFact,
		SourceSkill: "query_metrics", DataSourceID: int64TestPointer(1),
	})
	if err != nil {
		t.Fatalf("add allowed evidence: %v", err)
	}
	if record.OwnerUserID == nil || *record.OwnerUserID != owner.ID {
		t.Fatalf("evidence owner not recorded: %+v", record)
	}
	if _, err := service.GetDetail(context.Background(), rcaUser(12), run.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("other user accessed RCA detail: %v", err)
	}
}

func TestRCARecoverySkipsSuccessfulSensitiveReadAndRetriesFailure(t *testing.T) {
	repo := newMemoryRCARepository()
	service := NewService(repo, &memoryEvidenceCreator{repository: repo}, fakeRCADataSources{})
	actor := rcaUser(11)
	run, _ := service.CreateRun(context.Background(), actor, CreateRunInput{Query: "slow"})
	round, _ := service.StartRound(context.Background(), actor, run.ID, StartRoundInput{})
	successful, _ := service.CreateAction(context.Background(), actor, run.ID, CreateActionInput{
		RoundID: round.ID, ActionKey: "logs", SkillName: "query_logs", SensitiveRead: true,
	})
	failed, _ := service.CreateAction(context.Background(), actor, run.ID, CreateActionInput{
		RoundID: round.ID, ActionKey: "metrics", SkillName: "query_metrics", SensitiveRead: true,
	})
	if _, err := service.CompleteAction(context.Background(), actor, run.ID, successful.ID, CompleteActionInput{
		Status: model.RCAActionStatusSuccess,
	}); err != nil {
		t.Fatalf("complete successful action: %v", err)
	}
	if _, err := service.CompleteAction(context.Background(), actor, run.ID, failed.ID, CompleteActionInput{
		Status: model.RCAActionStatusFailed, ErrorCode: "upstream_unavailable",
	}); err != nil {
		t.Fatalf("complete failed action: %v", err)
	}
	if _, err := service.CompleteRun(context.Background(), actor, run.ID, CompleteRunInput{
		Status: model.RCARunStatusFailed, ErrorCode: "upstream_unavailable",
	}); err != nil {
		t.Fatalf("fail run: %v", err)
	}
	plan, err := service.Recover(context.Background(), actor, run.ID)
	if err != nil {
		t.Fatalf("recover run: %v", err)
	}
	if len(plan.SkippedActionIDs) != 1 || plan.SkippedActionIDs[0] != successful.ID ||
		len(plan.RetryableActionIDs) != 1 || plan.RetryableActionIDs[0] != failed.ID {
		t.Fatalf("unexpected recovery plan: %+v", plan)
	}
	storedSuccessful, _ := repo.FindRCAActionByID(context.Background(), successful.ID)
	storedFailed, _ := repo.FindRCAActionByID(context.Background(), failed.ID)
	if storedSuccessful.Status != model.RCAActionStatusSuccess || storedSuccessful.Attempt != 1 {
		t.Fatalf("successful sensitive read was mutated: %+v", storedSuccessful)
	}
	if storedFailed.Status != model.RCAActionStatusPending || storedFailed.Attempt != 2 {
		t.Fatalf("failed action was not prepared for retry: %+v", storedFailed)
	}
	duplicate, err := service.CreateAction(context.Background(), actor, run.ID, CreateActionInput{
		RoundID: round.ID, ActionKey: "logs", SkillName: "query_logs", SensitiveRead: true,
	})
	if err != nil || duplicate.ID != successful.ID || duplicate.Status != model.RCAActionStatusSuccess {
		t.Fatalf("idempotent action key did not preserve success: action=%+v err=%v", duplicate, err)
	}
}

func TestRCACancelAndTimeoutAreTerminal(t *testing.T) {
	repo := newMemoryRCARepository()
	service := NewService(repo, &memoryEvidenceCreator{repository: repo}, fakeRCADataSources{})
	service.now = fixedRCATime
	actor := rcaUser(11)
	cancelled, _ := service.CreateRun(context.Background(), actor, CreateRunInput{Query: "cancel me"})
	cancelled, err := service.Cancel(context.Background(), actor, cancelled.ID)
	if err != nil || cancelled.Status != model.RCARunStatusCancelled || cancelled.CancelRequestedAt == nil {
		t.Fatalf("cancel failed: run=%+v err=%v", cancelled, err)
	}
	timed, _ := service.CreateRun(context.Background(), actor, CreateRunInput{Query: "timeout", TimeoutSeconds: 1})
	service.now = func() time.Time { return fixedRCATime().Add(2 * time.Second) }
	timed, err = service.GetRun(context.Background(), actor, timed.ID)
	if err != nil || timed.Status != model.RCARunStatusTimedOut || timed.ErrorCode == nil || *timed.ErrorCode != "run_timeout" {
		t.Fatalf("timeout failed: run=%+v err=%v", timed, err)
	}
}

type memoryRCARepository struct {
	mu              sync.Mutex
	nextRunID       int64
	nextRoundID     int64
	nextActionID    int64
	nextEvidenceID  int64
	nextCandidateID int64
	runs            map[int64]model.RCARun
	rounds          map[int64]model.RCARound
	actions         map[int64]model.RCAAction
	evidence        map[int64]model.EvidenceRecord
	candidates      map[int64]model.RCARootCauseCandidate
}

func newMemoryRCARepository() *memoryRCARepository {
	return &memoryRCARepository{
		nextRunID: 1, nextRoundID: 1, nextActionID: 1, nextEvidenceID: 1, nextCandidateID: 1,
		runs: map[int64]model.RCARun{}, rounds: map[int64]model.RCARound{},
		actions: map[int64]model.RCAAction{}, evidence: map[int64]model.EvidenceRecord{},
		candidates: map[int64]model.RCARootCauseCandidate{},
	}
}

func (r *memoryRCARepository) CreateRCARun(_ context.Context, run *model.RCARun) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run.ID = r.nextRunID
	r.nextRunID++
	run.CreatedAt = fixedRCATime()
	run.UpdatedAt = run.CreatedAt
	r.runs[run.ID] = *run
	return nil
}

func (r *memoryRCARepository) FindRCARunByID(_ context.Context, id int64) (*model.RCARun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return copyRCARun(run), nil
}

func (r *memoryRCARepository) ListRCARuns(_ context.Context, filters repository.RCARunFilters) ([]model.RCARun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := []model.RCARun{}
	for _, run := range r.runs {
		if filters.UserID > 0 && run.UserID != filters.UserID {
			continue
		}
		if filters.Status != "" && run.Status != filters.Status {
			continue
		}
		result = append(result, run)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID > result[j].ID })
	return result, nil
}

func (r *memoryRCARepository) UpdateRCARun(_ context.Context, id int64, updates repository.RCARunUpdates) (*model.RCARun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	if updates.Status != "" {
		run.Status = updates.Status
	}
	if updates.CurrentRound != nil {
		run.CurrentRound = *updates.CurrentRound
	}
	if updates.CancelRequestedAt != nil {
		run.CancelRequestedAt = updates.CancelRequestedAt
	}
	if updates.ClearError {
		run.ErrorCode, run.ErrorMessage = nil, nil
	} else {
		if updates.ErrorCode != nil {
			run.ErrorCode = updates.ErrorCode
		}
		if updates.ErrorMessage != nil {
			run.ErrorMessage = updates.ErrorMessage
		}
	}
	if updates.ClearStopReason {
		run.StopReason = nil
	} else if updates.StopReason != nil {
		run.StopReason = updates.StopReason
	}
	if updates.StartedAt != nil {
		run.StartedAt = updates.StartedAt
	}
	if updates.ClearFinishedAt {
		run.FinishedAt = nil
	} else if updates.FinishedAt != nil {
		run.FinishedAt = updates.FinishedAt
	}
	r.runs[id] = run
	return copyRCARun(run), nil
}

func (r *memoryRCARepository) MarkTimedOutRCARuns(_ context.Context, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, run := range r.runs {
		if (run.Status == model.RCARunStatusPending || run.Status == model.RCARunStatusRunning) &&
			run.TimeoutAt != nil && !run.TimeoutAt.After(now) {
			code, message := "run_timeout", "RCA run exceeded its configured deadline"
			stopReason := StopReasonWallTime
			run.Status, run.ErrorCode, run.ErrorMessage, run.StopReason, run.FinishedAt = model.RCARunStatusTimedOut, &code, &message, &stopReason, &now
			r.runs[id] = run
			for roundID, round := range r.rounds {
				if round.RunID == id && (round.Status == model.RCARoundStatusPending || round.Status == model.RCARoundStatusRunning) {
					round.Status, round.ErrorCode, round.FinishedAt = model.RCARoundStatusTimedOut, &code, &now
					r.rounds[roundID] = round
				}
			}
			for actionID, action := range r.actions {
				if action.RunID == id && (action.Status == model.RCAActionStatusPending || action.Status == model.RCAActionStatusRunning) {
					action.Status, action.ErrorCode, action.ErrorMessage, action.FinishedAt = model.RCAActionStatusTimedOut, &code, &message, &now
					r.actions[actionID] = action
				}
			}
		}
	}
	return nil
}

func (r *memoryRCARepository) CreateRCARound(_ context.Context, round *model.RCARound) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	round.ID = r.nextRoundID
	r.nextRoundID++
	r.rounds[round.ID] = *round
	return nil
}

func (r *memoryRCARepository) FindRCARoundByID(_ context.Context, id int64) (*model.RCARound, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	round, ok := r.rounds[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &round, nil
}

func (r *memoryRCARepository) ListRCARounds(_ context.Context, runID int64) ([]model.RCARound, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := []model.RCARound{}
	for _, round := range r.rounds {
		if round.RunID == runID {
			result = append(result, round)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].RoundNumber < result[j].RoundNumber })
	return result, nil
}

func (r *memoryRCARepository) UpdateRCARound(_ context.Context, id int64, updates repository.RCARoundUpdates) (*model.RCARound, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	round, ok := r.rounds[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	if updates.Status != "" {
		round.Status = updates.Status
	}
	if updates.NewEvidenceIDs != nil {
		round.NewEvidenceIDs = updates.NewEvidenceIDs
	}
	if updates.RejectedHypotheses != nil {
		round.RejectedHypotheses = updates.RejectedHypotheses
	}
	if updates.NextActions != nil {
		round.NextActions = updates.NextActions
	}
	if updates.ErrorCode != nil {
		round.ErrorCode = updates.ErrorCode
	}
	if updates.ClearError {
		round.ErrorCode = nil
	}
	if updates.StartedAt != nil {
		round.StartedAt = updates.StartedAt
	}
	if updates.ClearFinishedAt {
		round.FinishedAt = nil
	} else if updates.FinishedAt != nil {
		round.FinishedAt = updates.FinishedAt
	}
	r.rounds[id] = round
	return &round, nil
}

func (r *memoryRCARepository) CreateOrGetRCAAction(_ context.Context, action *model.RCAAction) (*model.RCAAction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, stored := range r.actions {
		if stored.RunID == action.RunID && stored.ActionKey == action.ActionKey {
			copy := stored
			return &copy, nil
		}
	}
	action.ID = r.nextActionID
	r.nextActionID++
	r.actions[action.ID] = *action
	copy := *action
	return &copy, nil
}

func (r *memoryRCARepository) FindRCAActionByID(_ context.Context, id int64) (*model.RCAAction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	action, ok := r.actions[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &action, nil
}

func (r *memoryRCARepository) ListRCAActions(_ context.Context, runID int64) ([]model.RCAAction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := []model.RCAAction{}
	for _, action := range r.actions {
		if action.RunID == runID {
			result = append(result, action)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (r *memoryRCARepository) UpdateRCAAction(_ context.Context, id int64, updates repository.RCAActionUpdates) (*model.RCAAction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	action, ok := r.actions[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	if updates.Status != "" {
		action.Status = updates.Status
	}
	if updates.Output != nil {
		action.Output = updates.Output
	}
	if updates.EvidenceIDs != nil {
		action.EvidenceIDs = updates.EvidenceIDs
	}
	if updates.ClearError {
		action.ErrorCode, action.ErrorMessage = nil, nil
	} else {
		if updates.ErrorCode != nil {
			action.ErrorCode = updates.ErrorCode
		}
		if updates.ErrorMessage != nil {
			action.ErrorMessage = updates.ErrorMessage
		}
	}
	if updates.Attempt != nil {
		action.Attempt = *updates.Attempt
	}
	if updates.StartedAt != nil {
		action.StartedAt = updates.StartedAt
	}
	if updates.ClearFinishedAt {
		action.FinishedAt = nil
	} else if updates.FinishedAt != nil {
		action.FinishedAt = updates.FinishedAt
	}
	r.actions[id] = action
	return &action, nil
}

func (r *memoryRCARepository) CreateRCARootCauseCandidate(_ context.Context, candidate *model.RCARootCauseCandidate) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	candidate.ID = r.nextCandidateID
	r.nextCandidateID++
	r.candidates[candidate.ID] = *candidate
	return nil
}

func (r *memoryRCARepository) ListRCARootCauseCandidates(_ context.Context, runID int64) ([]model.RCARootCauseCandidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := []model.RCARootCauseCandidate{}
	for _, candidate := range r.candidates {
		if candidate.RunID == runID {
			result = append(result, candidate)
		}
	}
	return result, nil
}

func (r *memoryRCARepository) EvidenceIDsBelongToRCARun(_ context.Context, runID int64, evidenceIDs []int64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range uniqueIDs(evidenceIDs) {
		record, ok := r.evidence[id]
		if !ok || record.RCARunID == nil || *record.RCARunID != runID {
			return false, nil
		}
	}
	return len(uniqueIDs(evidenceIDs)) > 0, nil
}

func (r *memoryRCARepository) ListRCAEvidence(_ context.Context, runID int64) ([]model.EvidenceRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := []model.EvidenceRecord{}
	for _, record := range r.evidence {
		if record.RCARunID != nil && *record.RCARunID == runID {
			result = append(result, record)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

type memoryEvidenceCreator struct {
	repository *memoryRCARepository
}

func (c *memoryEvidenceCreator) Create(_ context.Context, input evidencesvc.CreateInput) (*model.EvidenceRecord, error) {
	c.repository.mu.Lock()
	defer c.repository.mu.Unlock()
	record := model.EvidenceRecord{
		ID: c.repository.nextEvidenceID, EvidenceKey: input.EvidenceKey, SourceType: input.SourceType,
		SourceRef: input.SourceRef, ObservedAt: input.ObservedAt, Title: input.Title, Summary: input.Summary,
		Content: input.Content, Confidence: input.Confidence, RCARunID: input.RCARunID, RCARoundID: input.RCARoundID,
		RCAActionID: input.RCAActionID, Entity: input.Entity, WindowStart: input.WindowStart, WindowEnd: input.WindowEnd,
		DataSourceID: input.DataSourceID, OwnerUserID: input.OwnerUserID,
	}
	kind, skill, sensitivity := input.EvidenceKind, input.SourceSkill, input.Sensitivity
	record.EvidenceKind, record.SourceSkill, record.Sensitivity = &kind, &skill, &sensitivity
	if record.EvidenceKey == "" {
		record.EvidenceKey = "ev_test_" + strconv.FormatInt(record.ID, 10)
	}
	c.repository.nextEvidenceID++
	c.repository.evidence[record.ID] = record
	return &record, nil
}

type fakeRCADataSources struct {
	views []datasourcesvc.DataSourceView
}

func (f fakeRCADataSources) List(context.Context, *model.AppUser) ([]datasourcesvc.DataSourceView, error) {
	return f.views, nil
}

func accessibleViews(ids ...int64) []datasourcesvc.DataSourceView {
	prod := "prod"
	result := []datasourcesvc.DataSourceView{}
	for _, id := range ids {
		result = append(result, datasourcesvc.DataSourceView{
			ID: id, Name: "source", SourceType: model.DataSourceTypeElasticsearch,
			Environment: &prod, Enabled: true, ReadOnly: true,
		})
	}
	return result
}

func rcaUser(id int64) *model.AppUser {
	return &model.AppUser{ID: id, Username: "user", Role: model.RoleUser, Enabled: true}
}

func fixedRCATime() time.Time {
	return time.Date(2026, 7, 28, 6, 0, 0, 0, time.UTC)
}

func int64TestPointer(value int64) *int64 { return &value }

func copyRCARun(run model.RCARun) *model.RCARun {
	copy := run
	return &copy
}
