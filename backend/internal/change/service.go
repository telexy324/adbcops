package change

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"aiops-platform/backend/internal/model"
)

var (
	ErrInvalidInput       = errors.New("invalid input")
	ErrForbidden          = errors.New("change access forbidden")
	ErrUnsupportedSource  = errors.New("unsupported change data source")
	ErrDataSourceDisabled = errors.New("data source disabled")
)

type Repository interface {
	FindDataSourceByID(ctx context.Context, id int64) (*model.DataSource, error)
}

type SecretManager interface {
	Decrypt(value string) (string, error)
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type Service struct {
	repository Repository
	secrets    SecretManager
	client     HTTPClient
	now        func() time.Time
}

type QueryInput struct {
	DataSourceID int64
	From         *time.Time
	To           *time.Time
	Environment  string
	SystemName   string
	Component    string
	Limit        int
}

type Result struct {
	From     time.Time      `json:"from"`
	To       time.Time      `json:"to"`
	Timezone string         `json:"timezone"`
	Partial  bool           `json:"partial"`
	Sources  []SourceStatus `json:"sources"`
	Items    []ChangeItem   `json:"items"`
	Count    int            `json:"count"`
}

type SourceStatus struct {
	Kind    string `json:"kind"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Count   int    `json:"count"`
	Skipped bool   `json:"skipped,omitempty"`
}

type ChangeItem struct {
	Kind        string                 `json:"kind"`
	ID          string                 `json:"id,omitempty"`
	Title       string                 `json:"title"`
	Summary     string                 `json:"summary,omitempty"`
	Environment string                 `json:"environment,omitempty"`
	SystemName  string                 `json:"systemName,omitempty"`
	Component   string                 `json:"component,omitempty"`
	Author      string                 `json:"author,omitempty"`
	Revision    string                 `json:"revision,omitempty"`
	URL         string                 `json:"url,omitempty"`
	StartedAt   *time.Time             `json:"startedAt,omitempty"`
	FinishedAt  *time.Time             `json:"finishedAt,omitempty"`
	ObservedAt  time.Time              `json:"observedAt"`
	Raw         map[string]interface{} `json:"raw,omitempty"`
}

type config struct {
	BaseURL           string `json:"baseUrl"`
	RecentReleasePath string `json:"recentReleasePath"`
	ConfigChangePath  string `json:"configChangePath"`
	GitChangePath     string `json:"gitChangePath"`
	TimeoutMs         int    `json:"timeoutMs"`
}

func NewService(repository Repository, secrets SecretManager, client HTTPClient) *Service {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Service{
		repository: repository,
		secrets:    secrets,
		client:     client,
		now:        func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) QueryRecent(ctx context.Context, actor *model.AppUser, input QueryInput) (*Result, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	if input.DataSourceID <= 0 {
		return nil, ErrInvalidInput
	}
	dataSource, err := s.repository.FindDataSourceByID(ctx, input.DataSourceID)
	if err != nil {
		return nil, err
	}
	if !dataSource.Enabled {
		return nil, ErrDataSourceDisabled
	}
	if dataSource.SourceType != model.DataSourceTypeHTTP {
		return nil, ErrUnsupportedSource
	}
	cfg, err := parseConfig(dataSource.Config)
	if err != nil {
		return nil, err
	}
	from, to, err := s.resolveWindow(input)
	if err != nil {
		return nil, err
	}
	result := &Result{From: from, To: to, Timezone: "UTC"}
	credential := ""
	if dataSource.Credential != nil && s.secrets != nil {
		credential, _ = s.secrets.Decrypt(dataSource.Credential.EncryptedPayload)
	}
	for _, source := range []struct {
		kind string
		path string
	}{
		{"release", cfg.RecentReleasePath},
		{"config_change", cfg.ConfigChangePath},
		{"git_change", cfg.GitChangePath},
	} {
		status := SourceStatus{Kind: source.kind}
		if strings.TrimSpace(source.path) == "" {
			status.Skipped = true
			result.Sources = append(result.Sources, status)
			continue
		}
		items, err := s.queryEndpoint(ctx, cfg.BaseURL, source.path, credential, input, from, to, source.kind)
		if err != nil {
			status.OK = false
			status.Error = err.Error()
			result.Partial = true
			result.Sources = append(result.Sources, status)
			continue
		}
		status.OK = true
		status.Count = len(items)
		result.Sources = append(result.Sources, status)
		result.Items = append(result.Items, items...)
	}
	sort.SliceStable(result.Items, func(i, j int) bool {
		if result.Items[i].ObservedAt.Equal(result.Items[j].ObservedAt) {
			return result.Items[i].Kind < result.Items[j].Kind
		}
		return result.Items[i].ObservedAt.Before(result.Items[j].ObservedAt)
	})
	limit := input.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
	}
	result.Count = len(result.Items)
	return result, nil
}

func (s *Service) resolveWindow(input QueryInput) (time.Time, time.Time, error) {
	to := s.now().UTC()
	if input.To != nil {
		to = input.To.UTC()
	}
	from := to.Add(-2 * time.Hour)
	if input.From != nil {
		from = input.From.UTC()
	}
	if to.Before(from) {
		return time.Time{}, time.Time{}, ErrInvalidInput
	}
	return from, to, nil
}

func (s *Service) queryEndpoint(ctx context.Context, baseURL, path, credential string, input QueryInput, from, to time.Time, kind string) ([]ChangeItem, error) {
	endpoint, err := joinURL(baseURL, path)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set("from", from.Format(time.RFC3339))
	query.Set("to", to.Format(time.RFC3339))
	if input.Environment != "" {
		query.Set("environment", input.Environment)
	}
	if input.SystemName != "" {
		query.Set("systemName", input.SystemName)
	}
	if input.Component != "" {
		query.Set("component", input.Component)
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	applyCredential(req, credential)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	return parseItems(kind, body)
}

func parseConfig(raw []byte) (config, error) {
	var cfg config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return config{}, ErrInvalidInput
	}
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	if cfg.BaseURL == "" {
		return config{}, ErrInvalidInput
	}
	return cfg, nil
}

func joinURL(baseURL, path string) (*url.URL, error) {
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, ErrInvalidInput
	}
	ref, err := url.Parse(strings.TrimSpace(path))
	if err != nil {
		return nil, ErrInvalidInput
	}
	return base.ResolveReference(ref), nil
}

func applyCredential(req *http.Request, raw string) {
	if strings.TrimSpace(raw) == "" {
		return
	}
	var credential struct {
		BearerToken string `json:"bearerToken"`
		Username    string `json:"username"`
		Password    string `json:"password"`
	}
	if err := json.Unmarshal([]byte(raw), &credential); err != nil {
		return
	}
	if credential.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+credential.BearerToken)
		return
	}
	if credential.Username != "" || credential.Password != "" {
		req.SetBasicAuth(credential.Username, credential.Password)
	}
}

func parseItems(kind string, raw []byte) ([]ChangeItem, error) {
	var wrapped struct {
		Items []map[string]interface{} `json:"items"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Items != nil {
		return normalizeItems(kind, wrapped.Items), nil
	}
	var array []map[string]interface{}
	if err := json.Unmarshal(raw, &array); err != nil {
		return nil, ErrInvalidInput
	}
	return normalizeItems(kind, array), nil
}

func normalizeItems(kind string, rawItems []map[string]interface{}) []ChangeItem {
	items := make([]ChangeItem, 0, len(rawItems))
	for _, raw := range rawItems {
		item := ChangeItem{
			Kind:        kind,
			ID:          stringValue(raw, "id"),
			Title:       firstString(raw, "title", "name", "summary", "message"),
			Summary:     stringValue(raw, "summary"),
			Environment: stringValue(raw, "environment"),
			SystemName:  firstString(raw, "systemName", "system"),
			Component:   stringValue(raw, "component"),
			Author:      firstString(raw, "author", "user", "committer"),
			Revision:    firstString(raw, "revision", "commit", "version"),
			URL:         firstString(raw, "url", "link"),
			Raw:         raw,
		}
		item.StartedAt = optionalTime(raw, "startedAt", "startTime")
		item.FinishedAt = optionalTime(raw, "finishedAt", "endTime", "deployedAt", "committedAt", "changedAt")
		item.ObservedAt = observedTime(item, raw)
		if item.Title == "" {
			item.Title = kind + " change"
		}
		items = append(items, item)
	}
	return items
}

func observedTime(item ChangeItem, raw map[string]interface{}) time.Time {
	for _, key := range []string{"observedAt", "time", "timestamp", "createdAt"} {
		if parsed := optionalTime(raw, key); parsed != nil {
			return parsed.UTC()
		}
	}
	if item.FinishedAt != nil {
		return item.FinishedAt.UTC()
	}
	if item.StartedAt != nil {
		return item.StartedAt.UTC()
	}
	return time.Now().UTC()
}

func optionalTime(raw map[string]interface{}, keys ...string) *time.Time {
	for _, key := range keys {
		value := stringValue(raw, key)
		if value == "" {
			continue
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
			parsed, err := time.Parse(layout, value)
			if err == nil {
				utc := parsed.UTC()
				return &utc
			}
		}
	}
	return nil
}

func firstString(raw map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(raw, key); value != "" {
			return value
		}
	}
	return ""
}

func stringValue(raw map[string]interface{}, key string) string {
	value, ok := raw[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}
