package document

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	docx "github.com/fumiama/go-docx"
	"github.com/xuri/excelize/v2"
)

func TestParserFixturesMarkdownAndText(t *testing.T) {
	registry := newFixtureRegistry(t, DefaultParseLimits())
	markdownPath := writeFixture(t, "runbook.md", []byte("# Payment Runbook\n\n- Check pods\n\n```sh\nkubectl get pods\n```\n"))
	ast, err := registry.Parse(context.Background(), ParseRequest{Path: markdownPath, FileName: "runbook.md", FileType: "md"})
	if err != nil {
		t.Fatalf("Parse(markdown) error = %v", err)
	}
	assertBlockType(t, ast.Blocks, BlockTypeHeading)
	assertBlockType(t, ast.Blocks, BlockTypeListItem)
	assertBlockType(t, ast.Blocks, BlockTypeCode)
	if ast.ParserName != "markdown" || ast.ParserVersion == "" {
		t.Fatalf("parser identity = %q/%q", ast.ParserName, ast.ParserVersion)
	}

	textPath := writeFixture(t, "notes.txt", []byte("first paragraph\nsecond paragraph"))
	ast, err = registry.Parse(context.Background(), ParseRequest{Path: textPath, FileName: "notes.txt", FileType: "txt"})
	if err != nil {
		t.Fatalf("Parse(text) error = %v", err)
	}
	if len(ast.Blocks) != 2 || ast.Blocks[0].Type != BlockTypeParagraph {
		t.Fatalf("text blocks = %+v", ast.Blocks)
	}
}

func TestParserFixtureDOCXPreservesStructure(t *testing.T) {
	document := docx.New()
	document.AddParagraph().Style("Heading1").AddText("Recovery")
	document.AddParagraph().AddText("Restart only after approval.")
	list := document.AddParagraph().Style("ListParagraph")
	list.Properties.NumProperties = &docx.NumProperties{NumID: &docx.NumID{Val: "1"}}
	list.AddText("Check health")
	table := document.AddTable(2, 2, 0, nil)
	table.TableRows[0].TableCells[0].AddParagraph().AddText("Step")
	table.TableRows[0].TableCells[1].AddParagraph().AddText("Command")
	table.TableRows[1].TableCells[0].AddParagraph().AddText("Inspect")
	table.TableRows[1].TableCells[1].AddParagraph().AddText("kubectl get pods")
	var body bytes.Buffer
	if _, err := document.WriteTo(&body); err != nil {
		t.Fatalf("write DOCX fixture: %v", err)
	}
	path := writeFixture(t, "runbook.docx", body.Bytes())
	ast, err := newFixtureRegistry(t, DefaultParseLimits()).Parse(context.Background(), ParseRequest{Path: path, FileName: "runbook.docx", FileType: "docx"})
	if err != nil {
		t.Fatalf("Parse(DOCX) error = %v", err)
	}
	assertBlockType(t, ast.Blocks, BlockTypeHeading)
	assertBlockType(t, ast.Blocks, BlockTypeParagraph)
	assertBlockType(t, ast.Blocks, BlockTypeListItem)
	tableBlock := findBlock(ast.Blocks, BlockTypeTable)
	if tableBlock == nil || len(tableBlock.Children) != 2 || len(tableBlock.Children[0].Children) != 2 {
		t.Fatalf("DOCX table block = %+v", tableBlock)
	}
	if ast.Blocks[0].Page == nil {
		t.Fatal("DOCX block page information is missing")
	}
}

func TestParserFixtureXLSXPreservesSheetsCoordinatesAndMerges(t *testing.T) {
	file := excelize.NewFile()
	defer file.Close()
	if err := file.SetCellValue("Sheet1", "A1", "Service"); err != nil {
		t.Fatal(err)
	}
	if err := file.SetCellValue("Sheet1", "B1", "Status"); err != nil {
		t.Fatal(err)
	}
	if err := file.SetCellValue("Sheet1", "A2", "payment"); err != nil {
		t.Fatal(err)
	}
	if err := file.SetCellFormula("Sheet1", "B2", "=1+1"); err != nil {
		t.Fatal(err)
	}
	if err := file.MergeCell("Sheet1", "A3", "B3"); err != nil {
		t.Fatal(err)
	}
	if err := file.SetCellValue("Sheet1", "A3", "merged note"); err != nil {
		t.Fatal(err)
	}
	if _, err := file.NewSheet("Thresholds"); err != nil {
		t.Fatal(err)
	}
	if err := file.SetCellValue("Thresholds", "A1", "CPU"); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	if err := file.Write(&body); err != nil {
		t.Fatalf("write XLSX fixture: %v", err)
	}
	path := writeFixture(t, "capacity.xlsx", body.Bytes())
	ast, err := newFixtureRegistry(t, DefaultParseLimits()).Parse(context.Background(), ParseRequest{Path: path, FileName: "capacity.xlsx", FileType: "xlsx"})
	if err != nil {
		t.Fatalf("Parse(XLSX) error = %v", err)
	}
	if len(ast.Blocks) != 2 || ast.Blocks[0].Attributes["sheet"] != "Sheet1" {
		t.Fatalf("XLSX sheets = %+v", ast.Blocks)
	}
	merged, ok := ast.Blocks[0].Attributes["merged_cells"].([]map[string]any)
	if !ok || len(merged) != 1 || merged[0]["range"] != "A3:B3" {
		t.Fatalf("merged cells = %#v", ast.Blocks[0].Attributes["merged_cells"])
	}
	cell := ast.Blocks[0].Children[0].Children[0]
	if cell.Attributes["coordinate"] != "A1" || cell.Attributes["display_value"] != "Service" {
		t.Fatalf("cell attributes = %#v", cell.Attributes)
	}
}

func TestParserFixturesTextAndScannedPDF(t *testing.T) {
	registry := newFixtureRegistry(t, DefaultParseLimits())
	textPath := writeFixture(t, "text.pdf", minimalPDF("Payment failure runbook"))
	ast, err := registry.Parse(context.Background(), ParseRequest{Path: textPath, FileName: "text.pdf", FileType: "pdf"})
	if err != nil {
		t.Fatalf("Parse(text PDF) error = %v", err)
	}
	if len(ast.Blocks) == 0 || !strings.Contains(ast.Blocks[0].Text, "Payment") {
		t.Fatalf("text PDF blocks = %+v warnings = %+v", ast.Blocks, ast.ParseWarnings)
	}

	scannedPath := writeFixture(t, "scan.pdf", minimalPDF(""))
	ast, err = registry.Parse(context.Background(), ParseRequest{Path: scannedPath, FileName: "scan.pdf", FileType: "pdf"})
	if err != nil {
		t.Fatalf("Parse(scanned PDF) error = %v", err)
	}
	if !hasWarning(ast.ParseWarnings, "ocr_required") || !hasWarning(ast.ParseWarnings, "scanned_pdf") {
		t.Fatalf("scanned PDF warnings = %+v", ast.ParseWarnings)
	}
}

func TestParserRegistryRejectsLegacyDocAndSignatureMismatch(t *testing.T) {
	registry := newFixtureRegistry(t, DefaultParseLimits())
	_, err := registry.Parse(context.Background(), ParseRequest{FileName: "legacy.doc", FileType: "doc", Path: "unused"})
	if err != ErrLegacyDocUnsupported {
		t.Fatalf("legacy DOC error = %v", err)
	}
	fakePDF := writeFixture(t, "fake.pdf", []byte("not a pdf"))
	_, err = registry.Parse(context.Background(), ParseRequest{FileName: "fake.pdf", FileType: "pdf", Path: fakePDF})
	if err != ErrFileTypeMismatch {
		t.Fatalf("signature mismatch error = %v", err)
	}
}

func TestParserRegistryEnforcesBlockLimitAndTimeout(t *testing.T) {
	path := writeFixture(t, "many.txt", []byte("one\ntwo\nthree"))
	registry := newFixtureRegistry(t, ParseLimits{MaxBlocks: 2, Timeout: time.Second, MaxPages: 10, MaxBytes: 1024})
	_, err := registry.Parse(context.Background(), ParseRequest{Path: path, FileName: "many.txt", FileType: "txt"})
	if err != ErrBlockLimitExceeded {
		t.Fatalf("block limit error = %v", err)
	}

	timeoutRegistry, err := NewParserRegistry(ParseLimits{Timeout: 5 * time.Millisecond, MaxBlocks: 10, MaxPages: 10, MaxBytes: 1024}, blockingParser{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = timeoutRegistry.Parse(context.Background(), ParseRequest{Path: path, FileName: "many.txt", FileType: "txt"})
	if err != ErrParseTimeout {
		t.Fatalf("timeout error = %v", err)
	}
}

type blockingParser struct{}

func (blockingParser) Name() string        { return "blocking" }
func (blockingParser) Version() string     { return "test" }
func (blockingParser) FileTypes() []string { return []string{"txt"} }
func (blockingParser) Parse(context.Context, ParseRequest, ParseLimits) (*DocumentAST, error) {
	time.Sleep(50 * time.Millisecond)
	return &DocumentAST{}, nil
}

func newFixtureRegistry(t *testing.T, limits ParseLimits) *ParserRegistry {
	t.Helper()
	registry, err := NewDefaultParserRegistry(limits)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func writeFixture(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertBlockType(t *testing.T, blocks []DocumentBlock, blockType string) {
	t.Helper()
	if findBlock(blocks, blockType) == nil {
		t.Fatalf("block type %q not found in %+v", blockType, blocks)
	}
}

func findBlock(blocks []DocumentBlock, blockType string) *DocumentBlock {
	for index := range blocks {
		if blocks[index].Type == blockType {
			return &blocks[index]
		}
		if child := findBlock(blocks[index].Children, blockType); child != nil {
			return child
		}
	}
	return nil
}

func hasWarning(warnings []ParseWarning, code string) bool {
	for _, warning := range warnings {
		if warning.Code == code {
			return true
		}
	}
	return false
}

func minimalPDF(text string) []byte {
	stream := ""
	if text != "" {
		stream = fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET", strings.ReplaceAll(text, ")", "\\)"))
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
	}
	var builder strings.Builder
	builder.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for index, object := range objects {
		offsets = append(offsets, builder.Len())
		fmt.Fprintf(&builder, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := builder.Len()
	fmt.Fprintf(&builder, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&builder, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&builder, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return []byte(builder.String())
}
