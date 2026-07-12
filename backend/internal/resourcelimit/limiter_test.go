package resourcelimit

import (
	"context"
	"errors"
	"testing"
)

func TestKeyedLimiterRejectsSameKeyButAllowsDifferentKeys(t *testing.T) {
	limiter := NewKeyedLimiter(1)
	release, err := limiter.Acquire(context.Background(), "user:1")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer release()

	if _, err := limiter.Acquire(context.Background(), "user:1"); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("expected ErrLimitExceeded, got %v", err)
	}
	otherRelease, err := limiter.Acquire(context.Background(), "user:2")
	if err != nil {
		t.Fatalf("different key should be allowed: %v", err)
	}
	otherRelease()
}

func TestLimiterRejectsWhenFull(t *testing.T) {
	limiter := NewLimiter(1)
	release, err := limiter.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer release()

	if _, err := limiter.Acquire(context.Background()); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("expected ErrLimitExceeded, got %v", err)
	}
}
