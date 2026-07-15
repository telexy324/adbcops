package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type ReadinessChecker func(ctx context.Context) error

func health(c *gin.Context) {
	success(c, gin.H{
		"status": "ok",
	})
}

func liveness(c *gin.Context) {
	success(c, gin.H{
		"status": "alive",
	})
}

func readiness(check ReadinessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		if check == nil {
			success(c, gin.H{"status": "ready"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := check(ctx); err != nil {
			recordFailureError(c, err, "readiness check failed")
			failure(c, http.StatusServiceUnavailable, 50301, "service is not ready")
			return
		}
		success(c, gin.H{"status": "ready"})
	}
}
