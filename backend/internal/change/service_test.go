package change

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

func TestQueryRecentDefaultsToTwoHourWindow(t *testing.T) {
	now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	client := &fakeHTTPClient{responses: map[string]fakeHTTPResponse{
		"/release": {status: 200, body: `{"items":[{"id":"rel-1","title":"release","deployedAt":"2026-07-12T09:30:00Z"}]}`},
	}}
	repository := newChangeRepository()
	repository.dataSource.Config = []byte(`{"baseUrl":"https://changes.example","recentReleasePath":"/release"}`)
	service := NewService(repository, nil, client)
	service.now = func() time.Time { return now }

	result, err := service.QueryRecent(context.Background(), &model.AppUser{ID: 1}, QueryInput{DataSourceID: 1})
	if err != nil {
		t.Fatalf("query recent changes: %v", err)
	}
	if !result.From.Equal(now.Add(-2*time.Hour)) || !result.To.Equal(now) {
		t.Fatalf("expected default 2h window, got %s - %s", result.From, result.To)
	}
	if result.Count != 1 || result.Partial {
		t.Fatalf("unexpected result: %+v", result)
	}
	if client.requests[0].URL.Query().Get("from") != now.Add(-2*time.Hour).Format(time.RFC3339) {
		t.Fatalf("from query not set: %s", client.requests[0].URL.RawQuery)
	}
}

func TestQueryRecentSourceFailureDoesNotBlockOtherSources(t *testing.T) {
	client := &fakeHTTPClient{responses: map[string]fakeHTTPResponse{
		"/release": {status: 200, body: `[{"id":"rel-1","title":"release","deployedAt":"2026-07-12T09:30:00Z"}]`},
		"/config":  {status: 500, body: `error`},
		"/git":     {status: 200, body: `[{"id":"c1","title":"commit","committedAt":"2026-07-12T09:40:00Z"}]`},
	}}
	service := NewService(newChangeRepository(), nil, client)
	from := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)

	result, err := service.QueryRecent(context.Background(), &model.AppUser{ID: 1}, QueryInput{DataSourceID: 1, From: &from, To: &to})
	if err != nil {
		t.Fatalf("query recent changes: %v", err)
	}
	if !result.Partial {
		t.Fatalf("expected partial result for one failed source: %+v", result)
	}
	if result.Count != 2 {
		t.Fatalf("expected successful release and git changes, got %+v", result.Items)
	}
	if len(result.Sources) != 3 || result.Sources[1].OK || result.Sources[1].Error == "" {
		t.Fatalf("expected config source failure status, got %+v", result.Sources)
	}
}

func newChangeRepository() *memoryChangeRepository {
	return &memoryChangeRepository{dataSource: model.DataSource{
		ID:         1,
		Name:       "changes",
		SourceType: model.DataSourceTypeHTTP,
		Config:     []byte(`{"baseUrl":"https://changes.example","recentReleasePath":"/release","configChangePath":"/config","gitChangePath":"/git"}`),
		Enabled:    true,
		ReadOnly:   true,
	}}
}

type memoryChangeRepository struct {
	dataSource model.DataSource
}

func (r *memoryChangeRepository) FindDataSourceByID(_ context.Context, id int64) (*model.DataSource, error) {
	if id != r.dataSource.ID {
		return nil, repository.ErrNotFound
	}
	copied := r.dataSource
	return &copied, nil
}

type fakeHTTPResponse struct {
	status int
	body   string
}

type fakeHTTPClient struct {
	responses map[string]fakeHTTPResponse
	requests  []*http.Request
}

func (c *fakeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	c.requests = append(c.requests, req)
	response, ok := c.responses[req.URL.Path]
	if !ok {
		response = fakeHTTPResponse{status: 404, body: "not found"}
	}
	return &http.Response{
		StatusCode: response.status,
		Body:       io.NopCloser(strings.NewReader(response.body)),
		Header:     http.Header{},
	}, nil
}
