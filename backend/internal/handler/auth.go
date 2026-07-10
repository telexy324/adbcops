package handler

import (
	"errors"
	"net/http"
	"time"

	"aiops-platform/backend/internal/auth"
	appmiddleware "aiops-platform/backend/internal/middleware"
	"aiops-platform/backend/internal/model"
	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	service *auth.Service
}

func NewAuthHandler(service *auth.Service) *AuthHandler {
	return &AuthHandler{service: service}
}

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword" binding:"required"`
	NewPassword     string `json:"newPassword" binding:"required"`
}

type userResponse struct {
	ID          int64   `json:"id"`
	Username    string  `json:"username"`
	DisplayName *string `json:"displayName,omitempty"`
	Role        string  `json:"role"`
	Enabled     bool    `json:"enabled"`
}

func (h *AuthHandler) Login(c *gin.Context) {
	var request loginRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}

	result, err := h.service.Login(c.Request.Context(), request.Username, request.Password, auth.LoginMetadata{
		ClientIP:  c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	})
	if errors.Is(err, auth.ErrInvalidCredentials) || errors.Is(err, auth.ErrUserDisabled) {
		failure(c, http.StatusUnauthorized, 40102, "invalid username or password")
		return
	}
	if errors.Is(err, auth.ErrInvalidInput) {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	if err != nil {
		failure(c, http.StatusInternalServerError, 50001, "login failed")
		return
	}

	success(c, gin.H{
		"accessToken": result.Token,
		"tokenType":   "Bearer",
		"expiresAt":   result.ExpiresAt.Format(time.RFC3339),
		"user":        toUserResponse(result.User),
	})
}

func (h *AuthHandler) Me(c *gin.Context) {
	user, ok := appmiddleware.AuthenticatedUser(c)
	if !ok {
		failure(c, http.StatusUnauthorized, 40101, "authentication required")
		return
	}
	success(c, toUserResponse(user))
}

func (h *AuthHandler) ChangePassword(c *gin.Context) {
	user, ok := appmiddleware.AuthenticatedUser(c)
	if !ok {
		failure(c, http.StatusUnauthorized, 40101, "authentication required")
		return
	}
	var request changePasswordRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}

	err := h.service.ChangePassword(c.Request.Context(), user.ID, request.CurrentPassword, request.NewPassword)
	switch {
	case errors.Is(err, auth.ErrCurrentPassword):
		failure(c, http.StatusBadRequest, 40002, "current password is incorrect")
	case errors.Is(err, auth.ErrPasswordPolicy):
		failure(c, http.StatusBadRequest, 40003, "new password must contain 12 to 72 bytes and differ from the current password")
	case err != nil:
		failure(c, http.StatusInternalServerError, 50002, "password change failed")
	default:
		success(c, gin.H{"passwordChanged": true})
	}
}

func (h *AuthHandler) Logout(c *gin.Context) {
	success(c, gin.H{"loggedOut": true})
}

func toUserResponse(user *model.AppUser) userResponse {
	return userResponse{
		ID:          user.ID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Role:        user.Role,
		Enabled:     user.Enabled,
	}
}
