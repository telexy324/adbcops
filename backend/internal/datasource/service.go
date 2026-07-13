package datasource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"unicode/utf8"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/observability"
	"aiops-platform/backend/internal/repository"
)

const (
	maxNameBytes      = 120
	maxScopeBytes     = 100
	maxEnvironment    = 50
	maxCredentialJSON = 65536
)

var (
	ErrForbidden       = errors.New("data source access forbidden")
	ErrAdminRequired   = errors.New("admin role required")
	ErrInvalidInput    = errors.New("invalid input")
	ErrSensitiveConfig = errors.New("config contains sensitive credential fields")
	ErrUnsafeEndpoint  = errors.New("endpoint is not allowed")
)

type Repository interface {
	ListDataSources(ctx context.Context, enabledOnly bool) ([]model.DataSource, error)
	FindDataSourceByID(ctx context.Context, id int64) (*model.DataSource, error)
	CreateDataSource(ctx context.Context, dataSource *model.DataSource, credential *model.CredentialSecret) error
	UpdateDataSource(ctx context.Context, id int64, updates repository.DataSourceUpdates, credential *model.CredentialSecret) (*model.DataSource, error)
	DeleteDataSource(ctx context.Context, id int64) error
}

type SecretManager interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(value string) (string, error)
}

type Service struct {
	repository Repository
	secrets    SecretManager
	keyVersion string
}

type SaveInput struct {
	Name          string
	SourceType    string
	Environment   *string
	SystemName    *string
	ComponentName *string
	Config        json.RawMessage
	Credential    json.RawMessage
	Enabled       bool
	ReadOnly      bool
	CreatedBy     *int64
}

type UpdateInput struct {
	Name           *string
	SourceType     *string
	Environment    *string
	EnvironmentSet bool
	SystemName     *string
	SystemNameSet  bool
	ComponentName  *string
	ComponentSet   bool
	Config         json.RawMessage
	Credential     json.RawMessage
	Enabled        *bool
	ReadOnly       *bool
}

type DataSourceView struct {
	ID                   int64           `json:"id"`
	Name                 string          `json:"name"`
	SourceType           string          `json:"sourceType"`
	Environment          *string         `json:"environment,omitempty"`
	SystemName           *string         `json:"systemName,omitempty"`
	ComponentName        *string         `json:"componentName,omitempty"`
	Config               json.RawMessage `json:"config"`
	CredentialConfigured bool            `json:"credentialConfigured"`
	Enabled              bool            `json:"enabled"`
	ReadOnly             bool            `json:"readOnly"`
	CreatedBy            *int64          `json:"createdBy,omitempty"`
	CreatedAt            string          `json:"createdAt"`
	UpdatedAt            string          `json:"updatedAt"`
}

type TestResult struct {
	OK                   bool   `json:"ok"`
	SourceType           string `json:"sourceType"`
	CredentialConfigured bool   `json:"credentialConfigured"`
	Message              string `json:"message"`
}

func NewService(repository Repository, secrets SecretManager, keyVersion string) *Service {
	return &Service{repository: repository, secrets: secrets, keyVersion: keyVersion}
}

func (s *Service) List(ctx context.Context, actor *model.AppUser) ([]DataSourceView, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	dataSources, err := s.repository.ListDataSources(ctx, actor.Role != model.RoleAdmin)
	if err != nil {
		return nil, err
	}
	views := make([]DataSourceView, 0, len(dataSources))
	for index := range dataSources {
		views = append(views, toView(&dataSources[index]))
	}
	return views, nil
}

func (s *Service) Get(ctx context.Context, actor *model.AppUser, id int64) (*DataSourceView, error) {
	if actor == nil || id <= 0 {
		return nil, ErrInvalidInput
	}
	dataSource, err := s.repository.FindDataSourceByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if actor.Role != model.RoleAdmin && !dataSource.Enabled {
		return nil, ErrForbidden
	}
	view := toView(dataSource)
	return &view, nil
}

func (s *Service) Create(ctx context.Context, actor *model.AppUser, input SaveInput) (*DataSourceView, error) {
	if actor == nil {
		return nil, ErrInvalidInput
	}
	if actor.Role != model.RoleAdmin {
		return nil, ErrAdminRequired
	}
	normalized, credential, err := s.normalizeSaveInput(input)
	if err != nil {
		return nil, err
	}
	dataSource := &model.DataSource{
		Name:          normalized.Name,
		SourceType:    normalized.SourceType,
		Environment:   normalized.Environment,
		SystemName:    normalized.SystemName,
		ComponentName: normalized.ComponentName,
		Config:        normalized.Config,
		Enabled:       normalized.Enabled,
		ReadOnly:      normalized.ReadOnly,
		CreatedBy:     normalized.CreatedBy,
	}
	if err := s.repository.CreateDataSource(ctx, dataSource, credential); err != nil {
		return nil, err
	}
	view := toView(dataSource)
	return &view, nil
}

func (s *Service) Update(ctx context.Context, actor *model.AppUser, id int64, input UpdateInput) (*DataSourceView, error) {
	if actor == nil || id <= 0 {
		return nil, ErrInvalidInput
	}
	if actor.Role != model.RoleAdmin {
		return nil, ErrAdminRequired
	}
	updates, credential, err := s.normalizeUpdateInput(input)
	if err != nil {
		return nil, err
	}
	dataSource, err := s.repository.UpdateDataSource(ctx, id, updates, credential)
	if err != nil {
		return nil, err
	}
	view := toView(dataSource)
	return &view, nil
}

func (s *Service) Delete(ctx context.Context, actor *model.AppUser, id int64) error {
	if actor == nil || id <= 0 {
		return ErrInvalidInput
	}
	if actor.Role != model.RoleAdmin {
		return ErrAdminRequired
	}
	return s.repository.DeleteDataSource(ctx, id)
}

func (s *Service) Test(ctx context.Context, actor *model.AppUser, id int64) (*TestResult, error) {
	if actor == nil || id <= 0 {
		return nil, ErrInvalidInput
	}
	if actor.Role != model.RoleAdmin {
		return nil, ErrAdminRequired
	}
	dataSource, err := s.repository.FindDataSourceByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !dataSource.Enabled {
		return nil, ErrInvalidInput
	}
	if err := validateConfigByType(dataSource.SourceType, dataSource.Config); err != nil {
		return nil, err
	}
	if dataSource.CredentialID != nil && dataSource.Credential != nil && s.secrets != nil {
		if _, err := s.secrets.Decrypt(dataSource.Credential.EncryptedPayload); err != nil {
			observability.SetDatasourceHealth(dataSource.SourceType, dataSource.ID, false)
			return nil, fmt.Errorf("decrypt data source credential: %w", err)
		}
	}
	observability.SetDatasourceHealth(dataSource.SourceType, dataSource.ID, true)
	return &TestResult{
		OK:                   true,
		SourceType:           dataSource.SourceType,
		CredentialConfigured: dataSource.CredentialID != nil,
		Message:              "data source configuration is readable",
	}, nil
}

func (s *Service) normalizeSaveInput(input SaveInput) (SaveInput, *model.CredentialSecret, error) {
	name, err := normalizeRequired(input.Name, maxNameBytes)
	if err != nil {
		return SaveInput{}, nil, err
	}
	sourceType, err := normalizeSourceType(input.SourceType)
	if err != nil {
		return SaveInput{}, nil, err
	}
	environment, err := normalizeOptional(input.Environment, maxEnvironment)
	if err != nil {
		return SaveInput{}, nil, err
	}
	systemName, err := normalizeOptional(input.SystemName, maxScopeBytes)
	if err != nil {
		return SaveInput{}, nil, err
	}
	componentName, err := normalizeOptional(input.ComponentName, maxScopeBytes)
	if err != nil {
		return SaveInput{}, nil, err
	}
	config, err := normalizeConfig(sourceType, input.Config)
	if err != nil {
		return SaveInput{}, nil, err
	}
	credential, err := s.newCredential(sourceType, input.Credential, input.CreatedBy)
	if err != nil {
		return SaveInput{}, nil, err
	}
	input.Name = name
	input.SourceType = sourceType
	input.Environment = environment
	input.SystemName = systemName
	input.ComponentName = componentName
	input.Config = config
	if !input.ReadOnly {
		input.ReadOnly = true
	}
	return input, credential, nil
}

func (s *Service) normalizeUpdateInput(input UpdateInput) (repository.DataSourceUpdates, *model.CredentialSecret, error) {
	updates := repository.DataSourceUpdates{}
	var sourceType string
	if input.SourceType != nil {
		normalized, err := normalizeSourceType(*input.SourceType)
		if err != nil {
			return updates, nil, err
		}
		updates.SourceType = &normalized
		sourceType = normalized
	}
	if input.Name != nil {
		name, err := normalizeRequired(*input.Name, maxNameBytes)
		if err != nil {
			return updates, nil, err
		}
		updates.Name = &name
	}
	if input.EnvironmentSet {
		value, err := normalizeOptional(input.Environment, maxEnvironment)
		if err != nil {
			return updates, nil, err
		}
		updates.Environment = value
		updates.EnvironmentSet = true
	}
	if input.SystemNameSet {
		value, err := normalizeOptional(input.SystemName, maxScopeBytes)
		if err != nil {
			return updates, nil, err
		}
		updates.SystemName = value
		updates.SystemNameSet = true
	}
	if input.ComponentSet {
		value, err := normalizeOptional(input.ComponentName, maxScopeBytes)
		if err != nil {
			return updates, nil, err
		}
		updates.ComponentName = value
		updates.ComponentSet = true
	}
	if len(input.Config) > 0 {
		config, err := normalizeConfig(sourceType, input.Config)
		if err != nil {
			return updates, nil, err
		}
		updates.Config = config
		updates.ConfigSet = true
	}
	updates.Enabled = input.Enabled
	if input.ReadOnly != nil {
		readOnly := true
		updates.ReadOnly = &readOnly
	}
	credential, err := s.newCredential(sourceType, input.Credential, nil)
	if err != nil {
		return updates, nil, err
	}
	return updates, credential, nil
}

func (s *Service) newCredential(secretType string, raw json.RawMessage, createdBy *int64) (*model.CredentialSecret, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if len(raw) > maxCredentialJSON || !json.Valid(raw) {
		return nil, ErrInvalidInput
	}
	if s.secrets == nil {
		return nil, ErrInvalidInput
	}
	encrypted, err := s.secrets.Encrypt(string(raw))
	if err != nil {
		return nil, fmt.Errorf("encrypt data source credential: %w", err)
	}
	return &model.CredentialSecret{
		SecretType:       secretType,
		EncryptedPayload: encrypted,
		KeyVersion:       optionalKeyVersion(s.keyVersion),
		CreatedBy:        createdBy,
	}, nil
}

func normalizeConfig(sourceType string, raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage(`{}`)
	}
	if !json.Valid(raw) {
		return nil, ErrInvalidInput
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, ErrInvalidInput
	}
	if containsSensitiveKey(value) {
		return nil, ErrSensitiveConfig
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return nil, ErrInvalidInput
	}
	if err := validateConfigByType(sourceType, normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

func validateConfigByType(sourceType string, raw []byte) error {
	if len(raw) == 0 {
		return ErrInvalidInput
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return ErrInvalidInput
	}
	switch sourceType {
	case model.DataSourceTypeElasticsearch, model.DataSourceTypeOpenSearch, model.DataSourceTypePrometheus, model.DataSourceTypeHTTP, model.DataSourceTypeNacos, model.DataSourceTypeNginx:
		if endpoint, ok := config["baseUrl"].(string); ok && strings.TrimSpace(endpoint) != "" {
			return validateEndpoint(endpoint)
		}
	case model.DataSourceTypeKubernetes:
		if endpoint, ok := config["apiServer"].(string); ok && strings.TrimSpace(endpoint) != "" {
			return validateEndpoint(endpoint)
		}
	case model.DataSourceTypeSSH:
		if host, ok := config["host"].(string); ok && strings.TrimSpace(host) == "" {
			return ErrInvalidInput
		}
	case model.DataSourceTypeRedis:
		mode, _ := config["mode"].(string)
		if mode != "" && mode != "standalone" && mode != "sentinel" && mode != "cluster" {
			return ErrInvalidInput
		}
		if endpoints, ok := config["endpoints"].([]any); ok && len(endpoints) == 0 {
			return ErrInvalidInput
		}
	case model.DataSourceTypeTiDB:
		if dsn, ok := config["dsn"].(string); ok && strings.TrimSpace(dsn) == "" {
			return ErrInvalidInput
		}
	}
	return nil
}

func validateEndpoint(endpoint string) error {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ErrInvalidInput
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ErrInvalidInput
	}
	host := strings.Trim(strings.ToLower(parsed.Hostname()), "[]")
	if host == "" {
		return ErrInvalidInput
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return ErrUnsafeEndpoint
	}
	if ip := net.ParseIP(host); ip != nil && unsafeIP(ip) {
		return ErrUnsafeEndpoint
	}
	return nil
}

func unsafeIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast()
}

func containsSensitiveKey(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := strings.ToLower(strings.ReplaceAll(key, "_", ""))
			if strings.Contains(normalized, "password") ||
				strings.Contains(normalized, "secret") ||
				strings.Contains(normalized, "token") ||
				strings.Contains(normalized, "apikey") ||
				strings.Contains(normalized, "privatekey") {
				return true
			}
			if containsSensitiveKey(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsSensitiveKey(child) {
				return true
			}
		}
	}
	return false
}

func normalizeSourceType(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case model.DataSourceTypeElasticsearch,
		model.DataSourceTypeOpenSearch,
		model.DataSourceTypePrometheus,
		model.DataSourceTypeKubernetes,
		model.DataSourceTypeSSH,
		model.DataSourceTypeHTTP,
		model.DataSourceTypeNacos,
		model.DataSourceTypeRedis,
		model.DataSourceTypeTiDB,
		model.DataSourceTypeNginx:
		return normalized, nil
	default:
		return "", ErrInvalidInput
	}
}

func normalizeRequired(value string, maxBytes int) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" || len(normalized) > maxBytes || !utf8.ValidString(normalized) {
		return "", ErrInvalidInput
	}
	return normalized, nil
}

func normalizeOptional(value *string, maxBytes int) (*string, error) {
	if value == nil {
		return nil, nil
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil, nil
	}
	if len(normalized) > maxBytes || !utf8.ValidString(normalized) {
		return nil, ErrInvalidInput
	}
	return &normalized, nil
}

func optionalKeyVersion(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func toView(dataSource *model.DataSource) DataSourceView {
	return DataSourceView{
		ID:                   dataSource.ID,
		Name:                 dataSource.Name,
		SourceType:           dataSource.SourceType,
		Environment:          dataSource.Environment,
		SystemName:           dataSource.SystemName,
		ComponentName:        dataSource.ComponentName,
		Config:               json.RawMessage(dataSource.Config),
		CredentialConfigured: dataSource.CredentialID != nil,
		Enabled:              dataSource.Enabled,
		ReadOnly:             dataSource.ReadOnly,
		CreatedBy:            dataSource.CreatedBy,
		CreatedAt:            dataSource.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:            dataSource.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}
