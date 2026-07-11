package repository

import (
	"context"
	"fmt"
	"time"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
)

type SkillRunRepository interface {
	CreateSkillRun(ctx context.Context, run *model.SkillRun) error
	UpdateSkillRun(ctx context.Context, id int64, updates SkillRunUpdates) (*model.SkillRun, error)
	ListSkillRuns(ctx context.Context, limit int) ([]model.SkillRun, error)
}

type SkillRunUpdates struct {
	Status        string
	OutputSummary []byte
	ErrorMessage  *string
	FinishedAt    *time.Time
}

type GORMSkillRunRepository struct {
	db *gorm.DB
}

func NewSkillRunRepository(db *gorm.DB) *GORMSkillRunRepository {
	return &GORMSkillRunRepository{db: db}
}

func (r *GORMSkillRunRepository) CreateSkillRun(ctx context.Context, run *model.SkillRun) error {
	if err := r.db.WithContext(ctx).Create(run).Error; err != nil {
		return fmt.Errorf("create skill run: %w", err)
	}
	return nil
}

func (r *GORMSkillRunRepository) UpdateSkillRun(ctx context.Context, id int64, updates SkillRunUpdates) (*model.SkillRun, error) {
	values := map[string]any{"status": updates.Status}
	if updates.OutputSummary != nil {
		values["output_summary"] = updates.OutputSummary
	}
	if updates.ErrorMessage != nil {
		values["error_message"] = *updates.ErrorMessage
	}
	if updates.FinishedAt != nil {
		values["finished_at"] = *updates.FinishedAt
	}
	result := r.db.WithContext(ctx).Model(&model.SkillRun{}).Where("id = ?", id).Updates(values)
	if result.Error != nil {
		return nil, fmt.Errorf("update skill run: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, ErrNotFound
	}
	var run model.SkillRun
	if err := r.db.WithContext(ctx).First(&run, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &run, nil
}

func (r *GORMSkillRunRepository) ListSkillRuns(ctx context.Context, limit int) ([]model.SkillRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var runs []model.SkillRun
	if err := r.db.WithContext(ctx).Order("started_at DESC NULLS LAST, id DESC").Limit(limit).Find(&runs).Error; err != nil {
		return nil, fmt.Errorf("list skill runs: %w", err)
	}
	return runs, nil
}
