package tidb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/resourcelimit"
)

const (
	defaultDriver      = "mysql"
	defaultTimeout     = 8 * time.Second
	maxTimeout         = 30 * time.Second
	defaultRowLimit    = 100
	maxRowLimit        = 1000
	defaultColumnLimit = 80
	defaultByteLimit   = 1 << 20
	maxByteLimit       = 8 << 20
)

var (
	ErrForbidden          = errors.New("tidb access forbidden")
	ErrInvalidInput       = errors.New("invalid input")
	ErrUnsupportedSource  = errors.New("unsupported tidb data source")
	ErrDataSourceDisabled = errors.New("data source disabled")
	ErrDataSourceNotRead  = errors.New("tidb data source must be read-only")
	ErrUnsafeSQL          = errors.New("unsafe sql")
	ErrTiDBTimeout        = errors.New("tidb query timeout")
	ErrDataSourceLimited  = errors.New("tidb data source concurrency limit exceeded")
)

type Repository interface {
	FindDataSourceByID(ctx context.Context, id int64) (*model.DataSource, error)
}

type SecretManager interface {
	Decrypt(value string) (string, error)
}

type SQLExecutor interface {
	Query(ctx context.Context, driverName string, dsn string, query string, args ...any) ([]map[string]any, error)
}

type Service struct {
	repository Repository
	secrets    SecretManager
	executor   SQLExecutor
	limiter    *resourcelimit.KeyedLimiter
}

type Config struct {
	DriverName            string `json:"driverName"`
	DSN                   string `json:"dsn"`
	Environment           string `json:"environment"`
	ExplainAnalyzeEnabled bool   `json:"explainAnalyzeEnabled"`
	QueryTimeoutSeconds   int    `json:"queryTimeoutSeconds"`
	RowLimit              int    `json:"rowLimit"`
	ColumnLimit           int    `json:"columnLimit"`
	ByteLimit             int    `json:"byteLimit"`
}

type Credential struct {
	DSN      string `json:"dsn"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type QueryInput struct {
	DataSourceID int64
	Limit        int
}

type SlowQueryInput struct {
	DataSourceID int64
	Minutes      int
	Limit        int
}

type ExplainInput struct {
	DataSourceID int64
	SQL          string
	Analyze      bool
}

type QueryResult struct {
	DataSourceID int64               `json:"dataSourceId"`
	Rows         []map[string]string `json:"rows"`
	Truncated    bool                `json:"truncated"`
}

type ExplainResult struct {
	DataSourceID int64               `json:"dataSourceId"`
	SQL          string              `json:"sql"`
	Analyze      bool                `json:"analyze"`
	Rows         []map[string]string `json:"rows"`
}

type DBExecutor struct{}

func NewService(repository Repository, secrets SecretManager, executor SQLExecutor) *Service {
	if executor == nil {
		executor = DBExecutor{}
	}
	return &Service{
		repository: repository,
		secrets:    secrets,
		executor:   executor,
		limiter:    resourcelimit.NewKeyedLimiter(4),
	}
}

func (s *Service) SetDataSourceLimiter(limiter *resourcelimit.KeyedLimiter) {
	s.limiter = limiter
}

func (s *Service) Test(ctx context.Context, actor *model.AppUser, dataSourceID int64) error {
	_, err := s.query(ctx, actor, dataSourceID, "SELECT 1 AS ok", nil, defaultRowLimit)
	return err
}

func (s *Service) QueryClusterStatus(ctx context.Context, actor *model.AppUser, input QueryInput) (*QueryResult, error) {
	return s.query(ctx, actor, input.DataSourceID, "SELECT type, instance, status_address, version, git_hash FROM information_schema.cluster_info LIMIT ?", nil, input.Limit)
}

func (s *Service) QueryProcessList(ctx context.Context, actor *model.AppUser, input QueryInput) (*QueryResult, error) {
	return s.query(ctx, actor, input.DataSourceID, "SELECT id, user, host, db, command, time, state, info FROM information_schema.processlist ORDER BY time DESC LIMIT ?", nil, input.Limit)
}

func (s *Service) QuerySlowQueries(ctx context.Context, actor *model.AppUser, input SlowQueryInput) (*QueryResult, error) {
	if input.Minutes <= 0 || input.Minutes > 24*60 {
		input.Minutes = 60
	}
	// Rank digests by accumulated database time rather than one unusually long
	// sample. This makes the first row the highest-impact fingerprint in the
	// requested window while keeping the original SQL sanitized at the service
	// boundary.
	return s.query(ctx, actor, input.DataSourceID, `
SELECT
	digest,
	ANY_VALUE(query) AS query,
	COUNT(*) AS execution_count,
	SUM(query_time) AS total_query_time,
	MAX(query_time) AS max_query_time,
	SUM(process_time) AS total_process_time,
	SUM(wait_time) AS total_wait_time,
	MAX(memory_max) AS memory_max,
	MIN(time) AS first_seen_at,
	MAX(time) AS last_seen_at
FROM information_schema.cluster_slow_query
WHERE time >= NOW() - INTERVAL ? MINUTE
GROUP BY digest
ORDER BY total_query_time DESC, execution_count DESC
LIMIT ?`, []any{input.Minutes}, input.Limit)
}

func (s *Service) QueryLockWaits(ctx context.Context, actor *model.AppUser, input QueryInput) (*QueryResult, error) {
	return s.query(ctx, actor, input.DataSourceID, "SELECT * FROM information_schema.data_lock_waits LIMIT ?", nil, input.Limit)
}

func (s *Service) QueryStatisticsHealth(ctx context.Context, actor *model.AppUser, input QueryInput) (*QueryResult, error) {
	return s.query(ctx, actor, input.DataSourceID, "SELECT db_name, table_name, partition_name, healthy, modify_count, count FROM information_schema.tables WHERE table_schema NOT IN ('mysql','information_schema','performance_schema') ORDER BY healthy ASC LIMIT ?", nil, input.Limit)
}

func (s *Service) QueryHotRegions(ctx context.Context, actor *model.AppUser, input QueryInput) (*QueryResult, error) {
	return s.query(ctx, actor, input.DataSourceID, "SELECT * FROM information_schema.tikv_region_status ORDER BY written_bytes DESC LIMIT ?", nil, input.Limit)
}

func (s *Service) Explain(ctx context.Context, actor *model.AppUser, input ExplainInput) (*ExplainResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	normalizedSQL, err := normalizeReadonlySQL(input.SQL)
	if err != nil {
		return nil, err
	}
	dataSource, cfg, dsn, err := s.load(ctx, input.DataSourceID)
	if err != nil {
		return nil, err
	}
	if input.Analyze && !cfg.ExplainAnalyzeEnabled {
		return nil, ErrUnsafeSQL
	}
	prefix := "EXPLAIN FORMAT='brief' "
	if input.Analyze {
		prefix = "EXPLAIN ANALYZE "
	}
	query := prefix + normalizedSQL
	rows, _, err := s.execute(ctx, dataSource.ID, cfg, dsn, query)
	if err != nil {
		return nil, err
	}
	return &ExplainResult{DataSourceID: dataSource.ID, SQL: normalizedSQL, Analyze: input.Analyze, Rows: rows}, nil
}

func (s *Service) query(ctx context.Context, actor *model.AppUser, dataSourceID int64, statement string, args []any, requestedLimit int) (*QueryResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	dataSource, cfg, dsn, err := s.load(ctx, dataSourceID)
	if err != nil {
		return nil, err
	}
	limit := normalizeLimit(requestedLimit, cfg.RowLimit)
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, limit)
	rows, truncated, err := s.execute(ctx, dataSource.ID, cfg, dsn, statement, queryArgs...)
	if err != nil {
		return nil, err
	}
	return &QueryResult{DataSourceID: dataSource.ID, Rows: rows, Truncated: truncated}, nil
}

func (s *Service) execute(ctx context.Context, dataSourceID int64, cfg Config, dsn string, query string, args ...any) ([]map[string]string, bool, error) {
	release, err := s.limiter.Acquire(ctx, fmt.Sprintf("tidb:%d", dataSourceID))
	if err != nil {
		if errors.Is(err, resourcelimit.ErrLimitExceeded) {
			return nil, false, ErrDataSourceLimited
		}
		return nil, false, err
	}
	defer release()
	queryContext, cancel := context.WithTimeout(ctx, cfg.timeout())
	defer cancel()
	rawRows, err := s.executor.Query(queryContext, cfg.DriverName, dsn, query, args...)
	if err != nil {
		if errors.Is(queryContext.Err(), context.DeadlineExceeded) {
			return nil, false, ErrTiDBTimeout
		}
		return nil, false, err
	}
	return sanitizeRows(rawRows, cfg)
}

func (s *Service) load(ctx context.Context, dataSourceID int64) (*model.DataSource, Config, string, error) {
	if dataSourceID <= 0 {
		return nil, Config{}, "", ErrInvalidInput
	}
	dataSource, err := s.repository.FindDataSourceByID(ctx, dataSourceID)
	if err != nil {
		return nil, Config{}, "", err
	}
	if !dataSource.Enabled {
		return nil, Config{}, "", ErrDataSourceDisabled
	}
	if dataSource.SourceType != model.DataSourceTypeTiDB {
		return nil, Config{}, "", ErrUnsupportedSource
	}
	if !dataSource.ReadOnly {
		return nil, Config{}, "", ErrDataSourceNotRead
	}
	cfg, err := parseConfig(dataSource.Config)
	if err != nil {
		return nil, Config{}, "", err
	}
	credential, err := s.loadCredential(dataSource)
	if err != nil {
		return nil, Config{}, "", err
	}
	dsn := cfg.DSN
	if strings.TrimSpace(credential.DSN) != "" {
		dsn = strings.TrimSpace(credential.DSN)
	}
	if strings.TrimSpace(dsn) == "" {
		return nil, Config{}, "", ErrInvalidInput
	}
	return dataSource, cfg, dsn, nil
}

func parseConfig(raw []byte) (Config, error) {
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, ErrInvalidInput
	}
	cfg.DriverName = strings.TrimSpace(cfg.DriverName)
	if cfg.DriverName == "" {
		cfg.DriverName = defaultDriver
	}
	cfg.DSN = strings.TrimSpace(cfg.DSN)
	cfg.Environment = strings.ToLower(strings.TrimSpace(cfg.Environment))
	if cfg.Environment == "prod" || cfg.Environment == "production" {
		cfg.ExplainAnalyzeEnabled = false
	}
	if cfg.ColumnLimit <= 0 || cfg.ColumnLimit > defaultColumnLimit {
		cfg.ColumnLimit = defaultColumnLimit
	}
	if cfg.ByteLimit <= 0 {
		cfg.ByteLimit = defaultByteLimit
	}
	if cfg.ByteLimit > maxByteLimit {
		cfg.ByteLimit = maxByteLimit
	}
	return cfg, nil
}

func (cfg Config) timeout() time.Duration {
	if cfg.QueryTimeoutSeconds <= 0 {
		return defaultTimeout
	}
	timeout := time.Duration(cfg.QueryTimeoutSeconds) * time.Second
	if timeout > maxTimeout {
		return maxTimeout
	}
	return timeout
}

func (s *Service) loadCredential(dataSource *model.DataSource) (Credential, error) {
	if dataSource.Credential == nil || dataSource.Credential.EncryptedPayload == "" || s.secrets == nil {
		return Credential{}, nil
	}
	plaintext, err := s.secrets.Decrypt(dataSource.Credential.EncryptedPayload)
	if err != nil {
		return Credential{}, fmt.Errorf("decrypt tidb credential: %w", err)
	}
	var credential Credential
	if err := json.Unmarshal([]byte(plaintext), &credential); err != nil {
		return Credential{}, ErrInvalidInput
	}
	return credential, nil
}

func (DBExecutor) Query(ctx context.Context, driverName string, dsn string, query string, args ...any) ([]map[string]any, error) {
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		row := map[string]any{}
		for i, column := range columns {
			row[column] = values[i]
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func normalizeReadonlySQL(input string) (string, error) {
	sqlText := strings.TrimSpace(input)
	if sqlText == "" {
		return "", ErrInvalidInput
	}
	if strings.Contains(sqlText, "/*") || strings.Contains(sqlText, "--") || strings.Contains(sqlText, "#") {
		return "", ErrUnsafeSQL
	}
	sqlText = strings.TrimSuffix(sqlText, ";")
	if strings.Contains(sqlText, ";") {
		return "", ErrUnsafeSQL
	}
	upper := strings.ToUpper(strings.TrimSpace(sqlText))
	if strings.HasPrefix(upper, "EXPLAIN") {
		return "", ErrUnsafeSQL
	}
	if !(strings.HasPrefix(upper, "SELECT ") || strings.HasPrefix(upper, "SHOW ")) {
		return "", ErrUnsafeSQL
	}
	dangerous := []string{
		" INSERT ", " UPDATE ", " DELETE ", " DROP ", " ALTER ", " CREATE ", " TRUNCATE ", " REPLACE ",
		" INTO OUTFILE", " INTO DUMPFILE", " LOAD_FILE", " SLEEP(", " BENCHMARK(", " SYSTEM(",
	}
	padded := " " + upper + " "
	for _, token := range dangerous {
		if strings.Contains(padded, token) {
			return "", ErrUnsafeSQL
		}
	}
	return sqlText, nil
}

// NormalizeReadonlySQL exposes the same single-statement guard used by
// controlled EXPLAIN so RCA planning cannot schedule SQL that the TiDB tool
// would later reject.
func NormalizeReadonlySQL(input string) (string, error) {
	return normalizeReadonlySQL(input)
}

func normalizeLimit(requested, configured int) int {
	limit := requested
	if limit <= 0 {
		limit = configured
	}
	if limit <= 0 {
		limit = defaultRowLimit
	}
	if limit > maxRowLimit {
		limit = maxRowLimit
	}
	return limit
}

func sanitizeRows(rows []map[string]any, cfg Config) ([]map[string]string, bool, error) {
	result := make([]map[string]string, 0, len(rows))
	totalBytes := 0
	truncated := false
	for _, rawRow := range rows {
		row := map[string]string{}
		columnCount := 0
		for key, value := range rawRow {
			if columnCount >= cfg.ColumnLimit {
				truncated = true
				break
			}
			columnCount++
			safeValue := sanitizeValue(key, fmt.Sprint(value))
			totalBytes += len(key) + len(safeValue)
			if totalBytes > cfg.ByteLimit {
				truncated = true
				return result, truncated, nil
			}
			row[key] = safeValue
		}
		result = append(result, row)
	}
	return result, truncated, nil
}

var sensitiveColumnPattern = regexp.MustCompile(`(?i)(password|passwd|pwd|secret|token|authorization|cookie|credential|api[_-]?key|user(?:name)?|account|email|phone|mobile|id[_-]?card|ssn)`)

func sanitizeValue(column, value string) string {
	if sensitiveColumnPattern.MatchString(column) || sensitiveColumnPattern.MatchString(value) {
		return "***"
	}
	upperColumn := strings.ToUpper(column)
	if upperColumn == "INFO" || upperColumn == "QUERY" || upperColumn == "SQL" {
		return SanitizeSQLForEvidence(value)
	}
	return value
}

var (
	sqlBlockCommentPattern = regexp.MustCompile(`(?s)/\*.*?\*/`)
	sqlLineCommentPattern  = regexp.MustCompile(`(?m)(--[^\r\n]*|#[^\r\n]*)`)
	sqlQuotedPattern       = regexp.MustCompile(`(?s)('(?:''|\\.|[^'])*'|"(?:""|\\.|[^"])*")`)
	sqlHexPattern          = regexp.MustCompile(`(?i)\b(?:0x[0-9a-f]+|x'[0-9a-f]+')\b`)
	sqlNumberPattern       = regexp.MustCompile(`\b\d+(?:\.\d+)?\b`)
	sqlWhitespacePattern   = regexp.MustCompile(`\s+`)
	sqlPlaceholderPattern  = regexp.MustCompile(`(?:\?\s*,\s*)+\?`)
)

// SanitizeSQLForEvidence keeps enough SQL structure for a reviewer to identify
// the statement shape while removing literals, comments, credentials, tokens,
// account names embedded as values, and other user data.
func SanitizeSQLForEvidence(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	trimmed = sqlBlockCommentPattern.ReplaceAllString(trimmed, " ")
	trimmed = sqlLineCommentPattern.ReplaceAllString(trimmed, " ")
	trimmed = sqlHexPattern.ReplaceAllString(trimmed, "?")
	trimmed = sqlQuotedPattern.ReplaceAllString(trimmed, "?")
	trimmed = sqlNumberPattern.ReplaceAllString(trimmed, "?")
	trimmed = sqlPlaceholderPattern.ReplaceAllString(trimmed, "?")
	trimmed = strings.TrimSpace(sqlWhitespacePattern.ReplaceAllString(trimmed, " "))
	if trimmed == "" {
		return ""
	}
	const maxEvidenceSQLBytes = 2048
	if len(trimmed) > maxEvidenceSQLBytes {
		trimmed = trimmed[:maxEvidenceSQLBytes] + "…"
	}
	return trimmed
}

// SQLFingerprint is deterministic across literal-only changes. A native TiDB
// digest should be preferred when present; this fallback exists for explicitly
// supplied read-only SQL and for future database providers.
func SQLFingerprint(value string) string {
	sanitized := strings.ToLower(SanitizeSQLForEvidence(value))
	if sanitized == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(sanitized))
	return "sha256:" + fmt.Sprintf("%x", sum[:12])
}

// SlowQueryImpactScore is intentionally simple and deterministic. Total
// database time is primary, then execution count and maximum latency provide
// bounded tie-breaking signals.
func SlowQueryImpactScore(totalQueryTime string, executionCount string, maxQueryTime string) float64 {
	total, _ := strconv.ParseFloat(strings.TrimSpace(totalQueryTime), 64)
	count, _ := strconv.ParseFloat(strings.TrimSpace(executionCount), 64)
	maximum, _ := strconv.ParseFloat(strings.TrimSpace(maxQueryTime), 64)
	if total < 0 {
		total = 0
	}
	if count < 0 {
		count = 0
	}
	if maximum < 0 {
		maximum = 0
	}
	return total + count*0.01 + maximum*0.001
}
