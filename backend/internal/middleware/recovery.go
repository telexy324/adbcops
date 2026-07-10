package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
)

// Recovery converts panics into a safe JSON response and records diagnostics.
func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				requestID := GetRequestID(c)
				logger.ErrorContext(c.Request.Context(), "panic recovered",
					"request_id", requestID,
					"panic_type", fmt.Sprintf("%T", recovered),
					"stack", string(debug.Stack()),
				)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"code":      50000,
					"message":   "internal server error",
					"data":      nil,
					"requestId": requestID,
				})
			}
		}()
		c.Next()
	}
}
