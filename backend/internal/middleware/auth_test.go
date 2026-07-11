package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"aiops-platform/backend/internal/model"
	"github.com/gin-gonic/gin"
)

func TestAuthenticateRejectsMissingToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestID(), Authenticate(&fakeAuthenticator{}))
	router.GET("/protected", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/protected", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if response.Header().Get(RequestIDHeader) == "" {
		t.Fatal("unauthorized response is missing request ID")
	}
}

func TestAuthenticateStoresCurrentUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	wantUser := &model.AppUser{ID: 7, Username: "admin", Role: model.RoleAdmin, Enabled: true}
	router := gin.New()
	router.Use(RequestID(), Authenticate(&fakeAuthenticator{user: wantUser}))
	router.GET("/protected", func(c *gin.Context) {
		user, ok := AuthenticatedUser(c)
		if !ok || user.ID != wantUser.ID {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.Header.Set("Authorization", "Bearer valid-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestRequireAdminRejectsNormalUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	normalUser := &model.AppUser{ID: 8, Username: "operator", Role: model.RoleUser, Enabled: true}
	router := gin.New()
	router.Use(RequestID(), Authenticate(&fakeAuthenticator{user: normalUser}), RequireAdmin())
	router.GET("/admin", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	request := httptest.NewRequest(http.MethodGet, "/admin", nil)
	request.Header.Set("Authorization", "Bearer valid-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

type fakeAuthenticator struct {
	user *model.AppUser
}

func (f *fakeAuthenticator) Authenticate(_ context.Context, token string) (*model.AppUser, error) {
	if token != "valid-token" || f.user == nil {
		return nil, errors.New("invalid token")
	}
	return f.user, nil
}
