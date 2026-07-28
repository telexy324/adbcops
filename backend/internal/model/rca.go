package model

import (
	"encoding/json"
	"time"
)

const (
	RCARunStatusPending        = "pending"
	RCARunStatusRunning        = "running"
	RCARunStatusPartialSuccess = "partial_success"
	RCARunStatusSuccess        = "success"
	RCARunStatusFailed         = "failed"
	RCARunStatusCancelled      = "cancelled"
	RCARunStatusTimedOut       = "timed_out"

	RCARoundStatusPending        = "pending"
	RCARoundStatusRunning        = "running"
	RCARoundStatusPartialSuccess = "partial_success"
	RCARoundStatusSuccess        = "success"
	RCARoundStatusFailed         = "failed"
	RCARoundStatusCancelled      = "cancelled"
	RCARoundStatusTimedOut       = "timed_out"

	RCAActionStatusPending        = "pending"
	RCAActionStatusRunning        = "running"
	RCAActionStatusPartialSuccess = "partial_success"
	RCAActionStatusSuccess        = "success"
	RCAActionStatusFailed         = "failed"
	RCAActionStatusSkipped        = "skipped"
	RCAActionStatusCancelled      = "cancelled"
	RCAActionStatusTimedOut       = "timed_out"
)

type RCARun struct {
	ID                int64           `gorm:"column:id;primaryKey" json:"id"`
	UserID            int64           `gorm:"column:user_id;not null" json:"userId"`
	ConversationID    *int64          `gorm:"column:conversation_id" json:"conversationId,omitempty"`
	IncidentID        *int64          `gorm:"column:incident_id" json:"incidentId,omitempty"`
	WorkflowRunID     *int64          `gorm:"column:workflow_run_id" json:"workflowRunId,omitempty"`
	Status            string          `gorm:"column:status;size:30;not null" json:"status"`
	Query             string          `gorm:"column:query;not null" json:"query"`
	Scope             json.RawMessage `gorm:"column:scope;type:jsonb;not null" json:"scope"`
	CurrentRound      int             `gorm:"column:current_round;not null" json:"currentRound"`
	MaxRounds         int             `gorm:"column:max_rounds;not null" json:"maxRounds"`
	TimeoutAt         *time.Time      `gorm:"column:timeout_at" json:"timeoutAt,omitempty"`
	CancelRequestedAt *time.Time      `gorm:"column:cancel_requested_at" json:"cancelRequestedAt,omitempty"`
	ErrorCode         *string         `gorm:"column:error_code;size:80" json:"errorCode,omitempty"`
	ErrorMessage      *string         `gorm:"column:error_message" json:"errorMessage,omitempty"`
	StartedAt         *time.Time      `gorm:"column:started_at" json:"startedAt,omitempty"`
	FinishedAt        *time.Time      `gorm:"column:finished_at" json:"finishedAt,omitempty"`
	CreatedAt         time.Time       `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt         time.Time       `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (RCARun) TableName() string { return "rca_run" }

type RCARound struct {
	ID                 int64           `gorm:"column:id;primaryKey" json:"id"`
	RunID              int64           `gorm:"column:run_id;not null" json:"runId"`
	RoundNumber        int             `gorm:"column:round_number;not null" json:"roundNumber"`
	Status             string          `gorm:"column:status;size:30;not null" json:"status"`
	InputHypotheses    json.RawMessage `gorm:"column:input_hypotheses;type:jsonb;not null" json:"inputHypotheses"`
	NewEvidenceIDs     json.RawMessage `gorm:"column:new_evidence_ids;type:jsonb;not null" json:"newEvidenceIds"`
	RejectedHypotheses json.RawMessage `gorm:"column:rejected_hypotheses;type:jsonb;not null" json:"rejectedHypotheses"`
	NextActions        json.RawMessage `gorm:"column:next_actions;type:jsonb;not null" json:"nextActions"`
	ErrorCode          *string         `gorm:"column:error_code;size:80" json:"errorCode,omitempty"`
	StartedAt          *time.Time      `gorm:"column:started_at" json:"startedAt,omitempty"`
	FinishedAt         *time.Time      `gorm:"column:finished_at" json:"finishedAt,omitempty"`
	CreatedAt          time.Time       `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt          time.Time       `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (RCARound) TableName() string { return "rca_round" }

type RCAAction struct {
	ID            int64           `gorm:"column:id;primaryKey" json:"id"`
	RunID         int64           `gorm:"column:run_id;not null" json:"runId"`
	RoundID       int64           `gorm:"column:round_id;not null" json:"roundId"`
	ActionKey     string          `gorm:"column:action_key;size:160;not null" json:"actionKey"`
	SkillName     string          `gorm:"column:skill_name;size:120;not null" json:"skillName"`
	Status        string          `gorm:"column:status;size:30;not null" json:"status"`
	Input         json.RawMessage `gorm:"column:input;type:jsonb;not null" json:"input"`
	Output        json.RawMessage `gorm:"column:output;type:jsonb" json:"output,omitempty"`
	EvidenceIDs   json.RawMessage `gorm:"column:evidence_ids;type:jsonb;not null" json:"evidenceIds"`
	ErrorCode     *string         `gorm:"column:error_code;size:80" json:"errorCode,omitempty"`
	ErrorMessage  *string         `gorm:"column:error_message" json:"errorMessage,omitempty"`
	SensitiveRead bool            `gorm:"column:sensitive_read;not null" json:"sensitiveRead"`
	Attempt       int             `gorm:"column:attempt;not null" json:"attempt"`
	StartedAt     *time.Time      `gorm:"column:started_at" json:"startedAt,omitempty"`
	FinishedAt    *time.Time      `gorm:"column:finished_at" json:"finishedAt,omitempty"`
	CreatedAt     time.Time       `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt     time.Time       `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (RCAAction) TableName() string { return "rca_action" }

type RCARootCauseCandidate struct {
	ID          int64           `gorm:"column:id;primaryKey" json:"id"`
	RunID       int64           `gorm:"column:run_id;not null" json:"runId"`
	RoundID     int64           `gorm:"column:round_id;not null" json:"roundId"`
	Summary     string          `gorm:"column:summary;not null" json:"summary"`
	Confidence  float64         `gorm:"column:confidence;not null" json:"confidence"`
	EvidenceIDs json.RawMessage `gorm:"column:evidence_ids;type:jsonb;not null" json:"evidenceIds"`
	Rejected    bool            `gorm:"column:rejected;not null" json:"rejected"`
	CreatedAt   time.Time       `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt   time.Time       `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (RCARootCauseCandidate) TableName() string { return "rca_root_cause_candidate" }
