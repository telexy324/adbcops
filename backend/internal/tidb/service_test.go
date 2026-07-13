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
	if row["api_token"] != "***" || row["info"] != "***" {
		t.Fatalf("row was not sanitized: %+v", row)
	}
	if got := executor.args[0][0]; got != 50 {
		t.Fatalf("unexpected limit arg: %v", got)
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
