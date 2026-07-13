package redis

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"aiops-platform/backend/internal/model"
)

func TestRejectsNonWhitelistCommand(t *testing.T) {
	service := NewService(redisRepository{dataSource: redisDataSource(t, Config{Mode: ModeStandalone, Endpoints: []string{"redis-a:6379"}})}, nil, &fakeRunner{})

	_, err := service.RunAllowedCommand(context.Background(), &model.AppUser{ID: 1}, 1, Command{Name: "GET", Args: []string{"business:key"}})
	if !errors.Is(err, ErrCommandNotAllowed) {
		t.Fatalf("expected ErrCommandNotAllowed, got %v", err)
	}

	_, err = service.RunAllowedCommand(context.Background(), &model.AppUser{ID: 1}, 1, Command{Name: "CONFIG", Args: []string{"GET", "*"}})
	if !errors.Is(err, ErrCommandNotAllowed) {
		t.Fatalf("expected ErrCommandNotAllowed for CONFIG, got %v", err)
	}
}

func TestClientSummaryMasksSensitiveFields(t *testing.T) {
	runner := &fakeRunner{replies: map[string]Reply{
		"redis-a:6379|CLIENT LIST": "id=1 addr=10.0.0.1:5000 laddr=127.0.0.1:6379 name=app user=svc db=0 cmd=get\nid=2 addr=10.0.0.2:5000 name=batch user=default db=1 cmd=set\n",
	}}
	service := NewService(redisRepository{dataSource: redisDataSource(t, Config{Mode: ModeStandalone, Endpoints: []string{"redis-a:6379"}})}, nil, runner)

	result, err := service.ClientListSummary(context.Background(), &model.AppUser{ID: 1}, QueryInput{DataSourceID: 1})
	if err != nil {
		t.Fatalf("ClientListSummary() error = %v", err)
	}
	fields := result.Summary[0].Fields[0]
	if fields["addr"] != "***" || fields["name"] != "***" || fields["user"] != "***" {
		t.Fatalf("client sensitive fields were not redacted: %+v", fields)
	}
	if result.Summary[0].ByCmd["get"] != 1 || result.Summary[0].ByDB["0"] != 1 {
		t.Fatalf("unexpected client summary: %+v", result.Summary[0])
	}
}

func TestSlowLogRedactsCommandArguments(t *testing.T) {
	runner := &fakeRunner{replies: map[string]Reply{
		"redis-a:6379|SLOWLOG GET": []Reply{
			[]Reply{int64(1), int64(1780000000), int64(3000), []Reply{"SET", "order:1", "secret-value"}},
		},
	}}
	service := NewService(redisRepository{dataSource: redisDataSource(t, Config{Mode: ModeStandalone, Endpoints: []string{"redis-a:6379"}})}, nil, runner)

	result, err := service.SlowLog(context.Background(), &model.AppUser{ID: 1}, SlowLogInput{DataSourceID: 1, Limit: 1})
	if err != nil {
		t.Fatalf("SlowLog() error = %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].Command != "SET [args redacted]" {
		t.Fatalf("unexpected slowlog result: %+v", result.Items)
	}
	if strings.Contains(result.Items[0].Command, "secret-value") || strings.Contains(result.Items[0].Command, "order:1") {
		t.Fatalf("slowlog leaked key/value: %+v", result.Items[0])
	}
}

func TestScanSummaryLimitsAndDoesNotReturnKeys(t *testing.T) {
	runner := &fakeRunner{replies: map[string]Reply{
		"redis-a:6379|SCAN 0": []Reply{"5", []Reply{"order:1", "order:2", "user:1"}},
	}}
	service := NewService(redisRepository{dataSource: redisDataSource(t, Config{Mode: ModeStandalone, Endpoints: []string{"redis-a:6379"}, MaxScanIterations: 10, MaxScanKeys: 2})}, nil, runner)

	result, err := service.ScanSummary(context.Background(), &model.AppUser{ID: 1}, ScanInput{DataSourceID: 1, Count: 100})
	if err != nil {
		t.Fatalf("ScanSummary() error = %v", err)
	}
	if !result.Truncated || result.ScannedKeys != 2 {
		t.Fatalf("expected scan to truncate at key limit, got %+v", result)
	}
	if result.PrefixHistogram["order:*"] != 2 {
		t.Fatalf("unexpected prefix histogram: %+v", result.PrefixHistogram)
	}
}

func TestClusterSingleNodeFailureIsPartial(t *testing.T) {
	runner := &fakeRunner{replies: map[string]Reply{
		"redis-a:6379|CLUSTER INFO":  "cluster_state:ok\ncluster_slots_assigned:16384\n",
		"redis-a:6379|CLUSTER NODES": "node-a 10.0.0.1:6379@16379 master - 0 0 1 connected 0-8191\n",
	}, errors: map[string]error{
		"redis-b:6379|CLUSTER INFO": errors.New("connection refused"),
	}}
	service := NewService(redisRepository{dataSource: redisDataSource(t, Config{Mode: ModeCluster, Endpoints: []string{"redis-a:6379", "redis-b:6379"}})}, nil, runner)

	result, err := service.ClusterState(context.Background(), &model.AppUser{ID: 1}, QueryInput{DataSourceID: 1})
	if err != nil {
		t.Fatalf("ClusterState() error = %v", err)
	}
	if !result.Partial || len(result.NodeSummary["redis-a:6379"]) != 1 {
		t.Fatalf("unexpected cluster result: %+v", result)
	}
}

func TestCredentialIsDecryptedForRunner(t *testing.T) {
	credential := base64.StdEncoding.EncodeToString([]byte(`{"username":"readonly","password":"secret","db":2}`))
	runner := &fakeRunner{replies: map[string]Reply{"redis-a:6379|PING": "PONG"}}
	service := NewService(redisRepository{dataSource: redisDataSourceWithCredential(t, Config{Mode: ModeStandalone, Endpoints: []string{"redis-a:6379"}}, credential)}, redisSecrets{}, runner)

	if err := service.Test(context.Background(), &model.AppUser{ID: 1}, 1); err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	if runner.credentials[0].Username != "readonly" || runner.credentials[0].Password != "secret" || runner.credentials[0].DB != 2 {
		t.Fatalf("credential not passed to runner: %+v", runner.credentials[0])
	}
}

type redisRepository struct {
	dataSource *model.DataSource
}

func (r redisRepository) FindDataSourceByID(_ context.Context, id int64) (*model.DataSource, error) {
	if r.dataSource != nil && r.dataSource.ID == id {
		return r.dataSource, nil
	}
	return nil, errors.New("not found")
}

type redisSecrets struct{}

func (redisSecrets) Decrypt(value string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

type fakeRunner struct {
	replies     map[string]Reply
	errors      map[string]error
	commands    []Command
	endpoints   []string
	credentials []Credential
}

func (r *fakeRunner) Run(_ context.Context, endpoint string, credential Credential, command Command) (Reply, error) {
	r.commands = append(r.commands, command)
	r.endpoints = append(r.endpoints, endpoint)
	r.credentials = append(r.credentials, credential)
	key := endpoint + "|" + strings.ToUpper(command.Name)
	if len(command.Args) > 0 {
		key += " " + strings.ToUpper(command.Args[0])
	}
	if r.errors != nil && r.errors[key] != nil {
		return nil, r.errors[key]
	}
	if r.replies != nil {
		if reply, ok := r.replies[key]; ok {
			return reply, nil
		}
	}
	return "OK", nil
}

func redisDataSource(t *testing.T, cfg Config) *model.DataSource {
	t.Helper()
	return redisDataSourceWithCredential(t, cfg, "")
}

func redisDataSourceWithCredential(t *testing.T, cfg Config, credential string) *model.DataSource {
	t.Helper()
	rawConfig, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	dataSource := &model.DataSource{
		ID:         1,
		Name:       "redis-prod",
		SourceType: model.DataSourceTypeRedis,
		Config:     rawConfig,
		Enabled:    true,
		ReadOnly:   true,
	}
	if credential != "" {
		credentialID := int64(20)
		dataSource.CredentialID = &credentialID
		dataSource.Credential = &model.CredentialSecret{ID: credentialID, EncryptedPayload: credential}
	}
	return dataSource
}
