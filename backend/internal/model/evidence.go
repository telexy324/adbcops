package model

import "time"

const (
	EvidenceSensitivityPublic       = "public"
	EvidenceSensitivityInternal     = "internal"
	EvidenceSensitivityConfidential = "confidential"
	EvidenceSensitivityRestricted   = "restricted"
)

type EvidenceRecord struct {
	ID          int64      `gorm:"column:id;primaryKey" json:"id"`
	EvidenceKey string     `gorm:"column:evidence_key;size:100;not null;unique" json:"evidenceKey"`
	SourceType  string     `gorm:"column:source_type;size:50;not null" json:"sourceType"`
	SourceRef   []byte     `gorm:"column:source_ref;type:jsonb" json:"sourceRef,omitempty"`
	ObservedAt  *time.Time `gorm:"column:observed_at" json:"observedAt,omitempty"`
	Title       *string    `gorm:"column:title;size:255" json:"title,omitempty"`
	Summary     string     `gorm:"column:summary;not null" json:"summary"`
	Content     []byte     `gorm:"column:content;type:jsonb" json:"content,omitempty"`
	Confidence  *float64   `gorm:"column:confidence" json:"confidence,omitempty"`
	Sensitivity *string    `gorm:"column:sensitivity;size:30" json:"sensitivity,omitempty"`
	CreatedAt   time.Time  `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
}

func (EvidenceRecord) TableName() string {
	return "evidence"
}
