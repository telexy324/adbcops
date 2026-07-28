package tidb

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"aiops-platform/backend/internal/model"
)

func TestExplainRejectsUnsafeSQL(t *testing.T) {
	service := NewService(tidbRepository{dataSource: tidbDataSource(t, Config{DSN: "readonly@tcp(tidb:4000)/test"})}, nil, &fakeExecutor{})
	actor := &model.AppUser{ID: 1}

	unsafeSQL := []string{
		"update orders set status='x'",
		"select * from a; select * from b",
		"select sleep(10)",
		"select * from users -- bypass",
		"explain select * from users",
	}
	for _, statement := range unsafeSQL {
		_, err := service.Explain(context.Background(), actor, ExplainInput{DataSourceID: 1, SQL: statement})
		if !errors.Is(err, ErrUnsafeSQL) {
			t.Fatalf("expected ErrUnsafeSQL for %q, got %v", statement, err)
		}
	}
}

func TestExplainUsesControlledPrefixAndRejectsAnalyzeInProduction(t *testing.T) {
	executor := &fakeExecutor{rows: []map[string]any{{"id": "TableFullScan", "task": "cop[tikv]"}}}
	service := NewService(tidbRepository{dataSource: tidbDataSource(t, Config{DSN: "readonly@tcp(tidb:4000)/test", Environment: "production", ExplainAnalyzeEnabled: true})}, nil, executor)
	actor := &model.AppUser{ID: 1}

	result, err := service.Explain(context.Background(), actor, ExplainInput{DataSourceID: 1, SQL: "select * from orders where id = 1;"})
	if err != nil {
		t.Fatalf("Explain() error = %v", err)
	}
	if !strings.HasPrefix(executor.queries[0], "EXPLAIN FORMAT='brief' select * from orders") {
		t.Fatalf("unexpected explain query: %s", executor.queries[0])
	}
	if result.SQL != "select * from orders where id = 1" || result.Rows[0]["id"] != "TableFullScan" {
		t.Fatalf("unexpected explain result: %+v", result)
	}

	_, err = service.Explain(context.Background(), actor, ExplainInput{DataSourceID: 1, SQL: "select * from orders", Analyze: true})
	if !errors.Is(err, ErrUnsafeSQL) {
		t.Fatalf("expected production EXPLAIN ANALYZE rejection, got %v", err)
	}
}

func TestProcessListSanitizesSensitiveColumnsAndSQLText(t *testing.T) {
	executor := &fakeExecutor{rows: []map[string]any{
		{
			"id":        10,
			"user":      "readonly",
			"host":      "10.0.0.1",
			"command":   "Query",
			"info":      "select * from user where token='secret-token'",
			"api_token": "secret-token",
		},
	}}
	service := NewService(tidbRepository{dataSource: tidbDataSource(t, Config{DSN: "readonly@tcp(tidb:4000)/test"})}, nil, executor)

	result, err := service.QueryProcessList(context.Background(), &model.AppUser{ID: 1}, QueryInput{DataSourceID: 1, Limit: 50})
	if err != nil {
		t.Fatalf("QueryProcessList() error = %v", err)
	}
	row := result.Rows[0]
	if row["api_token"] != "***" || row["info"] != "***" || row["user"] != "***" {
		t.Fatalf("row was not sanitized: %+v", row)
	}
	if got := executor.args[0][0]; got != 50 {
		t.Fatalf("unexpected limit arg: %v", got)
	}
}

func TestSanitizeValueMasksDatabaseAccountsAndPersonalInformation(t *testing.T) {
	for _, testCase := range []struct {
		column string
		value  string
	}{
		{column: "USER", value: "application_writer"},
		{column: "account_name", value: "customer-42"},
		{column: "email", value: "alice@example.com"},
		{column: "mobile", value: "13800138000"},
	} {
		if got := sanitizeValue(testCase.column, testCase.value); got != "***" {
			t.Fatalf("sanitizeValue(%q, %q) = %q, want masked", testCase.column, testCase.value, got)
		}
	}
}

func TestSlowQueriesRankFingerprintsByAccumulatedImpactAndSanitizeSQL(t *testing.T) {
	executor := &fakeExecutor{rows: []map[string]any{{
		"digest":             "tidb-digest-1",
		"query":              "select * from customer where email='alice@example.com' and id=42",
		"execution_count":    "120",
		"total_query_time":   "84.5",
		"max_query_time":     "2.7",
		"total_wait_time":    "12.3",
		"total_process_time": "70.0",
	}}}
	service := NewService(tidbRepository{dataSource: tidbDataSource(t, Config{DSN: "readonly@tcp(tidb:4000)/test"})}, nil, executor)

	result, err := service.QuerySlowQueries(context.Background(), &model.AppUser{ID: 1}, SlowQueryInput{
		DataSourceID: 1, Minutes: 30, Limit: 20,
	})
	if err != nil {
		t.Fatalf("QuerySlowQueries() error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0]["digest"] != "tidb-digest-1" {
		t.Fatalf("unexpected slow-query result: %+v", result)
	}
	if strings.Contains(result.Rows[0]["query"], "alice@example.com") || strings.Contains(result.Rows[0]["query"], "42") {
		t.Fatalf("SQL literals were not sanitized: %+v", result.Rows[0])
	}
	query := executor.queries[0]
	if !strings.Contains(query, "SUM(query_time) AS total_query_time") ||
		!strings.Contains(query, "ORDER BY total_query_time DESC, execution_count DESC") {
		t.Fatalf("slow queries were not ranked by accumulated impact: %s", query)
	}
	if executor.args[0][0] != 30 || executor.args[0][1] != 20 {
		t.Fatalf("unexpected slow-query bounds: %+v", executor.args[0])
	}
}

func TestSQLFingerprintIgnoresLiteralsAndReadonlyGuardRejectsWrites(t *testing.T) {
	left := SQLFingerprint("SELECT * FROM orders WHERE customer_id=42 AND email='a@example.com'")
	right := SQLFingerprint("select * from orders where customer_id=7 and email='b@example.com'")
	if left == "" || left != right {
		t.Fatalf("literal-only changes should share a fingerprint: %q != %q", left, right)
	}
	sanitized := SanitizeSQLForEvidence("SELECT * FROM orders WHERE customer_id=42 AND email='a@example.com'")
	if strings.Contains(sanitized, "42") || strings.Contains(sanitized, "a@example.com") {
		t.Fatalf("sanitized SQL leaked literals: %s", sanitized)
	}
	for _, statement := range []string{
		"insert into orders(id) values (1)",
		"delete from orders",
		"begin",
		"call rebuild_stats()",
	} {
		if _, err := NormalizeReadonlySQL(statement); !errors.Is(err, ErrUnsafeSQL) {
			t.Fatalf("expected readonly guard to reject %q, got %v", statement, err)
		}
	}
}

func TestCredentialDSNOverridesConfigDSN(t *testing.T) {
	credential := base64.StdEncoding.EncodeToString([]byte(`{"dsn":"readonly:secret@tcp(tidb-secret:4000)/test"}`))
	executor := &fakeExecutor{rows: []map[string]any{{"ok": 1}}}
	service := NewService(tidbRepository{dataSource: tidbDataSourceWithCredential(t, Config{DSN: "readonly@tcp(tidb:4000)/test"}, credential)}, tidbSecrets{}, executor)

	if err := service.Test(context.Background(), &model.AppUser{ID: 1}, 1); err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	if executor.dsns[0] != "readonly:secret@tcp(tidb-secret:4000)/test" {
		t.Fatalf("credential dsn did not override config dsn: %q", executor.dsns[0])
	}
}

type tidbRepository struct {
	dataSource *model.DataSource
}

func (r tidbRepository) FindDataSourceByID(_ context.Context, id int64) (*model.DataSource, error) {
	if r.dataSource != nil && r.dataSource.ID == id {
		return r.dataSource, nil
	}
	return nil, errors.New("not found")
}

type tidbSecrets struct{}

func (tidbSecrets) Decrypt(value string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

type fakeExecutor struct {
	rows    []map[string]any
	queries []string
	args    [][]any
	dsns    []string
}

func (f *fakeExecutor) Query(_ context.Context, _ string, dsn string, query string, args ...any) ([]map[string]any, error) {
	f.dsns = append(f.dsns, dsn)
	f.queries = append(f.queries, query)
	f.args = append(f.args, args)
	return f.rows, nil
}

func tidbDataSource(t *testing.T, cfg Config) *model.DataSource {
	t.Helper()
	return tidbDataSourceWithCredential(t, cfg, "")
}

func tidbDataSourceWithCredential(t *testing.T, cfg Config, credential string) *model.DataSource {
	t.Helper()
	rawConfig, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	dataSource := &model.DataSource{
		ID:         1,
		Name:       "tidb-prod",
		SourceType: model.DataSourceTypeTiDB,
		Config:     rawConfig,
		Enabled:    true,
		ReadOnly:   true,
	}
	if credential != "" {
		credentialID := int64(30)
		dataSource.CredentialID = &credentialID
		dataSource.Credential = &model.CredentialSecret{ID: credentialID, EncryptedPayload: credential}
	}
	return dataSource
}
