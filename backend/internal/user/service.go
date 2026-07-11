package user

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
	maxDisplayBytes  = 120
)

var (
	ErrInvalidInput   = errors.New("invalid input")
	ErrPasswordPolicy = errors.New("password does not satisfy policy")
	ErrUsernameTaken  = errors.New("username already exists")
	ErrLastAdmin      = repository.ErrLastAdmin
)

type Repository interface {
	FindByID(ctx context.Context, id int64) (*model.AppUser, error)
	FindByUsername(ctx context.Context, username string) (*model.AppUser, error)
	CreateUser(ctx context.Context, user *model.AppUser) error
	ListUsers(ctx context.Context) ([]model.AppUser, error)
	UpdateUser(ctx context.Context, userID int64, updates repository.UserUpdates) (*model.AppUser, error)
	SetUserEnabled(ctx context.Context, userID int64, enabled bool) (*model.AppUser, error)
	UpdatePassword(ctx context.Context, userID int64, passwordHash string, changedAt time.Time) error
}

type Service struct {
	users      Repository
	bcryptCost int
	now        func() time.Time
}

type CreateInput struct {
	Username    string
	Password    string
	DisplayName *string
	Role        string
	Enabled     bool
}

type UpdateInput struct {
	DisplayName    *string
	DisplayNameSet bool
	Role           *string
}

func NewService(users Repository, bcryptCost int) (*Service, error) {
	if bcryptCost < bcrypt.MinCost || bcryptCost > bcrypt.MaxCost {
		return nil, fmt.Errorf("invalid bcrypt cost")
	}
	return &Service{users: users, bcryptCost: bcryptCost, now: time.Now}, nil
}

func (s *Service) List(ctx context.Context) ([]model.AppUser, error) {
	return s.users.ListUsers(ctx)
}

func (s *Service) Create(ctx context.Context, input CreateInput) (*model.AppUser, error) {
	username := strings.TrimSpace(input.Username)
	if err := validateUsername(username); err != nil {
		return nil, err
	}
	if err := validatePassword(input.Password); err != nil {
		return nil, err
	}
	role := input.Role
	if role == "" {
		role = model.RoleUser
	}
	if err := validateRole(role); err != nil {
		return nil, err
	}
	displayName, err := normalizeDisplayName(input.DisplayName)
	if err != nil {
		return nil, err
	}
	existing, err := s.users.FindByUsername(ctx, username)
	if err == nil && existing != nil {
		return nil, ErrUsernameTaken
	}
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("find user by username: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), s.bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	now := s.now().UTC()
	created := &model.AppUser{
		Username:          username,
		PasswordHash:      string(hash),
		DisplayName:       displayName,
		Role:              role,
		Enabled:           input.Enabled,
		PasswordChangedAt: &now,
	}
	if err := s.users.CreateUser(ctx, created); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return created, nil
}

func (s *Service) Update(ctx context.Context, userID int64, input UpdateInput) (*model.AppUser, error) {
	if userID <= 0 {
		return nil, ErrInvalidInput
	}
	updates := repository.UserUpdates{DisplayNameSet: input.DisplayNameSet}
	if input.DisplayNameSet {
		displayName, err := normalizeDisplayName(input.DisplayName)
		if err != nil {
			return nil, err
		}
		updates.DisplayName = displayName
	}
	if input.Role != nil {
		role := strings.TrimSpace(*input.Role)
		if err := validateRole(role); err != nil {
			return nil, err
		}
		updates.Role = &role
	}
	return s.users.UpdateUser(ctx, userID, updates)
}

func (s *Service) ResetPassword(ctx context.Context, userID int64, password string) error {
	if userID <= 0 {
		return ErrInvalidInput
	}
	if err := validatePassword(password); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.bcryptCost)
	if err != nil {
		return fmt.Errorf("hash reset password: %w", err)
	}
	if err := s.users.UpdatePassword(ctx, userID, string(hash), s.now().UTC()); err != nil {
		return fmt.Errorf("reset password: %w", err)
	}
	return nil
}

func (s *Service) SetEnabled(ctx context.Context, userID int64, enabled bool) (*model.AppUser, error) {
	if userID <= 0 {
		return nil, ErrInvalidInput
	}
	return s.users.SetUserEnabled(ctx, userID, enabled)
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

func validateRole(role string) error {
	if role != model.RoleAdmin && role != model.RoleUser {
		return ErrInvalidInput
	}
	return nil
}

func normalizeDisplayName(displayName *string) (*string, error) {
	if displayName == nil {
		return nil, nil
	}
	normalized := strings.TrimSpace(*displayName)
	if normalized == "" {
		return nil, nil
	}
	if len(normalized) > maxDisplayBytes || !utf8.ValidString(normalized) {
		return nil, ErrInvalidInput
	}
	return &normalized, nil
}
