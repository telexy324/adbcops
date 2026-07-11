package repository

import (
	"context"
	"fmt"
	"time"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
)

type AgentRunRepository interface {
	CreateAgentRun(ctx context.Context, run *model.AgentRun) error
	UpdateAgentRun(ctx context.Context, id int64, updates AgentRunUpdates) (*model.AgentRun, error)
	ListAgentRuns(ctx context.Context, limit int) ([]model.AgentRun, error)
	FindAgentRunByID(ctx context.Context, id int64) (*model.AgentRun, error)
}

type AgentRunUpdates struct {
	Status       string
	Output       []byte
	ErrorMessage *string
	FinishedAt   *time.Time
}

type GORMAgentRunRepository struct {
	db *gorm.DB
}

func NewAgentRunRepository(db *gorm.DB) *GORMAgentRunRepository {
	return &GORMAgentRunRepository{db: db}
}

func (r *GORMAgentRunRepository) CreateAgentRun(ctx context.Context, run *model.AgentRun) error {
	if err := r.db.WithContext(ctx).Create(run).Error; err != nil {
		return fmt.Errorf("create agent run: %w", err)
	}
	return nil
}

func (r *GORMAgentRunRepository) UpdateAgentRun(ctx context.Context, id int64, updates AgentRunUpdates) (*model.AgentRun, error) {
	values := map[string]any{"status": updates.Status}
	if updates.Output != nil {
		values["output"] = updates.Output
	}
	if updates.ErrorMessage != nil {
		values["error_message"] = *updates.ErrorMessage
	}
	if updates.FinishedAt != nil {
		values["finished_at"] = *updates.FinishedAt
	}
	result := r.db.WithContext(ctx).Model(&model.AgentRun{}).Where("id = ?", id).Updates(values)
	if result.Error != nil {
		return nil, fmt.Errorf("update agent run: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, ErrNotFound
	}
	return r.FindAgentRunByID(ctx, id)
}

func (r *GORMAgentRunRepository) ListAgentRuns(ctx context.Context, limit int) ([]model.AgentRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var runs []model.AgentRun
	if err := r.db.WithContext(ctx).Order("started_at DESC NULLS LAST, id DESC").Limit(limit).Find(&runs).Error; err != nil {
		return nil, fmt.Errorf("list agent runs: %w", err)
	}
	return runs, nil
}

func (r *GORMAgentRunRepository) FindAgentRunByID(ctx context.Context, id int64) (*model.AgentRun, error) {
	var run model.AgentRun
	if err := r.db.WithContext(ctx).First(&run, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &run, nil
}
