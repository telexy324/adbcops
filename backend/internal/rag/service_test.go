package rag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	llmsvc "aiops-platform/backend/internal/llm"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

func TestAskWithoutEvidenceReturnsClearNoEvidenceAnswer(t *testing.T) {
	store := newFakeRepository()
	service := NewService(store, nil, nil)
	result, err := service.Ask(context.Background(), &model.AppUser{ID: 7, Role: model.RoleUser}, AskInput{Question: "数据库连接池怎么排查？"})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if result.Answer != noEvidenceAnswer || result.RecallCount != 0 || len(result.Citations) != 0 {
		t.Fatalf("result = %+v", result)
	}
	if len(store.qaRecords) != 1 || len(store.messages[result.Conversation.ID]) != 2 {
		t.Fatalf("qaRecords=%d messages=%d", len(store.qaRecords), len(store.messages[result.Conversation.ID]))
	}
}

func TestAskCitesRealPublishedChunkOnly(t *testing.T) {
	store := newFakeRepository()
	publishedID := store.addDocument(model.DocumentStatusPublished)
	draftID := store.addDocument(model.DocumentStatusDraft)
	store.addChunk(publishedID, "数据库连接池耗尽时先查看活跃连接和慢查询。")
	store.addChunk(draftID, "数据库连接池草稿内容不应进入正式回答。")
	service := NewService(store, nil, nil)

	result, err := service.Ask(context.Background(), &model.AppUser{ID: 7, Role: model.RoleUser}, AskInput{Question: "数据库连接池怎么排查？", Limit: 5})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if result.RecallCount != 1 || len(result.Citations) != 1 {
		t.Fatalf("recall=%d citations=%+v", result.RecallCount, result.Citations)
	}
	if result.Citations[0].DocumentID != publishedID || result.Citations[0].ChunkID == 0 {
		t.Fatalf("citation does not point to real published chunk: %+v", result.Citations[0])
	}
	if strings.Contains(result.Answer, "草稿") {
		t.Fatalf("answer leaked unpublished content: %q", result.Answer)
	}
	var storedCitations []Citation
	if err := json.Unmarshal(store.qaRecords[0].Citations, &storedCitations); err != nil {
		t.Fatalf("qa record citations are invalid: %v", err)
	}
	if len(storedCitations) != 1 || storedCitations[0].ChunkID != result.Citations[0].ChunkID {
		t.Fatalf("stored citations = %+v, want %+v", storedCitations, result.Citations)
	}
}

func TestAskRejectsForeignConversation(t *testing.T) {
	store := newFakeRepository()
	conversation := store.addConversation(99, "foreign")
	service := NewService(store, nil, nil)
	_, err := service.Ask(context.Background(), &model.AppUser{ID: 7, Role: model.RoleUser}, AskInput{ConversationID: &conversation.ID, Question: "hello"})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Ask() error = %v, want ErrForbidden", err)
	}
}

func TestAskWorksWithEmbeddingAndRerankModels(t *testing.T) {
	store := newFakeRepository()
	store.llmConfigs[model.LLMPurposeEmbedding] = &model.LLMConfig{ID: 2, Purpose: model.LLMPurposeEmbedding, BaseURL: "https://embed.example", Model: "embed-model", Enabled: true, IsDefault: true}
	store.llmConfigs[model.LLMPurposeRerank] = &model.LLMConfig{ID: 3, Purpose: model.LLMPurposeRerank, BaseURL: "https://rerank.example", Model: "rerank-model", Enabled: true, IsDefault: true}
	firstID := store.addDocument(model.DocumentStatusPublished)
	secondID := store.addDocument(model.DocumentStatusPublished)
	store.addChunk(firstID, "缓存容量规划和淘汰策略。")
	store.addChunk(secondID, "数据库连接池耗尽时先查看活跃连接和慢查询。")
	client := &semanticFakeClient{}
	service := NewService(store, nil, client)

	result, err := service.Ask(context.Background(), &model.AppUser{ID: 7, Role: model.RoleUser}, AskInput{Question: "连接池耗尽怎么排查？", Limit: 1})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if result.RecallCount != 1 || result.Citations[0].DocumentID != secondID {
		t.Fatalf("result = %+v", result)
	}
	if client.embedCalls == 0 || client.rerankCalls == 0 {
		t.Fatalf("embedCalls=%d rerankCalls=%d", client.embedCalls, client.rerankCalls)
	}
	if store.trigramCalls == 0 || store.denseCalls == 0 {
		t.Fatalf("trigramCalls=%d denseCalls=%d", store.trigramCalls, store.denseCalls)
	}
}

func TestFuseRRFCombinesChannelRanks(t *testing.T) {
	first := model.KBChunk{ID: 1}
	second := model.KBChunk{ID: 2}
	result := fuseRRF(map[string][]repository.RankedKnowledgeChunk{
		"dense_vector":  {{Chunk: first, Score: .9}, {Chunk: second, Score: .8}},
		"pg_trgm":       {{Chunk: second, Score: .7}, {Chunk: first, Score: .6}},
		"exact_keyword": {{Chunk: second, Score: 1}},
	}, 60, 10)
	if len(result) != 2 || result[0].Chunk.ID != second.ID {
		t.Fatalf("fused result = %+v", result)
	}
	want := 1.0/61 + 1.0/61 + 1.0/62
	if diff := result[0].Score - want; diff < -1e-12 || diff > 1e-12 {
		t.Fatalf("score=%v want=%v", result[0].Score, want)
	}
	if len(result[0].Ranks) != 3 {
		t.Fatalf("ranks = %+v", result[0].Ranks)
	}
}

func TestAskDenseFailureDegradesToLexical(t *testing.T) {
	store := newFakeRepository()
	store.failDense = true
	store.llmConfigs[model.LLMPurposeEmbedding] = &model.LLMConfig{ID: 2, Purpose: model.LLMPurposeEmbedding, Model: "embed-model", Enabled: true, IsDefault: true}
	documentID := store.addDocument(model.DocumentStatusPublished)
	store.addChunk(documentID, "数据库连接池耗尽时先查看活跃连接。")
	service := NewService(store, nil, &semanticFakeClient{})
	result, err := service.Ask(context.Background(), &model.AppUser{ID: 7, Role: model.RoleUser}, AskInput{Question: "数据库连接池怎么排查？"})
	if err != nil || result.RecallCount != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if store.trigramCalls == 0 {
		t.Fatal("lexical channel was not executed")
	}
	foundDegraded := false
	for _, channel := range result.Retrieval.Channels {
		if channel.Channel == "dense_vector" && channel.Degraded {
			foundDegraded = true
		}
	}
	if !foundDegraded {
		t.Fatalf("trace = %+v", result.Retrieval)
	}
}

func TestAskAppliesMetadataFilterBeforeChannels(t *testing.T) {
	store := newFakeRepository()
	service := NewService(store, nil, nil)
	result, err := service.Ask(context.Background(), &model.AppUser{ID: 7, Role: model.RoleUser}, AskInput{Question: "生产环境故障排查"})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if store.lastFilter.PermissionScope != "authenticated_published" || store.lastFilter.Environment != "prod" {
		t.Fatalf("filter = %+v", store.lastFilter)
	}
	if len(store.lastFilter.DocTypes) != 1 || store.lastFilter.DocTypes[0] != "incident_report" {
		t.Fatalf("docTypes = %+v", store.lastFilter.DocTypes)
	}
	if len(result.Retrieval.Channels) == 0 || result.Retrieval.Channels[0].Channel != "metadata_filter" {
		t.Fatalf("channels = %+v", result.Retrieval.Channels)
	}
}

func TestAskRerankFailureFallsBackWithoutBlocking(t *testing.T) {
	store := newFakeRepository()
	store.llmConfigs[model.LLMPurposeRerank] = &model.LLMConfig{ID: 3, Purpose: model.LLMPurposeRerank, Model: "rerank-model", Enabled: true, IsDefault: true}
	documentID := store.addDocument(model.DocumentStatusPublished)
	store.addChunk(documentID, "数据库连接池耗尽时先查看活跃连接。")
	service := NewService(store, nil, &rerankFailureClient{})
	result, err := service.Ask(context.Background(), &model.AppUser{ID: 7, Role: model.RoleUser}, AskInput{Question: "连接池怎么排查？"})
	if err != nil || result.RecallCount != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !result.Retrieval.Rerank.Degraded || result.Retrieval.Rerank.Error == "" {
		t.Fatalf("rerank trace = %+v", result.Retrieval.Rerank)
	}
}

func TestContextBuilderLimitsSingleDocumentShare(t *testing.T) {
	chunks := []model.KBChunk{}
	for index := 0; index < 6; index++ {
		chunks = append(chunks, model.KBChunk{ID: int64(index + 1), DocumentID: 1})
	}
	chunks = append(chunks, model.KBChunk{ID: 7, DocumentID: 2}, model.KBChunk{ID: 8, DocumentID: 2})
	limited := limitDocumentShare(chunks, 6)
	counts := map[int64]int{}
	for _, chunk := range limited {
		counts[chunk.DocumentID]++
	}
	if counts[1] != 3 || counts[2] != 2 {
		t.Fatalf("document counts = %+v", counts)
	}
}

func TestContextBuilderPreservesTableHeaderAndRiskContext(t *testing.T) {
	table := model.KBChunk{ID: 1, DocumentID: 1, DocumentVersionID: 10, StrategyID: 2, ChunkIndex: 1, ChunkType: "table", Content: "Header: name | action\nRow 1: api | restart"}
	blocks, _ := NewService(newFakeRepository(), nil, nil).buildContext(context.Background(), []model.KBChunk{table}, nil, nil, 1, 25)
	if len(blocks) != 1 || !strings.HasPrefix(blocks[0].Content, "Header: name | action") {
		t.Fatalf("table context = %+v", blocks)
	}
	before, after := "变更已审批，确认影响范围。", "执行后验证服务并按预案回滚。"
	command := model.KBChunk{Content: "kubectl delete pod payment-0", ContextBefore: &before, ContextAfter: &after}
	protected := protectRiskContext(command)
	for _, expected := range []string{"Risk Context", before, after, command.Content} {
		if !strings.Contains(protected, expected) {
			t.Fatalf("risk context missing %q: %s", expected, protected)
		}
	}
}

func TestContextCitationPointsToVersionAndMergedChunks(t *testing.T) {
	sibling := "steps"
	chunks := []model.KBChunk{
		{ID: 11, DocumentID: 1, DocumentVersionID: 9, StrategyID: 2, ChunkIndex: 3, SiblingGroup: &sibling, Content: "步骤一：检查连接数。"},
		{ID: 12, DocumentID: 1, DocumentVersionID: 9, StrategyID: 2, ChunkIndex: 4, SiblingGroup: &sibling, Content: "步骤二：检查慢查询。"},
	}
	evidence := map[int64]ContextEvidenceTrace{11: {ChunkID: 11, RRFScore: .2, RerankRank: 1}, 12: {ChunkID: 12, RRFScore: .1, RerankRank: 2}}
	blocks, trace := NewService(newFakeRepository(), nil, nil).buildContext(context.Background(), chunks, nil, evidence, 6, 1000)
	citations := buildContextCitations(blocks)
	if len(citations) != 1 || citations[0].DocumentVersionID != 9 || len(citations[0].ChunkIDs) != 2 {
		t.Fatalf("citations = %+v", citations)
	}
	if len(trace.Blocks) != 1 || len(trace.Blocks[0].RetrievalTrace) != 2 {
		t.Fatalf("context trace = %+v", trace)
	}
}

func TestContextBuilderExpandsParentTitle(t *testing.T) {
	parentID := int64(20)
	section := "数据库 / 应急操作"
	child := model.KBChunk{ID: 21, DocumentID: 1, DocumentVersionID: 9, StrategyID: 2, ParentChunkID: &parentID, Content: "检查当前连接数。"}
	parent := model.KBChunk{ID: parentID, DocumentID: 1, DocumentVersionID: 9, SourceSection: &section, Content: "完整章节内容"}
	content, expanded := buildGroupContent([]model.KBChunk{child}, map[int64]model.KBChunk{parentID: parent})
	if !expanded || !strings.Contains(content, "Parent: "+section) {
		t.Fatalf("content=%q expanded=%v", content, expanded)
	}
}

func TestEvaluateRetrievalUsesExplicitConfigurationWithoutWritingQA(t *testing.T) {
	store := newFakeRepository()
	embedding := &model.LLMConfig{ID: 2, Purpose: model.LLMPurposeEmbedding, Model: "embed-model", Enabled: true}
	rerank := &model.LLMConfig{ID: 3, Purpose: model.LLMPurposeRerank, Model: "rerank-model", Enabled: true}
	store.llmConfigs[model.LLMPurposeEmbedding], store.llmConfigs[model.LLMPurposeRerank] = embedding, rerank
	documentID := store.addDocument(model.DocumentStatusPublished)
	store.addChunk(documentID, "数据库连接池排障步骤。")
	strategyID := int64(8)
	result, err := NewService(store, nil, &semanticFakeClient{}).EvaluateRetrieval(context.Background(), &model.AppUser{ID: 1, Role: model.RoleAdmin}, EvaluationSearchInput{
		Question: "连接池排障", EmbeddingConfigID: &embedding.ID, EmbeddingModelRevision: "revision-v2",
		RerankConfigID: &rerank.ID, ChunkStrategyID: &strategyID,
	})
	if err != nil || len(result.Citations) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	config := result.RetrievalTrace.Configuration
	if config.EmbeddingConfigID == nil || *config.EmbeddingConfigID != 2 || config.EmbeddingModelRevision != "revision-v2" || config.RerankConfigID == nil || config.ChunkStrategyID == nil {
		t.Fatalf("configuration = %+v", config)
	}
	if len(store.qaRecords) != 0 || len(store.conversations) != 0 || len(store.messages) != 0 {
		t.Fatalf("evaluation wrote production records: qa=%d conversations=%d messages=%d", len(store.qaRecords), len(store.conversations), len(store.messages))
	}
}

type fakeRepository struct {
	nextConversationID int64
	nextMessageID      int64
	nextDocumentID     int64
	nextChunkID        int64
	nextQARecordID     int64
	conversations      map[int64]*model.Conversation
	messages           map[int64][]model.Message
	documents          map[int64]string
	chunks             map[int64][]model.KBChunk
	embeddings         map[string]model.KBChunkEmbedding
	qaRecords          []model.QARecord
	llmConfigs         map[string]*model.LLMConfig
	trigramCalls       int
	denseCalls         int
	failDense          bool
	lastFilter         repository.KnowledgeRetrievalFilter
}

func (f *fakeRepository) rankedSearch(query string, limit int) []repository.RankedKnowledgeChunk {
	chunks, _ := f.SearchChunks(context.Background(), query, limit)
	results := make([]repository.RankedKnowledgeChunk, 0, len(chunks))
	for index, chunk := range chunks {
		results = append(results, repository.RankedKnowledgeChunk{Chunk: chunk, Score: 1 / float64(index+1)})
	}
	return results
}

func (f *fakeRepository) SearchChunksTrigram(_ context.Context, query string, filter repository.KnowledgeRetrievalFilter, limit int) ([]repository.RankedKnowledgeChunk, error) {
	f.trigramCalls++
	f.lastFilter = filter
	return f.rankedSearch(query, limit), nil
}

func (f *fakeRepository) SearchChunksExact(_ context.Context, terms []string, filter repository.KnowledgeRetrievalFilter, limit int) ([]repository.RankedKnowledgeChunk, error) {
	f.lastFilter = filter
	return f.rankedSearch(strings.Join(terms, " "), limit), nil
}

func (f *fakeRepository) SearchChunksTitleSection(_ context.Context, query string, filter repository.KnowledgeRetrievalFilter, limit int) ([]repository.RankedKnowledgeChunk, error) {
	f.lastFilter = filter
	return f.rankedSearch(query, limit), nil
}

func (f *fakeRepository) SearchChunksPossibleQuestions(_ context.Context, _ string, filter repository.KnowledgeRetrievalFilter, _ int) ([]repository.RankedKnowledgeChunk, error) {
	f.lastFilter = filter
	return nil, nil
}

func (f *fakeRepository) SearchChunksDense(_ context.Context, _ []float64, _ int64, _ string, _ repository.KnowledgeRetrievalFilter, limit int) ([]repository.RankedKnowledgeChunk, error) {
	f.denseCalls++
	if f.failDense {
		return nil, errors.New("dense index unavailable")
	}
	var results []repository.RankedKnowledgeChunk
	for documentID, chunks := range f.chunks {
		if f.documents[documentID] != model.DocumentStatusPublished {
			continue
		}
		for _, chunk := range chunks {
			score := .1
			if strings.Contains(chunk.Content, "连接池") {
				score = .9
			}
			results = append(results, repository.RankedKnowledgeChunk{Chunk: chunk, Score: score})
		}
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (f *fakeRepository) FindKnowledgeDocumentsByIDs(_ context.Context, ids []int64) ([]model.KBDocument, error) {
	results := make([]model.KBDocument, 0, len(ids))
	for _, id := range ids {
		if f.documents[id] != model.DocumentStatusPublished {
			continue
		}
		results = append(results, model.KBDocument{ID: id, Title: "Runbook", Status: model.DocumentStatusPublished})
	}
	return results, nil
}

func (f *fakeRepository) FindKnowledgeChunksByIDs(_ context.Context, ids []int64) ([]model.KBChunk, error) {
	wanted := map[int64]struct{}{}
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	results := []model.KBChunk{}
	for _, chunks := range f.chunks {
		for _, chunk := range chunks {
			if _, ok := wanted[chunk.ID]; ok {
				results = append(results, chunk)
			}
		}
	}
	return results, nil
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		nextConversationID: 1,
		nextMessageID:      1,
		nextDocumentID:     1,
		nextChunkID:        1,
		nextQARecordID:     1,
		conversations:      make(map[int64]*model.Conversation),
		messages:           make(map[int64][]model.Message),
		documents:          make(map[int64]string),
		chunks:             make(map[int64][]model.KBChunk),
		embeddings:         make(map[string]model.KBChunkEmbedding),
		llmConfigs:         make(map[string]*model.LLMConfig),
	}
}

func (f *fakeRepository) addConversation(userID int64, title string) *model.Conversation {
	conversation := &model.Conversation{UserID: userID, Title: &title, Status: model.ConversationStatusActive}
	_ = f.CreateConversation(context.Background(), conversation)
	return conversation
}

func (f *fakeRepository) addDocument(status string) int64 {
	id := f.nextDocumentID
	f.nextDocumentID++
	f.documents[id] = status
	return id
}

func (f *fakeRepository) addChunk(documentID int64, content string) {
	title := "Runbook"
	chunk := model.KBChunk{
		ID:          f.nextChunkID,
		DocumentID:  documentID,
		ChunkIndex:  len(f.chunks[documentID]),
		Content:     content,
		SourceTitle: &title,
	}
	f.nextChunkID++
	f.chunks[documentID] = append(f.chunks[documentID], chunk)
}

func (f *fakeRepository) CreateConversation(_ context.Context, conversation *model.Conversation) error {
	conversation.ID = f.nextConversationID
	f.nextConversationID++
	f.conversations[conversation.ID] = conversation
	return nil
}

func (f *fakeRepository) FindConversationByID(_ context.Context, id int64) (*model.Conversation, error) {
	conversation, ok := f.conversations[id]
	if !ok || conversation.Status == model.ConversationStatusDeleted {
		return nil, repository.ErrNotFound
	}
	return conversation, nil
}

func (f *fakeRepository) CreateMessage(_ context.Context, message *model.Message) error {
	if _, ok := f.conversations[message.ConversationID]; !ok {
		return repository.ErrNotFound
	}
	message.ID = f.nextMessageID
	f.nextMessageID++
	f.messages[message.ConversationID] = append(f.messages[message.ConversationID], *message)
	return nil
}

func (f *fakeRepository) SearchChunks(_ context.Context, query string, limit int) ([]model.KBChunk, error) {
	var results []model.KBChunk
	for documentID, chunks := range f.chunks {
		if f.documents[documentID] != model.DocumentStatusPublished {
			continue
		}
		for _, chunk := range chunks {
			if strings.Contains(chunk.Content, "数据库连接池") || strings.Contains(chunk.Content, query) {
				results = append(results, chunk)
				if len(results) >= limit {
					return results, nil
				}
			}
		}
	}
	return results, nil
}

func (f *fakeRepository) ListPublishedChunks(_ context.Context, limit int) ([]model.KBChunk, error) {
	var results []model.KBChunk
	for documentID, chunks := range f.chunks {
		if f.documents[documentID] != model.DocumentStatusPublished {
			continue
		}
		results = append(results, chunks...)
		if len(results) >= limit {
			return results[:limit], nil
		}
	}
	return results, nil
}

func (f *fakeRepository) ListPublishedChunkEmbeddings(_ context.Context, modelName string, limit int) ([]model.KBChunkEmbedding, error) {
	var results []model.KBChunkEmbedding
	for _, embedding := range f.embeddings {
		if embedding.Model != modelName {
			continue
		}
		chunk, ok := f.findChunk(embedding.ChunkID)
		if !ok || f.documents[chunk.DocumentID] != model.DocumentStatusPublished {
			continue
		}
		embedding.Chunk = chunk
		results = append(results, embedding)
		if len(results) >= limit {
			return results, nil
		}
	}
	return results, nil
}

func (f *fakeRepository) ListPublishedChunksMissingEmbedding(_ context.Context, modelName string, limit int) ([]model.KBChunk, error) {
	var results []model.KBChunk
	for documentID, chunks := range f.chunks {
		if f.documents[documentID] != model.DocumentStatusPublished {
			continue
		}
		for _, chunk := range chunks {
			if _, ok := f.embeddings[embeddingKey(chunk.ID, modelName)]; ok {
				continue
			}
			results = append(results, chunk)
			if len(results) >= limit {
				return results, nil
			}
		}
	}
	return results, nil
}

func (f *fakeRepository) UpsertChunkEmbeddings(_ context.Context, embeddings []model.KBChunkEmbedding) error {
	for _, embedding := range embeddings {
		f.embeddings[embeddingKey(embedding.ChunkID, embedding.Model)] = embedding
	}
	return nil
}

func (f *fakeRepository) findChunk(chunkID int64) (model.KBChunk, bool) {
	for _, chunks := range f.chunks {
		for _, chunk := range chunks {
			if chunk.ID == chunkID {
				return chunk, true
			}
		}
	}
	return model.KBChunk{}, false
}

func embeddingKey(chunkID int64, modelName string) string {
	return fmt.Sprintf("%d:%s", chunkID, modelName)
}

func (f *fakeRepository) FindDefaultEnabledLLMConfig(_ context.Context) (*model.LLMConfig, error) {
	return f.FindDefaultEnabledLLMConfigByPurpose(context.Background(), model.LLMPurposeChat)
}

func (f *fakeRepository) FindDefaultEnabledLLMConfigByPurpose(_ context.Context, purpose string) (*model.LLMConfig, error) {
	config, ok := f.llmConfigs[purpose]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return config, nil
}

func (f *fakeRepository) FindLLMConfigByID(_ context.Context, id int64) (*model.LLMConfig, error) {
	for _, config := range f.llmConfigs {
		if config.ID == id {
			return config, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (f *fakeRepository) FindReadyEmbeddingModelRevision(_ context.Context, _ int64, _ *int64) (string, error) {
	return "test-revision", nil
}

func (f *fakeRepository) CreateQARecord(_ context.Context, record *model.QARecord) error {
	record.ID = f.nextQARecordID
	f.nextQARecordID++
	f.qaRecords = append(f.qaRecords, *record)
	return nil
}

type semanticFakeClient struct {
	embedCalls  int
	rerankCalls int
}

type rerankFailureClient struct{}

func (f *rerankFailureClient) Chat(_ context.Context, req llmsvc.ChatRequest) (*llmsvc.ChatResult, error) {
	return &llmsvc.ChatResult{Content: "answer", Model: req.Model}, nil
}

func (f *rerankFailureClient) Rerank(_ context.Context, _ llmsvc.RerankRequest) (*llmsvc.RerankResult, error) {
	return nil, errors.New("rerank unavailable")
}

func (f *semanticFakeClient) Chat(_ context.Context, req llmsvc.ChatRequest) (*llmsvc.ChatResult, error) {
	return &llmsvc.ChatResult{Content: "answer", Model: req.Model}, nil
}

func (f *semanticFakeClient) Embed(_ context.Context, req llmsvc.EmbeddingRequest) (*llmsvc.EmbeddingResult, error) {
	f.embedCalls++
	embeddings := make([][]float64, 0, len(req.Input))
	for _, input := range req.Input {
		if strings.Contains(input, "连接池") || strings.Contains(input, "数据库") {
			embeddings = append(embeddings, []float64{1, 0})
			continue
		}
		embeddings = append(embeddings, []float64{0, 1})
	}
	return &llmsvc.EmbeddingResult{Model: req.Model, Embeddings: embeddings}, nil
}

func (f *semanticFakeClient) Rerank(_ context.Context, req llmsvc.RerankRequest) (*llmsvc.RerankResult, error) {
	f.rerankCalls++
	results := make([]llmsvc.RerankItem, 0, len(req.Documents))
	for index, document := range req.Documents {
		score := 0.1
		if strings.Contains(document, "连接池") {
			score = 0.9
		}
		results = append(results, llmsvc.RerankItem{Index: index, RelevanceScore: score})
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].RelevanceScore > results[j].RelevanceScore
	})
	return &llmsvc.RerankResult{Model: req.Model, Results: results}, nil
}
