package repository

import (
	"context"
	"fmt"

	"aiops-platform/backend/internal/model"
)

type DocumentRepository interface {
	CreateDocument(ctx context.Context, document *model.KBDocument) error
	ListDocuments(ctx context.Context, userID *int64) ([]model.KBDocument, error)
	FindDocumentByID(ctx context.Context, id int64) (*model.KBDocument, error)
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
