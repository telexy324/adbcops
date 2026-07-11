package handler

import (
	"errors"
	"net/http"

	"aiops-platform/backend/internal/repository"
	sshsftpsvc "aiops-platform/backend/internal/sshsftp"
	"github.com/gin-gonic/gin"
)

type SFTPHandler struct {
	service *sshsftpsvc.Service
}

func NewSFTPHandler(service *sshsftpsvc.Service) *SFTPHandler {
	return &SFTPHandler{service: service}
}

type readSFTPFileRequest struct {
	DataSourceID int64  `json:"dataSourceId" binding:"required"`
	Path         string `json:"path" binding:"required"`
	MaxBytes     int64  `json:"maxBytes"`
}

func (h *SFTPHandler) ReadFile(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request readSFTPFileRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	result, err := h.service.ReadFile(c.Request.Context(), actor, sshsftpsvc.ReadInput{
		DataSourceID: request.DataSourceID,
		Path:         request.Path,
		MaxBytes:     request.MaxBytes,
	})
	if handleSFTPError(c, err, "read sftp file failed") {
		return
	}
	success(c, result)
}

func handleSFTPError(c *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, sshsftpsvc.ErrPathTraversal):
		failure(c, http.StatusBadRequest, 40009, "path traversal is not allowed")
	case errors.Is(err, sshsftpsvc.ErrPathNotAllowed):
		failure(c, http.StatusForbidden, 40308, "path is outside allowlist")
	case errors.Is(err, sshsftpsvc.ErrSensitivePath):
		failure(c, http.StatusForbidden, 40309, "sensitive path is not allowed")
	case errors.Is(err, sshsftpsvc.ErrFileTooLarge):
		failure(c, http.StatusRequestEntityTooLarge, 41302, "file too large")
	case errors.Is(err, sshsftpsvc.ErrInvalidInput), errors.Is(err, sshsftpsvc.ErrUnsupportedSource):
		failure(c, http.StatusBadRequest, 40001, "invalid request")
	case errors.Is(err, sshsftpsvc.ErrForbidden):
		failure(c, http.StatusForbidden, 40308, "sftp access forbidden")
	case errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40401, "data source not found")
	default:
		failure(c, http.StatusInternalServerError, 50080, fallback)
	}
	return true
}
