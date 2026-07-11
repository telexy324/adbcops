package handler

import (
	"errors"
	"net/http"
	"time"

	logssvc "aiops-platform/backend/internal/logs"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
	"github.com/gin-gonic/gin"
)

type AnalysisHandler struct {
	logs *logssvc.Service
}

func NewAnalysisHandler(logs *logssvc.Service) *AnalysisHandler {
	return &AnalysisHandler{logs: logs}
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

func handleAnalysisError(c *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, logssvc.ErrTimeRangeTooLarge):
		failure(c, http.StatusBadRequest, 40008, "time range exceeds 24 hours")
	case errors.Is(err, logssvc.ErrLogQueryTimeout):
		failure(c, http.StatusGatewayTimeout, 50401, "log query timeout")
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

func parseRequestTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, logssvc.ErrInvalidInput
}
