package handler

import (
	"errors"
	"net/http"
	"strconv"

	"aiops-platform/backend/internal/agentruntime"
	"aiops-platform/backend/internal/repository"
	"github.com/gin-gonic/gin"
)

type AgentHandler struct {
	runtime *agentruntime.Runtime
}

func NewAgentHandler(runtime *agentruntime.Runtime) *AgentHandler {
	return &AgentHandler{runtime: runtime}
}

func (h *AgentHandler) List(c *gin.Context) {
	success(c, h.runtime.List())
}

func (h *AgentHandler) Get(c *gin.Context) {
	result, err := h.runtime.Get(c.Param("name"))
	if handleAgentError(c, err, "get agent failed") {
		return
	}
	success(c, result)
}

func (h *AgentHandler) Test(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request struct {
		Context agentruntime.AgentContext `json:"context"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	if request.Context.UserID == 0 {
		request.Context.UserID = actor.ID
	}
	result, err := h.runtime.Run(c.Request.Context(), agentruntime.RunInput{
		Actor:   actor,
		Name:    c.Param("name"),
		Context: request.Context,
	})
	if handleAgentError(c, err, "test agent failed") {
		return
	}
	success(c, result)
}

func (h *AgentHandler) ListRuns(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	result, err := h.runtime.ListRuns(c.Request.Context(), limit)
	if handleAgentError(c, err, "list agent runs failed") {
		return
	}
	success(c, result)
}

func (h *AgentHandler) GetRun(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid id")
		return
	}
	result, err := h.runtime.GetRun(c.Request.Context(), id)
	if handleAgentError(c, err, "get agent run failed") {
		return
	}
	success(c, result)
}

func handleAgentError(c *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, agentruntime.ErrInvalidInput), errors.Is(err, agentruntime.ErrContextTooLarge):
		failure(c, http.StatusBadRequest, 40001, "invalid request")
	case errors.Is(err, agentruntime.ErrAgentNotFound), errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40401, "agent not found")
	case errors.Is(err, agentruntime.ErrStepLimitExceeded):
		failure(c, http.StatusConflict, 40901, "agent step limit exceeded")
	case errors.Is(err, agentruntime.ErrSkillLimitExceeded):
		failure(c, http.StatusConflict, 40902, "agent skill call limit exceeded")
	case errors.Is(err, agentruntime.ErrEvidenceRefMissing):
		failure(c, http.StatusBadRequest, 40002, "agent evidence reference missing")
	default:
		failure(c, http.StatusInternalServerError, 50095, fallback)
	}
	return true
}
