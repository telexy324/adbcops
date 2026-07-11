package model

import "time"

const (
	WorkflowRunStatusPending        = "pending"
	WorkflowRunStatusRunning        = "running"
	WorkflowRunStatusWaiting        = "waiting"
	WorkflowRunStatusPartialSuccess = "partial_success"
	WorkflowRunStatusSuccess        = "success"
	WorkflowRunStatusFailed         = "failed"
	WorkflowRunStatusCancelled      = "cancelled"
)

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

type WorkflowRun struct {
	ID             int64               `gorm:"column:id;primaryKey" json:"id"`
	WorkflowID     *int64              `gorm:"column:workflow_id" json:"workflowId,omitempty"`
	UserID         *int64              `gorm:"column:user_id" json:"userId,omitempty"`
	ConversationID *int64              `gorm:"column:conversation_id" json:"conversationId,omitempty"`
	IncidentID     *int64              `gorm:"column:incident_id" json:"incidentId,omitempty"`
	Status         string              `gorm:"column:status;size:30;not null" json:"status"`
	Input          []byte              `gorm:"column:input;type:jsonb" json:"input,omitempty"`
	Output         []byte              `gorm:"column:output;type:jsonb" json:"output,omitempty"`
	ErrorMessage   *string             `gorm:"column:error_message" json:"errorMessage,omitempty"`
	StartedAt      *time.Time          `gorm:"column:started_at" json:"startedAt,omitempty"`
	FinishedAt     *time.Time          `gorm:"column:finished_at" json:"finishedAt,omitempty"`
	CreatedAt      time.Time           `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	NodeRuns       []WorkflowNodeRun   `gorm:"foreignKey:WorkflowRunID" json:"nodeRuns,omitempty"`
	Definition     *WorkflowDefinition `gorm:"foreignKey:WorkflowID" json:"-"`
}

func (WorkflowRun) TableName() string {
	return "workflow_run"
}

type WorkflowNodeRun struct {
	ID            int64      `gorm:"column:id;primaryKey" json:"id"`
	WorkflowRunID int64      `gorm:"column:workflow_run_id;not null" json:"workflowRunId"`
	NodeID        string     `gorm:"column:node_id;size:120;not null" json:"nodeId"`
	NodeType      string     `gorm:"column:node_type;size:50;not null" json:"nodeType"`
	Status        string     `gorm:"column:status;size:30;not null" json:"status"`
	Input         []byte     `gorm:"column:input;type:jsonb" json:"input,omitempty"`
	Output        []byte     `gorm:"column:output;type:jsonb" json:"output,omitempty"`
	ErrorMessage  *string    `gorm:"column:error_message" json:"errorMessage,omitempty"`
	Attempt       int        `gorm:"column:attempt;not null" json:"attempt"`
	StartedAt     *time.Time `gorm:"column:started_at" json:"startedAt,omitempty"`
	FinishedAt    *time.Time `gorm:"column:finished_at" json:"finishedAt,omitempty"`
}

func (WorkflowNodeRun) TableName() string {
	return "workflow_node_run"
}
