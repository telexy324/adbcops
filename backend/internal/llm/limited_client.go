package llm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"aiops-platform/backend/internal/observability"
	"aiops-platform/backend/internal/resourcelimit"
)

var ErrLLMLimited = errors.New("llm concurrency limit exceeded")

type LimitedClient struct {
	next    Client
	limiter *resourcelimit.Limiter
}

func NewLimitedClient(next Client, limit int) *LimitedClient {
	return &LimitedClient{next: next, limiter: resourcelimit.NewLimiter(limit)}
}

func (c *LimitedClient) Chat(ctx context.Context, req ChatRequest) (*ChatResult, error) {
	startedAt := time.Now()
	if c == nil || c.next == nil {
		err := fmt.Errorf("llm client is not configured")
		observability.ObserveLLM(req.Model, 0, 0, 0, err, time.Since(startedAt))
		return nil, err
	}
	release, err := c.limiter.Acquire(ctx)
	if err != nil {
		if errors.Is(err, resourcelimit.ErrLimitExceeded) {
			observability.ObserveLLM(req.Model, 0, 0, 0, ErrLLMLimited, time.Since(startedAt))
			return nil, ErrLLMLimited
		}
		observability.ObserveLLM(req.Model, 0, 0, 0, err, time.Since(startedAt))
		return nil, err
	}
	defer release()
	result, err := c.next.Chat(ctx, req)
	usage := Usage{}
	model := req.Model
	if result != nil {
		usage = result.Usage
		if result.Model != "" {
			model = result.Model
		}
	}
	observability.ObserveLLM(model, usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens, err, time.Since(startedAt))
	return result, err
}
