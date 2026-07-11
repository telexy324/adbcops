package conversation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

func TestUserCanOnlyAccessOwnConversation(t *testing.T) {
	store := newFakeConversationRepository()
	service := NewService(store)
	owner := &model.AppUser{ID: 1, Role: model.RoleUser}
	other := &model.AppUser{ID: 2, Role: model.RoleUser}
	conversation := store.addConversation(owner.ID, "owned")

	if _, err := service.Get(context.Background(), other, conversation.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Get() error = %v, want ErrForbidden", err)
	}
	if _, err := service.Get(context.Background(), owner, conversation.ID); err != nil {
		t.Fatalf("owner Get() error = %v", err)
	}
}

func TestAdminCanAuditButListDefaultsToOwnConversations(t *testing.T) {
	store := newFakeConversationRepository()
	service := NewService(store)
	admin := &model.AppUser{ID: 10, Role: model.RoleAdmin}
	user := &model.AppUser{ID: 20, Role: model.RoleUser}
	adminConversation := store.addConversation(admin.ID, "admin")
	userConversation := store.addConversation(user.ID, "user")

	defaultList, err := service.List(context.Background(), admin, nil)
	if err != nil {
		t.Fatalf("default List() error = %v", err)
	}
	if len(defaultList) != 1 || defaultList[0].ID != adminConversation.ID {
		t.Fatalf("default admin list = %+v, want only own conversation", defaultList)
	}

	requestedUserID := user.ID
	auditList, err := service.List(context.Background(), admin, &requestedUserID)
	if err != nil {
		t.Fatalf("audit List() error = %v", err)
	}
	if len(auditList) != 1 || auditList[0].ID != userConversation.ID {
		t.Fatalf("audit list = %+v, want user's conversation", auditList)
	}
}

func TestRecentMessagesKeepsLastEightRounds(t *testing.T) {
	store := newFakeConversationRepository()
	service := NewService(store)
	owner := &model.AppUser{ID: 1, Role: model.RoleUser}
	conversation := store.addConversation(owner.ID, "recent")
	for i := 1; i <= 20; i++ {
		store.addMessage(conversation.ID, model.MessageRoleUser, fmt.Sprintf("message-%02d", i))
	}

	detail, err := service.Get(context.Background(), owner, conversation.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(detail.RecentMessages) != recentRoundMessageLimit {
		t.Fatalf("recent message count = %d, want %d", len(detail.RecentMessages), recentRoundMessageLimit)
	}
	if detail.RecentMessages[0].Content != "message-05" || detail.RecentMessages[15].Content != "message-20" {
		t.Fatalf("recent messages range = %q..%q", detail.RecentMessages[0].Content, detail.RecentMessages[15].Content)
	}
}

type fakeConversationRepository struct {
	nextConversationID int64
	nextMessageID      int64
	conversations      map[int64]*model.Conversation
	messages           map[int64][]model.Message
}

func newFakeConversationRepository() *fakeConversationRepository {
	return &fakeConversationRepository{
		nextConversationID: 1,
		nextMessageID:      1,
		conversations:      make(map[int64]*model.Conversation),
		messages:           make(map[int64][]model.Message),
	}
}

func (f *fakeConversationRepository) addConversation(userID int64, title string) *model.Conversation {
	conversation := &model.Conversation{UserID: userID, Title: &title, Status: model.ConversationStatusActive}
	_ = f.CreateConversation(context.Background(), conversation)
	return conversation
}

func (f *fakeConversationRepository) addMessage(conversationID int64, role, content string) model.Message {
	message := &model.Message{ConversationID: conversationID, Role: role, Content: content}
	_ = f.CreateMessage(context.Background(), message)
	return *message
}

func (f *fakeConversationRepository) CreateConversation(_ context.Context, conversation *model.Conversation) error {
	conversation.ID = f.nextConversationID
	f.nextConversationID++
	conversation.CreatedAt = time.Now().UTC()
	conversation.UpdatedAt = conversation.CreatedAt
	f.conversations[conversation.ID] = conversation
	return nil
}

func (f *fakeConversationRepository) ListConversations(_ context.Context, userID int64) ([]model.Conversation, error) {
	var conversations []model.Conversation
	for _, conversation := range f.conversations {
		if conversation.UserID == userID && conversation.Status != model.ConversationStatusDeleted {
			conversations = append(conversations, *conversation)
		}
	}
	return conversations, nil
}

func (f *fakeConversationRepository) FindConversationByID(_ context.Context, id int64) (*model.Conversation, error) {
	conversation, ok := f.conversations[id]
	if !ok || conversation.Status == model.ConversationStatusDeleted {
		return nil, repository.ErrNotFound
	}
	return conversation, nil
}

func (f *fakeConversationRepository) SoftDeleteConversation(_ context.Context, id int64) error {
	conversation, ok := f.conversations[id]
	if !ok || conversation.Status == model.ConversationStatusDeleted {
		return repository.ErrNotFound
	}
	conversation.Status = model.ConversationStatusDeleted
	return nil
}

func (f *fakeConversationRepository) CreateMessage(_ context.Context, message *model.Message) error {
	if _, ok := f.conversations[message.ConversationID]; !ok {
		return repository.ErrNotFound
	}
	message.ID = f.nextMessageID
	f.nextMessageID++
	message.CreatedAt = time.Now().UTC()
	f.messages[message.ConversationID] = append(f.messages[message.ConversationID], *message)
	return nil
}

func (f *fakeConversationRepository) ListRecentMessages(_ context.Context, conversationID int64, limit int) ([]model.Message, error) {
	all := f.messages[conversationID]
	if len(all) <= limit {
		return append([]model.Message(nil), all...), nil
	}
	return append([]model.Message(nil), all[len(all)-limit:]...), nil
}
