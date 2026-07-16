package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultHTTPTimeout = 180 * time.Second

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type Usage struct {
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
	TotalTokens      int `json:"totalTokens"`
}

type ChatRequest struct {
	BaseURL     string
	Provider    string
	APIKey      string
	AppKey      string
	APISecret   string
	Model       string
	Temperature float64
	Messages    []ChatMessage
}

type EmbeddingRequest struct {
	BaseURL   string
	APIKey    string
	APISecret string
	Model     string
	Input     []string
}

type EmbeddingResult struct {
	Model      string
	Embedding  []float64
	Embeddings [][]float64
	Usage      Usage
}

type RerankRequest struct {
	BaseURL   string
	APIKey    string
	APISecret string
	Model     string
	Query     string
	Documents []string
	TopN      int
}

type RerankResult struct {
	Model   string
	Results []RerankItem
	Usage   Usage
}

type RerankItem struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevanceScore"`
}

type ChatResult struct {
	Content   string
	Model     string
	ToolCalls []ToolCall
	Usage     Usage
}

type Client interface {
	Chat(ctx context.Context, req ChatRequest) (*ChatResult, error)
}

type EmbeddingClient interface {
	Embed(ctx context.Context, req EmbeddingRequest) (*EmbeddingResult, error)
}

type RerankClient interface {
	Rerank(ctx context.Context, req RerankRequest) (*RerankResult, error)
}

type OpenAICompatibleClient struct {
	httpClient *http.Client
}

func NewOpenAICompatibleClient(httpClient *http.Client) *OpenAICompatibleClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &OpenAICompatibleClient{httpClient: httpClient}
}

func (c *OpenAICompatibleClient) Chat(ctx context.Context, req ChatRequest) (*ChatResult, error) {
	endpoint := modelEndpoint(req.BaseURL, "/v1/chat/completions")
	payload := map[string]any{
		"model":       req.Model,
		"stream":      false,
		"temperature": req.Temperature,
		"messages":    req.Messages,
	}
	if req.Provider == "qwen" {
		if req.AppKey != "" {
			payload["app_key"] = req.AppKey
		}
		if req.APISecret != "" {
			payload["app_secret"] = req.APISecret
		}
		payload["chat_template_kwargs"] = map[string]any{"enable_thinking": false}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode chat request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create chat request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if req.APIKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+req.APIKey)
	}
	if req.APISecret != "" && req.Provider != "qwen" {
		httpRequest.Header.Set("X-API-Secret", req.APISecret)
	}
	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("send chat request: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read chat response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, responseStatusError("llm", response.StatusCode, responseBody, req.APIKey, req.AppKey, req.APISecret)
	}
	var decoded struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode chat response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return nil, fmt.Errorf("llm returned no choices")
	}
	toolCalls := make([]ToolCall, 0, len(decoded.Choices[0].Message.ToolCalls))
	for _, call := range decoded.Choices[0].Message.ToolCalls {
		toolCalls = append(toolCalls, ToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
	}
	return &ChatResult{
		Content:   decoded.Choices[0].Message.Content,
		Model:     decoded.Model,
		ToolCalls: toolCalls,
		Usage: Usage{
			PromptTokens:     decoded.Usage.PromptTokens,
			CompletionTokens: decoded.Usage.CompletionTokens,
			TotalTokens:      decoded.Usage.TotalTokens,
		},
	}, nil
}

func (c *OpenAICompatibleClient) Embed(ctx context.Context, req EmbeddingRequest) (*EmbeddingResult, error) {
	endpoint := modelEndpoint(req.BaseURL, "/v1/embeddings")
	payload := map[string]any{
		"model": req.Model,
		"input": req.Input,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode embedding request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create embedding request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if req.APIKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+req.APIKey)
	}
	if req.APISecret != "" {
		httpRequest.Header.Set("X-API-Secret", req.APISecret)
	}
	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("send embedding request: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read embedding response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, responseStatusError("embedding model", response.StatusCode, responseBody, req.APIKey, req.APISecret)
	}
	var decoded struct {
		Model string `json:"model"`
		Data  []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if len(decoded.Data) == 0 {
		return nil, fmt.Errorf("embedding model returned no vectors")
	}
	embeddings := make([][]float64, len(decoded.Data))
	for _, item := range decoded.Data {
		index := item.Index
		if index < 0 || index >= len(embeddings) {
			index = 0
		}
		embeddings[index] = item.Embedding
	}
	return &EmbeddingResult{
		Model:      decoded.Model,
		Embedding:  embeddings[0],
		Embeddings: embeddings,
		Usage: Usage{
			PromptTokens: decoded.Usage.PromptTokens,
			TotalTokens:  decoded.Usage.TotalTokens,
		},
	}, nil
}

func (c *OpenAICompatibleClient) Rerank(ctx context.Context, req RerankRequest) (*RerankResult, error) {
	endpoint := modelEndpoint(req.BaseURL, "/v1/rerank")
	payload := map[string]any{
		"model":     req.Model,
		"query":     req.Query,
		"documents": req.Documents,
	}
	if req.TopN > 0 {
		payload["top_n"] = req.TopN
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode rerank request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create rerank request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if req.APIKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+req.APIKey)
	}
	if req.APISecret != "" {
		httpRequest.Header.Set("X-API-Secret", req.APISecret)
	}
	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("send rerank request: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("read rerank response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, responseStatusError("rerank model", response.StatusCode, responseBody, req.APIKey, req.APISecret)
	}
	var decoded struct {
		Model   string `json:"model"`
		Results []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
		} `json:"results"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode rerank response: %w", err)
	}
	items := make([]RerankItem, 0, len(decoded.Results))
	for _, item := range decoded.Results {
		items = append(items, RerankItem{Index: item.Index, RelevanceScore: item.RelevanceScore})
	}
	return &RerankResult{
		Model:   decoded.Model,
		Results: items,
		Usage: Usage{
			PromptTokens: decoded.Usage.PromptTokens,
			TotalTokens:  decoded.Usage.TotalTokens,
		},
	}, nil
}

func modelEndpoint(baseURL, suffix string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(base, suffix) {
		return base
	}
	if strings.HasSuffix(base, "/v1") && strings.HasPrefix(suffix, "/v1/") {
		return base + strings.TrimPrefix(suffix, "/v1")
	}
	return base + suffix
}

func responseStatusError(service string, status int, body []byte, secrets ...string) error {
	detail := strings.TrimSpace(string(body))
	for _, secret := range secrets {
		if secret != "" {
			detail = strings.ReplaceAll(detail, secret, "***")
		}
	}
	if len(detail) > 2048 {
		detail = detail[:2048] + "..."
	}
	if detail == "" {
		return fmt.Errorf("%s returned status %d", service, status)
	}
	return fmt.Errorf("%s returned status %d: %s", service, status, detail)
}
