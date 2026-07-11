package repository

import (
	"context"
	"fmt"
	"time"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DocumentRepository interface {
	CreateDocument(ctx context.Context, document *model.KBDocument) error
	ListDocuments(ctx context.Context, userID *int64) ([]model.KBDocument, error)
	FindDocumentByID(ctx context.Context, id int64) (*model.KBDocument, error)
	ReplaceDocumentChunks(ctx context.Context, documentID int64, chunks []model.KBChunk) error
	ListDocumentChunks(ctx context.Context, documentID int64) ([]model.KBChunk, error)
	UpdateDocumentQuality(ctx context.Context, id int64, score int, result []byte, status string) (*model.KBDocument, error)
	SearchChunks(ctx context.Context, query string, limit int) ([]model.KBChunk, error)
}

func (r *GORMUserRepository) CreateDocument(ctx context.Context, document *model.KBDocument) error {
	if err := r.db.WithContext(ctx).Create(document).Error; err != nil {
		return fmt.Errorf("create kb document: %w", err)
	}
	return nil
}

func (r *GORMUserRepository) ListDocuments(ctx context.Context, userID *int64) ([]model.KBDocument, error) {
	var documents []model.KBDocument
	query := r.db.WithContext(ctx).Order("created_at DESC, id DESC")
	if userID != nil {
		query = query.Where("created_by = ?", *userID)
	}
	if err := query.Find(&documents).Error; err != nil {
		return nil, fmt.Errorf("list kb documents: %w", err)
	}
	return documents, nil
}

func (r *GORMUserRepository) FindDocumentByID(ctx context.Context, id int64) (*model.KBDocument, error) {
	var document model.KBDocument
	if err := r.db.WithContext(ctx).First(&document, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &document, nil
}

func (r *GORMUserRepository) ReplaceDocumentChunks(ctx context.Context, documentID int64, chunks []model.KBChunk) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("document_id = ?", documentID).Delete(&model.KBChunk{}).Error; err != nil {
			return fmt.Errorf("delete old kb chunks: %w", err)
		}
		if len(chunks) == 0 {
			return nil
		}
		if err := tx.Create(&chunks).Error; err != nil {
			return fmt.Errorf("create kb chunks: %w", err)
		}
		return nil
	})
}

func (r *GORMUserRepository) ListDocumentChunks(ctx context.Context, documentID int64) ([]model.KBChunk, error) {
	var chunks []model.KBChunk
	if err := r.db.WithContext(ctx).
		Where("document_id = ?", documentID).
		Order("chunk_index ASC").
		Find(&chunks).Error; err != nil {
		return nil, fmt.Errorf("list kb chunks: %w", err)
	}
	return chunks, nil
}

func (r *GORMUserRepository) UpdateDocumentQuality(ctx context.Context, id int64, score int, result []byte, status string) (*model.KBDocument, error) {
	update := map[string]any{
		"quality_score":  score,
		"quality_result": result,
		"status":         status,
		"updated_at":     time.Now().UTC(),
	}
	dbResult := r.db.WithContext(ctx).Model(&model.KBDocument{}).Where("id = ?", id).Updates(update)
	if dbResult.Error != nil {
		return nil, fmt.Errorf("update document quality: %w", dbResult.Error)
	}
	if dbResult.RowsAffected != 1 {
		return nil, ErrNotFound
	}
	return r.FindDocumentByID(ctx, id)
}

func (r *GORMUserRepository) SearchChunks(ctx context.Context, query string, limit int) ([]model.KBChunk, error) {
	var chunks []model.KBChunk
	if limit <= 0 {
		limit = 10
	}
	pattern := "%" + query + "%"
	if err := r.db.WithContext(ctx).
		Where("search_text ILIKE ? OR content ILIKE ?", pattern, pattern).
		Order(clause.OrderBy{Expression: clause.Expr{SQL: "similarity(coalesce(search_text, ''), ?) DESC, chunk_index ASC", Vars: []any{query}}}).
		Limit(limit).
		Find(&chunks).Error; err != nil {
		return nil, fmt.Errorf("search kb chunks: %w", err)
	}
	return chunks, nil
}
