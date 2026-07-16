package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
