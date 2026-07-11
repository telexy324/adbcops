package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	evidencesvc "aiops-platform/backend/internal/evidence"
	"aiops-platform/backend/internal/repository"
	"github.com/gin-gonic/gin"
)

type EvidenceHandler struct {
	service *evidencesvc.Service
}

func NewEvidenceHandler(service *evidencesvc.Service) *EvidenceHandler {
	return &EvidenceHandler{service: service}
}

func (h *EvidenceHandler) Create(c *gin.Context) {
	var request evidencesvc.CreateInput
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	record, err := h.service.Create(c.Request.Context(), request)
	if handleEvidenceError(c, err, "create evidence failed") {
		return
	}
	success(c, record)
}

func (h *EvidenceHandler) List(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	from, ok := optionalEvidenceQueryTime(c, "from")
	if !ok {
		return
	}
	to, ok := optionalEvidenceQueryTime(c, "to")
	if !ok {
		return
	}
	result, err := h.service.List(c.Request.Context(), evidencesvc.Query{
		Limit:       limit,
		SourceType:  c.Query("sourceType"),
		Sensitivity: c.Query("sensitivity"),
		From:        from,
		To:          to,
	})
	if handleEvidenceError(c, err, "list evidence failed") {
		return
	}
	success(c, result)
}

func (h *EvidenceHandler) Get(c *gin.Context) {
	value := c.Param("idOrKey")
	if id, err := strconv.ParseInt(value, 10, 64); err == nil && id > 0 {
		record, err := h.service.GetByID(c.Request.Context(), id)
		if handleEvidenceError(c, err, "get evidence failed") {
			return
		}
		success(c, record)
		return
	}
	record, err := h.service.GetByKey(c.Request.Context(), value)
	if handleEvidenceError(c, err, "get evidence failed") {
		return
	}
	success(c, record)
}

func (h *EvidenceHandler) Validate(c *gin.Context) {
	var request struct {
		Keys []string `json:"keys"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	err := h.service.ValidateReferences(c.Request.Context(), request.Keys)
	if err != nil {
		if errors.Is(err, evidencesvc.ErrEvidenceRefMissing) {
			failure(c, http.StatusBadRequest, 40002, err.Error())
			return
		}
		if handleEvidenceError(c, err, "validate evidence failed") {
			return
		}
	}
	success(c, gin.H{"valid": true})
}

func optionalEvidenceQueryTime(c *gin.Context, key string) (*time.Time, bool) {
	value := c.Query(key)
	if value == "" {
		return nil, true
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return &parsed, true
		}
	}
	failure(c, http.StatusBadRequest, 40001, "invalid "+key)
	return nil, false
}

func handleEvidenceError(c *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, evidencesvc.ErrInvalidInput):
		failure(c, http.StatusBadRequest, 40001, "invalid request")
	case errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40401, "evidence not found")
	default:
		var syntaxError *json.SyntaxError
		if errors.As(err, &syntaxError) {
			failure(c, http.StatusBadRequest, 40001, "invalid request")
			return true
		}
		failure(c, http.StatusInternalServerError, 50097, fallback)
	}
	return true
}
