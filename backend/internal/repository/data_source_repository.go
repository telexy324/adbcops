package repository

import (
	"context"
	"fmt"
	"time"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
)

type DataSourceRepository interface {
	ListDataSources(ctx context.Context, enabledOnly bool) ([]model.DataSource, error)
	FindDataSourceByID(ctx context.Context, id int64) (*model.DataSource, error)
	CreateDataSource(ctx context.Context, dataSource *model.DataSource, credential *model.CredentialSecret) error
	UpdateDataSource(ctx context.Context, id int64, updates DataSourceUpdates, credential *model.CredentialSecret) (*model.DataSource, error)
	DeleteDataSource(ctx context.Context, id int64) error
}

type DataSourceUpdates struct {
	Name           *string
	SourceType     *string
	Environment    *string
	EnvironmentSet bool
	SystemName     *string
	SystemNameSet  bool
	ComponentName  *string
	ComponentSet   bool
	Config         []byte
	ConfigSet      bool
	Enabled        *bool
	ReadOnly       *bool
}

type GORMDataSourceRepository struct {
	db *gorm.DB
}

func NewDataSourceRepository(db *gorm.DB) *GORMDataSourceRepository {
	return &GORMDataSourceRepository{db: db}
}

func (r *GORMDataSourceRepository) ListDataSources(ctx context.Context, enabledOnly bool) ([]model.DataSource, error) {
	var dataSources []model.DataSource
	query := r.db.WithContext(ctx).Order("id ASC")
	if enabledOnly {
		query = query.Where("enabled = ?", true)
	}
	if err := query.Find(&dataSources).Error; err != nil {
		return nil, fmt.Errorf("list data sources: %w", err)
	}
	return dataSources, nil
}

func (r *GORMDataSourceRepository) FindDataSourceByID(ctx context.Context, id int64) (*model.DataSource, error) {
	var dataSource model.DataSource
	if err := r.db.WithContext(ctx).Preload("Credential").First(&dataSource, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &dataSource, nil
}

func (r *GORMDataSourceRepository) CreateDataSource(ctx context.Context, dataSource *model.DataSource, credential *model.CredentialSecret) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if credential != nil {
			if err := tx.Create(credential).Error; err != nil {
				return fmt.Errorf("create credential secret: %w", err)
			}
			dataSource.CredentialID = &credential.ID
		}
		if err := tx.Create(dataSource).Error; err != nil {
			return fmt.Errorf("create data source: %w", err)
		}
		return nil
	})
}

func (r *GORMDataSourceRepository) UpdateDataSource(ctx context.Context, id int64, updates DataSourceUpdates, credential *model.CredentialSecret) (*model.DataSource, error) {
	var updated model.DataSource
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		values := map[string]any{"updated_at": time.Now().UTC()}
		if updates.Name != nil {
			values["name"] = *updates.Name
		}
		if updates.SourceType != nil {
			values["source_type"] = *updates.SourceType
		}
		if updates.EnvironmentSet {
			values["environment"] = updates.Environment
		}
		if updates.SystemNameSet {
			values["system_name"] = updates.SystemName
		}
		if updates.ComponentSet {
			values["component_name"] = updates.ComponentName
		}
		if updates.ConfigSet {
			values["config"] = updates.Config
		}
		if updates.Enabled != nil {
			values["enabled"] = *updates.Enabled
		}
		if updates.ReadOnly != nil {
			values["read_only"] = *updates.ReadOnly
		}
		if credential != nil {
			if err := tx.Create(credential).Error; err != nil {
				return fmt.Errorf("create credential secret: %w", err)
			}
			values["credential_id"] = credential.ID
		}
		result := tx.Model(&model.DataSource{}).Where("id = ?", id).Updates(values)
		if result.Error != nil {
			return fmt.Errorf("update data source: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrNotFound
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

func (r *GORMDataSourceRepository) DeleteDataSource(ctx context.Context, id int64) error {
	result := r.db.WithContext(ctx).Delete(&model.DataSource{}, id)
	if result.Error != nil {
		return fmt.Errorf("delete data source: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}
