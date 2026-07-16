package repository

import (
	"context"
	"fmt"
	"time"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
)

type LLMRepository interface {
	ListLLMConfigs(ctx context.Context) ([]model.LLMConfig, error)
	FindLLMConfigByID(ctx context.Context, id int64) (*model.LLMConfig, error)
	FindDefaultEnabledLLMConfigByPurpose(ctx context.Context, purpose string) (*model.LLMConfig, error)
	CreateLLMConfig(ctx context.Context, config *model.LLMConfig) error
	UpdateLLMConfig(ctx context.Context, id int64, updates LLMConfigUpdates) (*model.LLMConfig, error)
	DeleteLLMConfig(ctx context.Context, id int64) error
	SetDefaultLLMConfig(ctx context.Context, id int64) (*model.LLMConfig, error)
}

type LLMConfigUpdates struct {
	Name            *string
	Provider        *string
	BaseURL         *string
	Model           *string
	Purpose         *string
	APIKeyRef       *string
	APIKeyRefSet    bool
	AppKeyRef       *string
	AppKeyRefSet    bool
	APISecretRef    *string
	APISecretRefSet bool
	Temperature     *float64
	Enabled         *bool
	IsDefault       *bool
}

type GORMLLMRepository struct {
	db *gorm.DB
}

func NewLLMRepository(db *gorm.DB) *GORMLLMRepository {
	return &GORMLLMRepository{db: db}
}

func (r *GORMLLMRepository) ListLLMConfigs(ctx context.Context) ([]model.LLMConfig, error) {
	var configs []model.LLMConfig
	if err := r.db.WithContext(ctx).Order("id ASC").Find(&configs).Error; err != nil {
		return nil, fmt.Errorf("list llm configs: %w", err)
	}
	return configs, nil
}

func (r *GORMLLMRepository) FindLLMConfigByID(ctx context.Context, id int64) (*model.LLMConfig, error) {
	var config model.LLMConfig
	if err := r.db.WithContext(ctx).First(&config, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &config, nil
}

func (r *GORMLLMRepository) FindDefaultEnabledLLMConfigByPurpose(ctx context.Context, purpose string) (*model.LLMConfig, error) {
	var config model.LLMConfig
	if err := r.db.WithContext(ctx).
		Where("enabled = ? AND is_default = ? AND purpose = ?", true, true, purpose).
		First(&config).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &config, nil
}

func (r *GORMLLMRepository) CreateLLMConfig(ctx context.Context, config *model.LLMConfig) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if config.IsDefault {
			if err := clearDefaultLLMConfig(tx, config.Purpose); err != nil {
				return err
			}
		}
		if err := tx.Create(config).Error; err != nil {
			return fmt.Errorf("create llm config: %w", err)
		}
		return nil
	})
}

func (r *GORMLLMRepository) UpdateLLMConfig(ctx context.Context, id int64, updates LLMConfigUpdates) (*model.LLMConfig, error) {
	var updated model.LLMConfig
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if updates.IsDefault != nil && *updates.IsDefault {
			purpose := model.LLMPurposeChat
			if updates.Purpose != nil {
				purpose = *updates.Purpose
			} else {
				var existing model.LLMConfig
				if err := tx.First(&existing, id).Error; err != nil {
					return mapRepositoryError(err)
				}
				purpose = existing.Purpose
			}
			if err := clearDefaultLLMConfig(tx, purpose); err != nil {
				return err
			}
		}
		values := map[string]any{"updated_at": time.Now().UTC()}
		if updates.Name != nil {
			values["name"] = *updates.Name
		}
		if updates.Provider != nil {
			values["provider"] = *updates.Provider
		}
		if updates.BaseURL != nil {
			values["base_url"] = *updates.BaseURL
		}
		if updates.Model != nil {
			values["model"] = *updates.Model
		}
		if updates.Purpose != nil {
			values["purpose"] = *updates.Purpose
		}
		if updates.APIKeyRefSet {
			values["api_key_ref"] = updates.APIKeyRef
		}
		if updates.AppKeyRefSet {
			values["app_key_ref"] = updates.AppKeyRef
		}
		if updates.APISecretRefSet {
			values["api_secret_ref"] = updates.APISecretRef
		}
		if updates.Temperature != nil {
			values["temperature"] = *updates.Temperature
		}
		if updates.Enabled != nil {
			values["enabled"] = *updates.Enabled
		}
		if updates.IsDefault != nil {
			values["is_default"] = *updates.IsDefault
		}
		result := tx.Model(&model.LLMConfig{}).Where("id = ?", id).Updates(values)
		if result.Error != nil {
			return fmt.Errorf("update llm config: %w", result.Error)
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

func (r *GORMLLMRepository) DeleteLLMConfig(ctx context.Context, id int64) error {
	result := r.db.WithContext(ctx).Delete(&model.LLMConfig{}, id)
	if result.Error != nil {
		return fmt.Errorf("delete llm config: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}

func (r *GORMLLMRepository) SetDefaultLLMConfig(ctx context.Context, id int64) (*model.LLMConfig, error) {
	return r.UpdateLLMConfig(ctx, id, LLMConfigUpdates{IsDefault: ptr(true)})
}

func clearDefaultLLMConfig(tx *gorm.DB, purpose string) error {
	if err := tx.Model(&model.LLMConfig{}).
		Where("is_default = ? AND purpose = ?", true, purpose).
		Update("is_default", false).Error; err != nil {
		return fmt.Errorf("clear default llm config: %w", err)
	}
	return nil
}

func ptr[T any](value T) *T {
	return &value
}
