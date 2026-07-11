package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
	"aiops-platform/backend/internal/skillframework"
	"aiops-platform/backend/internal/toolregistry"
)

func TestRunStopsInfiniteStepLoop(t *testing.T) {
	audit := newAgentMemoryAudit()
	runtime := newTestRuntime(t, audit, Limits{MaxSteps: 3}, stepLoopAgent{})

	_, err := runtime.Run(context.Background(), RunInput{
		Actor:   adminActor(),
		Name:    "step_loop",
		Context: AgentContext{UserID: 1, Query: "loop"},
	})
	if !errors.Is(err, ErrStepLimitExceeded) {
		t.Fatalf("expected ErrStepLimitExceeded, got %v", err)
	}
	if len(audit.runs) != 1 || audit.runs[0].Status != model.AgentRunStatusFailed || audit.runs[0].FinishedAt == nil {
		t.Fatalf("unexpected audit rows: %+v", audit.runs)
	}
}

func TestRunStopsExcessiveSkillCalls(t *testing.T) {
	runtime := newTestRuntime(t, newAgentMemoryAudit(), Limits{MaxSkillCalls: 1}, skillLoopAgent{})

	_, err := runtime.Run(context.Background(), RunInput{
		Actor:   adminActor(),
		Name:    "skill_loop",
		Context: AgentContext{UserID: 1, Query: "skills"},
	})
	if !errors.Is(err, ErrSkillLimitExceeded) {
		t.Fatalf("expected ErrSkillLimitExceeded, got %v", err)
	}
}

func TestRunAppliesTimeout(t *testing.T) {
	runtime := newTestRuntime(t, newAgentMemoryAudit(), Limits{Timeout: 5 * time.Millisecond}, timeoutAgent{})

	_, err := runtime.Run(context.Background(), RunInput{
		Actor:   adminActor(),
		Name:    "timeout_agent",
		Context: AgentContext{UserID: 1, Query: "timeout"},
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
}

func TestRunContextDoesNotExposeToolRegistry(t *testing.T) {
	runContextType := reflect.TypeOf(&RunContext{})
	exportedMethods := map[string]bool{}
	for index := 0; index < runContextType.NumMethod(); index++ {
		method := runContextType.Method(index)
		exportedMethods[method.Name] = true
	}

	if exportedMethods["ToolRegistry"] || exportedMethods["Tools"] || exportedMethods["GetToolRegistry"] {
		t.Fatalf("RunContext exposes tool registry accessors: %+v", exportedMethods)
	}
	if !exportedMethods["Step"] || !exportedMethods["ExecuteSkill"] || len(exportedMethods) != 2 {
		t.Fatalf("RunContext should only expose Step and ExecuteSkill, got %+v", exportedMethods)
	}
}

func TestRunAuditsSuccessfulResult(t *testing.T) {
	audit := newAgentMemoryAudit()
	runtime := newTestRuntime(t, audit, Limits{}, EchoAgent{})

	output, err := runtime.Run(context.Background(), RunInput{
		Actor:   adminActor(),
		Name:    "echo_agent",
		Context: AgentContext{UserID: 1, Query: "hello"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if output.RunID != 1 || output.Steps != 1 || output.Result.Summary != "hello" {
		t.Fatalf("unexpected output: %+v", output)
	}
	if len(audit.runs) != 1 || audit.runs[0].Status != model.AgentRunStatusSuccess || audit.runs[0].Output == nil {
		t.Fatalf("unexpected audit rows: %+v", audit.runs)
	}
}

func newTestRuntime(t *testing.T, audit AuditRepository, limits Limits, agents ...Agent) *Runtime {
	t.Helper()
	skillRegistry, err := skillframework.NewRegistry(toolregistry.NewBuiltinRegistry(), newSkillMemoryAudit(), agentTestSkill{})
	if err != nil {
		t.Fatalf("skill registry: %v", err)
	}
	runtime, err := NewRuntime(skillRegistry, audit, limits, agents...)
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	return runtime
}

type stepLoopAgent struct{}

func (stepLoopAgent) Name() string {
	return "step_loop"
}

func (stepLoopAgent) Description() string {
	return "test agent that exceeds step limit"
}

func (stepLoopAgent) Analyze(_ context.Context, _ AgentContext, runtime *RunContext) (*AgentResult, error) {
	for {
		if err := runtime.Step("loop"); err != nil {
			return nil, err
		}
	}
}

type skillLoopAgent struct{}

func (skillLoopAgent) Name() string {
	return "skill_loop"
}

func (skillLoopAgent) Description() string {
	return "test agent that exceeds skill call limit"
}

func (skillLoopAgent) Analyze(ctx context.Context, _ AgentContext, runtime *RunContext) (*AgentResult, error) {
	payload := json.RawMessage(`{"message":"hello"}`)
	if _, err := runtime.ExecuteSkill(ctx, "agent_test_skill", payload); err != nil {
		return nil, err
	}
	_, err := runtime.ExecuteSkill(ctx, "agent_test_skill", payload)
	return nil, err
}

type timeoutAgent struct{}

func (timeoutAgent) Name() string {
	return "timeout_agent"
}

func (timeoutAgent) Description() string {
	return "test agent that waits for timeout"
}

func (timeoutAgent) Analyze(ctx context.Context, _ AgentContext, _ *RunContext) (*AgentResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type agentTestSkill struct{}

func (agentTestSkill) Definition() skillframework.SkillDefinition {
	return skillframework.SkillDefinition{
		Name:          "agent_test_skill",
		Version:       "v1",
		Description:   "test skill",
		InputSchema:   json.RawMessage(`{"type":"object","required":["message"],"properties":{"message":{"type":"string"}}}`),
		OutputSchema:  json.RawMessage(`{"type":"object"}`),
		RiskLevel:     model.SkillRiskSafeRead,
		ReadOnly:      true,
		TimeoutSecond: 5,
	}
}

func (agentTestSkill) Execute(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	return input, nil
}

func adminActor() *model.AppUser {
	return &model.AppUser{ID: 1, Username: "admin", Role: model.RoleAdmin, Enabled: true}
}

type agentMemoryAudit struct {
	nextID int64
	runs   []model.AgentRun
}

func newAgentMemoryAudit() *agentMemoryAudit {
	return &agentMemoryAudit{nextID: 1}
}

func (a *agentMemoryAudit) CreateAgentRun(_ context.Context, run *model.AgentRun) error {
	run.ID = a.nextID
	a.nextID++
	copied := *run
	a.runs = append(a.runs, copied)
	return nil
}

func (a *agentMemoryAudit) UpdateAgentRun(_ context.Context, id int64, updates repository.AgentRunUpdates) (*model.AgentRun, error) {
	for index := range a.runs {
		if a.runs[index].ID != id {
			continue
		}
		a.runs[index].Status = updates.Status
		a.runs[index].Output = updates.Output
		a.runs[index].ErrorMessage = updates.ErrorMessage
		a.runs[index].FinishedAt = updates.FinishedAt
		return &a.runs[index], nil
	}
	return nil, repository.ErrNotFound
}

func (a *agentMemoryAudit) ListAgentRuns(_ context.Context, _ int) ([]model.AgentRun, error) {
	copied := make([]model.AgentRun, len(a.runs))
	copy(copied, a.runs)
	return copied, nil
}

func (a *agentMemoryAudit) FindAgentRunByID(_ context.Context, id int64) (*model.AgentRun, error) {
	for index := range a.runs {
		if a.runs[index].ID == id {
			return &a.runs[index], nil
		}
	}
	return nil, repository.ErrNotFound
}

type skillMemoryAudit struct {
	nextID int64
}

func newSkillMemoryAudit() *skillMemoryAudit {
	return &skillMemoryAudit{nextID: 1}
}

func (a *skillMemoryAudit) CreateSkillRun(_ context.Context, run *model.SkillRun) error {
	run.ID = a.nextID
	a.nextID++
	return nil
}

func (a *skillMemoryAudit) UpdateSkillRun(_ context.Context, _ int64, _ repository.SkillRunUpdates) (*model.SkillRun, error) {
	return &model.SkillRun{}, nil
}

func (a *skillMemoryAudit) ListSkillRuns(context.Context, int) ([]model.SkillRun, error) {
	return nil, nil
}
