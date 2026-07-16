package embeddingindex

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	llmsvc "aiops-platform/backend/internal/llm"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

func TestBuildBatchesVectorsAndMakesPublicationGateReady(t *testing.T) {
	store, client, service, admin := newEmbeddingTestService(5)
	index, err := service.Create(context.Background(), admin, embeddingTestCreateInput())
	if err != nil {
		t.Fatal(err)
	}
	ready, err := service.Build(context.Background(), admin, index.ID, BuildInput{BatchSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if client.calls != 3 {
		t.Fatalf("five chunks with batch size two must use three embedding calls, got %d", client.calls)
	}
	if ready.Status != model.EmbeddingIndexReady || ready.EmbeddedCount != 5 {
		t.Fatalf("unexpected ready index: %#v", ready)
	}
	status, err := service.Status(context.Background(), admin, 10, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Ready {
		t.Fatal("publication gate must see a complete ready index")
	}
	if len(store.embeddings[index.ID]) != 5 || store.hnswCalls != 1 {
		t.Fatalf("expected five vectors and one HNSW build, got vectors=%d hnsw=%d", len(store.embeddings[index.ID]), store.hnswCalls)
	}
}

func TestContentHashChangeMarksReadyIndexStale(t *testing.T) {
	store, _, service, admin := newEmbeddingTestService(2)
	index, err := service.Create(context.Background(), admin, embeddingTestCreateInput())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Build(context.Background(), admin, index.ID, BuildInput{}); err != nil {
		t.Fatal(err)
	}
	store.chunks[0].ContentHash = "changed-content-hash"
	refreshed, err := service.Get(context.Background(), admin, index.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Status != model.EmbeddingIndexStale {
		t.Fatalf("content hash change must mark index stale, got %q", refreshed.Status)
	}
}

func TestFailedDimensionBuildCanRetry(t *testing.T) {
	store, client, service, admin := newEmbeddingTestService(2)
	index, err := service.Create(context.Background(), admin, embeddingTestCreateInput())
	if err != nil {
		t.Fatal(err)
	}
	client.dimension = 2
	if _, err := service.Build(context.Background(), admin, index.ID, BuildInput{}); !errors.Is(err, ErrDimensionMismatch) {
		t.Fatalf("expected dimension mismatch, got %v", err)
	}
	if store.indexes[index.ID].Status != model.EmbeddingIndexFailed {
		t.Fatalf("failed call must persist failed status, got %q", store.indexes[index.ID].Status)
	}
	client.dimension = 3
	retried, err := service.Retry(context.Background(), admin, index.ID, BuildInput{BatchSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if retried.Status != model.EmbeddingIndexReady {
		t.Fatalf("retry must reach ready, got %q", retried.Status)
	}
}

func TestModelRevisionIsBoundToEveryVector(t *testing.T) {
	store, _, service, admin := newEmbeddingTestService(1)
	input := embeddingTestCreateInput()
	input.ModelRevision = "embed-v2026-07"
	index, err := service.Create(context.Background(), admin, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Build(context.Background(), admin, index.ID, BuildInput{}); err != nil {
		t.Fatal(err)
	}
	row := store.embeddings[index.ID][0]
	if row.Model != index.ModelName || row.ModelRevision != index.ModelRevision || row.Dimension != index.Dimension || row.LLMConfigID == nil || *row.LLMConfigID != index.EmbeddingConfigID {
		t.Fatalf("vector identity is not isolated: %#v", row)
	}
}

type embeddingFakeRepository struct {
	document   model.KBDocument
	version    model.KBDocumentVersion
	strategy   model.KBChunkStrategy
	config     model.LLMConfig
	chunks     []model.KBChunk
	indexes    map[int64]*model.KBEmbeddingIndex
	embeddings map[int64][]model.KBChunkEmbedding
	nextID     int64
	hnswCalls  int
}

func newEmbeddingTestService(chunkCount int) (*embeddingFakeRepository, *embeddingFakeClient, *Service, *model.AppUser) {
	adminID := int64(1)
	store := &embeddingFakeRepository{
		document:   model.KBDocument{ID: 1, CreatedBy: &adminID},
		version:    model.KBDocumentVersion{ID: 10, DocumentID: 1},
		strategy:   model.KBChunkStrategy{ID: 20, Enabled: true},
		config:     model.LLMConfig{ID: 30, Model: "embed-model", Purpose: model.LLMPurposeEmbedding, Enabled: true, UpdatedAt: time.Unix(100, 0)},
		indexes:    map[int64]*model.KBEmbeddingIndex{},
		embeddings: map[int64][]model.KBChunkEmbedding{},
		nextID:     1,
	}
	for index := 0; index < chunkCount; index++ {
		store.chunks = append(store.chunks, model.KBChunk{ID: int64(index + 1), DocumentVersionID: 10, StrategyID: 20, Content: fmt.Sprintf("chunk-%d", index), ContentHash: fmt.Sprintf("hash-%d", index)})
	}
	client := &embeddingFakeClient{dimension: 3}
	return store, client, NewService(store, nil, client), &model.AppUser{ID: adminID, Role: model.RoleAdmin}
}

func embeddingTestCreateInput() CreateInput {
	normalized := true
	return CreateInput{DocumentVersionID: 10, StrategyID: 20, EmbeddingConfigID: 30, ModelRevision: "rev-1", Dimension: 3, Normalized: &normalized, HNSWEnabled: true}
}

func (f *embeddingFakeRepository) FindDocumentVersionByID(context.Context, int64) (*model.KBDocumentVersion, error) {
	value := f.version
	return &value, nil
}
func (f *embeddingFakeRepository) FindDocumentByID(context.Context, int64) (*model.KBDocument, error) {
	value := f.document
	return &value, nil
}
func (f *embeddingFakeRepository) FindChunkStrategy(context.Context, int64) (*model.KBChunkStrategy, error) {
	value := f.strategy
	return &value, nil
}
func (f *embeddingFakeRepository) ListDocumentVersionChunks(context.Context, int64, *int64) ([]model.KBChunk, error) {
	return append([]model.KBChunk(nil), f.chunks...), nil
}
func (f *embeddingFakeRepository) FindLLMConfigByID(context.Context, int64) (*model.LLMConfig, error) {
	value := f.config
	return &value, nil
}
func (f *embeddingFakeRepository) CreateEmbeddingIndex(_ context.Context, index *model.KBEmbeddingIndex) error {
	index.ID = f.nextID
	f.nextID++
	value := *index
	f.indexes[index.ID] = &value
	return nil
}
func (f *embeddingFakeRepository) FindEmbeddingIndex(_ context.Context, id int64) (*model.KBEmbeddingIndex, error) {
	index, ok := f.indexes[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	value := *index
	return &value, nil
}
func (f *embeddingFakeRepository) ListEmbeddingIndexes(context.Context, int64, int64) ([]model.KBEmbeddingIndex, error) {
	result := []model.KBEmbeddingIndex{}
	for _, index := range f.indexes {
		result = append(result, *index)
	}
	return result, nil
}
func (f *embeddingFakeRepository) RefreshEmbeddingIndexFingerprint(_ context.Context, id int64, fingerprint string, count int) (*model.KBEmbeddingIndex, error) {
	index := f.indexes[id]
	if index.ContentFingerprint != fingerprint || index.ChunkCount != count {
		index.ContentFingerprint, index.ChunkCount = fingerprint, count
		if index.Status == model.EmbeddingIndexReady || index.Status == model.EmbeddingIndexFailed {
			index.Status = model.EmbeddingIndexStale
		}
	}
	value := *index
	return &value, nil
}
func (f *embeddingFakeRepository) ClaimEmbeddingIndexBuild(_ context.Context, id int64, allowed []string) (*model.KBEmbeddingIndex, error) {
	index := f.indexes[id]
	if !containsStatus(allowed, index.Status) {
		return nil, repository.ErrImmutable
	}
	index.Status, index.EmbeddedCount = model.EmbeddingIndexBuilding, 0
	f.embeddings[id] = nil
	value := *index
	return &value, nil
}
func (f *embeddingFakeRepository) UpsertEmbeddingIndexBatch(_ context.Context, rows []model.KBChunkEmbedding) error {
	if len(rows) > 0 && rows[0].IndexID != nil {
		id := *rows[0].IndexID
		f.embeddings[id] = append(f.embeddings[id], rows...)
	}
	return nil
}
func (f *embeddingFakeRepository) FinishEmbeddingIndexBuild(_ context.Context, id int64, count int, at time.Time) (*model.KBEmbeddingIndex, error) {
	index := f.indexes[id]
	index.Status, index.EmbeddedCount, index.CompletedAt = model.EmbeddingIndexReady, count, &at
	value := *index
	return &value, nil
}
func (f *embeddingFakeRepository) FailEmbeddingIndexBuild(_ context.Context, id int64, message string) error {
	index := f.indexes[id]
	index.Status, index.ErrorMessage = model.EmbeddingIndexFailed, &message
	return nil
}
func (f *embeddingFakeRepository) MarkEmbeddingIndexStale(_ context.Context, id int64) error {
	f.indexes[id].Status = model.EmbeddingIndexStale
	return nil
}
func (f *embeddingFakeRepository) EnsureEmbeddingHNSWIndex(context.Context, *model.KBEmbeddingIndex) error {
	f.hnswCalls++
	return nil
}

type embeddingFakeClient struct {
	calls     int
	dimension int
}

func (f *embeddingFakeClient) Embed(_ context.Context, request llmsvc.EmbeddingRequest) (*llmsvc.EmbeddingResult, error) {
	f.calls++
	vectors := make([][]float64, len(request.Input))
	for index := range vectors {
		vectors[index] = make([]float64, f.dimension)
		for dimension := range vectors[index] {
			vectors[index][dimension] = float64(dimension + 1)
		}
	}
	return &llmsvc.EmbeddingResult{Model: request.Model, Embeddings: vectors}, nil
}
