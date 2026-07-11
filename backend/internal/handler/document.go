package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	documentsvc "aiops-platform/backend/internal/document"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
	"github.com/gin-gonic/gin"
)

type DocumentHandler struct {
	service        *documentsvc.Service
	maxUploadBytes int64
}

func NewDocumentHandler(service *documentsvc.Service, maxUploadBytes int64) *DocumentHandler {
	return &DocumentHandler{service: service, maxUploadBytes: maxUploadBytes}
}

type documentResponse struct {
	ID            int64           `json:"id"`
	Title         string          `json:"title"`
	FileName      string          `json:"fileName"`
	FileType      string          `json:"fileType"`
	SystemName    *string         `json:"systemName,omitempty"`
	ComponentName *string         `json:"componentName,omitempty"`
	Environment   *string         `json:"environment,omitempty"`
	DocType       *string         `json:"docType,omitempty"`
	Version       string          `json:"version"`
	Status        string          `json:"status"`
	Tags          json.RawMessage `json:"tags,omitempty"`
	Summary       *string         `json:"summary,omitempty"`
	QualityScore  int             `json:"qualityScore"`
	CreatedBy     *int64          `json:"createdBy,omitempty"`
	CreatedAt     string          `json:"createdAt"`
	UpdatedAt     string          `json:"updatedAt"`
}

func (h *DocumentHandler) Upload(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.maxUploadBytes+1024*1024)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	document, err := h.service.Upload(c.Request.Context(), actor, fileHeader, documentsvc.UploadMetadata{
		Title:         c.PostForm("title"),
		SystemName:    c.PostForm("systemName"),
		ComponentName: c.PostForm("componentName"),
		Environment:   c.PostForm("environment"),
		DocType:       c.PostForm("docType"),
		Version:       c.PostForm("version"),
		Tags:          json.RawMessage(c.PostForm("tags")),
	})
	if handleDocumentError(c, err, "upload document failed") {
		return
	}
	success(c, toDocumentResponse(document))
}

func (h *DocumentHandler) List(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	documents, err := h.service.List(c.Request.Context(), actor)
	if handleDocumentError(c, err, "list documents failed") {
		return
	}
	response := make([]documentResponse, 0, len(documents))
	for index := range documents {
		response = append(response, toDocumentResponse(&documents[index]))
	}
	success(c, response)
}

func (h *DocumentHandler) Get(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	document, err := h.service.Get(c.Request.Context(), actor, id)
	if handleDocumentError(c, err, "get document failed") {
		return
	}
	success(c, toDocumentResponse(document))
}

func handleDocumentError(c *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, documentsvc.ErrUnsupportedExt):
		failure(c, http.StatusUnsupportedMediaType, 41501, "unsupported file type")
	case errors.Is(err, documentsvc.ErrFileTooLarge):
		failure(c, http.StatusRequestEntityTooLarge, 41301, "file too large")
	case errors.Is(err, documentsvc.ErrPathTraversal):
		failure(c, http.StatusBadRequest, 40004, "file path is not allowed")
	case errors.Is(err, documentsvc.ErrInvalidFile), errors.Is(err, documentsvc.ErrInvalidInput):
		failure(c, http.StatusBadRequest, 40001, "invalid request")
	case errors.Is(err, documentsvc.ErrForbidden):
		failure(c, http.StatusForbidden, 40303, "document access forbidden")
	case errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40401, "document not found")
	default:
		failure(c, http.StatusInternalServerError, 50040, fallback)
	}
	return true
}

func toDocumentResponse(document *model.KBDocument) documentResponse {
	return documentResponse{
		ID:            document.ID,
		Title:         document.Title,
		FileName:      document.FileName,
		FileType:      document.FileType,
		SystemName:    document.SystemName,
		ComponentName: document.ComponentName,
		Environment:   document.Environment,
		DocType:       document.DocType,
		Version:       document.Version,
		Status:        document.Status,
		Tags:          rawJSON(document.Tags),
		Summary:       document.Summary,
		QualityScore:  document.QualityScore,
		CreatedBy:     document.CreatedBy,
		CreatedAt:     document.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     document.UpdatedAt.Format(time.RFC3339),
	}
}
