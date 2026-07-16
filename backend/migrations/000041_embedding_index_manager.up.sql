CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE kb_embedding_index (
    id BIGSERIAL PRIMARY KEY,
    document_version_id BIGINT NOT NULL REFERENCES kb_document_version(id) ON DELETE CASCADE,
    strategy_id BIGINT NOT NULL REFERENCES kb_chunk_strategy(id),
    embedding_config_id BIGINT NOT NULL REFERENCES llm_config(id),
    model_name VARCHAR(120) NOT NULL,
    model_revision VARCHAR(120) NOT NULL,
    dimension INT NOT NULL CHECK (dimension > 0),
    normalized BOOLEAN NOT NULL DEFAULT true,
    distance_metric VARCHAR(30) NOT NULL DEFAULT 'cosine' CHECK (distance_metric = 'cosine'),
    status VARCHAR(30) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'building', 'ready', 'stale', 'failed')),
    chunk_count INT NOT NULL DEFAULT 0,
    embedded_count INT NOT NULL DEFAULT 0,
    content_fingerprint VARCHAR(128) NOT NULL,
    error_message TEXT,
    hnsw_enabled BOOLEAN NOT NULL DEFAULT false,
    hnsw_m INT NOT NULL DEFAULT 16 CHECK (hnsw_m BETWEEN 2 AND 100),
    hnsw_ef_construction INT NOT NULL DEFAULT 64 CHECK (hnsw_ef_construction BETWEEN 4 AND 1000),
    created_by BIGINT REFERENCES app_user(id),
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now(),
    completed_at TIMESTAMP,
    UNIQUE(document_version_id, strategy_id, embedding_config_id, model_revision)
);

ALTER TABLE kb_chunk_embedding
DROP CONSTRAINT IF EXISTS kb_chunk_embedding_chunk_id_model_key;

ALTER TABLE kb_chunk_embedding
RENAME COLUMN llm_config_id TO embedding_config_id;

ALTER TABLE kb_chunk_embedding
ADD COLUMN index_id BIGINT REFERENCES kb_embedding_index(id) ON DELETE CASCADE,
ADD COLUMN model_revision VARCHAR(120) NOT NULL DEFAULT 'legacy',
ADD COLUMN content_hash VARCHAR(128),
ADD COLUMN vector_data vector,
ADD COLUMN normalized BOOLEAN NOT NULL DEFAULT false,
ADD COLUMN distance_metric VARCHAR(30) NOT NULL DEFAULT 'cosine' CHECK (distance_metric = 'cosine'),
ADD COLUMN status VARCHAR(30) NOT NULL DEFAULT 'ready' CHECK (status IN ('pending', 'building', 'ready', 'stale', 'failed')),
ADD COLUMN error_message TEXT;

UPDATE kb_chunk_embedding AS embedding_row
SET content_hash = chunk.content_hash,
    vector_data = embedding_row.embedding::text::vector
FROM kb_chunk AS chunk
WHERE chunk.id = embedding_row.chunk_id;

ALTER TABLE kb_chunk_embedding
ALTER COLUMN content_hash SET NOT NULL;

CREATE UNIQUE INDEX uq_kb_chunk_embedding_identity
ON kb_chunk_embedding(chunk_id, embedding_config_id, model_revision, content_hash);

CREATE INDEX idx_kb_chunk_embedding_index_status
ON kb_chunk_embedding(index_id, status);

CREATE INDEX idx_kb_embedding_index_scope_status
ON kb_embedding_index(document_version_id, strategy_id, status);
