package repository

import (
	"context"
	"fmt"
	"time"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
)

type WorkflowRepository interface {
	ListWorkflowDefinitions(ctx context.Context, limit int) ([]model.WorkflowDefinition, error)
	FindWorkflowDefinitionByID(ctx context.Context, id int64) (*model.WorkflowDefinition, error)
	CreateWorkflowDefinition(ctx context.Context, definition *model.WorkflowDefinition) error
	UpdateWorkflowDefinition(ctx context.Context, id int64, updates WorkflowDefinitionUpdates) (*model.WorkflowDefinition, error)
	CreateWorkflowRun(ctx context.Context, run *model.WorkflowRun) error
	UpdateWorkflowRun(ctx context.Context, id int64, updates WorkflowRunUpdates) (*model.WorkflowRun, error)
	ListWorkflowRuns(ctx context.Context, limit int) ([]model.WorkflowRun, error)
	FindWorkflowRunByID(ctx context.Context, id int64) (*model.WorkflowRun, error)
	CreateWorkflowNodeRun(ctx context.Context, run *model.WorkflowNodeRun) error
	UpdateWorkflowNodeRun(ctx context.Context, id int64, updates WorkflowNodeRunUpdates) (*model.WorkflowNodeRun, error)
	CancelWorkflowRun(ctx context.Context, id int64, finishedAt time.Time) (*model.WorkflowRun, error)
}

type WorkflowDefinitionUpdates struct {
	Name           string
	Version        string
	Description    *string
	Definition     []byte
	Enabled        bool
	EnabledSet     bool
	DescriptionSet bool
}

type WorkflowRunUpdates struct {
	Status       string
	Output       []byte
	ErrorMessage *string
	StartedAt    *time.Time
	FinishedAt   *time.Time
}

type WorkflowNodeRunUpdates struct {
	Status       string
	Output       []byte
	ErrorMessage *string
	StartedAt    *time.Time
	FinishedAt   *time.Time
	Attempt      *int
}

type GORMWorkflowRepository struct {
	db *gorm.DB
}

func NewWorkflowRepository(db *gorm.DB) *GORMWorkflowRepository {
	return &GORMWorkflowRepository{db: db}
}

func (r *GORMWorkflowRepository) ListWorkflowDefinitions(ctx context.Context, limit int) ([]model.WorkflowDefinition, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var definitions []model.WorkflowDefinition
	if err := r.db.WithContext(ctx).Order("name ASC, version DESC, id DESC").Limit(limit).Find(&definitions).Error; err != nil {
		return nil, fmt.Errorf("list workflow definitions: %w", err)
	}
	return definitions, nil
}

func (r *GORMWorkflowRepository) FindWorkflowDefinitionByID(ctx context.Context, id int64) (*model.WorkflowDefinition, error) {
	var definition model.WorkflowDefinition
	if err := r.db.WithContext(ctx).First(&definition, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &definition, nil
}

func (r *GORMWorkflowRepository) CreateWorkflowDefinition(ctx context.Context, definition *model.WorkflowDefinition) error {
	if err := r.db.WithContext(ctx).Create(definition).Error; err != nil {
		return fmt.Errorf("create workflow definition: %w", err)
	}
	return nil
}

func (r *GORMWorkflowRepository) UpdateWorkflowDefinition(ctx context.Context, id int64, updates WorkflowDefinitionUpdates) (*model.WorkflowDefinition, error) {
	values := map[string]any{"updated_at": time.Now().UTC()}
	if updates.Name != "" {
		values["name"] = updates.Name
	}
	if updates.Version != "" {
		values["version"] = updates.Version
	}
	if updates.DescriptionSet {
		values["description"] = updates.Description
	}
	if updates.Definition != nil {
		values["definition"] = updates.Definition
	}
	if updates.EnabledSet {
		values["enabled"] = updates.Enabled
	}
	result := r.db.WithContext(ctx).Model(&model.WorkflowDefinition{}).Where("id = ?", id).Updates(values)
	if result.Error != nil {
		return nil, fmt.Errorf("update workflow definition: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, ErrNotFound
	}
	return r.FindWorkflowDefinitionByID(ctx, id)
}

func (r *GORMWorkflowRepository) CreateWorkflowRun(ctx context.Context, run *model.WorkflowRun) error {
	if err := r.db.WithContext(ctx).Create(run).Error; err != nil {
		return fmt.Errorf("create workflow run: %w", err)
	}
	return nil
}

func (r *GORMWorkflowRepository) UpdateWorkflowRun(ctx context.Context, id int64, updates WorkflowRunUpdates) (*model.WorkflowRun, error) {
	values := map[string]any{}
	if updates.Status != "" {
		values["status"] = updates.Status
	}
	if updates.Output != nil {
		values["output"] = updates.Output
	}
	if updates.ErrorMessage != nil {
		values["error_message"] = *updates.ErrorMessage
	}
	if updates.StartedAt != nil {
		values["started_at"] = *updates.StartedAt
	}
	if updates.FinishedAt != nil {
		values["finished_at"] = *updates.FinishedAt
	}
	if len(values) == 0 {
		return r.FindWorkflowRunByID(ctx, id)
	}
	result := r.db.WithContext(ctx).Model(&model.WorkflowRun{}).Where("id = ?", id).Updates(values)
	if result.Error != nil {
		return nil, fmt.Errorf("update workflow run: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, ErrNotFound
	}
	return r.FindWorkflowRunByID(ctx, id)
}

func (r *GORMWorkflowRepository) ListWorkflowRuns(ctx context.Context, limit int) ([]model.WorkflowRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var runs []model.WorkflowRun
	if err := r.db.WithContext(ctx).Preload("NodeRuns").Order("created_at DESC, id DESC").Limit(limit).Find(&runs).Error; err != nil {
		return nil, fmt.Errorf("list workflow runs: %w", err)
	}
	return runs, nil
}

func (r *GORMWorkflowRepository) FindWorkflowRunByID(ctx context.Context, id int64) (*model.WorkflowRun, error) {
	var run model.WorkflowRun
	if err := r.db.WithContext(ctx).Preload("NodeRuns", func(db *gorm.DB) *gorm.DB {
		return db.Order("id ASC")
	}).First(&run, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &run, nil
}

func (r *GORMWorkflowRepository) CreateWorkflowNodeRun(ctx context.Context, run *model.WorkflowNodeRun) error {
	if err := r.db.WithContext(ctx).Create(run).Error; err != nil {
		return fmt.Errorf("create workflow node run: %w", err)
	}
	return nil
}

func (r *GORMWorkflowRepository) UpdateWorkflowNodeRun(ctx context.Context, id int64, updates WorkflowNodeRunUpdates) (*model.WorkflowNodeRun, error) {
	values := map[string]any{}
	if updates.Status != "" {
		values["status"] = updates.Status
	}
	if updates.Output != nil {
		values["output"] = updates.Output
	}
	if updates.ErrorMessage != nil {
		values["error_message"] = *updates.ErrorMessage
	}
	if updates.StartedAt != nil {
		values["started_at"] = *updates.StartedAt
	}
	if updates.FinishedAt != nil {
		values["finished_at"] = *updates.FinishedAt
	}
	if updates.Attempt != nil {
		values["attempt"] = *updates.Attempt
	}
	result := r.db.WithContext(ctx).Model(&model.WorkflowNodeRun{}).Where("id = ?", id).Updates(values)
	if result.Error != nil {
		return nil, fmt.Errorf("update workflow node run: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, ErrNotFound
	}
	var run model.WorkflowNodeRun
	if err := r.db.WithContext(ctx).First(&run, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &run, nil
}

func (r *GORMWorkflowRepository) CancelWorkflowRun(ctx context.Context, id int64, finishedAt time.Time) (*model.WorkflowRun, error) {
	result := r.db.WithContext(ctx).Model(&model.WorkflowRun{}).
		Where("id = ? AND status IN ?", id, []string{model.WorkflowRunStatusPending, model.WorkflowRunStatusRunning, model.WorkflowRunStatusWaiting}).
		Updates(map[string]any{"status": model.WorkflowRunStatusCancelled, "finished_at": finishedAt})
	if result.Error != nil {
		return nil, fmt.Errorf("cancel workflow run: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, ErrNotFound
	}
	return r.FindWorkflowRunByID(ctx, id)
}
