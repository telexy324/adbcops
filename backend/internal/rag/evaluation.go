package rag

import (
	"context"
	"fmt"
	"strings"

	llmsvc "aiops-platform/backend/internal/llm"
	"aiops-platform/backend/internal/model"
)

type EvaluationSearchInput struct {
	Question               string
	Limit                  int
	EmbeddingConfigID      *int64
	EmbeddingModelRevision string
	RerankConfigID         *int64
	ChunkStrategyID        *int64
	DisableEmbedding       bool
	DisableRerank          bool
}

type EvaluationSearchResult struct {
	Citations      []Citation     `json:"citations"`
	Contexts       []ContextBlock `json:"contexts"`
	RetrievalTrace RetrievalTrace `json:"retrievalTrace"`
	ContextText    string         `json:"contextText"`
}

// EvaluateRetrieval executes the production retrieval pipeline without creating
// conversations, messages, QA records, or answers.
func (s *Service) EvaluateRetrieval(ctx context.Context, actor *model.AppUser, input EvaluationSearchInput) (*EvaluationSearchResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	question, err := normalizeQuestion(input.Question)
	if err != nil {
		return nil, err
	}
	chatConfig, chatCredential, chatReady, err := s.loadLLM(ctx)
	if err != nil {
		return nil, err
	}
	embeddingConfig, embeddingCredential, embeddingReady, err := s.evaluationModel(ctx, model.LLMPurposeEmbedding, input.EmbeddingConfigID, input.DisableEmbedding)
	if err != nil {
		return nil, err
	}
	rerankConfig, rerankCredential, rerankReady, err := s.evaluationModel(ctx, model.LLMPurposeRerank, input.RerankConfigID, input.DisableRerank)
	if err != nil {
		return nil, err
	}
	embeddingRevision := s.readyEmbeddingRevision(ctx, embeddingConfig, embeddingReady, input.ChunkStrategyID, input.EmbeddingModelRevision)
	understood := s.understandQuery(ctx, question, chatConfig, chatCredential, chatReady)
	chunks, trace := s.hybridRetrieve(ctx, understood, embeddingConfig, embeddingCredential, embeddingReady, retrievalOptions{
		StrategyID: input.ChunkStrategyID, EmbeddingModelRevision: embeddingRevision,
	})
	trace.Configuration = retrievalConfiguration(embeddingConfig, embeddingReady, embeddingRevision, rerankConfig, rerankReady, input.ChunkStrategyID)
	documents, documentErr := s.loadRetrievalDocuments(ctx, chunks)
	if documentErr != nil {
		documents = map[int64]model.KBDocument{}
	}
	chunks, rerankTrace := s.rerankCandidates(ctx, question, chunks, documents, rerankConfig, rerankCredential, rerankReady)
	if documentErr != nil {
		rerankTrace.Degraded = true
		rerankTrace.Error = documentErr.Error()
	}
	contexts, contextTrace := s.buildContext(ctx, chunks, documents, buildContextEvidence(trace, rerankTrace), normalizeLimit(input.Limit), defaultContextBudget)
	trace.Rerank, trace.Context = rerankTrace, contextTrace
	contextParts := make([]string, 0, len(contexts))
	for _, block := range contexts {
		contextParts = append(contextParts, block.Content)
	}
	return &EvaluationSearchResult{
		Citations: buildContextCitations(contexts), Contexts: contexts,
		RetrievalTrace: trace, ContextText: strings.Join(contextParts, "\n\n"),
	}, nil
}

func (s *Service) readyEmbeddingRevision(ctx context.Context, config *model.LLMConfig, ready bool, strategyID *int64, requested string) string {
	if strings.TrimSpace(requested) != "" || !ready || config == nil {
		return strings.TrimSpace(requested)
	}
	revision, err := s.repository.FindReadyEmbeddingModelRevision(ctx, config.ID, strategyID)
	if err != nil {
		return ""
	}
	return revision
}

func (s *Service) evaluationModel(ctx context.Context, purpose string, id *int64, disabled bool) (*model.LLMConfig, modelCredential, bool, error) {
	if disabled {
		return nil, modelCredential{}, false, nil
	}
	if id == nil {
		config, credential, ready := s.loadOptionalModel(ctx, purpose)
		return config, credential, ready, nil
	}
	if *id <= 0 {
		return nil, modelCredential{}, false, ErrInvalidInput
	}
	config, err := s.repository.FindLLMConfigByID(ctx, *id)
	if err != nil {
		return nil, modelCredential{}, false, err
	}
	if !config.Enabled || config.Purpose != purpose || s.client == nil {
		return nil, modelCredential{}, false, fmt.Errorf("%w: unavailable %s config", ErrInvalidInput, purpose)
	}
	if purpose == model.LLMPurposeEmbedding {
		if _, ok := s.client.(llmsvc.EmbeddingClient); !ok {
			return nil, modelCredential{}, false, fmt.Errorf("%w: embedding client unavailable", ErrInvalidInput)
		}
	}
	if purpose == model.LLMPurposeRerank {
		if _, ok := s.client.(llmsvc.RerankClient); !ok {
			return nil, modelCredential{}, false, fmt.Errorf("%w: rerank client unavailable", ErrInvalidInput)
		}
	}
	credential, err := s.decryptModelCredential(config)
	if err != nil {
		return nil, modelCredential{}, false, err
	}
	return config, credential, true, nil
}
