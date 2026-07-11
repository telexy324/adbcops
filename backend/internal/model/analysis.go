package model

import "time"

const (
	AnalysisTaskStatusRunning = "running"
	AnalysisTaskStatusSuccess = "success"
	AnalysisTaskStatusFailed  = "failed"

	AnalysisTaskTypeGeneral = "general"
)

type AnalysisTask struct {
	ID             int64      `gorm:"column:id;primaryKey" json:"id"`
	UserID         int64      `gorm:"column:user_id;not null" json:"userId"`
	ConversationID *int64     `gorm:"column:conversation_id" json:"conversationId,omitempty"`
	TaskType       string     `gorm:"column:task_type;size:50;not null" json:"taskType"`
	Question       string     `gorm:"column:question;not null" json:"question"`
	Scope          []byte     `gorm:"column:scope;type:jsonb" json:"scope,omitempty"`
	DataSourceIDs  []byte     `gorm:"column:data_source_ids;type:jsonb" json:"dataSourceIds,omitempty"`
	Status         string     `gorm:"column:status;size:30;not null" json:"status"`
	Summary        *string    `gorm:"column:summary" json:"summary,omitempty"`
	Result         []byte     `gorm:"column:result;type:jsonb" json:"result,omitempty"`
	ErrorMessage   *string    `gorm:"column:error_message" json:"errorMessage,omitempty"`
	StartedAt      *time.Time `gorm:"column:started_at" json:"startedAt,omitempty"`
	FinishedAt     *time.Time `gorm:"column:finished_at" json:"finishedAt,omitempty"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (AnalysisTask) TableName() string {
	return "analysis_task"
}
