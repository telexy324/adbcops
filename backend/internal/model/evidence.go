package model

import (
	"encoding/json"
	"time"
)

const (
	EvidenceSensitivityPublic       = "public"
	EvidenceSensitivityInternal     = "internal"
	EvidenceSensitivityConfidential = "confidential"
	EvidenceSensitivityRestricted   = "restricted"

	EvidenceKindFact            = "fact"
	EvidenceKindRule            = "rule"
	EvidenceKindKnowledge       = "knowledge"
	EvidenceKindModelHypothesis = "model_hypothesis"
)

type EvidenceRecord struct {
	ID           int64           `gorm:"column:id;primaryKey" json:"id"`
	EvidenceKey  string          `gorm:"column:evidence_key;size:100;not null;unique" json:"evidenceKey"`
	SourceType   string          `gorm:"column:source_type;size:50;not null" json:"sourceType"`
	SourceRef    json.RawMessage `gorm:"column:source_ref;type:jsonb" json:"sourceRef,omitempty"`
	ObservedAt   *time.Time      `gorm:"column:observed_at" json:"observedAt,omitempty"`
	Title        *string         `gorm:"column:title;size:255" json:"title,omitempty"`
	Summary      string          `gorm:"column:summary;not null" json:"summary"`
	Content      json.RawMessage `gorm:"column:content;type:jsonb" json:"content,omitempty"`
	Confidence   *float64        `gorm:"column:confidence" json:"confidence,omitempty"`
	Sensitivity  *string         `gorm:"column:sensitivity;size:30" json:"sensitivity,omitempty"`
	RCARunID     *int64          `gorm:"column:rca_run_id" json:"rcaRunId,omitempty"`
	RCARoundID   *int64          `gorm:"column:rca_round_id" json:"rcaRoundId,omitempty"`
	RCAActionID  *int64          `gorm:"column:rca_action_id" json:"rcaActionId,omitempty"`
	EvidenceKind *string         `gorm:"column:evidence_kind;size:40" json:"evidenceKind,omitempty"`
	Entity       json.RawMessage `gorm:"column:entity;type:jsonb" json:"entity,omitempty"`
	WindowStart  *time.Time      `gorm:"column:window_start" json:"windowStart,omitempty"`
	WindowEnd    *time.Time      `gorm:"column:window_end" json:"windowEnd,omitempty"`
	SourceSkill  *string         `gorm:"column:source_skill;size:120" json:"sourceSkill,omitempty"`
	DataSourceID *int64          `gorm:"column:data_source_id" json:"dataSourceId,omitempty"`
	OwnerUserID  *int64          `gorm:"column:owner_user_id" json:"ownerUserId,omitempty"`
	CreatedAt    time.Time       `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
}

func (EvidenceRecord) TableName() string {
	return "evidence"
}
