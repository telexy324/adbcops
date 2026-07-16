package model

import "time"

const (
	ConversationStatusActive  = "active"
	ConversationStatusDeleted = "deleted"

	MessageRoleUser      = "user"
	MessageRoleAssistant = "assistant"
	MessageRoleSystem    = "system"
	MessageRoleTool      = "tool"
)

// Conversation stores user-owned conversation context. JSON fields are raw
// JSONB payloads so later tasks can extend the context without a schema churn.
type Conversation struct {
	ID                  int64     `gorm:"column:id;primaryKey" json:"id"`
	UserID              int64     `gorm:"column:user_id;not null;index" json:"userId"`
	Title               *string   `gorm:"column:title;size:255" json:"title,omitempty"`
	Status              string    `gorm:"column:status;size:30;not null" json:"status"`
	ConversationSummary *string   `gorm:"column:conversation_summary" json:"conversationSummary,omitempty"`
	ContextSnapshot     []byte    `gorm:"column:context_snapshot;type:jsonb" json:"contextSnapshot,omitempty"`
	CreatedAt           time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt           time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
	Messages            []Message `gorm:"foreignKey:ConversationID" json:"messages,omitempty"`
}

func (Conversation) TableName() string {
	return "conversation"
}

type Message struct {
	ID             int64     `gorm:"column:id;primaryKey" json:"id"`
	ConversationID int64     `gorm:"column:conversation_id;not null;index" json:"conversationId"`
	Role           string    `gorm:"column:role;size:30;not null" json:"role"`
	Content        string    `gorm:"column:content;not null" json:"content"`
	Citations      []byte    `gorm:"column:citations;type:jsonb" json:"citations,omitempty"`
	Metadata       []byte    `gorm:"column:metadata;type:jsonb" json:"metadata,omitempty"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
}

func (Message) TableName() string {
	return "conversation_message"
}

type QARecord struct {
	ID             int64     `gorm:"column:id;primaryKey" json:"id"`
	ConversationID *int64    `gorm:"column:conversation_id" json:"conversationId,omitempty"`
	UserID         int64     `gorm:"column:user_id;not null" json:"userId"`
	Question       string    `gorm:"column:question;not null" json:"question"`
	RewrittenQuery string    `gorm:"column:rewritten_query;not null" json:"rewrittenQuery"`
	Answer         string    `gorm:"column:answer;not null" json:"answer"`
	Citations      []byte    `gorm:"column:citations;type:jsonb" json:"citations,omitempty"`
	RetrievalTrace []byte    `gorm:"column:retrieval_trace;type:jsonb" json:"retrievalTrace,omitempty"`
	RecallCount    int       `gorm:"column:recall_count;not null" json:"recallCount"`
	LLMConfigID    *int64    `gorm:"column:llm_config_id" json:"llmConfigId,omitempty"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
}

func (QARecord) TableName() string {
	return "qa_record"
}
