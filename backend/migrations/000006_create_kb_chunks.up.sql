CREATE TABLE kb_chunk (
    id BIGSERIAL PRIMARY KEY,
    document_id BIGINT NOT NULL REFERENCES kb_document(id) ON DELETE CASCADE,
    chunk_index INT NOT NULL,
    content TEXT NOT NULL,
    source_title VARCHAR(255),
    source_section VARCHAR(255),
    source_page INT,
    token_count INT DEFAULT 0,
    summary TEXT,
    search_text TEXT,
    keywords JSONB,
    possible_questions JSONB,
    created_at TIMESTAMP DEFAULT now(),
    UNIQUE(document_id, chunk_index)
);

CREATE INDEX idx_kb_chunk_document_id_chunk_index
ON kb_chunk(document_id, chunk_index);

CREATE INDEX idx_kb_chunk_search_text_trgm
ON kb_chunk USING gin (search_text gin_trgm_ops);

CREATE INDEX idx_kb_chunk_content_trgm
ON kb_chunk USING gin (content gin_trgm_ops);

CREATE TABLE kb_chunk_embedding (
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

CREATE INDEX idx_kb_chunk_embedding_model
ON kb_chunk_embedding(model);

CREATE INDEX idx_kb_chunk_embedding_chunk_id
ON kb_chunk_embedding(chunk_id);
