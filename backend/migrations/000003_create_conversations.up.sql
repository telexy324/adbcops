CREATE TABLE conversation (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES app_user(id),
    title VARCHAR(255),
    status VARCHAR(30) NOT NULL DEFAULT 'active',
    conversation_summary TEXT,
    context_snapshot JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now(),
    CONSTRAINT chk_conversation_status CHECK (status IN ('active', 'deleted'))
);

CREATE TABLE conversation_message (
    id BIGSERIAL PRIMARY KEY,
    conversation_id BIGINT NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
    role VARCHAR(30) NOT NULL,
    content TEXT NOT NULL,
    citations JSONB,
    metadata JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    CONSTRAINT chk_conversation_message_role CHECK (role IN ('user', 'assistant', 'system', 'tool'))
);

CREATE INDEX idx_conversation_user_id_updated_at
ON conversation(user_id, updated_at DESC);

CREATE INDEX idx_conversation_message_conversation_id_created_at
ON conversation_message(conversation_id, created_at DESC);
