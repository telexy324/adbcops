package handler

import (
	"errors"
	"net/http"
	"strconv"

	"aiops-platform/backend/internal/model"
	rcasvc "aiops-platform/backend/internal/rca"
	"aiops-platform/backend/internal/repository"
	"github.com/gin-gonic/gin"
)

type RCAHandler struct {
	service *rcasvc.Service
}

func NewRCAHandler(service *rcasvc.Service) *RCAHandler {
	return &RCAHandler{service: service}
}

func (h *RCAHandler) CreateRun(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request rcasvc.CreateRunInput
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	run, err := h.service.CreateRun(c.Request.Context(), actor, request)
	if handleRCAError(c, err, "create RCA run failed") {
		return
	}
	success(c, run)
}

func (h *RCAHandler) ListRuns(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	runs, err := h.service.ListRuns(c.Request.Context(), actor, limit, c.Query("status"))
	if handleRCAError(c, err, "list RCA runs failed") {
		return
	}
	success(c, runs)
}

func (h *RCAHandler) GetRun(c *gin.Context) {
	actor, runID, ok := currentUserAndRCAID(c)
	if !ok {
		return
	}
	detail, err := h.service.GetDetail(c.Request.Context(), actor, runID)
	if handleRCAError(c, err, "get RCA run failed") {
		return
	}
	success(c, detail)
}

func (h *RCAHandler) ListRounds(c *gin.Context) {
	actor, runID, ok := currentUserAndRCAID(c)
	if !ok {
		return
	}
	rounds, err := h.service.ListRounds(c.Request.Context(), actor, runID)
	if handleRCAError(c, err, "list RCA rounds failed") {
		return
	}
	success(c, rounds)
}

func (h *RCAHandler) ListActions(c *gin.Context) {
	actor, runID, ok := currentUserAndRCAID(c)
	if !ok {
		return
	}
	actions, err := h.service.ListActions(c.Request.Context(), actor, runID)
	if handleRCAError(c, err, "list RCA actions failed") {
		return
	}
	success(c, actions)
}

func (h *RCAHandler) ListEvidence(c *gin.Context) {
	actor, runID, ok := currentUserAndRCAID(c)
	if !ok {
		return
	}
	evidence, err := h.service.ListEvidence(c.Request.Context(), actor, runID)
	if handleRCAError(c, err, "list RCA evidence failed") {
		return
	}
	success(c, evidence)
}

func (h *RCAHandler) Cancel(c *gin.Context) {
	actor, runID, ok := currentUserAndRCAID(c)
	if !ok {
		return
	}
	run, err := h.service.Cancel(c.Request.Context(), actor, runID)
	if handleRCAError(c, err, "cancel RCA run failed") {
		return
	}
	success(c, run)
}

func (h *RCAHandler) Recover(c *gin.Context) {
	actor, runID, ok := currentUserAndRCAID(c)
	if !ok {
		return
	}
	plan, err := h.service.Recover(c.Request.Context(), actor, runID)
	if handleRCAError(c, err, "recover RCA run failed") {
		return
	}
	success(c, plan)
}

func currentUserAndRCAID(c *gin.Context) (*model.AppUser, int64, bool) {
	actor, ok := currentUser(c)
	if !ok {
		return nil, 0, false
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid RCA run id")
		return nil, 0, false
	}
	return actor, id, true
}

func handleRCAError(c *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	recordFailureError(c, err, fallback)
	switch {
	case errors.Is(err, rcasvc.ErrInvalidInput), errors.Is(err, rcasvc.ErrEvidenceRequired),
		errors.Is(err, rcasvc.ErrRoundLimit), errors.Is(err, rcasvc.ErrInvalidTransition):
		failure(c, http.StatusBadRequest, 40001, err.Error())
	case errors.Is(err, rcasvc.ErrForbidden):
		failure(c, http.StatusForbidden, 40301, "RCA access forbidden")
	case errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40401, "RCA run not found")
	default:
		failure(c, http.StatusInternalServerError, 50098, fallback)
	}
	return true
}
