package repository

import (
	"context"
	"fmt"
	"time"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
)

type RAGRepository interface {
	CreateConversation(ctx context.Context, conversation *model.Conversation) error
	FindConversationByID(ctx context.Context, id int64) (*model.Conversation, error)
	CreateMessage(ctx context.Context, message *model.Message) error
	SearchChunks(ctx context.Context, query string, limit int) ([]model.KBChunk, error)
	ListPublishedChunks(ctx context.Context, limit int) ([]model.KBChunk, error)
	ListPublishedChunkEmbeddings(ctx context.Context, modelName string, limit int) ([]model.KBChunkEmbedding, error)
	ListPublishedChunksMissingEmbedding(ctx context.Context, modelName string, limit int) ([]model.KBChunk, error)
	UpsertChunkEmbeddings(ctx context.Context, embeddings []model.KBChunkEmbedding) error
	FindDefaultEnabledLLMConfig(ctx context.Context) (*model.LLMConfig, error)
	FindDefaultEnabledLLMConfigByPurpose(ctx context.Context, purpose string) (*model.LLMConfig, error)
	CreateQARecord(ctx context.Context, record *model.QARecord) error
}

type GORMRAGRepository struct {
	db *gorm.DB
}

func NewRAGRepository(db *gorm.DB) *GORMRAGRepository {
	return &GORMRAGRepository{db: db}
}

func (r *GORMRAGRepository) CreateConversation(ctx context.Context, conversation *model.Conversation) error {
	if err := r.db.WithContext(ctx).Create(conversation).Error; err != nil {
		return fmt.Errorf("create conversation: %w", err)
	}
	return nil
}

func (r *GORMRAGRepository) FindConversationByID(ctx context.Context, id int64) (*model.Conversation, error) {
	var conversation model.Conversation
	if err := r.db.WithContext(ctx).
		Where("status <> ?", model.ConversationStatusDeleted).
		First(&conversation, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &conversation, nil
}

func (r *GORMRAGRepository) CreateMessage(ctx context.Context, message *model.Message) error {
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

func (r *GORMRAGRepository) SearchChunks(ctx context.Context, query string, limit int) ([]model.KBChunk, error) {
	return (&GORMUserRepository{db: r.db}).SearchChunks(ctx, query, limit)
}

func (r *GORMRAGRepository) ListPublishedChunks(ctx context.Context, limit int) ([]model.KBChunk, error) {
	return (&GORMUserRepository{db: r.db}).ListPublishedChunks(ctx, limit)
}

func (r *GORMRAGRepository) ListPublishedChunkEmbeddings(ctx context.Context, modelName string, limit int) ([]model.KBChunkEmbedding, error) {
	return (&GORMUserRepository{db: r.db}).ListPublishedChunkEmbeddings(ctx, modelName, limit)
}

func (r *GORMRAGRepository) ListPublishedChunksMissingEmbedding(ctx context.Context, modelName string, limit int) ([]model.KBChunk, error) {
	return (&GORMUserRepository{db: r.db}).ListPublishedChunksMissingEmbedding(ctx, modelName, limit)
}

func (r *GORMRAGRepository) UpsertChunkEmbeddings(ctx context.Context, embeddings []model.KBChunkEmbedding) error {
	return (&GORMUserRepository{db: r.db}).UpsertChunkEmbeddings(ctx, embeddings)
}

func (r *GORMRAGRepository) FindDefaultEnabledLLMConfig(ctx context.Context) (*model.LLMConfig, error) {
	return r.FindDefaultEnabledLLMConfigByPurpose(ctx, model.LLMPurposeChat)
}

func (r *GORMRAGRepository) FindDefaultEnabledLLMConfigByPurpose(ctx context.Context, purpose string) (*model.LLMConfig, error) {
	return (&GORMLLMRepository{db: r.db}).FindDefaultEnabledLLMConfigByPurpose(ctx, purpose)
}

func (r *GORMRAGRepository) CreateQARecord(ctx context.Context, record *model.QARecord) error {
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return fmt.Errorf("create qa record: %w", err)
	}
	return nil
}
