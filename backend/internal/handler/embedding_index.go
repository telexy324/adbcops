package handler

import (
	"errors"
	"net/http"
	"strconv"

	embeddingsvc "aiops-platform/backend/internal/embeddingindex"
	"aiops-platform/backend/internal/repository"
	"github.com/gin-gonic/gin"
)

type EmbeddingIndexHandler struct{ service *embeddingsvc.Service }

func NewEmbeddingIndexHandler(service *embeddingsvc.Service) *EmbeddingIndexHandler {
	return &EmbeddingIndexHandler{service: service}
}

func (h *EmbeddingIndexHandler) Create(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request embeddingsvc.CreateInput
	if c.ShouldBindJSON(&request) != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	index, err := h.service.Create(c.Request.Context(), actor, request)
	if handleEmbeddingIndexError(c, err) {
		return
	}
	success(c, index)
}

func (h *EmbeddingIndexHandler) Get(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	id, ok := embeddingIndexID(c)
	if !ok {
		return
	}
	index, err := h.service.Get(c.Request.Context(), actor, id)
	if handleEmbeddingIndexError(c, err) {
		return
	}
	success(c, index)
}

func (h *EmbeddingIndexHandler) Build(c *gin.Context) {
	h.runBuildOperation(c, false)
}

func (h *EmbeddingIndexHandler) Retry(c *gin.Context) {
	h.runBuildOperation(c, true)
}

func (h *EmbeddingIndexHandler) Rebuild(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request struct {
		IndexID   int64 `json:"indexId"`
		BatchSize int   `json:"batchSize"`
	}
	if c.ShouldBindJSON(&request) != nil || request.IndexID <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	index, err := h.service.Rebuild(c.Request.Context(), actor, request.IndexID, embeddingsvc.BuildInput{BatchSize: request.BatchSize})
	if handleEmbeddingIndexError(c, err) {
		return
	}
	success(c, index)
}

func (h *EmbeddingIndexHandler) Status(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	versionID, versionErr := strconv.ParseInt(c.Query("documentVersionId"), 10, 64)
	strategyID, strategyErr := strconv.ParseInt(c.Query("strategyId"), 10, 64)
	if versionErr != nil || strategyErr != nil || versionID <= 0 || strategyID <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	status, err := h.service.Status(c.Request.Context(), actor, versionID, strategyID)
	if handleEmbeddingIndexError(c, err) {
		return
	}
	success(c, status)
}

func (h *EmbeddingIndexHandler) runBuildOperation(c *gin.Context, retry bool) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	id, ok := embeddingIndexID(c)
	if !ok {
		return
	}
	var request embeddingsvc.BuildInput
	if c.Request.ContentLength != 0 && c.ShouldBindJSON(&request) != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	var index any
	var err error
	if retry {
		index, err = h.service.Retry(c.Request.Context(), actor, id, request)
	} else {
		index, err = h.service.Build(c.Request.Context(), actor, id, request)
	}
	if handleEmbeddingIndexError(c, err) {
		return
	}
	success(c, index)
}

func embeddingIndexID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return 0, false
	}
	return id, true
}

func handleEmbeddingIndexError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	recordFailureError(c, err, "embedding index request failed")
	switch {
	case errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40441, "embedding index resource not found")
	case errors.Is(err, embeddingsvc.ErrForbidden):
		failure(c, http.StatusForbidden, 40341, err.Error())
	case errors.Is(err, embeddingsvc.ErrInvalidState):
		failure(c, http.StatusConflict, 40941, err.Error())
	case errors.Is(err, embeddingsvc.ErrInvalidInput), errors.Is(err, embeddingsvc.ErrDimensionMismatch):
		failure(c, http.StatusUnprocessableEntity, 42241, err.Error())
	default:
		failure(c, http.StatusInternalServerError, 50041, "embedding index request failed")
	}
	return true
}
