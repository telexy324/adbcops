package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"aiops-platform/backend/internal/agentruntime"
	"aiops-platform/backend/internal/auditutil"
	appmiddleware "aiops-platform/backend/internal/middleware"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/observability"
	"aiops-platform/backend/internal/repository"
	"aiops-platform/backend/internal/resourcelimit"
	"aiops-platform/backend/internal/skillframework"
)

const defaultWorkflowTimeout = 180 * time.Second

var ErrWorkflowCancelled = errors.New("workflow cancelled")
var ErrWorkflowLimited = errors.New("workflow concurrency limit exceeded")

type DefinitionRepository interface {
	FindWorkflowDefinitionByID(ctx context.Context, id int64) (*model.WorkflowDefinition, error)
}

type RunRepository interface {
	CreateWorkflowRun(ctx context.Context, run *model.WorkflowRun) error
	UpdateWorkflowRun(ctx context.Context, id int64, updates repository.WorkflowRunUpdates) (*model.WorkflowRun, error)
	FindWorkflowRunByID(ctx context.Context, id int64) (*model.WorkflowRun, error)
	CreateWorkflowNodeRun(ctx context.Context, run *model.WorkflowNodeRun) error
	UpdateWorkflowNodeRun(ctx context.Context, id int64, updates repository.WorkflowNodeRunUpdates) (*model.WorkflowNodeRun, error)
}

type AgentRunner interface {
	Run(ctx context.Context, input agentruntime.RunInput) (*agentruntime.RunOutput, error)
}

type SkillRunner interface {
	Execute(ctx context.Context, input skillframework.ExecuteInput) (*skillframework.ExecuteResult, error)
}

type Executor struct {
	definitions  DefinitionRepository
	runs         RunRepository
	agents       AgentRunner
	skills       SkillRunner
	agentCatalog AgentCatalog
	skillCatalog SkillCatalog
	timeout      time.Duration
	limiter      *resourcelimit.Limiter
	now          func() time.Time
}

type ExecutorInput struct {
	Actor          *model.AppUser
	WorkflowID     int64
	ConversationID *int64
	IncidentID     *int64
	Input          json.RawMessage
	Timeout        time.Duration
}

func NewExecutor(repository interface {
	DefinitionRepository
	RunRepository
}, agents interface {
	AgentRunner
	AgentCatalog
}, skills interface {
	SkillRunner
	SkillCatalog
}, timeout time.Duration) *Executor {
	if timeout <= 0 {
		timeout = defaultWorkflowTimeout
	}
	return &Executor{
		definitions:  repository,
		runs:         repository,
		agents:       agents,
		skills:       skills,
		agentCatalog: agents,
		skillCatalog: skills,
		timeout:      timeout,
		limiter:      resourcelimit.NewLimiter(8),
		now:          func() time.Time { return time.Now().UTC() },
	}
}

func (e *Executor) SetLimiter(limiter *resourcelimit.Limiter) {
	e.limiter = limiter
}

func (e *Executor) Run(ctx context.Context, input ExecutorInput) (*model.WorkflowRun, error) {
	if input.Actor == nil || input.WorkflowID <= 0 {
		return nil, ErrInvalidDefinition
	}
	startedAt := e.now()
	release, err := e.limiter.Acquire(ctx)
	if err != nil {
		if errors.Is(err, resourcelimit.ErrLimitExceeded) {
			return nil, ErrWorkflowLimited
		}
		return nil, err
	}
	defer release()
	timeout := e.timeout
	if input.Timeout > 0 {
		timeout = input.Timeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	definitionRecord, err := e.definitions.FindWorkflowDefinitionByID(ctx, input.WorkflowID)
	if err != nil {
		return nil, err
	}
	if !definitionRecord.Enabled {
		return nil, fmt.Errorf("%w: workflow disabled", ErrInvalidDefinition)
	}
	definition, err := decodeDefinition(definitionRecord.Definition)
	if err != nil {
		return nil, err
	}
	if err := MustValidate(definition, e.agentCatalog, e.skillCatalog); err != nil {
		return nil, err
	}
	now := e.now()
	workflowID := definitionRecord.ID
	userID := input.Actor.ID
	requestID := appmiddleware.GetRequestIDFromContext(ctx)
	run := &model.WorkflowRun{
		WorkflowID:     &workflowID,
		UserID:         &userID,
		RequestID:      stringPtrOrNil(requestID),
		ConversationID: input.ConversationID,
		IncidentID:     input.IncidentID,
		Status:         model.WorkflowRunStatusRunning,
		Input:          auditutil.SanitizeJSON(normalizedJSON(input.Input), 8192),
		StartedAt:      &now,
	}
	if err := e.runs.CreateWorkflowRun(ctx, run); err != nil {
		return nil, err
	}
	output, executeErr := e.execute(runCtx, run.ID, input.Actor, definition, normalizedJSON(input.Input))
	finishedAt := e.now()
	status := model.WorkflowRunStatusSuccess
	var message *string
	if executeErr != nil {
		if errors.Is(executeErr, context.DeadlineExceeded) || errors.Is(executeErr, context.Canceled) {
			status = model.WorkflowRunStatusFailed
		} else {
			status = model.WorkflowRunStatusPartialSuccess
		}
		text := executeErr.Error()
		message = &text
	}
	outputRaw, _ := json.Marshal(output)
	updated, updateErr := e.runs.UpdateWorkflowRun(ctx, run.ID, repository.WorkflowRunUpdates{
		Status:       status,
		Output:       auditutil.SanitizeJSON(outputRaw, 8192),
		ErrorMessage: message,
		FinishedAt:   &finishedAt,
	})
	if updateErr != nil {
		observability.ObserveWorkflow(model.WorkflowRunStatusFailed, e.now().Sub(startedAt))
		return nil, updateErr
	}
	observability.ObserveWorkflow(status, finishedAt.Sub(startedAt))
	if executeErr != nil {
		return updated, executeErr
	}
	return updated, nil
}

func stringPtrOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (e *Executor) execute(ctx context.Context, workflowRunID int64, actor *model.AppUser, definition Definition, input json.RawMessage) (map[string]any, error) {
	graph := buildExecutionGraph(definition)
	completed := map[string]bool{}
	failed := map[string]bool{}
	outputs := map[string]json.RawMessage{}
	var firstErr error
	for len(completed)+len(failed) < len(graph.nodes) {
		if err := ctx.Err(); err != nil {
			return map[string]any{"nodes": outputs}, err
		}
		ready := graph.ready(completed, failed)
		if len(ready) == 0 {
			return map[string]any{"nodes": outputs}, fmt.Errorf("%w: no ready workflow nodes", ErrInvalidDefinition)
		}
		results := e.executeBatch(ctx, workflowRunID, actor, ready, input, outputs)
		for _, result := range results {
			if result.err != nil {
				failed[result.node.ID] = true
				if firstErr == nil {
					firstErr = result.err
				}
				continue
			}
			completed[result.node.ID] = true
			if len(result.output) > 0 {
				outputs[result.node.ID] = result.output
			}
		}
		if firstErr != nil {
			return map[string]any{"nodes": outputs}, firstErr
		}
	}
	return map[string]any{"nodes": outputs}, nil
}

type nodeResult struct {
	node   Node
	output json.RawMessage
	err    error
}

func (e *Executor) executeBatch(ctx context.Context, workflowRunID int64, actor *model.AppUser, nodes []Node, input json.RawMessage, previous map[string]json.RawMessage) []nodeResult {
	results := make([]nodeResult, len(nodes))
	var wg sync.WaitGroup
	for index, node := range nodes {
		wg.Add(1)
		go func(index int, node Node) {
			defer wg.Done()
			output, err := e.executeNode(ctx, workflowRunID, actor, node, input, previous)
			results[index] = nodeResult{node: node, output: output, err: err}
		}(index, node)
	}
	wg.Wait()
	return results
}

func (e *Executor) executeNode(ctx context.Context, workflowRunID int64, actor *model.AppUser, node Node, input json.RawMessage, previous map[string]json.RawMessage) (json.RawMessage, error) {
	startedAt := e.now()
	nodeInput := nodeInputPayload(node, input, previous)
	nodeRun := &model.WorkflowNodeRun{
		WorkflowRunID: workflowRunID,
		NodeID:        node.ID,
		NodeType:      node.Type,
		Status:        model.WorkflowRunStatusRunning,
		Input:         auditutil.SanitizeJSON(nodeInput, 8192),
		Attempt:       1,
		StartedAt:     &startedAt,
	}
	if err := e.runs.CreateWorkflowNodeRun(ctx, nodeRun); err != nil {
		return nil, err
	}
	output, executeErr := e.dispatchNode(ctx, workflowRunID, nodeRun.ID, actor, node, nodeInput)
	finishedAt := e.now()
	status := model.WorkflowRunStatusSuccess
	var message *string
	if executeErr != nil {
		status = model.WorkflowRunStatusFailed
		text := executeErr.Error()
		message = &text
	}
	if _, err := e.runs.UpdateWorkflowNodeRun(ctx, nodeRun.ID, repository.WorkflowNodeRunUpdates{
		Status:       status,
		Output:       auditutil.SanitizeJSON(output, 8192),
		ErrorMessage: message,
		FinishedAt:   &finishedAt,
	}); err != nil {
		return nil, err
	}
	return output, executeErr
}

func (e *Executor) dispatchNode(ctx context.Context, workflowRunID, nodeRunID int64, actor *model.AppUser, node Node, input json.RawMessage) (json.RawMessage, error) {
	switch node.Type {
	case NodeTypeStart, NodeTypeEnd, NodeTypeCondition, NodeTypeMerge:
		return json.Marshal(map[string]any{"status": "ok", "nodeType": node.Type})
	case NodeTypeSkill:
		result, err := e.skills.Execute(ctx, skillframework.ExecuteInput{
			Actor:         actor,
			Name:          node.SkillName,
			Payload:       skillPayload(input),
			WorkflowRunID: &workflowRunID,
			NodeRunID:     &nodeRunID,
		})
		if err != nil {
			return nil, err
		}
		return result.Output, nil
	case NodeTypeAgent:
		result, err := e.agents.Run(ctx, agentruntime.RunInput{
			Actor:         actor,
			Name:          node.AgentName,
			Context:       agentContext(actor, input),
			WorkflowRunID: &workflowRunID,
		})
		if err != nil {
			return nil, err
		}
		return json.Marshal(result.Result)
	default:
		return nil, fmt.Errorf("%w: unsupported node type %s", ErrInvalidDefinition, node.Type)
	}
}

type executionGraph struct {
	nodes    map[string]Node
	incoming map[string][]string
	order    []string
}

func buildExecutionGraph(definition Definition) executionGraph {
	graph := executionGraph{nodes: map[string]Node{}, incoming: map[string][]string{}, order: []string{}}
	for _, node := range definition.Nodes {
		graph.nodes[node.ID] = node
		graph.order = append(graph.order, node.ID)
	}
	for _, edge := range definition.Edges {
		graph.incoming[edge.To] = append(graph.incoming[edge.To], edge.From)
	}
	return graph
}

func (g executionGraph) ready(completed, failed map[string]bool) []Node {
	ready := []Node{}
	for _, id := range g.order {
		if completed[id] || failed[id] {
			continue
		}
		blocked := false
		for _, parent := range g.incoming[id] {
			if failed[parent] || !completed[parent] {
				blocked = true
				break
			}
		}
		if !blocked {
			ready = append(ready, g.nodes[id])
		}
	}
	return ready
}

func decodeDefinition(raw []byte) (Definition, error) {
	var definition Definition
	if err := json.Unmarshal(raw, &definition); err != nil {
		return Definition{}, err
	}
	return definition, nil
}

func normalizedJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return json.RawMessage(`{}`)
	}
	return raw
}

func nodeInputPayload(node Node, workflowInput json.RawMessage, previous map[string]json.RawMessage) json.RawMessage {
	var config map[string]json.RawMessage
	if len(node.Config) > 0 && json.Unmarshal(node.Config, &config) == nil {
		if input, ok := config["input"]; ok && json.Valid(input) {
			return input
		}
		if contextInput, ok := config["context"]; ok && json.Valid(contextInput) {
			return contextInput
		}
	}
	envelope := map[string]any{"workflowInput": json.RawMessage(workflowInput), "previous": previous}
	raw, _ := json.Marshal(envelope)
	return raw
}

func skillPayload(input json.RawMessage) json.RawMessage {
	var envelope map[string]json.RawMessage
	if json.Unmarshal(input, &envelope) == nil {
		if workflowInput, ok := envelope["workflowInput"]; ok && json.Valid(workflowInput) {
			return workflowInput
		}
	}
	return input
}

func agentContext(actor *model.AppUser, input json.RawMessage) agentruntime.AgentContext {
	var body map[string]any
	_ = json.Unmarshal(input, &body)
	query := ""
	if value, ok := body["query"].(string); ok {
		query = value
	}
	scope, _ := body["scope"].(map[string]any)
	variables, _ := body["variables"].(map[string]any)
	return agentruntime.AgentContext{
		UserID:    actor.ID,
		Query:     query,
		Scope:     scope,
		Variables: variables,
	}
}
