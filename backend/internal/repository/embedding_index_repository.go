package repository

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var safeSQLIdentifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

func (r *GORMUserRepository) FindLLMConfigByID(ctx context.Context, id int64) (*model.LLMConfig, error) {
	return (&GORMLLMRepository{db: r.db}).FindLLMConfigByID(ctx, id)
}

func (r *GORMUserRepository) CreateEmbeddingIndex(ctx context.Context, index *model.KBEmbeddingIndex) error {
	if err := r.db.WithContext(ctx).Create(index).Error; err != nil {
		return fmt.Errorf("create embedding index: %w", err)
	}
	return nil
}

func (r *GORMUserRepository) FindEmbeddingIndex(ctx context.Context, id int64) (*model.KBEmbeddingIndex, error) {
	var index model.KBEmbeddingIndex
	if err := r.db.WithContext(ctx).First(&index, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &index, nil
}

func (r *GORMUserRepository) ListEmbeddingIndexes(ctx context.Context, versionID, strategyID int64) ([]model.KBEmbeddingIndex, error) {
	var indexes []model.KBEmbeddingIndex
	query := r.db.WithContext(ctx)
	if versionID > 0 {
		query = query.Where("document_version_id = ?", versionID)
	}
	if strategyID > 0 {
		query = query.Where("strategy_id = ?", strategyID)
	}
	if err := query.Order("created_at DESC, id DESC").Find(&indexes).Error; err != nil {
		return nil, fmt.Errorf("list embedding indexes: %w", err)
	}
	return indexes, nil
}

func (r *GORMUserRepository) RefreshEmbeddingIndexFingerprint(ctx context.Context, id int64, fingerprint string, chunkCount int) (*model.KBEmbeddingIndex, error) {
	var index model.KBEmbeddingIndex
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&index, id).Error; err != nil {
			return mapRepositoryError(err)
		}
		if index.ContentFingerprint == fingerprint && index.ChunkCount == chunkCount {
			return nil
		}
		status := index.Status
		if status == model.EmbeddingIndexReady || status == model.EmbeddingIndexFailed {
			status = model.EmbeddingIndexStale
		}
		if err := tx.Model(&index).Updates(map[string]any{"content_fingerprint": fingerprint, "chunk_count": chunkCount, "status": status, "updated_at": time.Now().UTC()}).Error; err != nil {
			return fmt.Errorf("refresh embedding index fingerprint: %w", err)
		}
		index.ContentFingerprint, index.ChunkCount, index.Status = fingerprint, chunkCount, status
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &index, nil
}

func (r *GORMUserRepository) ClaimEmbeddingIndexBuild(ctx context.Context, id int64, allowed []string) (*model.KBEmbeddingIndex, error) {
	var index model.KBEmbeddingIndex
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&index, id).Error; err != nil {
			return mapRepositoryError(err)
		}
		allowedStatus := false
		for _, status := range allowed {
			allowedStatus = allowedStatus || index.Status == status
		}
		if !allowedStatus {
			return ErrImmutable
		}
		if err := tx.Where("index_id = ?", id).Delete(&model.KBChunkEmbedding{}).Error; err != nil {
			return fmt.Errorf("clear embedding index vectors: %w", err)
		}
		if err := tx.Model(&index).Updates(map[string]any{"status": model.EmbeddingIndexBuilding, "embedded_count": 0, "error_message": nil, "completed_at": nil, "updated_at": time.Now().UTC()}).Error; err != nil {
			return fmt.Errorf("claim embedding index build: %w", err)
		}
		index.Status, index.EmbeddedCount, index.ErrorMessage, index.CompletedAt = model.EmbeddingIndexBuilding, 0, nil, nil
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &index, nil
}

func (r *GORMUserRepository) UpsertEmbeddingIndexBatch(ctx context.Context, embeddings []model.KBChunkEmbedding) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, embedding := range embeddings {
			if embedding.IndexID == nil || embedding.LLMConfigID == nil {
				return fmt.Errorf("upsert embedding batch: missing index or config")
			}
			now := time.Now().UTC()
			if err := tx.Exec(`
INSERT INTO kb_chunk_embedding (
    index_id, chunk_id, embedding_config_id, model, model_revision, dimension,
    embedding, vector_data, content_hash, normalized, distance_metric, status,
    created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?::jsonb, ?::vector, ?, ?, ?, ?, ?, ?)
ON CONFLICT (chunk_id, embedding_config_id, model_revision, content_hash)
DO UPDATE SET index_id = EXCLUDED.index_id, model = EXCLUDED.model,
    dimension = EXCLUDED.dimension, embedding = EXCLUDED.embedding,
    vector_data = EXCLUDED.vector_data, normalized = EXCLUDED.normalized,
    distance_metric = EXCLUDED.distance_metric, status = EXCLUDED.status,
    error_message = NULL, updated_at = EXCLUDED.updated_at`,
				*embedding.IndexID, embedding.ChunkID, *embedding.LLMConfigID, embedding.Model, embedding.ModelRevision, embedding.Dimension,
				string(embedding.Embedding), embedding.VectorData, embedding.ContentHash, embedding.Normalized, embedding.DistanceMetric, embedding.Status, now, now).Error; err != nil {
				return fmt.Errorf("upsert embedding vector for chunk %d: %w", embedding.ChunkID, err)
			}
		}
		return nil
	})
}

func (r *GORMUserRepository) FinishEmbeddingIndexBuild(ctx context.Context, id int64, embeddedCount int, completedAt time.Time) (*model.KBEmbeddingIndex, error) {
	result := r.db.WithContext(ctx).Model(&model.KBEmbeddingIndex{}).Where("id = ? AND status = ?", id, model.EmbeddingIndexBuilding).Updates(map[string]any{"status": model.EmbeddingIndexReady, "embedded_count": embeddedCount, "error_message": nil, "completed_at": completedAt, "updated_at": completedAt})
	if result.Error != nil {
		return nil, fmt.Errorf("finish embedding index build: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, ErrImmutable
	}
	return r.FindEmbeddingIndex(ctx, id)
}

func (r *GORMUserRepository) FailEmbeddingIndexBuild(ctx context.Context, id int64, message string) error {
	if err := r.db.WithContext(ctx).Model(&model.KBEmbeddingIndex{}).Where("id = ?", id).Updates(map[string]any{"status": model.EmbeddingIndexFailed, "error_message": message, "updated_at": time.Now().UTC()}).Error; err != nil {
		return fmt.Errorf("fail embedding index build: %w", err)
	}
	return nil
}

func (r *GORMUserRepository) MarkEmbeddingIndexStale(ctx context.Context, id int64) error {
	result := r.db.WithContext(ctx).Model(&model.KBEmbeddingIndex{}).Where("id = ? AND status <> ?", id, model.EmbeddingIndexBuilding).Updates(map[string]any{"status": model.EmbeddingIndexStale, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return fmt.Errorf("mark embedding index stale: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrImmutable
	}
	return nil
}

func (r *GORMUserRepository) EnsureEmbeddingHNSWIndex(ctx context.Context, index *model.KBEmbeddingIndex) error {
	if index == nil || !index.HNSWEnabled {
		return nil
	}
	name := fmt.Sprintf("idx_kb_embedding_hnsw_%d", index.ID)
	if !safeSQLIdentifier.MatchString(name) || index.Dimension <= 0 || index.HNSWM < 2 || index.HNSWM > 100 || index.HNSWEFConstruction < 4 || index.HNSWEFConstruction > 1000 {
		return fmt.Errorf("invalid hnsw index configuration")
	}
	statement := fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON kb_chunk_embedding USING hnsw ((vector_data::vector(%d)) vector_cosine_ops) WITH (m = %d, ef_construction = %d) WHERE index_id = %d AND status = 'ready'", name, index.Dimension, index.HNSWM, index.HNSWEFConstruction, index.ID)
	if err := r.db.WithContext(ctx).Exec(statement).Error; err != nil {
		return fmt.Errorf("create embedding hnsw index: %w", err)
	}
	return nil
}
