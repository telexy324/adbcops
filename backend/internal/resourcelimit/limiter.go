package resourcelimit

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var ErrLimitExceeded = errors.New("resource limit exceeded")

type KeyedLimiter struct {
	mu       sync.Mutex
	limit    int
	inflight map[string]int
}

func NewKeyedLimiter(limit int) *KeyedLimiter {
	if limit <= 0 {
		limit = 1
	}
	return &KeyedLimiter{limit: limit, inflight: map[string]int{}}
}

func (l *KeyedLimiter) Acquire(ctx context.Context, key string) (func(), error) {
	if l == nil {
		return func() {}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if key == "" {
		key = "default"
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inflight[key] >= l.limit {
		return nil, fmt.Errorf("%w: %s", ErrLimitExceeded, key)
	}
	l.inflight[key]++
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.inflight[key]--
		if l.inflight[key] <= 0 {
			delete(l.inflight, key)
		}
	}, nil
}

type Limiter struct {
	ch chan struct{}
}

func NewLimiter(limit int) *Limiter {
	if limit <= 0 {
		limit = 1
	}
	return &Limiter{ch: make(chan struct{}, limit)}
}

func (l *Limiter) Acquire(ctx context.Context) (func(), error) {
	if l == nil {
		return func() {}, nil
	}
	select {
	case l.ch <- struct{}{}:
		return func() { <-l.ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, ErrLimitExceeded
	}
}
