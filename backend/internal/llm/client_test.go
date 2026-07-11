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
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") == "Bearer test-key" {
			sawAuthorization = true
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
	if result.Content != "pong" || result.Model != "mock-model" || result.Usage.TotalTokens != 2 {
		t.Fatalf("result = %+v", result)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
