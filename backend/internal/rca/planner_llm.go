package rca

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	llmsvc "aiops-platform/backend/internal/llm"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

type PlannerLLMConfigRepository interface {
	FindDefaultEnabledLLMConfigByPurpose(context.Context, string) (*model.LLMConfig, error)
}

type PlannerSecretManager interface {
	Decrypt(string) (string, error)
}

type LLMPlanner struct {
	configs PlannerLLMConfigRepository
	secrets PlannerSecretManager
	client  llmsvc.Client
	timeout time.Duration
}

func NewLLMPlanner(configs PlannerLLMConfigRepository, secrets PlannerSecretManager, client llmsvc.Client) *LLMPlanner {
	return &LLMPlanner{configs: configs, secrets: secrets, client: client, timeout: 20 * time.Second}
}

func (p *LLMPlanner) Plan(ctx context.Context, request PlannerModelRequest) (json.RawMessage, error) {
	if p == nil || p.configs == nil || p.secrets == nil || p.client == nil {
		return nil, errors.New("llm planner unavailable")
	}
	config, err := p.configs.FindDefaultEnabledLLMConfigByPurpose(ctx, model.LLMPurposeChat)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errors.New("llm planner is not configured")
		}
		return nil, fmt.Errorf("load planner llm config: %w", err)
	}
	apiKey, err := p.decrypt(config.APIKeyRef)
	if err != nil {
		return nil, fmt.Errorf("decrypt planner api key: %w", err)
	}
	appKey, err := p.decrypt(config.AppKeyRef)
	if err != nil {
		return nil, fmt.Errorf("decrypt planner app key: %w", err)
	}
	apiSecret, err := p.decrypt(config.APISecretRef)
	if err != nil {
		return nil, fmt.Errorf("decrypt planner api secret: %w", err)
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	response, err := p.client.Chat(callCtx, llmsvc.ChatRequest{
		BaseURL: config.BaseURL, Provider: config.Provider, APIKey: apiKey, AppKey: appKey,
		APISecret: apiSecret, Model: config.Model, Temperature: config.Temperature,
		Messages: []llmsvc.ChatMessage{
			{Role: "system", Content: request.SystemPrompt},
			{Role: "user", Content: string(payload)},
		},
	})
	if err != nil {
		return nil, err
	}
	if response == nil || strings.TrimSpace(response.Content) == "" {
		return nil, errors.New("llm planner returned empty content")
	}
	return json.RawMessage(response.Content), nil
}

func (p *LLMPlanner) decrypt(reference *string) (string, error) {
	if reference == nil || strings.TrimSpace(*reference) == "" {
		return "", nil
	}
	return p.secrets.Decrypt(*reference)
}
