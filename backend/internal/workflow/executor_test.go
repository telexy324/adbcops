package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
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

func TestExecutorPartialSkillContinuesAndPersistsPartialSuccess(t *testing.T) {
	definition := validExecutorDefinition()
	repo := newExecutorMemoryRepo(definition)
	executor := NewExecutor(repo, fakeWorkflowAgents{}, fakeWorkflowSkills{partial: true}, time.Second)

	run, err := executor.Run(context.Background(), ExecutorInput{
		Actor: adminWorkflowActor(), WorkflowID: 1, Input: json.RawMessage(`{"hostId":1}`),
	})
	if !errors.Is(err, ErrWorkflowPartial) || run == nil || run.Status != model.WorkflowRunStatusPartialSuccess {
		t.Fatalf("partial workflow result = %+v, error = %v", run, err)
	}
	if len(run.NodeRuns) != len(definition.Nodes) {
		t.Fatalf("partial result stopped downstream nodes: got %d want %d", len(run.NodeRuns), len(definition.Nodes))
	}
	partialNodes := 0
	for _, node := range run.NodeRuns {
		if node.Status == model.WorkflowRunStatusPartialSuccess {
			partialNodes++
		}
	}
	if partialNodes != 1 {
		t.Fatalf("partial node status count = %d, nodes=%+v", partialNodes, run.NodeRuns)
	}
}

func TestExecutorRunsReadyCollectorNodesInParallel(t *testing.T) {
	definition := parallelExecutorDefinition()
	repo := newExecutorMemoryRepo(definition)
	skills := &concurrentWorkflowSkills{delay: 40 * time.Millisecond}
	executor := NewExecutor(repo, fakeWorkflowAgents{}, skills, time.Second)

	run, err := executor.Run(context.Background(), ExecutorInput{Actor: adminWorkflowActor(), WorkflowID: 1, Input: json.RawMessage(`{"hostId":1}`)})
	if err != nil || run.Status != model.WorkflowRunStatusSuccess {
		t.Fatalf("parallel workflow run = %+v, error = %v", run, err)
	}
	if skills.maxActive < 3 {
		t.Fatalf("collector nodes were not parallel, max active = %d", skills.maxActive)
	}
}

func TestExecutorCancelStopsActiveNodesAndPersistsCancelled(t *testing.T) {
	definition := Definition{
		Name: "cancel_workflow", Version: "v1",
		Nodes: []Node{{ID: "start", Type: NodeTypeStart}, {ID: "wait", Type: NodeTypeSkill, SkillName: "echo_safe"}, {ID: "end", Type: NodeTypeEnd}},
		Edges: []Edge{{From: "start", To: "wait"}, {From: "wait", To: "end"}},
	}
	repo := newExecutorMemoryRepo(definition)
	skills := &blockingWorkflowSkills{started: make(chan struct{})}
	executor := NewExecutor(repo, fakeWorkflowAgents{}, skills, time.Second)
	type outcome struct {
		run *model.WorkflowRun
		err error
	}
	finished := make(chan outcome, 1)
	go func() {
		run, err := executor.Run(context.Background(), ExecutorInput{Actor: adminWorkflowActor(), WorkflowID: 1, Input: json.RawMessage(`{}`)})
		finished <- outcome{run: run, err: err}
	}()
	<-skills.started
	cancelled, err := executor.Cancel(context.Background(), 1)
	if err != nil || cancelled.Status != model.WorkflowRunStatusCancelled {
		t.Fatalf("Cancel() = %+v, %v", cancelled, err)
	}
	result := <-finished
	if !errors.Is(result.err, context.Canceled) || result.run == nil || result.run.Status != model.WorkflowRunStatusCancelled {
		t.Fatalf("cancelled run = %+v, error = %v", result.run, result.err)
	}
	if !hasNodeStatus(result.run.NodeRuns, model.WorkflowRunStatusCancelled) {
		t.Fatalf("active node was not marked cancelled: %+v", result.run.NodeRuns)
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

func hasNodeStatus(nodes []model.WorkflowNodeRun, status string) bool {
	for _, node := range nodes {
		if node.Status == status {
			return true
		}
	}
	return false
}

func parallelExecutorDefinition() Definition {
	return Definition{
		Name: "parallel_workflow", Version: "v1",
		Nodes: []Node{
			{ID: "start", Type: NodeTypeStart},
			{ID: "cpu", Type: NodeTypeSkill, SkillName: "echo_safe"},
			{ID: "memory", Type: NodeTypeSkill, SkillName: "echo_safe"},
			{ID: "filesystem", Type: NodeTypeSkill, SkillName: "echo_safe"},
			{ID: "merge", Type: NodeTypeMerge}, {ID: "end", Type: NodeTypeEnd},
		},
		Edges: []Edge{
			{From: "start", To: "cpu"}, {From: "start", To: "memory"}, {From: "start", To: "filesystem"},
			{From: "cpu", To: "merge"}, {From: "memory", To: "merge"}, {From: "filesystem", To: "merge"}, {From: "merge", To: "end"},
		},
	}
}

type executorMemoryRepo struct {
	mu         sync.Mutex
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
	r.mu.Lock()
	defer r.mu.Unlock()
	run.ID = r.nextRunID
	r.nextRunID++
	copied := *run
	r.runs = append(r.runs, copied)
	return nil
}

func (r *executorMemoryRepo) UpdateWorkflowRun(_ context.Context, id int64, updates repository.WorkflowRunUpdates) (*model.WorkflowRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
		return r.findWorkflowRunByID(id)
	}
	return nil, repository.ErrNotFound
}

func (r *executorMemoryRepo) FindWorkflowRunByID(_ context.Context, id int64) (*model.WorkflowRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.findWorkflowRunByID(id)
}

func (r *executorMemoryRepo) findWorkflowRunByID(id int64) (*model.WorkflowRun, error) {
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
	r.mu.Lock()
	defer r.mu.Unlock()
	run.ID = r.nextNodeID
	r.nextNodeID++
	copied := *run
	r.nodes = append(r.nodes, copied)
	return nil
}

func (r *executorMemoryRepo) UpdateWorkflowNodeRun(_ context.Context, id int64, updates repository.WorkflowNodeRunUpdates) (*model.WorkflowNodeRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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

func (r *executorMemoryRepo) CancelWorkflowRun(_ context.Context, id int64, finishedAt time.Time) (*model.WorkflowRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index := range r.runs {
		if r.runs[index].ID == id && r.runs[index].Status == model.WorkflowRunStatusRunning {
			r.runs[index].Status = model.WorkflowRunStatusCancelled
			r.runs[index].FinishedAt = &finishedAt
			return r.findWorkflowRunByID(id)
		}
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
	fail    bool
	partial bool
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
	if s.partial {
		return &skillframework.ExecuteResult{SkillName: input.Name, Output: json.RawMessage(`{"partial":true,"status":"unknown"}`)}, nil
	}
	return &skillframework.ExecuteResult{SkillName: input.Name, Output: json.RawMessage(`{"ok":true}`)}, nil
}

type concurrentWorkflowSkills struct {
	mu        sync.Mutex
	active    int
	maxActive int
	delay     time.Duration
}

func (s *concurrentWorkflowSkills) Get(name string) (skillframework.SkillDefinition, error) {
	if name != "echo_safe" {
		return skillframework.SkillDefinition{}, errors.New("skill not found")
	}
	return skillframework.SkillDefinition{Name: name, Enabled: true}, nil
}

func (s *concurrentWorkflowSkills) Execute(ctx context.Context, input skillframework.ExecuteInput) (*skillframework.ExecuteResult, error) {
	s.mu.Lock()
	s.active++
	if s.active > s.maxActive {
		s.maxActive = s.active
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.active--
		s.mu.Unlock()
	}()
	select {
	case <-time.After(s.delay):
		return &skillframework.ExecuteResult{SkillName: input.Name, Output: json.RawMessage(`{"ok":true}`)}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type blockingWorkflowSkills struct {
	once    sync.Once
	started chan struct{}
}

func (s *blockingWorkflowSkills) Get(name string) (skillframework.SkillDefinition, error) {
	return skillframework.SkillDefinition{Name: name, Enabled: true}, nil
}

func (s *blockingWorkflowSkills) Execute(ctx context.Context, _ skillframework.ExecuteInput) (*skillframework.ExecuteResult, error) {
	s.once.Do(func() { close(s.started) })
	<-ctx.Done()
	return nil, ctx.Err()
}

func adminWorkflowActor() *model.AppUser {
	return &model.AppUser{ID: 1, Username: "admin", Role: model.RoleAdmin, Enabled: true}
}
