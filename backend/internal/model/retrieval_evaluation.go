package model

import (
	"encoding/json"
	"time"
)

const (
	RetrievalEvaluationModeSmoke = "smoke"
	RetrievalEvaluationModeLab   = "lab"

	RetrievalEvaluationRunning   = "running"
	RetrievalEvaluationCompleted = "completed"
	RetrievalEvaluationFailed    = "failed"
)

type KBRetrievalTestCase struct {
	ID                  int64           `gorm:"column:id;primaryKey" json:"id"`
	DocumentID          *int64          `gorm:"column:document_id" json:"documentId,omitempty"`
	DocumentVersionID   *int64          `gorm:"column:document_version_id" json:"documentVersionId,omitempty"`
	Question            string          `gorm:"column:question;not null" json:"question"`
	Category            string          `gorm:"column:category;size:30;not null" json:"category"`
	ExpectedDocumentIDs json.RawMessage `gorm:"column:expected_document_ids;type:jsonb;not null" json:"expectedDocumentIds"`
	ExpectedChunkIDs    json.RawMessage `gorm:"column:expected_chunk_ids;type:jsonb;not null" json:"expectedChunkIds"`
	ExpectedSections    json.RawMessage `gorm:"column:expected_sections;type:jsonb;not null" json:"expectedSections"`
	MustIncludeFacts    json.RawMessage `gorm:"column:must_include_facts;type:jsonb;not null" json:"mustIncludeFacts"`
	MustNotInclude      json.RawMessage `gorm:"column:must_not_include;type:jsonb;not null" json:"mustNotInclude"`
	ExpectNoAnswer      bool            `gorm:"column:expect_no_answer;not null" json:"expectNoAnswer"`
	Source              string          `gorm:"column:source;size:30;not null" json:"source"`
	Enabled             bool            `gorm:"column:enabled;not null" json:"enabled"`
	CreatedBy           int64           `gorm:"column:created_by;not null" json:"createdBy"`
	CreatedAt           time.Time       `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt           time.Time       `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (KBRetrievalTestCase) TableName() string { return "kb_retrieval_test_case" }

type KBRetrievalEvaluationRun struct {
	ID                     int64                         `gorm:"column:id;primaryKey" json:"id"`
	Mode                   string                        `gorm:"column:mode;size:20;not null" json:"mode"`
	Name                   string                        `gorm:"column:name;size:120;not null" json:"name"`
	Status                 string                        `gorm:"column:status;size:20;not null" json:"status"`
	DocumentVersionID      *int64                        `gorm:"column:document_version_id" json:"documentVersionId,omitempty"`
	EmbeddingConfigID      *int64                        `gorm:"column:embedding_config_id" json:"embeddingConfigId,omitempty"`
	EmbeddingModel         *string                       `gorm:"column:embedding_model;size:120" json:"embeddingModel,omitempty"`
	EmbeddingModelRevision *string                       `gorm:"column:embedding_model_revision;size:120" json:"embeddingModelRevision,omitempty"`
	RerankConfigID         *int64                        `gorm:"column:rerank_config_id" json:"rerankConfigId,omitempty"`
	RerankModel            *string                       `gorm:"column:rerank_model;size:120" json:"rerankModel,omitempty"`
	ChunkStrategyID        *int64                        `gorm:"column:chunk_strategy_id" json:"chunkStrategyId,omitempty"`
	RetrievalConfig        json.RawMessage               `gorm:"column:retrieval_config;type:jsonb;not null" json:"retrievalConfig"`
	Thresholds             json.RawMessage               `gorm:"column:thresholds;type:jsonb;not null" json:"thresholds"`
	Metrics                json.RawMessage               `gorm:"column:metrics;type:jsonb;not null" json:"metrics"`
	CaseCount              int                           `gorm:"column:case_count;not null" json:"caseCount"`
	Passed                 bool                          `gorm:"column:passed;not null" json:"passed"`
	ErrorMessage           *string                       `gorm:"column:error_message" json:"errorMessage,omitempty"`
	CreatedBy              int64                         `gorm:"column:created_by;not null" json:"createdBy"`
	CreatedAt              time.Time                     `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	CompletedAt            *time.Time                    `gorm:"column:completed_at" json:"completedAt,omitempty"`
	Results                []KBRetrievalEvaluationResult `gorm:"foreignKey:RunID" json:"results,omitempty"`
}

func (KBRetrievalEvaluationRun) TableName() string { return "kb_retrieval_evaluation_run" }

type KBRetrievalEvaluationResult struct {
	ID                   int64           `gorm:"column:id;primaryKey" json:"id"`
	RunID                int64           `gorm:"column:run_id;not null" json:"runId"`
	TestCaseID           int64           `gorm:"column:test_case_id;not null" json:"testCaseId"`
	RetrievedDocumentIDs json.RawMessage `gorm:"column:retrieved_document_ids;type:jsonb;not null" json:"retrievedDocumentIds"`
	RetrievedChunkIDs    json.RawMessage `gorm:"column:retrieved_chunk_ids;type:jsonb;not null" json:"retrievedChunkIds"`
	CitationIDs          json.RawMessage `gorm:"column:citation_ids;type:jsonb;not null" json:"citationIds"`
	ContextText          string          `gorm:"column:context_text;not null" json:"contextText"`
	Metrics              json.RawMessage `gorm:"column:metrics;type:jsonb;not null" json:"metrics"`
	RetrievalTrace       json.RawMessage `gorm:"column:retrieval_trace;type:jsonb;not null" json:"retrievalTrace"`
	Passed               bool            `gorm:"column:passed;not null" json:"passed"`
	ErrorMessage         *string         `gorm:"column:error_message" json:"errorMessage,omitempty"`
	CreatedAt            time.Time       `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
}

func (KBRetrievalEvaluationResult) TableName() string { return "kb_retrieval_evaluation_result" }
