package rag

import (
	"context"
	"fmt"

	"aiops-platform/backend/internal/model"
)

type publishedSearchResult struct {
	Rewritten       string
	Citations       []Citation
	ContextBlocks   []ContextBlock
	Retrieval       RetrievalTrace
	LLMConfig       *model.LLMConfig
	LLMCredential   modelCredential
	LLMReady        bool
	EmbeddingConfig *model.LLMConfig
	EmbeddingReady  bool
	RerankConfig    *model.LLMConfig
	RerankReady     bool
}

func (s *Service) searchPublishedKnowledge(ctx context.Context, actor *model.AppUser, query string, limit int) (*publishedSearchResult, error) {
	if actor == nil || actor.ID <= 0 || (actor.Role != model.RoleAdmin && actor.Role != model.RoleUser) {
		return nil, ErrForbidden
	}
	hasPublishedChunks, err := s.repository.HasPublishedChunks(ctx)
	if err != nil {
		return nil, fmt.Errorf("check published knowledge: %w", err)
	}
	var llmConfig, embeddingConfig, rerankConfig *model.LLMConfig
	var llmCredential, embeddingCredential, rerankCredential modelCredential
	var llmReady, embeddingReady, rerankReady bool
	if hasPublishedChunks {
		llmConfig, llmCredential, llmReady, err = s.loadLLM(ctx)
		if err != nil {
			return nil, err
		}
		embeddingConfig, embeddingCredential, embeddingReady = s.loadOptionalModel(ctx, model.LLMPurposeEmbedding)
		rerankConfig, rerankCredential, rerankReady = s.loadOptionalModel(ctx, model.LLMPurposeRerank)
	}
	embeddingRevision := s.readyEmbeddingRevision(ctx, embeddingConfig, embeddingReady, nil, "")
	understood := s.understandQuery(ctx, query, llmConfig, llmCredential, llmReady)
	rewritten := understood.NormalizedQuery
	options := retrievalOptions{EmbeddingModelRevision: embeddingRevision, Actor: actor}
	chunks, retrievalTrace := s.hybridRetrieve(ctx, understood, embeddingConfig, embeddingCredential, embeddingReady, options)
	logRetrievalAttempt(ctx, "query_understanding", retrievalTrace, chunks)
	if len(chunks) == 0 && llmReady {
		fallbackUnderstanding := relaxedQueryUnderstanding(query, understood)
		if !sameQueryUnderstanding(understood, fallbackUnderstanding) {
			fallbackChunks, fallbackTrace := s.hybridRetrieve(ctx, fallbackUnderstanding, embeddingConfig, embeddingCredential, embeddingReady, options)
			logRetrievalAttempt(ctx, "relaxed_fallback", fallbackTrace, fallbackChunks)
			fallbackTrace.Channels = append([]ChannelTrace{{
				Channel:  "local_query_fallback",
				Count:    len(fallbackChunks),
				Degraded: true,
				Error:    "LLM query filters returned no candidates; retried with local terms",
			}}, fallbackTrace.Channels...)
			chunks, retrievalTrace = fallbackChunks, fallbackTrace
			understood, rewritten = fallbackUnderstanding, fallbackUnderstanding.NormalizedQuery
		}
	}
	retrievalTrace.Configuration = retrievalConfiguration(embeddingConfig, embeddingReady, embeddingRevision, rerankConfig, rerankReady, nil)
	documents, documentErr := s.loadRetrievalDocuments(ctx, chunks)
	if documentErr != nil {
		documents = map[int64]model.KBDocument{}
	}
	chunks, rerankTrace := s.rerankCandidates(ctx, query, chunks, documents, rerankConfig, rerankCredential, rerankReady)
	if documentErr != nil {
		rerankTrace.Degraded = true
		rerankTrace.Error = documentErr.Error()
	}
	contextBlocks, contextTrace := s.buildContext(ctx, chunks, documents, buildContextEvidence(retrievalTrace, rerankTrace), limit, defaultContextBudget)
	retrievalTrace.Rerank = rerankTrace
	retrievalTrace.Context = contextTrace
	logRAGPipelineResult(ctx, retrievalTrace)
	return &publishedSearchResult{
		Rewritten: rewritten, Citations: buildContextCitations(contextBlocks), ContextBlocks: contextBlocks, Retrieval: retrievalTrace,
		LLMConfig: llmConfig, LLMCredential: llmCredential, LLMReady: llmReady,
		EmbeddingConfig: embeddingConfig, EmbeddingReady: embeddingReady, RerankConfig: rerankConfig, RerankReady: rerankReady,
	}, nil
}
