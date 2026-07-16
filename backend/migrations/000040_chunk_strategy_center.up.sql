CREATE TABLE kb_chunk_strategy (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(120) NOT NULL,
    version VARCHAR(50) NOT NULL,
    applicable_doc_types JSONB,
    config JSONB NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_by BIGINT REFERENCES app_user(id),
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now(),
    UNIQUE(name, version)
);

INSERT INTO kb_chunk_strategy (name, version, applicable_doc_types, config, enabled)
VALUES (
    'semantic-ops',
    '1.0',
    '["runbook", "alert_handbook", "incident_report", "change_plan", "rollback_plan"]'::jsonb,
    '{"mode":"semantic_ops","maxChildChars":1200,"tableRowsPerChunk":20,"contextBlocks":3,"parentChild":true}'::jsonb,
    true
);

ALTER TABLE kb_chunk
DROP CONSTRAINT IF EXISTS kb_chunk_document_id_chunk_index_key;

ALTER TABLE kb_chunk
ADD COLUMN document_version_id BIGINT REFERENCES kb_document_version(id) ON DELETE CASCADE,
ADD COLUMN strategy_id BIGINT REFERENCES kb_chunk_strategy(id),
ADD COLUMN parent_chunk_id BIGINT REFERENCES kb_chunk(id) ON DELETE CASCADE,
ADD COLUMN chunk_type VARCHAR(50) NOT NULL DEFAULT 'fixed_window',
ADD COLUMN section_path JSONB,
ADD COLUMN source_block_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
ADD COLUMN source_page_start INT,
ADD COLUMN source_page_end INT,
ADD COLUMN context_before TEXT,
ADD COLUMN context_after TEXT,
ADD COLUMN content_hash VARCHAR(128),
ADD COLUMN sibling_group VARCHAR(128),
ADD COLUMN semantic_unit VARCHAR(80);

UPDATE kb_chunk AS chunk
SET document_version_id = (
    SELECT id
    FROM kb_document_version
    WHERE document_id = chunk.document_id
    ORDER BY created_at DESC, id DESC
    LIMIT 1
);

UPDATE kb_chunk
SET strategy_id = (SELECT id FROM kb_chunk_strategy WHERE name = 'semantic-ops' AND version = '1.0'),
    content_hash = md5(content),
    semantic_unit = 'legacy_fixed_window';

ALTER TABLE kb_chunk
ALTER COLUMN document_version_id SET NOT NULL,
ALTER COLUMN strategy_id SET NOT NULL,
ALTER COLUMN content_hash SET NOT NULL;

CREATE UNIQUE INDEX uq_kb_chunk_version_strategy_index
ON kb_chunk(document_version_id, strategy_id, chunk_index);

CREATE INDEX idx_kb_chunk_version_strategy
ON kb_chunk(document_version_id, strategy_id, chunk_index);

CREATE INDEX idx_kb_chunk_parent
ON kb_chunk(parent_chunk_id);
