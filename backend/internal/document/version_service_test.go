package document

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"aiops-platform/backend/internal/model"
)

func TestDocumentVersionPersistsTraceableASTAndStableOrder(t *testing.T) {
	store := newFakeRepository()
	service := newTestService(t, store, t.TempDir(), 4096)
	owner := &model.AppUser{ID: 7, Role: model.RoleUser}
	document, err := service.Upload(context.Background(), owner, newFileHeader(t, "runbook.md", "# Recovery\n\n## Checks\n\n- inspect pods\n\n| Item | Value |\n| --- | --- |\n| API | healthy |"), UploadMetadata{Title: "Runbook", Version: "v2.0"})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	version, err := service.GetLatestDocumentVersion(context.Background(), owner, document.ID)
	if err != nil {
		t.Fatalf("GetLatestDocumentVersion() error = %v", err)
	}
	if version.RevisionNo != 1 || version.Version != "v2.0" || len(version.FileHash) != 64 || version.FilePath == "" {
		t.Fatalf("initial version = %+v", version)
	}

	parsed, err := service.ParseDocumentVersion(context.Background(), owner, version.ID)
	if err != nil {
		t.Fatalf("ParseDocumentVersion() error = %v", err)
	}
	if !parsed.ParseQuality.ParseSuccess || parsed.ParseQuality.BlockCount < 5 || parsed.Version.Status == model.DocumentVersionStatusFailed {
		t.Fatalf("parsed structure = %+v", parsed)
	}
	assertStableBlockIdentity(t, parsed.Blocks, 0, map[string]bool{})

	stored, err := service.GetParsedStructure(context.Background(), owner, parsed.Version.ID)
	if err != nil {
		t.Fatalf("GetParsedStructure() error = %v", err)
	}
	if len(stored.Blocks) == 0 || stored.Blocks[1].SectionPath[0] != "Recovery" || stored.ParseQuality.BlockCount != parsed.ParseQuality.BlockCount {
		t.Fatalf("stored parsed structure = %+v", stored)
	}
}

func TestReparseCreatesRevisionAndPreservesHistoricalAST(t *testing.T) {
	store := newFakeRepository()
	service := newTestService(t, store, t.TempDir(), 4096)
	owner := &model.AppUser{ID: 7, Role: model.RoleUser}
	document, err := service.Upload(context.Background(), owner, newFileHeader(t, "guide.md", "# Guide\n\nOriginal content"), UploadMetadata{Title: "Guide"})
	if err != nil {
		t.Fatal(err)
	}
	initial, _ := service.GetLatestDocumentVersion(context.Background(), owner, document.ID)
	first, err := service.ParseDocumentVersion(context.Background(), owner, initial.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.ParseDocumentVersion(context.Background(), owner, initial.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Version.ID == second.Version.ID || second.Version.RevisionNo != 2 {
		t.Fatalf("parse revisions: first=%+v second=%+v", first.Version, second.Version)
	}
	historical, err := service.GetParsedStructure(context.Background(), owner, first.Version.ID)
	if err != nil || len(historical.Blocks) == 0 || historical.Blocks[0].Text != "Guide" {
		t.Fatalf("historical structure = %+v, error = %v", historical, err)
	}
	latest, _ := service.GetLatestDocumentVersion(context.Background(), owner, document.ID)
	if latest.ID != second.Version.ID {
		t.Fatalf("latest version ID = %d, want %d", latest.ID, second.Version.ID)
	}
}

func TestParseFailureIsPersistedAndBlocksFormalQualityReview(t *testing.T) {
	store := newFakeRepository()
	service := newTestService(t, store, t.TempDir(), 4096)
	owner := &model.AppUser{ID: 7, Role: model.RoleUser}
	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	document, err := service.Upload(context.Background(), owner, newFileHeader(t, "scan.pdf", "not a real pdf"), UploadMetadata{Title: "Scan"})
	if err != nil {
		t.Fatal(err)
	}
	version, _ := service.GetLatestDocumentVersion(context.Background(), owner, document.ID)
	parsed, err := service.ParseDocumentVersion(context.Background(), owner, version.ID)
	if err != nil {
		t.Fatalf("failed parse should be persisted as a result, error = %v", err)
	}
	if parsed.ParseQuality.ParseSuccess || parsed.Version.Status != model.DocumentVersionStatusFailed || !hasWarning(parsed.Warnings, "parse_failed") {
		t.Fatalf("failed parse result = %+v", parsed)
	}
	_, _, err = service.ReviewQuality(context.Background(), admin, document.ID, json.RawMessage(`{"score":90,"summary":"invalid","findings":[],"suggestions":[]}`))
	if !errors.Is(err, ErrParseQualityFailed) {
		t.Fatalf("ReviewQuality() error = %v, want ErrParseQualityFailed", err)
	}
}

func assertStableBlockIdentity(t *testing.T, blocks []DocumentBlock, previous int, seen map[string]bool) int {
	t.Helper()
	for _, block := range blocks {
		if block.Order <= previous || block.ID == "" || seen[block.ID] {
			t.Fatalf("unstable block identity: previous=%d block=%+v", previous, block)
		}
		seen[block.ID] = true
		previous = block.Order
		previous = assertStableBlockIdentity(t, block.Children, previous, seen)
	}
	return previous
}
