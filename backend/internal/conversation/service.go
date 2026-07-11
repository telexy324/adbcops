package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

const (
	recentRoundMessageLimit = 16
	maxTitleBytes           = 255
	maxContentBytes         = 262144
)

var (
	ErrForbidden    = errors.New("conversation access forbidden")
	ErrInvalidInput = errors.New("invalid input")
)

type Repository interface {
	CreateConversation(ctx context.Context, conversation *model.Conversation) error
	ListConversations(ctx context.Context, userID int64) ([]model.Conversation, error)
	FindConversationByID(ctx context.Context, id int64) (*model.Conversation, error)
	SoftDeleteConversation(ctx context.Context, id int64) error
	CreateMessage(ctx context.Context, message *model.Message) error
	ListRecentMessages(ctx context.Context, conversationID int64, limit int) ([]model.Message, error)
}

type Service struct {
	conversations Repository
}

type CreateInput struct {
	Title           *string
	ContextSnapshot json.RawMessage
}

type MessageInput struct {
	Role      string
	Content   string
	Citations json.RawMessage
	Metadata  json.RawMessage
}

type Detail struct {
	Conversation   *model.Conversation
	RecentMessages []model.Message
}

func NewService(conversations Repository) *Service {
	return &Service{conversations: conversations}
}

func (s *Service) List(ctx context.Context, actor *model.AppUser, requestedUserID *int64) ([]model.Conversation, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	userID := actor.ID
	if requestedUserID != nil {
		if actor.Role != model.RoleAdmin {
			return nil, ErrForbidden
		}
		if *requestedUserID <= 0 {
			return nil, ErrInvalidInput
		}
		userID = *requestedUserID
	}
	return s.conversations.ListConversations(ctx, userID)
}

func (s *Service) Create(ctx context.Context, actor *model.AppUser, input CreateInput) (*model.Conversation, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	title, err := normalizeTitle(input.Title)
	if err != nil {
		return nil, err
	}
	contextSnapshot, err := normalizeJSON(input.ContextSnapshot)
	if err != nil {
		return nil, err
	}
	created := &model.Conversation{
		UserID:          actor.ID,
		Title:           title,
		Status:          model.ConversationStatusActive,
		ContextSnapshot: contextSnapshot,
	}
	if err := s.conversations.CreateConversation(ctx, created); err != nil {
		return nil, fmt.Errorf("create conversation: %w", err)
	}
	return created, nil
}

func (s *Service) Get(ctx context.Context, actor *model.AppUser, conversationID int64) (*Detail, error) {
	conversation, err := s.findAccessibleConversation(ctx, actor, conversationID)
	if err != nil {
		return nil, err
	}
	messages, err := s.conversations.ListRecentMessages(ctx, conversation.ID, recentRoundMessageLimit)
	if err != nil {
		return nil, fmt.Errorf("load recent messages: %w", err)
	}
	return &Detail{Conversation: conversation, RecentMessages: messages}, nil
}

func (s *Service) Delete(ctx context.Context, actor *model.AppUser, conversationID int64) error {
	conversation, err := s.findAccessibleConversation(ctx, actor, conversationID)
	if err != nil {
		return err
	}
	if err := s.conversations.SoftDeleteConversation(ctx, conversation.ID); err != nil {
		return fmt.Errorf("delete conversation: %w", err)
	}
	return nil
}

func (s *Service) AddMessage(ctx context.Context, actor *model.AppUser, conversationID int64, input MessageInput) (*model.Message, []model.Message, error) {
	conversation, err := s.findAccessibleConversation(ctx, actor, conversationID)
	if err != nil {
		return nil, nil, err
	}
	role := strings.TrimSpace(input.Role)
	if role == "" {
		role = model.MessageRoleUser
	}
	if err := validateMessageRole(role); err != nil {
		return nil, nil, err
	}
	content := strings.TrimSpace(input.Content)
	if content == "" || len(content) > maxContentBytes || !utf8.ValidString(content) {
		return nil, nil, ErrInvalidInput
	}
	citations, err := normalizeJSON(input.Citations)
	if err != nil {
		return nil, nil, err
	}
	metadata, err := normalizeJSON(input.Metadata)
	if err != nil {
		return nil, nil, err
	}
	message := &model.Message{
		ConversationID: conversation.ID,
		Role:           role,
		Content:        content,
		Citations:      citations,
		Metadata:       metadata,
	}
	if err := s.conversations.CreateMessage(ctx, message); err != nil {
		return nil, nil, fmt.Errorf("create message: %w", err)
	}
	recent, err := s.conversations.ListRecentMessages(ctx, conversation.ID, recentRoundMessageLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("load recent messages: %w", err)
	}
	return message, recent, nil
}

func (s *Service) Summary(ctx context.Context, actor *model.AppUser, conversationID int64) (*model.Conversation, error) {
	return s.findAccessibleConversation(ctx, actor, conversationID)
}

func (s *Service) findAccessibleConversation(ctx context.Context, actor *model.AppUser, conversationID int64) (*model.Conversation, error) {
	if actor == nil || conversationID <= 0 {
		return nil, ErrInvalidInput
	}
	conversation, err := s.conversations.FindConversationByID(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if actor.Role != model.RoleAdmin && conversation.UserID != actor.ID {
		return nil, ErrForbidden
	}
	return conversation, nil
}

func normalizeTitle(title *string) (*string, error) {
	if title == nil {
		return nil, nil
	}
	normalized := strings.TrimSpace(*title)
	if normalized == "" {
		return nil, nil
	}
	if len(normalized) > maxTitleBytes || !utf8.ValidString(normalized) {
		return nil, ErrInvalidInput
	}
	return &normalized, nil
}

func normalizeJSON(value json.RawMessage) ([]byte, error) {
	if len(value) == 0 || string(value) == "null" {
		return nil, nil
	}
	if !json.Valid(value) {
		return nil, ErrInvalidInput
	}
	normalized := make([]byte, len(value))
	copy(normalized, value)
	return normalized, nil
}

func validateMessageRole(role string) error {
	switch role {
	case model.MessageRoleUser, model.MessageRoleAssistant, model.MessageRoleSystem, model.MessageRoleTool:
		return nil
	default:
		return ErrInvalidInput
	}
}

func IsNotFound(err error) bool {
	return errors.Is(err, repository.ErrNotFound)
}
