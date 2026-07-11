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
	SearchChunks(ctx context.Context, query string, limit int) ([]model.KBChunk, error)
}

type Service struct {
	documents      Repository
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
	content, err := os.ReadFile(document.FilePath)
	if err != nil {
		return nil, fmt.Errorf("read document file: %w", err)
	}
	chunks := BuildChunks(document, string(content), s.chunkSize, s.chunkOverlap)
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
