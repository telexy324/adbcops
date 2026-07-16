package document

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"aiops-platform/backend/internal/model"
)

const (
	ChunkTypeSection         = "section"
	ChunkTypeProcedure       = "procedure"
	ChunkTypeStepGroup       = "step_group"
	ChunkTypeTable           = "table"
	ChunkTypeCodeWithContext = "code_with_context"
	ChunkTypeFAQPair         = "faq_pair"
	ChunkTypeIncidentSection = "incident_section"
	ChunkTypeFixedWindow     = "fixed_window"
)

type ChunkStrategyConfig struct {
	Mode              string `json:"mode"`
	MaxChildChars     int    `json:"maxChildChars"`
	TableRowsPerChunk int    `json:"tableRowsPerChunk"`
	ContextBlocks     int    `json:"contextBlocks"`
	ParentChild       bool   `json:"parentChild"`
}

func ParseChunkStrategyConfig(raw json.RawMessage) (ChunkStrategyConfig, error) {
	config := ChunkStrategyConfig{Mode: "semantic_ops", MaxChildChars: 1200, TableRowsPerChunk: 20, ContextBlocks: 3, ParentChild: true}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &config); err != nil {
			return ChunkStrategyConfig{}, fmt.Errorf("decode chunk strategy config: %w", err)
		}
	}
	if config.Mode != "semantic_ops" || !config.ParentChild || config.MaxChildChars < 200 || config.MaxChildChars > 12000 || config.TableRowsPerChunk < 1 || config.TableRowsPerChunk > 200 || config.ContextBlocks < 1 || config.ContextBlocks > 10 {
		return ChunkStrategyConfig{}, fmt.Errorf("%w: invalid semantic chunk strategy config", ErrInvalidInput)
	}
	return config, nil
}

type chunkSection struct {
	path   []string
	blocks []model.KBDocumentBlock
}

func BuildSemanticChunks(document *model.KBDocument, version *model.KBDocumentVersion, strategy *model.KBChunkStrategy, blocks []model.KBDocumentBlock) ([]model.KBChunk, error) {
	if document == nil || version == nil || strategy == nil || len(blocks) == 0 {
		return nil, ErrInvalidInput
	}
	config, err := ParseChunkStrategyConfig(strategy.Config)
	if err != nil {
		return nil, err
	}
	byParent := map[int64][]model.KBDocumentBlock{}
	var roots []model.KBDocumentBlock
	for _, block := range blocks {
		if block.ParentBlockID == nil {
			roots = append(roots, block)
		} else {
			byParent[*block.ParentBlockID] = append(byParent[*block.ParentBlockID], block)
		}
	}
	sort.SliceStable(roots, func(i, j int) bool { return roots[i].OrderNo < roots[j].OrderNo })
	sections := groupChunkSections(roots)
	chunks := make([]model.KBChunk, 0, len(roots)+len(sections))
	for sectionIndex, section := range sections {
		parentContent, parentIDs := renderBlocks(section.blocks, byParent)
		if strings.TrimSpace(parentContent) == "" {
			continue
		}
		parentIndex := len(chunks)
		parent := makeSemanticChunk(document, version, strategy, parentIndex, ChunkTypeSection, section.path, parentIDs, parentContent, "complete_section", fmt.Sprintf("section-%d", sectionIndex), nil, nil)
		setChunkPages(&parent, section.blocks)
		chunks = append(chunks, parent)
		children := buildSectionChildren(document, version, strategy, section, byParent, config, parentIndex, sectionIndex)
		chunks = append(chunks, children...)
	}
	for index := range chunks {
		chunks[index].ChunkIndex = index
	}
	return chunks, nil
}

func groupChunkSections(roots []model.KBDocumentBlock) []chunkSection {
	sections := []chunkSection{}
	indexByKey := map[string]int{}
	for _, block := range roots {
		path := decodeSectionPath(block.SectionPath)
		if len(path) == 0 && block.BlockType == BlockTypeHeading && strings.TrimSpace(block.TextContent) != "" {
			path = []string{strings.TrimSpace(block.TextContent)}
		}
		key := strings.Join(path, "\x00")
		index, ok := indexByKey[key]
		if !ok {
			index = len(sections)
			indexByKey[key] = index
			sections = append(sections, chunkSection{path: path})
		}
		sections[index].blocks = append(sections[index].blocks, block)
	}
	return sections
}

func buildSectionChildren(document *model.KBDocument, version *model.KBDocumentVersion, strategy *model.KBChunkStrategy, section chunkSection, byParent map[int64][]model.KBDocumentBlock, config ChunkStrategyConfig, parentIndex, sectionIndex int) []model.KBChunk {
	children := []model.KBChunk{}
	regular := []model.KBDocumentBlock{}
	flushRegular := func() {
		if len(regular) == 0 {
			return
		}
		content, ids := renderBlocks(regular, byParent)
		before, after := neighboringContext(section.blocks, regular[0].ID, regular[len(regular)-1].ID, config.ContextBlocks)
		children = append(children, makeSemanticChunk(document, version, strategy, 0, semanticChunkType(section.path, regular), section.path, ids, content, "semantic_block_group", fmt.Sprintf("section-%d", sectionIndex), &before, &after))
		setChunkPages(&children[len(children)-1], regular)
		children[len(children)-1].ParentChunkIndex = &parentIndex
		regular = nil
	}
	regularSize := 0
	for index := 0; index < len(section.blocks); index++ {
		block := section.blocks[index]
		switch block.BlockType {
		case BlockTypeTable:
			flushRegular()
			for _, chunk := range buildTableChunks(document, version, strategy, section.path, block, byParent, parentIndex, sectionIndex, config.TableRowsPerChunk) {
				children = append(children, chunk)
			}
		case BlockTypeCode, BlockTypeCommand:
			flushRegular()
			before, after := commandContext(section.blocks, index, config.ContextBlocks)
			content := joinCommandContext(before, block.TextContent, after)
			ids := []string{block.BlockKey}
			for _, contextBlock := range append(append([]model.KBDocumentBlock{}, before...), after...) {
				ids = append(ids, contextBlock.BlockKey)
			}
			beforeText, _ := renderBlocks(before, byParent)
			afterText, _ := renderBlocks(after, byParent)
			chunk := makeSemanticChunk(document, version, strategy, 0, ChunkTypeCodeWithContext, section.path, uniqueStrings(ids), content, "command_with_context", fmt.Sprintf("section-%d", sectionIndex), &beforeText, &afterText)
			pageBlocks := append(append([]model.KBDocumentBlock{}, before...), block)
			pageBlocks = append(pageBlocks, after...)
			setChunkPages(&chunk, pageBlocks)
			chunk.ParentChunkIndex = &parentIndex
			children = append(children, chunk)
		default:
			text, _ := renderBlocks([]model.KBDocumentBlock{block}, byParent)
			if len(regular) > 0 && regularSize+utf8.RuneCountInString(text) > config.MaxChildChars {
				flushRegular()
				regularSize = 0
			}
			regular = append(regular, block)
			regularSize += utf8.RuneCountInString(text)
			if isRiskWarning(block) && index+1 < len(section.blocks) {
				next := section.blocks[index+1]
				if next.BlockType != BlockTypeTable && next.BlockType != BlockTypeCode && next.BlockType != BlockTypeCommand {
					regular = append(regular, next)
					index++
				}
			}
		}
	}
	flushRegular()
	return children
}

func buildTableChunks(document *model.KBDocument, version *model.KBDocumentVersion, strategy *model.KBChunkStrategy, path []string, table model.KBDocumentBlock, byParent map[int64][]model.KBDocumentBlock, parentIndex, sectionIndex, rowsPerChunk int) []model.KBChunk {
	rows := byParent[table.ID]
	if len(rows) == 0 {
		content := strings.TrimSpace(table.TextContent)
		chunk := makeSemanticChunk(document, version, strategy, 0, ChunkTypeTable, path, []string{table.BlockKey}, content, "table", fmt.Sprintf("section-%d-table-%d", sectionIndex, table.ID), nil, nil)
		setChunkPages(&chunk, []model.KBDocumentBlock{table})
		chunk.ParentChunkIndex = &parentIndex
		return []model.KBChunk{chunk}
	}
	header := rows[0]
	headerText, headerIDs := renderTableRow(header, byParent)
	dataRows := rows[1:]
	if len(dataRows) == 0 {
		dataRows = rows[:1]
	}
	result := []model.KBChunk{}
	for start := 0; start < len(dataRows); start += rowsPerChunk {
		end := start + rowsPerChunk
		if end > len(dataRows) {
			end = len(dataRows)
		}
		lines := []string{"Header: " + headerText}
		ids := append([]string{table.BlockKey, header.BlockKey}, headerIDs...)
		for rowIndex, row := range dataRows[start:end] {
			rowText, rowIDs := renderTableRow(row, byParent)
			lines = append(lines, fmt.Sprintf("Row %d: %s", start+rowIndex+1, rowText))
			ids = append(ids, row.BlockKey)
			ids = append(ids, rowIDs...)
		}
		unit := fmt.Sprintf("table_rows_%d_%d", start+1, end)
		chunk := makeSemanticChunk(document, version, strategy, 0, ChunkTypeTable, path, uniqueStrings(ids), strings.Join(lines, "\n"), unit, fmt.Sprintf("section-%d-table-%d", sectionIndex, table.ID), nil, nil)
		setChunkPages(&chunk, []model.KBDocumentBlock{table})
		chunk.ParentChunkIndex = &parentIndex
		result = append(result, chunk)
	}
	return result
}

func renderBlocks(blocks []model.KBDocumentBlock, byParent map[int64][]model.KBDocumentBlock) (string, []string) {
	lines, ids := []string{}, []string{}
	for _, block := range blocks {
		ids = append(ids, block.BlockKey)
		if block.BlockType == BlockTypeTable {
			for _, row := range byParent[block.ID] {
				text, rowIDs := renderTableRow(row, byParent)
				if text != "" {
					lines = append(lines, text)
				}
				ids = append(ids, row.BlockKey)
				ids = append(ids, rowIDs...)
			}
			continue
		}
		if text := strings.TrimSpace(block.TextContent); text != "" {
			if block.BlockType == BlockTypeHeading {
				text = strings.Repeat("#", maxInt(1, block.Level)) + " " + text
			}
			lines = append(lines, text)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n\n")), uniqueStrings(ids)
}

func renderTableRow(row model.KBDocumentBlock, byParent map[int64][]model.KBDocumentBlock) (string, []string) {
	cells, ids := []string{}, []string{}
	for _, cell := range byParent[row.ID] {
		cells = append(cells, strings.TrimSpace(cell.TextContent))
		ids = append(ids, cell.BlockKey)
	}
	return strings.Join(cells, " | "), ids
}

func makeSemanticChunk(document *model.KBDocument, version *model.KBDocumentVersion, strategy *model.KBChunkStrategy, index int, chunkType string, path, blockIDs []string, content, unit, sibling string, before, after *string) model.KBChunk {
	sectionJSON, _ := json.Marshal(path)
	blockJSON, _ := json.Marshal(blockIDs)
	hash := sha256.Sum256([]byte(content))
	section := strings.Join(path, " / ")
	chunk := model.KBChunk{DocumentID: document.ID, DocumentVersionID: version.ID, StrategyID: strategy.ID, ChunkIndex: index, ChunkType: chunkType, SectionPath: sectionJSON, SourceBlockIDs: blockJSON, Content: strings.TrimSpace(content), ContextBefore: optionalTrimmed(before), ContextAfter: optionalTrimmed(after), TokenCount: utf8.RuneCountInString(strings.TrimSpace(content)), ContentHash: hex.EncodeToString(hash[:]), SiblingGroup: &sibling, SemanticUnit: &unit}
	if document.Title != "" {
		chunk.SourceTitle = &document.Title
	}
	if section != "" {
		chunk.SourceSection = &section
	}
	EnhanceChunk(&chunk)
	return chunk
}

func semanticChunkType(path []string, blocks []model.KBDocumentBlock) string {
	joined := strings.ToLower(strings.Join(path, " "))
	switch {
	case strings.Contains(joined, "faq") || strings.Contains(joined, "问答"):
		return ChunkTypeFAQPair
	case strings.Contains(joined, "incident") || strings.Contains(joined, "故障"):
		return ChunkTypeIncidentSection
	case strings.Contains(joined, "step") || strings.Contains(joined, "步骤") || containsBlockType(blocks, BlockTypeListItem):
		return ChunkTypeStepGroup
	default:
		return ChunkTypeProcedure
	}
}

func commandContext(blocks []model.KBDocumentBlock, index, limit int) ([]model.KBDocumentBlock, []model.KBDocumentBlock) {
	start := index - limit
	if start < 0 {
		start = 0
	}
	end := index + limit + 1
	if end > len(blocks) {
		end = len(blocks)
	}
	before := append([]model.KBDocumentBlock(nil), blocks[start:index]...)
	after := append([]model.KBDocumentBlock(nil), blocks[index+1:end]...)
	return before, after
}

func neighboringContext(blocks []model.KBDocumentBlock, firstID, lastID int64, limit int) (string, string) {
	first, last := 0, len(blocks)-1
	for index, block := range blocks {
		if block.ID == firstID {
			first = index
		}
		if block.ID == lastID {
			last = index
		}
	}
	beforeStart := first - limit
	if beforeStart < 0 {
		beforeStart = 0
	}
	afterEnd := last + limit + 1
	if afterEnd > len(blocks) {
		afterEnd = len(blocks)
	}
	before, _ := renderBlocks(blocks[beforeStart:first], nil)
	after, _ := renderBlocks(blocks[last+1:afterEnd], nil)
	return before, after
}

func joinCommandContext(before []model.KBDocumentBlock, command string, after []model.KBDocumentBlock) string {
	beforeText, _ := renderBlocks(before, nil)
	afterText, _ := renderBlocks(after, nil)
	parts := []string{}
	if beforeText != "" {
		parts = append(parts, "Prerequisites / Risk / Step:\n"+beforeText)
	}
	parts = append(parts, "Command:\n"+strings.TrimSpace(command))
	if afterText != "" {
		parts = append(parts, "Verification / Rollback:\n"+afterText)
	}
	return strings.Join(parts, "\n\n")
}

func decodeSectionPath(raw []byte) []string {
	var path []string
	_ = json.Unmarshal(raw, &path)
	return path
}

func containsBlockType(blocks []model.KBDocumentBlock, blockType string) bool {
	for _, block := range blocks {
		if block.BlockType == blockType {
			return true
		}
	}
	return false
}

func isRiskWarning(block model.KBDocumentBlock) bool {
	text := strings.ToLower(block.TextContent)
	return block.BlockType == BlockTypeWarning || strings.Contains(text, "warning") || strings.Contains(text, "risk") || strings.Contains(text, "警告") || strings.Contains(text, "风险") || strings.Contains(text, "注意")
}

func uniqueStrings(values []string) []string {
	seen, result := map[string]struct{}{}, []string{}
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func optionalTrimmed(value *string) *string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

func setChunkPages(chunk *model.KBChunk, blocks []model.KBDocumentBlock) {
	if chunk == nil {
		return
	}
	var start, end *int
	for _, block := range blocks {
		if block.PageNo == nil {
			continue
		}
		if start == nil || *block.PageNo < *start {
			value := *block.PageNo
			start = &value
		}
		if end == nil || *block.PageNo > *end {
			value := *block.PageNo
			end = &value
		}
	}
	chunk.SourcePageStart, chunk.SourcePageEnd = start, end
	chunk.SourcePage = start
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
