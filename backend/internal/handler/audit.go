package handler

import (
	"net/http"
	"strconv"

	"aiops-platform/backend/internal/repository"
	"github.com/gin-gonic/gin"
)

type AuditHandler struct {
	repository repository.AuditLogRepository
}

func NewAuditHandler(repository repository.AuditLogRepository) *AuditHandler {
	return &AuditHandler{repository: repository}
}

func (h *AuditHandler) List(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	var userID *int64
	if raw := c.Query("userId"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			failure(c, http.StatusBadRequest, 40001, "invalid userId")
			return
		}
		userID = &parsed
	}
	logs, err := h.repository.ListAuditLogs(c.Request.Context(), repository.AuditLogFilters{
		Limit:     limit,
		RequestID: c.Query("requestId"),
		UserID:    userID,
		Action:    c.Query("action"),
		Resource:  c.Query("resource"),
	})
	if err != nil {
		failure(c, http.StatusInternalServerError, 50099, "list audit logs failed")
		return
	}
	success(c, logs)
}
