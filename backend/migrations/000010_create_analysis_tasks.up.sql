CREATE TABLE analysis_task (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES app_user(id),
    conversation_id BIGINT REFERENCES conversation(id) ON DELETE SET NULL,
    task_type VARCHAR(50) NOT NULL,
    question TEXT NOT NULL,
    scope JSONB,
    data_source_ids JSONB,
    status VARCHAR(30) NOT NULL,
    summary TEXT,
    result JSONB,
    error_message TEXT,
    started_at TIMESTAMP,
    finished_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now(),
    CONSTRAINT chk_analysis_task_status CHECK (status IN ('running', 'success', 'failed'))
);

CREATE INDEX idx_analysis_task_user_id_created_at
ON analysis_task(user_id, created_at DESC);

CREATE INDEX idx_analysis_task_status_created_at
ON analysis_task(status, created_at DESC);
