package logs

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/resourcelimit"
)

const (
	defaultSize      = 100
	maxSize          = 1000
	defaultTimeout   = 10 * time.Second
	maxTimeout       = 30 * time.Second
	maxDefaultWindow = 24 * time.Hour
)

var (
	ErrForbidden          = errors.New("log access forbidden")
	ErrInvalidInput       = errors.New("invalid input")
	ErrUnsupportedSource  = errors.New("unsupported log data source")
	ErrTimeRangeTooLarge  = errors.New("time range exceeds 24 hours")
	ErrLogQueryTimeout    = errors.New("log query timeout")
	ErrDataSourceDisabled = errors.New("data source disabled")
	ErrDataSourceLimited  = errors.New("data source concurrency limit exceeded")
)

type Repository interface {
	FindDataSourceByID(ctx context.Context, id int64) (*model.DataSource, error)
}

type SecretManager interface {
	Decrypt(value string) (string, error)
}

type Service struct {
	repository Repository
	secrets    SecretManager
	client     *http.Client
	limiter    *resourcelimit.KeyedLimiter
}

type QueryInput struct {
	DataSourceID    int64
	Index           string
	From            time.Time
	To              time.Time
	Keyword         string
	QueryString     string
	Level           string
	Size            int
	Timeout         time.Duration
	AllowLargeRange bool
}

type QueryResult struct {
	Items      []model.LogItem `json:"items"`
	Total      int64           `json:"total"`
	TimedOut   bool            `json:"timedOut"`
	DataSource int64           `json:"dataSourceId"`
	Index      string          `json:"index"`
}

type esConfig struct {
	BaseURL        string `json:"baseUrl"`
	Index          string `json:"index"`
	TimeField      string `json:"timeField"`
	LevelField     string `json:"levelField"`
	MessageField   string `json:"messageField"`
	SourceField    string `json:"sourceField"`
	HostField      string `json:"hostField"`
	ClusterField   string `json:"clusterField"`
	NamespaceField string `json:"namespaceField"`
	PodField       string `json:"podField"`
	ContainerField string `json:"containerField"`
	TraceIDField   string `json:"traceIdField"`
	RequestIDField string `json:"requestIdField"`
	ErrorCodeField string `json:"errorCodeField"`
}

type credentialConfig struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	BearerToken string `json:"bearerToken"`
	APIKey      string `json:"apiKey"`
}

func NewService(repository Repository, secrets SecretManager, client *http.Client) *Service {
	if client == nil {
		client = &http.Client{Timeout: maxTimeout}
	}
	return &Service{repository: repository, secrets: secrets, client: client, limiter: resourcelimit.NewKeyedLimiter(4)}
}

func (s *Service) SetDataSourceLimiter(limiter *resourcelimit.KeyedLimiter) {
	s.limiter = limiter
}

func (s *Service) Query(ctx context.Context, actor *model.AppUser, input QueryInput) (*QueryResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	normalized, err := normalizeInput(input)
	if err != nil {
		return nil, err
	}
	release, err := s.limiter.Acquire(ctx, fmt.Sprintf("logs:%d", normalized.DataSourceID))
	if err != nil {
		if errors.Is(err, resourcelimit.ErrLimitExceeded) {
			return nil, ErrDataSourceLimited
		}
		return nil, err
	}
	defer release()
	dataSource, err := s.repository.FindDataSourceByID(ctx, normalized.DataSourceID)
	if err != nil {
		return nil, err
	}
	if !dataSource.Enabled {
		return nil, ErrDataSourceDisabled
	}
	if dataSource.SourceType != model.DataSourceTypeElasticsearch && dataSource.SourceType != model.DataSourceTypeOpenSearch {
		return nil, ErrUnsupportedSource
	}
	config, err := parseESConfig(dataSource.Config)
	if err != nil {
		return nil, err
	}
	index := normalized.Index
	if index == "" {
		index = config.Index
	}
	if index == "" {
		return nil, ErrInvalidInput
	}
	credential, err := s.loadCredential(dataSource)
	if err != nil {
		return nil, err
	}
	requestBody, err := buildQuery(config, normalized)
	if err != nil {
		return nil, err
	}
	queryContext, cancel := context.WithTimeout(ctx, normalized.Timeout)
	defer cancel()
	endpoint := strings.TrimRight(config.BaseURL, "/") + "/" + url.PathEscape(index) + "/_search"
	request, err := http.NewRequestWithContext(queryContext, http.MethodPost, endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("create elasticsearch query request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	applyCredential(request, credential)
	response, err := s.client.Do(request)
	if err != nil {
		if isTimeout(err) || errors.Is(queryContext.Err(), context.DeadlineExceeded) {
			return nil, ErrLogQueryTimeout
		}
		return nil, fmt.Errorf("query elasticsearch: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("read elasticsearch response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("elasticsearch returned status %d", response.StatusCode)
	}
	result, err := decodeResponse(responseBody, config, dataSource)
	if err != nil {
		return nil, err
	}
	result.DataSource = dataSource.ID
	result.Index = index
	return result, nil
}

func normalizeInput(input QueryInput) (QueryInput, error) {
	if input.DataSourceID <= 0 || input.From.IsZero() || input.To.IsZero() || !input.From.Before(input.To) {
		return QueryInput{}, ErrInvalidInput
	}
	if !input.AllowLargeRange && input.To.Sub(input.From) > maxDefaultWindow {
		return QueryInput{}, ErrTimeRangeTooLarge
	}
	if input.Size <= 0 {
		input.Size = defaultSize
	}
	if input.Size > maxSize {
		input.Size = maxSize
	}
	if input.Timeout <= 0 {
		input.Timeout = defaultTimeout
	}
	if input.Timeout > maxTimeout {
		input.Timeout = maxTimeout
	}
	input.Keyword = strings.TrimSpace(input.Keyword)
	input.QueryString = strings.TrimSpace(input.QueryString)
	input.Level = strings.TrimSpace(input.Level)
	input.Index = strings.TrimSpace(input.Index)
	if !utf8.ValidString(input.Keyword) || !utf8.ValidString(input.QueryString) || !utf8.ValidString(input.Level) || !utf8.ValidString(input.Index) {
		return QueryInput{}, ErrInvalidInput
	}
	return input, nil
}

func parseESConfig(raw []byte) (esConfig, error) {
	var config esConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return esConfig{}, ErrInvalidInput
	}
	config.BaseURL = strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	parsed, err := url.Parse(config.BaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return esConfig{}, ErrInvalidInput
	}
	config.TimeField = valueOrDefault(config.TimeField, "@timestamp")
	config.LevelField = valueOrDefault(config.LevelField, "level")
	config.MessageField = valueOrDefault(config.MessageField, "message")
	config.SourceField = valueOrDefault(config.SourceField, "source")
	config.HostField = valueOrDefault(config.HostField, "host")
	config.ClusterField = valueOrDefault(config.ClusterField, "cluster")
	config.NamespaceField = valueOrDefault(config.NamespaceField, "namespace")
	config.PodField = valueOrDefault(config.PodField, "pod")
	config.ContainerField = valueOrDefault(config.ContainerField, "container")
	config.TraceIDField = valueOrDefault(config.TraceIDField, "traceId")
	config.RequestIDField = valueOrDefault(config.RequestIDField, "requestId")
	config.ErrorCodeField = valueOrDefault(config.ErrorCodeField, "errorCode")
	return config, nil
}

func (s *Service) loadCredential(dataSource *model.DataSource) (credentialConfig, error) {
	if dataSource.Credential == nil || dataSource.Credential.EncryptedPayload == "" || s.secrets == nil {
		return credentialConfig{}, nil
	}
	plaintext, err := s.secrets.Decrypt(dataSource.Credential.EncryptedPayload)
	if err != nil {
		return credentialConfig{}, fmt.Errorf("decrypt elasticsearch credential: %w", err)
	}
	var credential credentialConfig
	if err := json.Unmarshal([]byte(plaintext), &credential); err != nil {
		return credentialConfig{}, ErrInvalidInput
	}
	return credential, nil
}

func buildQuery(config esConfig, input QueryInput) ([]byte, error) {
	filter := []any{
		map[string]any{"range": map[string]any{config.TimeField: map[string]any{
			"gte": input.From.Format(time.RFC3339Nano),
			"lte": input.To.Format(time.RFC3339Nano),
		}}},
	}
	must := []any{}
	if input.Level != "" {
		filter = append(filter, map[string]any{"term": map[string]any{config.LevelField: input.Level}})
	}
	if input.Keyword != "" {
		must = append(must, map[string]any{"query_string": map[string]any{
			"query":                  input.Keyword,
			"default_field":          config.MessageField,
			"allow_leading_wildcard": false,
		}})
	}
	if input.QueryString != "" {
		must = append(must, map[string]any{"query_string": map[string]any{
			"query": input.QueryString,
		}})
	}
	body := map[string]any{
		"size":    input.Size,
		"timeout": strconv.Itoa(int(input.Timeout.Milliseconds())) + "ms",
		"sort":    []any{map[string]any{config.TimeField: map[string]any{"order": "desc"}}},
		"query": map[string]any{
			"bool": map[string]any{
				"filter": filter,
				"must":   must,
			},
		},
	}
	return json.Marshal(body)
}

func applyCredential(request *http.Request, credential credentialConfig) {
	switch {
	case credential.BearerToken != "":
		request.Header.Set("Authorization", "Bearer "+credential.BearerToken)
	case credential.APIKey != "":
		request.Header.Set("Authorization", "ApiKey "+credential.APIKey)
	case credential.Username != "" || credential.Password != "":
		token := base64.StdEncoding.EncodeToString([]byte(credential.Username + ":" + credential.Password))
		request.Header.Set("Authorization", "Basic "+token)
	}
}

func decodeResponse(raw []byte, config esConfig, dataSource *model.DataSource) (*QueryResult, error) {
	var decoded struct {
		TimedOut bool `json:"timed_out"`
		Hits     struct {
			Total any `json:"total"`
			Hits  []struct {
				Source map[string]any `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decode elasticsearch response: %w", err)
	}
	items := make([]model.LogItem, 0, len(decoded.Hits.Hits))
	for _, hit := range decoded.Hits.Hits {
		items = append(items, sourceToLogItem(hit.Source, config, dataSource))
	}
	return &QueryResult{Items: items, Total: parseTotal(decoded.Hits.Total), TimedOut: decoded.TimedOut}, nil
}

func sourceToLogItem(source map[string]any, config esConfig, dataSource *model.DataSource) model.LogItem {
	raw, _ := json.Marshal(source)
	return model.LogItem{
		Timestamp:   parseTime(getString(source, config.TimeField)),
		Level:       getString(source, config.LevelField),
		Message:     firstNonEmpty(getString(source, config.MessageField), getString(source, "log"), string(raw)),
		Source:      firstNonEmpty(getString(source, config.SourceField), dataSource.SourceType),
		SystemName:  deref(dataSource.SystemName),
		Component:   firstNonEmpty(getString(source, "component"), deref(dataSource.ComponentName)),
		Environment: firstNonEmpty(getString(source, "environment"), deref(dataSource.Environment)),
		Host:        getString(source, config.HostField),
		Cluster:     getString(source, config.ClusterField),
		Namespace:   getString(source, config.NamespaceField),
		Pod:         getString(source, config.PodField),
		Container:   getString(source, config.ContainerField),
		TraceID:     getString(source, config.TraceIDField),
		RequestID:   getString(source, config.RequestIDField),
		ErrorCode:   getString(source, config.ErrorCodeField),
		Raw:         string(raw),
	}
}

func getString(source map[string]any, field string) string {
	if field == "" {
		return ""
	}
	value, ok := source[field]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	default:
		encoded, _ := json.Marshal(typed)
		return string(encoded)
	}
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func parseTotal(total any) int64 {
	switch typed := total.(type) {
	case float64:
		return int64(typed)
	case map[string]any:
		if value, ok := typed["value"].(float64); ok {
			return int64(value)
		}
	}
	return 0
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
