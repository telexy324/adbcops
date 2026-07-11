package llm

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

func TestCreateEncryptsAPIKeyAndDefaultIsUnique(t *testing.T) {
	store := newFakeRepository()
	secrets := &fakeSecrets{}
	service := NewService(store, secrets, &fakeClient{})

	first, err := service.Create(context.Background(), SaveInput{
		Name:        "primary",
		Provider:    model.ProviderOpenAICompatible,
		BaseURL:     "https://llm.example",
		Model:       "model-a",
		APIKey:      ptr("secret-key-a"),
		Temperature: 0.2,
		Enabled:     true,
		IsDefault:   true,
	})
	if err != nil {
		t.Fatalf("Create(first) error = %v", err)
	}
	second, err := service.Create(context.Background(), SaveInput{
		Name:        "secondary",
		Provider:    model.ProviderOpenAICompatible,
		BaseURL:     "https://llm2.example",
		Model:       "model-b",
		APIKey:      ptr("secret-key-b"),
		Temperature: 0.2,
		Enabled:     true,
		IsDefault:   true,
	})
	if err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}

	if store.configs[first.ID].IsDefault {
		t.Fatal("first config remained default after second default was created")
	}
	if !store.configs[second.ID].IsDefault {
		t.Fatal("second config is not default")
	}
	if first.APIKeyRef == nil || strings.Contains(*first.APIKeyRef, "secret-key-a") {
		t.Fatalf("api key ref leaked plaintext: %v", first.APIKeyRef)
	}
}

func TestServiceTestUsesDecryptedAPIKey(t *testing.T) {
	store := newFakeRepository()
	secrets := &fakeSecrets{}
	client := &fakeClient{}
	service := NewService(store, secrets, client)
	created, err := service.Create(context.Background(), SaveInput{
		Name:        "test",
		Provider:    model.ProviderOpenAICompatible,
		BaseURL:     "https://llm.example",
		Model:       "mock-model",
		APIKey:      ptr("secret-key"),
		Temperature: 0.2,
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	result, err := service.Test(context.Background(), created.ID, "ping")
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	if !result.OK || result.Content != "ok" {
		t.Fatalf("result = %+v", result)
	}
	if client.last.APIKey != "secret-key" || client.last.Messages[0].Content != "ping" {
		t.Fatalf("client request = %+v", client.last)
	}
}

type fakeRepository struct {
	nextID  int64
	configs map[int64]*model.LLMConfig
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{nextID: 1, configs: make(map[int64]*model.LLMConfig)}
}

func (f *fakeRepository) ListLLMConfigs(_ context.Context) ([]model.LLMConfig, error) {
	configs := make([]model.LLMConfig, 0, len(f.configs))
	for _, config := range f.configs {
		configs = append(configs, *config)
	}
	return configs, nil
}

func (f *fakeRepository) FindLLMConfigByID(_ context.Context, id int64) (*model.LLMConfig, error) {
	config, ok := f.configs[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return config, nil
}

func (f *fakeRepository) CreateLLMConfig(_ context.Context, config *model.LLMConfig) error {
	if config.IsDefault {
		f.clearDefault()
	}
	config.ID = f.nextID
	f.nextID++
	f.configs[config.ID] = config
	return nil
}

func (f *fakeRepository) UpdateLLMConfig(_ context.Context, id int64, updates repository.LLMConfigUpdates) (*model.LLMConfig, error) {
	config, ok := f.configs[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	if updates.IsDefault != nil && *updates.IsDefault {
		f.clearDefault()
	}
	if updates.Name != nil {
		config.Name = *updates.Name
	}
	if updates.Provider != nil {
		config.Provider = *updates.Provider
	}
	if updates.BaseURL != nil {
		config.BaseURL = *updates.BaseURL
	}
	if updates.Model != nil {
		config.Model = *updates.Model
	}
	if updates.APIKeyRefSet {
		config.APIKeyRef = updates.APIKeyRef
	}
	if updates.Temperature != nil {
		config.Temperature = *updates.Temperature
	}
	if updates.Enabled != nil {
		config.Enabled = *updates.Enabled
	}
	if updates.IsDefault != nil {
		config.IsDefault = *updates.IsDefault
	}
	return config, nil
}

func (f *fakeRepository) DeleteLLMConfig(_ context.Context, id int64) error {
	if _, ok := f.configs[id]; !ok {
		return repository.ErrNotFound
	}
	delete(f.configs, id)
	return nil
}

func (f *fakeRepository) SetDefaultLLMConfig(ctx context.Context, id int64) (*model.LLMConfig, error) {
	return f.UpdateLLMConfig(ctx, id, repository.LLMConfigUpdates{IsDefault: ptr(true)})
}

func (f *fakeRepository) clearDefault() {
	for _, config := range f.configs {
		config.IsDefault = false
	}
}

type fakeSecrets struct{}

func (f *fakeSecrets) Encrypt(plaintext string) (string, error) {
	return "encrypted:" + base64.RawURLEncoding.EncodeToString([]byte(plaintext)), nil
}

func (f *fakeSecrets) Decrypt(value string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "encrypted:"))
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

type fakeClient struct {
	last ChatRequest
}

func (f *fakeClient) Chat(_ context.Context, req ChatRequest) (*ChatResult, error) {
	f.last = req
	return &ChatResult{Content: "ok", Model: req.Model, Usage: Usage{TotalTokens: 2}}, nil
}

func ptr[T any](value T) *T {
	return &value
}
