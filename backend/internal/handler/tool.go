package handler

import (
	"errors"
	"net/http"

	"aiops-platform/backend/internal/toolregistry"
	"github.com/gin-gonic/gin"
)

type ToolHandler struct {
	registry *toolregistry.Registry
}

func NewToolHandler(registry *toolregistry.Registry) *ToolHandler {
	return &ToolHandler{registry: registry}
}

func (h *ToolHandler) List(c *gin.Context) {
	success(c, h.registry.List())
}

func (h *ToolHandler) Get(c *gin.Context) {
	result, err := h.registry.Get(c.Param("name"))
	if handleToolError(c, err, "get tool failed") {
		return
	}
	success(c, result)
}

func (h *ToolHandler) Test(c *gin.Context) {
	if err := h.registry.Test(c.Request.Context(), c.Param("name")); handleToolError(c, err, "test tool failed") {
		return
	}
	success(c, gin.H{"ok": true})
}

func (h *ToolHandler) Enable(c *gin.Context) {
	result, err := h.registry.Enable(c.Param("name"))
	if handleToolError(c, err, "enable tool failed") {
		return
	}
	success(c, result)
}

func (h *ToolHandler) Disable(c *gin.Context) {
	result, err := h.registry.Disable(c.Param("name"))
	if handleToolError(c, err, "disable tool failed") {
		return
	}
	success(c, result)
}

func handleToolError(c *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, toolregistry.ErrInvalidInput):
		failure(c, http.StatusBadRequest, 40001, "invalid request")
	case errors.Is(err, toolregistry.ErrToolNotFound):
		failure(c, http.StatusNotFound, 40401, "tool not found")
	case errors.Is(err, toolregistry.ErrToolDisabled):
		failure(c, http.StatusForbidden, 40315, "tool disabled")
	default:
		failure(c, http.StatusInternalServerError, 50093, fallback)
	}
	return true
}
