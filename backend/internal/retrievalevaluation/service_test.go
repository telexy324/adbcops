package retrievalevaluation

import (
	"context"
	"encoding/json"
	"testing"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/rag"
	"aiops-platform/backend/internal/repository"
)

func TestSmokeTestRunsBeforePublicationAndTracesConfiguration(t *testing.T) {
	store := newFakeRepository()
	versionID := int64(7)
	documentID := int64(3)
	store.version = &model.KBDocumentVersion{ID: versionID, DocumentID: documentID, Status: model.DocumentVersionStatusReviewing}
	for index, category := range smokeCategories {
		expectNoAnswer := category == "irrelevant"
		expected := []int64{int64(index + 10)}
		if expectNoAnswer {
			expected = nil
		}
		store.cases = append(store.cases, model.KBRetrievalTestCase{
			ID: int64(index + 1), DocumentID: &documentID, DocumentVersionID: &versionID,
			Question: category, Category: category, ExpectedChunkIDs: mustJSON(expected),
			ExpectedDocumentIDs: json.RawMessage(`[]`), ExpectedSections: json.RawMessage(`[]`),
			MustIncludeFacts: json.RawMessage(`[]`), MustNotInclude: json.RawMessage(`[]`),
			ExpectNoAnswer: expectNoAnswer, Enabled: true,
		})
	}
	retriever := &fakeRetriever{byQuestion: map[string]int64{"title": 10, "core_step": 11, "error_code": 12, "scenario": 13}}
	service := NewService(store, retriever)
	actor := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	embeddingID, rerankID, strategyID := int64(20), int64(21), int64(22)
	run, err := service.RunSmoke(context.Background(), actor, versionID, RunConfig{
		EmbeddingConfigID: &embeddingID, EmbeddingModelRevision: "rev-3", RerankConfigID: &rerankID, ChunkStrategyID: &strategyID,
	})
	if err != nil {
		t.Fatalf("RunSmoke() error = %v", err)
	}
	if !run.Passed || run.Status != model.RetrievalEvaluationCompleted || run.CaseCount != 5 {
		t.Fatalf("run = %+v", run)
	}
	var metrics AggregateMetrics
	if json.Unmarshal(run.Metrics, &metrics) != nil || metrics.SmokeCoverage != 1 || metrics.CitationAccuracy != 1 || metrics.RecallAtK != .8 {
		t.Fatalf("metrics = %+v raw=%s", metrics, run.Metrics)
	}
	if run.EmbeddingModelRevision == nil || *run.EmbeddingModelRevision != "rev-3" || run.EmbeddingModel == nil || *run.EmbeddingModel != "embed-model" {
		t.Fatalf("configuration trace = %+v", run)
	}
}

func TestRetrievalLabComparesVariants(t *testing.T) {
	store := newFakeRepository()
	store.cases = []model.KBRetrievalTestCase{{ID: 1, Question: "title", Category: "title", ExpectedChunkIDs: mustJSON([]int64{10}), ExpectedDocumentIDs: json.RawMessage(`[]`), ExpectedSections: json.RawMessage(`[]`), MustIncludeFacts: json.RawMessage(`[]`), MustNotInclude: json.RawMessage(`[]`), Enabled: true}}
	retriever := &fakeRetriever{byQuestion: map[string]int64{"title": 10}}
	service := NewService(store, retriever)
	firstEmbedding, secondEmbedding, strategy := int64(2), int64(3), int64(4)
	runs, err := service.RunLab(context.Background(), &model.AppUser{ID: 1, Role: model.RoleAdmin}, LabInput{Variants: []RunConfig{
		{Name: "lexical", DisableEmbedding: true, DisableRerank: true},
		{Name: "semantic", EmbeddingConfigID: &firstEmbedding, ChunkStrategyID: &strategy},
		{Name: "semantic-v2", EmbeddingConfigID: &secondEmbedding, EmbeddingModelRevision: "v2"},
	}})
	if err != nil || len(runs) != 3 || len(retriever.inputs) != 3 {
		t.Fatalf("runs=%d inputs=%+v err=%v", len(runs), retriever.inputs, err)
	}
	if !retriever.inputs[0].DisableEmbedding || retriever.inputs[1].EmbeddingConfigID == nil || retriever.inputs[1].ChunkStrategyID == nil || retriever.inputs[2].EmbeddingModelRevision != "v2" {
		t.Fatalf("inputs = %+v", retriever.inputs)
	}
}

type fakeRepository struct {
	version *model.KBDocumentVersion
	cases   []model.KBRetrievalTestCase
	runs    []*model.KBRetrievalEvaluationRun
}

func newFakeRepository() *fakeRepository { return &fakeRepository{} }
func (f *fakeRepository) CreateRetrievalTestCase(_ context.Context, item *model.KBRetrievalTestCase) error {
	item.ID = int64(len(f.cases) + 1)
	f.cases = append(f.cases, *item)
	return nil
}
func (f *fakeRepository) ListRetrievalTestCases(_ context.Context, versionID *int64, ids []int64, enabledOnly bool) ([]model.KBRetrievalTestCase, error) {
	wanted := map[int64]struct{}{}
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	result := []model.KBRetrievalTestCase{}
	for _, item := range f.cases {
		if versionID != nil && (item.DocumentVersionID == nil || *item.DocumentVersionID != *versionID) {
			continue
		}
		if len(wanted) > 0 {
			if _, ok := wanted[item.ID]; !ok {
				continue
			}
		}
		if enabledOnly && !item.Enabled {
			continue
		}
		result = append(result, item)
	}
	return result, nil
}
func (f *fakeRepository) FindDocumentVersionByID(_ context.Context, id int64) (*model.KBDocumentVersion, error) {
	if f.version == nil || f.version.ID != id {
		return nil, repository.ErrNotFound
	}
	return f.version, nil
}
func (f *fakeRepository) CreateRetrievalEvaluationRun(_ context.Context, run *model.KBRetrievalEvaluationRun) error {
	run.ID = int64(len(f.runs) + 1)
	f.runs = append(f.runs, run)
	return nil
}
func (f *fakeRepository) CompleteRetrievalEvaluationRun(_ context.Context, _ *model.KBRetrievalEvaluationRun, _ []model.KBRetrievalEvaluationResult) error {
	return nil
}
func (f *fakeRepository) FindRetrievalEvaluationRun(_ context.Context, id int64) (*model.KBRetrievalEvaluationRun, error) {
	for _, run := range f.runs {
		if run.ID == id {
			return run, nil
		}
	}
	return nil, repository.ErrNotFound
}
func (f *fakeRepository) ListRetrievalEvaluationRuns(_ context.Context, _ int) ([]model.KBRetrievalEvaluationRun, error) {
	result := []model.KBRetrievalEvaluationRun{}
	for _, run := range f.runs {
		result = append(result, *run)
	}
	return result, nil
}

type fakeRetriever struct {
	byQuestion map[string]int64
	inputs     []rag.EvaluationSearchInput
}

func (f *fakeRetriever) EvaluateRetrieval(_ context.Context, _ *model.AppUser, input rag.EvaluationSearchInput) (*rag.EvaluationSearchResult, error) {
	f.inputs = append(f.inputs, input)
	chunkID, ok := f.byQuestion[input.Question]
	trace := rag.RetrievalTrace{Configuration: rag.RetrievalConfigurationTrace{EmbeddingModel: "embed-model", EmbeddingModelRevision: input.EmbeddingModelRevision, RerankModel: "rerank-model", ChunkStrategyID: input.ChunkStrategyID}}
	if !ok {
		return &rag.EvaluationSearchResult{RetrievalTrace: trace}, nil
	}
	return &rag.EvaluationSearchResult{Citations: []rag.Citation{{CitationID: "KC-7-1", DocumentID: 3, DocumentVersionID: 7, ChunkID: chunkID, ChunkIDs: []int64{chunkID}}}, ContextText: "expected fact", RetrievalTrace: trace}, nil
}
