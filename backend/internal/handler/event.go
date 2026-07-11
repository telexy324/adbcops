package handler

import (
	"errors"
	"net/http"

	alertsvc "aiops-platform/backend/internal/alert"
	"github.com/gin-gonic/gin"
)

type EventHandler struct {
	alertService *alertsvc.Service
}

func NewEventHandler(alertService *alertsvc.Service) *EventHandler {
	return &EventHandler{alertService: alertService}
}

func (h *EventHandler) Alertmanager(c *gin.Context) {
	var request alertsvc.AlertmanagerWebhook
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	result, err := h.alertService.ReceiveAlertmanager(c.Request.Context(), request)
	if err != nil {
		if errors.Is(err, alertsvc.ErrInvalidInput) {
			failure(c, http.StatusBadRequest, 40001, "invalid request")
			return
		}
		failure(c, http.StatusInternalServerError, 50092, "receive alertmanager webhook failed")
		return
	}
	success(c, result)
}
