package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	conversationsvc "aiops-platform/backend/internal/conversation"
	appmiddleware "aiops-platform/backend/internal/middleware"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
	"github.com/gin-gonic/gin"
)

type ConversationHandler struct {
	service *conversationsvc.Service
}

func NewConversationHandler(service *conversationsvc.Service) *ConversationHandler {
	return &ConversationHandler{service: service}
}

type createConversationRequest struct {
	Title           *string         `json:"title"`
	ContextSnapshot json.RawMessage `json:"contextSnapshot"`
}

type createMessageRequest struct {
	Role      string          `json:"role"`
	Content   string          `json:"content" binding:"required"`
	Citations json.RawMessage `json:"citations"`
	Metadata  json.RawMessage `json:"metadata"`
}

type conversationResponse struct {
	ID                  int64           `json:"id"`
	UserID              int64           `json:"userId"`
	Title               *string         `json:"title,omitempty"`
	Status              string          `json:"status"`
	ConversationSummary *string         `json:"conversationSummary,omitempty"`
	ContextSnapshot     json.RawMessage `json:"contextSnapshot,omitempty"`
	CreatedAt           string          `json:"createdAt"`
	UpdatedAt           string          `json:"updatedAt"`
}

type messageResponse struct {
	ID             int64           `json:"id"`
	ConversationID int64           `json:"conversationId"`
	Role           string          `json:"role"`
	Content        string          `json:"content"`
	Citations      json.RawMessage `json:"citations,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
	CreatedAt      string          `json:"createdAt"`
}

func (h *ConversationHandler) List(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	requestedUserID, ok := parseOptionalUserID(c)
	if !ok {
		return
	}
	conversations, err := h.service.List(c.Request.Context(), actor, requestedUserID)
	if handleConversationError(c, err, "list conversations failed") {
		return
	}
	response := make([]conversationResponse, 0, len(conversations))
	for index := range conversations {
		response = append(response, toConversationResponse(&conversations[index]))
	}
	success(c, response)
}

func (h *ConversationHandler) Create(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request createConversationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	created, err := h.service.Create(c.Request.Context(), actor, conversationsvc.CreateInput{
		Title:           request.Title,
		ContextSnapshot: request.ContextSnapshot,
	})
	if handleConversationError(c, err, "create conversation failed") {
		return
	}
	success(c, toConversationResponse(created))
}

func (h *ConversationHandler) Get(c *gin.Context) {
	actor, conversationID, ok := currentUserAndConversationID(c)
	if !ok {
		return
	}
	detail, err := h.service.Get(c.Request.Context(), actor, conversationID)
	if handleConversationError(c, err, "get conversation failed") {
		return
	}
	success(c, gin.H{
		"conversation":   toConversationResponse(detail.Conversation),
		"recentMessages": toMessageResponses(detail.RecentMessages),
	})
}

func (h *ConversationHandler) Delete(c *gin.Context) {
	actor, conversationID, ok := currentUserAndConversationID(c)
	if !ok {
		return
	}
	err := h.service.Delete(c.Request.Context(), actor, conversationID)
	if handleConversationError(c, err, "delete conversation failed") {
		return
	}
	success(c, gin.H{"deleted": true})
}

func (h *ConversationHandler) AddMessage(c *gin.Context) {
	actor, conversationID, ok := currentUserAndConversationID(c)
	if !ok {
		return
	}
	var request createMessageRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	message, recent, err := h.service.AddMessage(c.Request.Context(), actor, conversationID, conversationsvc.MessageInput{
		Role:      request.Role,
		Content:   request.Content,
		Citations: request.Citations,
		Metadata:  request.Metadata,
	})
	if handleConversationError(c, err, "create conversation message failed") {
		return
	}
	success(c, gin.H{
		"message":        toMessageResponse(message),
		"recentMessages": toMessageResponses(recent),
	})
}

func (h *ConversationHandler) Summary(c *gin.Context) {
	actor, conversationID, ok := currentUserAndConversationID(c)
	if !ok {
		return
	}
	conversation, err := h.service.Summary(c.Request.Context(), actor, conversationID)
	if handleConversationError(c, err, "get conversation summary failed") {
		return
	}
	success(c, gin.H{
		"conversationId":       conversation.ID,
		"conversationSummary":  conversation.ConversationSummary,
		"contextSnapshot":      rawJSON(conversation.ContextSnapshot),
		"summaryUpdatePending": false,
	})
}

func currentUser(c *gin.Context) (*model.AppUser, bool) {
	user, ok := appmiddleware.AuthenticatedUser(c)
	if !ok {
		failure(c, http.StatusUnauthorized, 40101, "authentication required")
		return nil, false
	}
	return user, true
}

func currentUserAndConversationID(c *gin.Context) (*model.AppUser, int64, bool) {
	user, ok := currentUser(c)
	if !ok {
		return nil, 0, false
	}
	conversationID, ok := parseConversationID(c)
	return user, conversationID, ok
}

func parseConversationID(c *gin.Context) (int64, bool) {
	conversationID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || conversationID <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return 0, false
	}
	return conversationID, true
}

func parseOptionalUserID(c *gin.Context) (*int64, bool) {
	value := c.Query("userId")
	if value == "" {
		return nil, true
	}
	userID, err := strconv.ParseInt(value, 10, 64)
	if err != nil || userID <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return nil, false
	}
	return &userID, true
}

func handleConversationError(c *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	recordFailureError(c, err, fallback)
	switch {
	case errors.Is(err, conversationsvc.ErrInvalidInput):
		failure(c, http.StatusBadRequest, 40001, "invalid request")
	case errors.Is(err, conversationsvc.ErrForbidden):
		failure(c, http.StatusForbidden, 40302, "conversation access forbidden")
	case errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40401, "conversation not found")
	default:
		failure(c, http.StatusInternalServerError, 50020, fallback)
	}
	return true
}

func toConversationResponse(conversation *model.Conversation) conversationResponse {
	return conversationResponse{
		ID:                  conversation.ID,
		UserID:              conversation.UserID,
		Title:               conversation.Title,
		Status:              conversation.Status,
		ConversationSummary: conversation.ConversationSummary,
		ContextSnapshot:     rawJSON(conversation.ContextSnapshot),
		CreatedAt:           conversation.CreatedAt.Format(http.TimeFormat),
		UpdatedAt:           conversation.UpdatedAt.Format(http.TimeFormat),
	}
}

func toMessageResponses(messages []model.Message) []messageResponse {
	response := make([]messageResponse, 0, len(messages))
	for index := range messages {
		response = append(response, toMessageResponse(&messages[index]))
	}
	return response
}

func toMessageResponse(message *model.Message) messageResponse {
	return messageResponse{
		ID:             message.ID,
		ConversationID: message.ConversationID,
		Role:           message.Role,
		Content:        message.Content,
		Citations:      rawJSON(message.Citations),
		Metadata:       rawJSON(message.Metadata),
		CreatedAt:      message.CreatedAt.Format(http.TimeFormat),
	}
}

func rawJSON(value []byte) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	return json.RawMessage(value)
}
