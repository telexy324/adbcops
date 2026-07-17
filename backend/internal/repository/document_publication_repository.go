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

func (r *GORMUserRepository) ListDocumentVersions(ctx context.Context, documentID int64) ([]model.KBDocumentVersion, error) {
	var versions []model.KBDocumentVersion
	if err := r.db.WithContext(ctx).Where("document_id = ?", documentID).
		Order("created_at DESC, id DESC").Find(&versions).Error; err != nil {
		return nil, fmt.Errorf("list document versions: %w", err)
	}
	return versions, nil
}

func (r *GORMUserRepository) CreateDocumentVersion(ctx context.Context, version *model.KBDocumentVersion) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var document model.KBDocument
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&document, version.DocumentID).Error; err != nil {
			return mapRepositoryError(err)
		}
		var maximum int
		if err := tx.Model(&model.KBDocumentVersion{}).
			Where("document_id = ? AND version = ?", version.DocumentID, version.Version).
			Select("COALESCE(MAX(revision_no), 0)").Scan(&maximum).Error; err != nil {
			return fmt.Errorf("find next document revision: %w", err)
		}
		version.RevisionNo = maximum + 1
		if err := tx.Create(version).Error; err != nil {
			return fmt.Errorf("create document version: %w", err)
		}
		return nil
	})
}

func (r *GORMUserRepository) FindPublicationQualityEvaluation(ctx context.Context, versionID int64) (*model.KBQualityEvaluation, error) {
	var evaluation model.KBQualityEvaluation
	if err := r.db.WithContext(ctx).Where("document_version_id = ? AND status = 'completed' AND review_status = 'published'", versionID).
		Order("published_at DESC NULLS LAST, id DESC").First(&evaluation).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &evaluation, nil
}

func (r *GORMUserRepository) FindPublicationEmbeddingIndex(ctx context.Context, versionID int64) (*model.KBEmbeddingIndex, error) {
	var index model.KBEmbeddingIndex
	if err := r.db.WithContext(ctx).Where("document_version_id = ?", versionID).
		Order("completed_at DESC NULLS LAST, id DESC").First(&index).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &index, nil
}

func (r *GORMUserRepository) FindPublicationSmokeRun(ctx context.Context, versionID int64) (*model.KBRetrievalEvaluationRun, error) {
	var run model.KBRetrievalEvaluationRun
	if err := r.db.WithContext(ctx).Where("document_version_id = ? AND mode = ?", versionID, model.RetrievalEvaluationModeSmoke).
		Order("created_at DESC, id DESC").First(&run).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &run, nil
}

func (r *GORMUserRepository) PublishDocumentVersion(ctx context.Context, versionID, actorID int64, gate []byte, comment *string) (*model.KBDocument, error) {
	var updated model.KBDocument
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var version model.KBDocumentVersion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&version, versionID).Error; err != nil {
			return mapRepositoryError(err)
		}
		var document model.KBDocument
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&document, version.DocumentID).Error; err != nil {
			return mapRepositoryError(err)
		}
		if version.Status == model.DocumentVersionStatusDeprecated || version.Status == model.DocumentVersionStatusFailed {
			return ErrImmutable
		}
		now := time.Now().UTC()
		if document.CurrentPublishedVersionID != nil && *document.CurrentPublishedVersionID != version.ID {
			oldID := *document.CurrentPublishedVersionID
			if err := tx.Model(&model.KBDocumentVersion{}).Where("id = ? AND status = ?", oldID, model.DocumentVersionStatusPublished).
				Updates(map[string]any{"status": model.DocumentVersionStatusSuperseded, "superseded_at": now, "updated_at": now}).Error; err != nil {
				return fmt.Errorf("supersede document version: %w", err)
			}
			if err := tx.Exec("INSERT INTO kb_document_version_publication (document_id, document_version_id, action, actor_id, created_at) VALUES (?, ?, 'supersede', ?, ?)", document.ID, oldID, actorID, now).Error; err != nil {
				return fmt.Errorf("record superseded version: %w", err)
			}
		}
		if err := tx.Model(&model.KBDocumentVersion{}).Where("id = ?", version.ID).Updates(map[string]any{
			"status": model.DocumentVersionStatusPublished, "published_by": actorID, "published_at": now,
			"reviewed_by": actorID, "reviewed_at": now, "publication_gate": json.RawMessage(gate), "updated_at": now,
		}).Error; err != nil {
			return fmt.Errorf("publish document version: %w", err)
		}
		if err := tx.Model(&model.KBDocument{}).Where("id = ?", document.ID).Updates(map[string]any{
			"current_published_version_id": version.ID, "version": version.Version, "file_name": version.FileName,
			"file_path": version.FilePath, "file_type": version.FileType, "status": model.DocumentStatusPublished,
			"reviewed_by": actorID, "reviewed_at": now, "updated_at": now,
		}).Error; err != nil {
			return fmt.Errorf("update current published version: %w", err)
		}
		if err := tx.Exec("INSERT INTO kb_document_version_publication (document_id, document_version_id, action, gate_snapshot, actor_id, comment, created_at) VALUES (?, ?, 'publish', ?::jsonb, ?, ?, ?)", document.ID, version.ID, string(gate), actorID, comment, now).Error; err != nil {
			return fmt.Errorf("record version publication: %w", err)
		}
		return tx.First(&updated, document.ID).Error
	})
	return &updated, err
}

func (r *GORMUserRepository) DeprecateDocumentVersion(ctx context.Context, versionID, actorID int64, comment *string) (*model.KBDocumentVersion, error) {
	var version model.KBDocumentVersion
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&version, versionID).Error; err != nil {
			return mapRepositoryError(err)
		}
		if version.Status != model.DocumentVersionStatusPublished && version.Status != model.DocumentVersionStatusSuperseded {
			return ErrImmutable
		}
		now := time.Now().UTC()
		if err := tx.Model(&version).Updates(map[string]any{"status": model.DocumentVersionStatusDeprecated, "deprecated_at": now, "updated_at": now}).Error; err != nil {
			return fmt.Errorf("deprecate document version: %w", err)
		}
		if err := tx.Model(&model.KBDocument{}).Where("id = ? AND current_published_version_id = ?", version.DocumentID, version.ID).
			Updates(map[string]any{"current_published_version_id": nil, "status": model.DocumentStatusDeprecated, "updated_at": now}).Error; err != nil {
			return fmt.Errorf("deprecate current document version: %w", err)
		}
		if err := tx.Exec("INSERT INTO kb_document_version_publication (document_id, document_version_id, action, actor_id, comment, created_at) VALUES (?, ?, 'deprecate', ?, ?, ?)", version.DocumentID, version.ID, actorID, comment, now).Error; err != nil {
			return fmt.Errorf("record version deprecation: %w", err)
		}
		return tx.First(&version, version.ID).Error
	})
	return &version, err
}

func (r *GORMUserRepository) FindHistoricalCitation(ctx context.Context, versionID, chunkID int64) (*model.KBDocumentVersion, *model.KBChunk, error) {
	var version model.KBDocumentVersion
	if err := r.db.WithContext(ctx).First(&version, versionID).Error; err != nil {
		return nil, nil, mapRepositoryError(err)
	}
	var chunk model.KBChunk
	if err := r.db.WithContext(ctx).Where("id = ? AND document_version_id = ?", chunkID, versionID).First(&chunk).Error; err != nil {
		return nil, nil, mapRepositoryError(err)
	}
	return &version, &chunk, nil
}
