package document

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"aiops-platform/backend/internal/model"
)

func TestBuildSemanticChunksPreservesParentTraceAndCommandRiskContext(t *testing.T) {
	blocks := []model.KBDocumentBlock{
		chunkTestBlock(1, "heading", BlockTypeHeading, "执行变更", []string{"执行变更"}, nil),
		chunkTestBlock(2, "prerequisite", BlockTypeParagraph, "前置条件：确认双人审批", []string{"执行变更"}, nil),
		chunkTestBlock(3, "warning", BlockTypeWarning, "风险提示：命令会删除生产 Pod", []string{"执行变更"}, nil),
		chunkTestBlock(4, "command", BlockTypeCommand, "kubectl delete pod payment-0", []string{"执行变更"}, nil),
		chunkTestBlock(5, "verify", BlockTypeParagraph, "验证方式：确认新 Pod Ready", []string{"执行变更"}, nil),
	}
	chunks := buildChunkTestChunks(t, blocks)
	var command *model.KBChunk
	for index := range chunks {
		if chunks[index].ChunkType == ChunkTypeCodeWithContext {
			command = &chunks[index]
		}
	}
	if command == nil {
		t.Fatal("expected code_with_context chunk")
	}
	for _, expected := range []string{"风险提示", "kubectl delete", "验证方式"} {
		if !strings.Contains(command.Content, expected) {
			t.Fatalf("command chunk must retain %q, got %q", expected, command.Content)
		}
	}
	if command.ParentChunkIndex == nil {
		t.Fatal("child must reference a parent chunk")
	}
	var sourceIDs []string
	if err := json.Unmarshal(command.SourceBlockIDs, &sourceIDs); err != nil || !containsString(sourceIDs, "command") {
		t.Fatalf("chunk must trace to command AST block: %s", command.SourceBlockIDs)
	}
}

func TestBuildSemanticChunksRepeatsTableHeader(t *testing.T) {
	table := chunkTestBlock(10, "table", BlockTypeTable, "变更参数", []string{"参数"}, nil)
	header := chunkTestBlock(11, "header-row", BlockTypeTableRow, "", []string{"参数"}, &table.ID)
	headerA := chunkTestBlock(12, "header-name", BlockTypeTableCell, "参数", []string{"参数"}, &header.ID)
	headerB := chunkTestBlock(13, "header-value", BlockTypeTableCell, "值", []string{"参数"}, &header.ID)
	blocks := []model.KBDocumentBlock{table, header, headerA, headerB}
	for row := 0; row < 3; row++ {
		rowBlock := chunkTestBlock(int64(20+row*3), "row-"+string(rune('a'+row)), BlockTypeTableRow, "", []string{"参数"}, &table.ID)
		valueA := chunkTestBlock(rowBlock.ID+1, rowBlock.BlockKey+"-a", BlockTypeTableCell, "key", []string{"参数"}, &rowBlock.ID)
		valueB := chunkTestBlock(rowBlock.ID+2, rowBlock.BlockKey+"-b", BlockTypeTableCell, "value", []string{"参数"}, &rowBlock.ID)
		blocks = append(blocks, rowBlock, valueA, valueB)
	}
	strategy := chunkTestStrategy()
	strategy.Config = json.RawMessage(`{"mode":"semantic_ops","maxChildChars":1200,"tableRowsPerChunk":1,"contextBlocks":3,"parentChild":true}`)
	chunks, err := BuildSemanticChunks(chunkTestDocument(), chunkTestVersion(), strategy, blocks)
	if err != nil {
		t.Fatal(err)
	}
	tableCount := 0
	for _, chunk := range chunks {
		if chunk.ChunkType != ChunkTypeTable {
			continue
		}
		tableCount++
		if !strings.Contains(chunk.Content, "Header: 参数 | 值") {
			t.Fatalf("every table chunk must retain header, got %q", chunk.Content)
		}
	}
	if tableCount != 3 {
		t.Fatalf("expected 3 table chunks, got %d", tableCount)
	}
}

func TestDifferentStrategyVersionsProduceIndependentChunkIdentity(t *testing.T) {
	blocks := []model.KBDocumentBlock{chunkTestBlock(1, "p", BlockTypeParagraph, "检查服务状态", []string{"检查"}, nil)}
	first := chunkTestStrategy()
	second := chunkTestStrategy()
	second.ID, second.Version = 2, "2.0"
	firstChunks, err := BuildSemanticChunks(chunkTestDocument(), chunkTestVersion(), first, blocks)
	if err != nil {
		t.Fatal(err)
	}
	secondChunks, err := BuildSemanticChunks(chunkTestDocument(), chunkTestVersion(), second, blocks)
	if err != nil {
		t.Fatal(err)
	}
	if firstChunks[0].StrategyID == secondChunks[0].StrategyID {
		t.Fatal("strategy version must bind chunks to an independent strategy id")
	}
}

func TestChunkDocumentVersionKeepsOldStrategySet(t *testing.T) {
	store := newFakeRepository()
	ownerID := int64(7)
	owner := &model.AppUser{ID: ownerID, Role: model.RoleUser}
	docType := "runbook"
	store.documents[1] = &model.KBDocument{ID: 1, Title: "Runbook", DocType: &docType, CreatedBy: &ownerID}
	store.versions[10] = &model.KBDocumentVersion{ID: 10, DocumentID: 1, ParseQuality: ParseQuality{ParseSuccess: true}.JSON()}
	store.versionBlocks[10] = []model.KBDocumentBlock{chunkTestBlock(1, "step", BlockTypeParagraph, "检查实例状态", []string{"检查"}, nil)}
	store.strategies[1] = chunkTestStrategy()
	second := chunkTestStrategy()
	second.ID, second.Version = 2, "2.0"
	store.strategies[2] = second
	service := &Service{documents: store}

	first, err := service.ChunkDocumentVersion(context.Background(), owner, 10, 1)
	if err != nil {
		t.Fatal(err)
	}
	secondSet, err := service.ChunkDocumentVersion(context.Background(), owner, 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	all, err := service.ListDocumentVersionChunks(context.Background(), owner, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != len(first)+len(secondSet) {
		t.Fatalf("strategy change must preserve old set: first=%d second=%d all=%d", len(first), len(secondSet), len(all))
	}
	if _, err := service.ChunkDocumentVersion(context.Background(), owner, 10, 1); !errors.Is(err, ErrChunkSetExists) {
		t.Fatalf("same immutable chunk set must not be overwritten, got %v", err)
	}
}

func buildChunkTestChunks(t *testing.T, blocks []model.KBDocumentBlock) []model.KBChunk {
	t.Helper()
	chunks, err := BuildSemanticChunks(chunkTestDocument(), chunkTestVersion(), chunkTestStrategy(), blocks)
	if err != nil {
		t.Fatal(err)
	}
	return chunks
}

func chunkTestDocument() *model.KBDocument {
	docType := "runbook"
	return &model.KBDocument{ID: 1, Title: "运维手册", DocType: &docType}
}

func chunkTestVersion() *model.KBDocumentVersion {
	return &model.KBDocumentVersion{ID: 10, DocumentID: 1}
}

func chunkTestStrategy() *model.KBChunkStrategy {
	return &model.KBChunkStrategy{ID: 1, Name: "semantic-ops", Version: "1.0", Enabled: true, Config: json.RawMessage(`{"mode":"semantic_ops","maxChildChars":1200,"tableRowsPerChunk":20,"contextBlocks":3,"parentChild":true}`)}
}

func chunkTestBlock(id int64, key, blockType, text string, path []string, parentID *int64) model.KBDocumentBlock {
	pathJSON, _ := json.Marshal(path)
	return model.KBDocumentBlock{ID: id, DocumentVersionID: 10, BlockKey: key, ParentBlockID: parentID, BlockType: blockType, OrderNo: int(id), TextContent: text, SectionPath: pathJSON}
}
