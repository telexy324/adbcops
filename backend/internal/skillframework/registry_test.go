package skillframework

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
	"aiops-platform/backend/internal/toolregistry"
)

func TestExecuteRejectsInvalidInput(t *testing.T) {
	registry := newTestRegistry(t, testSkill{name: "safe_skill", risk: model.SkillRiskSafeRead})

	_, err := registry.Execute(context.Background(), ExecuteInput{
		Actor:   userActor(),
		Name:    "safe_skill",
		Payload: json.RawMessage(`{"wrong":"value"}`),
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestExecuteRejectsDisabledSkill(t *testing.T) {
	registry := newTestRegistry(t, testSkill{name: "safe_skill", risk: model.SkillRiskSafeRead})
	if _, err := registry.Disable("safe_skill"); err != nil {
		t.Fatalf("disable: %v", err)
	}

	_, err := registry.Execute(context.Background(), ExecuteInput{
		Actor:   userActor(),
		Name:    "safe_skill",
		Payload: json.RawMessage(`{"message":"hello"}`),
	})
	if !errors.Is(err, ErrSkillDisabled) {
		t.Fatalf("expected ErrSkillDisabled, got %v", err)
	}
}

func TestSensitiveReadRequiresAdmin(t *testing.T) {
	registry := newTestRegistry(t, testSkill{name: "sensitive_skill", risk: model.SkillRiskSensitiveRead})

	_, err := registry.Execute(context.Background(), ExecuteInput{
		Actor:   userActor(),
		Name:    "sensitive_skill",
		Payload: json.RawMessage(`{"message":"hello"}`),
	})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied, got %v", err)
	}

	result, err := registry.Execute(context.Background(), ExecuteInput{
		Actor:   adminActor(),
		Name:    "sensitive_skill",
		Payload: json.RawMessage(`{"message":"hello"}`),
	})
	if err != nil {
		t.Fatalf("admin execute: %v", err)
	}
	if result.SkillName != "sensitive_skill" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestDisabledRequiredToolBlocksExecution(t *testing.T) {
	tools := toolregistry.NewBuiltinRegistry()
	if _, err := tools.Disable("prometheus"); err != nil {
		t.Fatalf("disable tool: %v", err)
	}
	audit := newMemoryAudit()
	registry, err := NewRegistry(tools, audit, testSkill{name: "metrics_skill", risk: model.SkillRiskSafeRead, requiredTools: []string{"prometheus"}})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}

	_, err = registry.Execute(context.Background(), ExecuteInput{
		Actor:   userActor(),
		Name:    "metrics_skill",
		Payload: json.RawMessage(`{"message":"hello"}`),
	})
	if !errors.Is(err, ErrToolUnavailable) {
		t.Fatalf("expected ErrToolUnavailable, got %v", err)
	}
}

func TestExecuteAuditsSkillRun(t *testing.T) {
	audit := newMemoryAudit()
	registry, err := NewRegistry(toolregistry.NewBuiltinRegistry(), audit, testSkill{name: "safe_skill", risk: model.SkillRiskSafeRead})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}

	result, err := registry.Execute(context.Background(), ExecuteInput{
		Actor:   userActor(),
		Name:    "safe_skill",
		Payload: json.RawMessage(`{"message":"hello"}`),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.RunID != 1 {
		t.Fatalf("run id = %d, want 1", result.RunID)
	}
	runs, err := registry.ListRuns(context.Background(), 10)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].Status != model.SkillRunStatusSuccess || runs[0].FinishedAt == nil {
		t.Fatalf("unexpected runs: %+v", runs)
	}
}

func newTestRegistry(t *testing.T, skills ...Skill) *Registry {
	t.Helper()
	registry, err := NewRegistry(toolregistry.NewBuiltinRegistry(), newMemoryAudit(), skills...)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	return registry
}

type testSkill struct {
	name          string
	risk          string
	requiredTools []string
}

func (s testSkill) Definition() SkillDefinition {
	return SkillDefinition{
		Name:          s.name,
		Version:       "v1",
		Description:   "test skill",
		InputSchema:   json.RawMessage(`{"type":"object","required":["message"],"properties":{"message":{"type":"string"}}}`),
		OutputSchema:  json.RawMessage(`{"type":"object"}`),
		RiskLevel:     s.risk,
		ReadOnly:      true,
		TimeoutSecond: 5,
		RequiredTools: s.requiredTools,
	}
}

func (s testSkill) Execute(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	return input, nil
}

func userActor() *model.AppUser {
	return &model.AppUser{ID: 10, Username: "operator", Role: model.RoleUser, Enabled: true}
}

func adminActor() *model.AppUser {
	return &model.AppUser{ID: 1, Username: "admin", Role: model.RoleAdmin, Enabled: true}
}

type memoryAudit struct {
	nextID int64
	runs   []model.SkillRun
}

func newMemoryAudit() *memoryAudit {
	return &memoryAudit{nextID: 1}
}

func (a *memoryAudit) CreateSkillRun(_ context.Context, run *model.SkillRun) error {
	run.ID = a.nextID
	a.nextID++
	copied := *run
	a.runs = append(a.runs, copied)
	return nil
}

func (a *memoryAudit) UpdateSkillRun(_ context.Context, id int64, updates repository.SkillRunUpdates) (*model.SkillRun, error) {
	for index := range a.runs {
		if a.runs[index].ID != id {
			continue
		}
		a.runs[index].Status = updates.Status
		a.runs[index].OutputSummary = updates.OutputSummary
		a.runs[index].ErrorMessage = updates.ErrorMessage
		a.runs[index].FinishedAt = updates.FinishedAt
		return &a.runs[index], nil
	}
	return nil, errors.New("not found")
}

func (a *memoryAudit) ListSkillRuns(context.Context, int) ([]model.SkillRun, error) {
	copied := make([]model.SkillRun, len(a.runs))
	copy(copied, a.runs)
	return copied, nil
}

var _ = time.Second
