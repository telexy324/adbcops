package document

import (
	"encoding/json"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	ParseQualityExcellent = "excellent"
	ParseQualityGood      = "good"
	ParseQualityWarning   = "warning"
	ParseQualityFailed    = "failed"
)

type ParseQuality struct {
	ParseSuccess         bool           `json:"parseSuccess"`
	TextCoverage         float64        `json:"textCoverage"`
	HeadingDetectionRate float64        `json:"headingDetectionRate"`
	TableDetectionCount  int            `json:"tableDetectionCount"`
	UnknownBlockRatio    float64        `json:"unknownBlockRatio"`
	GarbledTextRatio     float64        `json:"garbledTextRatio"`
	EmptyPageRatio       float64        `json:"emptyPageRatio"`
	OrderConfidence      float64        `json:"orderConfidence"`
	MetadataCompleteness float64        `json:"metadataCompleteness"`
	BlockCount           int            `json:"blockCount"`
	Level                string         `json:"level"`
	Warnings             []ParseWarning `json:"warnings"`
}

func EvaluateParseQuality(ast *DocumentAST, fileSize int64, parseErr error) ParseQuality {
	quality := ParseQuality{Level: ParseQualityFailed, OrderConfidence: 1, Warnings: []ParseWarning{}}
	if ast != nil {
		quality.Warnings = append(quality.Warnings, ast.ParseWarnings...)
	}
	if parseErr != nil {
		quality.Warnings = append(quality.Warnings, ParseWarning{Code: "parse_failed", Message: parseErr.Error()})
		return quality
	}
	if ast == nil {
		quality.Warnings = append(quality.Warnings, ParseWarning{Code: "parse_failed", Message: "parser returned no document AST"})
		return quality
	}

	stats := parseQualityStats{}
	collectParseQualityStats(ast.Blocks, &stats)
	quality.BlockCount = stats.blocks
	quality.TableDetectionCount = stats.tables
	quality.UnknownBlockRatio = ratio(stats.unknown, stats.blocks)
	quality.GarbledTextRatio = ratio(stats.garbledRunes, stats.textRunes)
	if stats.headings > 0 {
		quality.HeadingDetectionRate = 1
	}
	if fileSize > 0 {
		quality.TextCoverage = math.Min(1, float64(stats.textBytes)/float64(fileSize))
	}
	if stats.confidenceCount > 0 {
		quality.OrderConfidence = stats.confidenceTotal / float64(stats.confidenceCount)
	}
	quality.EmptyPageRatio = emptyPageRatio(ast.ParseWarnings, stats.maxPage)
	quality.MetadataCompleteness = metadataCompleteness(ast.Metadata)
	quality.ParseSuccess = stats.blocks > 0 && stats.textRunes > 0 && !hasFatalParseWarning(ast.ParseWarnings)
	quality.Level = parseQualityLevel(quality)
	return quality
}

func (quality ParseQuality) JSON() []byte {
	value, _ := json.Marshal(quality)
	return value
}

type parseQualityStats struct {
	blocks          int
	headings        int
	tables          int
	unknown         int
	textBytes       int
	textRunes       int
	garbledRunes    int
	maxPage         int
	confidenceTotal float64
	confidenceCount int
}

func collectParseQualityStats(blocks []DocumentBlock, stats *parseQualityStats) {
	for _, block := range blocks {
		stats.blocks++
		switch block.Type {
		case BlockTypeHeading:
			stats.headings++
		case BlockTypeTable:
			stats.tables++
		case BlockTypeUnknown:
			stats.unknown++
		}
		stats.textBytes += len(block.Text)
		for _, current := range block.Text {
			stats.textRunes++
			if current == utf8.RuneError || unicode.IsControl(current) && current != '\n' && current != '\t' && current != '\r' {
				stats.garbledRunes++
			}
		}
		if block.Page != nil && *block.Page > stats.maxPage {
			stats.maxPage = *block.Page
		}
		confidence := 1.0
		if value, ok := block.Attributes["order_confidence"].(float64); ok {
			confidence = value
		}
		stats.confidenceTotal += confidence
		stats.confidenceCount++
		collectParseQualityStats(block.Children, stats)
	}
}

func emptyPageRatio(warnings []ParseWarning, maxPage int) float64 {
	emptyPages := map[int]struct{}{}
	for _, warning := range warnings {
		if warning.Code == "ocr_required" && warning.Page != nil {
			emptyPages[*warning.Page] = struct{}{}
			if *warning.Page > maxPage {
				maxPage = *warning.Page
			}
		}
	}
	if maxPage == 0 {
		return 0
	}
	return ratio(len(emptyPages), maxPage)
}

func metadataCompleteness(metadata DocumentMetadata) float64 {
	completed := 0
	values := []string{metadata.Author, metadata.Subject, metadata.DeclaredVersion, metadata.DeclaredOwner, metadata.DeclaredReviewer}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			completed++
		}
	}
	if len(metadata.Keywords) > 0 {
		completed++
	}
	if metadata.CreatedAt != nil {
		completed++
	}
	if metadata.ModifiedAt != nil {
		completed++
	}
	return ratio(completed, 8)
}

func hasFatalParseWarning(warnings []ParseWarning) bool {
	for _, warning := range warnings {
		if warning.Code == "scanned_pdf" || warning.Code == "parse_failed" {
			return true
		}
	}
	return false
}

func parseQualityLevel(quality ParseQuality) string {
	if !quality.ParseSuccess || quality.TextCoverage < 0.01 || quality.GarbledTextRatio > 0.2 {
		return ParseQualityFailed
	}
	if quality.TextCoverage >= 0.8 && quality.UnknownBlockRatio <= 0.02 && quality.GarbledTextRatio <= 0.01 && quality.OrderConfidence >= 0.9 {
		return ParseQualityExcellent
	}
	if quality.TextCoverage >= 0.3 && quality.UnknownBlockRatio <= 0.1 && quality.GarbledTextRatio <= 0.05 && quality.OrderConfidence >= 0.75 {
		return ParseQualityGood
	}
	return ParseQualityWarning
}

func ratio(numerator, denominator int) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}
