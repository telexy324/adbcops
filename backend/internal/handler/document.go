package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
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
	ID                        int64           `json:"id"`
	Title                     string          `json:"title"`
	FileName                  string          `json:"fileName"`
	FileType                  string          `json:"fileType"`
	SystemName                *string         `json:"systemName,omitempty"`
	ComponentName             *string         `json:"componentName,omitempty"`
	Environment               *string         `json:"environment,omitempty"`
	DocType                   *string         `json:"docType,omitempty"`
	Version                   string          `json:"version"`
	Status                    string          `json:"status"`
	Tags                      json.RawMessage `json:"tags,omitempty"`
	Summary                   *string         `json:"summary,omitempty"`
	QualityScore              int             `json:"qualityScore"`
	CreatedBy                 *int64          `json:"createdBy,omitempty"`
	CreatedAt                 string          `json:"createdAt"`
	UpdatedAt                 string          `json:"updatedAt"`
	CurrentPublishedVersionID *int64          `json:"currentPublishedVersionId,omitempty"`
}

type qualityStandardResponse struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	FileName  string `json:"fileName"`
	FileType  string `json:"fileType"`
	Enabled   bool   `json:"enabled"`
	CreatedBy *int64 `json:"createdBy,omitempty"`
	CreatedAt string `json:"createdAt"`
	Preview   string `json:"preview"`
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

type documentVersionResponse struct {
	ID             int64           `json:"id"`
	DocumentID     int64           `json:"documentId"`
	Version        string          `json:"version"`
	RevisionNo     int             `json:"revisionNo"`
	FileName       string          `json:"fileName"`
	FileType       string          `json:"fileType"`
	FileHash       string          `json:"fileHash"`
	ParserName     *string         `json:"parserName,omitempty"`
	ParserVersion  *string         `json:"parserVersion,omitempty"`
	Language       *string         `json:"language,omitempty"`
	Status         string          `json:"status"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
	ParseQuality   json.RawMessage `json:"parseQuality,omitempty"`
	DocumentSchema json.RawMessage `json:"documentSchema,omitempty"`
	CreatedBy      *int64          `json:"createdBy,omitempty"`
	CreatedAt      string          `json:"createdAt"`
	UpdatedAt      string          `json:"updatedAt"`
	PublishedAt    *string         `json:"publishedAt,omitempty"`
	SupersededAt   *string         `json:"supersededAt,omitempty"`
	DeprecatedAt   *string         `json:"deprecatedAt,omitempty"`
}

type parsedStructureResponse struct {
	Version        documentVersionResponse              `json:"version"`
	ParseQuality   documentsvc.ParseQuality             `json:"parseQuality"`
	DocumentSchema documentsvc.DocumentSchemaExtraction `json:"documentSchema"`
	Warnings       []documentsvc.ParseWarning           `json:"warnings"`
	Blocks         []documentsvc.DocumentBlock          `json:"blocks"`
}

type reviewDocumentRequest struct {
	Result      json.RawMessage `json:"result"`
	Action      string          `json:"action"`
	Comment     string          `json:"comment"`
	AutoQuality bool            `json:"autoQuality"`
	UseDefault  *bool           `json:"useDefault"`
	StandardIDs []int64         `json:"standardIds"`
}

type chunkDocumentVersionRequest struct {
	StrategyID int64 `json:"strategyId"`
}

type knowledgeSearchRequest struct {
	Query string `json:"query" binding:"required"`
	Limit int    `json:"limit"`
}

type versionActionRequest struct {
	Comment string `json:"comment"`
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

func (h *DocumentHandler) UploadVersion(c *gin.Context) {
	actor, documentID, ok := currentUserAndDocumentID(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.maxUploadBytes+1024*1024)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	version, err := h.service.UploadVersion(c.Request.Context(), actor, documentID, fileHeader, c.PostForm("version"))
	if handleDocumentError(c, err, "upload document version failed") {
		return
	}
	success(c, toDocumentVersionResponse(version))
}

func (h *DocumentHandler) Versions(c *gin.Context) {
	actor, documentID, ok := currentUserAndDocumentID(c)
	if !ok {
		return
	}
	versions, err := h.service.ListVersions(c.Request.Context(), actor, documentID)
	if handleDocumentError(c, err, "list document versions failed") {
		return
	}
	items := make([]documentVersionResponse, 0, len(versions))
	for i := range versions {
		items = append(items, toDocumentVersionResponse(&versions[i]))
	}
	success(c, gin.H{"items": items, "count": len(items)})
}

func (h *DocumentHandler) UploadQualityStandard(c *gin.Context) {
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
	standard, err := h.service.UploadQualityStandard(c.Request.Context(), actor, fileHeader, c.PostForm("title"))
	if handleDocumentError(c, err, "upload quality standard failed") {
		return
	}
	success(c, toQualityStandardResponse(standard))
}

func (h *DocumentHandler) QualityStandards(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	standards, err := h.service.ListQualityStandards(c.Request.Context(), actor)
	if handleDocumentError(c, err, "list quality standards failed") {
		return
	}
	response := make([]qualityStandardResponse, 0, len(standards))
	for index := range standards {
		response = append(response, toQualityStandardResponse(&standards[index]))
	}
	success(c, response)
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
	latestVersion, err := h.service.GetLatestDocumentVersion(c.Request.Context(), actor, documentID)
	if handleDocumentError(c, err, "get parsed document version failed") {
		return
	}
	success(c, gin.H{
		"documentId":      documentID,
		"documentVersion": toDocumentVersionResponse(latestVersion),
		"chunkCount":      len(chunks),
		"chunks":          toChunkResponses(chunks),
	})
}

func (h *DocumentHandler) LatestVersion(c *gin.Context) {
	actor, documentID, ok := currentUserAndDocumentID(c)
	if !ok {
		return
	}
	version, err := h.service.GetLatestDocumentVersion(c.Request.Context(), actor, documentID)
	if handleDocumentError(c, err, "get latest document version failed") {
		return
	}
	success(c, toDocumentVersionResponse(version))
}

func (h *DocumentHandler) GetVersion(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	versionID, ok := parseVersionID(c)
	if !ok {
		return
	}
	version, err := h.service.GetDocumentVersion(c.Request.Context(), actor, versionID)
	if handleDocumentError(c, err, "get document version failed") {
		return
	}
	success(c, toDocumentVersionResponse(version))
}

func (h *DocumentHandler) PublicationGate(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	versionID, ok := parseVersionID(c)
	if !ok {
		return
	}
	gate, err := h.service.EvaluatePublicationGate(c.Request.Context(), actor, versionID)
	if handleDocumentError(c, err, "evaluate publication gate failed") {
		return
	}
	success(c, gate)
}

func (h *DocumentHandler) PublishVersion(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	versionID, ok := parseVersionID(c)
	if !ok {
		return
	}
	var request versionActionRequest
	if c.Request.ContentLength > 0 && c.ShouldBindJSON(&request) != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	document, gate, err := h.service.PublishVersion(c.Request.Context(), actor, versionID, request.Comment)
	if err != nil {
		if errors.Is(err, documentsvc.ErrPublicationGate) {
			recordFailureError(c, err, "publish document version failed")
			failureWithData(c, http.StatusConflict, 40903, publicationGateFailureMessage(gate), gate)
			return
		}
		if handleDocumentError(c, err, "publish document version failed") {
			return
		}
	}
	success(c, gin.H{"document": toDocumentResponse(document), "gate": gate})
}

func publicationGateFailureMessage(gate *documentsvc.PublicationGate) string {
	if gate == nil {
		return "publication gate is not satisfied"
	}
	failed := make([]string, 0, len(gate.Checks))
	for _, check := range gate.Checks {
		if !check.Passed {
			failed = append(failed, check.Name)
		}
	}
	if len(failed) == 0 {
		return "publication gate is not satisfied"
	}
	return "publication gate is not satisfied: " + strings.Join(failed, ", ")
}

func (h *DocumentHandler) DeprecateVersion(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	versionID, ok := parseVersionID(c)
	if !ok {
		return
	}
	var request versionActionRequest
	if c.Request.ContentLength > 0 && c.ShouldBindJSON(&request) != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	version, err := h.service.DeprecateVersion(c.Request.Context(), actor, versionID, request.Comment)
	if handleDocumentError(c, err, "deprecate document version failed") {
		return
	}
	success(c, toDocumentVersionResponse(version))
}

func (h *DocumentHandler) DiffVersions(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	fromID, ok := parseVersionID(c)
	if !ok {
		return
	}
	toID, err := strconv.ParseInt(c.Param("otherVersionId"), 10, 64)
	if err != nil || toID <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	diff, err := h.service.DiffVersions(c.Request.Context(), actor, fromID, toID)
	if handleDocumentError(c, err, "diff document versions failed") {
		return
	}
	success(c, diff)
}

func (h *DocumentHandler) HistoricalCitation(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	versionID, ok := parseVersionID(c)
	if !ok {
		return
	}
	chunkID, err := strconv.ParseInt(c.Param("chunkId"), 10, 64)
	if err != nil || chunkID <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	citation, err := h.service.ResolveHistoricalCitation(c.Request.Context(), actor, versionID, chunkID)
	if handleDocumentError(c, err, "resolve historical citation failed") {
		return
	}
	success(c, citation)
}

func (h *DocumentHandler) ParseVersion(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	versionID, ok := parseVersionID(c)
	if !ok {
		return
	}
	structure, err := h.service.ParseDocumentVersion(c.Request.Context(), actor, versionID)
	if handleDocumentError(c, err, "parse document version failed") {
		return
	}
	success(c, toParsedStructureResponse(structure))
}

func (h *DocumentHandler) ParsedStructure(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	versionID, ok := parseVersionID(c)
	if !ok {
		return
	}
	structure, err := h.service.GetParsedStructure(c.Request.Context(), actor, versionID)
	if handleDocumentError(c, err, "get parsed document structure failed") {
		return
	}
	success(c, toParsedStructureResponse(structure))
}

func (h *DocumentHandler) CreateChunkStrategy(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request documentsvc.CreateChunkStrategyInput
	if c.ShouldBindJSON(&request) != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	strategy, err := h.service.CreateChunkStrategy(c.Request.Context(), actor, request)
	if handleDocumentError(c, err, "create chunk strategy failed") {
		return
	}
	success(c, strategy)
}

func (h *DocumentHandler) ChunkStrategies(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	strategies, err := h.service.ListChunkStrategies(c.Request.Context(), actor)
	if handleDocumentError(c, err, "list chunk strategies failed") {
		return
	}
	success(c, gin.H{"items": strategies, "count": len(strategies)})
}

func (h *DocumentHandler) ChunkStrategy(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("strategyId"), 10, 64)
	if err != nil || id <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	strategy, err := h.service.GetChunkStrategy(c.Request.Context(), actor, id)
	if handleDocumentError(c, err, "get chunk strategy failed") {
		return
	}
	success(c, strategy)
}

func (h *DocumentHandler) ChunkVersion(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	versionID, ok := parseVersionID(c)
	if !ok {
		return
	}
	var request chunkDocumentVersionRequest
	if c.ShouldBindJSON(&request) != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	chunks, err := h.service.ChunkDocumentVersion(c.Request.Context(), actor, versionID, request.StrategyID)
	if handleDocumentError(c, err, "chunk document version failed") {
		return
	}
	success(c, gin.H{"documentVersionId": versionID, "strategyId": request.StrategyID, "chunkCount": len(chunks), "chunks": chunks})
}

func (h *DocumentHandler) VersionChunks(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	versionID, ok := parseVersionID(c)
	if !ok {
		return
	}
	var strategyID *int64
	if raw := c.Query("strategyId"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			failure(c, http.StatusBadRequest, 40001, "invalid request")
			return
		}
		strategyID = &value
	}
	chunks, err := h.service.ListDocumentVersionChunks(c.Request.Context(), actor, versionID, strategyID)
	if handleDocumentError(c, err, "list document version chunks failed") {
		return
	}
	success(c, gin.H{"documentVersionId": versionID, "chunkCount": len(chunks), "chunks": chunks})
}

func parseVersionID(c *gin.Context) (int64, bool) {
	versionID, err := strconv.ParseInt(c.Param("versionId"), 10, 64)
	if err != nil || versionID <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return 0, false
	}
	return versionID, true
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
	if request.Action != "" {
		document, err := h.service.ReviewDecision(c.Request.Context(), actor, documentID, documentsvc.ReviewDecision{
			Action:  request.Action,
			Comment: request.Comment,
		})
		if handleDocumentError(c, err, "review document failed") {
			return
		}
		success(c, gin.H{
			"document":   toDocumentResponse(document),
			"action":     request.Action,
			"canPublish": h.service.CanPublish(document),
		})
		return
	}
	if len(request.Result) == 0 {
		if request.AutoQuality {
			useDefault := true
			if request.UseDefault != nil {
				useDefault = *request.UseDefault
			}
			document, result, err := h.service.AutoReviewQuality(c.Request.Context(), actor, documentID, documentsvc.AutoQualityInput{
				UseDefault:  useDefault,
				StandardIDs: request.StandardIDs,
			})
			if handleDocumentError(c, err, "auto review document failed") {
				return
			}
			success(c, gin.H{
				"document":      toDocumentResponse(document),
				"qualityResult": result,
				"canPublish":    h.service.CanPublish(document),
			})
			return
		}
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	document, result, err := h.service.ReviewQuality(c.Request.Context(), actor, documentID, request.Result)
	if handleDocumentError(c, err, "review document failed") {
		return
	}
	success(c, gin.H{
		"document":      toDocumentResponse(document),
		"qualityResult": result,
		"canPublish":    h.service.CanPublish(document),
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
	recordFailureError(c, err, fallback)
	switch {
	case errors.Is(err, documentsvc.ErrLegacyDocUnsupported):
		failure(c, http.StatusUnsupportedMediaType, 41502, "legacy .doc is unsupported; convert the file to .docx")
	case errors.Is(err, documentsvc.ErrUnsupportedExt):
		failure(c, http.StatusUnsupportedMediaType, 41501, "unsupported file type")
	case errors.Is(err, documentsvc.ErrFileTooLarge):
		failure(c, http.StatusRequestEntityTooLarge, 41301, "file too large")
	case errors.Is(err, documentsvc.ErrPathTraversal):
		failure(c, http.StatusBadRequest, 40004, "file path is not allowed")
	case errors.Is(err, documentsvc.ErrFileTypeMismatch):
		failure(c, http.StatusBadRequest, 40007, "file content does not match its extension")
	case errors.Is(err, documentsvc.ErrParseTimeout):
		failure(c, http.StatusRequestTimeout, 40801, "document parsing timed out")
	case errors.Is(err, documentsvc.ErrBlockLimitExceeded), errors.Is(err, documentsvc.ErrPageLimitExceeded):
		failure(c, http.StatusUnprocessableEntity, 42201, err.Error())
	case errors.Is(err, documentsvc.ErrParseQualityFailed):
		failure(c, http.StatusUnprocessableEntity, 42202, err.Error())
	case errors.Is(err, documentsvc.ErrInvalidFile), errors.Is(err, documentsvc.ErrInvalidInput):
		failure(c, http.StatusBadRequest, 40001, "invalid request")
	case errors.Is(err, documentsvc.ErrInvalidQualityJSON):
		failure(c, http.StatusBadRequest, 40005, err.Error())
	case errors.Is(err, documentsvc.ErrInvalidReviewAction):
		failure(c, http.StatusBadRequest, 40006, "invalid review action")
	case errors.Is(err, documentsvc.ErrCannotPublish):
		failure(c, http.StatusConflict, 40901, "document cannot be published")
	case errors.Is(err, documentsvc.ErrPublicationGate):
		failure(c, http.StatusConflict, 40903, "publication gate is not satisfied")
	case errors.Is(err, repository.ErrImmutable):
		failure(c, http.StatusConflict, 40904, "document version state is immutable")
	case errors.Is(err, documentsvc.ErrChunkSetExists):
		failure(c, http.StatusConflict, 40902, err.Error())
	case errors.Is(err, documentsvc.ErrAdminRequired):
		failure(c, http.StatusForbidden, 40304, "admin role required")
	case errors.Is(err, documentsvc.ErrForbidden):
		failure(c, http.StatusForbidden, 40303, "document access forbidden")
	case errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40401, "document not found")
	default:
		failure(c, http.StatusInternalServerError, 50040, fallback)
	}
	return true
}

func toDocumentVersionResponse(version *model.KBDocumentVersion) documentVersionResponse {
	if version == nil {
		return documentVersionResponse{}
	}
	return documentVersionResponse{
		ID: version.ID, DocumentID: version.DocumentID, Version: version.Version, RevisionNo: version.RevisionNo,
		FileName: version.FileName, FileType: version.FileType, FileHash: version.FileHash,
		ParserName: version.ParserName, ParserVersion: version.ParserVersion, Language: version.Language,
		Status: version.Status, Metadata: json.RawMessage(version.Metadata), ParseQuality: json.RawMessage(version.ParseQuality), DocumentSchema: json.RawMessage(version.DocumentSchema),
		CreatedBy: version.CreatedBy, CreatedAt: version.CreatedAt.Format(time.RFC3339), UpdatedAt: version.UpdatedAt.Format(time.RFC3339),
		PublishedAt: formatOptionalTime(version.PublishedAt), SupersededAt: formatOptionalTime(version.SupersededAt), DeprecatedAt: formatOptionalTime(version.DeprecatedAt),
	}
}

func toParsedStructureResponse(structure *documentsvc.ParsedStructure) parsedStructureResponse {
	if structure == nil {
		return parsedStructureResponse{}
	}
	return parsedStructureResponse{
		Version: toDocumentVersionResponse(&structure.Version), ParseQuality: structure.ParseQuality, DocumentSchema: structure.DocumentSchema,
		Warnings: structure.Warnings, Blocks: structure.Blocks,
	}
}

func toDocumentResponse(document *model.KBDocument) documentResponse {
	return documentResponse{
		ID:                        document.ID,
		Title:                     document.Title,
		FileName:                  document.FileName,
		FileType:                  document.FileType,
		SystemName:                document.SystemName,
		ComponentName:             document.ComponentName,
		Environment:               document.Environment,
		DocType:                   document.DocType,
		Version:                   document.Version,
		Status:                    document.Status,
		Tags:                      rawJSON(document.Tags),
		Summary:                   document.Summary,
		QualityScore:              document.QualityScore,
		CreatedBy:                 document.CreatedBy,
		CreatedAt:                 document.CreatedAt.Format(time.RFC3339),
		UpdatedAt:                 document.UpdatedAt.Format(time.RFC3339),
		CurrentPublishedVersionID: document.CurrentPublishedVersionID,
	}
}

func formatOptionalTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.Format(time.RFC3339)
	return &formatted
}

func toQualityStandardResponse(standard *model.KBQualityStandard) qualityStandardResponse {
	return qualityStandardResponse{
		ID:        standard.ID,
		Title:     standard.Title,
		FileName:  standard.FileName,
		FileType:  standard.FileType,
		Enabled:   standard.Enabled,
		CreatedBy: standard.CreatedBy,
		CreatedAt: standard.CreatedAt.Format(time.RFC3339),
		Preview:   snippetText(standard.Content, 180),
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

func snippetText(content string, limit int) string {
	runes := []rune(content)
	if len(runes) <= limit {
		return content
	}
	return string(runes[:limit]) + "..."
}
