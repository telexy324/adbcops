package model

import (
	"encoding/json"
	"time"
)

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

const (
	DocumentVersionStatusDraft      = "draft"
	DocumentVersionStatusProcessing = "processing"
	DocumentVersionStatusReviewing  = "reviewing"
	DocumentVersionStatusFailed     = "failed"
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

type KBDocumentVersion struct {
	ID             int64      `gorm:"column:id;primaryKey" json:"id"`
	DocumentID     int64      `gorm:"column:document_id;not null" json:"documentId"`
	Version        string     `gorm:"column:version;size:50;not null" json:"version"`
	RevisionNo     int        `gorm:"column:revision_no;not null" json:"revisionNo"`
	FileName       string     `gorm:"column:file_name;size:255;not null" json:"fileName"`
	FilePath       string     `gorm:"column:file_path;not null" json:"-"`
	FileType       string     `gorm:"column:file_type;size:50;not null" json:"fileType"`
	FileHash       string     `gorm:"column:file_hash;size:128;not null" json:"fileHash"`
	ParserName     *string    `gorm:"column:parser_name;size:100" json:"parserName,omitempty"`
	ParserVersion  *string    `gorm:"column:parser_version;size:50" json:"parserVersion,omitempty"`
	Language       *string    `gorm:"column:language;size:30" json:"language,omitempty"`
	Status         string     `gorm:"column:status;size:30;not null" json:"status"`
	Metadata       []byte     `gorm:"column:metadata;type:jsonb" json:"metadata,omitempty"`
	DocumentSchema []byte     `gorm:"column:document_schema;type:jsonb" json:"documentSchema,omitempty"`
	ParseQuality   []byte     `gorm:"column:parse_quality;type:jsonb" json:"parseQuality,omitempty"`
	ContentSummary *string    `gorm:"column:content_summary" json:"contentSummary,omitempty"`
	ValidFrom      *time.Time `gorm:"column:valid_from" json:"validFrom,omitempty"`
	ValidUntil     *time.Time `gorm:"column:valid_until" json:"validUntil,omitempty"`
	ReviewDueAt    *time.Time `gorm:"column:review_due_at" json:"reviewDueAt,omitempty"`
	CreatedBy      *int64     `gorm:"column:created_by" json:"createdBy,omitempty"`
	ReviewedBy     *int64     `gorm:"column:reviewed_by" json:"reviewedBy,omitempty"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
	ReviewedAt     *time.Time `gorm:"column:reviewed_at" json:"reviewedAt,omitempty"`
}

func (KBDocumentVersion) TableName() string { return "kb_document_version" }

type KBDocumentBlock struct {
	ID                int64     `gorm:"column:id;primaryKey" json:"id"`
	DocumentVersionID int64     `gorm:"column:document_version_id;not null" json:"documentVersionId"`
	BlockKey          string    `gorm:"column:block_key;size:100;not null" json:"blockKey"`
	ParentBlockID     *int64    `gorm:"column:parent_block_id" json:"parentBlockId,omitempty"`
	BlockType         string    `gorm:"column:block_type;size:50;not null" json:"blockType"`
	Level             int       `gorm:"column:level" json:"level"`
	OrderNo           int       `gorm:"column:order_no;not null" json:"orderNo"`
	PageNo            *int      `gorm:"column:page_no" json:"pageNo,omitempty"`
	SectionPath       []byte    `gorm:"column:section_path;type:jsonb" json:"sectionPath,omitempty"`
	TextContent       string    `gorm:"column:text_content" json:"textContent"`
	Attributes        []byte    `gorm:"column:attributes;type:jsonb" json:"attributes,omitempty"`
	ContentHash       *string   `gorm:"column:content_hash;size:128" json:"contentHash,omitempty"`
	CreatedAt         time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	ParentBlockKey    *string   `gorm:"-" json:"-"`
}

func (KBDocumentBlock) TableName() string { return "kb_document_block" }

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
	return "kb_quality_standard_legacy"
}

type KBChunk struct {
	ID                int64           `gorm:"column:id;primaryKey" json:"id"`
	DocumentID        int64           `gorm:"column:document_id;not null" json:"documentId"`
	DocumentVersionID int64           `gorm:"column:document_version_id;not null" json:"documentVersionId"`
	StrategyID        int64           `gorm:"column:strategy_id;not null" json:"strategyId"`
	ParentChunkID     *int64          `gorm:"column:parent_chunk_id" json:"parentChunkId,omitempty"`
	ChunkIndex        int             `gorm:"column:chunk_index;not null" json:"chunkIndex"`
	ChunkType         string          `gorm:"column:chunk_type;size:50;not null" json:"chunkType"`
	SectionPath       json.RawMessage `gorm:"column:section_path;type:jsonb" json:"sectionPath,omitempty"`
	SourceBlockIDs    json.RawMessage `gorm:"column:source_block_ids;type:jsonb;not null" json:"sourceBlockIds"`
	SourcePageStart   *int            `gorm:"column:source_page_start" json:"sourcePageStart,omitempty"`
	SourcePageEnd     *int            `gorm:"column:source_page_end" json:"sourcePageEnd,omitempty"`
	Content           string          `gorm:"column:content;not null" json:"content"`
	ContextBefore     *string         `gorm:"column:context_before" json:"contextBefore,omitempty"`
	ContextAfter      *string         `gorm:"column:context_after" json:"contextAfter,omitempty"`
	TokenCount        int             `gorm:"column:token_count" json:"tokenCount"`
	ContentHash       string          `gorm:"column:content_hash;size:128;not null" json:"contentHash"`
	SiblingGroup      *string         `gorm:"column:sibling_group;size:128" json:"siblingGroup,omitempty"`
	SemanticUnit      *string         `gorm:"column:semantic_unit;size:80" json:"semanticUnit,omitempty"`
	SourceTitle       *string         `gorm:"column:source_title;size:255" json:"sourceTitle,omitempty"`
	SourceSection     *string         `gorm:"column:source_section;size:255" json:"sourceSection,omitempty"`
	SourcePage        *int            `gorm:"column:source_page" json:"sourcePage,omitempty"`
	Summary           *string         `gorm:"column:summary" json:"summary,omitempty"`
	SearchText        *string         `gorm:"column:search_text" json:"searchText,omitempty"`
	Keywords          []byte          `gorm:"column:keywords;type:jsonb" json:"keywords,omitempty"`
	PossibleQuestions []byte          `gorm:"column:possible_questions;type:jsonb" json:"possibleQuestions,omitempty"`
	CreatedAt         time.Time       `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	ParentChunkIndex  *int            `gorm:"-" json:"-"`
}

func (KBChunk) TableName() string {
	return "kb_chunk"
}

type KBChunkStrategy struct {
	ID                 int64           `gorm:"column:id;primaryKey" json:"id"`
	Name               string          `gorm:"column:name;size:120;not null" json:"name"`
	Version            string          `gorm:"column:version;size:50;not null" json:"version"`
	ApplicableDocTypes json.RawMessage `gorm:"column:applicable_doc_types;type:jsonb" json:"applicableDocTypes,omitempty"`
	Config             json.RawMessage `gorm:"column:config;type:jsonb;not null" json:"config"`
	Enabled            bool            `gorm:"column:enabled;not null" json:"enabled"`
	CreatedBy          *int64          `gorm:"column:created_by" json:"createdBy,omitempty"`
	CreatedAt          time.Time       `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt          time.Time       `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (KBChunkStrategy) TableName() string { return "kb_chunk_strategy" }

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
