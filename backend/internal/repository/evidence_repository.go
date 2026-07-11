package repository

import (
	"context"
	"fmt"
	"time"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
)

type EvidenceRepository interface {
	CreateEvidence(ctx context.Context, evidence *model.EvidenceRecord) error
	FindEvidenceByID(ctx context.Context, id int64) (*model.EvidenceRecord, error)
	FindEvidenceByKey(ctx context.Context, key string) (*model.EvidenceRecord, error)
	ListEvidence(ctx context.Context, filters EvidenceFilters) ([]model.EvidenceRecord, error)
	MissingEvidenceKeys(ctx context.Context, keys []string) ([]string, error)
}

type EvidenceFilters struct {
	Limit       int
	SourceType  string
	Sensitivity string
	From        *time.Time
	To          *time.Time
}

type GORMEvidenceRepository struct {
	db *gorm.DB
}

func NewEvidenceRepository(db *gorm.DB) *GORMEvidenceRepository {
	return &GORMEvidenceRepository{db: db}
}

func (r *GORMEvidenceRepository) CreateEvidence(ctx context.Context, evidence *model.EvidenceRecord) error {
	if err := r.db.WithContext(ctx).Create(evidence).Error; err != nil {
		return fmt.Errorf("create evidence: %w", err)
	}
	return nil
}

func (r *GORMEvidenceRepository) FindEvidenceByID(ctx context.Context, id int64) (*model.EvidenceRecord, error) {
	var evidence model.EvidenceRecord
	if err := r.db.WithContext(ctx).First(&evidence, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &evidence, nil
}

func (r *GORMEvidenceRepository) FindEvidenceByKey(ctx context.Context, key string) (*model.EvidenceRecord, error) {
	var evidence model.EvidenceRecord
	if err := r.db.WithContext(ctx).Where("evidence_key = ?", key).First(&evidence).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &evidence, nil
}

func (r *GORMEvidenceRepository) ListEvidence(ctx context.Context, filters EvidenceFilters) ([]model.EvidenceRecord, error) {
	limit := filters.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := r.db.WithContext(ctx).Order("observed_at DESC NULLS LAST, id DESC").Limit(limit)
	if filters.SourceType != "" {
		query = query.Where("source_type = ?", filters.SourceType)
	}
	if filters.Sensitivity != "" {
		query = query.Where("sensitivity = ?", filters.Sensitivity)
	}
	if filters.From != nil {
		query = query.Where("observed_at >= ?", *filters.From)
	}
	if filters.To != nil {
		query = query.Where("observed_at <= ?", *filters.To)
	}
	var evidence []model.EvidenceRecord
	if err := query.Find(&evidence).Error; err != nil {
		return nil, fmt.Errorf("list evidence: %w", err)
	}
	return evidence, nil
}

func (r *GORMEvidenceRepository) MissingEvidenceKeys(ctx context.Context, keys []string) ([]string, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	var existing []string
	if err := r.db.WithContext(ctx).Model(&model.EvidenceRecord{}).Where("evidence_key IN ?", keys).Pluck("evidence_key", &existing).Error; err != nil {
		return nil, fmt.Errorf("find evidence keys: %w", err)
	}
	seen := map[string]struct{}{}
	for _, key := range existing {
		seen[key] = struct{}{}
	}
	missing := []string{}
	for _, key := range keys {
		if _, ok := seen[key]; !ok {
			missing = append(missing, key)
		}
	}
	return missing, nil
}
