package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"aiops-platform/backend/internal/model"
	"github.com/gin-gonic/gin"
)

const authenticatedUserKey = "authenticated_user"

type TokenAuthenticator interface {
	Authenticate(ctx context.Context, token string) (*model.AppUser, error)
}

// Authenticate requires a valid Bearer token and rechecks the current user
// state in the database on every request.
func Authenticate(service TokenAuthenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		parts := strings.Fields(header)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
			abortUnauthorized(c, errors.New("missing or invalid bearer token"))
			return
		}

		user, err := service.Authenticate(c.Request.Context(), parts[1])
		if err != nil {
			abortUnauthorized(c, fmt.Errorf("authenticate bearer token: %w", err))
			return
		}
		c.Set(authenticatedUserKey, user)
		c.Next()
	}
}

func AuthenticatedUser(c *gin.Context) (*model.AppUser, bool) {
	value, exists := c.Get(authenticatedUserKey)
	if !exists {
		return nil, false
	}
	user, ok := value.(*model.AppUser)
	return user, ok
}

func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := AuthenticatedUser(c)
		if !ok {
			abortUnauthorized(c, errors.New("authenticated user missing from context"))
			return
		}
		if user.Role != model.RoleAdmin {
			_ = c.Error(fmt.Errorf("admin role required: user_id=%d role=%s", user.ID, user.Role))
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":      40301,
				"message":   "admin role required",
				"data":      nil,
				"requestId": GetRequestID(c),
			})
			return
		}
		c.Next()
	}
}

func abortUnauthorized(c *gin.Context, err error) {
	if err != nil {
		_ = c.Error(err)
	}
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
		"code":      40101,
		"message":   "authentication required",
		"data":      nil,
		"requestId": GetRequestID(c),
	})
}
