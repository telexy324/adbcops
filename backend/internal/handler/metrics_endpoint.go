package handler

import (
	"net/http"

	"aiops-platform/backend/internal/observability"
	"github.com/gin-gonic/gin"
)

func platformMetrics(c *gin.Context) {
	c.Data(http.StatusOK, "text/plain; version=0.0.4; charset=utf-8", observability.Default.WritePrometheus())
}
