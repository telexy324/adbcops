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
