package auth

import (
	"errors"
	"testing"
	"time"

	"aiops-platform/backend/internal/model"
)

func TestTokenManagerIssueAndParse(t *testing.T) {
	manager, err := NewTokenManager("test-jwt-secret-with-at-least-32-characters", time.Hour)
	if err != nil {
		t.Fatalf("NewTokenManager() error = %v", err)
	}
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	user := &model.AppUser{ID: 42, Username: "admin", Role: model.RoleAdmin}

	issued, err := manager.Issue(user)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	claims, err := manager.Parse(issued.Value)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if claims.UserID != user.ID || claims.Username != user.Username || claims.Role != user.Role {
		t.Fatalf("claims = %+v", claims)
	}

	manager.now = func() time.Time { return now.Add(2 * time.Hour) }
	if _, err := manager.Parse(issued.Value); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expired Parse() error = %v, want ErrInvalidToken", err)
	}
}

func TestTokenManagerRejectsUnexpectedSigningMethod(t *testing.T) {
	manager, err := NewTokenManager("test-jwt-secret-with-at-least-32-characters", time.Hour)
	if err != nil {
		t.Fatalf("NewTokenManager() error = %v", err)
	}
	if _, err := manager.Parse("not-a-token"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Parse() error = %v, want ErrInvalidToken", err)
	}
}
