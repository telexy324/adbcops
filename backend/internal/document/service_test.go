package document

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"path/filepath"
	"strings"
	"testing"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

func TestUploadStoresAllowedFileWithCreator(t *testing.T) {
	store := newFakeRepository()
	dir := t.TempDir()
	service := newTestService(t, store, dir, 1024)
	fileHeader := newFileHeader(t, "guide.md", "# hello")
	actor := &model.AppUser{ID: 7, Role: model.RoleUser}

	document, err := service.Upload(context.Background(), actor, fileHeader, UploadMetadata{Title: "Guide"})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if document.CreatedBy == nil || *document.CreatedBy != actor.ID {
		t.Fatalf("CreatedBy = %v, want %d", document.CreatedBy, actor.ID)
	}
	if document.FileName != "guide.md" || document.FileType != model.DocumentFileTypeMarkdown {
		t.Fatalf("document file fields = %+v", document)
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks() error = %v", err)
	}
	relative, err := filepath.Rel(realDir, document.FilePath)
	if err != nil || strings.HasPrefix(relative, "..") || filepath.IsAbs(relative) {
		t.Fatalf("stored path %q escaped %q", document.FilePath, realDir)
	}
}

func TestReprocessTypicalMarkdownCreatesContinuousNonEmptyChunks(t *testing.T) {
	store := newFakeRepository()
	dir := t.TempDir()
	service := newTestServiceWithChunk(t, store, dir, 1024, 45, 8)
	actor := &model.AppUser{ID: 7, Role: model.RoleUser}
	content := "# 支付系统\n\n支付接口在高峰期需要关注延迟。\n\n## 排查步骤\n\n第一步查看错误率。第二步查看数据库连接池。第三步查看上游依赖。第四步确认发布记录。"
	document, err := service.Upload(context.Background(), actor, newFileHeader(t, "runbook.md", content), UploadMetadata{Title: "支付系统 Runbook"})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	chunks, err := service.Reprocess(context.Background(), actor, document.ID)
	if err != nil {
		t.Fatalf("Reprocess() error = %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("chunk count = %d, want at least 2", len(chunks))
	}
	for index, chunk := range chunks {
		if chunk.ChunkIndex != index {
			t.Fatalf("chunk index at %d = %d", index, chunk.ChunkIndex)
		}
		if strings.TrimSpace(chunk.Content) == "" {
			t.Fatalf("chunk %d is empty", index)
		}
		if chunk.SourceTitle == nil || *chunk.SourceTitle != "支付系统 Runbook" {
			t.Fatalf("chunk %d source title = %v", index, chunk.SourceTitle)
		}
	}
	if chunks[0].SourceSection == nil || *chunks[0].SourceSection != "支付系统" {
		t.Fatalf("first source section = %v", chunks[0].SourceSection)
	}
}

func TestUploadRejectsUnsupportedFileType(t *testing.T) {
	service := newTestService(t, newFakeRepository(), t.TempDir(), 1024)
	_, err := service.Upload(context.Background(), &model.AppUser{ID: 1}, newFileHeader(t, "bad.pdf", "pdf"), UploadMetadata{})
	if err != ErrUnsupportedExt {
		t.Fatalf("Upload() error = %v, want ErrUnsupportedExt", err)
	}
}

func TestUploadRejectsPathTraversalFileName(t *testing.T) {
	_, err := normalizeFileName("../bad.md")
	if err != ErrPathTraversal {
		t.Fatalf("normalizeFileName() error = %v, want ErrPathTraversal", err)
	}
}

func TestUploadRejectsOversizedFile(t *testing.T) {
	service := newTestService(t, newFakeRepository(), t.TempDir(), 4)
	_, err := service.Upload(context.Background(), &model.AppUser{ID: 1}, newFileHeader(t, "big.txt", "12345"), UploadMetadata{})
	if err != ErrFileTooLarge {
		t.Fatalf("Upload() error = %v, want ErrFileTooLarge", err)
	}
}

func TestReviewQualityRejectsInvalidJSONWithClearError(t *testing.T) {
	_, _, err := ParseQualityResult(json.RawMessage(`{"score":101,"summary":"bad","findings":["x"],"suggestions":["y"]}`))
	if !errors.Is(err, ErrInvalidQualityJSON) || !strings.Contains(err.Error(), "score must be from 0 to 100") {
		t.Fatalf("ParseQualityResult() error = %v", err)
	}
}

func TestReviewQualitySetsStatusByScore(t *testing.T) {
	store := newFakeRepository()
	service := newTestService(t, store, t.TempDir(), 1024)
	actor := &model.AppUser{ID: 7, Role: model.RoleUser}
	document, err := service.Upload(context.Background(), actor, newFileHeader(t, "guide.md", "# hello"), UploadMetadata{Title: "Guide"})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	low, _, err := service.ReviewQuality(context.Background(), actor, document.ID, json.RawMessage(`{"score":69,"summary":"thin","findings":["missing owner"],"suggestions":["add owner"]}`))
	if err != nil {
		t.Fatalf("ReviewQuality(low) error = %v", err)
	}
	if low.Status != model.DocumentStatusRejected || low.QualityScore != 69 || CanPublish(low) {
		t.Fatalf("low-quality document = %+v, canPublish=%v", low, CanPublish(low))
	}

	high, _, err := service.ReviewQuality(context.Background(), actor, document.ID, json.RawMessage(`{"score":70,"summary":"ok","findings":["clear scope"],"suggestions":["keep updated"]}`))
	if err != nil {
		t.Fatalf("ReviewQuality(high) error = %v", err)
	}
	if high.Status != model.DocumentStatusReviewing || high.QualityScore != 70 || !CanPublish(high) {
		t.Fatalf("high-quality document = %+v, canPublish=%v", high, CanPublish(high))
	}
}

func newTestService(t *testing.T, store *fakeRepository, dir string, maxBytes int64) *Service {
	t.Helper()
	return newTestServiceWithChunk(t, store, dir, maxBytes, 80, 10)
}

func newTestServiceWithChunk(t *testing.T, store *fakeRepository, dir string, maxBytes int64, chunkSize, overlap int) *Service {
	t.Helper()
	service, err := NewService(store, dir, maxBytes, chunkSize, overlap)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func newFileHeader(t *testing.T, name, content string) *multipart.FileHeader {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	reader := multipart.NewReader(&body, writer.Boundary())
	form, err := reader.ReadForm(1024)
	if err != nil {
		t.Fatalf("ReadForm() error = %v", err)
	}
	files := form.File["file"]
	if len(files) != 1 {
		t.Fatal("multipart form did not contain file")
	}
	return files[0]
}

type fakeRepository struct {
	nextID      int64
	nextChunkID int64
	documents   map[int64]*model.KBDocument
	chunks      map[int64][]model.KBChunk
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{nextID: 1, nextChunkID: 1, documents: make(map[int64]*model.KBDocument), chunks: make(map[int64][]model.KBChunk)}
}

func (f *fakeRepository) CreateDocument(_ context.Context, document *model.KBDocument) error {
	document.ID = f.nextID
	f.nextID++
	f.documents[document.ID] = document
	return nil
}

func (f *fakeRepository) ListDocuments(_ context.Context, userID *int64) ([]model.KBDocument, error) {
	documents := make([]model.KBDocument, 0, len(f.documents))
	for _, document := range f.documents {
		if userID == nil || (document.CreatedBy != nil && *document.CreatedBy == *userID) {
			documents = append(documents, *document)
		}
	}
	return documents, nil
}

func (f *fakeRepository) FindDocumentByID(_ context.Context, id int64) (*model.KBDocument, error) {
	document, ok := f.documents[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return document, nil
}

func (f *fakeRepository) ReplaceDocumentChunks(_ context.Context, documentID int64, chunks []model.KBChunk) error {
	if _, ok := f.documents[documentID]; !ok {
		return repository.ErrNotFound
	}
	stored := make([]model.KBChunk, len(chunks))
	for index := range chunks {
		chunks[index].ID = f.nextChunkID
		f.nextChunkID++
		stored[index] = chunks[index]
	}
	f.chunks[documentID] = stored
	return nil
}

func (f *fakeRepository) ListDocumentChunks(_ context.Context, documentID int64) ([]model.KBChunk, error) {
	return append([]model.KBChunk(nil), f.chunks[documentID]...), nil
}

func (f *fakeRepository) UpdateDocumentQuality(_ context.Context, id int64, score int, result []byte, status string) (*model.KBDocument, error) {
	document, ok := f.documents[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	document.QualityScore = score
	document.QualityResult = append([]byte(nil), result...)
	document.Status = status
	return document, nil
}
