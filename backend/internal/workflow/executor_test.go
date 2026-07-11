package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"aiops-platform/backend/internal/agentruntime"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
	"aiops-platform/backend/internal/skillframework"
)

func TestExecutorPersistsReadableState(t *testing.T) {
	repo := newExecutorMemoryRepo(validExecutorDefinition())
	executor := NewExecutor(repo, fakeWorkflowAgents{}, fakeWorkflowSkills{}, time.Second)

	run, err := executor.Run(context.Background(), ExecutorInput{
		Actor:      adminWorkflowActor(),
		WorkflowID: 1,
		Input:      json.RawMessage(`{"message":"hello"}`),
	})
	if err != nil {
		t.Fatalf("run workflow: %v", err)
	}
	if run.Status != model.WorkflowRunStatusSuccess {
		t.Fatalf("status = %s, want success", run.Status)
	}
	reloaded, err := repo.FindWorkflowRunByID(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("reload run: %v", err)
	}
	if reloaded.Status != model.WorkflowRunStatusSuccess || len(reloaded.NodeRuns) != 4 {
		t.Fatalf("unexpected persisted run: %+v", reloaded)
	}
}

func TestExecutorNodeFailureProducesPartialSuccess(t *testing.T) {
	definition := validExecutorDefinition()
	definition.Nodes[2].SkillName = "failing_skill"
	repo := newExecutorMemoryRepo(definition)
	executor := NewExecutor(repo, fakeWorkflowAgents{}, fakeWorkflowSkills{fail: true}, time.Second)

	run, err := executor.Run(context.Background(), ExecutorInput{
		Actor:      adminWorkflowActor(),
		WorkflowID: 1,
		Input:      json.RawMessage(`{"message":"hello"}`),
	})
	if err == nil {
		t.Fatalf("expected node failure")
	}
	if run == nil || run.Status != model.WorkflowRunStatusPartialSuccess {
		t.Fatalf("expected partial_success run, got run=%+v err=%v", run, err)
	}
	reloaded, err := repo.FindWorkflowRunByID(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("reload run: %v", err)
	}
	if reloaded.Status != model.WorkflowRunStatusPartialSuccess || !hasFailedNode(reloaded.NodeRuns) {
		t.Fatalf("expected persisted failed node, got %+v", reloaded)
	}
}

func validExecutorDefinition() Definition {
	return Definition{
		Name:    "test_workflow",
		Version: "v1",
		Nodes: []Node{
			{ID: "start", Type: NodeTypeStart},
			{ID: "agent", Type: NodeTypeAgent, AgentName: "echo_agent", Config: json.RawMessage(`{"context":{"query":"hello"}}`)},
			{ID: "skill", Type: NodeTypeSkill, SkillName: "echo_safe", Config: json.RawMessage(`{"input":{"message":"hello"}}`)},
			{ID: "end", Type: NodeTypeEnd},
		},
		Edges: []Edge{
			{From: "start", To: "agent"},
			{From: "agent", To: "skill"},
			{From: "skill", To: "end"},
		},
	}
}

func hasFailedNode(nodes []model.WorkflowNodeRun) bool {
	for _, node := range nodes {
		if node.Status == model.WorkflowRunStatusFailed {
			return true
		}
	}
	return false
}

type executorMemoryRepo struct {
	definition model.WorkflowDefinition
	nextRunID  int64
	nextNodeID int64
	runs       []model.WorkflowRun
	nodes      []model.WorkflowNodeRun
}

func newExecutorMemoryRepo(definition Definition) *executorMemoryRepo {
	raw, _ := json.Marshal(definition)
	return &executorMemoryRepo{
		definition: model.WorkflowDefinition{ID: 1, Name: definition.Name, Version: definition.Version, Definition: raw, Enabled: true},
		nextRunID:  1,
		nextNodeID: 1,
	}
}

func (r *executorMemoryRepo) FindWorkflowDefinitionByID(_ context.Context, id int64) (*model.WorkflowDefinition, error) {
	if id != r.definition.ID {
		return nil, repository.ErrNotFound
	}
	return &r.definition, nil
}

func (r *executorMemoryRepo) CreateWorkflowRun(_ context.Context, run *model.WorkflowRun) error {
	run.ID = r.nextRunID
	r.nextRunID++
	copied := *run
	r.runs = append(r.runs, copied)
	return nil
}

func (r *executorMemoryRepo) UpdateWorkflowRun(_ context.Context, id int64, updates repository.WorkflowRunUpdates) (*model.WorkflowRun, error) {
	for index := range r.runs {
		if r.runs[index].ID != id {
			continue
		}
		if updates.Status != "" {
			r.runs[index].Status = updates.Status
		}
		r.runs[index].Output = updates.Output
		r.runs[index].ErrorMessage = updates.ErrorMessage
		r.runs[index].FinishedAt = updates.FinishedAt
		return r.FindWorkflowRunByID(context.Background(), id)
	}
	return nil, repository.ErrNotFound
}

func (r *executorMemoryRepo) FindWorkflowRunByID(_ context.Context, id int64) (*model.WorkflowRun, error) {
	for _, run := range r.runs {
		if run.ID != id {
			continue
		}
		copied := run
		for _, node := range r.nodes {
			if node.WorkflowRunID == id {
				copied.NodeRuns = append(copied.NodeRuns, node)
			}
		}
		return &copied, nil
	}
	return nil, repository.ErrNotFound
}

func (r *executorMemoryRepo) CreateWorkflowNodeRun(_ context.Context, run *model.WorkflowNodeRun) error {
	run.ID = r.nextNodeID
	r.nextNodeID++
	copied := *run
	r.nodes = append(r.nodes, copied)
	return nil
}

func (r *executorMemoryRepo) UpdateWorkflowNodeRun(_ context.Context, id int64, updates repository.WorkflowNodeRunUpdates) (*model.WorkflowNodeRun, error) {
	for index := range r.nodes {
		if r.nodes[index].ID != id {
			continue
		}
		if updates.Status != "" {
			r.nodes[index].Status = updates.Status
		}
		r.nodes[index].Output = updates.Output
		r.nodes[index].ErrorMessage = updates.ErrorMessage
		r.nodes[index].FinishedAt = updates.FinishedAt
		return &r.nodes[index], nil
	}
	return nil, repository.ErrNotFound
}

type fakeWorkflowAgents struct{}

func (fakeWorkflowAgents) Get(name string) (agentruntime.AgentDefinition, error) {
	if name == "echo_agent" {
		return agentruntime.AgentDefinition{Name: name, Enabled: true}, nil
	}
	return agentruntime.AgentDefinition{}, errors.New("agent not found")
}

func (fakeWorkflowAgents) Run(context.Context, agentruntime.RunInput) (*agentruntime.RunOutput, error) {
	return &agentruntime.RunOutput{Result: &agentruntime.AgentResult{Summary: "ok", Confidence: 1}}, nil
}

type fakeWorkflowSkills struct {
	fail bool
}

func (s fakeWorkflowSkills) Get(name string) (skillframework.SkillDefinition, error) {
	if name == "echo_safe" || name == "failing_skill" {
		return skillframework.SkillDefinition{Name: name, Enabled: true}, nil
	}
	return skillframework.SkillDefinition{}, errors.New("skill not found")
}

func (s fakeWorkflowSkills) Execute(_ context.Context, input skillframework.ExecuteInput) (*skillframework.ExecuteResult, error) {
	if s.fail || input.Name == "failing_skill" {
		return nil, errors.New("skill failed")
	}
	return &skillframework.ExecuteResult{SkillName: input.Name, Output: json.RawMessage(`{"ok":true}`)}, nil
}

func adminWorkflowActor() *model.AppUser {
	return &model.AppUser{ID: 1, Username: "admin", Role: model.RoleAdmin, Enabled: true}
}
