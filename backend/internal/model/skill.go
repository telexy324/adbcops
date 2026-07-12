package model

import "time"

const (
	SkillRiskSafeRead      = "safe_read"
	SkillRiskSensitiveRead = "sensitive_read"

	SkillRunStatusRunning = "running"
	SkillRunStatusSuccess = "success"
	SkillRunStatusFailed  = "failed"
)

type SkillRun struct {
	ID            int64      `gorm:"column:id;primaryKey" json:"id"`
	WorkflowRunID *int64     `gorm:"column:workflow_run_id" json:"workflowRunId,omitempty"`
	NodeRunID     *int64     `gorm:"column:node_run_id" json:"nodeRunId,omitempty"`
	RequestID     *string    `gorm:"column:request_id;size:160" json:"requestId,omitempty"`
	SkillName     string     `gorm:"column:skill_name;size:120;not null" json:"skillName"`
	ToolName      *string    `gorm:"column:tool_name;size:120" json:"toolName,omitempty"`
	InputSummary  []byte     `gorm:"column:input_summary;type:jsonb" json:"inputSummary,omitempty"`
	OutputSummary []byte     `gorm:"column:output_summary;type:jsonb" json:"outputSummary,omitempty"`
	Status        string     `gorm:"column:status;size:30" json:"status"`
	ErrorMessage  *string    `gorm:"column:error_message" json:"errorMessage,omitempty"`
	StartedAt     *time.Time `gorm:"column:started_at" json:"startedAt,omitempty"`
	FinishedAt    *time.Time `gorm:"column:finished_at" json:"finishedAt,omitempty"`
}

func (SkillRun) TableName() string {
	return "skill_run"
}
