package handler

import (
	"errors"
	"net/http"
	"strconv"

	"aiops-platform/backend/internal/repository"
	timelinesvc "aiops-platform/backend/internal/timeline"
	"github.com/gin-gonic/gin"
)

type TimelineHandler struct {
	service *timelinesvc.Service
}

func NewTimelineHandler(service *timelinesvc.Service) *TimelineHandler {
	return &TimelineHandler{service: service}
}

func (h *TimelineHandler) Build(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	anchorEventID, _ := strconv.ParseInt(c.Query("anchorEventId"), 10, 64)
	beforeMinutes, _ := strconv.Atoi(c.Query("beforeMinutes"))
	afterMinutes, _ := strconv.Atoi(c.Query("afterMinutes"))
	maxEvidence, _ := strconv.Atoi(c.Query("maxEvidencePerEvent"))
	from, ok := optionalQueryTime(c, "from")
	if !ok {
		return
	}
	to, ok := optionalQueryTime(c, "to")
	if !ok {
		return
	}
	result, err := h.service.Build(c.Request.Context(), timelinesvc.Query{
		Limit:               limit,
		SourceType:          c.Query("sourceType"),
		Environment:         c.Query("environment"),
		SystemName:          c.Query("systemName"),
		ComponentName:       c.Query("componentName"),
		Namespace:           c.Query("namespace"),
		ResourceName:        c.Query("resourceName"),
		From:                from,
		To:                  to,
		AnchorEventID:       anchorEventID,
		BeforeMinutes:       beforeMinutes,
		AfterMinutes:        afterMinutes,
		IncludeEvidence:     c.Query("includeEvidence") == "true" || c.Query("includeEvidence") == "1",
		MaxEvidencePerEvent: maxEvidence,
	})
	if handleTimelineError(c, err, "build timeline failed") {
		return
	}
	success(c, result)
}

func handleTimelineError(c *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, timelinesvc.ErrInvalidInput):
		failure(c, http.StatusBadRequest, 40001, "invalid request")
	case errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40401, "event not found")
	default:
		failure(c, http.StatusInternalServerError, 50095, fallback)
	}
	return true
}
