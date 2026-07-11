package document

import (
	"bytes"
	"context"
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

func newTestService(t *testing.T, store *fakeRepository, dir string, maxBytes int64) *Service {
	t.Helper()
	service, err := NewService(store, dir, maxBytes)
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
	nextID    int64
	documents map[int64]*model.KBDocument
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{nextID: 1, documents: make(map[int64]*model.KBDocument)}
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
