package middleware

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"aiops-platform/backend/internal/model"
	"github.com/gin-gonic/gin"
)

type countingAuditRecorder struct {
	count int
}

func (r *countingAuditRecorder) CreateAuditLog(context.Context, *model.AuditLog) error {
	r.count++
	return nil
}

func TestAuditSkipsReadinessProbe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &countingAuditRecorder{}
	router := gin.New()
	router.Use(Audit(recorder, slog.New(slog.NewJSONHandler(io.Discard, nil))))
	router.GET(readinessProbePath, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})

	request := httptest.NewRequest(http.MethodGet, readinessProbePath, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if recorder.count != 0 {
		t.Fatalf("readiness probe wrote %d audit logs", recorder.count)
	}
}
