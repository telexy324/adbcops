package repository

import (
	"context"
	"fmt"
	"time"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
)

type AnalysisRepository interface {
	CreateAnalysisTask(ctx context.Context, task *model.AnalysisTask) error
	UpdateAnalysisTask(ctx context.Context, id int64, updates AnalysisTaskUpdates) (*model.AnalysisTask, error)
	ListAnalysisTasks(ctx context.Context, userID *int64) ([]model.AnalysisTask, error)
	FindAnalysisTaskByID(ctx context.Context, id int64) (*model.AnalysisTask, error)
	SearchChunks(ctx context.Context, query string, limit int) ([]model.KBChunk, error)
	FindDefaultEnabledLLMConfig(ctx context.Context) (*model.LLMConfig, error)
}

type AnalysisTaskUpdates struct {
	Status       string
	Summary      *string
	Result       []byte
	ErrorMessage *string
	FinishedAt   *time.Time
}

type GORMAnalysisRepository struct {
	db *gorm.DB
}

func NewAnalysisRepository(db *gorm.DB) *GORMAnalysisRepository {
	return &GORMAnalysisRepository{db: db}
}

func (r *GORMAnalysisRepository) CreateAnalysisTask(ctx context.Context, task *model.AnalysisTask) error {
	if err := r.db.WithContext(ctx).Create(task).Error; err != nil {
		return fmt.Errorf("create analysis task: %w", err)
	}
	return nil
}

func (r *GORMAnalysisRepository) UpdateAnalysisTask(ctx context.Context, id int64, updates AnalysisTaskUpdates) (*model.AnalysisTask, error) {
	values := map[string]any{
		"status":     updates.Status,
		"updated_at": time.Now().UTC(),
	}
	if updates.Summary != nil {
		values["summary"] = *updates.Summary
	}
	if updates.Result != nil {
		values["result"] = updates.Result
	}
	if updates.ErrorMessage != nil {
		values["error_message"] = *updates.ErrorMessage
	}
	if updates.FinishedAt != nil {
		values["finished_at"] = *updates.FinishedAt
	}
	result := r.db.WithContext(ctx).Model(&model.AnalysisTask{}).Where("id = ?", id).Updates(values)
	if result.Error != nil {
		return nil, fmt.Errorf("update analysis task: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, ErrNotFound
	}
	return r.FindAnalysisTaskByID(ctx, id)
}

func (r *GORMAnalysisRepository) ListAnalysisTasks(ctx context.Context, userID *int64) ([]model.AnalysisTask, error) {
	var tasks []model.AnalysisTask
	query := r.db.WithContext(ctx).Order("created_at DESC, id DESC")
	if userID != nil {
		query = query.Where("user_id = ?", *userID)
	}
	if err := query.Find(&tasks).Error; err != nil {
		return nil, fmt.Errorf("list analysis tasks: %w", err)
	}
	return tasks, nil
}

func (r *GORMAnalysisRepository) FindAnalysisTaskByID(ctx context.Context, id int64) (*model.AnalysisTask, error) {
	var task model.AnalysisTask
	if err := r.db.WithContext(ctx).First(&task, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &task, nil
}

func (r *GORMAnalysisRepository) SearchChunks(ctx context.Context, query string, limit int) ([]model.KBChunk, error) {
	return (&GORMUserRepository{db: r.db}).SearchChunks(ctx, query, limit)
}

func (r *GORMAnalysisRepository) FindDefaultEnabledLLMConfig(ctx context.Context) (*model.LLMConfig, error) {
	return (&GORMRAGRepository{db: r.db}).FindDefaultEnabledLLMConfig(ctx)
}
