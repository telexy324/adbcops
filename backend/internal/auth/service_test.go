package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

func TestBootstrapAdminCreatesOnceWithoutOverwritingPassword(t *testing.T) {
	store := newFakeUserRepository()
	service := newTestService(t, store)

	if err := service.BootstrapAdmin(context.Background(), "admin", "initial-password"); err != nil {
		t.Fatalf("BootstrapAdmin() error = %v", err)
	}
	created := store.usersByUsername["admin"]
	firstHash := created.PasswordHash
	if created.Role != model.RoleAdmin || !created.Enabled {
		t.Fatalf("created admin = %+v", created)
	}

	if err := service.BootstrapAdmin(context.Background(), "admin", "different-password"); err != nil {
		t.Fatalf("second BootstrapAdmin() error = %v", err)
	}
	if store.usersByUsername["admin"].PasswordHash != firstHash {
		t.Fatal("existing admin password was overwritten")
	}
}

func TestLoginAuditsSuccessFailureAndDisabledUser(t *testing.T) {
	store := newFakeUserRepository()
	service := newTestService(t, store)
	if err := service.BootstrapAdmin(context.Background(), "admin", "initial-password"); err != nil {
		t.Fatalf("BootstrapAdmin() error = %v", err)
	}

	result, err := service.Login(context.Background(), "admin", "initial-password", LoginMetadata{ClientIP: "127.0.0.1", UserAgent: "test"})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if result.Token == "" || len(store.audits) != 1 || !store.audits[0].Success {
		t.Fatalf("successful login result or audit is invalid")
	}

	if _, err := service.Login(context.Background(), "admin", "wrong-password", LoginMetadata{}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong-password Login() error = %v", err)
	}
	if store.audits[1].FailureReason == nil || *store.audits[1].FailureReason != "invalid_password" {
		t.Fatalf("failed login audit = %+v", store.audits[1])
	}

	store.usersByUsername["admin"].Enabled = false
	if _, err := service.Login(context.Background(), "admin", "initial-password", LoginMetadata{}); !errors.Is(err, ErrUserDisabled) {
		t.Fatalf("disabled Login() error = %v", err)
	}
	if store.audits[2].FailureReason == nil || *store.audits[2].FailureReason != "disabled" {
		t.Fatalf("disabled login audit = %+v", store.audits[2])
	}

	if _, err := service.Login(context.Background(), "", "", LoginMetadata{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid-input Login() error = %v", err)
	}
	if store.audits[3].FailureReason == nil || *store.audits[3].FailureReason != "invalid_input" {
		t.Fatalf("invalid input login audit = %+v", store.audits[3])
	}
}

func TestChangePassword(t *testing.T) {
	store := newFakeUserRepository()
	service := newTestService(t, store)
	if err := service.BootstrapAdmin(context.Background(), "admin", "initial-password"); err != nil {
		t.Fatalf("BootstrapAdmin() error = %v", err)
	}
	user := store.usersByUsername["admin"]

	if err := service.ChangePassword(context.Background(), user.ID, "wrong-password", "new-secure-password"); !errors.Is(err, ErrCurrentPassword) {
		t.Fatalf("wrong current password error = %v", err)
	}
	if err := service.ChangePassword(context.Background(), user.ID, "initial-password", "initial-password"); !errors.Is(err, ErrPasswordPolicy) {
		t.Fatalf("reused password error = %v", err)
	}
	if err := service.ChangePassword(context.Background(), user.ID, "initial-password", "new-secure-password"); err != nil {
		t.Fatalf("ChangePassword() error = %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("new-secure-password")) != nil {
		t.Fatal("new password was not persisted")
	}
}

func TestPasswordChangeInvalidatesEarlierToken(t *testing.T) {
	store := newFakeUserRepository()
	service := newTestService(t, store)
	initialTime := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return initialTime }
	service.tokens.now = func() time.Time { return initialTime }
	if err := service.BootstrapAdmin(context.Background(), "admin", "initial-password"); err != nil {
		t.Fatalf("BootstrapAdmin() error = %v", err)
	}
	result, err := service.Login(context.Background(), "admin", "initial-password", LoginMetadata{})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if _, err := service.Authenticate(context.Background(), result.Token); err != nil {
		t.Fatalf("Authenticate() before password change error = %v", err)
	}

	service.now = func() time.Time { return initialTime.Add(time.Minute) }
	if err := service.ChangePassword(context.Background(), result.User.ID, "initial-password", "new-secure-password"); err != nil {
		t.Fatalf("ChangePassword() error = %v", err)
	}
	if _, err := service.Authenticate(context.Background(), result.Token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Authenticate() after password change error = %v, want ErrInvalidToken", err)
	}
}

func newTestService(t *testing.T, store *fakeUserRepository) *Service {
	t.Helper()
	tokens, err := NewTokenManager("test-jwt-secret-with-at-least-32-characters", time.Hour)
	if err != nil {
		t.Fatalf("NewTokenManager() error = %v", err)
	}
	service, err := NewService(store, tokens, bcrypt.MinCost)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

type fakeUserRepository struct {
	nextID          int64
	usersByID       map[int64]*model.AppUser
	usersByUsername map[string]*model.AppUser
	audits          []*model.LoginAudit
}

func newFakeUserRepository() *fakeUserRepository {
	return &fakeUserRepository{
		nextID:          1,
		usersByID:       make(map[int64]*model.AppUser),
		usersByUsername: make(map[string]*model.AppUser),
	}
}

func (f *fakeUserRepository) FindByID(_ context.Context, id int64) (*model.AppUser, error) {
	user, ok := f.usersByID[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return user, nil
}

func (f *fakeUserRepository) FindByUsername(_ context.Context, username string) (*model.AppUser, error) {
	user, ok := f.usersByUsername[username]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return user, nil
}

func (f *fakeUserRepository) CreateUser(_ context.Context, user *model.AppUser) error {
	user.ID = f.nextID
	f.nextID++
	f.usersByID[user.ID] = user
	f.usersByUsername[user.Username] = user
	return nil
}

func (f *fakeUserRepository) RecordLogin(_ context.Context, audit *model.LoginAudit, lastLoginAt *time.Time) error {
	f.audits = append(f.audits, audit)
	if audit.UserID != nil && lastLoginAt != nil {
		f.usersByID[*audit.UserID].LastLoginAt = lastLoginAt
	}
	return nil
}

func (f *fakeUserRepository) UpdatePassword(_ context.Context, userID int64, passwordHash string, changedAt time.Time) error {
	user, ok := f.usersByID[userID]
	if !ok {
		return repository.ErrNotFound
	}
	user.PasswordHash = passwordHash
	user.PasswordChangedAt = &changedAt
	return nil
}
