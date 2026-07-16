CREATE TABLE kb_quality_standard_import (
    id BIGSERIAL PRIMARY KEY,
    standard_id BIGINT REFERENCES kb_quality_standard(id) ON DELETE SET NULL,
    original_file_name VARCHAR(255) NOT NULL,
    stored_file_path TEXT NOT NULL,
    file_type VARCHAR(20) NOT NULL CHECK (file_type IN ('docx', 'xlsx')),
    file_size BIGINT NOT NULL CHECK (file_size > 0),
    file_hash VARCHAR(64) NOT NULL,
    parser_name VARCHAR(120),
    parser_version VARCHAR(50),
    status VARCHAR(40) NOT NULL DEFAULT 'uploaded'
        CHECK (status IN ('uploaded', 'parsed', 'structured_draft', 'validation_failed', 'awaiting_confirmation')),
    warnings JSONB NOT NULL DEFAULT '[]'::jsonb,
    validation_errors JSONB NOT NULL DEFAULT '[]'::jsonb,
    preview JSONB,
    created_by BIGINT REFERENCES app_user(id),
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now()
);

CREATE INDEX idx_kb_quality_standard_import_status
ON kb_quality_standard_import(status, created_at DESC);

CREATE INDEX idx_kb_quality_standard_import_standard
ON kb_quality_standard_import(standard_id) WHERE standard_id IS NOT NULL;
