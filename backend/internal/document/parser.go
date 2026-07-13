package document

import (
	"fmt"
	"os"
	"strings"

	"aiops-platform/backend/internal/model"
	docx "github.com/fumiama/go-docx"
	"github.com/xuri/excelize/v2"
)

const maxExtractedTextBytes = 10 << 20

func ExtractText(document *model.KBDocument) (string, error) {
	if document == nil || strings.TrimSpace(document.FilePath) == "" {
		return "", ErrInvalidFile
	}
	return ExtractTextFromFile(document.FilePath, document.FileType)
}

func ExtractTextFromFile(path, fileType string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", ErrInvalidFile
	}
	switch fileType {
	case model.DocumentFileTypeMarkdown, model.DocumentFileTypeText:
		content, err := readTextFile(path)
		if err != nil {
			return "", err
		}
		return string(content), nil
	case model.DocumentFileTypeDocx:
		return extractDocxText(path)
	case model.DocumentFileTypeXlsx:
		return extractXlsxText(path)
	default:
		return "", ErrUnsupportedExt
	}
}

func readTextFile(path string) ([]byte, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read document file: %w", err)
	}
	if len(content) > maxExtractedTextBytes {
		return nil, ErrFileTooLarge
	}
	return content, nil
}

func extractDocxText(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open docx: %w", err)
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("stat docx: %w", err)
	}
	if stat.Size() > maxExtractedTextBytes {
		return "", ErrFileTooLarge
	}
	document, err := docx.Parse(file, stat.Size())
	if err != nil {
		return "", fmt.Errorf("parse docx: %w", err)
	}
	var builder strings.Builder
	appendDocxItems(&builder, document.Document.Body.Items)
	text := strings.TrimSpace(builder.String())
	if text == "" {
		return "", ErrInvalidFile
	}
	return text, nil
}

func appendDocxItems(builder *strings.Builder, items []interface{}) {
	for _, item := range items {
		switch value := item.(type) {
		case *docx.Paragraph:
			appendParagraphText(builder, value)
			appendLine(builder)
		case docx.Paragraph:
			appendParagraphText(builder, &value)
			appendLine(builder)
		case *docx.Table:
			appendTableText(builder, value)
		case docx.Table:
			appendTableText(builder, &value)
		}
	}
}

func appendParagraphText(builder *strings.Builder, paragraph *docx.Paragraph) {
	if paragraph == nil {
		return
	}
	for _, child := range paragraph.Children {
		switch value := child.(type) {
		case *docx.Run:
			appendRunText(builder, value)
		case docx.Run:
			appendRunText(builder, &value)
		case *docx.Hyperlink:
			appendRunText(builder, &value.Run)
		case docx.Hyperlink:
			appendRunText(builder, &value.Run)
		}
	}
}

func appendRunText(builder *strings.Builder, run *docx.Run) {
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
			builder.WriteString("\t")
		}
	}
}

func appendTableText(builder *strings.Builder, table *docx.Table) {
	if table == nil {
		return
	}
	for _, row := range table.TableRows {
		var cells []string
		for _, cell := range row.TableCells {
			var cellBuilder strings.Builder
			for _, paragraph := range cell.Paragraphs {
				appendParagraphText(&cellBuilder, paragraph)
			}
			cellText := strings.TrimSpace(cellBuilder.String())
			if cellText != "" {
				cells = append(cells, cellText)
			}
			for _, nested := range cell.Tables {
				appendTableText(builder, nested)
			}
		}
		if len(cells) > 0 {
			builder.WriteString(strings.Join(cells, " | "))
			appendLine(builder)
		}
	}
}

func appendLine(builder *strings.Builder) {
	if builder.Len() > 0 && !strings.HasSuffix(builder.String(), "\n") {
		builder.WriteString("\n")
	}
}

func extractXlsxText(path string) (string, error) {
	file, err := excelize.OpenFile(path)
	if err != nil {
		return "", fmt.Errorf("open xlsx: %w", err)
	}
	defer file.Close()
	var builder strings.Builder
	for _, sheetName := range file.GetSheetList() {
		rows, err := file.GetRows(sheetName)
		if err != nil {
			return "", fmt.Errorf("read xlsx sheet %s: %w", sheetName, err)
		}
		for _, row := range rows {
			cells := compactCells(row)
			if len(cells) == 0 {
				continue
			}
			builder.WriteString(sheetName)
			builder.WriteString(": ")
			builder.WriteString(strings.Join(cells, " | "))
			builder.WriteString("\n")
			if builder.Len() > maxExtractedTextBytes {
				return "", ErrFileTooLarge
			}
		}
	}
	text := strings.TrimSpace(builder.String())
	if text == "" {
		return "", ErrInvalidFile
	}
	return text, nil
}

func compactCells(row []string) []string {
	cells := make([]string, 0, len(row))
	for _, cell := range row {
		value := strings.TrimSpace(cell)
		if value != "" {
			cells = append(cells, value)
		}
	}
	return cells
}
