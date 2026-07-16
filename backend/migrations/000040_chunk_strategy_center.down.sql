DROP INDEX IF EXISTS idx_kb_chunk_parent;
DROP INDEX IF EXISTS idx_kb_chunk_version_strategy;
DROP INDEX IF EXISTS uq_kb_chunk_version_strategy_index;

ALTER TABLE kb_chunk
DROP COLUMN IF EXISTS semantic_unit,
DROP COLUMN IF EXISTS sibling_group,
DROP COLUMN IF EXISTS content_hash,
DROP COLUMN IF EXISTS context_after,
DROP COLUMN IF EXISTS context_before,
DROP COLUMN IF EXISTS source_page_end,
DROP COLUMN IF EXISTS source_page_start,
DROP COLUMN IF EXISTS source_block_ids,
DROP COLUMN IF EXISTS section_path,
DROP COLUMN IF EXISTS chunk_type,
DROP COLUMN IF EXISTS parent_chunk_id,
DROP COLUMN IF EXISTS strategy_id,
DROP COLUMN IF EXISTS document_version_id;

ALTER TABLE kb_chunk
ADD CONSTRAINT kb_chunk_document_id_chunk_index_key UNIQUE(document_id, chunk_index);

DROP TABLE IF EXISTS kb_chunk_strategy;
