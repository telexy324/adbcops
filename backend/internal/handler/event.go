package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	alertsvc "aiops-platform/backend/internal/alert"
	"aiops-platform/backend/internal/repository"
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

func (h *EventHandler) Manual(c *gin.Context) {
	var request alertsvc.NormalizedEventInput
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	event, err := h.alertService.CreateManualEvent(c.Request.Context(), request)
	if handleEventError(c, err, "create event failed") {
		return
	}
	success(c, event)
}

func (h *EventHandler) List(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	from, ok := optionalQueryTime(c, "from")
	if !ok {
		return
	}
	to, ok := optionalQueryTime(c, "to")
	if !ok {
		return
	}
	events, err := h.alertService.ListEvents(c.Request.Context(), alertsvc.EventQuery{
		Limit:         limit,
		SourceType:    c.Query("sourceType"),
		Status:        c.Query("status"),
		Environment:   c.Query("environment"),
		SystemName:    c.Query("systemName"),
		ComponentName: c.Query("componentName"),
		Namespace:     c.Query("namespace"),
		ResourceName:  c.Query("resourceName"),
		From:          from,
		To:            to,
	})
	if handleEventError(c, err, "list events failed") {
		return
	}
	success(c, events)
}

func (h *EventHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid id")
		return
	}
	event, err := h.alertService.GetEvent(c.Request.Context(), id)
	if handleEventError(c, err, "get event failed") {
		return
	}
	success(c, event)
}

func optionalQueryTime(c *gin.Context, key string) (*time.Time, bool) {
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

func handleEventError(c *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	recordFailureError(c, err, fallback)
	switch {
	case errors.Is(err, alertsvc.ErrInvalidInput):
		failure(c, http.StatusBadRequest, 40001, "invalid request")
	case errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40401, "event not found")
	default:
		failure(c, http.StatusInternalServerError, 50092, fallback)
	}
	return true
}
