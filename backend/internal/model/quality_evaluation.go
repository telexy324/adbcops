package model

import (
	"encoding/json"
	"time"
)

type KBQualityEvaluation struct {
	ID                     int64                 `gorm:"column:id;primaryKey" json:"id"`
	DocumentVersionID      int64                 `gorm:"column:document_version_id;not null" json:"documentVersionId"`
	QualityProfileID       int64                 `gorm:"column:quality_profile_id;not null" json:"qualityProfileId"`
	QualityProfileVersion  string                `gorm:"column:quality_profile_version;size:50;not null" json:"qualityProfileVersion"`
	Mode                   string                `gorm:"column:mode;size:30;not null" json:"mode"`
	SelectedCriteria       json.RawMessage       `gorm:"column:selected_criteria;type:jsonb;not null" json:"selectedCriteria"`
	RequestFingerprint     *string               `gorm:"column:request_fingerprint;size:64" json:"-"`
	ParseScore             *float64              `gorm:"column:parse_score;type:numeric(8,2)" json:"parseScore,omitempty"`
	ContentScore           *float64              `gorm:"column:content_score;type:numeric(8,2)" json:"contentScore,omitempty"`
	RetrievalScore         *float64              `gorm:"column:retrieval_score;type:numeric(8,2)" json:"retrievalScore,omitempty"`
	TotalScore             *float64              `gorm:"column:total_score;type:numeric(8,2)" json:"totalScore,omitempty"`
	GateStatus             string                `gorm:"column:gate_status;size:30;not null" json:"gateStatus"`
	Level                  *string               `gorm:"column:level;size:30" json:"level,omitempty"`
	Source                 string                `gorm:"column:source;size:50;not null" json:"source"`
	ModelConfigID          *int64                `gorm:"column:model_config_id" json:"modelConfigId,omitempty"`
	Summary                *string               `gorm:"column:summary" json:"summary,omitempty"`
	Result                 json.RawMessage       `gorm:"column:result;type:jsonb" json:"result,omitempty"`
	Status                 string                `gorm:"column:status;size:30;not null" json:"status"`
	ReviewStatus           string                `gorm:"column:review_status;size:30;not null" json:"reviewStatus"`
	PublishedBy            *int64                `gorm:"column:published_by" json:"publishedBy,omitempty"`
	PublishedAt            *time.Time            `gorm:"column:published_at" json:"publishedAt,omitempty"`
	SupersedesEvaluationID *int64                `gorm:"column:supersedes_evaluation_id" json:"supersedesEvaluationId,omitempty"`
	CreatedAt              time.Time             `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	CompletedAt            *time.Time            `gorm:"column:completed_at" json:"completedAt,omitempty"`
	RuleResults            []KBQualityRuleResult `gorm:"foreignKey:EvaluationID" json:"ruleResults,omitempty"`
}

func (KBQualityEvaluation) TableName() string { return "kb_quality_evaluation" }

type KBQualityRuleResult struct {
	ID                 int64           `gorm:"column:id;primaryKey" json:"id"`
	EvaluationID       int64           `gorm:"column:evaluation_id;not null" json:"evaluationId"`
	CriterionKey       string          `gorm:"column:criterion_key;size:120;not null" json:"criterionKey"`
	RuleKey            string          `gorm:"column:rule_key;size:160;not null" json:"ruleKey"`
	Score              *float64        `gorm:"column:score;type:numeric(8,2)" json:"score,omitempty"`
	MaxScore           *float64        `gorm:"column:max_score;type:numeric(8,2)" json:"maxScore,omitempty"`
	FindingStatus      *string         `gorm:"column:finding_status;size:50" json:"status,omitempty"`
	Confidence         *float64        `gorm:"column:confidence;type:numeric(5,4)" json:"confidence,omitempty"`
	Evidence           json.RawMessage `gorm:"column:evidence;type:jsonb;not null" json:"evidence"`
	DeductionReason    *string         `gorm:"column:deduction_reason" json:"deductionReason,omitempty"`
	Suggestion         *string         `gorm:"column:suggestion" json:"suggestion,omitempty"`
	Source             string          `gorm:"column:source;size:50;not null" json:"source"`
	ManuallyOverridden bool            `gorm:"column:manually_overridden;not null" json:"manuallyOverridden"`
	OverriddenBy       *int64          `gorm:"column:overridden_by" json:"overriddenBy,omitempty"`
	OverrideComment    *string         `gorm:"column:override_comment" json:"overrideComment,omitempty"`
}

func (KBQualityRuleResult) TableName() string { return "kb_quality_rule_result" }

type KBQualityEvaluationOverride struct {
	ID               int64     `gorm:"column:id;primaryKey" json:"id"`
	EvaluationID     int64     `gorm:"column:evaluation_id;not null" json:"evaluationId"`
	RuleResultID     int64     `gorm:"column:rule_result_id;not null" json:"ruleResultId"`
	PreviousScore    *float64  `gorm:"column:previous_score;type:numeric(8,2)" json:"previousScore,omitempty"`
	OverriddenScore  *float64  `gorm:"column:overridden_score;type:numeric(8,2)" json:"overriddenScore,omitempty"`
	PreviousStatus   *string   `gorm:"column:previous_status;size:50" json:"previousStatus,omitempty"`
	OverriddenStatus *string   `gorm:"column:overridden_status;size:50" json:"overriddenStatus,omitempty"`
	Comment          string    `gorm:"column:comment;not null" json:"comment"`
	CreatedBy        int64     `gorm:"column:created_by;not null" json:"createdBy"`
	CreatedAt        time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
}

func (KBQualityEvaluationOverride) TableName() string {
	return "kb_quality_evaluation_override"
}
