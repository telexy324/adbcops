CREATE TABLE IF NOT EXISTS kb_chunk_embedding (
    id BIGSERIAL PRIMARY KEY,
    chunk_id BIGINT NOT NULL REFERENCES kb_chunk(id) ON DELETE CASCADE,
    llm_config_id BIGINT REFERENCES llm_config(id) ON DELETE SET NULL,
    model VARCHAR(255) NOT NULL,
    dimension INT NOT NULL,
    embedding JSONB NOT NULL,
    created_at TIMESTAMP DEFAULT now(),
    updated_at TIMESTAMP DEFAULT now(),
    UNIQUE(chunk_id, model),
    CONSTRAINT chk_kb_chunk_embedding_dimension CHECK (dimension > 0)
);

CREATE INDEX IF NOT EXISTS idx_kb_chunk_embedding_model
ON kb_chunk_embedding(model);

CREATE INDEX IF NOT EXISTS idx_kb_chunk_embedding_chunk_id
ON kb_chunk_embedding(chunk_id);
