package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DocumentRepository interface {
	CreateDocument(ctx context.Context, document *model.KBDocument) error
	CreateDocumentWithVersion(ctx context.Context, document *model.KBDocument, version *model.KBDocumentVersion) error
	ListDocuments(ctx context.Context, userID *int64) ([]model.KBDocument, error)
	FindDocumentByID(ctx context.Context, id int64) (*model.KBDocument, error)
	FindDocumentVersionByID(ctx context.Context, id int64) (*model.KBDocumentVersion, error)
	FindLatestDocumentVersion(ctx context.Context, documentID int64) (*model.KBDocumentVersion, error)
	RecordDocumentVersionParse(ctx context.Context, versionID int64, parserName, parserVersion, language string, metadata, documentSchema, parseQuality []byte, status string, blocks []model.KBDocumentBlock) (*model.KBDocumentVersion, error)
	ListDocumentVersionBlocks(ctx context.Context, versionID int64) ([]model.KBDocumentBlock, error)
	ReplaceDocumentChunks(ctx context.Context, documentID int64, chunks []model.KBChunk) error
	ListDocumentChunks(ctx context.Context, documentID int64) ([]model.KBChunk, error)
	UpdateDocumentQuality(ctx context.Context, id int64, score int, result []byte, status string) (*model.KBDocument, error)
	RecordDocumentReview(ctx context.Context, id int64, reviewerID int64, action, toStatus string, comment *string) (*model.KBDocument, error)
	CreateQualityStandard(ctx context.Context, standard *model.KBQualityStandard) error
	ListQualityStandards(ctx context.Context, enabledOnly bool) ([]model.KBQualityStandard, error)
	FindQualityStandardsByIDs(ctx context.Context, ids []int64) ([]model.KBQualityStandard, error)
	SearchChunks(ctx context.Context, query string, limit int) ([]model.KBChunk, error)
	ListPublishedChunkEmbeddings(ctx context.Context, modelName string, limit int) ([]model.KBChunkEmbedding, error)
	ListPublishedChunksMissingEmbedding(ctx context.Context, modelName string, limit int) ([]model.KBChunk, error)
	UpsertChunkEmbeddings(ctx context.Context, embeddings []model.KBChunkEmbedding) error
}

func (r *GORMUserRepository) CreateDocument(ctx context.Context, document *model.KBDocument) error {
	if err := r.db.WithContext(ctx).Create(document).Error; err != nil {
		return fmt.Errorf("create kb document: %w", err)
	}
	return nil
}

func (r *GORMUserRepository) CreateDocumentWithVersion(ctx context.Context, document *model.KBDocument, version *model.KBDocumentVersion) error {
	if document == nil || version == nil {
		return fmt.Errorf("create kb document with version: invalid input")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(document).Error; err != nil {
			return fmt.Errorf("create kb document: %w", err)
		}
		version.DocumentID = document.ID
		if err := tx.Create(version).Error; err != nil {
			return fmt.Errorf("create kb document version: %w", err)
		}
		return nil
	})
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

func (r *GORMUserRepository) FindDocumentVersionByID(ctx context.Context, id int64) (*model.KBDocumentVersion, error) {
	var version model.KBDocumentVersion
	if err := r.db.WithContext(ctx).First(&version, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &version, nil
}

func (r *GORMUserRepository) FindLatestDocumentVersion(ctx context.Context, documentID int64) (*model.KBDocumentVersion, error) {
	var version model.KBDocumentVersion
	if err := r.db.WithContext(ctx).
		Where("document_id = ?", documentID).
		Order("created_at DESC, id DESC").
		First(&version).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &version, nil
}

func (r *GORMUserRepository) RecordDocumentVersionParse(ctx context.Context, versionID int64, parserName, parserVersion, language string, metadata, documentSchema, parseQuality []byte, status string, blocks []model.KBDocumentBlock) (*model.KBDocumentVersion, error) {
	var saved model.KBDocumentVersion
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var base model.KBDocumentVersion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&base, versionID).Error; err != nil {
			return mapRepositoryError(err)
		}
		target := base
		if len(base.ParseQuality) > 0 {
			var maximum int
			if err := tx.Model(&model.KBDocumentVersion{}).
				Where("document_id = ? AND version = ?", base.DocumentID, base.Version).
				Select("COALESCE(MAX(revision_no), 0)").Scan(&maximum).Error; err != nil {
				return fmt.Errorf("find latest document parse revision: %w", err)
			}
			target.ID = 0
			target.RevisionNo = maximum + 1
			target.ParserName = nil
			target.ParserVersion = nil
			target.Language = nil
			target.Metadata = nil
			target.DocumentSchema = nil
			target.ParseQuality = nil
			target.ContentSummary = nil
			target.Status = model.DocumentVersionStatusProcessing
			target.CreatedAt = time.Time{}
			target.UpdatedAt = time.Time{}
			if err := tx.Create(&target).Error; err != nil {
				return fmt.Errorf("create document parse revision: %w", err)
			}
		}
		updates := map[string]any{
			"parser_name": parserName, "parser_version": parserVersion, "language": language,
			"metadata": metadata, "document_schema": documentSchema, "parse_quality": parseQuality, "status": status, "updated_at": time.Now().UTC(),
		}
		if err := tx.Model(&model.KBDocumentVersion{}).Where("id = ?", target.ID).Updates(updates).Error; err != nil {
			return fmt.Errorf("update document parse result: %w", err)
		}
		blockIDs := make(map[string]int64, len(blocks))
		for index := range blocks {
			blocks[index].ID = 0
			blocks[index].DocumentVersionID = target.ID
			if blocks[index].ParentBlockKey != nil {
				parentID, ok := blockIDs[*blocks[index].ParentBlockKey]
				if !ok {
					return fmt.Errorf("create document block %s: parent %s not found", blocks[index].BlockKey, *blocks[index].ParentBlockKey)
				}
				blocks[index].ParentBlockID = &parentID
			}
			if err := tx.Create(&blocks[index]).Error; err != nil {
				return fmt.Errorf("create document block %s: %w", blocks[index].BlockKey, err)
			}
			blockIDs[blocks[index].BlockKey] = blocks[index].ID
		}
		if err := tx.First(&saved, target.ID).Error; err != nil {
			return mapRepositoryError(err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &saved, nil
}

func (r *GORMUserRepository) ListDocumentVersionBlocks(ctx context.Context, versionID int64) ([]model.KBDocumentBlock, error) {
	var blocks []model.KBDocumentBlock
	if err := r.db.WithContext(ctx).
		Where("document_version_id = ?", versionID).
		Order("order_no ASC, id ASC").
		Find(&blocks).Error; err != nil {
		return nil, fmt.Errorf("list document version blocks: %w", err)
	}
	return blocks, nil
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

func (r *GORMUserRepository) RecordDocumentReview(ctx context.Context, id int64, reviewerID int64, action, toStatus string, comment *string) (*model.KBDocument, error) {
	var updated model.KBDocument
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var document model.KBDocument
		if err := tx.First(&document, id).Error; err != nil {
			return mapRepositoryError(err)
		}
		review := &model.KBDocumentReview{
			DocumentID: id,
			ReviewerID: reviewerID,
			Action:     action,
			FromStatus: document.Status,
			ToStatus:   toStatus,
			Comment:    comment,
		}
		now := time.Now().UTC()
		updates := map[string]any{
			"status":      toStatus,
			"reviewed_by": reviewerID,
			"reviewed_at": now,
			"updated_at":  now,
		}
		result := tx.Model(&model.KBDocument{}).Where("id = ?", id).Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("update document review status: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrNotFound
		}
		if err := tx.Create(review).Error; err != nil {
			return fmt.Errorf("create document review: %w", err)
		}
		if err := tx.First(&updated, id).Error; err != nil {
			return mapRepositoryError(err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (r *GORMUserRepository) CreateQualityStandard(ctx context.Context, standard *model.KBQualityStandard) error {
	if err := r.db.WithContext(ctx).Create(standard).Error; err != nil {
		return fmt.Errorf("create kb quality standard: %w", err)
	}
	return nil
}

func (r *GORMUserRepository) ListQualityStandards(ctx context.Context, enabledOnly bool) ([]model.KBQualityStandard, error) {
	var standards []model.KBQualityStandard
	query := r.db.WithContext(ctx).Order("created_at DESC, id DESC")
	if enabledOnly {
		query = query.Where("enabled = ?", true)
	}
	if err := query.Find(&standards).Error; err != nil {
		return nil, fmt.Errorf("list kb quality standards: %w", err)
	}
	return standards, nil
}

func (r *GORMUserRepository) FindQualityStandardsByIDs(ctx context.Context, ids []int64) ([]model.KBQualityStandard, error) {
	var standards []model.KBQualityStandard
	if len(ids) == 0 {
		return standards, nil
	}
	if err := r.db.WithContext(ctx).
		Where("id IN ? AND enabled = ?", ids, true).
		Order("id ASC").
		Find(&standards).Error; err != nil {
		return nil, fmt.Errorf("find kb quality standards: %w", err)
	}
	if len(standards) != len(uniqueInt64(ids)) {
		return nil, ErrNotFound
	}
	return standards, nil
}

func (r *GORMUserRepository) FindDefaultEnabledLLMConfigByPurpose(ctx context.Context, purpose string) (*model.LLMConfig, error) {
	return (&GORMLLMRepository{db: r.db}).FindDefaultEnabledLLMConfigByPurpose(ctx, purpose)
}

func (r *GORMUserRepository) SearchChunks(ctx context.Context, query string, limit int) ([]model.KBChunk, error) {
	var chunks []model.KBChunk
	if limit <= 0 {
		limit = 10
	}
	pattern := "%" + query + "%"
	if err := r.db.WithContext(ctx).
		Joins("JOIN kb_document ON kb_document.id = kb_chunk.document_id").
		Where("kb_document.status = ?", model.DocumentStatusPublished).
		Where("kb_chunk.search_text ILIKE ? OR kb_chunk.content ILIKE ?", pattern, pattern).
		Order(clause.OrderBy{Expression: clause.Expr{SQL: "similarity(coalesce(kb_chunk.search_text, ''), ?) DESC, kb_chunk.chunk_index ASC", Vars: []any{query}}}).
		Limit(limit).
		Find(&chunks).Error; err != nil {
		return nil, fmt.Errorf("search kb chunks: %w", err)
	}
	return chunks, nil
}

func (r *GORMUserRepository) ListPublishedChunks(ctx context.Context, limit int) ([]model.KBChunk, error) {
	var chunks []model.KBChunk
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	if err := r.db.WithContext(ctx).
		Joins("JOIN kb_document ON kb_document.id = kb_chunk.document_id").
		Where("kb_document.status = ?", model.DocumentStatusPublished).
		Order("kb_chunk.id DESC").
		Limit(limit).
		Find(&chunks).Error; err != nil {
		return nil, fmt.Errorf("list published kb chunks: %w", err)
	}
	return chunks, nil
}

func (r *GORMUserRepository) ListPublishedChunkEmbeddings(ctx context.Context, modelName string, limit int) ([]model.KBChunkEmbedding, error) {
	var embeddings []model.KBChunkEmbedding
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	if err := r.db.WithContext(ctx).
		Model(&model.KBChunkEmbedding{}).
		Joins("JOIN kb_chunk ON kb_chunk.id = kb_chunk_embedding.chunk_id").
		Joins("JOIN kb_document ON kb_document.id = kb_chunk.document_id").
		Where("kb_document.status = ?", model.DocumentStatusPublished).
		Where("kb_chunk_embedding.model = ?", modelName).
		Preload("Chunk").
		Order("kb_chunk_embedding.updated_at DESC, kb_chunk_embedding.id DESC").
		Limit(limit).
		Find(&embeddings).Error; err != nil {
		return nil, fmt.Errorf("list published kb chunk embeddings: %w", err)
	}
	return embeddings, nil
}

func (r *GORMUserRepository) ListPublishedChunksMissingEmbedding(ctx context.Context, modelName string, limit int) ([]model.KBChunk, error) {
	var chunks []model.KBChunk
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if err := r.db.WithContext(ctx).
		Model(&model.KBChunk{}).
		Joins("JOIN kb_document ON kb_document.id = kb_chunk.document_id").
		Where("kb_document.status = ?", model.DocumentStatusPublished).
		Where("NOT EXISTS (?)",
			r.db.Model(&model.KBChunkEmbedding{}).
				Select("1").
				Where("kb_chunk_embedding.chunk_id = kb_chunk.id").
				Where("kb_chunk_embedding.model = ?", modelName),
		).
		Order("kb_chunk.id ASC").
		Limit(limit).
		Find(&chunks).Error; err != nil {
		return nil, fmt.Errorf("list published kb chunks missing embedding: %w", err)
	}
	return chunks, nil
}

func (r *GORMUserRepository) UpsertChunkEmbeddings(ctx context.Context, embeddings []model.KBChunkEmbedding) error {
	if len(embeddings) == 0 {
		return nil
	}
	now := time.Now().UTC()
	for index := range embeddings {
		embeddings[index].UpdatedAt = now
		if embeddings[index].CreatedAt.IsZero() {
			embeddings[index].CreatedAt = now
		}
		if !json.Valid(embeddings[index].Embedding) {
			return fmt.Errorf("upsert kb chunk embeddings: invalid embedding json for chunk %d", embeddings[index].ChunkID)
		}
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "chunk_id"}, {Name: "model"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"llm_config_id",
			"dimension",
			"embedding",
			"updated_at",
		}),
	}).Create(&embeddings).Error; err != nil {
		return fmt.Errorf("upsert kb chunk embeddings: %w", err)
	}
	return nil
}

func uniqueInt64(values []int64) []int64 {
	seen := map[int64]struct{}{}
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
