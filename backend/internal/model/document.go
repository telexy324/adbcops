package model

import "time"

const (
	DocumentStatusDraft      = "draft"
	DocumentStatusReviewing  = "reviewing"
	DocumentStatusRejected   = "rejected"
	DocumentStatusPublished  = "published"
	DocumentStatusArchived   = "archived"
	DocumentStatusDeprecated = "deprecated"

	DocumentFileTypeMarkdown = "md"
	DocumentFileTypeText     = "txt"
	DocumentFileTypeDocx     = "docx"
	DocumentFileTypeXlsx     = "xlsx"
	DocumentFileTypePDF      = "pdf"
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

const (
	DocumentReviewActionPublish   = "publish"
	DocumentReviewActionReject    = "reject"
	DocumentReviewActionArchive   = "archive"
	DocumentReviewActionDeprecate = "deprecate"
	DocumentReviewActionQuality   = "quality"
)

func (KBDocument) TableName() string {
	return "kb_document"
}

type KBQualityStandard struct {
	ID        int64     `gorm:"column:id;primaryKey" json:"id"`
	Title     string    `gorm:"column:title;size:255;not null" json:"title"`
	FileName  string    `gorm:"column:file_name;size:255;not null" json:"fileName"`
	FilePath  string    `gorm:"column:file_path;not null" json:"filePath"`
	FileType  string    `gorm:"column:file_type;size:50;not null" json:"fileType"`
	Content   string    `gorm:"column:content;not null" json:"content"`
	Enabled   bool      `gorm:"column:enabled;not null" json:"enabled"`
	CreatedBy *int64    `gorm:"column:created_by" json:"createdBy,omitempty"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (KBQualityStandard) TableName() string {
	return "kb_quality_standard"
}

type KBChunk struct {
	ID                int64     `gorm:"column:id;primaryKey" json:"id"`
	DocumentID        int64     `gorm:"column:document_id;not null" json:"documentId"`
	ChunkIndex        int       `gorm:"column:chunk_index;not null" json:"chunkIndex"`
	Content           string    `gorm:"column:content;not null" json:"content"`
	SourceTitle       *string   `gorm:"column:source_title;size:255" json:"sourceTitle,omitempty"`
	SourceSection     *string   `gorm:"column:source_section;size:255" json:"sourceSection,omitempty"`
	SourcePage        *int      `gorm:"column:source_page" json:"sourcePage,omitempty"`
	TokenCount        int       `gorm:"column:token_count" json:"tokenCount"`
	Summary           *string   `gorm:"column:summary" json:"summary,omitempty"`
	SearchText        *string   `gorm:"column:search_text" json:"searchText,omitempty"`
	Keywords          []byte    `gorm:"column:keywords;type:jsonb" json:"keywords,omitempty"`
	PossibleQuestions []byte    `gorm:"column:possible_questions;type:jsonb" json:"possibleQuestions,omitempty"`
	CreatedAt         time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
}

func (KBChunk) TableName() string {
	return "kb_chunk"
}

type KBChunkEmbedding struct {
	ID          int64     `gorm:"column:id;primaryKey" json:"id"`
	ChunkID     int64     `gorm:"column:chunk_id;not null" json:"chunkId"`
	LLMConfigID *int64    `gorm:"column:llm_config_id" json:"llmConfigId,omitempty"`
	Model       string    `gorm:"column:model;size:255;not null" json:"model"`
	Dimension   int       `gorm:"column:dimension;not null" json:"dimension"`
	Embedding   []byte    `gorm:"column:embedding;type:jsonb;not null" json:"embedding,omitempty"`
	Chunk       KBChunk   `gorm:"foreignKey:ChunkID;references:ID" json:"chunk,omitempty"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (KBChunkEmbedding) TableName() string {
	return "kb_chunk_embedding"
}

type KBDocumentReview struct {
	ID         int64     `gorm:"column:id;primaryKey" json:"id"`
	DocumentID int64     `gorm:"column:document_id;not null" json:"documentId"`
	ReviewerID int64     `gorm:"column:reviewer_id;not null" json:"reviewerId"`
	Action     string    `gorm:"column:action;size:50;not null" json:"action"`
	FromStatus string    `gorm:"column:from_status;size:50;not null" json:"fromStatus"`
	ToStatus   string    `gorm:"column:to_status;size:50;not null" json:"toStatus"`
	Comment    *string   `gorm:"column:comment" json:"comment,omitempty"`
	CreatedAt  time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
}

func (KBDocumentReview) TableName() string {
	return "kb_document_review"
}
