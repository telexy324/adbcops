CREATE TABLE kb_document (
    id BIGSERIAL PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    file_name VARCHAR(255) NOT NULL,
    file_path TEXT NOT NULL,
    file_type VARCHAR(50) NOT NULL,
    system_name VARCHAR(100),
    component_name VARCHAR(100),
    environment VARCHAR(50),
    doc_type VARCHAR(100),
    version VARCHAR(50) DEFAULT 'v1.0',
    status VARCHAR(50) DEFAULT 'draft',
    tags JSONB,
    summary TEXT,
    valid_from TIMESTAMP,
    valid_until TIMESTAMP,
    quality_score INT DEFAULT 0,
    quality_result JSONB,
    created_by BIGINT REFERENCES app_user(id),
    reviewed_by BIGINT REFERENCES app_user(id),
    created_at TIMESTAMP DEFAULT now(),
    updated_at TIMESTAMP DEFAULT now(),
    reviewed_at TIMESTAMP
);

CREATE INDEX idx_kb_document_created_by_created_at
ON kb_document(created_by, created_at DESC);

CREATE INDEX idx_kb_document_status_created_at
ON kb_document(status, created_at DESC);
