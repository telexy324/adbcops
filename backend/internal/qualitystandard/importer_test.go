package qualitystandard

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"testing"

	"aiops-platform/backend/internal/document"
	"github.com/xuri/excelize/v2"
)

func TestExcelImportCreatesAwaitingConfirmationDraftAndPreservesSource(t *testing.T) {
	repository := &stubRepository{}
	service := NewService(repository)
	if err := service.ConfigureImporter(t.TempDir(), 2<<20); err != nil {
		t.Fatalf("ConfigureImporter() error = %v", err)
	}
	header := xlsxFileHeader(t, "standard.xlsx", [][]string{
		excelHeaders(),
		{"runbook", "runbook", "completeness", "Completeness", "50%", "required_sections", "Required sections", "Sections must exist", "section_presence", "50", "", "true", "false", "high", `{"required":"block"}`, "", "", "", ""},
		{"runbook", "runbook", "safety", "Safety", "50%", "credential_exposure", "Credentials", "Credentials must not be exposed", "safety", "50", "50", "true", "true", "critical", `{"required":"block"}`, "", "check secrets", "safe", "password=secret"},
	})
	result, err := service.Import(context.Background(), 1, header, ImportOptions{Name: "Imported", Version: "v2.0"})
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Import.Status != "awaiting_confirmation" || result.Draft == nil || result.Draft.Status != "draft" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if !result.Validation.Valid || len(result.Draft.Profiles) != 1 || len(result.Draft.Profiles[0].Criteria) != 2 {
		t.Fatalf("invalid draft: %+v", result.Draft)
	}
	if result.Draft.Profiles[0].TotalScore != 100 || result.Draft.Profiles[0].Criteria[0].Weight != 0.5 {
		t.Fatalf("scores not normalized: %+v", result.Draft.Profiles[0])
	}
	if repository.importRecord == nil {
		t.Fatal("import audit record was not created")
	}
	if _, err := os.Stat(repository.importRecord.StoredFilePath); err != nil {
		t.Fatalf("source file was not preserved: %v", err)
	}
}

func TestInvalidExcelImportIsRejectedButSourceAndPreviewRemain(t *testing.T) {
	repository := &stubRepository{}
	service := NewService(repository)
	if err := service.ConfigureImporter(t.TempDir(), 2<<20); err != nil {
		t.Fatal(err)
	}
	header := xlsxFileHeader(t, "invalid.xlsx", [][]string{
		excelHeaders(),
		{"runbook", "runbook", "complete", "Complete", "60", "duplicate", "One", "Description", "manual", "50", "", "", "", "medium", "", "", "", "", ""},
		{"runbook", "runbook", "safety", "Safety", "40", "duplicate", "Two", "Description", "safety", "-10", "", "", "", "high", "", "", "", "", ""},
	})
	result, err := service.Import(context.Background(), 1, header, ImportOptions{Name: "Invalid"})
	var validationError *ImportValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if result == nil || result.Import.Status != "validation_failed" || result.Validation.Valid {
		t.Fatalf("unexpected invalid result: %+v", result)
	}
	if !containsError(result.Validation.Errors, "rule key must be unique") || !containsError(result.Validation.Errors, "negative") {
		t.Fatalf("missing validation errors: %v", result.Validation.Errors)
	}
	if repository.importRecord == nil || len(repository.importRecord.Preview) == 0 {
		t.Fatal("failed import preview was not retained")
	}
	if _, statErr := os.Stat(repository.importRecord.StoredFilePath); statErr != nil {
		t.Fatalf("failed source file was not retained: %v", statErr)
	}
}

func TestExcelMissingProfileIsRejected(t *testing.T) {
	ast := spreadsheetAST([][]string{excelHeaders(), {"", "runbook", "complete", "Complete", "100", "rule", "Rule", "Description", "manual", "100", "", "", "", "medium", "", "", "", "", ""}})
	standard, warnings := buildStructuredDraft(ast, ImportOptions{Name: "Missing profile"})
	validation := Validate(standard)
	if validation.Valid || !containsError(validation.Errors, "key and name are required") {
		t.Fatalf("missing profile accepted: %v", validation.Errors)
	}
	if !hasWarning(warnings, "missing_profile") {
		t.Fatalf("missing profile warning absent: %v", warnings)
	}
}

func TestWordAliasHeadersProduceMappingWarnings(t *testing.T) {
	ast := &document.DocumentAST{Title: "Word Standard", ParserName: "go-docx", Blocks: []document.DocumentBlock{{
		ID: "table-1", Type: document.BlockTypeTable, SectionPath: []string{"完整性"}, Children: []document.DocumentBlock{
			{Type: document.BlockTypeTableRow, Children: []document.DocumentBlock{{Text: "规则名称"}, {Text: "分值"}, {Text: "权重"}, {Text: "说明"}, {Text: "必填"}}},
			{Type: document.BlockTypeTableRow, Children: []document.DocumentBlock{{Text: "步骤完整"}, {Text: "100"}, {Text: "100"}, {Text: "检查操作步骤"}, {Text: "是"}}},
		},
	}}}
	standard, warnings := buildStructuredDraft(ast, ImportOptions{ProfileKey: "word", ProfileName: "Word"})
	if !Validate(standard).Valid {
		t.Fatalf("Word draft invalid: %v", Validate(standard).Errors)
	}
	if !hasWarning(warnings, "ambiguous_header_mapping") || !hasWarning(warnings, "inferred_rule_key") || !hasWarning(warnings, "inferred_criterion_key") {
		t.Fatalf("expected mapping warnings, got %v", warnings)
	}
}

func excelHeaders() []string {
	return []string{"profile", "doc_type", "criterion_key", "criterion_name", "criterion_weight", "rule_key", "rule_name", "rule_description", "rule_type", "max_score", "deduction", "required", "hard_gate", "severity", "evidence_requirement", "detector_config", "llm_instruction", "positive_example", "negative_example"}
}

func xlsxFileHeader(t *testing.T, name string, rows [][]string) *multipart.FileHeader {
	t.Helper()
	file := excelize.NewFile()
	sheet := file.GetSheetName(0)
	for r, row := range rows {
		for c, value := range row {
			axis, _ := excelize.CoordinatesToCellName(c+1, r+1)
			if err := file.SetCellValue(sheet, axis, value); err != nil {
				t.Fatal(err)
			}
		}
	}
	buffer, err := file.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(buffer.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if err := request.ParseMultipartForm(4 << 20); err != nil {
		t.Fatal(err)
	}
	return request.MultipartForm.File["file"][0]
}

func spreadsheetAST(rows [][]string) *document.DocumentAST {
	table := document.DocumentBlock{ID: "sheet-1", Type: document.BlockTypeTable}
	for _, values := range rows {
		row := document.DocumentBlock{Type: document.BlockTypeTableRow}
		for _, value := range values {
			row.Children = append(row.Children, document.DocumentBlock{Type: document.BlockTypeTableCell, Text: value})
		}
		table.Children = append(table.Children, row)
	}
	return &document.DocumentAST{Title: "Sheet", ParserName: "excelize", Blocks: []document.DocumentBlock{table}}
}

func hasWarning(values []ImportWarning, code string) bool {
	for _, value := range values {
		if value.Code == code {
			return true
		}
	}
	return false
}
