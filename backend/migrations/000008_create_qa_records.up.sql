CREATE TABLE qa_record (
    id BIGSERIAL PRIMARY KEY,
    conversation_id BIGINT REFERENCES conversation(id) ON DELETE SET NULL,
    user_id BIGINT NOT NULL REFERENCES app_user(id),
    question TEXT NOT NULL,
    rewritten_query TEXT NOT NULL,
    answer TEXT NOT NULL,
    citations JSONB,
    recall_count INTEGER NOT NULL DEFAULT 0,
    llm_config_id BIGINT REFERENCES llm_config(id) ON DELETE SET NULL,
    created_at TIMESTAMP NOT NULL DEFAULT now()
);

CREATE INDEX idx_qa_record_conversation_id_created_at
ON qa_record(conversation_id, created_at DESC);

CREATE INDEX idx_qa_record_user_id_created_at
ON qa_record(user_id, created_at DESC);
