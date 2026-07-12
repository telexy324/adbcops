package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
	workflowdsl "aiops-platform/backend/internal/workflow"
	"github.com/gin-gonic/gin"
)

type WorkflowHandler struct {
	repository repository.WorkflowRepository
	executor   *workflowdsl.Executor
	agents     workflowdsl.AgentCatalog
	skills     workflowdsl.SkillCatalog
}

func NewWorkflowHandler(repository repository.WorkflowRepository, executor *workflowdsl.Executor, agents workflowdsl.AgentCatalog, skills workflowdsl.SkillCatalog) *WorkflowHandler {
	return &WorkflowHandler{repository: repository, executor: executor, agents: agents, skills: skills}
}

type saveWorkflowRequest struct {
	Name        string                 `json:"name"`
	Version     string                 `json:"version"`
	Description *string                `json:"description"`
	Definition  workflowdsl.Definition `json:"definition" binding:"required"`
	Enabled     *bool                  `json:"enabled"`
}

type workflowDefinitionView struct {
	ID          int64                  `json:"id"`
	Name        string                 `json:"name"`
	Version     string                 `json:"version"`
	Description *string                `json:"description,omitempty"`
	Definition  workflowdsl.Definition `json:"definition"`
	Enabled     bool                   `json:"enabled"`
	CreatedBy   *int64                 `json:"createdBy,omitempty"`
	CreatedAt   any                    `json:"createdAt"`
	UpdatedAt   any                    `json:"updatedAt"`
}

type runWorkflowRequest struct {
	Input          json.RawMessage `json:"input"`
	ConversationID *int64          `json:"conversationId"`
	IncidentID     *int64          `json:"incidentId"`
	TimeoutSeconds int             `json:"timeoutSeconds"`
}

type workflowRunView struct {
	ID             int64                 `json:"id"`
	WorkflowID     *int64                `json:"workflowId,omitempty"`
	UserID         *int64                `json:"userId,omitempty"`
	ConversationID *int64                `json:"conversationId,omitempty"`
	IncidentID     *int64                `json:"incidentId,omitempty"`
	Status         string                `json:"status"`
	Input          json.RawMessage       `json:"input,omitempty"`
	Output         json.RawMessage       `json:"output,omitempty"`
	ErrorMessage   *string               `json:"errorMessage,omitempty"`
	StartedAt      any                   `json:"startedAt,omitempty"`
	FinishedAt     any                   `json:"finishedAt,omitempty"`
	CreatedAt      any                   `json:"createdAt"`
	NodeRuns       []workflowNodeRunView `json:"nodeRuns,omitempty"`
}

type workflowNodeRunView struct {
	ID           int64           `json:"id"`
	NodeID       string          `json:"nodeId"`
	NodeType     string          `json:"nodeType"`
	Status       string          `json:"status"`
	Input        json.RawMessage `json:"input,omitempty"`
	Output       json.RawMessage `json:"output,omitempty"`
	ErrorMessage *string         `json:"errorMessage,omitempty"`
	Attempt      int             `json:"attempt"`
	StartedAt    any             `json:"startedAt,omitempty"`
	FinishedAt   any             `json:"finishedAt,omitempty"`
}

func (h *WorkflowHandler) List(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	definitions, err := h.repository.ListWorkflowDefinitions(c.Request.Context(), limit)
	if handleWorkflowError(c, err, "list workflows failed") {
		return
	}
	views := make([]workflowDefinitionView, 0, len(definitions))
	for _, definition := range definitions {
		view, err := workflowView(definition)
		if handleWorkflowError(c, err, "decode workflow failed") {
			return
		}
		views = append(views, view)
	}
	success(c, views)
}

func (h *WorkflowHandler) Get(c *gin.Context) {
	id, ok := workflowID(c)
	if !ok {
		return
	}
	definition, err := h.repository.FindWorkflowDefinitionByID(c.Request.Context(), id)
	if handleWorkflowError(c, err, "get workflow failed") {
		return
	}
	view, err := workflowView(*definition)
	if handleWorkflowError(c, err, "decode workflow failed") {
		return
	}
	success(c, view)
}

func (h *WorkflowHandler) Create(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	request, ok := bindWorkflowRequest(c)
	if !ok {
		return
	}
	definition := normalizeRequestDefinition(request)
	if result := workflowdsl.Validate(definition, h.agents, h.skills); !result.Valid {
		failure(c, http.StatusBadRequest, 40003, "invalid workflow definition")
		return
	}
	raw, err := json.Marshal(definition)
	if handleWorkflowError(c, err, "encode workflow failed") {
		return
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	createdBy := actor.ID
	record := &model.WorkflowDefinition{
		Name:        definition.Name,
		Version:     definition.Version,
		Description: request.Description,
		Definition:  raw,
		Enabled:     enabled,
		CreatedBy:   &createdBy,
	}
	if err := h.repository.CreateWorkflowDefinition(c.Request.Context(), record); handleWorkflowError(c, err, "create workflow failed") {
		return
	}
	view, err := workflowView(*record)
	if handleWorkflowError(c, err, "decode workflow failed") {
		return
	}
	success(c, view)
}

func (h *WorkflowHandler) Update(c *gin.Context) {
	id, ok := workflowID(c)
	if !ok {
		return
	}
	request, ok := bindWorkflowRequest(c)
	if !ok {
		return
	}
	definition := normalizeRequestDefinition(request)
	if result := workflowdsl.Validate(definition, h.agents, h.skills); !result.Valid {
		failure(c, http.StatusBadRequest, 40003, "invalid workflow definition")
		return
	}
	raw, err := json.Marshal(definition)
	if handleWorkflowError(c, err, "encode workflow failed") {
		return
	}
	enabled := true
	enabledSet := false
	if request.Enabled != nil {
		enabled = *request.Enabled
		enabledSet = true
	}
	record, err := h.repository.UpdateWorkflowDefinition(c.Request.Context(), id, repository.WorkflowDefinitionUpdates{
		Name:           definition.Name,
		Version:        definition.Version,
		Description:    request.Description,
		DescriptionSet: true,
		Definition:     raw,
		Enabled:        enabled,
		EnabledSet:     enabledSet,
	})
	if handleWorkflowError(c, err, "update workflow failed") {
		return
	}
	view, err := workflowView(*record)
	if handleWorkflowError(c, err, "decode workflow failed") {
		return
	}
	success(c, view)
}

func (h *WorkflowHandler) Validate(c *gin.Context) {
	request, ok := bindWorkflowRequest(c)
	if !ok {
		return
	}
	definition := normalizeRequestDefinition(request)
	success(c, workflowdsl.Validate(definition, h.agents, h.skills))
}

func (h *WorkflowHandler) Run(c *gin.Context) {
	if h.executor == nil {
		failure(c, http.StatusInternalServerError, 50096, "workflow executor is not configured")
		return
	}
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	id, ok := workflowID(c)
	if !ok {
		return
	}
	var request runWorkflowRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	run, err := h.executor.Run(c.Request.Context(), workflowdsl.ExecutorInput{
		Actor:          actor,
		WorkflowID:     id,
		ConversationID: request.ConversationID,
		IncidentID:     request.IncidentID,
		Input:          request.Input,
		Timeout:        secondsDuration(request.TimeoutSeconds),
	})
	if run != nil {
		success(c, workflowRunViewFromModel(*run))
		return
	}
	if handleWorkflowError(c, err, "run workflow failed") {
		return
	}
	success(c, gin.H{"ok": true})
}

func (h *WorkflowHandler) ListRuns(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	runs, err := h.repository.ListWorkflowRuns(c.Request.Context(), limit)
	if handleWorkflowError(c, err, "list workflow runs failed") {
		return
	}
	views := make([]workflowRunView, 0, len(runs))
	for _, run := range runs {
		views = append(views, workflowRunViewFromModel(run))
	}
	success(c, views)
}

func (h *WorkflowHandler) GetRun(c *gin.Context) {
	id, ok := workflowID(c)
	if !ok {
		return
	}
	run, err := h.repository.FindWorkflowRunByID(c.Request.Context(), id)
	if handleWorkflowError(c, err, "get workflow run failed") {
		return
	}
	success(c, workflowRunViewFromModel(*run))
}

func (h *WorkflowHandler) CancelRun(c *gin.Context) {
	id, ok := workflowID(c)
	if !ok {
		return
	}
	run, err := h.repository.CancelWorkflowRun(c.Request.Context(), id, nowUTC())
	if handleWorkflowError(c, err, "cancel workflow run failed") {
		return
	}
	success(c, workflowRunViewFromModel(*run))
}

func bindWorkflowRequest(c *gin.Context) (saveWorkflowRequest, bool) {
	var request saveWorkflowRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return saveWorkflowRequest{}, false
	}
	return request, true
}

func normalizeRequestDefinition(request saveWorkflowRequest) workflowdsl.Definition {
	definition := request.Definition
	if definition.Name == "" {
		definition.Name = request.Name
	}
	if definition.Version == "" {
		definition.Version = request.Version
	}
	if definition.Description == "" && request.Description != nil {
		definition.Description = *request.Description
	}
	return definition
}

func workflowID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid id")
		return 0, false
	}
	return id, true
}

func workflowView(definition model.WorkflowDefinition) (workflowDefinitionView, error) {
	var decoded workflowdsl.Definition
	if err := json.Unmarshal(definition.Definition, &decoded); err != nil {
		return workflowDefinitionView{}, err
	}
	return workflowDefinitionView{
		ID:          definition.ID,
		Name:        definition.Name,
		Version:     definition.Version,
		Description: definition.Description,
		Definition:  decoded,
		Enabled:     definition.Enabled,
		CreatedBy:   definition.CreatedBy,
		CreatedAt:   definition.CreatedAt,
		UpdatedAt:   definition.UpdatedAt,
	}, nil
}

func workflowRunViewFromModel(run model.WorkflowRun) workflowRunView {
	nodes := make([]workflowNodeRunView, 0, len(run.NodeRuns))
	for _, node := range run.NodeRuns {
		nodes = append(nodes, workflowNodeRunView{
			ID:           node.ID,
			NodeID:       node.NodeID,
			NodeType:     node.NodeType,
			Status:       node.Status,
			Input:        json.RawMessage(node.Input),
			Output:       json.RawMessage(node.Output),
			ErrorMessage: node.ErrorMessage,
			Attempt:      node.Attempt,
			StartedAt:    node.StartedAt,
			FinishedAt:   node.FinishedAt,
		})
	}
	return workflowRunView{
		ID:             run.ID,
		WorkflowID:     run.WorkflowID,
		UserID:         run.UserID,
		ConversationID: run.ConversationID,
		IncidentID:     run.IncidentID,
		Status:         run.Status,
		Input:          json.RawMessage(run.Input),
		Output:         json.RawMessage(run.Output),
		ErrorMessage:   run.ErrorMessage,
		StartedAt:      run.StartedAt,
		FinishedAt:     run.FinishedAt,
		CreatedAt:      run.CreatedAt,
		NodeRuns:       nodes,
	}
}

func secondsDuration(seconds int) time.Duration {
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func nowUTC() time.Time {
	return time.Now().UTC()
}

func handleWorkflowError(c *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, workflowdsl.ErrInvalidDefinition):
		failure(c, http.StatusBadRequest, 40003, "invalid workflow definition")
	case errors.Is(err, workflowdsl.ErrWorkflowLimited):
		failure(c, http.StatusTooManyRequests, 42903, "workflow concurrency limit exceeded")
	case errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40401, "workflow not found")
	default:
		failure(c, http.StatusInternalServerError, 50096, fallback)
	}
	return true
}
