package middleware

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"aiops-platform/backend/internal/auditutil"
	"github.com/gin-gonic/gin"
)

const readinessProbePath = "/api/ready"
const maxHTTPDebugBodyBytes = 64 << 10

// Logger writes one structured log entry after each HTTP request.
func Logger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
		debugEnabled := logger.Enabled(c.Request.Context(), slog.LevelDebug)
		requestBody, requestTruncated := captureJSONRequestBody(c.Request, debugEnabled)
		var responseCapture *debugResponseWriter
		if debugEnabled {
			responseCapture = &debugResponseWriter{ResponseWriter: c.Writer}
			c.Writer = responseCapture
		}
		c.Next()
		if c.Request.URL.Path == readinessProbePath {
			return
		}

		attrs := []any{
			"request_id", GetRequestID(c),
			"method", c.Request.Method,
			"path", requestPath(c),
			"status", c.Writer.Status(),
			"latency_ms", time.Since(startedAt).Milliseconds(),
			"client_ip", c.ClientIP(),
			"error_count", len(c.Errors),
		}
		if debugEnabled {
			logger.DebugContext(c.Request.Context(), "http request exchange",
				"request_id", GetRequestID(c),
				"method", c.Request.Method,
				"path", requestPath(c),
				"query", sanitizedQuery(c.Request),
				"request_body", sanitizedDebugBody(requestBody),
				"request_body_truncated", requestTruncated,
				"status", c.Writer.Status(),
				"response_body", sanitizedDebugBody(responseCapture.Bytes()),
				"response_body_truncated", responseCapture.truncated,
			)
		}
		if len(c.Errors) > 0 {
			messages := make([]string, 0, len(c.Errors))
			for _, item := range c.Errors {
				if item == nil || item.Err == nil {
					continue
				}
				messages = append(messages, item.Err.Error())
			}
			attrs = append(attrs, "errors", messages)
			logger.ErrorContext(c.Request.Context(), "http request failed", attrs...)
			return
		}
		logger.InfoContext(c.Request.Context(), "http request completed", attrs...)
	}
}

type debugResponseWriter struct {
	gin.ResponseWriter
	buffer    bytes.Buffer
	truncated bool
}

func (w *debugResponseWriter) Write(data []byte) (int, error) {
	w.capture(data)
	return w.ResponseWriter.Write(data)
}

func (w *debugResponseWriter) WriteString(value string) (int, error) {
	w.capture([]byte(value))
	return w.ResponseWriter.WriteString(value)
}

func (w *debugResponseWriter) capture(data []byte) {
	remaining := maxHTTPDebugBodyBytes - w.buffer.Len()
	if remaining <= 0 {
		w.truncated = w.truncated || len(data) > 0
		return
	}
	if len(data) > remaining {
		_, _ = w.buffer.Write(data[:remaining])
		w.truncated = true
		return
	}
	_, _ = w.buffer.Write(data)
}

func (w *debugResponseWriter) Bytes() []byte { return w.buffer.Bytes() }

func captureJSONRequestBody(request *http.Request, enabled bool) ([]byte, bool) {
	if !enabled || request == nil || request.Body == nil || !strings.Contains(strings.ToLower(request.Header.Get("Content-Type")), "json") {
		return nil, false
	}
	captured, err := io.ReadAll(io.LimitReader(request.Body, maxHTTPDebugBodyBytes+1))
	if err != nil {
		request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(captured), request.Body))
		return captured, false
	}
	request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(captured), request.Body))
	if len(captured) > maxHTTPDebugBodyBytes {
		return captured[:maxHTTPDebugBodyBytes], true
	}
	return captured, false
}

func sanitizedDebugBody(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	if sanitized := auditutil.SanitizeJSON(raw, maxHTTPDebugBodyBytes); len(sanitized) > 0 {
		return string(sanitized)
	}
	return "[NON_JSON_BODY_OMITTED]"
}

func sanitizedQuery(request *http.Request) map[string][]string {
	result := map[string][]string{}
	if request == nil || request.URL == nil {
		return result
	}
	for key, values := range request.URL.Query() {
		if auditutil.IsSensitiveKey(key) {
			result[key] = []string{auditutil.RedactedValue}
			continue
		}
		result[key] = append([]string(nil), values...)
	}
	return result
}

func requestPath(c *gin.Context) string {
	if fullPath := c.FullPath(); fullPath != "" {
		return fullPath
	}
	return c.Request.URL.Path
}
