package rca

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	datasourcesvc "aiops-platform/backend/internal/datasource"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/skillframework"
)

func TestOrchestratorCompletesThreeEvidenceDrivenRounds(t *testing.T) {
	repo := newMemoryRCARepository()
	executor := newRoundOneFakeExecutor()
	views := append(roundOneViews(), plannerDataSource(7, model.DataSourceTypeTiDB))
	service := NewService(repo, &memoryEvidenceCreator{repository: repo}, fakeRCADataSources{views: views}).
		WithSkillExecutor(executor).
		WithPlanner(plannerTestCatalog(), nil)
	service.now = fixedRCATime
	actor := &model.AppUser{ID: 1, Username: "admin", Role: model.RoleAdmin, Enabled: true}
	run, err := service.CreateRun(context.Background(), actor, CreateRunInput{
		Query: "订单服务变慢，请查询可能原因",
		Scope: json.RawMessage(`{
			"serviceName":"order-service","environment":"prod","topologyNodeKey":"svc:order",
			"from":"2026-07-28T05:30:00Z","to":"2026-07-28T06:00:00Z",
			"logDataSourceId":1,"metricsDataSourceId":2,"tidbDataSourceId":7
		}`),
		MaxRounds: 3,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	result, err := service.Orchestrate(context.Background(), actor, run.ID, OrchestrateInput{
		UseLLM: boolPointer(false),
		Budget: OrchestratorBudget{
			MaxRounds: 3, MaxSkillCallsPerRound: 12, MaxSkillCalls: 24,
			MaxConcurrentSkills: 2, MaxTokens: 100000, MaxContextBytes: 1 << 20,
			MaxWallTimeSeconds: 30, ConfidenceThreshold: 1,
		},
	})
	if err != nil {
		t.Fatalf("orchestrate: %v", err)
	}
	if len(result.Rounds) != 3 || result.Usage.RoundsCompleted != 3 {
		t.Fatalf("orchestrator did not complete three rounds: %+v", result)
	}
	if result.StopReason != StopReasonMaxRounds || result.Run.StopReason == nil || *result.Run.StopReason != StopReasonMaxRounds {
		t.Fatalf("stop reason was not persisted: %+v", result.Run)
	}
	if executor.maxActive > 2 {
		t.Fatalf("orchestrator exceeded concurrency budget: maxActive=%d", executor.maxActive)
	}
	actions, err := service.ListActions(context.Background(), actor, run.ID)
	if err != nil {
		t.Fatalf("list actions: %v", err)
	}
	foundSlowSQL, foundDeepening := false, false
	for _, action := range actions {
		if action.SkillName == "query_tidb_slow_queries" {
			foundSlowSQL = strings.Contains(string(action.Input), `"dataSourceId":7`)
		}
		if action.SkillName == "query_tidb_processlist" {
			foundDeepening = strings.Contains(string(action.Input), `"dataSourceId":7`)
		}
		if strings.Contains(string(action.Input), `"dataSourceId":1`) && strings.Contains(action.SkillName, "tidb") {
			t.Fatalf("component action used a hard-coded example data source: %+v", action)
		}
	}
	if !foundSlowSQL || !foundDeepening {
		t.Fatalf("later rounds were not derived from evidence and resolved scope: %+v", actions)
	}
}

func TestOrchestratorHonorsGlobalSkillBudgetInRoundOne(t *testing.T) {
	repo := newMemoryRCARepository()
	executor := newRoundOneFakeExecutor()
	service := NewService(repo, &memoryEvidenceCreator{repository: repo}, fakeRCADataSources{views: roundOneViews()}).
		WithSkillExecutor(executor).
		WithPlanner(plannerTestCatalog(), nil)
	service.now = fixedRCATime
	actor := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	run, _ := service.CreateRun(context.Background(), actor, CreateRunInput{
		Query: "订单服务变慢", Scope: json.RawMessage(`{
			"serviceName":"order-service","from":"2026-07-28T05:30:00Z","to":"2026-07-28T06:00:00Z",
			"logDataSourceId":1,"metricsDataSourceId":2
		}`),
	})
	result, err := service.Orchestrate(context.Background(), actor, run.ID, OrchestrateInput{
		UseLLM: boolPointer(false),
		Budget: OrchestratorBudget{
			MaxRounds: 3, MaxSkillCallsPerRound: 2, MaxSkillCalls: 2, MaxConcurrentSkills: 1,
			MaxTokens: 10000, MaxContextBytes: 1 << 20, MaxWallTimeSeconds: 30, ConfidenceThreshold: 1,
		},
	})
	if err != nil {
		t.Fatalf("orchestrate with bounded budget: %v", err)
	}
	if result.Usage.SkillCalls != 2 || result.StopReason != StopReasonSkillBudget || executor.maxActive > 1 {
		t.Fatalf("skill/concurrency budget was not enforced: %+v maxActive=%d", result, executor.maxActive)
	}
}

func TestPlannerActionEvidenceIsIdempotent(t *testing.T) {
	service, actor, runID, roundID := plannerTestRun(t, json.RawMessage(`{"serviceName":"order","tidbDataSourceId":7}`), []datasourcesvc.DataSourceView{
		plannerDataSource(7, model.DataSourceTypeTiDB),
	})
	action, err := service.CreateAction(context.Background(), actor, runID, CreateActionInput{
		RoundID: roundID, ActionKey: "same", SkillName: "query_tidb_slow_queries",
		Input: json.RawMessage(`{"dataSourceId":7}`), SensitiveRead: true,
	})
	if err != nil {
		t.Fatalf("create action: %v", err)
	}
	execution := &plannedExecution{
		plan:   PlannerAction{ActionKey: "same", SkillName: "query_tidb_slow_queries", Input: action.Input},
		action: action,
		result: &skillframework.ExecuteResult{RunID: 9, Output: json.RawMessage(`{"summary":"slow sql found"}`)},
	}
	round, _ := service.repository.FindRCARoundByID(context.Background(), roundID)
	first, err := service.addPlannerActionEvidence(context.Background(), actor, runID, round, execution)
	if err != nil {
		t.Fatalf("add first evidence: %v", err)
	}
	second, err := service.addPlannerActionEvidence(context.Background(), actor, runID, round, execution)
	if err != nil || second.ID != first.ID {
		t.Fatalf("idempotent evidence write failed: first=%+v second=%+v err=%v", first, second, err)
	}
	if len(service.repository.(*memoryRCARepository).evidence) != 1 {
		t.Fatal("recovery duplicated Evidence")
	}
}

func TestOrchestratorCancellationPersistsUserStopReason(t *testing.T) {
	repo := newMemoryRCARepository()
	executor := &blockingRCAExecutor{started: make(chan struct{})}
	service := NewService(repo, &memoryEvidenceCreator{repository: repo}, fakeRCADataSources{views: roundOneViews()}).
		WithSkillExecutor(executor).
		WithPlanner(plannerTestCatalog(), nil)
	service.now = fixedRCATime
	actor := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	run, _ := service.CreateRun(context.Background(), actor, CreateRunInput{
		Query: "订单服务变慢", Scope: json.RawMessage(`{
			"serviceName":"order-service","from":"2026-07-28T05:30:00Z","to":"2026-07-28T06:00:00Z",
			"logDataSourceId":1,"metricsDataSourceId":2
		}`),
	})
	type answer struct {
		result *OrchestratorResult
		err    error
	}
	done := make(chan answer, 1)
	go func() {
		result, err := service.Orchestrate(context.Background(), actor, run.ID, OrchestrateInput{
			UseLLM: boolPointer(false),
			Budget: OrchestratorBudget{
				MaxRounds: 3, MaxSkillCallsPerRound: 12, MaxSkillCalls: 24, MaxConcurrentSkills: 2,
				MaxTokens: 10000, MaxContextBytes: 1 << 20, MaxWallTimeSeconds: 30, ConfidenceThreshold: 1,
			},
		})
		done <- answer{result: result, err: err}
	}()
	<-executor.started
	cancelled, err := service.Cancel(context.Background(), actor, run.ID)
	if err != nil || cancelled.StopReason == nil || *cancelled.StopReason != StopReasonUserCancelled {
		t.Fatalf("cancel run: run=%+v err=%v", cancelled, err)
	}
	finished := <-done
	if finished.err != nil || finished.result == nil || finished.result.StopReason != StopReasonUserCancelled {
		t.Fatalf("orchestrator did not stop cleanly after cancellation: %+v err=%v", finished.result, finished.err)
	}
}

type blockingRCAExecutor struct {
	once    sync.Once
	started chan struct{}
}

func (b *blockingRCAExecutor) Execute(ctx context.Context, input skillframework.ExecuteInput) (*skillframework.ExecuteResult, error) {
	b.once.Do(func() { close(b.started) })
	<-ctx.Done()
	return nil, ctx.Err()
}
