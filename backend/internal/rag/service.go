package rag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
	SearchChunks(ctx context.Context, query string, limit int) ([]model.KBChunk, error)
	FindDefaultEnabledLLMConfig(ctx context.Context) (*model.LLMConfig, error)
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
	llmConfig, apiKey, llmReady, err := s.loadLLM(ctx)
	if err != nil {
		return nil, err
	}
	rewritten := s.rewriteQuery(ctx, question, llmConfig, apiKey, llmReady)
	chunks, err := s.recall(ctx, question, rewritten, limit*2)
	if err != nil {
		return nil, fmt.Errorf("recall knowledge chunks: %w", err)
	}
	chunks = rerankChunks(question, rewritten, chunks, limit)
	citations := buildCitations(chunks)
	answer, err := s.answer(ctx, question, rewritten, chunks, citations, llmConfig, apiKey, llmReady)
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
	assistantMetadata, _ := json.Marshal(map[string]any{"source": "rag", "recallCount": len(chunks)})
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

func (s *Service) loadLLM(ctx context.Context) (*model.LLMConfig, string, bool, error) {
	config, err := s.repository.FindDefaultEnabledLLMConfig(ctx)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, fmt.Errorf("load default llm config: %w", err)
	}
	apiKey := ""
	if config.APIKeyRef != nil && *config.APIKeyRef != "" && s.secrets != nil {
		decrypted, err := s.secrets.Decrypt(*config.APIKeyRef)
		if err != nil {
			return nil, "", false, fmt.Errorf("decrypt api key: %w", err)
		}
		apiKey = decrypted
	}
	return config, apiKey, s.client != nil, nil
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

func (s *Service) rewriteQuery(ctx context.Context, question string, config *model.LLMConfig, apiKey string, ready bool) string {
	fallback := ruleBasedRewrite(question)
	if !ready || config == nil {
		return fallback
	}
	result, err := s.client.Chat(ctx, llmsvc.ChatRequest{
		BaseURL:     config.BaseURL,
		APIKey:      apiKey,
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

func (s *Service) answer(ctx context.Context, question, rewritten string, chunks []model.KBChunk, citations []Citation, config *model.LLMConfig, apiKey string, ready bool) (string, error) {
	if len(chunks) == 0 {
		return noEvidenceAnswer, nil
	}
	if !ready || config == nil {
		return localAnswer(chunks), nil
	}
	result, err := s.client.Chat(ctx, llmsvc.ChatRequest{
		BaseURL:     config.BaseURL,
		APIKey:      apiKey,
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

func rerankChunks(question, rewritten string, chunks []model.KBChunk, limit int) []model.KBChunk {
	terms := tokenize(question + " " + rewritten)
	sort.SliceStable(chunks, func(i, j int) bool {
		return chunkScore(chunks[i], terms) > chunkScore(chunks[j], terms)
	})
	if len(chunks) > limit {
		return chunks[:limit]
	}
	return chunks
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
