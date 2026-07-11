package model

import "time"

const (
	AgentRunStatusRunning = "running"
	AgentRunStatusSuccess = "success"
	AgentRunStatusFailed  = "failed"
)

type AgentRun struct {
	ID            int64      `gorm:"column:id;primaryKey" json:"id"`
	WorkflowRunID *int64     `gorm:"column:workflow_run_id" json:"workflowRunId,omitempty"`
	AgentName     string     `gorm:"column:agent_name;size:120;not null" json:"agentName"`
	InputSummary  *string    `gorm:"column:input_summary" json:"inputSummary,omitempty"`
	Output        []byte     `gorm:"column:output;type:jsonb" json:"output,omitempty"`
	ModelName     *string    `gorm:"column:model_name;size:120" json:"modelName,omitempty"`
	TokenUsage    []byte     `gorm:"column:token_usage;type:jsonb" json:"tokenUsage,omitempty"`
	Status        string     `gorm:"column:status;size:30" json:"status"`
	ErrorMessage  *string    `gorm:"column:error_message" json:"errorMessage,omitempty"`
	StartedAt     *time.Time `gorm:"column:started_at" json:"startedAt,omitempty"`
	FinishedAt    *time.Time `gorm:"column:finished_at" json:"finishedAt,omitempty"`
}

func (AgentRun) TableName() string {
	return "agent_run"
}
