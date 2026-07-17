package metrics

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	internalhttp "aiops-platform/backend/internal/httpclient"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/observability"
	"aiops-platform/backend/internal/resourcelimit"
)

const (
	defaultTimeout   = 10 * time.Second
	maxTimeout       = 30 * time.Second
	defaultMaxSeries = 20
	maxSeriesLimit   = 100
	defaultMaxPoints = 500
	maxPointsLimit   = 2000
	defaultStep      = 60 * time.Second
	minStep          = 15 * time.Second
	maxRangeWindow   = 7 * 24 * time.Hour
)

var (
	ErrForbidden          = errors.New("metrics access forbidden")
	ErrInvalidInput       = errors.New("invalid input")
	ErrUnsupportedSource  = errors.New("unsupported metrics data source")
	ErrDataSourceDisabled = errors.New("data source disabled")
	ErrMetricsTimeout     = errors.New("metrics query timeout")
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
	DataSourceID int64
	Query        string
	Range        bool
	Start        time.Time
	End          time.Time
	Step         time.Duration
	Timeout      time.Duration
	MaxSeries    int
	MaxPoints    int
}

type QueryResult struct {
	DataSourceID int64          `json:"dataSourceId"`
	Query        string         `json:"query"`
	Range        bool           `json:"range"`
	Series       []MetricSeries `json:"series"`
	Warnings     []string       `json:"warnings,omitempty"`
	Limit        MetricLimit    `json:"limit"`
}

type MetricLimit struct {
	MaxSeries int `json:"maxSeries"`
	MaxPoints int `json:"maxPoints"`
}

type MetricSeries struct {
	Metric map[string]string `json:"metric"`
	Points []MetricPoint     `json:"points"`
}

type MetricPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
	RawValue  string    `json:"rawValue"`
}

type TestResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type promConfig struct {
	BaseURL         string `json:"baseUrl"`
	TimeoutMs       int    `json:"timeoutMs"`
	InsecureSkipTLS bool   `json:"insecureSkipTlsVerify"`
}

type credentialConfig struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	BearerToken string `json:"bearerToken"`
}

type promResponse struct {
	Status    string   `json:"status"`
	Data      promData `json:"data"`
	ErrorType string   `json:"errorType"`
	Error     string   `json:"error"`
	Warnings  []string `json:"warnings"`
}

type promData struct {
	ResultType string          `json:"resultType"`
	Result     json.RawMessage `json:"result"`
}

type promVectorResult struct {
	Metric map[string]string `json:"metric"`
	Value  promSample        `json:"value"`
}

type promMatrixResult struct {
	Metric map[string]string `json:"metric"`
	Values []promSample      `json:"values"`
}

type promSample []json.RawMessage

func NewService(repository Repository, secrets SecretManager, client *http.Client) *Service {
	if client == nil {
		client = &http.Client{Timeout: maxTimeout}
	}
	return &Service{repository: repository, secrets: secrets, client: client, limiter: resourcelimit.NewKeyedLimiter(4)}
}

func (s *Service) SetDataSourceLimiter(limiter *resourcelimit.KeyedLimiter) {
	s.limiter = limiter
}

func (s *Service) Test(ctx context.Context, actor *model.AppUser, dataSourceID int64) (*TestResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	_, err := s.Query(ctx, actor, QueryInput{DataSourceID: dataSourceID, Query: "up", MaxSeries: 1, MaxPoints: 1})
	if err != nil {
		observability.SetDatasourceHealth(model.DataSourceTypePrometheus, dataSourceID, false)
		return nil, err
	}
	observability.SetDatasourceHealth(model.DataSourceTypePrometheus, dataSourceID, true)
	return &TestResult{OK: true, Message: "prometheus query API is readable"}, nil
}

func (s *Service) Query(ctx context.Context, actor *model.AppUser, input QueryInput) (*QueryResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	normalized, err := normalizeInput(input)
	if err != nil {
		return nil, err
	}
	release, err := s.limiter.Acquire(ctx, fmt.Sprintf("metrics:%d", normalized.DataSourceID))
	if err != nil {
		if errors.Is(err, resourcelimit.ErrLimitExceeded) {
			return nil, ErrDataSourceLimited
		}
		return nil, err
	}
	defer release()
	dataSource, config, credential, err := s.load(ctx, normalized.DataSourceID)
	if err != nil {
		return nil, err
	}
	endpoint, err := buildEndpoint(config.BaseURL, normalized)
	if err != nil {
		return nil, err
	}
	queryContext, cancel := context.WithTimeout(ctx, normalized.Timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(queryContext, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create prometheus request: %w", err)
	}
	applyCredential(request, credential)
	response, err := internalhttp.WithInsecureTLS(s.client, config.InsecureSkipTLS).Do(request)
	if err != nil {
		if isTimeout(err) || errors.Is(queryContext.Err(), context.DeadlineExceeded) {
			return nil, ErrMetricsTimeout
		}
		return nil, fmt.Errorf("query prometheus: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 20<<20))
	if err != nil {
		return nil, fmt.Errorf("read prometheus response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("prometheus returned status %d", response.StatusCode)
	}
	series, warnings, err := decodePromResponse(body, normalized.Range)
	if err != nil {
		return nil, err
	}
	series = limitSeries(series, normalized.MaxSeries, normalized.MaxPoints)
	return &QueryResult{
		DataSourceID: dataSource.ID,
		Query:        normalized.Query,
		Range:        normalized.Range,
		Series:       series,
		Warnings:     warnings,
		Limit:        MetricLimit{MaxSeries: normalized.MaxSeries, MaxPoints: normalized.MaxPoints},
	}, nil
}

func (s *Service) load(ctx context.Context, dataSourceID int64) (*model.DataSource, promConfig, credentialConfig, error) {
	if dataSourceID <= 0 {
		return nil, promConfig{}, credentialConfig{}, ErrInvalidInput
	}
	dataSource, err := s.repository.FindDataSourceByID(ctx, dataSourceID)
	if err != nil {
		return nil, promConfig{}, credentialConfig{}, err
	}
	if !dataSource.Enabled {
		return nil, promConfig{}, credentialConfig{}, ErrDataSourceDisabled
	}
	if dataSource.SourceType != model.DataSourceTypePrometheus {
		return nil, promConfig{}, credentialConfig{}, ErrUnsupportedSource
	}
	config, err := parseConfig(dataSource.Config)
	if err != nil {
		return nil, promConfig{}, credentialConfig{}, err
	}
	credential, err := s.loadCredential(dataSource)
	if err != nil {
		return nil, promConfig{}, credentialConfig{}, err
	}
	return dataSource, config, credential, nil
}

func parseConfig(raw []byte) (promConfig, error) {
	var config promConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return promConfig{}, ErrInvalidInput
	}
	config.BaseURL = strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	parsed, err := url.Parse(config.BaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return promConfig{}, ErrInvalidInput
	}
	return config, nil
}

func (s *Service) loadCredential(dataSource *model.DataSource) (credentialConfig, error) {
	if dataSource.Credential == nil || dataSource.Credential.EncryptedPayload == "" || s.secrets == nil {
		return credentialConfig{}, nil
	}
	plaintext, err := s.secrets.Decrypt(dataSource.Credential.EncryptedPayload)
	if err != nil {
		return credentialConfig{}, fmt.Errorf("decrypt prometheus credential: %w", err)
	}
	var credential credentialConfig
	if err := json.Unmarshal([]byte(plaintext), &credential); err != nil {
		return credentialConfig{}, ErrInvalidInput
	}
	return credential, nil
}

func normalizeInput(input QueryInput) (QueryInput, error) {
	input.Query = strings.TrimSpace(input.Query)
	if input.DataSourceID <= 0 || input.Query == "" || !utf8.ValidString(input.Query) {
		return QueryInput{}, ErrInvalidInput
	}
	if input.Range {
		if input.Start.IsZero() || input.End.IsZero() || !input.Start.Before(input.End) || input.End.Sub(input.Start) > maxRangeWindow {
			return QueryInput{}, ErrInvalidInput
		}
		if input.Step <= 0 {
			input.Step = defaultStep
		}
		if input.Step < minStep {
			input.Step = minStep
		}
	} else if input.End.IsZero() {
		input.End = time.Now().UTC()
	}
	if input.Timeout <= 0 {
		input.Timeout = defaultTimeout
	}
	if input.Timeout > maxTimeout {
		input.Timeout = maxTimeout
	}
	if input.MaxSeries <= 0 {
		input.MaxSeries = defaultMaxSeries
	}
	if input.MaxSeries > maxSeriesLimit {
		input.MaxSeries = maxSeriesLimit
	}
	if input.MaxPoints <= 0 {
		input.MaxPoints = defaultMaxPoints
	}
	if input.MaxPoints > maxPointsLimit {
		input.MaxPoints = maxPointsLimit
	}
	return input, nil
}

func buildEndpoint(baseURL string, input QueryInput) (string, error) {
	path := "/api/v1/query"
	values := url.Values{}
	values.Set("query", input.Query)
	values.Set("limit", strconv.Itoa(input.MaxSeries))
	if input.Range {
		path = "/api/v1/query_range"
		values.Set("start", formatPromTime(input.Start))
		values.Set("end", formatPromTime(input.End))
		values.Set("step", formatPromDuration(input.Step))
	} else if !input.End.IsZero() {
		values.Set("time", formatPromTime(input.End))
	}
	return strings.TrimRight(baseURL, "/") + path + "?" + values.Encode(), nil
}

func formatPromTime(value time.Time) string {
	return strconv.FormatFloat(float64(value.UnixNano())/1e9, 'f', 3, 64)
}

func formatPromDuration(value time.Duration) string {
	return strconv.FormatFloat(value.Seconds(), 'f', -1, 64)
}

func decodePromResponse(raw []byte, requestedRange bool) ([]MetricSeries, []string, error) {
	var decoded promResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, nil, ErrInvalidInput
	}
	if decoded.Status != "success" {
		if decoded.Error != "" {
			return nil, nil, fmt.Errorf("prometheus error %s: %s", decoded.ErrorType, decoded.Error)
		}
		return nil, nil, ErrInvalidInput
	}
	switch decoded.Data.ResultType {
	case "vector":
		series, err := decodeVector(decoded.Data.Result)
		return series, decoded.Warnings, err
	case "matrix":
		series, err := decodeMatrix(decoded.Data.Result)
		return series, decoded.Warnings, err
	case "scalar":
		var sample promSample
		if err := json.Unmarshal(decoded.Data.Result, &sample); err != nil {
			return nil, nil, ErrInvalidInput
		}
		point, ok := sampleToPoint(sample)
		if !ok {
			return nil, nil, ErrInvalidInput
		}
		return []MetricSeries{{Metric: map[string]string{"__name__": "scalar"}, Points: []MetricPoint{point}}}, decoded.Warnings, nil
	default:
		if requestedRange {
			return nil, nil, ErrInvalidInput
		}
		return nil, nil, ErrInvalidInput
	}
}

func decodeVector(rawResult json.RawMessage) ([]MetricSeries, error) {
	var rawItems []json.RawMessage
	if err := json.Unmarshal(rawResult, &rawItems); err != nil {
		return nil, ErrInvalidInput
	}
	series := make([]MetricSeries, 0, len(rawItems))
	for _, raw := range rawItems {
		var item promVectorResult
		if err := json.Unmarshal(raw, &item); err != nil {
			continue
		}
		point, ok := sampleToPoint(item.Value)
		if !ok {
			continue
		}
		series = append(series, MetricSeries{Metric: item.Metric, Points: []MetricPoint{point}})
	}
	return series, nil
}

func decodeMatrix(rawResult json.RawMessage) ([]MetricSeries, error) {
	var rawItems []json.RawMessage
	if err := json.Unmarshal(rawResult, &rawItems); err != nil {
		return nil, ErrInvalidInput
	}
	series := make([]MetricSeries, 0, len(rawItems))
	for _, raw := range rawItems {
		var item promMatrixResult
		if err := json.Unmarshal(raw, &item); err != nil {
			continue
		}
		points := make([]MetricPoint, 0, len(item.Values))
		for _, sample := range item.Values {
			point, ok := sampleToPoint(sample)
			if ok {
				points = append(points, point)
			}
		}
		series = append(series, MetricSeries{Metric: item.Metric, Points: points})
	}
	return series, nil
}

func sampleToPoint(sample promSample) (MetricPoint, bool) {
	if len(sample) != 2 {
		return MetricPoint{}, false
	}
	var timestamp float64
	if err := json.Unmarshal(sample[0], &timestamp); err != nil {
		return MetricPoint{}, false
	}
	var rawValue string
	if err := json.Unmarshal(sample[1], &rawValue); err != nil {
		return MetricPoint{}, false
	}
	value, err := strconv.ParseFloat(rawValue, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return MetricPoint{}, false
	}
	seconds := int64(timestamp)
	nanos := int64((timestamp - float64(seconds)) * 1e9)
	return MetricPoint{Timestamp: time.Unix(seconds, nanos).UTC(), Value: value, RawValue: rawValue}, true
}

func limitSeries(series []MetricSeries, maxSeries int, maxPoints int) []MetricSeries {
	if len(series) > maxSeries {
		series = series[:maxSeries]
	}
	for index := range series {
		if len(series[index].Points) > maxPoints {
			series[index].Points = series[index].Points[len(series[index].Points)-maxPoints:]
		}
	}
	return series
}

func applyCredential(request *http.Request, credential credentialConfig) {
	if credential.BearerToken != "" {
		request.Header.Set("Authorization", "Bearer "+credential.BearerToken)
		return
	}
	if credential.Username != "" || credential.Password != "" {
		token := base64.StdEncoding.EncodeToString([]byte(credential.Username + ":" + credential.Password))
		request.Header.Set("Authorization", "Basic "+token)
	}
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
