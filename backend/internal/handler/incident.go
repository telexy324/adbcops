package handler

import (
	"errors"
	"net/http"
	"strconv"

	incidentsvc "aiops-platform/backend/internal/incident"
	"aiops-platform/backend/internal/repository"
	"github.com/gin-gonic/gin"
)

type IncidentHandler struct {
	service *incidentsvc.Service
}

func NewIncidentHandler(service *incidentsvc.Service) *IncidentHandler {
	return &IncidentHandler{service: service}
}

func (h *IncidentHandler) Create(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request incidentsvc.CreateInput
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	detail, err := h.service.Create(c.Request.Context(), actor, request)
	if handleIncidentError(c, err, "create incident failed") {
		return
	}
	success(c, detail)
}

func (h *IncidentHandler) PromoteAnalysis(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request incidentsvc.PromoteInput
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	detail, err := h.service.PromoteAnalysis(c.Request.Context(), actor, request)
	if handleIncidentError(c, err, "promote analysis to incident failed") {
		return
	}
	success(c, detail)
}

func (h *IncidentHandler) Update(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	id, ok := incidentIDParam(c)
	if !ok {
		return
	}
	var request incidentsvc.UpdateInput
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	detail, err := h.service.Update(c.Request.Context(), actor, id, request)
	if handleIncidentError(c, err, "update incident failed") {
		return
	}
	success(c, detail)
}

func (h *IncidentHandler) List(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	incidents, err := h.service.List(c.Request.Context(), actor, incidentsvc.Query{
		Limit:       limit,
		Status:      c.Query("status"),
		Severity:    c.Query("severity"),
		Environment: c.Query("environment"),
		SystemName:  c.Query("systemName"),
	})
	if handleIncidentError(c, err, "list incidents failed") {
		return
	}
	success(c, incidents)
}

func (h *IncidentHandler) Get(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	id, ok := incidentIDParam(c)
	if !ok {
		return
	}
	detail, err := h.service.Get(c.Request.Context(), actor, id)
	if handleIncidentError(c, err, "get incident failed") {
		return
	}
	success(c, detail)
}

func (h *IncidentHandler) Similar(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	id, ok := incidentIDParam(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	result, err := h.service.MatchHistory(c.Request.Context(), actor, id, incidentsvc.MatchQuery{Limit: limit})
	if handleIncidentError(c, err, "match similar incidents failed") {
		return
	}
	success(c, result)
}

func (h *IncidentHandler) ConfirmRootCause(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	id, ok := incidentIDParam(c)
	if !ok {
		return
	}
	candidateID, err := strconv.ParseInt(c.Param("candidateId"), 10, 64)
	if err != nil || candidateID <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid candidate id")
		return
	}
	detail, err := h.service.ConfirmRootCause(c.Request.Context(), actor, id, candidateID)
	if handleIncidentError(c, err, "confirm root cause failed") {
		return
	}
	success(c, detail)
}

func incidentIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid id")
		return 0, false
	}
	return id, true
}

func handleIncidentError(c *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, incidentsvc.ErrInvalidInput):
		failure(c, http.StatusBadRequest, 40001, "invalid request")
	case errors.Is(err, incidentsvc.ErrForbidden):
		failure(c, http.StatusForbidden, 40301, "forbidden")
	case errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40401, "incident not found")
	default:
		failure(c, http.StatusInternalServerError, 50093, fallback)
	}
	return true
}
