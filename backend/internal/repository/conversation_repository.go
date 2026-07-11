package repository

import (
	"context"
	"fmt"
	"time"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
)

type ConversationRepository interface {
	CreateConversation(ctx context.Context, conversation *model.Conversation) error
	ListConversations(ctx context.Context, userID int64) ([]model.Conversation, error)
	FindConversationByID(ctx context.Context, id int64) (*model.Conversation, error)
	SoftDeleteConversation(ctx context.Context, id int64) error
	CreateMessage(ctx context.Context, message *model.Message) error
	ListRecentMessages(ctx context.Context, conversationID int64, limit int) ([]model.Message, error)
}

type GORMConversationRepository struct {
	db *gorm.DB
}

func NewConversationRepository(db *gorm.DB) *GORMConversationRepository {
	return &GORMConversationRepository{db: db}
}

func (r *GORMConversationRepository) CreateConversation(ctx context.Context, conversation *model.Conversation) error {
	if err := r.db.WithContext(ctx).Create(conversation).Error; err != nil {
		return fmt.Errorf("create conversation: %w", err)
	}
	return nil
}

func (r *GORMConversationRepository) ListConversations(ctx context.Context, userID int64) ([]model.Conversation, error) {
	var conversations []model.Conversation
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND status <> ?", userID, model.ConversationStatusDeleted).
		Order("updated_at DESC, id DESC").
		Find(&conversations).Error; err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	return conversations, nil
}

func (r *GORMConversationRepository) FindConversationByID(ctx context.Context, id int64) (*model.Conversation, error) {
	var conversation model.Conversation
	if err := r.db.WithContext(ctx).
		Where("status <> ?", model.ConversationStatusDeleted).
		First(&conversation, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &conversation, nil
}

func (r *GORMConversationRepository) SoftDeleteConversation(ctx context.Context, id int64) error {
	result := r.db.WithContext(ctx).Model(&model.Conversation{}).
		Where("id = ? AND status <> ?", id, model.ConversationStatusDeleted).
		Updates(map[string]any{
			"status":     model.ConversationStatusDeleted,
			"updated_at": time.Now().UTC(),
		})
	if result.Error != nil {
		return fmt.Errorf("delete conversation: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}

func (r *GORMConversationRepository) CreateMessage(ctx context.Context, message *model.Message) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(message).Error; err != nil {
			return fmt.Errorf("create conversation message: %w", err)
		}
		result := tx.Model(&model.Conversation{}).
			Where("id = ? AND status <> ?", message.ConversationID, model.ConversationStatusDeleted).
			Update("updated_at", time.Now().UTC())
		if result.Error != nil {
			return fmt.Errorf("touch conversation: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrNotFound
		}
		return nil
	})
}

func (r *GORMConversationRepository) ListRecentMessages(ctx context.Context, conversationID int64, limit int) ([]model.Message, error) {
	var newest []model.Message
	if err := r.db.WithContext(ctx).
		Where("conversation_id = ?", conversationID).
		Order("created_at DESC, id DESC").
		Limit(limit).
		Find(&newest).Error; err != nil {
		return nil, fmt.Errorf("list recent messages: %w", err)
	}
	messages := make([]model.Message, len(newest))
	for index := range newest {
		messages[len(newest)-1-index] = newest[index]
	}
	return messages, nil
}
