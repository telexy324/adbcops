CREATE TABLE IF NOT EXISTS workflow_run (
    id BIGSERIAL PRIMARY KEY,
    workflow_id BIGINT REFERENCES workflow_definition(id),
    user_id BIGINT REFERENCES app_user(id),
    conversation_id BIGINT REFERENCES conversation(id),
    incident_id BIGINT,
    status VARCHAR(30) NOT NULL DEFAULT 'pending',
    input JSONB,
    output JSONB,
    error_message TEXT,
    started_at TIMESTAMP,
    finished_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT now()
);

CREATE TABLE IF NOT EXISTS workflow_node_run (
    id BIGSERIAL PRIMARY KEY,
    workflow_run_id BIGINT NOT NULL REFERENCES workflow_run(id) ON DELETE CASCADE,
    node_id VARCHAR(120) NOT NULL,
    node_type VARCHAR(50) NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'pending',
    input JSONB,
    output JSONB,
    error_message TEXT,
    attempt INT NOT NULL DEFAULT 0,
    started_at TIMESTAMP,
    finished_at TIMESTAMP,
    UNIQUE(workflow_run_id, node_id)
);

CREATE INDEX IF NOT EXISTS idx_workflow_run_status ON workflow_run(status);
CREATE INDEX IF NOT EXISTS idx_workflow_run_created_at ON workflow_run(created_at);
CREATE INDEX IF NOT EXISTS idx_workflow_node_run_workflow_run_id ON workflow_node_run(workflow_run_id);
