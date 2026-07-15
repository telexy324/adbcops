package handler

import (
	"errors"
	"net/http"

	ragsvc "aiops-platform/backend/internal/rag"
	"aiops-platform/backend/internal/repository"
	"github.com/gin-gonic/gin"
)

type RAGHandler struct {
	service *ragsvc.Service
}

func NewRAGHandler(service *ragsvc.Service) *RAGHandler {
	return &RAGHandler{service: service}
}

type knowledgeAskRequest struct {
	ConversationID *int64 `json:"conversationId"`
	Question       string `json:"question" binding:"required"`
	Limit          int    `json:"limit"`
}

func (h *RAGHandler) Ask(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request knowledgeAskRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	result, err := h.service.Ask(c.Request.Context(), actor, ragsvc.AskInput{
		ConversationID: request.ConversationID,
		Question:       request.Question,
		Limit:          request.Limit,
	})
	if handleRAGError(c, err, "ask knowledge failed") {
		return
	}
	success(c, gin.H{
		"conversation":   toConversationResponse(result.Conversation),
		"userMessage":    toMessageResponse(result.UserMessage),
		"message":        toMessageResponse(result.Message),
		"qaRecordId":     result.QARecord.ID,
		"question":       result.Question,
		"rewrittenQuery": result.Rewritten,
		"answer":         result.Answer,
		"citations":      result.Citations,
		"recallCount":    result.RecallCount,
	})
}

func handleRAGError(c *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	recordFailureError(c, err, fallback)
	switch {
	case errors.Is(err, ragsvc.ErrInvalidInput):
		failure(c, http.StatusBadRequest, 40001, "invalid request")
	case errors.Is(err, ragsvc.ErrForbidden):
		failure(c, http.StatusForbidden, 40305, "knowledge conversation access forbidden")
	case errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40401, "record not found")
	default:
		failure(c, http.StatusInternalServerError, 50050, fallback)
	}
	return true
}
