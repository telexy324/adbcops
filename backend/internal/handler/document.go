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

type chunkResponse struct {
	ID            int64   `json:"id"`
	DocumentID    int64   `json:"documentId"`
	ChunkIndex    int     `json:"chunkIndex"`
	Content       string  `json:"content"`
	SourceTitle   *string `json:"sourceTitle,omitempty"`
	SourceSection *string `json:"sourceSection,omitempty"`
	TokenCount    int     `json:"tokenCount"`
	CreatedAt     string  `json:"createdAt"`
}

type reviewDocumentRequest struct {
	Result json.RawMessage `json:"result" binding:"required"`
}

type knowledgeSearchRequest struct {
	Query string `json:"query" binding:"required"`
	Limit int    `json:"limit"`
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

func (h *DocumentHandler) Reprocess(c *gin.Context) {
	actor, documentID, ok := currentUserAndDocumentID(c)
	if !ok {
		return
	}
	chunks, err := h.service.Reprocess(c.Request.Context(), actor, documentID)
	if handleDocumentError(c, err, "reprocess document failed") {
		return
	}
	success(c, gin.H{
		"documentId": documentID,
		"chunkCount": len(chunks),
		"chunks":     toChunkResponses(chunks),
	})
}

func (h *DocumentHandler) Chunks(c *gin.Context) {
	actor, documentID, ok := currentUserAndDocumentID(c)
	if !ok {
		return
	}
	chunks, err := h.service.ListChunks(c.Request.Context(), actor, documentID)
	if handleDocumentError(c, err, "list document chunks failed") {
		return
	}
	success(c, gin.H{
		"documentId": documentID,
		"chunkCount": len(chunks),
		"chunks":     toChunkResponses(chunks),
	})
}

func (h *DocumentHandler) Review(c *gin.Context) {
	actor, documentID, ok := currentUserAndDocumentID(c)
	if !ok {
		return
	}
	var request reviewDocumentRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid JSON request")
		return
	}
	document, result, err := h.service.ReviewQuality(c.Request.Context(), actor, documentID, request.Result)
	if handleDocumentError(c, err, "review document failed") {
		return
	}
	success(c, gin.H{
		"document":      toDocumentResponse(document),
		"qualityResult": result,
		"canPublish":    documentsvc.CanPublish(document),
	})
}

func (h *DocumentHandler) Search(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request knowledgeSearchRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	chunks, err := h.service.Search(c.Request.Context(), actor, request.Query, request.Limit)
	if handleDocumentError(c, err, "search knowledge failed") {
		return
	}
	success(c, gin.H{
		"query":  request.Query,
		"count":  len(chunks),
		"chunks": toChunkResponses(chunks),
	})
}

func currentUserAndDocumentID(c *gin.Context) (*model.AppUser, int64, bool) {
	actor, ok := currentUser(c)
	if !ok {
		return nil, 0, false
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return nil, 0, false
	}
	return actor, id, true
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
	case errors.Is(err, documentsvc.ErrInvalidQualityJSON):
		failure(c, http.StatusBadRequest, 40005, err.Error())
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

func toChunkResponses(chunks []model.KBChunk) []chunkResponse {
	response := make([]chunkResponse, 0, len(chunks))
	for index := range chunks {
		response = append(response, toChunkResponse(&chunks[index]))
	}
	return response
}

func toChunkResponse(chunk *model.KBChunk) chunkResponse {
	return chunkResponse{
		ID:            chunk.ID,
		DocumentID:    chunk.DocumentID,
		ChunkIndex:    chunk.ChunkIndex,
		Content:       chunk.Content,
		SourceTitle:   chunk.SourceTitle,
		SourceSection: chunk.SourceSection,
		TokenCount:    chunk.TokenCount,
		CreatedAt:     chunk.CreatedAt.Format(time.RFC3339),
	}
}
