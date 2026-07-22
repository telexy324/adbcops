package httpclient

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	appmiddleware "aiops-platform/backend/internal/middleware"
)

func TestDoWithDebugLogRecordsBodiesAndRedactsCredentials(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))
	request, err := http.NewRequestWithContext(
		appmiddleware.ContextWithRequestID(context.Background(), "req-datasource-log"),
		http.MethodPost,
		"https://example.test/_search?token=query-secret",
		strings.NewReader(`{"query":"sentinel","password":"request-secret"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer header-secret")
	client := doerFunc(func(received *http.Request) (*http.Response, error) {
		body, readErr := io.ReadAll(received.Body)
		if readErr != nil || !strings.Contains(string(body), "sentinel") {
			t.Fatalf("request body was not preserved: body=%q err=%v", body, readErr)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Set-Cookie": []string{"session=response-secret"}},
			Body:       io.NopCloser(strings.NewReader(`{"result":"ok","apiKey":"response-secret"}`)),
		}, nil
	})

	response, err := DoWithDebugLog(client, request, DataSourceLogOptions{SourceType: "elasticsearch", DataSourceID: 9, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil || !strings.Contains(string(body), `"result":"ok"`) {
		t.Fatalf("response body was not preserved: body=%q err=%v", body, err)
	}
	logs := output.String()
	for _, expected := range []string{"data source outbound request", "data source outbound response", "req-datasource-log", "elasticsearch", "sentinel", "[REDACTED]"} {
		if !strings.Contains(logs, expected) {
			t.Fatalf("log missing %q: %s", expected, logs)
		}
	}
	for _, secret := range []string{"query-secret", "request-secret", "header-secret", "response-secret"} {
		if strings.Contains(logs, secret) {
			t.Fatalf("log leaked %q: %s", secret, logs)
		}
	}
}

type doerFunc func(*http.Request) (*http.Response, error)

func (fn doerFunc) Do(request *http.Request) (*http.Response, error) { return fn(request) }
