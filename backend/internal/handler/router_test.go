package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appmiddleware "aiops-platform/backend/internal/middleware"
	"github.com/gin-gonic/gin"
)

func TestHealth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(discardLogger(), RouterDependencies{})

	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	request.Header.Set(appmiddleware.RequestIDHeader, "req-test-health")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Header().Get(appmiddleware.RequestIDHeader) != "req-test-health" {
		t.Fatalf("X-Request-ID = %q", response.Header().Get(appmiddleware.RequestIDHeader))
	}

	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != 0 || body.Message != "success" || body.Data.Status != "ok" {
		t.Fatalf("response = %+v", body)
	}
}

func TestRequestIDRejectsUnsafeValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(discardLogger(), RouterDependencies{})

	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	request.Header.Set(appmiddleware.RequestIDHeader, "unsafe\nvalue")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	requestID := response.Header().Get(appmiddleware.RequestIDHeader)
	if !strings.HasPrefix(requestID, "req-") || strings.ContainsAny(requestID, "\r\n") {
		t.Fatalf("generated X-Request-ID = %q", requestID)
	}
}

func TestRecoveryIncludesRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(discardLogger(), RouterDependencies{})
	router.GET("/panic", func(_ *gin.Context) { panic("test panic") })

	request := httptest.NewRequest(http.MethodGet, "/panic", nil)
	request.Header.Set(appmiddleware.RequestIDHeader, "req-test-panic")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	var body struct {
		Code      int    `json:"code"`
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != 50000 || body.RequestID != "req-test-panic" {
		t.Fatalf("response = %+v", body)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
