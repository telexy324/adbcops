package handler

import (
	"errors"
	"net/http"

	qualityeval "aiops-platform/backend/internal/qualityevaluation"
	"aiops-platform/backend/internal/repository"
	"github.com/gin-gonic/gin"
)

type QualityEvaluationHandler struct{ service *qualityeval.Service }

func NewQualityEvaluationHandler(service *qualityeval.Service) *QualityEvaluationHandler {
	return &QualityEvaluationHandler{service: service}
}

func (h *QualityEvaluationHandler) Create(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request qualityeval.CreateInput
	if c.ShouldBindJSON(&request) != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	evaluation, err := h.service.Create(c.Request.Context(), actor, request)
	if handleQualityEvaluationError(c, err) {
		return
	}
	success(c, evaluation)
}

func (h *QualityEvaluationHandler) Get(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	id, ok := qualityID(c, "id")
	if !ok {
		return
	}
	evaluation, err := h.service.Get(c.Request.Context(), actor, id)
	if handleQualityEvaluationError(c, err) {
		return
	}
	success(c, evaluation)
}

func (h *QualityEvaluationHandler) RuleResults(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	id, ok := qualityID(c, "id")
	if !ok {
		return
	}
	results, err := h.service.RuleResults(c.Request.Context(), actor, id)
	if handleQualityEvaluationError(c, err) {
		return
	}
	success(c, gin.H{"items": results, "count": len(results)})
}

func handleQualityEvaluationError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	recordFailureError(c, err, "quality evaluation request failed")
	switch {
	case errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40431, "quality evaluation resource not found")
	case errors.Is(err, qualityeval.ErrForbidden):
		failure(c, http.StatusForbidden, 40331, err.Error())
	case errors.Is(err, qualityeval.ErrProfileNotPublished), errors.Is(err, qualityeval.ErrDocumentNotParsed):
		failure(c, http.StatusConflict, 40931, err.Error())
	case errors.Is(err, qualityeval.ErrUnsupportedMode), errors.Is(err, qualityeval.ErrInvalidEvaluation):
		failure(c, http.StatusUnprocessableEntity, 42231, err.Error())
	default:
		failure(c, http.StatusInternalServerError, 50001, "quality evaluation request failed")
	}
	return true
}
