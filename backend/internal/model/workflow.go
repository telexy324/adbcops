package model

import "time"

type WorkflowDefinition struct {
	ID          int64     `gorm:"column:id;primaryKey" json:"id"`
	Name        string    `gorm:"column:name;size:120;not null" json:"name"`
	Version     string    `gorm:"column:version;size:50;not null" json:"version"`
	Description *string   `gorm:"column:description" json:"description,omitempty"`
	Definition  []byte    `gorm:"column:definition;type:jsonb;not null" json:"definition"`
	Enabled     bool      `gorm:"column:enabled;not null" json:"enabled"`
	CreatedBy   *int64    `gorm:"column:created_by" json:"createdBy,omitempty"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (WorkflowDefinition) TableName() string {
	return "workflow_definition"
}
