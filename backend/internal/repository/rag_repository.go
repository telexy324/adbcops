package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
)

type KnowledgeRetrievalFilter struct {
	PermissionScope        string    `json:"permissionScope"`
	SystemName             string    `json:"systemName,omitempty"`
	ComponentName          string    `json:"componentName,omitempty"`
	Environment            string    `json:"environment,omitempty"`
	DocTypes               []string  `json:"docTypes,omitempty"`
	MustHaveTerms          []string  `json:"mustHaveTerms,omitempty"`
	NegativeTerms          []string  `json:"negativeTerms,omitempty"`
	StrategyID             *int64    `json:"strategyId,omitempty"`
	EmbeddingModelRevision string    `json:"embeddingModelRevision,omitempty"`
	Now                    time.Time `json:"evaluatedAt"`
}

type RankedKnowledgeChunk struct {
	Chunk model.KBChunk
	Score float64
}

type RAGRepository interface {
	CreateConversation(ctx context.Context, conversation *model.Conversation) error
	FindConversationByID(ctx context.Context, id int64) (*model.Conversation, error)
	CreateMessage(ctx context.Context, message *model.Message) error
	SearchChunksTrigram(ctx context.Context, query string, filter KnowledgeRetrievalFilter, limit int) ([]RankedKnowledgeChunk, error)
	SearchChunksExact(ctx context.Context, terms []string, filter KnowledgeRetrievalFilter, limit int) ([]RankedKnowledgeChunk, error)
	SearchChunksTitleSection(ctx context.Context, query string, filter KnowledgeRetrievalFilter, limit int) ([]RankedKnowledgeChunk, error)
	SearchChunksPossibleQuestions(ctx context.Context, query string, filter KnowledgeRetrievalFilter, limit int) ([]RankedKnowledgeChunk, error)
	SearchChunksDense(ctx context.Context, vector []float64, configID int64, modelName string, filter KnowledgeRetrievalFilter, limit int) ([]RankedKnowledgeChunk, error)
	FindKnowledgeDocumentsByIDs(ctx context.Context, ids []int64) ([]model.KBDocument, error)
	FindKnowledgeChunksByIDs(ctx context.Context, ids []int64) ([]model.KBChunk, error)
	FindLLMConfigByID(ctx context.Context, id int64) (*model.LLMConfig, error)
	FindReadyEmbeddingModelRevision(ctx context.Context, configID int64, strategyID *int64) (string, error)
	FindDefaultEnabledLLMConfig(ctx context.Context) (*model.LLMConfig, error)
	FindDefaultEnabledLLMConfigByPurpose(ctx context.Context, purpose string) (*model.LLMConfig, error)
	CreateQARecord(ctx context.Context, record *model.QARecord) error
}

type rankedKnowledgeChunkRow struct {
	model.KBChunk
	RetrievalScore float64 `gorm:"column:retrieval_score"`
}

func (r *GORMRAGRepository) retrievalScope(ctx context.Context, filter KnowledgeRetrievalFilter) *gorm.DB {
	now := filter.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	query := r.db.WithContext(ctx).Table("kb_chunk").
		Joins("JOIN kb_document ON kb_document.id = kb_chunk.document_id").
		Where("kb_document.status = ?", model.DocumentStatusPublished).
		Where("(kb_document.valid_from IS NULL OR kb_document.valid_from <= ?)", now).
		Where("(kb_document.valid_until IS NULL OR kb_document.valid_until > ?)", now)
	if filter.SystemName != "" {
		query = query.Where("lower(coalesce(kb_document.system_name, '')) = lower(?)", filter.SystemName)
	}
	if filter.ComponentName != "" {
		query = query.Where("lower(coalesce(kb_document.component_name, '')) = lower(?)", filter.ComponentName)
	}
	if filter.Environment != "" {
		query = query.Where("lower(coalesce(kb_document.environment, '')) = lower(?)", filter.Environment)
	}
	if len(filter.DocTypes) > 0 {
		query = query.Where("kb_document.doc_type IN ?", filter.DocTypes)
	}
	if filter.StrategyID != nil {
		query = query.Where("kb_chunk.strategy_id = ?", *filter.StrategyID)
	}
	for _, term := range filter.MustHaveTerms {
		if term = strings.TrimSpace(term); term != "" {
			query = query.Where("(kb_chunk.content ILIKE ? OR coalesce(kb_chunk.search_text, '') ILIKE ?)", "%"+term+"%", "%"+term+"%")
		}
	}
	for _, term := range filter.NegativeTerms {
		if term = strings.TrimSpace(term); term != "" {
			query = query.Where("kb_chunk.content NOT ILIKE ? AND coalesce(kb_chunk.search_text, '') NOT ILIKE ?", "%"+term+"%", "%"+term+"%")
		}
	}
	return query
}

func rowsToRankedKnowledgeChunks(rows []rankedKnowledgeChunkRow) []RankedKnowledgeChunk {
	result := make([]RankedKnowledgeChunk, 0, len(rows))
	for _, row := range rows {
		result = append(result, RankedKnowledgeChunk{Chunk: row.KBChunk, Score: row.RetrievalScore})
	}
	return result
}

func (r *GORMRAGRepository) SearchChunksTrigram(ctx context.Context, query string, filter KnowledgeRetrievalFilter, limit int) ([]RankedKnowledgeChunk, error) {
	var rows []rankedKnowledgeChunkRow
	if err := r.retrievalScope(ctx, filter).
		Select("kb_chunk.*, greatest(similarity(coalesce(kb_chunk.search_text, ''), ?), similarity(kb_chunk.content, ?)) AS retrieval_score", query, query).
		Where("coalesce(kb_chunk.search_text, '') % ? OR kb_chunk.content % ?", query, query).
		Order("retrieval_score DESC, kb_chunk.id ASC").Limit(limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("trigram search kb chunks: %w", err)
	}
	return rowsToRankedKnowledgeChunks(rows), nil
}

func (r *GORMRAGRepository) SearchChunksExact(ctx context.Context, terms []string, filter KnowledgeRetrievalFilter, limit int) ([]RankedKnowledgeChunk, error) {
	query := r.retrievalScope(ctx, filter)
	matched := 0
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		query = query.Where("(kb_chunk.content ILIKE ? OR coalesce(kb_chunk.search_text, '') ILIKE ?)", "%"+term+"%", "%"+term+"%")
		matched++
	}
	if matched == 0 {
		return nil, nil
	}
	var rows []rankedKnowledgeChunkRow
	if err := query.Select("kb_chunk.*, ?::float8 AS retrieval_score", float64(matched)).
		Order("kb_chunk.id ASC").Limit(limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("exact search kb chunks: %w", err)
	}
	return rowsToRankedKnowledgeChunks(rows), nil
}

func (r *GORMRAGRepository) SearchChunksTitleSection(ctx context.Context, query string, filter KnowledgeRetrievalFilter, limit int) ([]RankedKnowledgeChunk, error) {
	var rows []rankedKnowledgeChunkRow
	pattern := "%" + query + "%"
	if err := r.retrievalScope(ctx, filter).
		Select("kb_chunk.*, greatest(similarity(coalesce(kb_chunk.source_title, ''), ?), similarity(coalesce(kb_chunk.source_section, ''), ?)) AS retrieval_score", query, query).
		Where("coalesce(kb_chunk.source_title, '') ILIKE ? OR coalesce(kb_chunk.source_section, '') ILIKE ?", pattern, pattern).
		Order("retrieval_score DESC, kb_chunk.id ASC").Limit(limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("title section search kb chunks: %w", err)
	}
	return rowsToRankedKnowledgeChunks(rows), nil
}

func (r *GORMRAGRepository) SearchChunksPossibleQuestions(ctx context.Context, query string, filter KnowledgeRetrievalFilter, limit int) ([]RankedKnowledgeChunk, error) {
	var rows []rankedKnowledgeChunkRow
	pattern := "%" + query + "%"
	if err := r.retrievalScope(ctx, filter).
		Select("kb_chunk.*, similarity(coalesce(kb_chunk.possible_questions::text, ''), ?) AS retrieval_score", query).
		Where("coalesce(kb_chunk.possible_questions::text, '') ILIKE ?", pattern).
		Order("retrieval_score DESC, kb_chunk.id ASC").Limit(limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("possible question search kb chunks: %w", err)
	}
	return rowsToRankedKnowledgeChunks(rows), nil
}

func (r *GORMRAGRepository) SearchChunksDense(ctx context.Context, vector []float64, configID int64, modelName string, filter KnowledgeRetrievalFilter, limit int) ([]RankedKnowledgeChunk, error) {
	if len(vector) == 0 || len(vector) > 4096 {
		return nil, fmt.Errorf("invalid query embedding dimension")
	}
	encoded, err := json.Marshal(vector)
	if err != nil {
		return nil, fmt.Errorf("encode query embedding: %w", err)
	}
	cast := fmt.Sprintf("?::vector(%d)", len(vector))
	distance := "kb_chunk_embedding.vector_data <=> " + cast
	var rows []rankedKnowledgeChunkRow
	query := r.retrievalScope(ctx, filter).
		Joins("JOIN kb_chunk_embedding ON kb_chunk_embedding.chunk_id = kb_chunk.id").
		Joins("JOIN kb_embedding_index ON kb_embedding_index.id = kb_chunk_embedding.index_id").
		Where("kb_chunk_embedding.embedding_config_id = ?", configID).
		Where("kb_chunk_embedding.model = ?", modelName).
		Where("kb_chunk_embedding.dimension = ?", len(vector)).
		Where("kb_chunk_embedding.status = 'ready'").
		Where("kb_chunk_embedding.vector_data IS NOT NULL").
		Where("kb_chunk_embedding.content_hash = kb_chunk.content_hash").
		Where("kb_embedding_index.status = 'ready'")
	if filter.EmbeddingModelRevision != "" {
		query = query.Where("kb_chunk_embedding.model_revision = ?", filter.EmbeddingModelRevision)
	}
	if err := query.Select("kb_chunk.*, 1 - ("+distance+") AS retrieval_score", string(encoded)).
		Order(gorm.Expr(distance+" ASC, kb_chunk.id ASC", string(encoded))).
		Limit(limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("dense search kb chunks: %w", err)
	}
	return rowsToRankedKnowledgeChunks(rows), nil
}

func (r *GORMRAGRepository) FindKnowledgeDocumentsByIDs(ctx context.Context, ids []int64) ([]model.KBDocument, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var documents []model.KBDocument
	if err := r.db.WithContext(ctx).
		Where("id IN ? AND status = ?", ids, model.DocumentStatusPublished).
		Find(&documents).Error; err != nil {
		return nil, fmt.Errorf("find retrieval documents: %w", err)
	}
	return documents, nil
}

func (r *GORMRAGRepository) FindKnowledgeChunksByIDs(ctx context.Context, ids []int64) ([]model.KBChunk, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var chunks []model.KBChunk
	if err := r.db.WithContext(ctx).Table("kb_chunk").
		Joins("JOIN kb_document ON kb_document.id = kb_chunk.document_id").
		Where("kb_chunk.id IN ? AND kb_document.status = ?", ids, model.DocumentStatusPublished).
		Order("kb_chunk.id ASC").Find(&chunks).Error; err != nil {
		return nil, fmt.Errorf("find retrieval chunks: %w", err)
	}
	return chunks, nil
}

func (r *GORMRAGRepository) FindLLMConfigByID(ctx context.Context, id int64) (*model.LLMConfig, error) {
	return (&GORMLLMRepository{db: r.db}).FindLLMConfigByID(ctx, id)
}

func (r *GORMRAGRepository) FindReadyEmbeddingModelRevision(ctx context.Context, configID int64, strategyID *int64) (string, error) {
	var index model.KBEmbeddingIndex
	query := r.db.WithContext(ctx).Where("embedding_config_id = ? AND status = ?", configID, model.EmbeddingIndexReady)
	if strategyID != nil {
		query = query.Where("strategy_id = ?", *strategyID)
	}
	if err := query.Order("completed_at DESC NULLS LAST, id DESC").First(&index).Error; err != nil {
		return "", mapRepositoryError(err)
	}
	return index.ModelRevision, nil
}

type GORMRAGRepository struct {
	db *gorm.DB
}

func NewRAGRepository(db *gorm.DB) *GORMRAGRepository {
	return &GORMRAGRepository{db: db}
}

func (r *GORMRAGRepository) CreateConversation(ctx context.Context, conversation *model.Conversation) error {
	if err := r.db.WithContext(ctx).Create(conversation).Error; err != nil {
		return fmt.Errorf("create conversation: %w", err)
	}
	return nil
}

func (r *GORMRAGRepository) FindConversationByID(ctx context.Context, id int64) (*model.Conversation, error) {
	var conversation model.Conversation
	if err := r.db.WithContext(ctx).
		Where("status <> ?", model.ConversationStatusDeleted).
		First(&conversation, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &conversation, nil
}

func (r *GORMRAGRepository) CreateMessage(ctx context.Context, message *model.Message) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(message).Error; err != nil {
			return fmt.Errorf("create conversation message: %w", err)
		}
		result := tx.Model(&model.Conversation{}).
			Where("id = ? AND status <> ?", message.ConversationID, model.ConversationStatusDeleted).
			Update("updated_at", time.Now().UTC())
		if result.Error != nil {
			return fmt.Errorf("touch conversation: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrNotFound
		}
		return nil
	})
}

func (r *GORMRAGRepository) SearchChunks(ctx context.Context, query string, limit int) ([]model.KBChunk, error) {
	return (&GORMUserRepository{db: r.db}).SearchChunks(ctx, query, limit)
}

func (r *GORMRAGRepository) ListPublishedChunks(ctx context.Context, limit int) ([]model.KBChunk, error) {
	return (&GORMUserRepository{db: r.db}).ListPublishedChunks(ctx, limit)
}

func (r *GORMRAGRepository) ListPublishedChunkEmbeddings(ctx context.Context, modelName string, limit int) ([]model.KBChunkEmbedding, error) {
	return (&GORMUserRepository{db: r.db}).ListPublishedChunkEmbeddings(ctx, modelName, limit)
}

func (r *GORMRAGRepository) ListPublishedChunksMissingEmbedding(ctx context.Context, modelName string, limit int) ([]model.KBChunk, error) {
	return (&GORMUserRepository{db: r.db}).ListPublishedChunksMissingEmbedding(ctx, modelName, limit)
}

func (r *GORMRAGRepository) UpsertChunkEmbeddings(ctx context.Context, embeddings []model.KBChunkEmbedding) error {
	return (&GORMUserRepository{db: r.db}).UpsertChunkEmbeddings(ctx, embeddings)
}

func (r *GORMRAGRepository) FindDefaultEnabledLLMConfig(ctx context.Context) (*model.LLMConfig, error) {
	return r.FindDefaultEnabledLLMConfigByPurpose(ctx, model.LLMPurposeChat)
}

func (r *GORMRAGRepository) FindDefaultEnabledLLMConfigByPurpose(ctx context.Context, purpose string) (*model.LLMConfig, error) {
	return (&GORMLLMRepository{db: r.db}).FindDefaultEnabledLLMConfigByPurpose(ctx, purpose)
}

func (r *GORMRAGRepository) CreateQARecord(ctx context.Context, record *model.QARecord) error {
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return fmt.Errorf("create qa record: %w", err)
	}
	return nil
}
