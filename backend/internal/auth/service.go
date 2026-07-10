package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

const (
	minPasswordBytes = 12
	maxPasswordBytes = 72
	maxUsernameBytes = 100
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidInput       = errors.New("invalid input")
	ErrUserDisabled       = errors.New("user disabled")
	ErrCurrentPassword    = errors.New("current password is incorrect")
	ErrPasswordPolicy     = errors.New("password does not satisfy policy")
)

type LoginMetadata struct {
	ClientIP  string
	UserAgent string
}

type LoginResult struct {
	User      *model.AppUser
	Token     string
	ExpiresAt time.Time
}

type Service struct {
	users      repository.UserRepository
	tokens     *TokenManager
	bcryptCost int
	dummyHash  []byte
	now        func() time.Time
}

func NewService(users repository.UserRepository, tokens *TokenManager, bcryptCost int) (*Service, error) {
	if bcryptCost < bcrypt.MinCost || bcryptCost > bcrypt.MaxCost {
		return nil, fmt.Errorf("invalid bcrypt cost")
	}
	dummyHash, err := bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("create timing hash: %w", err)
	}
	return &Service{users: users, tokens: tokens, bcryptCost: bcryptCost, dummyHash: dummyHash, now: time.Now}, nil
}

func (s *Service) BootstrapAdmin(ctx context.Context, username, password string) error {
	username = strings.TrimSpace(username)
	if err := validateUsername(username); err != nil {
		return err
	}
	if err := validatePassword(password); err != nil {
		return err
	}

	existing, err := s.users.FindByUsername(ctx, username)
	if err == nil {
		if existing.Role != model.RoleAdmin {
			return fmt.Errorf("initial admin username already belongs to a non-admin user")
		}
		return nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return fmt.Errorf("find initial admin: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.bcryptCost)
	if err != nil {
		return fmt.Errorf("hash initial admin password: %w", err)
	}
	now := s.now().UTC()
	displayName := "Platform Admin"
	user := &model.AppUser{
		Username:          username,
		PasswordHash:      string(hash),
		DisplayName:       &displayName,
		Role:              model.RoleAdmin,
		Enabled:           true,
		PasswordChangedAt: &now,
	}
	if err := s.users.CreateUser(ctx, user); err != nil {
		return fmt.Errorf("create initial admin: %w", err)
	}
	return nil
}

func (s *Service) Login(ctx context.Context, username, password string, metadata LoginMetadata) (*LoginResult, error) {
	username = strings.TrimSpace(username)
	if validateUsername(username) != nil || password == "" || len(password) > maxPasswordBytes {
		if err := s.recordLogin(ctx, nil, username, false, "invalid_input", metadata, nil); err != nil {
			return nil, err
		}
		return nil, ErrInvalidInput
	}

	user, findErr := s.users.FindByUsername(ctx, username)
	if errors.Is(findErr, repository.ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		if err := s.recordLogin(ctx, nil, username, false, "user_not_found", metadata, nil); err != nil {
			return nil, err
		}
		return nil, ErrInvalidCredentials
	}
	if findErr != nil {
		return nil, fmt.Errorf("find user for login: %w", findErr)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		if auditErr := s.recordLogin(ctx, user, username, false, "invalid_password", metadata, nil); auditErr != nil {
			return nil, auditErr
		}
		return nil, ErrInvalidCredentials
	}
	if !user.Enabled {
		if err := s.recordLogin(ctx, user, username, false, "disabled", metadata, nil); err != nil {
			return nil, err
		}
		return nil, ErrUserDisabled
	}

	now := s.now().UTC()
	if err := s.recordLogin(ctx, user, username, true, "", metadata, &now); err != nil {
		return nil, err
	}
	issued, err := s.tokens.Issue(user)
	if err != nil {
		return nil, err
	}
	user.LastLoginAt = &now
	return &LoginResult{User: user, Token: issued.Value, ExpiresAt: issued.ExpiresAt}, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (*model.AppUser, error) {
	claims, err := s.tokens.Parse(token)
	if err != nil {
		return nil, ErrInvalidToken
	}
	user, err := s.users.FindByID(ctx, claims.UserID)
	if err != nil || user.Username != claims.Username || user.Role != claims.Role || !user.Enabled {
		return nil, ErrInvalidToken
	}
	if user.PasswordChangedAt != nil &&
		time.Unix(0, claims.IssuedAtUnixNano).Before(user.PasswordChangedAt.UTC()) {
		return nil, ErrInvalidToken
	}
	return user, nil
}

func (s *Service) ChangePassword(ctx context.Context, userID int64, currentPassword, newPassword string) error {
	if err := validatePassword(newPassword); err != nil {
		return ErrPasswordPolicy
	}
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("find user for password change: %w", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)); err != nil {
		return ErrCurrentPassword
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(newPassword)) == nil {
		return ErrPasswordPolicy
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), s.bcryptCost)
	if err != nil {
		return fmt.Errorf("hash new password: %w", err)
	}
	if err := s.users.UpdatePassword(ctx, userID, string(hash), s.now().UTC()); err != nil {
		return fmt.Errorf("persist new password: %w", err)
	}
	return nil
}

func (s *Service) recordLogin(
	ctx context.Context,
	user *model.AppUser,
	username string,
	success bool,
	failureReason string,
	metadata LoginMetadata,
	lastLoginAt *time.Time,
) error {
	var userID *int64
	if user != nil {
		id := user.ID
		userID = &id
	}
	var reason *string
	if failureReason != "" {
		reason = &failureReason
	}
	audit := &model.LoginAudit{
		UserID:        userID,
		Username:      truncate(username, 100),
		Success:       success,
		ClientIP:      truncate(metadata.ClientIP, 100),
		UserAgent:     truncate(metadata.UserAgent, 1024),
		FailureReason: reason,
	}
	if err := s.users.RecordLogin(ctx, audit, lastLoginAt); err != nil {
		return fmt.Errorf("record login audit: %w", err)
	}
	return nil
}

func validateUsername(username string) error {
	if username == "" || len(username) > maxUsernameBytes || !utf8.ValidString(username) {
		return ErrInvalidInput
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < minPasswordBytes || len(password) > maxPasswordBytes || !utf8.ValidString(password) {
		return ErrPasswordPolicy
	}
	return nil
}

func truncate(value string, max int) string {
	value = strings.ToValidUTF8(value, "�")
	if len(value) <= max {
		return value
	}
	value = value[:max]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
