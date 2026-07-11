CREATE TABLE IF NOT EXISTS workflow_definition (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(120) NOT NULL,
    version VARCHAR(50) NOT NULL,
    description TEXT,
    definition JSONB NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_by BIGINT REFERENCES app_user(id),
    created_at TIMESTAMP DEFAULT now(),
    updated_at TIMESTAMP DEFAULT now(),
    UNIQUE(name, version)
);

CREATE INDEX IF NOT EXISTS idx_workflow_definition_enabled ON workflow_definition(enabled);
CREATE INDEX IF NOT EXISTS idx_workflow_definition_name ON workflow_definition(name);
