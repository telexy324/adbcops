package handler

import (
	"errors"
	"net/http"
	"time"

	metricssvc "aiops-platform/backend/internal/metrics"
	"aiops-platform/backend/internal/repository"
	"github.com/gin-gonic/gin"
)

type MetricsHandler struct {
	service *metricssvc.Service
}

func NewMetricsHandler(service *metricssvc.Service) *MetricsHandler {
	return &MetricsHandler{service: service}
}

type metricsTestRequest struct {
	DataSourceID int64 `json:"dataSourceId" binding:"required"`
}

type metricsQueryRequest struct {
	DataSourceID int64  `json:"dataSourceId" binding:"required"`
	Query        string `json:"query" binding:"required"`
	Range        bool   `json:"range"`
	Start        string `json:"start"`
	End          string `json:"end"`
	StepSeconds  int    `json:"stepSeconds"`
	TimeoutMs    int    `json:"timeoutMs"`
	MaxSeries    int    `json:"maxSeries"`
	MaxPoints    int    `json:"maxPoints"`
}

func (h *MetricsHandler) Test(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request metricsTestRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	result, err := h.service.Test(c.Request.Context(), actor, request.DataSourceID)
	if handleMetricsError(c, err, "test prometheus data source failed") {
		return
	}
	success(c, result)
}

func (h *MetricsHandler) Query(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request metricsQueryRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	start, err := parseOptionalTime(request.Start)
	if err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid start")
		return
	}
	end, err := parseOptionalTime(request.End)
	if err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid end")
		return
	}
	result, err := h.service.Query(c.Request.Context(), actor, metricssvc.QueryInput{
		DataSourceID: request.DataSourceID,
		Query:        request.Query,
		Range:        request.Range,
		Start:        start,
		End:          end,
		Step:         time.Duration(request.StepSeconds) * time.Second,
		Timeout:      time.Duration(request.TimeoutMs) * time.Millisecond,
		MaxSeries:    request.MaxSeries,
		MaxPoints:    request.MaxPoints,
	})
	if handleMetricsError(c, err, "query prometheus metrics failed") {
		return
	}
	success(c, result)
}

func handleMetricsError(c *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, metricssvc.ErrInvalidInput), errors.Is(err, metricssvc.ErrUnsupportedSource), errors.Is(err, metricssvc.ErrDataSourceDisabled):
		failure(c, http.StatusBadRequest, 40001, "invalid request")
	case errors.Is(err, metricssvc.ErrForbidden):
		failure(c, http.StatusForbidden, 40314, "metrics access forbidden")
	case errors.Is(err, metricssvc.ErrMetricsTimeout):
		failure(c, http.StatusGatewayTimeout, 50401, "metrics query timeout")
	case errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40401, "data source not found")
	default:
		failure(c, http.StatusInternalServerError, 50091, fallback)
	}
	return true
}

func parseOptionalTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return parseRequestTime(value)
}
