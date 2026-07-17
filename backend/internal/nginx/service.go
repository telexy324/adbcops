package nginx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	internalhttp "aiops-platform/backend/internal/httpclient"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/resourcelimit"
)

const (
	defaultTimeout = 8 * time.Second
	maxTimeout     = 30 * time.Second
	defaultLimit   = 200
	maxLimit       = 2000
	maxBodyBytes   = 8 << 20
)

var (
	ErrForbidden          = errors.New("nginx access forbidden")
	ErrInvalidInput       = errors.New("invalid input")
	ErrUnsupportedSource  = errors.New("unsupported nginx data source")
	ErrDataSourceDisabled = errors.New("data source disabled")
	ErrDataSourceNotRead  = errors.New("nginx data source must be read-only")
	ErrNginxTimeout       = errors.New("nginx query timeout")
	ErrDataSourceLimited  = errors.New("nginx data source concurrency limit exceeded")
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
	limiter    *resourcelimit.KeyedLimiter
}

type Config struct {
	BaseURL            string `json:"baseUrl"`
	InsecureSkipTLS    bool   `json:"insecureSkipTlsVerify"`
	AccessLogPath      string `json:"accessLogPath"`
	ErrorLogPath       string `json:"errorLogPath"`
	MetricsPath        string `json:"metricsPath"`
	UpstreamStatusPath string `json:"upstreamStatusPath"`
	ConfigMetadataPath string `json:"configMetadataPath"`
	QueryTimeoutSec    int    `json:"queryTimeoutSeconds"`
	MaskClientIP       bool   `json:"maskClientIp"`
	MaxLines           int    `json:"maxLines"`
}

type credentialConfig struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	BearerToken string `json:"bearerToken"`
}

type QueryInput struct {
	DataSourceID int64
	Limit        int
}

type AccessLogResult struct {
	DataSourceID int64             `json:"dataSourceId"`
	Items        []AccessLogRecord `json:"items"`
	Truncated    bool              `json:"truncated"`
}

type AccessLogRecord struct {
	ClientIP   string            `json:"clientIp,omitempty"`
	Method     string            `json:"method,omitempty"`
	Path       string            `json:"path,omitempty"`
	Status     int               `json:"status,omitempty"`
	Bytes      int64             `json:"bytes,omitempty"`
	Referer    string            `json:"referer,omitempty"`
	UserAgent  string            `json:"userAgent,omitempty"`
	Raw        string            `json:"raw,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

type ErrorLogResult struct {
	DataSourceID int64            `json:"dataSourceId"`
	Items        []ErrorLogRecord `json:"items"`
	Truncated    bool             `json:"truncated"`
}

type ErrorLogRecord struct {
	Time     string `json:"time,omitempty"`
	Level    string `json:"level,omitempty"`
	Message  string `json:"message"`
	ClientIP string `json:"clientIp,omitempty"`
	Raw      string `json:"raw,omitempty"`
}

type MetricsResult struct {
	DataSourceID int64             `json:"dataSourceId"`
	Metrics      map[string]string `json:"metrics"`
}

type UpstreamStatusResult struct {
	DataSourceID int64                  `json:"dataSourceId"`
	Upstreams    []map[string]string    `json:"upstreams"`
	Raw          map[string]interface{} `json:"raw,omitempty"`
}

type ConfigMetadataResult struct {
	DataSourceID int64             `json:"dataSourceId"`
	Metadata     map[string]string `json:"metadata"`
}

func NewService(repository Repository, secrets SecretManager, client HTTPClient) *Service {
	if client == nil {
		client = &http.Client{Timeout: maxTimeout}
	}
	return &Service{repository: repository, secrets: secrets, client: client, limiter: resourcelimit.NewKeyedLimiter(4)}
}

func (s *Service) SetDataSourceLimiter(limiter *resourcelimit.KeyedLimiter) {
	s.limiter = limiter
}

func (s *Service) QueryAccessLogs(ctx context.Context, actor *model.AppUser, input QueryInput) (*AccessLogResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	dataSource, cfg, credential, err := s.load(ctx, input.DataSourceID)
	if err != nil {
		return nil, err
	}
	raw, err := s.fetch(ctx, dataSource.ID, cfg, credential, cfg.AccessLogPath)
	if err != nil {
		return nil, err
	}
	limit := normalizeLimit(input.Limit, cfg.MaxLines)
	items := parseAccessLogs(raw, cfg.MaskClientIP, limit)
	return &AccessLogResult{DataSourceID: dataSource.ID, Items: items, Truncated: len(strings.Split(strings.TrimSpace(raw), "\n")) > limit}, nil
}

func (s *Service) QueryErrorLogs(ctx context.Context, actor *model.AppUser, input QueryInput) (*ErrorLogResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	dataSource, cfg, credential, err := s.load(ctx, input.DataSourceID)
	if err != nil {
		return nil, err
	}
	raw, err := s.fetch(ctx, dataSource.ID, cfg, credential, cfg.ErrorLogPath)
	if err != nil {
		return nil, err
	}
	limit := normalizeLimit(input.Limit, cfg.MaxLines)
	items := parseErrorLogs(raw, cfg.MaskClientIP, limit)
	return &ErrorLogResult{DataSourceID: dataSource.ID, Items: items, Truncated: len(strings.Split(strings.TrimSpace(raw), "\n")) > limit}, nil
}

func (s *Service) QueryMetrics(ctx context.Context, actor *model.AppUser, input QueryInput) (*MetricsResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	dataSource, cfg, credential, err := s.load(ctx, input.DataSourceID)
	if err != nil {
		return nil, err
	}
	raw, err := s.fetch(ctx, dataSource.ID, cfg, credential, cfg.MetricsPath)
	if err != nil {
		return nil, err
	}
	return &MetricsResult{DataSourceID: dataSource.ID, Metrics: parseStubStatus(raw)}, nil
}

func (s *Service) QueryUpstreamStatus(ctx context.Context, actor *model.AppUser, input QueryInput) (*UpstreamStatusResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	dataSource, cfg, credential, err := s.load(ctx, input.DataSourceID)
	if err != nil {
		return nil, err
	}
	raw, err := s.fetch(ctx, dataSource.ID, cfg, credential, cfg.UpstreamStatusPath)
	if err != nil {
		return nil, err
	}
	upstreams, decoded := parseUpstreamStatus(raw)
	return &UpstreamStatusResult{DataSourceID: dataSource.ID, Upstreams: upstreams, Raw: decoded}, nil
}

func (s *Service) QueryConfigMetadata(ctx context.Context, actor *model.AppUser, input QueryInput) (*ConfigMetadataResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	dataSource, cfg, credential, err := s.load(ctx, input.DataSourceID)
	if err != nil {
		return nil, err
	}
	raw, err := s.fetch(ctx, dataSource.ID, cfg, credential, cfg.ConfigMetadataPath)
	if err != nil {
		return nil, err
	}
	return &ConfigMetadataResult{DataSourceID: dataSource.ID, Metadata: sanitizeConfigMetadata(raw)}, nil
}

func (s *Service) load(ctx context.Context, dataSourceID int64) (*model.DataSource, Config, credentialConfig, error) {
	if dataSourceID <= 0 {
		return nil, Config{}, credentialConfig{}, ErrInvalidInput
	}
	dataSource, err := s.repository.FindDataSourceByID(ctx, dataSourceID)
	if err != nil {
		return nil, Config{}, credentialConfig{}, err
	}
	if !dataSource.Enabled {
		return nil, Config{}, credentialConfig{}, ErrDataSourceDisabled
	}
	if dataSource.SourceType != model.DataSourceTypeNginx {
		return nil, Config{}, credentialConfig{}, ErrUnsupportedSource
	}
	if !dataSource.ReadOnly {
		return nil, Config{}, credentialConfig{}, ErrDataSourceNotRead
	}
	cfg, err := parseConfig(dataSource.Config)
	if err != nil {
		return nil, Config{}, credentialConfig{}, err
	}
	credential, err := s.loadCredential(dataSource)
	if err != nil {
		return nil, Config{}, credentialConfig{}, err
	}
	return dataSource, cfg, credential, nil
}

func parseConfig(raw []byte) (Config, error) {
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, ErrInvalidInput
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	parsed, err := url.Parse(cfg.BaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return Config{}, ErrInvalidInput
	}
	cfg.AccessLogPath = defaultPath(cfg.AccessLogPath, "/nginx/access.log")
	cfg.ErrorLogPath = defaultPath(cfg.ErrorLogPath, "/nginx/error.log")
	cfg.MetricsPath = defaultPath(cfg.MetricsPath, "/nginx_status")
	cfg.UpstreamStatusPath = defaultPath(cfg.UpstreamStatusPath, "/nginx/upstreams")
	cfg.ConfigMetadataPath = defaultPath(cfg.ConfigMetadataPath, "/nginx/config/metadata")
	return cfg, nil
}

func (cfg Config) timeout() time.Duration {
	if cfg.QueryTimeoutSec <= 0 {
		return defaultTimeout
	}
	timeout := time.Duration(cfg.QueryTimeoutSec) * time.Second
	if timeout > maxTimeout {
		return maxTimeout
	}
	return timeout
}

func (s *Service) loadCredential(dataSource *model.DataSource) (credentialConfig, error) {
	if dataSource.Credential == nil || dataSource.Credential.EncryptedPayload == "" || s.secrets == nil {
		return credentialConfig{}, nil
	}
	plaintext, err := s.secrets.Decrypt(dataSource.Credential.EncryptedPayload)
	if err != nil {
		return credentialConfig{}, fmt.Errorf("decrypt nginx credential: %w", err)
	}
	var credential credentialConfig
	if err := json.Unmarshal([]byte(plaintext), &credential); err != nil {
		return credentialConfig{}, ErrInvalidInput
	}
	return credential, nil
}

func (s *Service) fetch(ctx context.Context, dataSourceID int64, cfg Config, credential credentialConfig, path string) (string, error) {
	release, err := s.limiter.Acquire(ctx, fmt.Sprintf("nginx:%d", dataSourceID))
	if err != nil {
		if errors.Is(err, resourcelimit.ErrLimitExceeded) {
			return "", ErrDataSourceLimited
		}
		return "", err
	}
	defer release()
	queryContext, cancel := context.WithTimeout(ctx, cfg.timeout())
	defer cancel()
	request, err := http.NewRequestWithContext(queryContext, http.MethodGet, cfg.BaseURL+path, nil)
	if err != nil {
		return "", err
	}
	applyCredential(request, credential)
	response, err := internalhttp.WithInsecureTLS(s.client, cfg.InsecureSkipTLS).Do(request)
	if err != nil {
		if errors.Is(queryContext.Err(), context.DeadlineExceeded) {
			return "", ErrNginxTimeout
		}
		return "", err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes))
	if err != nil {
		return "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("nginx returned status %d", response.StatusCode)
	}
	return string(body), nil
}

func applyCredential(request *http.Request, credential credentialConfig) {
	if strings.TrimSpace(credential.BearerToken) != "" {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(credential.BearerToken))
		return
	}
	if credential.Username != "" || credential.Password != "" {
		request.SetBasicAuth(credential.Username, credential.Password)
	}
}

func normalizeLimit(requested, configured int) int {
	limit := requested
	if limit <= 0 {
		limit = configured
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return limit
}

var combinedLogPattern = regexp.MustCompile(`^(\S+) \S+ \S+ \[[^\]]+\] "(\S+) ([^"]*) HTTP/[^"]+" (\d{3}) (\d+|-) "([^"]*)" "([^"]*)"(.*)$`)

func parseAccessLogs(raw string, maskIP bool, limit int) []AccessLogRecord {
	lines := nonEmptyLines(raw, limit)
	records := make([]AccessLogRecord, 0, len(lines))
	for _, line := range lines {
		record := AccessLogRecord{Raw: sanitizeFreeText(line)}
		matches := combinedLogPattern.FindStringSubmatch(line)
		if len(matches) == 9 {
			record.ClientIP = sanitizeIP(matches[1], maskIP)
			record.Method = matches[2]
			record.Path = sanitizeURL(matches[3])
			record.Status, _ = strconv.Atoi(matches[4])
			record.Bytes, _ = strconv.ParseInt(zeroDash(matches[5]), 10, 64)
			record.Referer = sanitizeURL(matches[6])
			record.UserAgent = sanitizeFreeText(matches[7])
			record.Attributes = map[string]string{"extra": sanitizeFreeText(strings.TrimSpace(matches[8]))}
		}
		records = append(records, record)
	}
	return records
}

var errorLogPattern = regexp.MustCompile(`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}) \[([a-z]+)\] .*?: (.*)$`)
var clientIPPattern = regexp.MustCompile(`client: ([^,\s]+)`)

func parseErrorLogs(raw string, maskIP bool, limit int) []ErrorLogRecord {
	lines := nonEmptyLines(raw, limit)
	records := make([]ErrorLogRecord, 0, len(lines))
	for _, line := range lines {
		record := ErrorLogRecord{Raw: sanitizeFreeText(line), Message: sanitizeFreeText(line)}
		if matches := errorLogPattern.FindStringSubmatch(line); len(matches) == 4 {
			record.Time = matches[1]
			record.Level = matches[2]
			record.Message = sanitizeFreeText(matches[3])
		}
		if matches := clientIPPattern.FindStringSubmatch(line); len(matches) == 2 {
			record.ClientIP = sanitizeIP(matches[1], maskIP)
		}
		records = append(records, record)
	}
	return records
}

func parseStubStatus(raw string) map[string]string {
	result := map[string]string{}
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Active connections:") {
			result["active_connections"] = strings.TrimSpace(strings.TrimPrefix(line, "Active connections:"))
		}
		if strings.HasPrefix(line, "Reading:") {
			fields := strings.Fields(line)
			for i := 0; i+1 < len(fields); i += 2 {
				result[strings.ToLower(strings.TrimSuffix(fields[i], ":"))] = fields[i+1]
			}
		}
	}
	return result
}

func parseUpstreamStatus(raw string) ([]map[string]string, map[string]interface{}) {
	var decoded map[string]interface{}
	if json.Unmarshal([]byte(raw), &decoded) != nil {
		return nil, nil
	}
	upstreams := make([]map[string]string, 0)
	for key, value := range decoded {
		if strings.Contains(strings.ToLower(key), "upstream") {
			if rows, ok := value.([]interface{}); ok {
				for _, row := range rows {
					if mapped, ok := row.(map[string]interface{}); ok {
						upstreams = append(upstreams, sanitizeMap(mapped))
					}
				}
			}
		}
	}
	return upstreams, decoded
}

func sanitizeConfigMetadata(raw string) map[string]string {
	var decoded map[string]interface{}
	if json.Unmarshal([]byte(raw), &decoded) == nil {
		return sanitizeMap(decoded)
	}
	result := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || isPrivateKeyLine(key) || isPrivateKeyLine(value) {
			continue
		}
		result[strings.TrimSpace(key)] = sanitizeFreeText(value)
	}
	return result
}

func sanitizeMap(raw map[string]interface{}) map[string]string {
	result := map[string]string{}
	for key, value := range raw {
		if isPrivateKeyLine(key) || isPrivateKeyLine(fmt.Sprint(value)) {
			continue
		}
		result[key] = sanitizeFreeText(fmt.Sprint(value))
	}
	return result
}

func isPrivateKeyLine(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "private_key") ||
		strings.Contains(lower, "ssl_certificate_key") ||
		strings.Contains(lower, "begin private key") ||
		strings.Contains(lower, "rsa private key")
}

func sanitizeURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "-" {
		return value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return sanitizeFreeText(value)
	}
	query := parsed.Query()
	for key := range query {
		if isSensitiveKey(key) {
			query.Set(key, "***")
		}
	}
	parsed.RawQuery = query.Encode()
	return sanitizeFreeText(parsed.String())
}

func sanitizeFreeText(value string) string {
	value = strings.ReplaceAll(value, "\r", "")
	for _, key := range []string{"authorization", "cookie", "token", "password", "passwd", "secret", "api_key", "apikey"} {
		re := regexp.MustCompile(`(?i)(` + key + `=)[^&\s"]+`)
		value = re.ReplaceAllString(value, `${1}***`)
	}
	return strings.TrimSpace(value)
}

func sanitizeIP(value string, mask bool) string {
	value = strings.TrimSpace(value)
	if !mask {
		return value
	}
	ip := net.ParseIP(value)
	if ip == nil {
		return "***"
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		return fmt.Sprintf("%d.%d.%d.0", ipv4[0], ipv4[1], ipv4[2])
	}
	return "***"
}

func isSensitiveKey(key string) bool {
	switch strings.ToLower(key) {
	case "authorization", "cookie", "token", "access_token", "password", "passwd", "secret", "api_key", "apikey":
		return true
	default:
		return false
	}
}

func nonEmptyLines(raw string, limit int) []string {
	lines := make([]string, 0)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
		if len(lines) >= limit {
			break
		}
	}
	return lines
}

func zeroDash(value string) string {
	if value == "-" {
		return "0"
	}
	return value
}

func defaultPath(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	if !strings.HasPrefix(value, "/") {
		return "/" + value
	}
	return value
}
