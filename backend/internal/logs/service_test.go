package logs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

func TestQueryRejectsLargeTimeRangeByDefault(t *testing.T) {
	service := NewService(newFakeRepository(nil), &fakeSecrets{}, nil)
	_, err := service.Query(context.Background(), &model.AppUser{ID: 1, Role: model.RoleUser}, QueryInput{
		DataSourceID: 1,
		From:         time.Now().Add(-25 * time.Hour),
		To:           time.Now(),
	})
	if err != ErrTimeRangeTooLarge {
		t.Fatalf("Query() error = %v, want ErrTimeRangeTooLarge", err)
	}
}

func TestQueryTimeoutIsRecognizable(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, timeoutError{}
	})}
	service := NewService(newFakeRepository(ptrDataSource("https://es.example")), &fakeSecrets{}, client)
	_, err := service.Query(context.Background(), &model.AppUser{ID: 1, Role: model.RoleUser}, QueryInput{
		DataSourceID: 1,
		From:         time.Now().Add(-time.Hour),
		To:           time.Now(),
		Timeout:      10 * time.Millisecond,
	})
	if err != ErrLogQueryTimeout {
		t.Fatalf("Query() error = %v, want ErrLogQueryTimeout", err)
	}
}

func TestQueryReturnsUnifiedLogItems(t *testing.T) {
	var capturedAuth string
	var capturedPath string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		capturedAuth = r.Header.Get("Authorization")
		capturedPath = r.URL.Path
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
			"timed_out": false,
			"hits": {
				"total": {"value": 1},
				"hits": [{
					"_source": {
						"@timestamp": "2026-07-11T08:00:00Z",
						"level": "ERROR",
						"message": "database pool exhausted",
						"host": "node-a",
						"namespace": "prod",
						"pod": "payment-0",
						"traceId": "trace-1"
					}
				}]
			}
		}`)),
		}, nil
	})}
	service := NewService(newFakeRepository(ptrDataSource("https://es.example")), &fakeSecrets{}, client)
	result, err := service.Query(context.Background(), &model.AppUser{ID: 1, Role: model.RoleUser}, QueryInput{
		DataSourceID: 1,
		From:         time.Date(2026, 7, 11, 7, 0, 0, 0, time.UTC),
		To:           time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC),
		Keyword:      "database",
		Level:        "ERROR",
		Size:         10,
	})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("result = %+v", result)
	}
	item := result.Items[0]
	if item.Level != "ERROR" || item.Message != "database pool exhausted" || item.Pod != "payment-0" || item.TraceID != "trace-1" || item.Raw == "" {
		t.Fatalf("log item = %+v", item)
	}
	if capturedAuth == "" || !strings.HasPrefix(capturedAuth, "Basic ") {
		t.Fatalf("Authorization header = %q, want Basic auth", capturedAuth)
	}
	if !strings.HasSuffix(capturedPath, "/logs-*/_search") && !strings.HasSuffix(capturedPath, "/logs-%2A/_search") {
		t.Fatalf("unexpected path: %s", capturedPath)
	}
}

type fakeRepository struct {
	dataSource *model.DataSource
}

func newFakeRepository(dataSource *model.DataSource) *fakeRepository {
	if dataSource == nil {
		dataSource = ptrDataSource("https://es.example")
	}
	return &fakeRepository{dataSource: dataSource}
}

func (f *fakeRepository) FindDataSourceByID(_ context.Context, id int64) (*model.DataSource, error) {
	if id != f.dataSource.ID {
		return nil, repository.ErrNotFound
	}
	return f.dataSource, nil
}

func ptrDataSource(baseURL string) *model.DataSource {
	config, _ := json.Marshal(map[string]any{
		"baseUrl": baseURL,
		"index":   "logs-*",
	})
	credentialID := int64(9)
	return &model.DataSource{
		ID:           1,
		Name:         "prod-es",
		SourceType:   model.DataSourceTypeElasticsearch,
		Config:       config,
		CredentialID: &credentialID,
		Credential: &model.CredentialSecret{
			ID:               credentialID,
			EncryptedPayload: "encrypted:" + base64.RawURLEncoding.EncodeToString([]byte(`{"username":"elastic","password":"secret"}`)),
		},
		Enabled:  true,
		ReadOnly: true,
	}
}

type fakeSecrets struct{}

func (f *fakeSecrets) Decrypt(value string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "encrypted:"))
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type timeoutError struct{}

func (timeoutError) Error() string {
	return "timeout"
}

func (timeoutError) Timeout() bool {
	return true
}

func (timeoutError) Temporary() bool {
	return true
}

var _ interface {
	error
	Timeout() bool
	Temporary() bool
} = timeoutError{}
