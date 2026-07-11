CREATE TABLE credential_secret (
    id BIGSERIAL PRIMARY KEY,
    secret_type VARCHAR(50) NOT NULL,
    encrypted_payload TEXT NOT NULL,
    key_version VARCHAR(50),
    created_by BIGINT REFERENCES app_user(id),
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now()
);

CREATE TABLE data_source (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(120) NOT NULL,
    source_type VARCHAR(50) NOT NULL,
    environment VARCHAR(50),
    system_name VARCHAR(100),
    component_name VARCHAR(100),
    config JSONB NOT NULL,
    credential_id BIGINT REFERENCES credential_secret(id),
    enabled BOOLEAN NOT NULL DEFAULT true,
    read_only BOOLEAN NOT NULL DEFAULT true,
    created_by BIGINT REFERENCES app_user(id),
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now(),
    CONSTRAINT chk_data_source_type CHECK (source_type IN ('elasticsearch', 'opensearch', 'prometheus', 'kubernetes', 'ssh', 'http'))
);

CREATE INDEX idx_data_source_enabled_type
ON data_source(enabled, source_type);

CREATE INDEX idx_data_source_scope
ON data_source(environment, system_name, component_name);
