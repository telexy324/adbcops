package model

import "time"

const (
	DocumentStatusDraft = "draft"

	DocumentFileTypeMarkdown = "md"
	DocumentFileTypeText     = "txt"
)

type KBDocument struct {
	ID            int64      `gorm:"column:id;primaryKey" json:"id"`
	Title         string     `gorm:"column:title;size:255;not null" json:"title"`
	FileName      string     `gorm:"column:file_name;size:255;not null" json:"fileName"`
	FilePath      string     `gorm:"column:file_path;not null" json:"filePath"`
	FileType      string     `gorm:"column:file_type;size:50;not null" json:"fileType"`
	SystemName    *string    `gorm:"column:system_name;size:100" json:"systemName,omitempty"`
	ComponentName *string    `gorm:"column:component_name;size:100" json:"componentName,omitempty"`
	Environment   *string    `gorm:"column:environment;size:50" json:"environment,omitempty"`
	DocType       *string    `gorm:"column:doc_type;size:100" json:"docType,omitempty"`
	Version       string     `gorm:"column:version;size:50" json:"version"`
	Status        string     `gorm:"column:status;size:50" json:"status"`
	Tags          []byte     `gorm:"column:tags;type:jsonb" json:"tags,omitempty"`
	Summary       *string    `gorm:"column:summary" json:"summary,omitempty"`
	ValidFrom     *time.Time `gorm:"column:valid_from" json:"validFrom,omitempty"`
	ValidUntil    *time.Time `gorm:"column:valid_until" json:"validUntil,omitempty"`
	QualityScore  int        `gorm:"column:quality_score" json:"qualityScore"`
	QualityResult []byte     `gorm:"column:quality_result;type:jsonb" json:"qualityResult,omitempty"`
	CreatedBy     *int64     `gorm:"column:created_by" json:"createdBy,omitempty"`
	ReviewedBy    *int64     `gorm:"column:reviewed_by" json:"reviewedBy,omitempty"`
	CreatedAt     time.Time  `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt     time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
	ReviewedAt    *time.Time `gorm:"column:reviewed_at" json:"reviewedAt,omitempty"`
}

func (KBDocument) TableName() string {
	return "kb_document"
}
