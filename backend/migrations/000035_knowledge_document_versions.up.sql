CREATE TABLE kb_document_version (
    id BIGSERIAL PRIMARY KEY,
    document_id BIGINT NOT NULL REFERENCES kb_document(id) ON DELETE CASCADE,
    version VARCHAR(50) NOT NULL,
    revision_no INT NOT NULL DEFAULT 1,
    file_name VARCHAR(255) NOT NULL,
    file_path TEXT NOT NULL,
    file_type VARCHAR(50) NOT NULL,
    file_hash VARCHAR(128) NOT NULL,
    parser_name VARCHAR(100),
    parser_version VARCHAR(50),
    language VARCHAR(30),
    status VARCHAR(30) NOT NULL DEFAULT 'draft',
    metadata JSONB,
    document_schema JSONB,
    parse_quality JSONB,
    content_summary TEXT,
    valid_from TIMESTAMP,
    valid_until TIMESTAMP,
    review_due_at TIMESTAMP,
    created_by BIGINT REFERENCES app_user(id),
    reviewed_by BIGINT REFERENCES app_user(id),
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now(),
    reviewed_at TIMESTAMP,
    UNIQUE(document_id, version, revision_no)
);

CREATE INDEX idx_kb_document_version_document_created
ON kb_document_version(document_id, created_at DESC, id DESC);

CREATE INDEX idx_kb_document_version_status
ON kb_document_version(status, updated_at DESC);

INSERT INTO kb_document_version (
    document_id,
    version,
    revision_no,
    file_name,
    file_path,
    file_type,
    file_hash,
    status,
    valid_from,
    valid_until,
    created_by,
    reviewed_by,
    created_at,
    updated_at,
    reviewed_at
)
SELECT
    id,
    COALESCE(NULLIF(version, ''), 'v1.0'),
    1,
    file_name,
    file_path,
    file_type,
    'legacy-unverified:' || id::text,
    CASE WHEN status = 'rejected' THEN 'failed' ELSE 'draft' END,
    valid_from,
    valid_until,
    created_by,
    reviewed_by,
    COALESCE(created_at, now()),
    COALESCE(updated_at, now()),
    reviewed_at
FROM kb_document;

CREATE TABLE kb_document_block (
    id BIGSERIAL PRIMARY KEY,
    document_version_id BIGINT NOT NULL REFERENCES kb_document_version(id) ON DELETE CASCADE,
    block_key VARCHAR(100) NOT NULL,
    parent_block_id BIGINT REFERENCES kb_document_block(id) ON DELETE CASCADE,
    block_type VARCHAR(50) NOT NULL,
    level INT,
    order_no INT NOT NULL,
    page_no INT,
    section_path JSONB,
    text_content TEXT,
    attributes JSONB,
    content_hash VARCHAR(128),
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    UNIQUE(document_version_id, block_key),
    UNIQUE(document_version_id, order_no)
);

CREATE INDEX idx_kb_document_block_version_order
ON kb_document_block(document_version_id, order_no ASC);

CREATE INDEX idx_kb_document_block_parent
ON kb_document_block(parent_block_id);
