package document

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"aiops-platform/backend/internal/model"
)

type ParsedStructure struct {
	Version        model.KBDocumentVersion  `json:"version"`
	ParseQuality   ParseQuality             `json:"parseQuality"`
	DocumentSchema DocumentSchemaExtraction `json:"documentSchema"`
	Warnings       []ParseWarning           `json:"warnings"`
	Blocks         []DocumentBlock          `json:"blocks"`
}

func (s *Service) GetDocumentVersion(ctx context.Context, actor *model.AppUser, versionID int64) (*model.KBDocumentVersion, error) {
	if actor == nil || versionID <= 0 {
		return nil, ErrInvalidInput
	}
	version, err := s.documents.FindDocumentVersionByID(ctx, versionID)
	if err != nil {
		return nil, err
	}
	if _, err := s.Get(ctx, actor, version.DocumentID); err != nil {
		return nil, err
	}
	return version, nil
}

func (s *Service) GetLatestDocumentVersion(ctx context.Context, actor *model.AppUser, documentID int64) (*model.KBDocumentVersion, error) {
	if _, err := s.Get(ctx, actor, documentID); err != nil {
		return nil, err
	}
	return s.documents.FindLatestDocumentVersion(ctx, documentID)
}

func (s *Service) ParseDocumentVersion(ctx context.Context, actor *model.AppUser, versionID int64) (*ParsedStructure, error) {
	version, err := s.GetDocumentVersion(ctx, actor, versionID)
	if err != nil {
		return nil, err
	}
	document, err := s.Get(ctx, actor, version.DocumentID)
	if err != nil {
		return nil, err
	}
	stat, err := os.Stat(version.FilePath)
	if err != nil {
		return nil, fmt.Errorf("stat document version file: %w", err)
	}
	ast, parseErr := s.parserRegistry.Parse(ctx, ParseRequest{
		Path: version.FilePath, FileName: version.FileName, FileType: version.FileType,
		DocumentID: document.ID, Title: document.Title,
	})
	quality := EvaluateParseQuality(ast, stat.Size(), parseErr)
	status := model.DocumentVersionStatusDraft
	parserName, parserVersion, language := "", "", ""
	metadata := []byte("{}")
	documentSchema := []byte("{}")
	schemaExtraction := DocumentSchemaExtraction{Fields: map[string]ExtractedField{}, MissingFields: []string{}, Entities: []DocumentEntity{}, Sections: []SectionClassification{}, Diagnostics: []SchemaDiagnostic{}}
	var blocks []model.KBDocumentBlock
	if ast != nil {
		parserName, parserVersion, language = ast.ParserName, ast.ParserVersion, ast.Language
		metadata, _ = json.Marshal(ast.Metadata)
		schemaExtraction = ExtractDocumentSchema(document, ast)
		documentSchema = schemaExtraction.JSON()
		blocks = flattenASTBlocks(ast.Blocks)
	}
	if !quality.ParseSuccess {
		status = model.DocumentVersionStatusFailed
	}
	saved, err := s.documents.RecordDocumentVersionParse(ctx, version.ID, parserName, parserVersion, language, metadata, documentSchema, quality.JSON(), status, blocks)
	if err != nil {
		return nil, err
	}
	structure := &ParsedStructure{Version: *saved, ParseQuality: quality, DocumentSchema: schemaExtraction, Warnings: quality.Warnings}
	if ast != nil {
		structure.Blocks = ast.Blocks
	}
	return structure, nil
}

func (s *Service) GetParsedStructure(ctx context.Context, actor *model.AppUser, versionID int64) (*ParsedStructure, error) {
	version, err := s.GetDocumentVersion(ctx, actor, versionID)
	if err != nil {
		return nil, err
	}
	rows, err := s.documents.ListDocumentVersionBlocks(ctx, version.ID)
	if err != nil {
		return nil, err
	}
	quality := ParseQuality{Level: ParseQualityFailed, Warnings: []ParseWarning{}}
	schemaExtraction := DocumentSchemaExtraction{Fields: map[string]ExtractedField{}, MissingFields: []string{}, Entities: []DocumentEntity{}, Sections: []SectionClassification{}, Diagnostics: []SchemaDiagnostic{}}
	if len(version.ParseQuality) > 0 {
		if err := json.Unmarshal(version.ParseQuality, &quality); err != nil {
			return nil, fmt.Errorf("decode parse quality: %w", err)
		}
	}
	if len(version.DocumentSchema) > 0 {
		if err := json.Unmarshal(version.DocumentSchema, &schemaExtraction); err != nil {
			return nil, fmt.Errorf("decode document schema: %w", err)
		}
	}
	return &ParsedStructure{
		Version: *version, ParseQuality: quality, DocumentSchema: schemaExtraction, Warnings: quality.Warnings, Blocks: restoreASTBlocks(rows),
	}, nil
}

func (s *Service) ensureParseSuccessful(ctx context.Context, documentID int64) error {
	version, err := s.documents.FindLatestDocumentVersion(ctx, documentID)
	if err != nil {
		return err
	}
	var quality ParseQuality
	if len(version.ParseQuality) == 0 || json.Unmarshal(version.ParseQuality, &quality) != nil || !quality.ParseSuccess {
		return ErrParseQualityFailed
	}
	return nil
}

func flattenASTBlocks(blocks []DocumentBlock) []model.KBDocumentBlock {
	result := make([]model.KBDocumentBlock, 0, countBlocks(blocks))
	var walk func([]DocumentBlock, *string)
	walk = func(current []DocumentBlock, parentKey *string) {
		for _, block := range current {
			sectionPath, _ := json.Marshal(block.SectionPath)
			attributes, _ := json.Marshal(block.Attributes)
			hashBytes := sha256.Sum256([]byte(block.Text))
			hash := hex.EncodeToString(hashBytes[:])
			row := model.KBDocumentBlock{
				BlockKey: block.ID, ParentBlockKey: parentKey, BlockType: block.Type,
				Level: block.Level, OrderNo: block.Order, PageNo: block.Page,
				SectionPath: sectionPath, TextContent: block.Text, Attributes: attributes, ContentHash: &hash,
			}
			result = append(result, row)
			key := block.ID
			walk(block.Children, &key)
		}
	}
	walk(blocks, nil)
	sort.SliceStable(result, func(i, j int) bool { return result[i].OrderNo < result[j].OrderNo })
	return result
}

func restoreASTBlocks(rows []model.KBDocumentBlock) []DocumentBlock {
	byParent := make(map[int64][]model.KBDocumentBlock)
	var roots []model.KBDocumentBlock
	for _, row := range rows {
		if row.ParentBlockID == nil {
			roots = append(roots, row)
		} else {
			byParent[*row.ParentBlockID] = append(byParent[*row.ParentBlockID], row)
		}
	}
	var build func(model.KBDocumentBlock) DocumentBlock
	build = func(row model.KBDocumentBlock) DocumentBlock {
		block := DocumentBlock{ID: row.BlockKey, Type: row.BlockType, Level: row.Level, Text: row.TextContent, Page: row.PageNo, Order: row.OrderNo, Attributes: map[string]any{}}
		_ = json.Unmarshal(row.SectionPath, &block.SectionPath)
		_ = json.Unmarshal(row.Attributes, &block.Attributes)
		for _, child := range byParent[row.ID] {
			block.Children = append(block.Children, build(child))
		}
		return block
	}
	result := make([]DocumentBlock, 0, len(roots))
	for _, root := range roots {
		result = append(result, build(root))
	}
	return result
}
