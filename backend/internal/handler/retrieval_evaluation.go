package handler

import (
	"errors"
	"net/http"
	"strconv"

	"aiops-platform/backend/internal/repository"
	retrievaleval "aiops-platform/backend/internal/retrievalevaluation"
	"github.com/gin-gonic/gin"
)

type RetrievalEvaluationHandler struct{ service *retrievaleval.Service }

func NewRetrievalEvaluationHandler(service *retrievaleval.Service) *RetrievalEvaluationHandler {
	return &RetrievalEvaluationHandler{service: service}
}

func (h *RetrievalEvaluationHandler) CreateTestCase(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request retrievaleval.CreateTestCaseInput
	if c.ShouldBindJSON(&request) != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	item, err := h.service.CreateTestCase(c.Request.Context(), actor, request)
	if handleRetrievalEvaluationError(c, err) {
		return
	}
	success(c, item)
}

func (h *RetrievalEvaluationHandler) ListTestCases(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var versionID *int64
	if raw := c.Query("documentVersionId"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			failure(c, http.StatusBadRequest, 40001, "invalid request")
			return
		}
		versionID = &value
	}
	items, err := h.service.ListTestCases(c.Request.Context(), actor, versionID)
	if handleRetrievalEvaluationError(c, err) {
		return
	}
	success(c, gin.H{"items": items, "count": len(items)})
}

func (h *RetrievalEvaluationHandler) Smoke(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request retrievaleval.RunConfig
	if c.ShouldBindJSON(&request) != nil || request.DocumentVersionID == nil || *request.DocumentVersionID <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	run, err := h.service.RunSmoke(c.Request.Context(), actor, *request.DocumentVersionID, request)
	if handleRetrievalEvaluationError(c, err) {
		return
	}
	success(c, run)
}

func (h *RetrievalEvaluationHandler) Lab(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request retrievaleval.LabInput
	if c.ShouldBindJSON(&request) != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	runs, err := h.service.RunLab(c.Request.Context(), actor, request)
	if handleRetrievalEvaluationError(c, err) {
		return
	}
	success(c, gin.H{"runs": runs, "count": len(runs)})
}

func (h *RetrievalEvaluationHandler) GetRun(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	run, serviceErr := h.service.GetRun(c.Request.Context(), actor, id)
	if handleRetrievalEvaluationError(c, serviceErr) {
		return
	}
	success(c, run)
}

func (h *RetrievalEvaluationHandler) ListRuns(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	runs, err := h.service.ListRuns(c.Request.Context(), actor, limit)
	if handleRetrievalEvaluationError(c, err) {
		return
	}
	success(c, gin.H{"items": runs, "count": len(runs)})
}

func handleRetrievalEvaluationError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	recordFailureError(c, err, "retrieval evaluation request failed")
	switch {
	case errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40443, "retrieval evaluation resource not found")
	case errors.Is(err, retrievaleval.ErrInvalidInput):
		failure(c, http.StatusUnprocessableEntity, 42243, err.Error())
	default:
		failure(c, http.StatusInternalServerError, 50043, "retrieval evaluation request failed")
	}
	return true
}
