package handler

import (
	"errors"
	"net/http"

	k8ssvc "aiops-platform/backend/internal/k8s"
	"aiops-platform/backend/internal/repository"
	"github.com/gin-gonic/gin"
)

type K8sHandler struct {
	service *k8ssvc.Service
}

func NewK8sHandler(service *k8ssvc.Service) *K8sHandler {
	return &K8sHandler{service: service}
}

type k8sTestRequest struct {
	DataSourceID int64 `json:"dataSourceId" binding:"required"`
}

type k8sResourceRequest struct {
	DataSourceID int64  `json:"dataSourceId" binding:"required"`
	Namespace    string `json:"namespace"`
	Resource     string `json:"resource" binding:"required"`
	Name         string `json:"name"`
	Limit        int    `json:"limit"`
}

type k8sPodDiagnosisRequest struct {
	DataSourceID        int64  `json:"dataSourceId" binding:"required"`
	Namespace           string `json:"namespace" binding:"required"`
	PodName             string `json:"podName" binding:"required"`
	IncludeNode         bool   `json:"includeNode"`
	LogTailLines        int    `json:"logTailLines"`
	LogMaxBytes         int    `json:"logMaxBytes"`
	IncludePreviousLogs bool   `json:"includePreviousLogs"`
}

func (h *K8sHandler) Test(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request k8sTestRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	result, err := h.service.Test(c.Request.Context(), actor, request.DataSourceID)
	if handleK8sError(c, err, "test kubernetes data source failed") {
		return
	}
	success(c, result)
}

func (h *K8sHandler) Resources(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request k8sResourceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	result, err := h.service.Resources(c.Request.Context(), actor, k8ssvc.ResourceInput{
		DataSourceID: request.DataSourceID,
		Namespace:    request.Namespace,
		Resource:     request.Resource,
		Name:         request.Name,
		Limit:        request.Limit,
	})
	if handleK8sError(c, err, "read kubernetes resources failed") {
		return
	}
	success(c, result)
}

func (h *K8sHandler) DiagnosePod(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request k8sPodDiagnosisRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	result, err := h.service.DiagnosePod(c.Request.Context(), actor, k8ssvc.PodDiagnosisInput{
		DataSourceID:        request.DataSourceID,
		Namespace:           request.Namespace,
		PodName:             request.PodName,
		IncludeNode:         request.IncludeNode,
		LogTailLines:        request.LogTailLines,
		LogMaxBytes:         request.LogMaxBytes,
		IncludePreviousLogs: request.IncludePreviousLogs,
	})
	if handleK8sError(c, err, "diagnose kubernetes pod failed") {
		return
	}
	success(c, result)
}

func handleK8sError(c *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, k8ssvc.ErrNamespaceNotAllowed):
		failure(c, http.StatusForbidden, 40311, "namespace is not allowed")
	case errors.Is(err, k8ssvc.ErrNoAllowedNamespaces):
		failure(c, http.StatusForbidden, 40312, "allowed namespaces are required")
	case errors.Is(err, k8ssvc.ErrInvalidInput), errors.Is(err, k8ssvc.ErrUnsupportedSource), errors.Is(err, k8ssvc.ErrUnsupportedResource), errors.Is(err, k8ssvc.ErrDataSourceDisabled):
		failure(c, http.StatusBadRequest, 40001, "invalid request")
	case errors.Is(err, k8ssvc.ErrForbidden):
		failure(c, http.StatusForbidden, 40313, "kubernetes access forbidden")
	case errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40401, "data source not found")
	default:
		failure(c, http.StatusInternalServerError, 50090, fallback)
	}
	return true
}
