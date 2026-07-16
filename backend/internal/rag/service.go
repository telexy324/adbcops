package rag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	llmsvc "aiops-platform/backend/internal/llm"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

const (
	defaultRecallLimit = 5
	maxRecallLimit     = 10
	maxQuestionBytes   = 8192
	noEvidenceAnswer   = "未找到可依据的已发布知识，无法基于知识库回答该问题。"
)

var (
	ErrInvalidInput = errors.New("invalid input")
	ErrForbidden    = errors.New("rag access forbidden")
)

type Repository interface {
	CreateConversation(ctx context.Context, conversation *model.Conversation) error
	FindConversationByID(ctx context.Context, id int64) (*model.Conversation, error)
	CreateMessage(ctx context.Context, message *model.Message) error
	SearchChunksTrigram(ctx context.Context, query string, filter repository.KnowledgeRetrievalFilter, limit int) ([]repository.RankedKnowledgeChunk, error)
	SearchChunksExact(ctx context.Context, terms []string, filter repository.KnowledgeRetrievalFilter, limit int) ([]repository.RankedKnowledgeChunk, error)
	SearchChunksTitleSection(ctx context.Context, query string, filter repository.KnowledgeRetrievalFilter, limit int) ([]repository.RankedKnowledgeChunk, error)
	SearchChunksPossibleQuestions(ctx context.Context, query string, filter repository.KnowledgeRetrievalFilter, limit int) ([]repository.RankedKnowledgeChunk, error)
	SearchChunksDense(ctx context.Context, vector []float64, configID int64, modelName string, filter repository.KnowledgeRetrievalFilter, limit int) ([]repository.RankedKnowledgeChunk, error)
	FindKnowledgeDocumentsByIDs(ctx context.Context, ids []int64) ([]model.KBDocument, error)
	FindKnowledgeChunksByIDs(ctx context.Context, ids []int64) ([]model.KBChunk, error)
	FindDefaultEnabledLLMConfig(ctx context.Context) (*model.LLMConfig, error)
	FindDefaultEnabledLLMConfigByPurpose(ctx context.Context, purpose string) (*model.LLMConfig, error)
	CreateQARecord(ctx context.Context, record *model.QARecord) error
}

type SecretManager interface {
	Decrypt(value string) (string, error)
}

type Service struct {
	repository Repository
	secrets    SecretManager
	client     llmsvc.Client
}

type modelCredential struct {
	APIKey    string
	AppKey    string
	APISecret string
}

type AskInput struct {
	ConversationID *int64
	Question       string
	Limit          int
}

type Citation struct {
	CitationID        string  `json:"citationId"`
	DocumentID        int64   `json:"documentId"`
	DocumentVersionID int64   `json:"documentVersionId"`
	ChunkID           int64   `json:"chunkId"`
	ChunkIDs          []int64 `json:"chunkIds"`
	ChunkIndex        int     `json:"chunkIndex"`
	SourceTitle       *string `json:"sourceTitle,omitempty"`
	SourceSection     *string `json:"sourceSection,omitempty"`
	Snippet           string  `json:"snippet"`
}

type AskResult struct {
	Conversation *model.Conversation `json:"-"`
	UserMessage  *model.Message      `json:"-"`
	Message      *model.Message      `json:"-"`
	QARecord     *model.QARecord     `json:"-"`
	Question     string              `json:"question"`
	Rewritten    string              `json:"rewrittenQuery"`
	Answer       string              `json:"answer"`
	Citations    []Citation          `json:"citations"`
	RecallCount  int                 `json:"recallCount"`
	Retrieval    RetrievalTrace      `json:"retrievalTrace"`
}

func NewService(repository Repository, secrets SecretManager, client llmsvc.Client) *Service {
	return &Service{repository: repository, secrets: secrets, client: client}
}

func (s *Service) Ask(ctx context.Context, actor *model.AppUser, input AskInput) (*AskResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	question, err := normalizeQuestion(input.Question)
	if err != nil {
		return nil, err
	}
	limit := normalizeLimit(input.Limit)
	conversation, err := s.ensureConversation(ctx, actor, input.ConversationID, question)
	if err != nil {
		return nil, err
	}
	llmConfig, llmCredential, llmReady, err := s.loadLLM(ctx)
	if err != nil {
		return nil, err
	}
	embeddingConfig, embeddingCredential, embeddingReady := s.loadOptionalModel(ctx, model.LLMPurposeEmbedding)
	rerankConfig, rerankCredential, rerankReady := s.loadOptionalModel(ctx, model.LLMPurposeRerank)
	understood := s.understandQuery(ctx, question, llmConfig, llmCredential, llmReady)
	rewritten := understood.NormalizedQuery
	chunks, retrievalTrace := s.hybridRetrieve(ctx, understood, embeddingConfig, embeddingCredential, embeddingReady)
	documents, documentErr := s.loadRetrievalDocuments(ctx, chunks)
	if documentErr != nil {
		documents = map[int64]model.KBDocument{}
	}
	chunks, rerankTrace := s.rerankCandidates(ctx, question, chunks, documents, rerankConfig, rerankCredential, rerankReady)
	if documentErr != nil {
		rerankTrace.Degraded = true
		rerankTrace.Error = documentErr.Error()
	}
	contextBlocks, contextTrace := s.buildContext(ctx, chunks, documents, buildContextEvidence(retrievalTrace, rerankTrace), limit, defaultContextBudget)
	retrievalTrace.Rerank = rerankTrace
	retrievalTrace.Context = contextTrace
	citations := buildContextCitations(contextBlocks)
	answer, err := s.answer(ctx, question, rewritten, contextBlocks, citations, llmConfig, llmCredential, llmReady)
	if err != nil {
		return nil, err
	}
	citationJSON, err := json.Marshal(citations)
	if err != nil {
		return nil, fmt.Errorf("encode citations: %w", err)
	}
	retrievalJSON, err := json.Marshal(retrievalTrace)
	if err != nil {
		return nil, fmt.Errorf("encode retrieval trace: %w", err)
	}
	userMetadata, _ := json.Marshal(map[string]any{"source": "rag", "rewrittenQuery": rewritten})
	userMessage := &model.Message{
		ConversationID: conversation.ID,
		Role:           model.MessageRoleUser,
		Content:        question,
		Metadata:       userMetadata,
	}
	if err := s.repository.CreateMessage(ctx, userMessage); err != nil {
		return nil, fmt.Errorf("create user message: %w", err)
	}
	assistantMetadata, _ := json.Marshal(map[string]any{
		"source":         "rag",
		"recallCount":    len(contextBlocks),
		"embeddingModel": modelName(embeddingConfig, embeddingReady),
		"rerankModel":    modelName(rerankConfig, rerankReady),
		"retrievalTrace": json.RawMessage(retrievalJSON),
	})
	assistantMessage := &model.Message{
		ConversationID: conversation.ID,
		Role:           model.MessageRoleAssistant,
		Content:        answer,
		Citations:      citationJSON,
		Metadata:       assistantMetadata,
	}
	if err := s.repository.CreateMessage(ctx, assistantMessage); err != nil {
		return nil, fmt.Errorf("create assistant message: %w", err)
	}
	var llmConfigID *int64
	if llmReady && llmConfig != nil {
		id := llmConfig.ID
		llmConfigID = &id
	}
	conversationID := conversation.ID
	record := &model.QARecord{
		ConversationID: &conversationID,
		UserID:         actor.ID,
		Question:       question,
		RewrittenQuery: rewritten,
		Answer:         answer,
		Citations:      citationJSON,
		RecallCount:    len(contextBlocks),
		LLMConfigID:    llmConfigID,
		RetrievalTrace: retrievalJSON,
	}
	if err := s.repository.CreateQARecord(ctx, record); err != nil {
		return nil, fmt.Errorf("create qa record: %w", err)
	}
	return &AskResult{
		Conversation: conversation,
		UserMessage:  userMessage,
		Message:      assistantMessage,
		QARecord:     record,
		Question:     question,
		Rewritten:    rewritten,
		Answer:       answer,
		Citations:    citations,
		RecallCount:  len(contextBlocks),
		Retrieval:    retrievalTrace,
	}, nil
}

func (s *Service) ensureConversation(ctx context.Context, actor *model.AppUser, conversationID *int64, question string) (*model.Conversation, error) {
	if conversationID != nil {
		if *conversationID <= 0 {
			return nil, ErrInvalidInput
		}
		conversation, err := s.repository.FindConversationByID(ctx, *conversationID)
		if err != nil {
			return nil, err
		}
		if actor.Role != model.RoleAdmin && conversation.UserID != actor.ID {
			return nil, ErrForbidden
		}
		return conversation, nil
	}
	title := question
	if len([]rune(title)) > 40 {
		title = string([]rune(title)[:40])
	}
	conversation := &model.Conversation{
		UserID: actor.ID,
		Title:  &title,
		Status: model.ConversationStatusActive,
	}
	if err := s.repository.CreateConversation(ctx, conversation); err != nil {
		return nil, fmt.Errorf("create conversation: %w", err)
	}
	return conversation, nil
}

func (s *Service) loadLLM(ctx context.Context) (*model.LLMConfig, modelCredential, bool, error) {
	config, err := s.repository.FindDefaultEnabledLLMConfig(ctx)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, modelCredential{}, false, nil
	}
	if err != nil {
		return nil, modelCredential{}, false, fmt.Errorf("load default llm config: %w", err)
	}
	credential, err := s.decryptModelCredential(config)
	if err != nil {
		return nil, modelCredential{}, false, err
	}
	return config, credential, s.client != nil, nil
}

func (s *Service) loadOptionalModel(ctx context.Context, purpose string) (*model.LLMConfig, modelCredential, bool) {
	if s.client == nil {
		return nil, modelCredential{}, false
	}
	if purpose == model.LLMPurposeEmbedding {
		if _, ok := s.client.(llmsvc.EmbeddingClient); !ok {
			return nil, modelCredential{}, false
		}
	}
	if purpose == model.LLMPurposeRerank {
		if _, ok := s.client.(llmsvc.RerankClient); !ok {
			return nil, modelCredential{}, false
		}
	}
	config, err := s.repository.FindDefaultEnabledLLMConfigByPurpose(ctx, purpose)
	if err != nil {
		return nil, modelCredential{}, false
	}
	credential, err := s.decryptModelCredential(config)
	if err != nil {
		return nil, modelCredential{}, false
	}
	return config, credential, true
}

func (s *Service) decryptModelCredential(config *model.LLMConfig) (modelCredential, error) {
	if config == nil || s.secrets == nil {
		return modelCredential{}, nil
	}
	credential := modelCredential{}
	if config.APIKeyRef != nil && *config.APIKeyRef != "" && s.secrets != nil {
		decrypted, err := s.secrets.Decrypt(*config.APIKeyRef)
		if err != nil {
			return modelCredential{}, fmt.Errorf("decrypt api key: %w", err)
		}
		credential.APIKey = decrypted
	}
	if config.APISecretRef != nil && *config.APISecretRef != "" && s.secrets != nil {
		decrypted, err := s.secrets.Decrypt(*config.APISecretRef)
		if err != nil {
			return modelCredential{}, fmt.Errorf("decrypt api secret: %w", err)
		}
		credential.APISecret = decrypted
	}
	if config.AppKeyRef != nil && *config.AppKeyRef != "" && s.secrets != nil {
		decrypted, err := s.secrets.Decrypt(*config.AppKeyRef)
		if err != nil {
			return modelCredential{}, fmt.Errorf("decrypt app key: %w", err)
		}
		credential.AppKey = decrypted
	}
	return credential, nil
}

func (s *Service) answer(ctx context.Context, question, rewritten string, blocks []ContextBlock, citations []Citation, config *model.LLMConfig, credential modelCredential, ready bool) (string, error) {
	if len(blocks) == 0 {
		return noEvidenceAnswer, nil
	}
	if !ready || config == nil {
		return localAnswer(blocks), nil
	}
	result, err := s.client.Chat(ctx, llmsvc.ChatRequest{
		BaseURL:     config.BaseURL,
		Provider:    config.Provider,
		APIKey:      credential.APIKey,
		AppKey:      credential.AppKey,
		APISecret:   credential.APISecret,
		Model:       config.Model,
		Temperature: config.Temperature,
		Messages: []llmsvc.ChatMessage{
			{Role: model.MessageRoleSystem, Content: "Answer strictly from the provided published knowledge context. If the context does not support the answer, say there is no evidence. Cite claims with the exact citation ID shown in brackets."},
			{Role: model.MessageRoleUser, Content: buildAnswerPrompt(question, rewritten, blocks)},
		},
	})
	if err != nil {
		return "", fmt.Errorf("generate rag answer: %w", err)
	}
	answer := strings.TrimSpace(result.Content)
	if answer == "" {
		return noEvidenceAnswer, nil
	}
	_ = citations
	return answer, nil
}

func normalizeQuestion(question string) (string, error) {
	normalized := strings.TrimSpace(question)
	if normalized == "" || len(normalized) > maxQuestionBytes || !utf8.ValidString(normalized) {
		return "", ErrInvalidInput
	}
	return normalized, nil
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return defaultRecallLimit
	}
	if limit > maxRecallLimit {
		return maxRecallLimit
	}
	return limit
}

func ruleBasedRewrite(question string) string {
	replacer := strings.NewReplacer("？", " ", "?", " ", "，", " ", ",", " ", "。", " ", ".", " ", "\n", " ")
	return strings.Join(strings.Fields(replacer.Replace(question)), " ")
}

func modelName(config *model.LLMConfig, ready bool) string {
	if !ready || config == nil {
		return ""
	}
	return config.Model
}

func tokenize(value string) []string {
	value = strings.ToLower(value)
	var terms []string
	for _, field := range strings.FieldsFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r)
	}) {
		term := strings.TrimSpace(field)
		if len([]rune(term)) >= 2 {
			terms = append(terms, term)
		}
	}
	return terms
}

func buildContextCitations(blocks []ContextBlock) []Citation {
	citations := make([]Citation, 0, len(blocks))
	for _, block := range blocks {
		if len(block.ChunkIDs) == 0 {
			continue
		}
		citations = append(citations, Citation{
			CitationID:        block.CitationID,
			DocumentID:        block.DocumentID,
			DocumentVersionID: block.DocumentVersionID,
			ChunkID:           block.ChunkIDs[0],
			ChunkIDs:          append([]int64(nil), block.ChunkIDs...),
			ChunkIndex:        block.ChunkIndex,
			SourceTitle:       block.Title,
			SourceSection:     block.Section,
			Snippet:           snippet(block.Content),
		})
	}
	return citations
}

func snippet(content string) string {
	value := strings.TrimSpace(content)
	runes := []rune(value)
	if len(runes) <= 160 {
		return value
	}
	return string(runes[:160]) + "..."
}

func localAnswer(blocks []ContextBlock) string {
	lines := []string{"根据已发布知识库资料："}
	for _, block := range blocks {
		lines = append(lines, fmt.Sprintf("- [%s] %s", block.CitationID, snippet(block.Content)))
	}
	return strings.Join(lines, "\n")
}

func buildAnswerPrompt(question, rewritten string, blocks []ContextBlock) string {
	var builder strings.Builder
	builder.WriteString("Question: ")
	builder.WriteString(question)
	builder.WriteString("\nRewritten query: ")
	builder.WriteString(rewritten)
	builder.WriteString("\nPublished knowledge context:\n")
	for _, block := range blocks {
		builder.WriteString(fmt.Sprintf("[%s] document_id=%d document_version_id=%d chunk_ids=%v", block.CitationID, block.DocumentID, block.DocumentVersionID, block.ChunkIDs))
		if block.Title != nil {
			builder.WriteString(" title=" + *block.Title)
		}
		if block.Section != nil {
			builder.WriteString(" section=" + *block.Section)
		}
		if block.Applicability != "" {
			builder.WriteString(" applicability=" + block.Applicability)
		}
		builder.WriteString("\n")
		builder.WriteString(block.Content)
		builder.WriteString("\n")
	}
	return builder.String()
}
