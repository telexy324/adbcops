package middleware

import (
	"time"

	"aiops-platform/backend/internal/observability"
	"github.com/gin-gonic/gin"
)

func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
		c.Next()
		observability.ObserveHTTPRequest(c.Request.Method, c.FullPath(), c.Writer.Status(), time.Since(startedAt))
	}
}
