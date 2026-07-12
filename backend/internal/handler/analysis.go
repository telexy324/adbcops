package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	analysissvc "aiops-platform/backend/internal/analysis"
	logssvc "aiops-platform/backend/internal/logs"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
	"github.com/gin-gonic/gin"
)

type AnalysisHandler struct {
	logs     *logssvc.Service
	analysis *analysissvc.Service
}

func NewAnalysisHandler(logs *logssvc.Service, analysis *analysissvc.Service) *AnalysisHandler {
	return &AnalysisHandler{logs: logs, analysis: analysis}
}

type queryLogsRequest struct {
	DataSourceID    int64  `json:"dataSourceId" binding:"required"`
	Index           string `json:"index"`
	From            string `json:"from" binding:"required"`
	To              string `json:"to" binding:"required"`
	Keyword         string `json:"keyword"`
	QueryString     string `json:"queryString"`
	Level           string `json:"level"`
	Size            int    `json:"size"`
	TimeoutMs       int    `json:"timeoutMs"`
	AllowLargeRange bool   `json:"allowLargeRange"`
}

type preprocessLogsRequest struct {
	Items         []model.LogItem `json:"items" binding:"required"`
	StackMaxLines int             `json:"stackMaxLines"`
}

type analysisScopeRequest struct {
	Environment   string `json:"environment"`
	SystemName    string `json:"systemName"`
	ComponentName string `json:"componentName"`
	TimeStart     string `json:"timeStart" binding:"required"`
	TimeEnd       string `json:"timeEnd" binding:"required"`
}

type generalAnalysisRequest struct {
	ConversationID *int64               `json:"conversationId"`
	Question       string               `json:"question" binding:"required"`
	Scope          analysisScopeRequest `json:"scope" binding:"required"`
	DataSourceIDs  []int64              `json:"dataSourceIds" binding:"required"`
}

func (h *AnalysisHandler) QueryLogs(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request queryLogsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	from, err := parseRequestTime(request.From)
	if err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	to, err := parseRequestTime(request.To)
	if err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	result, err := h.logs.Query(c.Request.Context(), actor, logssvc.QueryInput{
		DataSourceID:    request.DataSourceID,
		Index:           request.Index,
		From:            from,
		To:              to,
		Keyword:         request.Keyword,
		QueryString:     request.QueryString,
		Level:           request.Level,
		Size:            request.Size,
		Timeout:         time.Duration(request.TimeoutMs) * time.Millisecond,
		AllowLargeRange: request.AllowLargeRange,
	})
	if handleAnalysisError(c, err, "query logs failed") {
		return
	}
	success(c, result)
}

func (h *AnalysisHandler) PreprocessLogs(c *gin.Context) {
	var request preprocessLogsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	success(c, logssvc.Preprocess(logssvc.PreprocessInput{
		Items:         request.Items,
		StackMaxLines: request.StackMaxLines,
	}))
}

func (h *AnalysisHandler) RunGeneral(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request generalAnalysisRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	start, err := parseRequestTime(request.Scope.TimeStart)
	if err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	end, err := parseRequestTime(request.Scope.TimeEnd)
	if err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	output, err := h.analysis.RunGeneral(c.Request.Context(), actor, analysissvc.RunInput{
		ConversationID: request.ConversationID,
		Question:       request.Question,
		Scope: analysissvc.Scope{
			Environment:   request.Scope.Environment,
			SystemName:    request.Scope.SystemName,
			ComponentName: request.Scope.ComponentName,
			TimeStart:     start,
			TimeEnd:       end,
		},
		DataSourceIDs: request.DataSourceIDs,
	})
	if handleAnalysisError(c, err, "run analysis failed") {
		return
	}
	success(c, output)
}

func (h *AnalysisHandler) ListTasks(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	tasks, err := h.analysis.List(c.Request.Context(), actor)
	if handleAnalysisError(c, err, "list analysis tasks failed") {
		return
	}
	success(c, toAnalysisTaskResponses(tasks))
}

func (h *AnalysisHandler) GetTask(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	task, err := h.analysis.Get(c.Request.Context(), actor, id)
	if handleAnalysisError(c, err, "get analysis task failed") {
		return
	}
	success(c, toAnalysisTaskResponse(task))
}

func handleAnalysisError(c *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, analysissvc.ErrInvalidInput):
		failure(c, http.StatusBadRequest, 40001, "invalid request")
	case errors.Is(err, analysissvc.ErrForbidden):
		failure(c, http.StatusForbidden, 40310, "analysis access forbidden")
	case errors.Is(err, analysissvc.ErrRateLimited):
		failure(c, http.StatusTooManyRequests, 42901, "analysis concurrency limit exceeded")
	case errors.Is(err, logssvc.ErrTimeRangeTooLarge):
		failure(c, http.StatusBadRequest, 40008, "time range exceeds 24 hours")
	case errors.Is(err, logssvc.ErrLogQueryTimeout):
		failure(c, http.StatusGatewayTimeout, 50401, "log query timeout")
	case errors.Is(err, logssvc.ErrDataSourceLimited):
		failure(c, http.StatusTooManyRequests, 42902, "data source concurrency limit exceeded")
	case errors.Is(err, logssvc.ErrInvalidInput), errors.Is(err, logssvc.ErrUnsupportedSource), errors.Is(err, logssvc.ErrDataSourceDisabled):
		failure(c, http.StatusBadRequest, 40001, "invalid request")
	case errors.Is(err, logssvc.ErrForbidden):
		failure(c, http.StatusForbidden, 40307, "log access forbidden")
	case errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40401, "data source not found")
	default:
		failure(c, http.StatusInternalServerError, 50070, fallback)
	}
	return true
}

type analysisTaskResponse struct {
	ID             int64           `json:"id"`
	UserID         int64           `json:"userId"`
	ConversationID *int64          `json:"conversationId,omitempty"`
	TaskType       string          `json:"taskType"`
	Question       string          `json:"question"`
	Scope          json.RawMessage `json:"scope,omitempty"`
	DataSourceIDs  json.RawMessage `json:"dataSourceIds,omitempty"`
	Status         string          `json:"status"`
	Summary        *string         `json:"summary,omitempty"`
	Result         json.RawMessage `json:"result,omitempty"`
	ErrorMessage   *string         `json:"errorMessage,omitempty"`
	StartedAt      *time.Time      `json:"startedAt,omitempty"`
	FinishedAt     *time.Time      `json:"finishedAt,omitempty"`
	CreatedAt      string          `json:"createdAt"`
	UpdatedAt      string          `json:"updatedAt"`
}

func toAnalysisTaskResponses(tasks []model.AnalysisTask) []analysisTaskResponse {
	response := make([]analysisTaskResponse, 0, len(tasks))
	for index := range tasks {
		response = append(response, toAnalysisTaskResponse(&tasks[index]))
	}
	return response
}

func toAnalysisTaskResponse(task *model.AnalysisTask) analysisTaskResponse {
	return analysisTaskResponse{
		ID:             task.ID,
		UserID:         task.UserID,
		ConversationID: task.ConversationID,
		TaskType:       task.TaskType,
		Question:       task.Question,
		Scope:          rawJSON(task.Scope),
		DataSourceIDs:  rawJSON(task.DataSourceIDs),
		Status:         task.Status,
		Summary:        task.Summary,
		Result:         rawJSON(task.Result),
		ErrorMessage:   task.ErrorMessage,
		StartedAt:      task.StartedAt,
		FinishedAt:     task.FinishedAt,
		CreatedAt:      task.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      task.UpdatedAt.Format(time.RFC3339),
	}
}

func parseRequestTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, logssvc.ErrInvalidInput
}
