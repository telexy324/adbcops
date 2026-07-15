package document

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	llmsvc "aiops-platform/backend/internal/llm"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

const (
	maxTitleBytes       = 255
	maxFileNameBytes    = 255
	maxSystemNameBytes  = 100
	maxEnvironmentBytes = 50
	defaultVersion      = "v1.0"
)

var (
	ErrForbidden           = errors.New("document access forbidden")
	ErrAdminRequired       = errors.New("admin role required")
	ErrInvalidInput        = errors.New("invalid input")
	ErrInvalidFile         = errors.New("invalid file")
	ErrFileTooLarge        = errors.New("file too large")
	ErrPathTraversal       = errors.New("file path traversal is not allowed")
	ErrUnsupportedExt      = errors.New("unsupported file type")
	ErrInvalidReviewAction = errors.New("invalid review action")
	ErrCannotPublish       = errors.New("document cannot be published")
)

type Repository interface {
	CreateDocument(ctx context.Context, document *model.KBDocument) error
	ListDocuments(ctx context.Context, userID *int64) ([]model.KBDocument, error)
	FindDocumentByID(ctx context.Context, id int64) (*model.KBDocument, error)
	ReplaceDocumentChunks(ctx context.Context, documentID int64, chunks []model.KBChunk) error
	ListDocumentChunks(ctx context.Context, documentID int64) ([]model.KBChunk, error)
	UpdateDocumentQuality(ctx context.Context, id int64, score int, result []byte, status string) (*model.KBDocument, error)
	RecordDocumentReview(ctx context.Context, id int64, reviewerID int64, action, toStatus string, comment *string) (*model.KBDocument, error)
	CreateQualityStandard(ctx context.Context, standard *model.KBQualityStandard) error
	ListQualityStandards(ctx context.Context, enabledOnly bool) ([]model.KBQualityStandard, error)
	FindQualityStandardsByIDs(ctx context.Context, ids []int64) ([]model.KBQualityStandard, error)
	SearchChunks(ctx context.Context, query string, limit int) ([]model.KBChunk, error)
	FindDefaultEnabledLLMConfigByPurpose(ctx context.Context, purpose string) (*model.LLMConfig, error)
}

type SecretManager interface {
	Decrypt(value string) (string, error)
}

type Service struct {
	documents      Repository
	secrets        SecretManager
	llmClient      llmsvc.Client
	localFileDir   string
	maxUploadBytes int64
	chunkSize      int
	chunkOverlap   int
}

type UploadMetadata struct {
	Title         string
	SystemName    string
	ComponentName string
	Environment   string
	DocType       string
	Version       string
	Tags          json.RawMessage
}

type ReviewDecision struct {
	Action  string
	Comment string
}

type AutoQualityInput struct {
	UseDefault  bool
	StandardIDs []int64
}

func NewService(documents Repository, localFileDir string, maxUploadBytes int64, chunkSize, chunkOverlap int) (*Service, error) {
	if strings.TrimSpace(localFileDir) == "" {
		return nil, fmt.Errorf("local file dir is required")
	}
	if maxUploadBytes <= 0 {
		return nil, fmt.Errorf("max upload bytes must be positive")
	}
	if chunkSize <= 0 || chunkOverlap < 0 || chunkOverlap >= chunkSize {
		return nil, fmt.Errorf("invalid chunk settings")
	}
	return &Service{documents: documents, localFileDir: localFileDir, maxUploadBytes: maxUploadBytes, chunkSize: chunkSize, chunkOverlap: chunkOverlap}, nil
}

func (s *Service) WithQualityLLM(secrets SecretManager, client llmsvc.Client) *Service {
	s.secrets = secrets
	s.llmClient = client
	return s
}

func (s *Service) Upload(ctx context.Context, actor *model.AppUser, fileHeader *multipart.FileHeader, metadata UploadMetadata) (*model.KBDocument, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	if fileHeader == nil || fileHeader.Size <= 0 {
		return nil, ErrInvalidFile
	}
	if fileHeader.Size > s.maxUploadBytes {
		return nil, ErrFileTooLarge
	}
	originalName, err := normalizeFileName(fileHeader.Filename)
	if err != nil {
		return nil, err
	}
	fileType, ext, err := detectFileType(originalName)
	if err != nil {
		return nil, err
	}
	title, err := normalizeTitle(metadata.Title, originalName)
	if err != nil {
		return nil, err
	}
	systemName, err := optionalString(metadata.SystemName, maxSystemNameBytes)
	if err != nil {
		return nil, err
	}
	componentName, err := optionalString(metadata.ComponentName, maxSystemNameBytes)
	if err != nil {
		return nil, err
	}
	environment, err := optionalString(metadata.Environment, maxEnvironmentBytes)
	if err != nil {
		return nil, err
	}
	docType, err := optionalString(metadata.DocType, maxSystemNameBytes)
	if err != nil {
		return nil, err
	}
	version := strings.TrimSpace(metadata.Version)
	if version == "" {
		version = defaultVersion
	}
	version, err = normalizeRequired(version, maxEnvironmentBytes)
	if err != nil {
		return nil, err
	}
	tags, err := normalizeJSON(metadata.Tags)
	if err != nil {
		return nil, err
	}

	baseDir, err := ensureBaseDir(s.localFileDir)
	if err != nil {
		return nil, err
	}
	storedPath, err := s.newStoragePath(baseDir, ext)
	if err != nil {
		return nil, err
	}
	if err := saveMultipartFile(fileHeader, storedPath, s.maxUploadBytes); err != nil {
		return nil, err
	}

	createdBy := actor.ID
	document := &model.KBDocument{
		Title:         title,
		FileName:      originalName,
		FilePath:      storedPath,
		FileType:      fileType,
		SystemName:    systemName,
		ComponentName: componentName,
		Environment:   environment,
		DocType:       docType,
		Version:       version,
		Status:        model.DocumentStatusDraft,
		Tags:          tags,
		QualityScore:  0,
		CreatedBy:     &createdBy,
	}
	if err := s.documents.CreateDocument(ctx, document); err != nil {
		_ = os.Remove(storedPath)
		return nil, fmt.Errorf("create document record: %w", err)
	}
	return document, nil
}

func (s *Service) List(ctx context.Context, actor *model.AppUser) ([]model.KBDocument, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	var userID *int64
	if actor.Role != model.RoleAdmin {
		id := actor.ID
		userID = &id
	}
	return s.documents.ListDocuments(ctx, userID)
}

func (s *Service) Get(ctx context.Context, actor *model.AppUser, id int64) (*model.KBDocument, error) {
	if actor == nil || id <= 0 {
		return nil, ErrInvalidInput
	}
	document, err := s.documents.FindDocumentByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if actor.Role != model.RoleAdmin && (document.CreatedBy == nil || *document.CreatedBy != actor.ID) {
		return nil, ErrForbidden
	}
	return document, nil
}

func (s *Service) Reprocess(ctx context.Context, actor *model.AppUser, id int64) ([]model.KBChunk, error) {
	document, err := s.Get(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	content, err := ExtractText(document)
	if err != nil {
		return nil, err
	}
	chunks := BuildChunks(document, content, s.chunkSize, s.chunkOverlap)
	if len(chunks) == 0 {
		return nil, ErrInvalidFile
	}
	if err := s.documents.ReplaceDocumentChunks(ctx, document.ID, chunks); err != nil {
		return nil, fmt.Errorf("replace document chunks: %w", err)
	}
	return s.documents.ListDocumentChunks(ctx, document.ID)
}

func (s *Service) ListChunks(ctx context.Context, actor *model.AppUser, id int64) ([]model.KBChunk, error) {
	document, err := s.Get(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	return s.documents.ListDocumentChunks(ctx, document.ID)
}

func (s *Service) UploadQualityStandard(ctx context.Context, actor *model.AppUser, fileHeader *multipart.FileHeader, title string) (*model.KBQualityStandard, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	if actor.Role != model.RoleAdmin {
		return nil, ErrAdminRequired
	}
	if fileHeader == nil || fileHeader.Size <= 0 {
		return nil, ErrInvalidFile
	}
	if fileHeader.Size > s.maxUploadBytes {
		return nil, ErrFileTooLarge
	}
	originalName, err := normalizeFileName(fileHeader.Filename)
	if err != nil {
		return nil, err
	}
	fileType, ext, err := detectFileType(originalName)
	if err != nil {
		return nil, err
	}
	normalizedTitle, err := normalizeTitle(title, originalName)
	if err != nil {
		return nil, err
	}
	baseDir, err := ensureBaseDir(s.localFileDir)
	if err != nil {
		return nil, err
	}
	storedPath, err := s.newStoragePath(baseDir, ext)
	if err != nil {
		return nil, err
	}
	if err := saveMultipartFile(fileHeader, storedPath, s.maxUploadBytes); err != nil {
		return nil, err
	}
	content, err := ExtractTextFromFile(storedPath, fileType)
	if err != nil {
		_ = os.Remove(storedPath)
		return nil, err
	}
	if strings.TrimSpace(content) == "" {
		_ = os.Remove(storedPath)
		return nil, ErrInvalidFile
	}
	createdBy := actor.ID
	standard := &model.KBQualityStandard{
		Title:     normalizedTitle,
		FileName:  originalName,
		FilePath:  storedPath,
		FileType:  fileType,
		Content:   content,
		Enabled:   true,
		CreatedBy: &createdBy,
	}
	if err := s.documents.CreateQualityStandard(ctx, standard); err != nil {
		_ = os.Remove(storedPath)
		return nil, fmt.Errorf("create quality standard: %w", err)
	}
	return standard, nil
}

func (s *Service) ListQualityStandards(ctx context.Context, actor *model.AppUser) ([]model.KBQualityStandard, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	return s.documents.ListQualityStandards(ctx, true)
}

func (s *Service) ReviewQuality(ctx context.Context, actor *model.AppUser, id int64, rawResult json.RawMessage) (*model.KBDocument, QualityResult, error) {
	if actor == nil || id <= 0 {
		return nil, QualityResult{}, ErrInvalidInput
	}
	if actor.Role != model.RoleAdmin {
		return nil, QualityResult{}, ErrAdminRequired
	}
	document, err := s.documents.FindDocumentByID(ctx, id)
	if err != nil {
		return nil, QualityResult{}, err
	}
	result, normalized, err := ParseQualityResult(rawResult)
	if err != nil {
		return nil, QualityResult{}, err
	}
	status := StatusAfterQualityScore(result.Score)
	updated, err := s.documents.UpdateDocumentQuality(ctx, document.ID, result.Score, normalized, status)
	if err != nil {
		return nil, QualityResult{}, fmt.Errorf("update document quality: %w", err)
	}
	return updated, result, nil
}

func (s *Service) AutoReviewQuality(ctx context.Context, actor *model.AppUser, id int64, input AutoQualityInput) (*model.KBDocument, QualityResult, error) {
	if actor == nil || id <= 0 {
		return nil, QualityResult{}, ErrInvalidInput
	}
	if actor.Role != model.RoleAdmin {
		return nil, QualityResult{}, ErrAdminRequired
	}
	document, err := s.documents.FindDocumentByID(ctx, id)
	if err != nil {
		return nil, QualityResult{}, err
	}
	content, err := s.documentQualityContent(ctx, document)
	if err != nil {
		return nil, QualityResult{}, err
	}
	var standards []model.KBQualityStandard
	if len(input.StandardIDs) > 0 {
		standards, err = s.documents.FindQualityStandardsByIDs(ctx, input.StandardIDs)
		if err != nil {
			return nil, QualityResult{}, err
		}
	}
	result, err := s.buildAutoQualityResult(ctx, document, content, standards, input)
	if err != nil {
		return nil, QualityResult{}, err
	}
	normalized, err := json.Marshal(result)
	if err != nil {
		return nil, QualityResult{}, fmt.Errorf("normalize quality result: %w", err)
	}
	status := StatusAfterQualityScore(result.Score)
	updated, err := s.documents.UpdateDocumentQuality(ctx, document.ID, result.Score, normalized, status)
	if err != nil {
		return nil, QualityResult{}, fmt.Errorf("update document quality: %w", err)
	}
	return updated, result, nil
}

func (s *Service) buildAutoQualityResult(ctx context.Context, document *model.KBDocument, content string, standards []model.KBQualityStandard, input AutoQualityInput) (QualityResult, error) {
	result, llmReady, err := s.buildLLMQualityResult(ctx, document, content, standards, input.UseDefault)
	if err == nil && llmReady {
		return result, nil
	}
	fallback := BuildQualityResult(document, content, standards, input.UseDefault)
	if llmReady && err != nil {
		fallback.Source = "rule-based-fallback"
		fallback.Suggestions = append(fallback.Suggestions, "LLM 评分接口暂不可用，已按本地规则生成临时评分，请稍后重新发起自动评分。")
	}
	return fallback, nil
}

func (s *Service) buildLLMQualityResult(ctx context.Context, document *model.KBDocument, content string, standards []model.KBQualityStandard, useDefault bool) (QualityResult, bool, error) {
	if s.llmClient == nil {
		return QualityResult{}, false, nil
	}
	config, err := s.documents.FindDefaultEnabledLLMConfigByPurpose(ctx, model.LLMPurposeChat)
	if errors.Is(err, repository.ErrNotFound) {
		return QualityResult{}, false, nil
	}
	if err != nil {
		return QualityResult{}, false, fmt.Errorf("load default chat llm config: %w", err)
	}
	credential, err := s.decryptModelCredential(config)
	if err != nil {
		return QualityResult{}, true, err
	}
	response, err := s.llmClient.Chat(ctx, llmsvc.ChatRequest{
		BaseURL:     config.BaseURL,
		APIKey:      credential.APIKey,
		APISecret:   credential.APISecret,
		Model:       config.Model,
		Temperature: config.Temperature,
		Messages: []llmsvc.ChatMessage{
			{Role: "system", Content: QualityPrompt},
			{Role: "user", Content: BuildQualityLLMPrompt(document, content, standards, useDefault)},
		},
	})
	if err != nil {
		return QualityResult{}, true, fmt.Errorf("call quality llm: %w", err)
	}
	if response == nil || strings.TrimSpace(response.Content) == "" {
		return QualityResult{}, true, fmt.Errorf("quality llm returned empty content")
	}
	result, _, err := ParseQualityResult(json.RawMessage(extractJSONContent(response.Content)))
	if err != nil {
		return QualityResult{}, true, err
	}
	if result.Source == "" {
		result.Source = "llm"
	}
	if len(result.Standards) == 0 {
		result.Standards = selectedStandardNames(standards, useDefault)
	}
	return result, true, nil
}

type modelCredential struct {
	APIKey    string
	APISecret string
}

func (s *Service) decryptModelCredential(config *model.LLMConfig) (modelCredential, error) {
	if config == nil || s.secrets == nil {
		return modelCredential{}, nil
	}
	credential := modelCredential{}
	if config.APIKeyRef != nil && *config.APIKeyRef != "" {
		decrypted, err := s.secrets.Decrypt(*config.APIKeyRef)
		if err != nil {
			return modelCredential{}, fmt.Errorf("decrypt api key: %w", err)
		}
		credential.APIKey = decrypted
	}
	if config.APISecretRef != nil && *config.APISecretRef != "" {
		decrypted, err := s.secrets.Decrypt(*config.APISecretRef)
		if err != nil {
			return modelCredential{}, fmt.Errorf("decrypt api secret: %w", err)
		}
		credential.APISecret = decrypted
	}
	return credential, nil
}

func (s *Service) documentQualityContent(ctx context.Context, document *model.KBDocument) (string, error) {
	chunks, err := s.documents.ListDocumentChunks(ctx, document.ID)
	if err == nil && len(chunks) > 0 {
		var builder strings.Builder
		for _, chunk := range chunks {
			builder.WriteString(chunk.Content)
			builder.WriteString("\n")
		}
		return builder.String(), nil
	}
	return ExtractText(document)
}

func (s *Service) ReviewDecision(ctx context.Context, actor *model.AppUser, id int64, input ReviewDecision) (*model.KBDocument, error) {
	if actor == nil || id <= 0 {
		return nil, ErrInvalidInput
	}
	if actor.Role != model.RoleAdmin {
		return nil, ErrAdminRequired
	}
	document, err := s.documents.FindDocumentByID(ctx, id)
	if err != nil {
		return nil, err
	}
	action := strings.ToLower(strings.TrimSpace(input.Action))
	toStatus, err := reviewActionTargetStatus(action)
	if err != nil {
		return nil, err
	}
	if action == model.DocumentReviewActionPublish && !CanPublish(document) {
		return nil, ErrCannotPublish
	}
	comment := optionalReviewComment(input.Comment)
	return s.documents.RecordDocumentReview(ctx, document.ID, actor.ID, action, toStatus, comment)
}

func (s *Service) Search(ctx context.Context, actor *model.AppUser, query string, limit int) ([]model.KBChunk, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, ErrInvalidInput
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	return s.documents.SearchChunks(ctx, query, limit)
}

func reviewActionTargetStatus(action string) (string, error) {
	switch action {
	case model.DocumentReviewActionPublish:
		return model.DocumentStatusPublished, nil
	case model.DocumentReviewActionReject:
		return model.DocumentStatusRejected, nil
	case model.DocumentReviewActionArchive:
		return model.DocumentStatusArchived, nil
	case model.DocumentReviewActionDeprecate:
		return model.DocumentStatusDeprecated, nil
	default:
		return "", ErrInvalidReviewAction
	}
}

func optionalReviewComment(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeFileName(name string) (string, error) {
	normalized := strings.TrimSpace(name)
	if normalized == "" || len(normalized) > maxFileNameBytes || !utf8.ValidString(normalized) {
		return "", ErrInvalidFile
	}
	if normalized != filepath.Base(normalized) || strings.Contains(normalized, "/") || strings.Contains(normalized, "\\") || strings.Contains(normalized, "..") {
		return "", ErrPathTraversal
	}
	return normalized, nil
}

func detectFileType(name string) (string, string, error) {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".md":
		return model.DocumentFileTypeMarkdown, ext, nil
	case ".txt":
		return model.DocumentFileTypeText, ext, nil
	case ".docx":
		return model.DocumentFileTypeDocx, ext, nil
	case ".xlsx":
		return model.DocumentFileTypeXlsx, ext, nil
	default:
		return "", "", ErrUnsupportedExt
	}
}

func normalizeTitle(title, fallbackName string) (string, error) {
	if strings.TrimSpace(title) == "" {
		title = strings.TrimSuffix(fallbackName, filepath.Ext(fallbackName))
	}
	return normalizeRequired(title, maxTitleBytes)
}

func normalizeRequired(value string, maxBytes int) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" || len(normalized) > maxBytes || !utf8.ValidString(normalized) {
		return "", ErrInvalidInput
	}
	return normalized, nil
}

func optionalString(value string, maxBytes int) (*string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return nil, nil
	}
	if len(normalized) > maxBytes || !utf8.ValidString(normalized) {
		return nil, ErrInvalidInput
	}
	return &normalized, nil
}

func normalizeJSON(value json.RawMessage) ([]byte, error) {
	if len(value) == 0 || string(value) == "null" {
		return nil, nil
	}
	if !json.Valid(value) {
		return nil, ErrInvalidInput
	}
	normalized := make([]byte, len(value))
	copy(normalized, value)
	return normalized, nil
}

func ensureBaseDir(dir string) (string, error) {
	absolute, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return "", fmt.Errorf("resolve upload dir: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return "", fmt.Errorf("create upload dir: %w", err)
	}
	real, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve upload dir symlinks: %w", err)
	}
	return real, nil
}

func (s *Service) newStoragePath(baseDir, ext string) (string, error) {
	for attempts := 0; attempts < 5; attempts++ {
		token := make([]byte, 16)
		if _, err := rand.Read(token); err != nil {
			return "", fmt.Errorf("generate file name: %w", err)
		}
		candidate := filepath.Join(baseDir, hex.EncodeToString(token)+ext)
		if err := ensureUnderBase(baseDir, candidate); err != nil {
			return "", err
		}
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("generate unique file name: exhausted attempts")
}

func ensureUnderBase(baseDir, candidate string) error {
	relative, err := filepath.Rel(baseDir, candidate)
	if err != nil {
		return fmt.Errorf("check storage path: %w", err)
	}
	if relative == "." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || relative == ".." || filepath.IsAbs(relative) {
		return ErrPathTraversal
	}
	return nil
}

func saveMultipartFile(fileHeader *multipart.FileHeader, destination string, maxBytes int64) error {
	source, err := fileHeader.Open()
	if err != nil {
		return fmt.Errorf("open uploaded file: %w", err)
	}
	defer source.Close()

	target, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create stored file: %w", err)
	}
	written, copyErr := io.Copy(target, io.LimitReader(source, maxBytes+1))
	closeErr := target.Close()
	if copyErr != nil {
		_ = os.Remove(destination)
		return fmt.Errorf("write stored file: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(destination)
		return fmt.Errorf("close stored file: %w", closeErr)
	}
	if written > maxBytes {
		_ = os.Remove(destination)
		return ErrFileTooLarge
	}
	return nil
}

func IsNotFound(err error) bool {
	return errors.Is(err, repository.ErrNotFound)
}
