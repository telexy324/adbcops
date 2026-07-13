package skillframework

import (
	"context"
	"encoding/json"
	"testing"

	"aiops-platform/backend/internal/model"
	nginxsvc "aiops-platform/backend/internal/nginx"
)

func TestNginxConfigMetadataSkillDoesNotReturnPrivateKey(t *testing.T) {
	fake := &fakeNginxQuerier{
		config: &nginxsvc.ConfigMetadataResult{DataSourceID: 1, Metadata: map[string]string{"version": "1.25"}},
	}
	skill := nginxSkillByName(t, fake, "query_nginx_config_metadata")
	output, err := skill.Execute(ContextWithActor(context.Background(), adminActor()), json.RawMessage(`{"dataSourceId":1}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if jsonContainsString(output, "private_key") || jsonContainsString(output, "ssl_certificate_key") {
		t.Fatalf("nginx config metadata leaked private key: %s", string(output))
	}
}

func TestNginx504DiagnosisCitesEvidenceAndApprovalRule(t *testing.T) {
	fake := &fakeNginxQuerier{
		access:    &nginxsvc.AccessLogResult{DataSourceID: 1, Items: []nginxsvc.AccessLogRecord{{Status: 504, Path: "/api/orders"}}},
		errors:    &nginxsvc.ErrorLogResult{DataSourceID: 1, Items: []nginxsvc.ErrorLogRecord{{Level: "error", Message: "upstream timed out"}}},
		upstreams: &nginxsvc.UpstreamStatusResult{DataSourceID: 1, Upstreams: []map[string]string{{"name": "api", "state": "up"}}},
		metrics:   &nginxsvc.MetricsResult{DataSourceID: 1, Metrics: map[string]string{"active_connections": "10"}},
	}
	skill := nginxSkillByName(t, fake, "diagnose_nginx_504")
	output, err := skill.Execute(ContextWithActor(context.Background(), adminActor()), json.RawMessage(`{"dataSourceId":1}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, expected := range []string{"query_nginx_access_logs", "query_nginx_error_logs", "query_nginx_upstreams", "high"} {
		if !jsonContainsString(output, expected) {
			t.Fatalf("diagnosis missing %s: %s", expected, string(output))
		}
	}
}

func TestNginxSkillWithoutServiceReturnsPartial(t *testing.T) {
	skill := nginxSkillByName(t, nil, "query_nginx_metrics")
	output, err := skill.Execute(ContextWithActor(context.Background(), adminActor()), json.RawMessage(`{"dataSourceId":1}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if decoded["partial"] != true {
		t.Fatalf("expected partial output: %s", string(output))
	}
}

func nginxSkillByName(t *testing.T, nginx NginxQuerier, name string) Skill {
	t.Helper()
	for _, skill := range NginxSkills(nginx) {
		if skill.Definition().Name == name {
			return skill
		}
	}
	t.Fatalf("nginx skill %s not found", name)
	return nil
}

type fakeNginxQuerier struct {
	access    *nginxsvc.AccessLogResult
	errors    *nginxsvc.ErrorLogResult
	metrics   *nginxsvc.MetricsResult
	upstreams *nginxsvc.UpstreamStatusResult
	config    *nginxsvc.ConfigMetadataResult
}

func (f *fakeNginxQuerier) QueryAccessLogs(context.Context, *model.AppUser, nginxsvc.QueryInput) (*nginxsvc.AccessLogResult, error) {
	if f.access != nil {
		return f.access, nil
	}
	return &nginxsvc.AccessLogResult{}, nil
}

func (f *fakeNginxQuerier) QueryErrorLogs(context.Context, *model.AppUser, nginxsvc.QueryInput) (*nginxsvc.ErrorLogResult, error) {
	if f.errors != nil {
		return f.errors, nil
	}
	return &nginxsvc.ErrorLogResult{}, nil
}

func (f *fakeNginxQuerier) QueryMetrics(context.Context, *model.AppUser, nginxsvc.QueryInput) (*nginxsvc.MetricsResult, error) {
	if f.metrics != nil {
		return f.metrics, nil
	}
	return &nginxsvc.MetricsResult{}, nil
}

func (f *fakeNginxQuerier) QueryUpstreamStatus(context.Context, *model.AppUser, nginxsvc.QueryInput) (*nginxsvc.UpstreamStatusResult, error) {
	if f.upstreams != nil {
		return f.upstreams, nil
	}
	return &nginxsvc.UpstreamStatusResult{}, nil
}

func (f *fakeNginxQuerier) QueryConfigMetadata(context.Context, *model.AppUser, nginxsvc.QueryInput) (*nginxsvc.ConfigMetadataResult, error) {
	if f.config != nil {
		return f.config, nil
	}
	return &nginxsvc.ConfigMetadataResult{}, nil
}
