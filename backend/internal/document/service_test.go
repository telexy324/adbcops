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

	llmsvc "aiops-platform/backend/internal/llm"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
	docx "github.com/fumiama/go-docx"
	"github.com/xuri/excelize/v2"
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
		if chunk.Summary == nil || strings.TrimSpace(*chunk.Summary) == "" {
			t.Fatalf("chunk %d summary is empty", index)
		}
		if chunk.SearchText == nil || strings.TrimSpace(*chunk.SearchText) == "" {
			t.Fatalf("chunk %d search text is empty", index)
		}
		if len(chunk.Keywords) == 0 || string(chunk.Keywords) == "[]" {
			t.Fatalf("chunk %d keywords are empty", index)
		}
		if len(chunk.PossibleQuestions) == 0 || string(chunk.PossibleQuestions) == "[]" {
			t.Fatalf("chunk %d possible questions are empty", index)
		}
		if chunk.SourceTitle == nil || *chunk.SourceTitle != "支付系统 Runbook" {
			t.Fatalf("chunk %d source title = %v", index, chunk.SourceTitle)
		}
	}
	if chunks[0].SourceSection == nil || *chunks[0].SourceSection != "支付系统" {
		t.Fatalf("first source section = %v", chunks[0].SourceSection)
	}

	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	if _, _, err := service.ReviewQuality(context.Background(), admin, document.ID, json.RawMessage(`{"score":70,"summary":"ok","findings":["clear scope"],"suggestions":["keep updated"]}`)); err != nil {
		t.Fatalf("ReviewQuality() error = %v", err)
	}
	if _, err := service.ReviewDecision(context.Background(), admin, document.ID, ReviewDecision{Action: model.DocumentReviewActionPublish}); err != nil {
		t.Fatalf("ReviewDecision(publish) error = %v", err)
	}
	results, err := service.Search(context.Background(), actor, "数据库连接池", 5)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Search() returned no chunks")
	}
	if !strings.Contains(results[0].Content, "数据库连接池") && (results[0].SearchText == nil || !strings.Contains(*results[0].SearchText, "数据库连接池")) {
		t.Fatalf("top result did not recall database pool chunk: %+v", results[0])
	}
}

func TestReprocessDocxExtractsParagraphText(t *testing.T) {
	store := newFakeRepository()
	service := newTestServiceWithChunk(t, store, t.TempDir(), 4096, 80, 10)
	actor := &model.AppUser{ID: 7, Role: model.RoleUser}
	document, err := service.Upload(context.Background(), actor, newBytesFileHeader(t, "runbook.docx", minimalDocx(t, []string{
		"支付系统排障手册",
		"数据库连接池耗尽时先查看活跃连接和慢查询。",
	})), UploadMetadata{Title: "Docx Runbook"})
	if err != nil {
		t.Fatalf("Upload(docx) error = %v", err)
	}
	if document.FileType != model.DocumentFileTypeDocx {
		t.Fatalf("FileType = %q, want docx", document.FileType)
	}

	chunks, err := service.Reprocess(context.Background(), actor, document.ID)
	if err != nil {
		t.Fatalf("Reprocess(docx) error = %v", err)
	}
	if len(chunks) == 0 || !strings.Contains(chunks[0].Content, "数据库连接池") {
		t.Fatalf("docx chunks = %+v", chunks)
	}
}

func TestReprocessXlsxExtractsWorksheetText(t *testing.T) {
	store := newFakeRepository()
	service := newTestServiceWithChunk(t, store, t.TempDir(), 16384, 80, 10)
	actor := &model.AppUser{ID: 7, Role: model.RoleUser}
	document, err := service.Upload(context.Background(), actor, newBytesFileHeader(t, "capacity.xlsx", minimalXlsx(t, [][]string{
		{"系统", "指标", "阈值"},
		{"支付系统", "数据库连接池", "80%"},
	})), UploadMetadata{Title: "Xlsx Capacity"})
	if err != nil {
		t.Fatalf("Upload(xlsx) error = %v", err)
	}
	if document.FileType != model.DocumentFileTypeXlsx {
		t.Fatalf("FileType = %q, want xlsx", document.FileType)
	}

	chunks, err := service.Reprocess(context.Background(), actor, document.ID)
	if err != nil {
		t.Fatalf("Reprocess(xlsx) error = %v", err)
	}
	joined := ""
	for _, chunk := range chunks {
		joined += chunk.Content
	}
	if !strings.Contains(joined, "支付系统") || !strings.Contains(joined, "数据库连接池") {
		t.Fatalf("xlsx chunks = %+v", chunks)
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
	_, err = normalizeFileName(`..\bad.md`)
	if err != ErrPathTraversal {
		t.Fatalf("normalizeFileName(windows) error = %v, want ErrPathTraversal", err)
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
	owner := &model.AppUser{ID: 7, Role: model.RoleUser}
	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	document, err := service.Upload(context.Background(), owner, newFileHeader(t, "guide.md", "# hello"), UploadMetadata{Title: "Guide"})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	if _, _, err := service.ReviewQuality(context.Background(), owner, document.ID, json.RawMessage(`{"score":70,"summary":"ok","findings":["clear scope"],"suggestions":["keep updated"]}`)); !errors.Is(err, ErrAdminRequired) {
		t.Fatalf("ReviewQuality(non-admin) error = %v, want ErrAdminRequired", err)
	}

	low, _, err := service.ReviewQuality(context.Background(), admin, document.ID, json.RawMessage(`{"score":69,"summary":"thin","findings":["missing owner"],"suggestions":["add owner"]}`))
	if err != nil {
		t.Fatalf("ReviewQuality(low) error = %v", err)
	}
	if low.Status != model.DocumentStatusRejected || low.QualityScore != 69 || CanPublish(low) {
		t.Fatalf("low-quality document = %+v, canPublish=%v", low, CanPublish(low))
	}

	high, _, err := service.ReviewQuality(context.Background(), admin, document.ID, json.RawMessage(`{"score":70,"summary":"ok","findings":["clear scope"],"suggestions":["keep updated"]}`))
	if err != nil {
		t.Fatalf("ReviewQuality(high) error = %v", err)
	}
	if high.Status != model.DocumentStatusReviewing || high.QualityScore != 70 || !CanPublish(high) {
		t.Fatalf("high-quality document = %+v, canPublish=%v", high, CanPublish(high))
	}
}

func TestAutoReviewQualityUsesDefaultAndCustomStandards(t *testing.T) {
	store := newFakeRepository()
	service := newTestServiceWithChunk(t, store, t.TempDir(), 4096, 120, 10)
	owner := &model.AppUser{ID: 7, Role: model.RoleUser}
	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	document, err := service.Upload(context.Background(), owner, newFileHeader(t, "guide.md", "# 支付系统\n\n范围：生产环境 payment-api 组件 v1.0。\n\n步骤：检查日志、告警、错误、延迟、指标和数据库连接池，执行命令并确认风险、影响、应急恢复、降级与回滚方案。\n\n负责人：SRE，更新时间：2026-07-13，维护链接和参考变更记录。"), UploadMetadata{Title: "Guide"})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if _, err := service.Reprocess(context.Background(), owner, document.ID); err != nil {
		t.Fatalf("Reprocess() error = %v", err)
	}
	standard, err := service.UploadQualityStandard(context.Background(), admin, newFileHeader(t, "standard.md", "- 必须包含连接池\n- 必须包含回滚方案"), "DB 标准")
	if err != nil {
		t.Fatalf("UploadQualityStandard() error = %v", err)
	}

	updated, result, err := service.AutoReviewQuality(context.Background(), admin, document.ID, AutoQualityInput{UseDefault: true, StandardIDs: []int64{standard.ID}})
	if err != nil {
		t.Fatalf("AutoReviewQuality() error = %v", err)
	}
	if updated.QualityScore != result.Score || result.Score < 70 || updated.Status != model.DocumentStatusReviewing {
		t.Fatalf("updated=%+v result=%+v", updated, result)
	}
	if len(result.CriteriaScores) == 0 || !containsString(result.Standards, "DB 标准") || !containsString(result.Standards, "default") {
		t.Fatalf("quality result missing standards/criteria: %+v", result)
	}
}

func TestAutoReviewQualityUsesLLMWhenDefaultChatConfigExists(t *testing.T) {
	store := newFakeRepository()
	store.llmConfig = &model.LLMConfig{
		ID:          10,
		BaseURL:     "https://llm.example",
		Model:       "quality-model",
		Purpose:     model.LLMPurposeChat,
		Temperature: 0.1,
		Enabled:     true,
		IsDefault:   true,
	}
	client := &fakeLLMClient{
		content: `{"score":88,"summary":"符合评分标准","findings":["包含回滚方案"],"suggestions":["补充演练记录"],"criteriaScores":[{"name":"回滚方案","score":90,"matched":["回滚"],"missing":["演练"],"standard":"DB 标准"}],"standards":["default","DB 标准"],"source":"llm"}`,
	}
	service := newTestServiceWithChunk(t, store, t.TempDir(), 4096, 120, 10).WithQualityLLM(nil, client)
	owner := &model.AppUser{ID: 7, Role: model.RoleUser}
	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	document, err := service.Upload(context.Background(), owner, newFileHeader(t, "guide.md", "# 支付系统\n\n检查连接池并执行回滚方案。"), UploadMetadata{Title: "Guide"})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if _, err := service.Reprocess(context.Background(), owner, document.ID); err != nil {
		t.Fatalf("Reprocess() error = %v", err)
	}
	standard, err := service.UploadQualityStandard(context.Background(), admin, newFileHeader(t, "standard.md", "- 必须包含连接池\n- 必须包含回滚方案"), "DB 标准")
	if err != nil {
		t.Fatalf("UploadQualityStandard() error = %v", err)
	}

	updated, result, err := service.AutoReviewQuality(context.Background(), admin, document.ID, AutoQualityInput{UseDefault: true, StandardIDs: []int64{standard.ID}})
	if err != nil {
		t.Fatalf("AutoReviewQuality() error = %v", err)
	}
	if client.calls != 1 {
		t.Fatalf("llm calls = %d, want 1", client.calls)
	}
	if !strings.Contains(client.lastRequest.Messages[1].Content, "DB 标准") || !strings.Contains(client.lastRequest.Messages[1].Content, "default") {
		t.Fatalf("llm prompt missing standards: %s", client.lastRequest.Messages[1].Content)
	}
	if updated.QualityScore != 88 || result.Score != 88 || result.Source != "llm" || updated.Status != model.DocumentStatusReviewing {
		t.Fatalf("updated=%+v result=%+v", updated, result)
	}
}

func TestReviewDecisionRequiresAdminAndPublishableDocument(t *testing.T) {
	store := newFakeRepository()
	service := newTestService(t, store, t.TempDir(), 1024)
	owner := &model.AppUser{ID: 7, Role: model.RoleUser}
	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	document, err := service.Upload(context.Background(), owner, newFileHeader(t, "guide.md", "# hello"), UploadMetadata{Title: "Guide"})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if _, err := service.ReviewDecision(context.Background(), owner, document.ID, ReviewDecision{Action: model.DocumentReviewActionPublish}); !errors.Is(err, ErrAdminRequired) {
		t.Fatalf("ReviewDecision(non-admin) error = %v, want ErrAdminRequired", err)
	}
	if _, err := service.ReviewDecision(context.Background(), admin, document.ID, ReviewDecision{Action: model.DocumentReviewActionPublish}); !errors.Is(err, ErrCannotPublish) {
		t.Fatalf("ReviewDecision(draft publish) error = %v, want ErrCannotPublish", err)
	}

	reviewing, _, err := service.ReviewQuality(context.Background(), admin, document.ID, json.RawMessage(`{"score":70,"summary":"ok","findings":["clear scope"],"suggestions":["keep updated"]}`))
	if err != nil {
		t.Fatalf("ReviewQuality() error = %v", err)
	}
	if !CanPublish(reviewing) {
		t.Fatalf("reviewing document should be publishable: %+v", reviewing)
	}
	published, err := service.ReviewDecision(context.Background(), admin, document.ID, ReviewDecision{Action: model.DocumentReviewActionPublish, Comment: " approved "})
	if err != nil {
		t.Fatalf("ReviewDecision(publish) error = %v", err)
	}
	if published.Status != model.DocumentStatusPublished || published.ReviewedBy == nil || *published.ReviewedBy != admin.ID {
		t.Fatalf("published document = %+v", published)
	}
	if len(store.reviews) != 1 || store.reviews[0].Action != model.DocumentReviewActionPublish || store.reviews[0].ToStatus != model.DocumentStatusPublished {
		t.Fatalf("review records = %+v", store.reviews)
	}
}

func TestSearchOnlyReturnsPublishedDocumentChunks(t *testing.T) {
	store := newFakeRepository()
	service := newTestServiceWithChunk(t, store, t.TempDir(), 1024, 50, 5)
	owner := &model.AppUser{ID: 7, Role: model.RoleUser}
	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	draft, err := service.Upload(context.Background(), owner, newFileHeader(t, "draft.md", "数据库连接池 draft"), UploadMetadata{Title: "Draft"})
	if err != nil {
		t.Fatalf("Upload(draft) error = %v", err)
	}
	if _, err := service.Reprocess(context.Background(), owner, draft.ID); err != nil {
		t.Fatalf("Reprocess(draft) error = %v", err)
	}
	published, err := service.Upload(context.Background(), owner, newFileHeader(t, "published.md", "数据库连接池 published"), UploadMetadata{Title: "Published"})
	if err != nil {
		t.Fatalf("Upload(published) error = %v", err)
	}
	if _, err := service.Reprocess(context.Background(), owner, published.ID); err != nil {
		t.Fatalf("Reprocess(published) error = %v", err)
	}
	if _, _, err := service.ReviewQuality(context.Background(), admin, published.ID, json.RawMessage(`{"score":70,"summary":"ok","findings":["clear scope"],"suggestions":["keep updated"]}`)); err != nil {
		t.Fatalf("ReviewQuality() error = %v", err)
	}
	if _, err := service.ReviewDecision(context.Background(), admin, published.ID, ReviewDecision{Action: model.DocumentReviewActionPublish}); err != nil {
		t.Fatalf("ReviewDecision(publish) error = %v", err)
	}

	results, err := service.Search(context.Background(), owner, "数据库连接池", 10)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Search() returned no published chunks")
	}
	for _, result := range results {
		if result.DocumentID != published.ID {
			t.Fatalf("Search() returned unpublished chunk: %+v", result)
		}
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
	return newBytesFileHeader(t, name, []byte(content))
}

func newBytesFileHeader(t *testing.T, name string, content []byte) *multipart.FileHeader {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := part.Write(content); err != nil {
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

func minimalDocx(t *testing.T, paragraphs []string) []byte {
	t.Helper()
	document := docx.New()
	for _, paragraph := range paragraphs {
		document.AddParagraph().AddText(paragraph)
	}
	var body bytes.Buffer
	if _, err := document.WriteTo(&body); err != nil {
		t.Fatalf("write docx: %v", err)
	}
	return body.Bytes()
}

func minimalXlsx(t *testing.T, rows [][]string) []byte {
	t.Helper()
	file := excelize.NewFile()
	defer file.Close()
	sheetName := "Sheet1"
	for rowIndex, row := range rows {
		for columnIndex, cell := range row {
			cellName, err := excelize.CoordinatesToCellName(columnIndex+1, rowIndex+1)
			if err != nil {
				t.Fatalf("cell coordinates: %v", err)
			}
			if err := file.SetCellValue(sheetName, cellName, cell); err != nil {
				t.Fatalf("set xlsx cell %s: %v", cellName, err)
			}
		}
	}
	var body bytes.Buffer
	if err := file.Write(&body); err != nil {
		t.Fatalf("write xlsx: %v", err)
	}
	return body.Bytes()
}

type fakeRepository struct {
	nextID         int64
	nextChunkID    int64
	nextStandardID int64
	documents      map[int64]*model.KBDocument
	chunks         map[int64][]model.KBChunk
	standards      map[int64]*model.KBQualityStandard
	llmConfig      *model.LLMConfig
	reviews        []model.KBDocumentReview
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		nextID:         1,
		nextChunkID:    1,
		nextStandardID: 1,
		documents:      make(map[int64]*model.KBDocument),
		chunks:         make(map[int64][]model.KBChunk),
		standards:      make(map[int64]*model.KBQualityStandard),
	}
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

func (f *fakeRepository) RecordDocumentReview(_ context.Context, id int64, reviewerID int64, action, toStatus string, comment *string) (*model.KBDocument, error) {
	document, ok := f.documents[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	f.reviews = append(f.reviews, model.KBDocumentReview{
		ID:         int64(len(f.reviews) + 1),
		DocumentID: id,
		ReviewerID: reviewerID,
		Action:     action,
		FromStatus: document.Status,
		ToStatus:   toStatus,
		Comment:    comment,
	})
	document.Status = toStatus
	document.ReviewedBy = &reviewerID
	return document, nil
}

func (f *fakeRepository) CreateQualityStandard(_ context.Context, standard *model.KBQualityStandard) error {
	standard.ID = f.nextStandardID
	f.nextStandardID++
	f.standards[standard.ID] = standard
	return nil
}

func (f *fakeRepository) ListQualityStandards(_ context.Context, enabledOnly bool) ([]model.KBQualityStandard, error) {
	standards := make([]model.KBQualityStandard, 0, len(f.standards))
	for _, standard := range f.standards {
		if !enabledOnly || standard.Enabled {
			standards = append(standards, *standard)
		}
	}
	return standards, nil
}

func (f *fakeRepository) FindQualityStandardsByIDs(_ context.Context, ids []int64) ([]model.KBQualityStandard, error) {
	standards := make([]model.KBQualityStandard, 0, len(ids))
	for _, id := range ids {
		standard, ok := f.standards[id]
		if !ok || !standard.Enabled {
			return nil, repository.ErrNotFound
		}
		standards = append(standards, *standard)
	}
	return standards, nil
}

func (f *fakeRepository) FindDefaultEnabledLLMConfigByPurpose(_ context.Context, purpose string) (*model.LLMConfig, error) {
	if f.llmConfig == nil || !f.llmConfig.Enabled || !f.llmConfig.IsDefault || f.llmConfig.Purpose != purpose {
		return nil, repository.ErrNotFound
	}
	config := *f.llmConfig
	return &config, nil
}

func (f *fakeRepository) SearchChunks(_ context.Context, query string, limit int) ([]model.KBChunk, error) {
	var results []model.KBChunk
	for documentID, chunks := range f.chunks {
		document, ok := f.documents[documentID]
		if !ok || document.Status != model.DocumentStatusPublished {
			continue
		}
		for _, chunk := range chunks {
			searchText := ""
			if chunk.SearchText != nil {
				searchText = *chunk.SearchText
			}
			if strings.Contains(chunk.Content, query) || strings.Contains(searchText, query) {
				results = append(results, chunk)
				if len(results) >= limit {
					return results, nil
				}
			}
		}
	}
	return results, nil
}

type fakeLLMClient struct {
	content     string
	calls       int
	lastRequest llmsvc.ChatRequest
}

func (f *fakeLLMClient) Chat(_ context.Context, request llmsvc.ChatRequest) (*llmsvc.ChatResult, error) {
	f.calls++
	f.lastRequest = request
	return &llmsvc.ChatResult{Content: f.content, Model: request.Model}, nil
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
