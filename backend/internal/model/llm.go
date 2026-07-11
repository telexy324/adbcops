package model

import "time"

const (
	ProviderDeepSeek         = "deepseek"
	ProviderQwen             = "qwen"
	ProviderOpenAICompatible = "openai-compatible"
	DefaultLLMTestPrompt     = "Say ok."
)

type LLMConfig struct {
	ID          int64     `gorm:"column:id;primaryKey" json:"id"`
	Name        string    `gorm:"column:name;size:120;not null" json:"name"`
	Provider    string    `gorm:"column:provider;size:50;not null" json:"provider"`
	BaseURL     string    `gorm:"column:base_url;not null" json:"baseUrl"`
	Model       string    `gorm:"column:model;size:120;not null" json:"model"`
	APIKeyRef   *string   `gorm:"column:api_key_ref" json:"-"`
	Temperature float64   `gorm:"column:temperature;not null" json:"temperature"`
	Enabled     bool      `gorm:"column:enabled;not null" json:"enabled"`
	IsDefault   bool      `gorm:"column:is_default;not null" json:"isDefault"`
	CreatedBy   *int64    `gorm:"column:created_by" json:"createdBy,omitempty"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (LLMConfig) TableName() string {
	return "llm_config"
}
