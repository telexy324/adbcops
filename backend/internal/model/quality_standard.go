package model

import (
	"encoding/json"
	"time"
)

const (
	QualityStandardDraft      = "draft"
	QualityStandardPublished  = "published"
	QualityStandardDeprecated = "deprecated"
)

type KBStructuredQualityStandard struct {
	ID                      int64              `gorm:"column:id;primaryKey" json:"id"`
	Name                    string             `gorm:"column:name;size:255;not null" json:"name"`
	Description             *string            `gorm:"column:description" json:"description,omitempty"`
	SourceDocumentVersionID *int64             `gorm:"column:source_document_version_id" json:"sourceDocumentVersionId,omitempty"`
	Version                 string             `gorm:"column:version;size:50;not null" json:"version"`
	Status                  string             `gorm:"column:status;size:30;not null" json:"status"`
	EffectiveFrom           *time.Time         `gorm:"column:effective_from" json:"effectiveFrom,omitempty"`
	EffectiveUntil          *time.Time         `gorm:"column:effective_until" json:"effectiveUntil,omitempty"`
	CreatedBy               *int64             `gorm:"column:created_by" json:"createdBy,omitempty"`
	ApprovedBy              *int64             `gorm:"column:approved_by" json:"approvedBy,omitempty"`
	CreatedAt               time.Time          `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt               time.Time          `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
	Profiles                []KBQualityProfile `gorm:"foreignKey:StandardID" json:"profiles,omitempty"`
}

func (KBStructuredQualityStandard) TableName() string { return "kb_quality_standard" }

type KBQualityProfile struct {
	ID                     int64                `gorm:"column:id;primaryKey" json:"id"`
	StandardID             int64                `gorm:"column:standard_id;not null" json:"standardId"`
	ProfileKey             string               `gorm:"column:profile_key;size:120;not null" json:"profileKey"`
	Name                   string               `gorm:"column:name;size:255;not null" json:"name"`
	ApplicableDocTypes     json.RawMessage      `gorm:"column:applicable_doc_types;type:jsonb;not null" json:"applicableDocTypes"`
	ApplicableSystems      json.RawMessage      `gorm:"column:applicable_systems;type:jsonb" json:"applicableSystems,omitempty"`
	ApplicableEnvironments json.RawMessage      `gorm:"column:applicable_environments;type:jsonb" json:"applicableEnvironments,omitempty"`
	TotalScore             float64              `gorm:"column:total_score;type:numeric(8,2);not null" json:"totalScore"`
	PassScore              float64              `gorm:"column:pass_score;type:numeric(8,2);not null" json:"passScore"`
	WarningScore           float64              `gorm:"column:warning_score;type:numeric(8,2);not null" json:"warningScore"`
	GatePolicy             json.RawMessage      `gorm:"column:gate_policy;type:jsonb" json:"hardGatePolicy,omitempty"`
	Status                 string               `gorm:"column:status;size:30;not null" json:"status"`
	CreatedAt              time.Time            `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt              time.Time            `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
	Criteria               []KBQualityCriterion `gorm:"foreignKey:ProfileID" json:"criteria,omitempty"`
}

func (KBQualityProfile) TableName() string { return "kb_quality_profile" }

type KBQualityCriterion struct {
	ID            int64           `gorm:"column:id;primaryKey" json:"id"`
	ProfileID     int64           `gorm:"column:profile_id;not null" json:"profileId"`
	CriterionKey  string          `gorm:"column:criterion_key;size:120;not null" json:"criterionKey"`
	Name          string          `gorm:"column:name;size:255;not null" json:"name"`
	Description   *string         `gorm:"column:description" json:"description,omitempty"`
	Weight        float64         `gorm:"column:weight;type:numeric(8,4);not null" json:"weight"`
	MaxScore      float64         `gorm:"column:max_score;type:numeric(8,2);not null" json:"maxScore"`
	ScoringMethod string          `gorm:"column:scoring_method;size:30;not null" json:"scoringMethod"`
	EvidenceScope json.RawMessage `gorm:"column:evidence_scope;type:jsonb" json:"evidenceScope,omitempty"`
	OrderNo       int             `gorm:"column:order_no;not null" json:"order"`
	Rules         []KBQualityRule `gorm:"foreignKey:CriterionID" json:"rules,omitempty"`
}

func (KBQualityCriterion) TableName() string { return "kb_quality_criterion" }

type KBQualityRule struct {
	ID                  int64           `gorm:"column:id;primaryKey" json:"id"`
	CriterionID         int64           `gorm:"column:criterion_id;not null" json:"criterionId"`
	RuleKey             string          `gorm:"column:rule_key;size:160;not null" json:"ruleKey"`
	Name                string          `gorm:"column:name;size:255;not null" json:"name"`
	Description         *string         `gorm:"column:description" json:"description,omitempty"`
	RuleType            string          `gorm:"column:rule_type;size:50;not null" json:"ruleType"`
	Severity            string          `gorm:"column:severity;size:30;not null" json:"severity"`
	MaxScore            float64         `gorm:"column:max_score;type:numeric(8,2);not null" json:"maxScore"`
	Deduction           *float64        `gorm:"column:deduction;type:numeric(8,2)" json:"deduction,omitempty"`
	Required            bool            `gorm:"column:required;not null" json:"required"`
	HardGate            bool            `gorm:"column:hard_gate;not null" json:"hardGate"`
	EvidenceRequirement json.RawMessage `gorm:"column:evidence_requirement;type:jsonb" json:"evidenceRequirement,omitempty"`
	DetectorConfig      json.RawMessage `gorm:"column:detector_config;type:jsonb" json:"detectorConfig,omitempty"`
	LLMInstruction      *string         `gorm:"column:llm_instruction" json:"llmInstruction,omitempty"`
	Examples            json.RawMessage `gorm:"column:examples;type:jsonb" json:"examples,omitempty"`
	OrderNo             int             `gorm:"column:order_no;not null" json:"order"`
}

func (KBQualityRule) TableName() string { return "kb_quality_rule" }

type KBQualityStandardImport struct {
	ID               int64           `gorm:"column:id;primaryKey" json:"id"`
	StandardID       *int64          `gorm:"column:standard_id" json:"standardId,omitempty"`
	OriginalFileName string          `gorm:"column:original_file_name;size:255;not null" json:"originalFileName"`
	StoredFilePath   string          `gorm:"column:stored_file_path;not null" json:"-"`
	FileType         string          `gorm:"column:file_type;size:20;not null" json:"fileType"`
	FileSize         int64           `gorm:"column:file_size;not null" json:"fileSize"`
	FileHash         string          `gorm:"column:file_hash;size:64;not null" json:"fileHash"`
	ParserName       *string         `gorm:"column:parser_name;size:120" json:"parserName,omitempty"`
	ParserVersion    *string         `gorm:"column:parser_version;size:50" json:"parserVersion,omitempty"`
	Status           string          `gorm:"column:status;size:40;not null" json:"status"`
	Warnings         json.RawMessage `gorm:"column:warnings;type:jsonb;not null" json:"warnings"`
	ValidationErrors json.RawMessage `gorm:"column:validation_errors;type:jsonb;not null" json:"validationErrors"`
	Preview          json.RawMessage `gorm:"column:preview;type:jsonb" json:"preview,omitempty"`
	CreatedBy        *int64          `gorm:"column:created_by" json:"createdBy,omitempty"`
	CreatedAt        time.Time       `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt        time.Time       `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (KBQualityStandardImport) TableName() string { return "kb_quality_standard_import" }
