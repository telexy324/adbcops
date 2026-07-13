package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	llmsvc "aiops-platform/backend/internal/llm"
	appmiddleware "aiops-platform/backend/internal/middleware"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
	"github.com/gin-gonic/gin"
)

type LLMHandler struct {
	service *llmsvc.Service
}

func NewLLMHandler(service *llmsvc.Service) *LLMHandler {
	return &LLMHandler{service: service}
}

type saveLLMConfigRequest struct {
	Name        string   `json:"name" binding:"required"`
	Provider    string   `json:"provider" binding:"required"`
	BaseURL     string   `json:"baseUrl" binding:"required"`
	Model       string   `json:"model" binding:"required"`
	Purpose     string   `json:"purpose"`
	APIKey      *string  `json:"apiKey"`
	APISecret   *string  `json:"apiSecret"`
	Temperature *float64 `json:"temperature"`
	Enabled     *bool    `json:"enabled"`
	IsDefault   bool     `json:"isDefault"`
}

type updateLLMConfigRequest struct {
	Name        *string  `json:"name"`
	Provider    *string  `json:"provider"`
	BaseURL     *string  `json:"baseUrl"`
	Model       *string  `json:"model"`
	Purpose     *string  `json:"purpose"`
	APIKey      *string  `json:"apiKey"`
	APISecret   *string  `json:"apiSecret"`
	Temperature *float64 `json:"temperature"`
	Enabled     *bool    `json:"enabled"`
	IsDefault   *bool    `json:"isDefault"`
}

type testLLMConfigRequest struct {
	Prompt string `json:"prompt"`
}

type llmConfigResponse struct {
	ID                  int64   `json:"id"`
	Name                string  `json:"name"`
	Provider            string  `json:"provider"`
	BaseURL             string  `json:"baseUrl"`
	Model               string  `json:"model"`
	Purpose             string  `json:"purpose"`
	Temperature         float64 `json:"temperature"`
	Enabled             bool    `json:"enabled"`
	IsDefault           bool    `json:"isDefault"`
	APIKeyConfigured    bool    `json:"apiKeyConfigured"`
	APISecretConfigured bool    `json:"apiSecretConfigured"`
	CreatedBy           *int64  `json:"createdBy,omitempty"`
	CreatedAt           string  `json:"createdAt"`
	UpdatedAt           string  `json:"updatedAt"`
}

func (h *LLMHandler) List(c *gin.Context) {
	configs, err := h.service.List(c.Request.Context())
	if handleLLMError(c, err, "list llm configs failed") {
		return
	}
	response := make([]llmConfigResponse, 0, len(configs))
	for index := range configs {
		response = append(response, toLLMConfigResponse(&configs[index]))
	}
	success(c, response)
}

func (h *LLMHandler) Create(c *gin.Context) {
	var request saveLLMConfigRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	temperature := 0.2
	if request.Temperature != nil {
		temperature = *request.Temperature
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	var createdBy *int64
	if user, ok := appmiddleware.AuthenticatedUser(c); ok {
		id := user.ID
		createdBy = &id
	}
	config, err := h.service.Create(c.Request.Context(), llmsvc.SaveInput{
		Name:        request.Name,
		Provider:    request.Provider,
		BaseURL:     request.BaseURL,
		Model:       request.Model,
		Purpose:     request.Purpose,
		APIKey:      request.APIKey,
		APISecret:   request.APISecret,
		Temperature: temperature,
		Enabled:     enabled,
		IsDefault:   request.IsDefault,
		CreatedBy:   createdBy,
	})
	if handleLLMError(c, err, "create llm config failed") {
		return
	}
	success(c, toLLMConfigResponse(config))
}

func (h *LLMHandler) Get(c *gin.Context) {
	id, ok := parseLLMConfigID(c)
	if !ok {
		return
	}
	config, err := h.service.Get(c.Request.Context(), id)
	if handleLLMError(c, err, "get llm config failed") {
		return
	}
	success(c, toLLMConfigResponse(config))
}

func (h *LLMHandler) Update(c *gin.Context) {
	id, ok := parseLLMConfigID(c)
	if !ok {
		return
	}
	var request updateLLMConfigRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	config, err := h.service.Update(c.Request.Context(), id, llmsvc.UpdateInput{
		Name:        request.Name,
		Provider:    request.Provider,
		BaseURL:     request.BaseURL,
		Model:       request.Model,
		Purpose:     request.Purpose,
		APIKey:      request.APIKey,
		APISecret:   request.APISecret,
		Temperature: request.Temperature,
		Enabled:     request.Enabled,
		IsDefault:   request.IsDefault,
	})
	if handleLLMError(c, err, "update llm config failed") {
		return
	}
	success(c, toLLMConfigResponse(config))
}

func (h *LLMHandler) Delete(c *gin.Context) {
	id, ok := parseLLMConfigID(c)
	if !ok {
		return
	}
	err := h.service.Delete(c.Request.Context(), id)
	if handleLLMError(c, err, "delete llm config failed") {
		return
	}
	success(c, gin.H{"deleted": true})
}

func (h *LLMHandler) SetDefault(c *gin.Context) {
	id, ok := parseLLMConfigID(c)
	if !ok {
		return
	}
	config, err := h.service.SetDefault(c.Request.Context(), id)
	if handleLLMError(c, err, "set default llm config failed") {
		return
	}
	success(c, toLLMConfigResponse(config))
}

func (h *LLMHandler) Test(c *gin.Context) {
	id, ok := parseLLMConfigID(c)
	if !ok {
		return
	}
	var request testLLMConfigRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	result, err := h.service.Test(c.Request.Context(), id, request.Prompt)
	if handleLLMError(c, err, "llm test failed") {
		return
	}
	success(c, result)
}

func parseLLMConfigID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return 0, false
	}
	return id, true
}

func handleLLMError(c *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, llmsvc.ErrInvalidInput):
		failure(c, http.StatusBadRequest, 40001, "invalid request")
	case errors.Is(err, llmsvc.ErrLLMLimited):
		failure(c, http.StatusTooManyRequests, 42904, "llm concurrency limit exceeded")
	case errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40401, "llm config not found")
	default:
		failure(c, http.StatusInternalServerError, 50030, fallback)
	}
	return true
}

func toLLMConfigResponse(config *model.LLMConfig) llmConfigResponse {
	return llmConfigResponse{
		ID:                  config.ID,
		Name:                config.Name,
		Provider:            config.Provider,
		BaseURL:             config.BaseURL,
		Model:               config.Model,
		Purpose:             config.Purpose,
		Temperature:         config.Temperature,
		Enabled:             config.Enabled,
		IsDefault:           config.IsDefault,
		APIKeyConfigured:    config.APIKeyRef != nil && *config.APIKeyRef != "",
		APISecretConfigured: config.APISecretRef != nil && *config.APISecretRef != "",
		CreatedBy:           config.CreatedBy,
		CreatedAt:           config.CreatedAt.Format(time.RFC3339),
		UpdatedAt:           config.UpdatedAt.Format(time.RFC3339),
	}
}
