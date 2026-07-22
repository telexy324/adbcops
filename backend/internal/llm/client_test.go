package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	appmiddleware "aiops-platform/backend/internal/middleware"
)

func TestOpenAICompatibleClientChat(t *testing.T) {
	var sawAuthorization bool
	var sawAPISecret bool
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") == "Bearer test-key" {
			sawAuthorization = true
		}
		if r.Header.Get("X-API-Secret") == "test-secret" {
			sawAPISecret = true
		}
		var request struct {
			Model    string        `json:"model"`
			Messages []ChatMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.Model != "mock-model" || len(request.Messages) != 1 || request.Messages[0].Content != "ping" {
			t.Fatalf("request = %+v", request)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"model":"mock-model","choices":[{"message":{"content":"pong"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)),
		}, nil
	})

	client := NewOpenAICompatibleClient(&http.Client{Transport: transport})
	result, err := client.Chat(context.Background(), ChatRequest{
		BaseURL:     "https://llm.example",
		APIKey:      "test-key",
		APISecret:   "test-secret",
		Model:       "mock-model",
		Temperature: 0.2,
		Messages:    []ChatMessage{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if !sawAuthorization {
		t.Fatal("authorization header was not sent")
	}
	if !sawAPISecret {
		t.Fatal("api secret header was not sent")
	}
	if result.Content != "pong" || result.Model != "mock-model" || result.Usage.TotalTokens != 2 {
		t.Fatalf("result = %+v", result)
	}
}

func TestOpenAICompatibleClientQwenGatewayPayload(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/Qwen3-32B/v1/chat/completions" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer bearer-token" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.Header.Get("X-API-Secret"); got != "" {
			t.Fatalf("qwen X-API-Secret = %q, want empty", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload["app_key"] != "app-key" || payload["app_secret"] != "app-secret" || payload["stream"] != false {
			t.Fatalf("payload = %#v", payload)
		}
		kwargs, ok := payload["chat_template_kwargs"].(map[string]any)
		if !ok || kwargs["enable_thinking"] != false {
			t.Fatalf("chat_template_kwargs = %#v", payload["chat_template_kwargs"])
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"model":"Qwen3-32B","choices":[{"message":{"content":"ok"}}]}`)),
		}, nil
	})

	client := NewOpenAICompatibleClient(&http.Client{Transport: transport})
	_, err := client.Chat(context.Background(), ChatRequest{
		BaseURL:   "http://193.108.7.173:30601/Qwen3-32B/v1",
		Provider:  "qwen",
		APIKey:    "bearer-token",
		AppKey:    "app-key",
		APISecret: "app-secret",
		Model:     "Qwen3-32B",
		Messages:  []ChatMessage{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
}

func TestOpenAICompatibleClientGatewayEmbeddingPayloadAndResponse(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer bearer-token" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.Header.Get("X-API-Secret"); got != "" {
			t.Fatalf("X-API-Secret = %q, want empty", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload["app_key"] != "app-key" || payload["app_secret"] != "app-secret" {
			t.Fatalf("payload = %#v", payload)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"model":"xiaobu-embedding-v2","embedding":[0.1,0.2,0.3],"prompt_tokens":1,"total_tokens":1}`)),
		}, nil
	})

	client := NewOpenAICompatibleClient(&http.Client{Transport: transport})
	result, err := client.Embed(context.Background(), EmbeddingRequest{
		BaseURL: "http://embedding.example/v1", APIKey: "bearer-token", AppKey: "app-key",
		APISecret: "app-secret", Model: "xiaobu-embedding-v2", Input: []string{"中国农业发展银行"},
	})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(result.Embeddings) != 1 || len(result.Embedding) != 3 || result.Usage.TotalTokens != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestOpenAICompatibleClientEmbeddingUsesResponseOrderWithoutIndexes(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"model":"xiaobu-embeddings","data":[{"embedding":[0.1,0.2]},{"embedding":[0.3,0.4]}]}`,
			)),
		}, nil
	})
	client := NewOpenAICompatibleClient(&http.Client{Transport: transport})
	result, err := client.Embed(context.Background(), EmbeddingRequest{
		BaseURL: "https://embedding.example", Input: []string{"first", "second"},
	})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(result.Embeddings) != 2 || result.Embeddings[0][0] != 0.1 || result.Embeddings[1][0] != 0.3 {
		t.Fatalf("embeddings = %#v", result.Embeddings)
	}
}

func TestOpenAICompatibleClientGatewayRerankPayloadAndResponse(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/bge-reranker-large/v1/rerank" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload["app_key"] != "app-key" || payload["app_secret"] != "app-secret" || payload["top_k"] != float64(2) {
			t.Fatalf("payload = %#v", payload)
		}
		if _, exists := payload["top_n"]; exists {
			t.Fatalf("gateway payload contains top_n: %#v", payload)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"rerank-1","model":"bge-reranker-large","usage":{"total_tokens":86},"results":[{"index":0,"document":{"text":"中国农业发展银行"},"relevance_score":0.7583}]}`)),
		}, nil
	})

	client := NewOpenAICompatibleClient(&http.Client{Transport: transport})
	result, err := client.Rerank(context.Background(), RerankRequest{
		BaseURL: "http://rerank.example/bge-reranker-large/v1", AppKey: "app-key", APISecret: "app-secret",
		Model: "bge-reranker-large", Query: "农发行", Documents: []string{"中国农业发展银行", "农行"}, TopN: 2,
	})
	if err != nil {
		t.Fatalf("Rerank() error = %v", err)
	}
	if len(result.Results) != 1 || result.Results[0].Index != 0 || result.Usage.TotalTokens != 86 {
		t.Fatalf("result = %+v", result)
	}
}

func TestOpenAICompatibleClientPreservesStandardRerankPayload(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload["top_n"] != float64(1) {
			t.Fatalf("payload = %#v", payload)
		}
		for _, key := range []string{"top_k", "app_key", "app_secret"} {
			if _, exists := payload[key]; exists {
				t.Fatalf("standard payload contains %s: %#v", key, payload)
			}
		}
		if got := r.Header.Get("X-API-Secret"); got != "header-secret" {
			t.Fatalf("X-API-Secret = %q", got)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"results":[{"index":0,"relevance_score":1}]}`))}, nil
	})
	client := NewOpenAICompatibleClient(&http.Client{Transport: transport})
	if _, err := client.Rerank(context.Background(), RerankRequest{BaseURL: "https://rerank.example", APISecret: "header-secret", TopN: 1}); err != nil {
		t.Fatalf("Rerank() error = %v", err)
	}
}

func TestOpenAICompatibleClientRejectsHTTP200BusinessError(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"code":4001,"message":"参数非法，请重试","results":null}`))}, nil
	})
	client := NewOpenAICompatibleClient(&http.Client{Transport: transport})
	_, err := client.Rerank(context.Background(), RerankRequest{BaseURL: "https://rerank.example"})
	if err == nil || !strings.Contains(err.Error(), "business code 4001") || !strings.Contains(err.Error(), "参数非法") {
		t.Fatalf("error = %v", err)
	}
}

func TestOpenAICompatibleClientErrorIncludesRedactedResponse(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":"invalid app-secret"}`)),
		}, nil
	})
	client := NewOpenAICompatibleClient(&http.Client{Transport: transport})
	_, err := client.Chat(context.Background(), ChatRequest{BaseURL: "https://llm.example", APISecret: "app-secret"})
	if err == nil || !strings.Contains(err.Error(), "status 401") || !strings.Contains(err.Error(), `invalid ***`) || strings.Contains(err.Error(), "app-secret") {
		t.Fatalf("error = %v", err)
	}
}

func TestOpenAICompatibleClientLogsChatEmbeddingAndRerankBodies(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var response string
		switch r.URL.Path {
		case "/v1/chat/completions":
			response = `{"model":"chat-model","choices":[{"message":{"content":"chat reply"}}]}`
		case "/v1/embeddings":
			response = `{"model":"embedding-model","data":[{"index":0,"embedding":[0.1,0.2]}]}`
		case "/v1/rerank":
			response = `{"model":"rerank-model","results":[{"index":0,"relevance_score":0.9}]}`
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(response)),
		}, nil
	})
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	client := NewOpenAICompatibleClient(&http.Client{Transport: transport}).WithLogger(logger)
	ctx := appmiddleware.ContextWithRequestID(context.Background(), "req-llm-log")

	if _, err := client.Chat(ctx, ChatRequest{
		BaseURL: "https://llm.example", Provider: "qwen", APIKey: "bearer-secret",
		AppKey: "app-key-secret", APISecret: "app-secret", Model: "chat-model",
		Messages: []ChatMessage{{Role: "user", Content: "chat request"}},
	}); err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if _, err := client.Embed(ctx, EmbeddingRequest{
		BaseURL: "https://llm.example", APIKey: "bearer-secret", APISecret: "app-secret",
		Model: "embedding-model", Input: []string{"embedding request"},
	}); err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if _, err := client.Rerank(ctx, RerankRequest{
		BaseURL: "https://llm.example", APIKey: "bearer-secret", APISecret: "app-secret",
		Model: "rerank-model", Query: "rerank query", Documents: []string{"rerank document"}, TopN: 1,
	}); err != nil {
		t.Fatalf("Rerank() error = %v", err)
	}

	output := logs.String()
	for _, want := range []string{
		`"msg":"llm outbound request"`, `"msg":"llm outbound response"`,
		`"msg":"llm outbound request body"`, `"msg":"llm outbound response body"`,
		`"request_id":"req-llm-log"`, `"operation":"chat"`, `"operation":"embedding"`, `"operation":"rerank"`,
		"chat request", "chat reply", "embedding request", `\"embedding\":[0.1,0.2]`,
		"rerank query", "rerank document", `\"relevance_score\":0.9`, `"authorization":"Bearer ***"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("logs missing %q: %s", want, output)
		}
	}
	for _, secret := range []string{"bearer-secret", "app-key-secret", "app-secret"} {
		if strings.Contains(output, secret) {
			t.Fatalf("logs leaked %q: %s", secret, output)
		}
	}
}

func TestOpenAICompatibleClientInfoLogsOmitBodies(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"model":"chat-model","choices":[{"message":{"content":"private reply"}}]}`)),
		}, nil
	})
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	client := NewOpenAICompatibleClient(&http.Client{Transport: transport}).WithLogger(logger)
	if _, err := client.Chat(context.Background(), ChatRequest{
		BaseURL: "https://llm.example", Model: "chat-model",
		Messages: []ChatMessage{{Role: "user", Content: "private request"}},
	}); err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	output := logs.String()
	for _, want := range []string{`"msg":"llm outbound request"`, `"msg":"llm outbound response"`, `"model":"chat-model"`, `"status":200`} {
		if !strings.Contains(output, want) {
			t.Fatalf("info logs missing %q: %s", want, output)
		}
	}
	for _, unwanted := range []string{"private request", "private reply", "request_body", "response_body"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("info logs contain body data %q: %s", unwanted, output)
		}
	}
}

func TestLogEndpointRemovesCredentialsAndQuery(t *testing.T) {
	endpoint := logEndpoint("https://user:password@llm.example/v1/chat/completions?token=query-secret")
	if endpoint != "https://llm.example/v1/chat/completions" {
		t.Fatalf("logEndpoint() = %q", endpoint)
	}
}

func TestSanitizeLogBodyTruncatesOversizedPayload(t *testing.T) {
	result := sanitizeLogBody([]byte(strings.Repeat("x", maxLogBodyBytes+1)))
	if len(result) <= maxLogBodyBytes || !strings.HasSuffix(result, "...[truncated]") {
		t.Fatalf("sanitizeLogBody() did not mark truncation")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
