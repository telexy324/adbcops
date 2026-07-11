package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"aiops-platform/backend/internal/skillframework"
	"github.com/gin-gonic/gin"
)

type SkillHandler struct {
	registry *skillframework.Registry
}

func NewSkillHandler(registry *skillframework.Registry) *SkillHandler {
	return &SkillHandler{registry: registry}
}

func (h *SkillHandler) List(c *gin.Context) {
	success(c, h.registry.List())
}

func (h *SkillHandler) Get(c *gin.Context) {
	result, err := h.registry.Get(c.Param("name"))
	if handleSkillError(c, err, "get skill failed") {
		return
	}
	success(c, result)
}

func (h *SkillHandler) Execute(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request struct {
		Input json.RawMessage `json:"input"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	result, err := h.registry.Execute(c.Request.Context(), skillframework.ExecuteInput{
		Actor:   actor,
		Name:    c.Param("name"),
		Payload: request.Input,
	})
	if handleSkillError(c, err, "execute skill failed") {
		return
	}
	success(c, result)
}

func (h *SkillHandler) Enable(c *gin.Context) {
	result, err := h.registry.Enable(c.Param("name"))
	if handleSkillError(c, err, "enable skill failed") {
		return
	}
	success(c, result)
}

func (h *SkillHandler) Disable(c *gin.Context) {
	result, err := h.registry.Disable(c.Param("name"))
	if handleSkillError(c, err, "disable skill failed") {
		return
	}
	success(c, result)
}

func (h *SkillHandler) ListRuns(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	result, err := h.registry.ListRuns(c.Request.Context(), limit)
	if handleSkillError(c, err, "list skill runs failed") {
		return
	}
	success(c, result)
}

func handleSkillError(c *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, skillframework.ErrInvalidInput):
		failure(c, http.StatusBadRequest, 40001, "invalid request")
	case errors.Is(err, skillframework.ErrSkillNotFound):
		failure(c, http.StatusNotFound, 40401, "skill not found")
	case errors.Is(err, skillframework.ErrSkillDisabled):
		failure(c, http.StatusForbidden, 40316, "skill disabled")
	case errors.Is(err, skillframework.ErrPermissionDenied):
		failure(c, http.StatusForbidden, 40317, "skill permission denied")
	case errors.Is(err, skillframework.ErrRiskNotAllowed):
		failure(c, http.StatusForbidden, 40318, "skill risk not allowed")
	case errors.Is(err, skillframework.ErrToolUnavailable):
		failure(c, http.StatusForbidden, 40319, "required tool unavailable")
	default:
		failure(c, http.StatusInternalServerError, 50094, fallback)
	}
	return true
}
