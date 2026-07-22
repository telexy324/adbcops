package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

const readinessProbePath = "/api/ready"

// Logger writes one structured log entry after each HTTP request.
func Logger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
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

func requestPath(c *gin.Context) string {
	if fullPath := c.FullPath(); fullPath != "" {
		return fullPath
	}
	return c.Request.URL.Path
}
