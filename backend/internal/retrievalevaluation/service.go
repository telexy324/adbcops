package retrievalevaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/rag"
)

var ErrInvalidInput = errors.New("invalid retrieval evaluation input")

var smokeCategories = []string{"title", "core_step", "error_code", "scenario", "irrelevant"}

type Repository interface {
	CreateRetrievalTestCase(context.Context, *model.KBRetrievalTestCase) error
	ListRetrievalTestCases(context.Context, *int64, []int64, bool) ([]model.KBRetrievalTestCase, error)
	FindDocumentVersionByID(context.Context, int64) (*model.KBDocumentVersion, error)
	CreateRetrievalEvaluationRun(context.Context, *model.KBRetrievalEvaluationRun) error
	CompleteRetrievalEvaluationRun(context.Context, *model.KBRetrievalEvaluationRun, []model.KBRetrievalEvaluationResult) error
	FindRetrievalEvaluationRun(context.Context, int64) (*model.KBRetrievalEvaluationRun, error)
	ListRetrievalEvaluationRuns(context.Context, int) ([]model.KBRetrievalEvaluationRun, error)
}

type Retriever interface {
	EvaluateRetrieval(context.Context, *model.AppUser, rag.EvaluationSearchInput) (*rag.EvaluationSearchResult, error)
}

type Service struct {
	repository Repository
	retriever  Retriever
	now        func() time.Time
}

type CreateTestCaseInput struct {
	DocumentID          *int64   `json:"documentId"`
	DocumentVersionID   *int64   `json:"documentVersionId"`
	Question            string   `json:"question"`
	Category            string   `json:"category"`
	ExpectedDocumentIDs []int64  `json:"expectedDocumentIds"`
	ExpectedChunkIDs    []int64  `json:"expectedChunkIds"`
	ExpectedSections    []string `json:"expectedSections"`
	MustIncludeFacts    []string `json:"mustIncludeFacts"`
	MustNotInclude      []string `json:"mustNotInclude"`
	ExpectNoAnswer      bool     `json:"expectNoAnswer"`
	Source              string   `json:"source"`
	Enabled             *bool    `json:"enabled"`
}

type Thresholds struct {
	MinimumRecallAtK        float64 `json:"minimumRecallAtK"`
	MinimumCitationAccuracy float64 `json:"minimumCitationAccuracy"`
}

type RunConfig struct {
	Name                   string     `json:"name"`
	CaseIDs                []int64    `json:"caseIds"`
	DocumentVersionID      *int64     `json:"documentVersionId"`
	EmbeddingConfigID      *int64     `json:"embeddingConfigId"`
	EmbeddingModelRevision string     `json:"embeddingModelRevision"`
	RerankConfigID         *int64     `json:"rerankConfigId"`
	ChunkStrategyID        *int64     `json:"chunkStrategyId"`
	DisableEmbedding       bool       `json:"disableEmbedding"`
	DisableRerank          bool       `json:"disableRerank"`
	Limit                  int        `json:"limit"`
	Thresholds             Thresholds `json:"thresholds"`
}

type LabInput struct {
	CaseIDs           []int64     `json:"caseIds"`
	DocumentVersionID *int64      `json:"documentVersionId"`
	Variants          []RunConfig `json:"variants"`
}

type CaseMetrics struct {
	RankingMetrics
	CitationAccuracy   float64 `json:"citationAccuracy"`
	AnswerGroundedness float64 `json:"answerGroundedness"`
	NoAnswerCorrect    float64 `json:"noAnswerCorrect"`
	ProhibitedHitRate  float64 `json:"prohibitedHitRate"`
}

type AggregateMetrics struct {
	RankingMetrics
	CitationAccuracy   float64 `json:"citationAccuracy"`
	AnswerGroundedness float64 `json:"answerGroundedness"`
	NoAnswerPrecision  float64 `json:"noAnswerPrecision"`
	SmokeCoverage      float64 `json:"smokeCoverage"`
}

func NewService(repository Repository, retriever Retriever) *Service {
	return &Service{repository: repository, retriever: retriever, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) CreateTestCase(ctx context.Context, actor *model.AppUser, input CreateTestCaseInput) (*model.KBRetrievalTestCase, error) {
	if actor == nil || actor.Role != model.RoleAdmin || strings.TrimSpace(input.Question) == "" || utf8.RuneCountInString(input.Question) > 2000 || !validCategory(input.Category) {
		return nil, ErrInvalidInput
	}
	if input.DocumentVersionID != nil {
		if *input.DocumentVersionID <= 0 || input.DocumentID == nil || *input.DocumentID <= 0 {
			return nil, ErrInvalidInput
		}
		version, err := s.repository.FindDocumentVersionByID(ctx, *input.DocumentVersionID)
		if err != nil {
			return nil, err
		}
		if version.DocumentID != *input.DocumentID {
			return nil, ErrInvalidInput
		}
	}
	if !input.ExpectNoAnswer && len(input.ExpectedDocumentIDs) == 0 && len(input.ExpectedChunkIDs) == 0 && len(input.ExpectedSections) == 0 {
		return nil, ErrInvalidInput
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	source := input.Source
	if source == "" {
		source = "manual"
	}
	if !validSource(source) {
		return nil, ErrInvalidInput
	}
	testCase := &model.KBRetrievalTestCase{
		DocumentID: input.DocumentID, DocumentVersionID: input.DocumentVersionID,
		Question: strings.TrimSpace(input.Question), Category: input.Category,
		ExpectedDocumentIDs: mustJSON(input.ExpectedDocumentIDs), ExpectedChunkIDs: mustJSON(input.ExpectedChunkIDs),
		ExpectedSections: mustJSON(cleanStrings(input.ExpectedSections)), MustIncludeFacts: mustJSON(cleanStrings(input.MustIncludeFacts)),
		MustNotInclude: mustJSON(cleanStrings(input.MustNotInclude)), ExpectNoAnswer: input.ExpectNoAnswer,
		Source: source, Enabled: enabled, CreatedBy: actor.ID,
	}
	if err := s.repository.CreateRetrievalTestCase(ctx, testCase); err != nil {
		return nil, err
	}
	return testCase, nil
}

func (s *Service) ListTestCases(ctx context.Context, actor *model.AppUser, versionID *int64) ([]model.KBRetrievalTestCase, error) {
	if actor == nil {
		return nil, ErrInvalidInput
	}
	return s.repository.ListRetrievalTestCases(ctx, versionID, nil, false)
}

func (s *Service) RunSmoke(ctx context.Context, actor *model.AppUser, versionID int64, config RunConfig) (*model.KBRetrievalEvaluationRun, error) {
	if versionID <= 0 {
		return nil, ErrInvalidInput
	}
	config.DocumentVersionID = &versionID
	if config.Name == "" {
		config.Name = fmt.Sprintf("Smoke Test - version %d", versionID)
	}
	return s.run(ctx, actor, model.RetrievalEvaluationModeSmoke, config)
}

func (s *Service) RunLab(ctx context.Context, actor *model.AppUser, input LabInput) ([]*model.KBRetrievalEvaluationRun, error) {
	if actor == nil || actor.Role != model.RoleAdmin || len(input.Variants) < 2 || len(input.Variants) > 10 {
		return nil, ErrInvalidInput
	}
	runs := make([]*model.KBRetrievalEvaluationRun, 0, len(input.Variants))
	for index, config := range input.Variants {
		config.CaseIDs = input.CaseIDs
		config.DocumentVersionID = input.DocumentVersionID
		if config.Name == "" {
			config.Name = fmt.Sprintf("Variant %d", index+1)
		}
		run, err := s.run(ctx, actor, model.RetrievalEvaluationModeLab, config)
		if err != nil {
			return runs, err
		}
		runs = append(runs, run)
	}
	return runs, nil
}

func (s *Service) run(ctx context.Context, actor *model.AppUser, mode string, config RunConfig) (*model.KBRetrievalEvaluationRun, error) {
	if actor == nil || actor.Role != model.RoleAdmin || s.retriever == nil {
		return nil, ErrInvalidInput
	}
	if !validOptionalID(config.DocumentVersionID) || !validOptionalID(config.EmbeddingConfigID) || !validOptionalID(config.RerankConfigID) || !validOptionalID(config.ChunkStrategyID) || config.Thresholds.MinimumRecallAtK < 0 || config.Thresholds.MinimumRecallAtK > 1 || config.Thresholds.MinimumCitationAccuracy < 0 || config.Thresholds.MinimumCitationAccuracy > 1 {
		return nil, ErrInvalidInput
	}
	if config.DocumentVersionID != nil {
		if _, err := s.repository.FindDocumentVersionByID(ctx, *config.DocumentVersionID); err != nil {
			return nil, err
		}
	}
	testCases, err := s.repository.ListRetrievalTestCases(ctx, config.DocumentVersionID, config.CaseIDs, true)
	if err != nil {
		return nil, err
	}
	if len(testCases) == 0 {
		return nil, ErrInvalidInput
	}
	config.Limit = normalizeK(config.Limit)
	config.Thresholds = normalizeThresholds(config.Thresholds)
	configJSON, _ := json.Marshal(config)
	thresholdJSON, _ := json.Marshal(config.Thresholds)
	emptyJSON := json.RawMessage(`{}`)
	run := &model.KBRetrievalEvaluationRun{
		Mode: mode, Name: strings.TrimSpace(config.Name), Status: model.RetrievalEvaluationRunning,
		DocumentVersionID: config.DocumentVersionID, EmbeddingConfigID: config.EmbeddingConfigID,
		EmbeddingModelRevision: optionalString(config.EmbeddingModelRevision), RerankConfigID: config.RerankConfigID,
		ChunkStrategyID: config.ChunkStrategyID, RetrievalConfig: configJSON, Thresholds: thresholdJSON,
		Metrics: emptyJSON, CreatedBy: actor.ID,
	}
	if err := s.repository.CreateRetrievalEvaluationRun(ctx, run); err != nil {
		return nil, err
	}
	results := make([]model.KBRetrievalEvaluationResult, 0, len(testCases))
	caseMetrics := make([]CaseMetrics, 0, len(testCases))
	for _, testCase := range testCases {
		result, metrics := s.evaluateCase(ctx, actor, run.ID, testCase, config)
		results = append(results, result)
		caseMetrics = append(caseMetrics, metrics)
		if run.EmbeddingModel == nil {
			var trace rag.RetrievalTrace
			if json.Unmarshal(result.RetrievalTrace, &trace) == nil {
				run.EmbeddingModel = optionalString(trace.Configuration.EmbeddingModel)
				run.EmbeddingConfigID = trace.Configuration.EmbeddingConfigID
				if run.EmbeddingModelRevision == nil {
					run.EmbeddingModelRevision = optionalString(trace.Configuration.EmbeddingModelRevision)
				}
				run.RerankModel = optionalString(trace.Configuration.RerankModel)
				run.RerankConfigID = trace.Configuration.RerankConfigID
				if run.ChunkStrategyID == nil {
					run.ChunkStrategyID = trace.Configuration.ChunkStrategyID
				}
			}
		}
	}
	aggregate := aggregateMetrics(caseMetrics, testCases)
	metricsJSON, _ := json.Marshal(aggregate)
	now := s.now()
	run.Status, run.CaseCount, run.Metrics, run.CompletedAt = model.RetrievalEvaluationCompleted, len(testCases), metricsJSON, &now
	run.Passed = aggregate.RecallAtK >= config.Thresholds.MinimumRecallAtK && aggregate.CitationAccuracy >= config.Thresholds.MinimumCitationAccuracy
	if mode == model.RetrievalEvaluationModeSmoke {
		run.Passed = run.Passed && aggregate.SmokeCoverage == 1
	}
	if err := s.repository.CompleteRetrievalEvaluationRun(ctx, run, results); err != nil {
		return nil, err
	}
	run.Results = results
	return run, nil
}

func (s *Service) evaluateCase(ctx context.Context, actor *model.AppUser, runID int64, testCase model.KBRetrievalTestCase, config RunConfig) (model.KBRetrievalEvaluationResult, CaseMetrics) {
	search, err := s.retriever.EvaluateRetrieval(ctx, actor, rag.EvaluationSearchInput{
		Question: testCase.Question, Limit: config.Limit, DocumentVersionID: config.DocumentVersionID, EmbeddingConfigID: config.EmbeddingConfigID,
		EmbeddingModelRevision: config.EmbeddingModelRevision, RerankConfigID: config.RerankConfigID,
		ChunkStrategyID: config.ChunkStrategyID, DisableEmbedding: config.DisableEmbedding, DisableRerank: config.DisableRerank,
	})
	if err != nil {
		message := err.Error()
		return model.KBRetrievalEvaluationResult{RunID: runID, TestCaseID: testCase.ID, RetrievedDocumentIDs: json.RawMessage(`[]`), RetrievedChunkIDs: json.RawMessage(`[]`), CitationIDs: json.RawMessage(`[]`), Metrics: json.RawMessage(`{}`), RetrievalTrace: json.RawMessage(`{}`), ErrorMessage: &message}, CaseMetrics{}
	}
	expectedDocs, expectedChunks := decodeInt64s(testCase.ExpectedDocumentIDs), decodeInt64s(testCase.ExpectedChunkIDs)
	expectedSections := decodeStrings(testCase.ExpectedSections)
	retrievedDocs, retrievedChunks, citationIDs := []int64{}, []int64{}, []string{}
	for _, citation := range search.Citations {
		retrievedDocs = append(retrievedDocs, citation.DocumentID)
		retrievedChunks = append(retrievedChunks, citation.ChunkIDs...)
		citationIDs = append(citationIDs, citation.CitationID)
	}
	expected, retrieved := expectedChunks, retrievedChunks
	if len(expected) == 0 {
		expected, retrieved = expectedDocs, retrievedDocs
	}
	ranking := CalculateRankingMetrics(expected, retrieved, config.Limit)
	citationAccuracy := CitationAccuracy(expected, retrieved, testCase.ExpectNoAnswer)
	if len(expected) == 0 && len(expectedSections) > 0 {
		retrievedSections := make([]string, 0, len(search.Citations))
		for _, citation := range search.Citations {
			if citation.SourceSection != nil {
				retrievedSections = append(retrievedSections, *citation.SourceSection)
			} else {
				retrievedSections = append(retrievedSections, "")
			}
		}
		ranking, citationAccuracy = sectionMetrics(expectedSections, retrievedSections, config.Limit)
	}
	groundedness, prohibited := factMetrics(search.ContextText, decodeStrings(testCase.MustIncludeFacts), decodeStrings(testCase.MustNotInclude))
	noAnswerCorrect := 0.0
	if testCase.ExpectNoAnswer && len(search.Citations) == 0 {
		noAnswerCorrect = 1
	}
	metrics := CaseMetrics{RankingMetrics: ranking, CitationAccuracy: citationAccuracy, AnswerGroundedness: groundedness, NoAnswerCorrect: noAnswerCorrect, ProhibitedHitRate: prohibited}
	passed := false
	if testCase.ExpectNoAnswer {
		passed = noAnswerCorrect == 1 && prohibited == 0
	} else {
		passed = ranking.HitRate == 1 && citationAccuracy > 0 && groundedness == 1 && prohibited == 0
	}
	metricsJSON, _ := json.Marshal(metrics)
	traceJSON, _ := json.Marshal(search.RetrievalTrace)
	return model.KBRetrievalEvaluationResult{
		RunID: runID, TestCaseID: testCase.ID, RetrievedDocumentIDs: mustJSON(retrievedDocs), RetrievedChunkIDs: mustJSON(retrievedChunks),
		CitationIDs: mustJSON(citationIDs), ContextText: search.ContextText, Metrics: metricsJSON, RetrievalTrace: traceJSON, Passed: passed,
	}, metrics
}

func (s *Service) GetRun(ctx context.Context, actor *model.AppUser, id int64) (*model.KBRetrievalEvaluationRun, error) {
	if actor == nil || id <= 0 {
		return nil, ErrInvalidInput
	}
	return s.repository.FindRetrievalEvaluationRun(ctx, id)
}

func (s *Service) ListRuns(ctx context.Context, actor *model.AppUser, limit int) ([]model.KBRetrievalEvaluationRun, error) {
	if actor == nil {
		return nil, ErrInvalidInput
	}
	return s.repository.ListRetrievalEvaluationRuns(ctx, limit)
}

func aggregateMetrics(values []CaseMetrics, cases []model.KBRetrievalTestCase) AggregateMetrics {
	result := AggregateMetrics{}
	if len(values) == 0 {
		return result
	}
	noAnswerCount := 0
	categorySeen := map[string]struct{}{}
	for index, value := range values {
		result.RecallAtK += value.RecallAtK
		result.MRR += value.MRR
		result.NDCGAtK += value.NDCGAtK
		result.HitRate += value.HitRate
		result.CitationAccuracy += value.CitationAccuracy
		result.AnswerGroundedness += value.AnswerGroundedness
		categorySeen[cases[index].Category] = struct{}{}
		if cases[index].ExpectNoAnswer {
			result.NoAnswerPrecision += value.NoAnswerCorrect
			noAnswerCount++
		}
	}
	count := float64(len(values))
	result.RecallAtK /= count
	result.MRR /= count
	result.NDCGAtK /= count
	result.HitRate /= count
	result.CitationAccuracy /= count
	result.AnswerGroundedness /= count
	if noAnswerCount > 0 {
		result.NoAnswerPrecision /= float64(noAnswerCount)
	}
	covered := 0
	for _, category := range smokeCategories {
		if _, ok := categorySeen[category]; ok {
			covered++
		}
	}
	result.SmokeCoverage = float64(covered) / float64(len(smokeCategories))
	return result
}

func factMetrics(contextText string, required, prohibited []string) (float64, float64) {
	lower := strings.ToLower(contextText)
	grounded := 1.0
	if len(required) > 0 {
		hits := 0
		for _, fact := range required {
			if strings.Contains(lower, strings.ToLower(fact)) {
				hits++
			}
		}
		grounded = float64(hits) / float64(len(required))
	}
	prohibitedHits := 0
	for _, value := range prohibited {
		if strings.Contains(lower, strings.ToLower(value)) {
			prohibitedHits++
		}
	}
	if len(prohibited) == 0 {
		return grounded, 0
	}
	return grounded, float64(prohibitedHits) / float64(len(prohibited))
}

func sectionMetrics(expected, retrieved []string, k int) (RankingMetrics, float64) {
	expectedIDs := make([]int64, len(expected))
	for index := range expected {
		expectedIDs[index] = int64(index + 1)
	}
	retrievedIDs := make([]int64, 0, len(retrieved))
	correct := 0
	for index, section := range retrieved {
		matched := int64(-(index + 1))
		for expectedIndex, value := range expected {
			if strings.Contains(strings.ToLower(section), strings.ToLower(value)) {
				matched = int64(expectedIndex + 1)
				correct++
				break
			}
		}
		retrievedIDs = append(retrievedIDs, matched)
	}
	accuracy := 0.0
	if len(retrieved) > 0 {
		accuracy = float64(correct) / float64(len(retrieved))
	}
	return CalculateRankingMetrics(expectedIDs, retrievedIDs, k), accuracy
}

func normalizeK(value int) int {
	if value <= 0 {
		return 5
	}
	if value > 20 {
		return 20
	}
	return value
}
func normalizeThresholds(value Thresholds) Thresholds {
	if value.MinimumRecallAtK == 0 {
		value.MinimumRecallAtK = .8
	}
	if value.MinimumCitationAccuracy == 0 {
		value.MinimumCitationAccuracy = .95
	}
	return value
}
func validCategory(value string) bool {
	for _, item := range append(smokeCategories, "custom") {
		if value == item {
			return true
		}
	}
	return false
}
func validSource(value string) bool {
	for _, item := range []string{"manual", "author", "llm_reviewed", "qa_feedback"} {
		if value == item {
			return true
		}
	}
	return false
}
func mustJSON(value any) json.RawMessage { raw, _ := json.Marshal(value); return raw }
func decodeInt64s(raw json.RawMessage) []int64 {
	var result []int64
	_ = json.Unmarshal(raw, &result)
	return result
}
func decodeStrings(raw json.RawMessage) []string {
	var result []string
	_ = json.Unmarshal(raw, &result)
	return result
}
func cleanStrings(values []string) []string {
	result := []string{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
func optionalString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	value = strings.TrimSpace(value)
	return &value
}
func validOptionalID(value *int64) bool { return value == nil || *value > 0 }
