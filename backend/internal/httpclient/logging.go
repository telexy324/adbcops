package httpclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"aiops-platform/backend/internal/auditutil"
	appmiddleware "aiops-platform/backend/internal/middleware"
)

const maxDataSourceLogBodyBytes = 20 << 20

var sensitiveTextValue = regexp.MustCompile(`(?i)(password|passwd|pwd|token|secret|authorization|api[_-]?key)(\s*[:=]\s*)([^\s,;]+)`)

type DataSourceLogOptions struct {
	SourceType   string
	DataSourceID int64
	Logger       *slog.Logger
}

// DoWithDebugLog records a sanitized outbound exchange without changing the
// request or response body observed by the caller.
func DoWithDebugLog(client Doer, request *http.Request, options DataSourceLogOptions) (*http.Response, error) {
	if client == nil {
		client = http.DefaultClient
	}
	return doWithDebugLog(request, options, client.Do)
}

func NewDebugLoggingRoundTripper(next http.RoundTripper, options DataSourceLogOptions) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	return roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return doWithDebugLog(request, options, next.RoundTrip)
	})
}

func doWithDebugLog(request *http.Request, options DataSourceLogOptions, execute func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	ctx := context.Background()
	if request != nil {
		ctx = request.Context()
	}
	if request == nil || !logger.Enabled(ctx, slog.LevelDebug) {
		return execute(request)
	}

	requestBody, requestTruncated, err := captureBody(&request.Body)
	if err != nil {
		logger.DebugContext(ctx, "data source outbound request body unavailable",
			baseLogAttrs(ctx, options, request)...,
		)
	}
	logger.DebugContext(ctx, "data source outbound request",
		append(baseLogAttrs(ctx, options, request),
			"headers", sanitizedHeaders(request.Header),
			"request_body", sanitizedBody(requestBody),
			"body_truncated", requestTruncated,
		)...,
	)

	startedAt := time.Now()
	response, executeErr := execute(request)
	if executeErr != nil {
		logger.DebugContext(ctx, "data source outbound request failed",
			append(baseLogAttrs(ctx, options, request),
				"latency_ms", time.Since(startedAt).Milliseconds(),
				"error", sanitizeText(executeErr.Error()),
			)...,
		)
		return response, executeErr
	}

	responseBody, responseTruncated, readErr := captureBody(&response.Body)
	attrs := append(baseLogAttrs(ctx, options, request),
		"status", response.StatusCode,
		"latency_ms", time.Since(startedAt).Milliseconds(),
		"headers", sanitizedHeaders(response.Header),
		"response_body", sanitizedBody(responseBody),
		"body_truncated", responseTruncated,
	)
	if readErr != nil {
		attrs = append(attrs, "body_read_error", sanitizeText(readErr.Error()))
	}
	logger.DebugContext(ctx, "data source outbound response", attrs...)
	return response, nil
}

func baseLogAttrs(ctx context.Context, options DataSourceLogOptions, request *http.Request) []any {
	method, endpoint := "", ""
	if request != nil {
		method = request.Method
		if request.URL != nil {
			endpoint = sanitizedURL(request.URL)
		}
	}
	return []any{
		"request_id", appmiddleware.GetRequestIDFromContext(ctx),
		"source_type", options.SourceType,
		"data_source_id", options.DataSourceID,
		"method", method,
		"endpoint", endpoint,
	}
}

func captureBody(body *io.ReadCloser) ([]byte, bool, error) {
	if body == nil || *body == nil {
		return nil, false, nil
	}
	original := *body
	captured, err := io.ReadAll(io.LimitReader(original, maxDataSourceLogBodyBytes+1))
	if err != nil {
		*body = &combinedReadCloser{Reader: io.MultiReader(bytes.NewReader(captured), original), Closer: original}
		return captured, false, err
	}
	truncated := len(captured) > maxDataSourceLogBodyBytes
	logged := captured
	if truncated {
		logged = captured[:maxDataSourceLogBodyBytes]
	}
	*body = &combinedReadCloser{Reader: io.MultiReader(bytes.NewReader(captured), original), Closer: original}
	return logged, truncated, nil
}

func sanitizedHeaders(headers http.Header) map[string][]string {
	result := make(map[string][]string, len(headers))
	for key, values := range headers {
		if auditutil.IsSensitiveKey(key) {
			result[key] = []string{auditutil.RedactedValue}
			continue
		}
		result[key] = append([]string(nil), values...)
	}
	return result
}

func sanitizedURL(value *url.URL) string {
	if value == nil {
		return ""
	}
	copyValue := *value
	query := copyValue.Query()
	for key := range query {
		if auditutil.IsSensitiveKey(key) {
			query.Set(key, auditutil.RedactedValue)
		}
	}
	copyValue.RawQuery = query.Encode()
	copyValue.User = nil
	return copyValue.String()
}

func sanitizedBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	if sanitized := auditutil.SanitizeJSON(body, 0); len(sanitized) > 0 {
		return string(sanitized)
	}
	return sanitizeText(string(body))
}

func sanitizeText(value string) string {
	return sensitiveTextValue.ReplaceAllString(value, "$1$2"+auditutil.RedactedValue)
}

type combinedReadCloser struct {
	io.Reader
	io.Closer
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	if fn == nil {
		return nil, fmt.Errorf("nil round tripper")
	}
	return fn(request)
}
