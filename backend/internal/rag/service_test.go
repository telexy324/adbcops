package rag

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

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
	qaRecords          []model.QARecord
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

func (f *fakeRepository) FindDefaultEnabledLLMConfig(_ context.Context) (*model.LLMConfig, error) {
	return nil, repository.ErrNotFound
}

func (f *fakeRepository) CreateQARecord(_ context.Context, record *model.QARecord) error {
	record.ID = f.nextQARecordID
	f.nextQARecordID++
	f.qaRecords = append(f.qaRecords, *record)
	return nil
}
