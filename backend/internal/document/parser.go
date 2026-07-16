package document

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"aiops-platform/backend/internal/model"
	docx "github.com/fumiama/go-docx"
	"github.com/xuri/excelize/v2"
	"rsc.io/pdf"
)

const maxExtractedTextBytes = 10 << 20

// ExtractText preserves the v1 API while using the structured parser registry.
func ExtractText(document *model.KBDocument) (string, error) {
	if document == nil || strings.TrimSpace(document.FilePath) == "" {
		return "", ErrInvalidFile
	}
	return ExtractTextFromFile(document.FilePath, document.FileType)
}

func ExtractTextFromFile(path, fileType string) (string, error) {
	registry, err := NewDefaultParserRegistry(DefaultParseLimits())
	if err != nil {
		return "", err
	}
	ast, err := registry.Parse(context.Background(), ParseRequest{Path: path, FileName: filepath.Base(path), FileType: fileType})
	if err != nil {
		return "", err
	}
	var lines []string
	appendBlockText(&lines, ast.Blocks)
	text := strings.TrimSpace(strings.Join(lines, "\n"))
	if text == "" {
		return "", ErrInvalidFile
	}
	return text, nil
}

func appendBlockText(lines *[]string, blocks []DocumentBlock) {
	for _, block := range blocks {
		if strings.TrimSpace(block.Text) != "" {
			text := block.Text
			if block.Type == BlockTypeHeading && block.Level > 0 {
				text = strings.Repeat("#", block.Level) + " " + text
			}
			*lines = append(*lines, text)
		}
		appendBlockText(lines, block.Children)
	}
}

type markdownParser struct{}

func (markdownParser) Name() string        { return "markdown" }
func (markdownParser) Version() string     { return "2.0.0" }
func (markdownParser) FileTypes() []string { return []string{"md"} }
func (markdownParser) Parse(ctx context.Context, request ParseRequest, limits ParseLimits) (*DocumentAST, error) {
	content, err := readLimitedText(request.Path, limits.MaxBytes)
	if err != nil {
		return nil, err
	}
	return parseMarkdown(ctx, request, string(content), limits)
}

type textParser struct{}

func (textParser) Name() string        { return "plain-text" }
func (textParser) Version() string     { return "2.0.0" }
func (textParser) FileTypes() []string { return []string{"txt"} }
func (textParser) Parse(ctx context.Context, request ParseRequest, limits ParseLimits) (*DocumentAST, error) {
	content, err := readLimitedText(request.Path, limits.MaxBytes)
	if err != nil {
		return nil, err
	}
	ast := newAST(request)
	order := 0
	for _, paragraph := range splitParagraphs(string(content)) {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		order++
		if order > limits.MaxBlocks {
			return nil, ErrBlockLimitExceeded
		}
		ast.Blocks = append(ast.Blocks, newBlock(BlockTypeParagraph, paragraph, 0, order))
	}
	if len(ast.Blocks) == 0 {
		return nil, ErrInvalidFile
	}
	return ast, nil
}

func parseMarkdown(ctx context.Context, request ParseRequest, content string, limits ParseLimits) (*DocumentAST, error) {
	ast := newAST(request)
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	section := []string{}
	order := 0
	inCode := false
	codeLanguage := ""
	var codeLines []string
	for i := 0; i < len(lines); i++ {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inCode {
				order++
				block := newBlock(BlockTypeCode, strings.Join(codeLines, "\n"), 0, order)
				block.SectionPath = append([]string(nil), section...)
				block.Attributes["language"] = codeLanguage
				ast.Blocks = append(ast.Blocks, block)
				codeLines, inCode, codeLanguage = nil, false, ""
			} else {
				inCode = true
				codeLanguage = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			}
			if order > limits.MaxBlocks {
				return nil, ErrBlockLimitExceeded
			}
			continue
		}
		if inCode {
			codeLines = append(codeLines, line)
			continue
		}
		if trimmed == "" {
			continue
		}
		blockType, text, level := markdownLineType(trimmed)
		if blockType == BlockTypeTable && i+1 < len(lines) && isMarkdownTableSeparator(lines[i+1]) {
			rows := []DocumentBlock{}
			for ; i < len(lines) && strings.Contains(lines[i], "|"); i++ {
				if isMarkdownTableSeparator(lines[i]) {
					continue
				}
				cells := strings.Split(strings.Trim(strings.TrimSpace(lines[i]), "|"), "|")
				row := newBlock(BlockTypeTableRow, "", 0, 0)
				for column, cell := range cells {
					child := newBlock(BlockTypeTableCell, strings.TrimSpace(cell), 0, 0)
					child.Attributes["column"] = column + 1
					row.Children = append(row.Children, child)
				}
				rows = append(rows, row)
			}
			i--
			order++
			block := newBlock(BlockTypeTable, "", 0, order)
			block.Children = rows
			block.SectionPath = append([]string(nil), section...)
			ast.Blocks = append(ast.Blocks, block)
			continue
		}
		order++
		if order > limits.MaxBlocks {
			return nil, ErrBlockLimitExceeded
		}
		block := newBlock(blockType, text, level, order)
		if blockType == BlockTypeHeading {
			section = updateSection(section, text, level)
			if ast.Title == "" || ast.Title == request.Title {
				ast.Title = text
			}
		}
		block.SectionPath = append([]string(nil), section...)
		ast.Blocks = append(ast.Blocks, block)
	}
	if inCode {
		ast.ParseWarnings = append(ast.ParseWarnings, ParseWarning{Code: "unclosed_code_fence", Message: "markdown code fence is not closed"})
		order++
		block := newBlock(BlockTypeCode, strings.Join(codeLines, "\n"), 0, order)
		block.Attributes["language"] = codeLanguage
		ast.Blocks = append(ast.Blocks, block)
	}
	if len(ast.Blocks) == 0 {
		return nil, ErrInvalidFile
	}
	return ast, nil
}

func markdownLineType(line string) (string, string, int) {
	if strings.HasPrefix(line, "#") {
		level := 0
		for level < len(line) && line[level] == '#' {
			level++
		}
		if level <= 6 && level < len(line) && line[level] == ' ' {
			return BlockTypeHeading, strings.TrimSpace(line[level:]), level
		}
	}
	if strings.HasPrefix(line, ">") {
		return BlockTypeQuote, strings.TrimSpace(strings.TrimPrefix(line, ">")), 0
	}
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "+ ") {
		return BlockTypeListItem, strings.TrimSpace(line[2:]), 0
	}
	if index := strings.Index(line, ". "); index > 0 {
		if _, err := strconv.Atoi(line[:index]); err == nil {
			return BlockTypeListItem, strings.TrimSpace(line[index+2:]), 0
		}
	}
	if strings.Contains(line, "|") {
		return BlockTypeTable, line, 0
	}
	return BlockTypeParagraph, line, 0
}

func isMarkdownTableSeparator(line string) bool {
	value := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(line), "|", ""), ":", "")
	value = strings.ReplaceAll(value, "-", "")
	return strings.TrimSpace(value) == ""
}

type docxParser struct{}

func (docxParser) Name() string        { return "go-docx" }
func (docxParser) Version() string     { return "2.0.0" }
func (docxParser) FileTypes() []string { return []string{"docx"} }
func (docxParser) Parse(ctx context.Context, request ParseRequest, limits ParseLimits) (*DocumentAST, error) {
	file, err := os.Open(request.Path)
	if err != nil {
		return nil, fmt.Errorf("open docx: %w", err)
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat docx: %w", err)
	}
	document, err := docx.Parse(file, stat.Size())
	if err != nil {
		return nil, fmt.Errorf("parse docx: %w", err)
	}
	ast := newAST(request)
	page, order := 1, 0
	section := []string{}
	for _, item := range document.Document.Body.Items {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		switch value := item.(type) {
		case *docx.Paragraph:
			pageBreaks := docxParagraphPageBreaks(value)
			for range pageBreaks {
				order++
				if order > limits.MaxBlocks {
					return nil, ErrBlockLimitExceeded
				}
				breakBlock := newBlock(BlockTypePageBreak, "", 0, order)
				breakBlock.Page = intPtr(page)
				breakBlock.SectionPath = append([]string(nil), section...)
				ast.Blocks = append(ast.Blocks, breakBlock)
				page++
				if page > limits.MaxPages {
					return nil, ErrPageLimitExceeded
				}
			}
			text := strings.TrimSpace(docxParagraphText(value))
			if text == "" {
				continue
			}
			order++
			if order > limits.MaxBlocks {
				return nil, ErrBlockLimitExceeded
			}
			blockType, level, attributes := docxParagraphType(value)
			block := newBlock(blockType, text, level, order)
			block.Page = intPtr(page)
			block.Attributes = attributes
			if blockType == BlockTypeHeading {
				section = updateSection(section, text, level)
				if level == 0 || ast.Title == request.Title {
					ast.Title = text
				}
			}
			block.SectionPath = append([]string(nil), section...)
			ast.Blocks = append(ast.Blocks, block)
		case *docx.Table:
			order++
			block, err := docxTableBlock(value, order, page, section, limits.MaxBlocks)
			if err != nil {
				return nil, err
			}
			ast.Blocks = append(ast.Blocks, block)
		}
	}
	if len(ast.Blocks) == 0 {
		return nil, ErrInvalidFile
	}
	ast.ParseWarnings = append(ast.ParseWarnings, ParseWarning{Code: "page_numbers_estimated", Message: "DOCX parser does not expose rendered pagination; blocks are assigned to the first logical page unless explicit breaks are available"})
	return ast, nil
}

func docxParagraphPageBreaks(paragraph *docx.Paragraph) []struct{} {
	var breaks []struct{}
	if paragraph == nil {
		return breaks
	}
	for _, child := range paragraph.Children {
		var run *docx.Run
		switch value := child.(type) {
		case *docx.Run:
			run = value
		case docx.Run:
			run = &value
		}
		if run == nil {
			continue
		}
		for _, runChild := range run.Children {
			switch value := runChild.(type) {
			case *docx.BarterRabbet:
				if value.Type == "page" {
					breaks = append(breaks, struct{}{})
				}
			case docx.BarterRabbet:
				if value.Type == "page" {
					breaks = append(breaks, struct{}{})
				}
			}
		}
	}
	return breaks
}

func docxParagraphType(paragraph *docx.Paragraph) (string, int, map[string]any) {
	attributes := map[string]any{}
	if paragraph == nil || paragraph.Properties == nil {
		return BlockTypeParagraph, 0, attributes
	}
	properties := paragraph.Properties
	style := ""
	if properties.Style != nil {
		style = properties.Style.Val
		attributes["style"] = style
	}
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(style, " ", ""), "_", ""))
	if normalized == "title" {
		return BlockTypeHeading, 0, attributes
	}
	for level := 1; level <= 9; level++ {
		if normalized == fmt.Sprintf("heading%d", level) || normalized == fmt.Sprintf("标题%d", level) {
			return BlockTypeHeading, level, attributes
		}
	}
	if properties.NumProperties != nil || strings.Contains(normalized, "listparagraph") {
		attributes["numbered"] = properties.NumProperties != nil
		return BlockTypeListItem, 0, attributes
	}
	if strings.Contains(normalized, "caption") {
		return BlockTypeImageCaption, 0, attributes
	}
	return BlockTypeParagraph, 0, attributes
}

func docxParagraphText(paragraph *docx.Paragraph) string {
	if paragraph == nil {
		return ""
	}
	var builder strings.Builder
	for _, child := range paragraph.Children {
		switch value := child.(type) {
		case *docx.Run:
			appendDocxRunText(&builder, value)
		case docx.Run:
			appendDocxRunText(&builder, &value)
		case *docx.Hyperlink:
			appendDocxRunText(&builder, &value.Run)
		case docx.Hyperlink:
			appendDocxRunText(&builder, &value.Run)
		}
	}
	return builder.String()
}

func appendDocxRunText(builder *strings.Builder, run *docx.Run) {
	if run == nil {
		return
	}
	for _, child := range run.Children {
		switch value := child.(type) {
		case *docx.Text:
			builder.WriteString(value.Text)
		case docx.Text:
			builder.WriteString(value.Text)
		case *docx.Tab, docx.Tab:
			builder.WriteByte('\t')
		case *docx.BarterRabbet, docx.BarterRabbet:
			builder.WriteByte('\n')
		}
	}
}

func docxTableBlock(table *docx.Table, order, page int, section []string, maxBlocks int) (DocumentBlock, error) {
	block := newBlock(BlockTypeTable, "", 0, order)
	block.Page, block.SectionPath = intPtr(page), append([]string(nil), section...)
	for rowIndex, row := range table.TableRows {
		rowBlock := newBlock(BlockTypeTableRow, "", 0, 0)
		rowBlock.Attributes["row"] = rowIndex + 1
		for columnIndex, cell := range row.TableCells {
			var parts []string
			for _, paragraph := range cell.Paragraphs {
				if text := strings.TrimSpace(docxParagraphText(paragraph)); text != "" {
					parts = append(parts, text)
				}
			}
			cellBlock := newBlock(BlockTypeTableCell, strings.Join(parts, "\n"), 0, 0)
			cellBlock.Attributes["row"], cellBlock.Attributes["column"] = rowIndex+1, columnIndex+1
			rowBlock.Children = append(rowBlock.Children, cellBlock)
		}
		block.Children = append(block.Children, rowBlock)
		if countBlocks([]DocumentBlock{block}) > maxBlocks {
			return DocumentBlock{}, ErrBlockLimitExceeded
		}
	}
	return block, nil
}

type xlsxParser struct{}

func (xlsxParser) Name() string        { return "excelize" }
func (xlsxParser) Version() string     { return "2.9.1" }
func (xlsxParser) FileTypes() []string { return []string{"xlsx", "xls"} }
func (xlsxParser) Parse(ctx context.Context, request ParseRequest, limits ParseLimits) (*DocumentAST, error) {
	if normalizeFileType(request.FileType) == "xls" {
		return nil, fmt.Errorf("%w: convert .xls to .xlsx for structured parsing", ErrUnsupportedExt)
	}
	file, err := excelize.OpenFile(request.Path)
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer file.Close()
	ast := newAST(request)
	order := 0
	for sheetIndex, sheetName := range file.GetSheetList() {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		rows, err := file.GetRows(sheetName)
		if err != nil {
			return nil, fmt.Errorf("read xlsx sheet %s: %w", sheetName, err)
		}
		merges, err := file.GetMergeCells(sheetName)
		if err != nil {
			return nil, fmt.Errorf("read xlsx merged cells %s: %w", sheetName, err)
		}
		order++
		sheet := newBlock(BlockTypeTable, sheetName, 0, order)
		sheet.Attributes["kind"], sheet.Attributes["sheet"], sheet.Attributes["sheet_index"] = "sheet", sheetName, sheetIndex+1
		for rowIndex, values := range rows {
			row := newBlock(BlockTypeTableRow, "", 0, 0)
			row.Attributes["row"] = rowIndex + 1
			for columnIndex := range values {
				axis, _ := excelize.CoordinatesToCellName(columnIndex+1, rowIndex+1)
				display, err := file.GetCellValue(sheetName, axis)
				if err != nil {
					return nil, fmt.Errorf("read xlsx cell %s!%s: %w", sheetName, axis, err)
				}
				formula, _ := file.GetCellFormula(sheetName, axis)
				cell := newBlock(BlockTypeTableCell, display, 0, 0)
				cell.Attributes["kind"], cell.Attributes["sheet"] = "cell", sheetName
				cell.Attributes["row"], cell.Attributes["column"], cell.Attributes["coordinate"] = rowIndex+1, columnIndex+1, axis
				cell.Attributes["display_value"] = display
				if formula != "" {
					cell.Attributes["formula"] = formula
				}
				row.Children = append(row.Children, cell)
			}
			sheet.Children = append(sheet.Children, row)
			if countBlocks(append(ast.Blocks, sheet)) > limits.MaxBlocks {
				return nil, ErrBlockLimitExceeded
			}
		}
		merged := make([]map[string]any, 0, len(merges))
		for _, cell := range merges {
			merged = append(merged, map[string]any{"range": cell.GetStartAxis() + ":" + cell.GetEndAxis(), "display_value": cell.GetCellValue()})
		}
		sheet.Attributes["merged_cells"] = merged
		ast.Blocks = append(ast.Blocks, sheet)
	}
	if len(ast.Blocks) == 0 {
		return nil, ErrInvalidFile
	}
	return ast, nil
}

type pdfParser struct{}

func (pdfParser) Name() string        { return "rsc-pdf" }
func (pdfParser) Version() string     { return "0.1.1" }
func (pdfParser) FileTypes() []string { return []string{"pdf"} }
func (pdfParser) Parse(ctx context.Context, request ParseRequest, limits ParseLimits) (*DocumentAST, error) {
	reader, err := pdf.Open(request.Path)
	if err != nil {
		return nil, fmt.Errorf("open pdf: %w", err)
	}
	if reader.NumPage() > limits.MaxPages {
		return nil, ErrPageLimitExceeded
	}
	ast := newAST(request)
	order, textPages := 0, 0
	for pageNumber := 1; pageNumber <= reader.NumPage(); pageNumber++ {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		page := reader.Page(pageNumber)
		content := page.Content()
		text := orderedPDFText(content.Text)
		if strings.TrimSpace(text) == "" {
			p := pageNumber
			ast.ParseWarnings = append(ast.ParseWarnings, ParseWarning{Code: "ocr_required", Message: "page has no extractable text and requires OCR", Page: &p})
			continue
		}
		textPages++
		for _, paragraph := range splitParagraphs(text) {
			order++
			if order > limits.MaxBlocks {
				return nil, ErrBlockLimitExceeded
			}
			block := newBlock(BlockTypeParagraph, paragraph, 0, order)
			block.Page = intPtr(pageNumber)
			block.Attributes["order_confidence"] = 0.8
			ast.Blocks = append(ast.Blocks, block)
		}
	}
	if textPages == 0 {
		ast.ParseWarnings = append(ast.ParseWarnings, ParseWarning{Code: "scanned_pdf", Message: "PDF contains no extractable text; OCR is required"})
		return ast, nil
	}
	if textPages < reader.NumPage() {
		ast.ParseWarnings = append(ast.ParseWarnings, ParseWarning{Code: "mixed_pdf", Message: "PDF contains both text and image-only pages"})
	}
	return ast, nil
}

func orderedPDFText(items []pdf.Text) string {
	sort.SliceStable(items, func(i, j int) bool {
		if diff := items[i].Y - items[j].Y; diff > 1 || diff < -1 {
			return items[i].Y > items[j].Y
		}
		return items[i].X < items[j].X
	})
	var builder strings.Builder
	lastY, lastEndX := 0.0, 0.0
	for index, item := range items {
		if index > 0 {
			if lastY-item.Y > 2 || item.Y-lastY > 2 {
				builder.WriteByte('\n')
			} else if item.X-lastEndX > item.FontSize*0.2 {
				builder.WriteByte(' ')
			}
		}
		builder.WriteString(item.S)
		lastY = item.Y
		lastEndX = item.X + item.W
	}
	return builder.String()
}

func newAST(request ParseRequest) *DocumentAST {
	title := strings.TrimSpace(request.Title)
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(request.FileName), filepath.Ext(request.FileName))
	}
	return &DocumentAST{DocumentID: request.DocumentID, Title: title, Blocks: []DocumentBlock{}, ParseWarnings: []ParseWarning{}}
}

func readLimitedText(path string, maxBytes int64) ([]byte, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read document file: %w", err)
	}
	if int64(len(content)) > maxBytes {
		return nil, ErrFileTooLarge
	}
	if !utf8.Valid(content) {
		return nil, ErrInvalidFile
	}
	return content, nil
}

func splitParagraphs(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	parts := strings.FieldsFunc(content, func(r rune) bool { return r == '\n' || r == '\r' })
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func updateSection(section []string, title string, level int) []string {
	if level <= 0 {
		return []string{title}
	}
	if len(section) >= level {
		section = section[:level-1]
	}
	for len(section) < level-1 {
		section = append(section, "")
	}
	return append(section, title)
}

func intPtr(value int) *int { return &value }
