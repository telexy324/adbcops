package model

import "time"

const (
	EmbeddingIndexPending  = "pending"
	EmbeddingIndexBuilding = "building"
	EmbeddingIndexReady    = "ready"
	EmbeddingIndexStale    = "stale"
	EmbeddingIndexFailed   = "failed"
)

type KBEmbeddingIndex struct {
	ID                 int64      `gorm:"column:id;primaryKey" json:"id"`
	DocumentVersionID  int64      `gorm:"column:document_version_id;not null" json:"documentVersionId"`
	StrategyID         int64      `gorm:"column:strategy_id;not null" json:"strategyId"`
	EmbeddingConfigID  int64      `gorm:"column:embedding_config_id;not null" json:"embeddingConfigId"`
	ModelName          string     `gorm:"column:model_name;size:120;not null" json:"modelName"`
	ModelRevision      string     `gorm:"column:model_revision;size:120;not null" json:"modelRevision"`
	Dimension          int        `gorm:"column:dimension;not null" json:"dimension"`
	Normalized         bool       `gorm:"column:normalized;not null" json:"normalized"`
	DistanceMetric     string     `gorm:"column:distance_metric;size:30;not null" json:"distanceMetric"`
	Status             string     `gorm:"column:status;size:30;not null" json:"status"`
	ChunkCount         int        `gorm:"column:chunk_count;not null" json:"chunkCount"`
	EmbeddedCount      int        `gorm:"column:embedded_count;not null" json:"embeddedCount"`
	ContentFingerprint string     `gorm:"column:content_fingerprint;size:128;not null" json:"contentFingerprint"`
	ErrorMessage       *string    `gorm:"column:error_message" json:"errorMessage,omitempty"`
	HNSWEnabled        bool       `gorm:"column:hnsw_enabled;not null" json:"hnswEnabled"`
	HNSWM              int        `gorm:"column:hnsw_m;not null" json:"hnswM"`
	HNSWEFConstruction int        `gorm:"column:hnsw_ef_construction;not null" json:"hnswEfConstruction"`
	CreatedBy          *int64     `gorm:"column:created_by" json:"createdBy,omitempty"`
	CreatedAt          time.Time  `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt          time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
	CompletedAt        *time.Time `gorm:"column:completed_at" json:"completedAt,omitempty"`
}

func (KBEmbeddingIndex) TableName() string { return "kb_embedding_index" }
