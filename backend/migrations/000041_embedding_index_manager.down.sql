DROP INDEX IF EXISTS idx_kb_embedding_index_scope_status;
DROP INDEX IF EXISTS idx_kb_chunk_embedding_index_status;
DROP INDEX IF EXISTS uq_kb_chunk_embedding_identity;

DELETE FROM kb_chunk_embedding newer
USING kb_chunk_embedding older
WHERE newer.chunk_id = older.chunk_id
  AND newer.model = older.model
  AND newer.id < older.id;

ALTER TABLE kb_chunk_embedding
DROP COLUMN IF EXISTS error_message,
DROP COLUMN IF EXISTS status,
DROP COLUMN IF EXISTS distance_metric,
DROP COLUMN IF EXISTS normalized,
DROP COLUMN IF EXISTS vector_data,
DROP COLUMN IF EXISTS content_hash,
DROP COLUMN IF EXISTS model_revision,
DROP COLUMN IF EXISTS index_id;

ALTER TABLE kb_chunk_embedding
RENAME COLUMN embedding_config_id TO llm_config_id;

ALTER TABLE kb_chunk_embedding
ADD CONSTRAINT kb_chunk_embedding_chunk_id_model_key UNIQUE(chunk_id, model);

DROP TABLE IF EXISTS kb_embedding_index;
