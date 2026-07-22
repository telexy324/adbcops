package middleware

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLoggerPrintsRecordedErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	router := gin.New()
	router.Use(RequestID(), Logger(logger))
	router.GET("/boom", func(c *gin.Context) {
		_ = c.Error(errors.New("repository exploded"))
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed"})
	})

	request := httptest.NewRequest(http.MethodGet, "/boom", nil)
	request.Header.Set(RequestIDHeader, "req-log-error")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	output := logs.String()
	for _, expected := range []string{"http request failed", "req-log-error", "repository exploded", `"status":500`} {
		if !strings.Contains(output, expected) {
			t.Fatalf("log output missing %q:\n%s", expected, output)
		}
	}
}

func TestLoggerSkipsReadinessProbe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	router := gin.New()
	router.Use(RequestID(), Logger(logger))
	router.GET(readinessProbePath, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})

	request := httptest.NewRequest(http.MethodGet, readinessProbePath, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if logs.Len() != 0 {
		t.Fatalf("readiness probe produced access logs: %s", logs.String())
	}
}
