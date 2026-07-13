package rag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	llmsvc "aiops-platform/backend/internal/llm"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

const (
	defaultRecallLimit  = 5
	maxRecallLimit      = 10
	maxQuestionBytes    = 8192
	noEvidenceAnswer    = "未找到可依据的已发布知识，无法基于知识库回答该问题。"
	maxVectorCandidates = 1000
	maxVectorBuildBatch = 100
)

var (
	ErrInvalidInput = errors.New("invalid input")
	ErrForbidden    = errors.New("rag access forbidden")
)

type Repository interface {
	CreateConversation(ctx context.Context, conversation *model.Conversation) error
	FindConversationByID(ctx context.Context, id int64) (*model.Conversation, error)
	CreateMessage(ctx context.Context, message *model.Message) error
	SearchChunks(ctx context.Context, query string, limit int) ([]model.KBChunk, error)
	ListPublishedChunks(ctx context.Context, limit int) ([]model.KBChunk, error)
	ListPublishedChunkEmbeddings(ctx context.Context, modelName string, limit int) ([]model.KBChunkEmbedding, error)
	ListPublishedChunksMissingEmbedding(ctx context.Context, modelName string, limit int) ([]model.KBChunk, error)
	UpsertChunkEmbeddings(ctx context.Context, embeddings []model.KBChunkEmbedding) error
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
	APISecret string
}

type AskInput struct {
	ConversationID *int64
	Question       string
	Limit          int
}

type Citation struct {
	DocumentID    int64   `json:"documentId"`
	ChunkID       int64   `json:"chunkId"`
	ChunkIndex    int     `json:"chunkIndex"`
	SourceTitle   *string `json:"sourceTitle,omitempty"`
	SourceSection *string `json:"sourceSection,omitempty"`
	Snippet       string  `json:"snippet"`
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
	rewritten := s.rewriteQuery(ctx, question, llmConfig, llmCredential, llmReady)
	chunks, err := s.recall(ctx, question, rewritten, limit*4)
	if err != nil {
		return nil, fmt.Errorf("recall knowledge chunks: %w", err)
	}
	if embeddingReady {
		chunks = s.embeddingRank(ctx, rewritten, chunks, limit*3, embeddingConfig, embeddingCredential)
	}
	if rerankReady {
		chunks = s.modelRerank(ctx, question, chunks, limit, rerankConfig, rerankCredential)
	} else {
		chunks = lexicalRerankChunks(question, rewritten, chunks, limit)
	}
	citations := buildCitations(chunks)
	answer, err := s.answer(ctx, question, rewritten, chunks, citations, llmConfig, llmCredential, llmReady)
	if err != nil {
		return nil, err
	}
	citationJSON, err := json.Marshal(citations)
	if err != nil {
		return nil, fmt.Errorf("encode citations: %w", err)
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
		"recallCount":    len(chunks),
		"embeddingModel": modelName(embeddingConfig, embeddingReady),
		"rerankModel":    modelName(rerankConfig, rerankReady),
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
		RecallCount:    len(chunks),
		LLMConfigID:    llmConfigID,
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
		RecallCount:  len(chunks),
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

func (s *Service) recall(ctx context.Context, question, rewritten string, limit int) ([]model.KBChunk, error) {
	chunks, err := s.repository.SearchChunks(ctx, rewritten, limit)
	if err != nil || len(chunks) > 0 {
		return chunks, err
	}
	seen := make(map[int64]struct{})
	var recalled []model.KBChunk
	for _, term := range tokenize(question + " " + rewritten) {
		if len([]rune(term)) < 2 {
			continue
		}
		more, err := s.repository.SearchChunks(ctx, term, limit)
		if err != nil {
			return nil, err
		}
		for _, chunk := range more {
			if _, ok := seen[chunk.ID]; ok {
				continue
			}
			seen[chunk.ID] = struct{}{}
			recalled = append(recalled, chunk)
			if len(recalled) >= limit {
				return recalled, nil
			}
		}
	}
	return recalled, nil
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
	return credential, nil
}

func (s *Service) embeddingRank(ctx context.Context, query string, chunks []model.KBChunk, limit int, config *model.LLMConfig, credential modelCredential) []model.KBChunk {
	client, ok := s.client.(llmsvc.EmbeddingClient)
	if !ok || config == nil {
		return chunks
	}
	result, err := client.Embed(ctx, llmsvc.EmbeddingRequest{
		BaseURL:   config.BaseURL,
		APIKey:    credential.APIKey,
		APISecret: credential.APISecret,
		Model:     config.Model,
		Input:     []string{query},
	})
	if err != nil || result == nil || len(result.Embeddings) == 0 {
		return chunks
	}
	queryEmbedding := result.Embeddings[0]
	s.ensureChunkEmbeddings(ctx, client, config, credential)
	indexes, err := s.repository.ListPublishedChunkEmbeddings(ctx, config.Model, maxVectorCandidates)
	if err != nil || len(indexes) == 0 {
		return chunks
	}
	type scoredChunk struct {
		chunk model.KBChunk
		score float64
	}
	scored := make([]scoredChunk, 0, len(indexes))
	for _, item := range indexes {
		vector, err := decodeEmbedding(item.Embedding)
		if err != nil || item.Dimension != len(vector) {
			continue
		}
		scored = append(scored, scoredChunk{chunk: item.Chunk, score: cosine(queryEmbedding, vector)})
	}
	if len(scored) == 0 {
		return chunks
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})
	ranked := make([]model.KBChunk, 0, len(scored))
	for _, item := range scored {
		if item.chunk.ID == 0 {
			continue
		}
		ranked = append(ranked, item.chunk)
		if len(ranked) >= limit {
			return ranked
		}
	}
	if len(ranked) == 0 {
		return chunks
	}
	return ranked
}

func (s *Service) ensureChunkEmbeddings(ctx context.Context, client llmsvc.EmbeddingClient, config *model.LLMConfig, credential modelCredential) {
	missing, err := s.repository.ListPublishedChunksMissingEmbedding(ctx, config.Model, maxVectorBuildBatch)
	if err != nil || len(missing) == 0 {
		return
	}
	inputs := make([]string, 0, len(missing))
	for _, chunk := range missing {
		inputs = append(inputs, chunk.Content)
	}
	result, err := client.Embed(ctx, llmsvc.EmbeddingRequest{
		BaseURL:   config.BaseURL,
		APIKey:    credential.APIKey,
		APISecret: credential.APISecret,
		Model:     config.Model,
		Input:     inputs,
	})
	if err != nil || result == nil || len(result.Embeddings) != len(missing) {
		return
	}
	configID := config.ID
	embeddings := make([]model.KBChunkEmbedding, 0, len(missing))
	for index, chunk := range missing {
		vector := result.Embeddings[index]
		if len(vector) == 0 {
			continue
		}
		encoded, err := json.Marshal(vector)
		if err != nil {
			continue
		}
		embeddings = append(embeddings, model.KBChunkEmbedding{
			ChunkID:     chunk.ID,
			LLMConfigID: &configID,
			Model:       config.Model,
			Dimension:   len(vector),
			Embedding:   encoded,
		})
	}
	_ = s.repository.UpsertChunkEmbeddings(ctx, embeddings)
}

func (s *Service) modelRerank(ctx context.Context, query string, chunks []model.KBChunk, limit int, config *model.LLMConfig, credential modelCredential) []model.KBChunk {
	client, ok := s.client.(llmsvc.RerankClient)
	if !ok || config == nil || len(chunks) == 0 {
		return lexicalRerankChunks(query, query, chunks, limit)
	}
	documents := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		documents = append(documents, chunk.Content)
	}
	result, err := client.Rerank(ctx, llmsvc.RerankRequest{
		BaseURL:   config.BaseURL,
		APIKey:    credential.APIKey,
		APISecret: credential.APISecret,
		Model:     config.Model,
		Query:     query,
		Documents: documents,
		TopN:      limit,
	})
	if err != nil || len(result.Results) == 0 {
		return lexicalRerankChunks(query, query, chunks, limit)
	}
	ranked := make([]model.KBChunk, 0, len(result.Results))
	seen := map[int]struct{}{}
	for _, item := range result.Results {
		if item.Index < 0 || item.Index >= len(chunks) {
			continue
		}
		if _, ok := seen[item.Index]; ok {
			continue
		}
		seen[item.Index] = struct{}{}
		ranked = append(ranked, chunks[item.Index])
		if len(ranked) >= limit {
			return ranked
		}
	}
	for index, chunk := range chunks {
		if _, ok := seen[index]; ok {
			continue
		}
		ranked = append(ranked, chunk)
		if len(ranked) >= limit {
			return ranked
		}
	}
	return ranked
}

func (s *Service) rewriteQuery(ctx context.Context, question string, config *model.LLMConfig, credential modelCredential, ready bool) string {
	fallback := ruleBasedRewrite(question)
	if !ready || config == nil {
		return fallback
	}
	result, err := s.client.Chat(ctx, llmsvc.ChatRequest{
		BaseURL:     config.BaseURL,
		APIKey:      credential.APIKey,
		APISecret:   credential.APISecret,
		Model:       config.Model,
		Temperature: 0,
		Messages: []llmsvc.ChatMessage{
			{Role: model.MessageRoleSystem, Content: "Rewrite the user's operations question into a concise Chinese knowledge-base search query. Return only the query."},
			{Role: model.MessageRoleUser, Content: question},
		},
	})
	if err != nil {
		return fallback
	}
	rewritten, err := normalizeQuestion(result.Content)
	if err != nil {
		return fallback
	}
	return rewritten
}

func (s *Service) answer(ctx context.Context, question, rewritten string, chunks []model.KBChunk, citations []Citation, config *model.LLMConfig, credential modelCredential, ready bool) (string, error) {
	if len(chunks) == 0 {
		return noEvidenceAnswer, nil
	}
	if !ready || config == nil {
		return localAnswer(chunks), nil
	}
	result, err := s.client.Chat(ctx, llmsvc.ChatRequest{
		BaseURL:     config.BaseURL,
		APIKey:      credential.APIKey,
		APISecret:   credential.APISecret,
		Model:       config.Model,
		Temperature: config.Temperature,
		Messages: []llmsvc.ChatMessage{
			{Role: model.MessageRoleSystem, Content: "Answer strictly from the provided published knowledge chunks. If the chunks do not support the answer, say there is no evidence. Cite chunk numbers like [1]."},
			{Role: model.MessageRoleUser, Content: buildAnswerPrompt(question, rewritten, chunks)},
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

func lexicalRerankChunks(question, rewritten string, chunks []model.KBChunk, limit int) []model.KBChunk {
	terms := tokenize(question + " " + rewritten)
	sort.SliceStable(chunks, func(i, j int) bool {
		return chunkScore(chunks[i], terms) > chunkScore(chunks[j], terms)
	})
	if len(chunks) > limit {
		return chunks[:limit]
	}
	return chunks
}

func dedupeChunks(chunks []model.KBChunk) []model.KBChunk {
	seen := map[int64]struct{}{}
	result := make([]model.KBChunk, 0, len(chunks))
	for _, chunk := range chunks {
		if _, ok := seen[chunk.ID]; ok {
			continue
		}
		seen[chunk.ID] = struct{}{}
		result = append(result, chunk)
	}
	return result
}

func cosine(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for index := range a {
		dot += a[index] * b[index]
		normA += a[index] * a[index]
		normB += b[index] * b[index]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func decodeEmbedding(raw []byte) ([]float64, error) {
	var vector []float64
	if err := json.Unmarshal(raw, &vector); err != nil {
		return nil, err
	}
	return vector, nil
}

func modelName(config *model.LLMConfig, ready bool) string {
	if !ready || config == nil {
		return ""
	}
	return config.Model
}

func chunkScore(chunk model.KBChunk, terms []string) int {
	text := strings.ToLower(chunk.Content)
	if chunk.SearchText != nil {
		text += "\n" + strings.ToLower(*chunk.SearchText)
	}
	score := 0
	for _, term := range terms {
		if strings.Contains(text, term) {
			score += len([]rune(term)) + 1
		}
	}
	return score
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

func buildCitations(chunks []model.KBChunk) []Citation {
	citations := make([]Citation, 0, len(chunks))
	for _, chunk := range chunks {
		citations = append(citations, Citation{
			DocumentID:    chunk.DocumentID,
			ChunkID:       chunk.ID,
			ChunkIndex:    chunk.ChunkIndex,
			SourceTitle:   chunk.SourceTitle,
			SourceSection: chunk.SourceSection,
			Snippet:       snippet(chunk.Content),
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

func localAnswer(chunks []model.KBChunk) string {
	lines := []string{"根据已发布知识库资料："}
	for index, chunk := range chunks {
		lines = append(lines, fmt.Sprintf("- [%d] %s", index+1, snippet(chunk.Content)))
	}
	return strings.Join(lines, "\n")
}

func buildAnswerPrompt(question, rewritten string, chunks []model.KBChunk) string {
	var builder strings.Builder
	builder.WriteString("Question: ")
	builder.WriteString(question)
	builder.WriteString("\nRewritten query: ")
	builder.WriteString(rewritten)
	builder.WriteString("\nPublished knowledge chunks:\n")
	for index, chunk := range chunks {
		builder.WriteString(fmt.Sprintf("[%d] document_id=%d chunk_id=%d chunk_index=%d\n", index+1, chunk.DocumentID, chunk.ID, chunk.ChunkIndex))
		builder.WriteString(chunk.Content)
		builder.WriteString("\n")
	}
	return builder.String()
}
