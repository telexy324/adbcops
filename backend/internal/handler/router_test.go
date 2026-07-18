package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appmiddleware "aiops-platform/backend/internal/middleware"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/toolregistry"
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

func TestLiveness(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(discardLogger(), RouterDependencies{})

	request := httptest.NewRequest(http.MethodGet, "/api/live", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var body struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.Status != "alive" {
		t.Fatalf("status = %q, want alive", body.Data.Status)
	}
}

func TestReadiness(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(discardLogger(), RouterDependencies{
		ReadinessCheck: func(_ context.Context) error { return nil },
	})

	request := httptest.NewRequest(http.MethodGet, "/api/ready", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestReadinessReportsUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(discardLogger(), RouterDependencies{
		ReadinessCheck: func(_ context.Context) error { return errors.New("database unavailable") },
	})

	request := httptest.NewRequest(http.MethodGet, "/api/ready", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
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

func TestMetricsEndpointExposesHTTPRequestMetrics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(discardLogger(), RouterDependencies{})

	healthRequest := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	healthResponse := httptest.NewRecorder()
	router.ServeHTTP(healthResponse, healthRequest)
	if healthResponse.Code != http.StatusOK {
		t.Fatalf("health status = %d", healthResponse.Code)
	}

	metricsRequest := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
	metricsResponse := httptest.NewRecorder()
	router.ServeHTTP(metricsResponse, metricsRequest)
	if metricsResponse.Code != http.StatusOK {
		t.Fatalf("metrics status = %d", metricsResponse.Code)
	}
	body := metricsResponse.Body.String()
	if !strings.Contains(body, "aiops_http_requests_total") || !strings.Contains(body, `route="/api/health"`) {
		t.Fatalf("metrics body missing http metrics:\n%s", body)
	}
}

func TestAuditMiddlewareRecordsRequestIDAndRedactsQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &routerAuditRecorder{}
	router := NewRouter(discardLogger(), RouterDependencies{AuditRecorder: recorder})

	request := httptest.NewRequest(http.MethodGet, "/api/health?password=plain-secret&source=alert", nil)
	request.Header.Set(appmiddleware.RequestIDHeader, "req-audit-http")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if len(recorder.logs) != 1 {
		t.Fatalf("audit log count = %d, want 1", len(recorder.logs))
	}
	log := recorder.logs[0]
	if log.RequestID != "req-audit-http" || log.Method != http.MethodGet || log.Resource != "health" {
		t.Fatalf("unexpected audit log: %+v", log)
	}
	metadata := string(log.Metadata)
	if strings.Contains(metadata, "plain-secret") {
		t.Fatalf("sensitive value leaked in metadata: %s", metadata)
	}
	if !strings.Contains(metadata, "[REDACTED]") {
		t.Fatalf("metadata did not redact sensitive query: %s", metadata)
	}
}

func TestUserAccessingAdminAPIIsForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	normalUser := &model.AppUser{ID: 11, Username: "operator", Role: model.RoleUser, Enabled: true}
	router := NewRouter(discardLogger(), RouterDependencies{
		UserHandler:  &UserHandler{},
		Authenticate: appmiddleware.Authenticate(&routerFakeAuthenticator{user: normalUser}),
		RequireAdmin: appmiddleware.RequireAdmin(),
	})

	request := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	request.Header.Set("Authorization", "Bearer valid-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestUserCannotConfigureLinuxCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	normalUser := &model.AppUser{ID: 11, Username: "operator", Role: model.RoleUser, Enabled: true}
	router := NewRouter(discardLogger(), RouterDependencies{
		LinuxHostHandler: &LinuxHostHandler{},
		Authenticate:     appmiddleware.Authenticate(&routerFakeAuthenticator{user: normalUser}),
		RequireAdmin:     appmiddleware.RequireAdmin(),
	})
	for _, path := range []string{"/api/linux/hosts", "/api/linux/credential-groups"} {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"password":"plain-secret"}`))
		request.Header.Set("Authorization", "Bearer valid-token")
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("POST %s status = %d, want %d", path, response.Code, http.StatusForbidden)
		}
		if strings.Contains(response.Body.String(), "plain-secret") {
			t.Fatalf("POST %s response leaked credential: %s", path, response.Body.String())
		}
	}
}

func TestToolRoutesExposeManagementButNotGenericInvoke(t *testing.T) {
	gin.SetMode(gin.TestMode)
	admin := &model.AppUser{ID: 1, Username: "admin", Role: model.RoleAdmin, Enabled: true}
	router := NewRouter(discardLogger(), RouterDependencies{
		ToolHandler:  NewToolHandler(toolregistry.NewBuiltinRegistry()),
		Authenticate: appmiddleware.Authenticate(&routerFakeAuthenticator{user: admin}),
		RequireAdmin: appmiddleware.RequireAdmin(),
	})

	listRequest := httptest.NewRequest(http.MethodGet, "/api/tools", nil)
	listRequest.Header.Set("Authorization", "Bearer valid-token")
	listResponse := httptest.NewRecorder()
	router.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listResponse.Code, http.StatusOK)
	}

	getRequest := httptest.NewRequest(http.MethodGet, "/api/tools/kubernetes", nil)
	getRequest.Header.Set("Authorization", "Bearer valid-token")
	getResponse := httptest.NewRecorder()
	router.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d", getResponse.Code, http.StatusOK)
	}

	invokeRequest := httptest.NewRequest(http.MethodPost, "/api/tools/kubernetes/invoke", strings.NewReader(`{}`))
	invokeRequest.Header.Set("Authorization", "Bearer valid-token")
	invokeResponse := httptest.NewRecorder()
	router.ServeHTTP(invokeResponse, invokeRequest)
	if invokeResponse.Code != http.StatusNotFound {
		t.Fatalf("invoke status = %d, want %d", invokeResponse.Code, http.StatusNotFound)
	}
}

func TestOptionsPreflightDoesNot404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(discardLogger(), RouterDependencies{})

	request := httptest.NewRequest(http.MethodOptions, "/api/topology/nodes", nil)
	request.Header.Set("Origin", "http://127.0.0.1:5173")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if response.Header().Get("Access-Control-Allow-Origin") != "http://127.0.0.1:5173" {
		t.Fatalf("missing CORS allow origin header: %+v", response.Header())
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

type routerFakeAuthenticator struct {
	user *model.AppUser
}

func (f *routerFakeAuthenticator) Authenticate(_ context.Context, token string) (*model.AppUser, error) {
	if token != "valid-token" || f.user == nil {
		return nil, http.ErrNoCookie
	}
	return f.user, nil
}

type routerAuditRecorder struct {
	logs []model.AuditLog
}

func (r *routerAuditRecorder) CreateAuditLog(_ context.Context, log *model.AuditLog) error {
	copied := *log
	r.logs = append(r.logs, copied)
	return nil
}
