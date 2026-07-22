package middleware

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"aiops-platform/backend/internal/auditutil"
	"aiops-platform/backend/internal/model"
	"github.com/gin-gonic/gin"
)

type AuditRecorder interface {
	CreateAuditLog(ctx context.Context, log *model.AuditLog) error
}

func Audit(recorder AuditRecorder, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
		c.Next()

		if recorder == nil || c.FullPath() == "" || c.Request.URL.Path == readinessProbePath {
			return
		}
		requestID := GetRequestID(c)
		if requestID == "" {
			requestID = GetRequestIDFromContext(c.Request.Context())
		}
		userID, username := auditUser(c)
		metadata, _ := json.Marshal(map[string]any{
			"query":      sanitizeValues(c.Request.URL.Query()),
			"params":     sanitizeParams(c.Params),
			"full_path":  c.FullPath(),
			"error_text": sanitizedErrorText(c.Errors.String()),
		})
		entry := &model.AuditLog{
			RequestID:  requestID,
			UserID:     userID,
			Username:   username,
			Method:     c.Request.Method,
			Path:       c.Request.URL.Path,
			Route:      c.FullPath(),
			Action:     classifyAuditAction(c),
			Resource:   auditResource(c.FullPath(), c.Request.URL.Path),
			StatusCode: c.Writer.Status(),
			ClientIP:   c.ClientIP(),
			UserAgent:  truncate(c.Request.UserAgent(), 300),
			Metadata:   metadata,
			ErrorCount: len(c.Errors),
			DurationMS: time.Since(startedAt).Milliseconds(),
		}
		if err := recorder.CreateAuditLog(c.Request.Context(), entry); err != nil && logger != nil {
			logger.WarnContext(c.Request.Context(), "record audit log failed",
				"request_id", requestID,
				"error", err,
			)
		}
	}
}

func auditUser(c *gin.Context) (*int64, *string) {
	user, ok := AuthenticatedUser(c)
	if !ok || user == nil {
		return nil, nil
	}
	return &user.ID, &user.Username
}

func classifyAuditAction(c *gin.Context) string {
	if c.Request.Method == http.MethodGet {
		return model.AuditActionAPI
	}
	user, ok := AuthenticatedUser(c)
	if ok && user != nil && user.Role == model.RoleAdmin {
		return model.AuditActionManagement
	}
	route := c.FullPath()
	if strings.Contains(route, "/enable") ||
		strings.Contains(route, "/disable") ||
		strings.Contains(route, "/reset-password") ||
		strings.Contains(route, "/default") ||
		strings.Contains(route, "/review") ||
		strings.Contains(route, "/cancel") {
		return model.AuditActionManagement
	}
	return model.AuditActionAPI
}

func auditResource(route, path string) string {
	source := route
	if source == "" {
		source = path
	}
	source = strings.TrimPrefix(source, "/api/")
	if source == "" || strings.HasPrefix(source, "/") {
		return "api"
	}
	parts := strings.Split(source, "/")
	if parts[0] == "" {
		return "api"
	}
	return strings.TrimPrefix(parts[0], ":")
}

func sanitizeValues(values map[string][]string) map[string]any {
	result := make(map[string]any, len(values))
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if auditutil.IsSensitiveKey(key) {
			result[key] = auditutil.RedactedValue
			continue
		}
		result[key] = values[key]
	}
	return result
}

func sanitizeParams(params gin.Params) map[string]any {
	result := make(map[string]any, len(params))
	for _, param := range params {
		if auditutil.IsSensitiveKey(param.Key) {
			result[param.Key] = auditutil.RedactedValue
			continue
		}
		result[param.Key] = param.Value
	}
	return result
}

func sanitizedErrorText(value string) string {
	if value == "" {
		return ""
	}
	if auditutil.ContainsSensitiveToken(value) {
		return auditutil.RedactedValue
	}
	return truncate(value, 500)
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
