package nginx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"aiops-platform/backend/internal/model"
)

func TestAccessLogSanitizesSensitiveQueryAndMasksIP(t *testing.T) {
	client := &fakeHTTPClient{responses: map[string]string{
		"/nginx/access.log": `10.1.2.3 - - [13/Jul/2026:10:00:00 +0800] "GET /api/orders?token=secret&safe=1 HTTP/1.1" 499 12 "https://example.test/?password=pw" "curl/8.0"`,
	}}
	service := NewService(nginxRepository{dataSource: nginxDataSource(t, Config{BaseURL: "https://nginx.example.test", MaskClientIP: true})}, nil, client)

	result, err := service.QueryAccessLogs(context.Background(), &model.AppUser{ID: 1}, QueryInput{DataSourceID: 1})
	if err != nil {
		t.Fatalf("QueryAccessLogs() error = %v", err)
	}
	item := result.Items[0]
	if item.ClientIP != "10.1.2.0" {
		t.Fatalf("client ip was not masked: %+v", item)
	}
	if strings.Contains(item.Path, "secret") || strings.Contains(item.Referer, "pw") {
		t.Fatalf("sensitive query leaked: %+v", item)
	}
	if item.Status != 499 {
		t.Fatalf("status not parsed: %+v", item)
	}
}

func TestErrorLogSanitizesCookieAndMasksClient(t *testing.T) {
	client := &fakeHTTPClient{responses: map[string]string{
		"/nginx/error.log": `2026/07/13 10:00:00 [error] 12#12: *1 upstream timed out, client: 10.2.3.4, server: example, request: "GET / HTTP/1.1", header: "Cookie=session=secret"`,
	}}
	service := NewService(nginxRepository{dataSource: nginxDataSource(t, Config{BaseURL: "https://nginx.example.test", MaskClientIP: true})}, nil, client)

	result, err := service.QueryErrorLogs(context.Background(), &model.AppUser{ID: 1}, QueryInput{DataSourceID: 1})
	if err != nil {
		t.Fatalf("QueryErrorLogs() error = %v", err)
	}
	item := result.Items[0]
	if item.ClientIP != "10.2.3.0" || strings.Contains(item.Message, "session=secret") {
		t.Fatalf("error log was not sanitized: %+v", item)
	}
}

func TestMetricsAndUpstreamStatusParse(t *testing.T) {
	client := &fakeHTTPClient{responses: map[string]string{
		"/nginx_status":    "Active connections: 12\nserver accepts handled requests\n 1 1 10\nReading: 1 Writing: 2 Waiting: 9\n",
		"/nginx/upstreams": `{"upstreams":[{"name":"api","state":"up","server":"10.0.0.1:8080"}]}`,
	}}
	service := NewService(nginxRepository{dataSource: nginxDataSource(t, Config{BaseURL: "https://nginx.example.test"})}, nil, client)

	metrics, err := service.QueryMetrics(context.Background(), &model.AppUser{ID: 1}, QueryInput{DataSourceID: 1})
	if err != nil {
		t.Fatalf("QueryMetrics() error = %v", err)
	}
	if metrics.Metrics["active_connections"] != "12" || metrics.Metrics["reading"] != "1" {
		t.Fatalf("unexpected metrics: %+v", metrics.Metrics)
	}

	upstreams, err := service.QueryUpstreamStatus(context.Background(), &model.AppUser{ID: 1}, QueryInput{DataSourceID: 1})
	if err != nil {
		t.Fatalf("QueryUpstreamStatus() error = %v", err)
	}
	if len(upstreams.Upstreams) != 1 || upstreams.Upstreams[0]["state"] != "up" {
		t.Fatalf("unexpected upstream status: %+v", upstreams)
	}
}

func TestConfigMetadataDropsPrivateKey(t *testing.T) {
	client := &fakeHTTPClient{responses: map[string]string{
		"/nginx/config/metadata": `{"version":"1.25","ssl_certificate_key":"/etc/nginx/private.key","private_key":"-----BEGIN PRIVATE KEY-----"}`,
	}}
	service := NewService(nginxRepository{dataSource: nginxDataSource(t, Config{BaseURL: "https://nginx.example.test"})}, nil, client)

	result, err := service.QueryConfigMetadata(context.Background(), &model.AppUser{ID: 1}, QueryInput{DataSourceID: 1})
	if err != nil {
		t.Fatalf("QueryConfigMetadata() error = %v", err)
	}
	if result.Metadata["version"] != "1.25" || result.Metadata["ssl_certificate_key"] != "" || result.Metadata["private_key"] != "" {
		t.Fatalf("private key metadata leaked: %+v", result.Metadata)
	}
}

type nginxRepository struct {
	dataSource *model.DataSource
}

func (r nginxRepository) FindDataSourceByID(_ context.Context, id int64) (*model.DataSource, error) {
	if r.dataSource != nil && r.dataSource.ID == id {
		return r.dataSource, nil
	}
	return nil, errors.New("not found")
}

type fakeHTTPClient struct {
	responses map[string]string
}

func (c *fakeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	body, ok := c.responses[req.URL.Path]
	if !ok {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found"))}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
}

func nginxDataSource(t *testing.T, cfg Config) *model.DataSource {
	t.Helper()
	rawConfig, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return &model.DataSource{
		ID:         1,
		Name:       "nginx-prod",
		SourceType: model.DataSourceTypeNginx,
		Config:     rawConfig,
		Enabled:    true,
		ReadOnly:   true,
	}
}
