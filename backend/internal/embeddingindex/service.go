package embeddingindex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	llmsvc "aiops-platform/backend/internal/llm"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

var (
	ErrInvalidInput      = errors.New("invalid embedding index input")
	ErrForbidden         = errors.New("embedding index access forbidden")
	ErrInvalidState      = errors.New("embedding index state does not allow this operation")
	ErrDimensionMismatch = errors.New("embedding vector dimension mismatch")
)

type Repository interface {
	FindDocumentVersionByID(context.Context, int64) (*model.KBDocumentVersion, error)
	FindDocumentByID(context.Context, int64) (*model.KBDocument, error)
	FindChunkStrategy(context.Context, int64) (*model.KBChunkStrategy, error)
	ListDocumentVersionChunks(context.Context, int64, *int64) ([]model.KBChunk, error)
	FindLLMConfigByID(context.Context, int64) (*model.LLMConfig, error)
	CreateEmbeddingIndex(context.Context, *model.KBEmbeddingIndex) error
	FindEmbeddingIndex(context.Context, int64) (*model.KBEmbeddingIndex, error)
	ListEmbeddingIndexes(context.Context, int64, int64) ([]model.KBEmbeddingIndex, error)
	RefreshEmbeddingIndexFingerprint(context.Context, int64, string, int) (*model.KBEmbeddingIndex, error)
	ClaimEmbeddingIndexBuild(context.Context, int64, []string) (*model.KBEmbeddingIndex, error)
	UpsertEmbeddingIndexBatch(context.Context, []model.KBChunkEmbedding) error
	FinishEmbeddingIndexBuild(context.Context, int64, int, time.Time) (*model.KBEmbeddingIndex, error)
	FailEmbeddingIndexBuild(context.Context, int64, string) error
	MarkEmbeddingIndexStale(context.Context, int64) error
	EnsureEmbeddingHNSWIndex(context.Context, *model.KBEmbeddingIndex) error
}

type SecretManager interface{ Decrypt(string) (string, error) }

type Service struct {
	repository Repository
	secrets    SecretManager
	client     llmsvc.EmbeddingClient
	now        func() time.Time
}

func NewService(repository Repository, secrets SecretManager, client llmsvc.EmbeddingClient) *Service {
	return &Service{repository: repository, secrets: secrets, client: client, now: time.Now}
}

type CreateInput struct {
	DocumentVersionID  int64  `json:"documentVersionId"`
	StrategyID         int64  `json:"strategyId"`
	EmbeddingConfigID  int64  `json:"embeddingConfigId"`
	ModelRevision      string `json:"modelRevision"`
	Dimension          int    `json:"dimension"`
	Normalized         *bool  `json:"normalized"`
	HNSWEnabled        bool   `json:"hnswEnabled"`
	HNSWM              int    `json:"hnswM"`
	HNSWEFConstruction int    `json:"hnswEfConstruction"`
}

type BuildInput struct {
	BatchSize int `json:"batchSize"`
}

type StatusResult struct {
	Ready   bool                     `json:"ready"`
	Indexes []model.KBEmbeddingIndex `json:"indexes"`
}

func (s *Service) Create(ctx context.Context, actor *model.AppUser, input CreateInput) (*model.KBEmbeddingIndex, error) {
	if actor == nil || actor.Role != model.RoleAdmin {
		return nil, ErrForbidden
	}
	if input.DocumentVersionID <= 0 || input.StrategyID <= 0 || input.EmbeddingConfigID <= 0 || input.Dimension <= 0 || input.Dimension > 65535 {
		return nil, ErrInvalidInput
	}
	version, _, chunks, config, err := s.loadScope(ctx, actor, input.DocumentVersionID, input.StrategyID, input.EmbeddingConfigID)
	if err != nil {
		return nil, err
	}
	revision := strings.TrimSpace(input.ModelRevision)
	if revision == "" {
		revision = fmt.Sprintf("config-%d-%d", config.ID, config.UpdatedAt.Unix())
	}
	if len(revision) > 120 {
		return nil, ErrInvalidInput
	}
	normalized := true
	if input.Normalized != nil {
		normalized = *input.Normalized
	}
	m, ef := input.HNSWM, input.HNSWEFConstruction
	if m == 0 {
		m = 16
	}
	if ef == 0 {
		ef = 64
	}
	if m < 2 || m > 100 || ef < 4 || ef > 1000 {
		return nil, ErrInvalidInput
	}
	actorID := actor.ID
	index := &model.KBEmbeddingIndex{DocumentVersionID: version.ID, StrategyID: input.StrategyID, EmbeddingConfigID: config.ID, ModelName: config.Model, ModelRevision: revision, Dimension: input.Dimension, Normalized: normalized, DistanceMetric: "cosine", Status: model.EmbeddingIndexPending, ChunkCount: len(chunks), ContentFingerprint: chunkFingerprint(chunks), HNSWEnabled: input.HNSWEnabled, HNSWM: m, HNSWEFConstruction: ef, CreatedBy: &actorID}
	if err := s.repository.CreateEmbeddingIndex(ctx, index); err != nil {
		return nil, err
	}
	return index, nil
}

func (s *Service) Get(ctx context.Context, actor *model.AppUser, id int64) (*model.KBEmbeddingIndex, error) {
	if actor == nil || id <= 0 {
		return nil, ErrInvalidInput
	}
	index, err := s.repository.FindEmbeddingIndex(ctx, id)
	if err != nil {
		return nil, err
	}
	if _, _, err := s.accessScope(ctx, actor, index.DocumentVersionID); err != nil {
		return nil, err
	}
	return s.refreshStale(ctx, index)
}

func (s *Service) Build(ctx context.Context, actor *model.AppUser, id int64, input BuildInput) (*model.KBEmbeddingIndex, error) {
	return s.build(ctx, actor, id, input.BatchSize, []string{model.EmbeddingIndexPending, model.EmbeddingIndexStale})
}

func (s *Service) Retry(ctx context.Context, actor *model.AppUser, id int64, input BuildInput) (*model.KBEmbeddingIndex, error) {
	return s.build(ctx, actor, id, input.BatchSize, []string{model.EmbeddingIndexFailed})
}

func (s *Service) Rebuild(ctx context.Context, actor *model.AppUser, id int64, input BuildInput) (*model.KBEmbeddingIndex, error) {
	if actor == nil || actor.Role != model.RoleAdmin {
		return nil, ErrForbidden
	}
	if err := s.repository.MarkEmbeddingIndexStale(ctx, id); err != nil {
		if errors.Is(err, repository.ErrImmutable) {
			return nil, ErrInvalidState
		}
		return nil, err
	}
	return s.build(ctx, actor, id, input.BatchSize, []string{model.EmbeddingIndexStale})
}

func (s *Service) Status(ctx context.Context, actor *model.AppUser, versionID, strategyID int64) (*StatusResult, error) {
	if actor == nil || versionID <= 0 || strategyID <= 0 {
		return nil, ErrInvalidInput
	}
	if _, _, err := s.accessScope(ctx, actor, versionID); err != nil {
		return nil, err
	}
	indexes, err := s.repository.ListEmbeddingIndexes(ctx, versionID, strategyID)
	if err != nil {
		return nil, err
	}
	ready := false
	for index := range indexes {
		refreshed, refreshErr := s.refreshStale(ctx, &indexes[index])
		if refreshErr != nil {
			return nil, refreshErr
		}
		indexes[index] = *refreshed
		ready = ready || refreshed.Status == model.EmbeddingIndexReady && refreshed.EmbeddedCount == refreshed.ChunkCount && refreshed.ChunkCount > 0
	}
	return &StatusResult{Ready: ready, Indexes: indexes}, nil
}

func (s *Service) build(ctx context.Context, actor *model.AppUser, id int64, batchSize int, allowed []string) (*model.KBEmbeddingIndex, error) {
	if actor == nil || actor.Role != model.RoleAdmin || s.client == nil {
		return nil, ErrForbidden
	}
	index, err := s.Get(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	if !containsStatus(allowed, index.Status) {
		return nil, ErrInvalidState
	}
	config, err := s.repository.FindLLMConfigByID(ctx, index.EmbeddingConfigID)
	if err != nil {
		return nil, err
	}
	if !config.Enabled || config.Purpose != model.LLMPurposeEmbedding || config.Model != index.ModelName {
		return nil, ErrInvalidInput
	}
	strategyID := index.StrategyID
	chunks, err := s.repository.ListDocumentVersionChunks(ctx, index.DocumentVersionID, &strategyID)
	if err != nil {
		return nil, err
	}
	if len(chunks) == 0 || chunkFingerprint(chunks) != index.ContentFingerprint {
		return nil, ErrInvalidState
	}
	claimed, err := s.repository.ClaimEmbeddingIndexBuild(ctx, id, allowed)
	if err != nil {
		if errors.Is(err, repository.ErrImmutable) {
			return nil, ErrInvalidState
		}
		return nil, err
	}
	credential, err := s.embeddingCredential(config)
	if err != nil {
		s.fail(ctx, id, err)
		return nil, err
	}
	batchSize = normalizeBatchSize(batchSize)
	embedded := 0
	for start := 0; start < len(chunks); start += batchSize {
		end := start + batchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		inputs := make([]string, 0, end-start)
		for _, chunk := range chunks[start:end] {
			inputs = append(inputs, chunk.Content)
		}
		result, callErr := s.client.Embed(ctx, llmsvc.EmbeddingRequest{BaseURL: config.BaseURL, APIKey: credential.apiKey, APISecret: credential.apiSecret, Model: config.Model, Input: inputs})
		if callErr != nil {
			s.fail(ctx, id, callErr)
			return nil, callErr
		}
		if result == nil || len(result.Embeddings) != len(inputs) || result.Model != "" && result.Model != index.ModelName {
			err = ErrDimensionMismatch
			s.fail(ctx, id, err)
			return nil, err
		}
		rows := make([]model.KBChunkEmbedding, 0, len(inputs))
		for offset, vector := range result.Embeddings {
			if len(vector) != index.Dimension {
				err = fmt.Errorf("%w: expected %d, got %d", ErrDimensionMismatch, index.Dimension, len(vector))
				s.fail(ctx, id, err)
				return nil, err
			}
			if index.Normalized {
				vector = normalizeVector(vector)
			}
			encoded, _ := json.Marshal(vector)
			indexID, configID := index.ID, config.ID
			chunk := chunks[start+offset]
			rows = append(rows, model.KBChunkEmbedding{IndexID: &indexID, ChunkID: chunk.ID, LLMConfigID: &configID, Model: index.ModelName, ModelRevision: index.ModelRevision, Dimension: index.Dimension, Embedding: encoded, VectorData: vectorLiteral(vector), ContentHash: chunk.ContentHash, Normalized: index.Normalized, DistanceMetric: "cosine", Status: model.EmbeddingIndexReady})
		}
		if err := s.repository.UpsertEmbeddingIndexBatch(ctx, rows); err != nil {
			s.fail(ctx, id, err)
			return nil, err
		}
		embedded += len(rows)
	}
	completed, err := s.repository.FinishEmbeddingIndexBuild(ctx, claimed.ID, embedded, s.now())
	if err != nil {
		return nil, err
	}
	if err := s.repository.EnsureEmbeddingHNSWIndex(ctx, completed); err != nil {
		s.fail(ctx, id, err)
		return nil, err
	}
	return completed, nil
}

func (s *Service) refreshStale(ctx context.Context, index *model.KBEmbeddingIndex) (*model.KBEmbeddingIndex, error) {
	strategyID := index.StrategyID
	chunks, err := s.repository.ListDocumentVersionChunks(ctx, index.DocumentVersionID, &strategyID)
	if err != nil {
		return nil, err
	}
	return s.repository.RefreshEmbeddingIndexFingerprint(ctx, index.ID, chunkFingerprint(chunks), len(chunks))
}

func (s *Service) loadScope(ctx context.Context, actor *model.AppUser, versionID, strategyID, configID int64) (*model.KBDocumentVersion, *model.KBDocument, []model.KBChunk, *model.LLMConfig, error) {
	version, document, err := s.accessScope(ctx, actor, versionID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if _, err := s.repository.FindChunkStrategy(ctx, strategyID); err != nil {
		return nil, nil, nil, nil, err
	}
	chunks, err := s.repository.ListDocumentVersionChunks(ctx, versionID, &strategyID)
	if err != nil || len(chunks) == 0 {
		if err == nil {
			err = ErrInvalidInput
		}
		return nil, nil, nil, nil, err
	}
	config, err := s.repository.FindLLMConfigByID(ctx, configID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if !config.Enabled || config.Purpose != model.LLMPurposeEmbedding {
		return nil, nil, nil, nil, ErrInvalidInput
	}
	return version, document, chunks, config, nil
}

func (s *Service) accessScope(ctx context.Context, actor *model.AppUser, versionID int64) (*model.KBDocumentVersion, *model.KBDocument, error) {
	version, err := s.repository.FindDocumentVersionByID(ctx, versionID)
	if err != nil {
		return nil, nil, err
	}
	document, err := s.repository.FindDocumentByID(ctx, version.DocumentID)
	if err != nil {
		return nil, nil, err
	}
	if actor.Role != model.RoleAdmin && (document.CreatedBy == nil || *document.CreatedBy != actor.ID) {
		return nil, nil, ErrForbidden
	}
	return version, document, nil
}

type embeddingCredential struct{ apiKey, apiSecret string }

func (s *Service) embeddingCredential(config *model.LLMConfig) (embeddingCredential, error) {
	credential := embeddingCredential{}
	if s.secrets == nil {
		if config.APIKeyRef != nil || config.APISecretRef != nil {
			return credential, ErrInvalidInput
		}
		return credential, nil
	}
	var err error
	if config.APIKeyRef != nil && *config.APIKeyRef != "" {
		credential.apiKey, err = s.secrets.Decrypt(*config.APIKeyRef)
		if err != nil {
			return credential, err
		}
	}
	if config.APISecretRef != nil && *config.APISecretRef != "" {
		credential.apiSecret, err = s.secrets.Decrypt(*config.APISecretRef)
	}
	return credential, err
}

func (s *Service) fail(ctx context.Context, id int64, err error) {
	message := err.Error()
	if len(message) > 2000 {
		message = message[:2000]
	}
	_ = s.repository.FailEmbeddingIndexBuild(ctx, id, message)
}

func chunkFingerprint(chunks []model.KBChunk) string {
	values := append([]model.KBChunk(nil), chunks...)
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	hash := sha256.New()
	for _, chunk := range values {
		_, _ = fmt.Fprintf(hash, "%d:%s\n", chunk.ID, chunk.ContentHash)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func normalizeBatchSize(value int) int {
	if value <= 0 {
		return 64
	}
	if value > 100 {
		return 100
	}
	return value
}

func normalizeVector(vector []float64) []float64 {
	norm := 0.0
	for _, value := range vector {
		norm += value * value
	}
	if norm == 0 {
		return append([]float64(nil), vector...)
	}
	norm = math.Sqrt(norm)
	result := make([]float64, len(vector))
	for index, value := range vector {
		result[index] = value / norm
	}
	return result
}

func vectorLiteral(vector []float64) string {
	values := make([]string, len(vector))
	for index, value := range vector {
		values[index] = strconv.FormatFloat(value, 'g', -1, 64)
	}
	return "[" + strings.Join(values, ",") + "]"
}

func containsStatus(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
