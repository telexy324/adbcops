package handler

import (
	"errors"
	"net/http"
	"strconv"

	"aiops-platform/backend/internal/model"
	qualitysvc "aiops-platform/backend/internal/qualitystandard"
	"aiops-platform/backend/internal/repository"
	"github.com/gin-gonic/gin"
)

type QualityStandardHandler struct{ service *qualitysvc.Service }

func NewQualityStandardHandler(service *qualitysvc.Service) *QualityStandardHandler {
	return &QualityStandardHandler{service: service}
}

func (h *QualityStandardHandler) List(c *gin.Context) {
	standards, err := h.service.List(c.Request.Context())
	if handleQualityStandardError(c, err) {
		return
	}
	success(c, gin.H{"items": standards, "count": len(standards)})
}

func (h *QualityStandardHandler) Create(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request model.KBStructuredQualityStandard
	if c.ShouldBindJSON(&request) != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	standard, err := h.service.Create(c.Request.Context(), actor.ID, &request)
	if handleQualityStandardError(c, err) {
		return
	}
	success(c, standard)
}

func (h *QualityStandardHandler) Get(c *gin.Context) {
	id, ok := qualityID(c, "id")
	if !ok {
		return
	}
	standard, err := h.service.Get(c.Request.Context(), id)
	if handleQualityStandardError(c, err) {
		return
	}
	success(c, standard)
}

func (h *QualityStandardHandler) Update(c *gin.Context) {
	id, ok := qualityID(c, "id")
	if !ok {
		return
	}
	var request model.KBStructuredQualityStandard
	if c.ShouldBindJSON(&request) != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	standard, err := h.service.Update(c.Request.Context(), id, &request)
	if handleQualityStandardError(c, err) {
		return
	}
	success(c, standard)
}

func (h *QualityStandardHandler) Validate(c *gin.Context) {
	id, ok := qualityID(c, "id")
	if !ok {
		return
	}
	result, err := h.service.ValidateStored(c.Request.Context(), id)
	if handleQualityStandardError(c, err) {
		return
	}
	success(c, result)
}

func (h *QualityStandardHandler) Publish(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	id, valid := qualityID(c, "id")
	if !valid {
		return
	}
	standard, err := h.service.Publish(c.Request.Context(), id, actor.ID)
	if handleQualityStandardError(c, err) {
		return
	}
	success(c, standard)
}

func (h *QualityStandardHandler) Deprecate(c *gin.Context) {
	id, ok := qualityID(c, "id")
	if !ok {
		return
	}
	standard, err := h.service.Deprecate(c.Request.Context(), id)
	if handleQualityStandardError(c, err) {
		return
	}
	success(c, standard)
}

func (h *QualityStandardHandler) CreateProfile(c *gin.Context) {
	var request model.KBQualityProfile
	if c.ShouldBindJSON(&request) != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	profile, err := h.service.CreateProfile(c.Request.Context(), &request)
	if handleQualityStandardError(c, err) {
		return
	}
	success(c, profile)
}

func (h *QualityStandardHandler) GetProfile(c *gin.Context) {
	id, ok := qualityID(c, "id")
	if !ok {
		return
	}
	profile, err := h.service.GetProfile(c.Request.Context(), id)
	if handleQualityStandardError(c, err) {
		return
	}
	success(c, profile)
}

func (h *QualityStandardHandler) UpdateProfile(c *gin.Context) {
	id, ok := qualityID(c, "id")
	if !ok {
		return
	}
	var request model.KBQualityProfile
	if c.ShouldBindJSON(&request) != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	profile, err := h.service.UpdateProfile(c.Request.Context(), id, &request)
	if handleQualityStandardError(c, err) {
		return
	}
	success(c, profile)
}

func (h *QualityStandardHandler) CloneProfile(c *gin.Context) {
	id, ok := qualityID(c, "id")
	if !ok {
		return
	}
	var request struct {
		ProfileKey string `json:"profileKey" binding:"required"`
		Name       string `json:"name" binding:"required"`
	}
	if c.ShouldBindJSON(&request) != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	profile, err := h.service.CloneProfile(c.Request.Context(), id, request.ProfileKey, request.Name)
	if handleQualityStandardError(c, err) {
		return
	}
	success(c, profile)
}

func qualityID(c *gin.Context, parameter string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(parameter), 10, 64)
	if err != nil || id <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return 0, false
	}
	return id, true
}

func handleQualityStandardError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	recordFailureError(c, err, "quality standard request failed")
	switch {
	case errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40401, "quality standard not found")
	case errors.Is(err, qualitysvc.ErrPublishedImmutable):
		failure(c, http.StatusConflict, 40921, err.Error())
	case errors.Is(err, qualitysvc.ErrInvalidTransition):
		failure(c, http.StatusConflict, 40922, err.Error())
	case errors.Is(err, qualitysvc.ErrInvalidStandard):
		failure(c, http.StatusUnprocessableEntity, 42221, err.Error())
	default:
		failure(c, http.StatusInternalServerError, 50001, "quality standard request failed")
	}
	return true
}
