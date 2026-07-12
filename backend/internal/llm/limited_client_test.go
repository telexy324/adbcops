package llm

import (
	"context"
	"errors"
	"strings"
	"testing"

	"aiops-platform/backend/internal/observability"
)

func TestLimitedClientRejectsWhenConcurrentLimitReached(t *testing.T) {
	block := make(chan struct{})
	started := make(chan struct{})
	client := NewLimitedClient(blockingClient{started: started, block: block}, 1)
	done := make(chan error, 1)
	go func() {
		_, err := client.Chat(context.Background(), ChatRequest{Model: "test"})
		done <- err
	}()
	<-started

	_, err := client.Chat(context.Background(), ChatRequest{Model: "test"})
	if !errors.Is(err, ErrLLMLimited) {
		t.Fatalf("expected ErrLLMLimited, got %v", err)
	}
	close(block)
	if err := <-done; err != nil {
		t.Fatalf("first chat should complete: %v", err)
	}
	metrics := string(observability.Default.WritePrometheus())
	if !strings.Contains(metrics, "aiops_llm_requests_total") || !strings.Contains(metrics, `type="total"`) {
		t.Fatalf("llm metrics missing usage:\n%s", metrics)
	}
}

type blockingClient struct {
	started chan struct{}
	block   chan struct{}
}

func (c blockingClient) Chat(ctx context.Context, _ ChatRequest) (*ChatResult, error) {
	close(c.started)
	select {
	case <-c.block:
		return &ChatResult{Content: "ok", Model: "test", Usage: Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
