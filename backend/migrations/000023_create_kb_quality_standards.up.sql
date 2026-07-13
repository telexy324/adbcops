CREATE TABLE kb_quality_standard (
    id BIGSERIAL PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    file_name VARCHAR(255) NOT NULL,
    file_path TEXT NOT NULL,
    file_type VARCHAR(50) NOT NULL,
    content TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_by BIGINT REFERENCES app_user(id),
    created_at TIMESTAMP DEFAULT now(),
    updated_at TIMESTAMP DEFAULT now()
);

CREATE INDEX idx_kb_quality_standard_enabled_created_at
ON kb_quality_standard(enabled, created_at DESC);
