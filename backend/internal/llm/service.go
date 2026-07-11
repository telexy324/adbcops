package llm

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

const (
	maxNameBytes  = 120
	maxModelBytes = 120
)

var (
	ErrInvalidInput  = errors.New("invalid input")
	ErrMissingAPIKey = errors.New("api key is required")
)

type SecretManager interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(value string) (string, error)
}

type Repository interface {
	ListLLMConfigs(ctx context.Context) ([]model.LLMConfig, error)
	FindLLMConfigByID(ctx context.Context, id int64) (*model.LLMConfig, error)
	CreateLLMConfig(ctx context.Context, config *model.LLMConfig) error
	UpdateLLMConfig(ctx context.Context, id int64, updates repository.LLMConfigUpdates) (*model.LLMConfig, error)
	DeleteLLMConfig(ctx context.Context, id int64) error
	SetDefaultLLMConfig(ctx context.Context, id int64) (*model.LLMConfig, error)
}

type Service struct {
	configs Repository
	secrets SecretManager
	client  Client
}

type SaveInput struct {
	Name        string
	Provider    string
	BaseURL     string
	Model       string
	APIKey      *string
	Temperature float64
	Enabled     bool
	IsDefault   bool
	CreatedBy   *int64
}

type UpdateInput struct {
	Name        *string
	Provider    *string
	BaseURL     *string
	Model       *string
	APIKey      *string
	Temperature *float64
	Enabled     *bool
	IsDefault   *bool
}

type TestResult struct {
	OK      bool   `json:"ok"`
	Model   string `json:"model"`
	Content string `json:"content"`
	Usage   Usage  `json:"usage"`
}

func NewService(configs Repository, secrets SecretManager, client Client) *Service {
	return &Service{configs: configs, secrets: secrets, client: client}
}

func (s *Service) List(ctx context.Context) ([]model.LLMConfig, error) {
	return s.configs.ListLLMConfigs(ctx)
}

func (s *Service) Get(ctx context.Context, id int64) (*model.LLMConfig, error) {
	if id <= 0 {
		return nil, ErrInvalidInput
	}
	return s.configs.FindLLMConfigByID(ctx, id)
}

func (s *Service) Create(ctx context.Context, input SaveInput) (*model.LLMConfig, error) {
	normalized, err := normalizeSaveInput(input)
	if err != nil {
		return nil, err
	}
	var apiKeyRef *string
	if input.APIKey != nil && strings.TrimSpace(*input.APIKey) != "" {
		encrypted, err := s.secrets.Encrypt(strings.TrimSpace(*input.APIKey))
		if err != nil {
			return nil, fmt.Errorf("encrypt api key: %w", err)
		}
		apiKeyRef = &encrypted
	}
	config := &model.LLMConfig{
		Name:        normalized.Name,
		Provider:    normalized.Provider,
		BaseURL:     normalized.BaseURL,
		Model:       normalized.Model,
		APIKeyRef:   apiKeyRef,
		Temperature: normalized.Temperature,
		Enabled:     normalized.Enabled,
		IsDefault:   normalized.IsDefault,
		CreatedBy:   normalized.CreatedBy,
	}
	if err := s.configs.CreateLLMConfig(ctx, config); err != nil {
		return nil, fmt.Errorf("create llm config: %w", err)
	}
	return config, nil
}

func (s *Service) Update(ctx context.Context, id int64, input UpdateInput) (*model.LLMConfig, error) {
	if id <= 0 {
		return nil, ErrInvalidInput
	}
	updates := repository.LLMConfigUpdates{}
	if input.Name != nil {
		name, err := normalizeSizedString(*input.Name, maxNameBytes)
		if err != nil {
			return nil, err
		}
		updates.Name = &name
	}
	if input.Provider != nil {
		provider := strings.TrimSpace(*input.Provider)
		if err := validateProvider(provider); err != nil {
			return nil, err
		}
		updates.Provider = &provider
	}
	if input.BaseURL != nil {
		baseURL, err := normalizeBaseURL(*input.BaseURL)
		if err != nil {
			return nil, err
		}
		updates.BaseURL = &baseURL
	}
	if input.Model != nil {
		modelName, err := normalizeSizedString(*input.Model, maxModelBytes)
		if err != nil {
			return nil, err
		}
		updates.Model = &modelName
	}
	if input.APIKey != nil {
		updates.APIKeyRefSet = true
		apiKey := strings.TrimSpace(*input.APIKey)
		if apiKey != "" {
			encrypted, err := s.secrets.Encrypt(apiKey)
			if err != nil {
				return nil, fmt.Errorf("encrypt api key: %w", err)
			}
			updates.APIKeyRef = &encrypted
		}
	}
	updates.Temperature = input.Temperature
	updates.Enabled = input.Enabled
	updates.IsDefault = input.IsDefault
	return s.configs.UpdateLLMConfig(ctx, id, updates)
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrInvalidInput
	}
	return s.configs.DeleteLLMConfig(ctx, id)
}

func (s *Service) SetDefault(ctx context.Context, id int64) (*model.LLMConfig, error) {
	if id <= 0 {
		return nil, ErrInvalidInput
	}
	return s.configs.SetDefaultLLMConfig(ctx, id)
}

func (s *Service) Test(ctx context.Context, id int64, prompt string) (*TestResult, error) {
	config, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !config.Enabled {
		return nil, ErrInvalidInput
	}
	apiKey := ""
	if config.APIKeyRef != nil && *config.APIKeyRef != "" {
		apiKey, err = s.secrets.Decrypt(*config.APIKeyRef)
		if err != nil {
			return nil, fmt.Errorf("decrypt api key: %w", err)
		}
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = model.DefaultLLMTestPrompt
	}
	result, err := s.client.Chat(ctx, ChatRequest{
		BaseURL:     config.BaseURL,
		APIKey:      apiKey,
		Model:       config.Model,
		Temperature: config.Temperature,
		Messages:    []ChatMessage{{Role: model.MessageRoleUser, Content: prompt}},
	})
	if err != nil {
		return nil, err
	}
	return &TestResult{OK: true, Model: result.Model, Content: result.Content, Usage: result.Usage}, nil
}

func normalizeSaveInput(input SaveInput) (SaveInput, error) {
	name, err := normalizeSizedString(input.Name, maxNameBytes)
	if err != nil {
		return SaveInput{}, err
	}
	provider := strings.TrimSpace(input.Provider)
	if err := validateProvider(provider); err != nil {
		return SaveInput{}, err
	}
	baseURL, err := normalizeBaseURL(input.BaseURL)
	if err != nil {
		return SaveInput{}, err
	}
	modelName, err := normalizeSizedString(input.Model, maxModelBytes)
	if err != nil {
		return SaveInput{}, err
	}
	if input.Temperature < 0 || input.Temperature > 2 {
		return SaveInput{}, ErrInvalidInput
	}
	input.Name = name
	input.Provider = provider
	input.BaseURL = baseURL
	input.Model = modelName
	return input, nil
}

func normalizeSizedString(value string, maxBytes int) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" || len(normalized) > maxBytes || !utf8.ValidString(normalized) {
		return "", ErrInvalidInput
	}
	return normalized, nil
}

func normalizeBaseURL(value string) (string, error) {
	normalized := strings.TrimRight(strings.TrimSpace(value), "/")
	parsed, err := url.Parse(normalized)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", ErrInvalidInput
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", ErrInvalidInput
	}
	return normalized, nil
}

func validateProvider(provider string) error {
	switch provider {
	case model.ProviderDeepSeek, model.ProviderQwen, model.ProviderOpenAICompatible:
		return nil
	default:
		return ErrInvalidInput
	}
}
