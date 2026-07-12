package handler

import (
	"errors"
	"net/http"

	correlationsvc "aiops-platform/backend/internal/correlation"
	"aiops-platform/backend/internal/repository"
	"github.com/gin-gonic/gin"
)

type CorrelationHandler struct {
	service *correlationsvc.Service
}

func NewCorrelationHandler(service *correlationsvc.Service) *CorrelationHandler {
	return &CorrelationHandler{service: service}
}

func (h *CorrelationHandler) Analyze(c *gin.Context) {
	var request correlationsvc.Query
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	result, err := h.service.Analyze(c.Request.Context(), request)
	if handleCorrelationError(c, err, "analyze correlation failed") {
		return
	}
	success(c, result)
}

func handleCorrelationError(c *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, correlationsvc.ErrInvalidInput):
		failure(c, http.StatusBadRequest, 40001, "invalid request")
	case errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40401, "target event not found")
	default:
		failure(c, http.StatusInternalServerError, 50094, fallback)
	}
	return true
}
