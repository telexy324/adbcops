package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"aiops-platform/backend/internal/repository"
	usersvc "aiops-platform/backend/internal/user"
	"github.com/gin-gonic/gin"
)

type UserHandler struct {
	service *usersvc.Service
}

func NewUserHandler(service *usersvc.Service) *UserHandler {
	return &UserHandler{service: service}
}

type createUserRequest struct {
	Username    string  `json:"username" binding:"required"`
	Password    string  `json:"password" binding:"required"`
	DisplayName *string `json:"displayName"`
	Role        string  `json:"role"`
	Enabled     *bool   `json:"enabled"`
}

type resetPasswordRequest struct {
	Password string `json:"password" binding:"required"`
}

func (h *UserHandler) List(c *gin.Context) {
	users, err := h.service.List(c.Request.Context())
	if err != nil {
		failure(c, http.StatusInternalServerError, 50010, "list users failed")
		return
	}
	response := make([]userResponse, 0, len(users))
	for index := range users {
		response = append(response, toUserResponse(&users[index]))
	}
	success(c, response)
}

func (h *UserHandler) Create(c *gin.Context) {
	var request createUserRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	created, err := h.service.Create(c.Request.Context(), usersvc.CreateInput{
		Username:    request.Username,
		Password:    request.Password,
		DisplayName: request.DisplayName,
		Role:        request.Role,
		Enabled:     enabled,
	})
	if handleUserServiceError(c, err, "create user failed") {
		return
	}
	success(c, toUserResponse(created))
}

func (h *UserHandler) Update(c *gin.Context) {
	userID, ok := parseUserID(c)
	if !ok {
		return
	}
	var raw map[string]json.RawMessage
	if err := c.ShouldBindJSON(&raw); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	input := usersvc.UpdateInput{}
	if value, exists := raw["displayName"]; exists {
		input.DisplayNameSet = true
		if string(value) != "null" {
			var displayName string
			if err := json.Unmarshal(value, &displayName); err != nil {
				failure(c, http.StatusBadRequest, 40001, "invalid request")
				return
			}
			input.DisplayName = &displayName
		}
	}
	if value, exists := raw["role"]; exists {
		var role string
		if err := json.Unmarshal(value, &role); err != nil {
			failure(c, http.StatusBadRequest, 40001, "invalid request")
			return
		}
		input.Role = &role
	}
	updated, err := h.service.Update(c.Request.Context(), userID, input)
	if handleUserServiceError(c, err, "update user failed") {
		return
	}
	success(c, toUserResponse(updated))
}

func (h *UserHandler) ResetPassword(c *gin.Context) {
	userID, ok := parseUserID(c)
	if !ok {
		return
	}
	var request resetPasswordRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	err := h.service.ResetPassword(c.Request.Context(), userID, request.Password)
	if handleUserServiceError(c, err, "reset password failed") {
		return
	}
	success(c, gin.H{"passwordReset": true})
}

func (h *UserHandler) Enable(c *gin.Context) {
	h.setEnabled(c, true)
}

func (h *UserHandler) Disable(c *gin.Context) {
	h.setEnabled(c, false)
}

func (h *UserHandler) setEnabled(c *gin.Context, enabled bool) {
	userID, ok := parseUserID(c)
	if !ok {
		return
	}
	updated, err := h.service.SetEnabled(c.Request.Context(), userID, enabled)
	if handleUserServiceError(c, err, "set user enabled failed") {
		return
	}
	success(c, toUserResponse(updated))
}

func parseUserID(c *gin.Context) (int64, bool) {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || userID <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return 0, false
	}
	return userID, true
}

func handleUserServiceError(c *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	recordFailureError(c, err, fallback)
	switch {
	case errors.Is(err, usersvc.ErrInvalidInput):
		failure(c, http.StatusBadRequest, 40001, "invalid request")
	case errors.Is(err, usersvc.ErrPasswordPolicy):
		failure(c, http.StatusBadRequest, 40003, "password must contain 12 to 72 bytes")
	case errors.Is(err, usersvc.ErrUsernameTaken):
		failure(c, http.StatusConflict, 40902, "username already exists")
	case errors.Is(err, usersvc.ErrLastAdmin):
		failure(c, http.StatusConflict, 40901, "cannot disable or demote the last enabled admin")
	case errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40401, "user not found")
	default:
		failure(c, http.StatusInternalServerError, 50010, fallback)
	}
	return true
}
