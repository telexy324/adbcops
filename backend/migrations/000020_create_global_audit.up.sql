CREATE TABLE IF NOT EXISTS audit_log (
    id BIGSERIAL PRIMARY KEY,
    request_id VARCHAR(160) NOT NULL,
    user_id BIGINT REFERENCES app_user(id),
    username VARCHAR(120),
    method VARCHAR(12) NOT NULL,
    path VARCHAR(300) NOT NULL,
    route VARCHAR(300),
    action VARCHAR(60) NOT NULL,
    resource VARCHAR(120) NOT NULL,
    status_code INT NOT NULL,
    client_ip VARCHAR(80),
    user_agent VARCHAR(300),
    metadata JSONB,
    error_count INT NOT NULL DEFAULT 0,
    duration_ms BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_audit_log_request_id ON audit_log(request_id);
CREATE INDEX IF NOT EXISTS idx_audit_log_user_id_created_at ON audit_log(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_action_created_at ON audit_log(action, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_resource_created_at ON audit_log(resource, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_created_at ON audit_log(created_at DESC);

ALTER TABLE skill_run ADD COLUMN IF NOT EXISTS request_id VARCHAR(160);
ALTER TABLE agent_run ADD COLUMN IF NOT EXISTS request_id VARCHAR(160);
ALTER TABLE workflow_run ADD COLUMN IF NOT EXISTS request_id VARCHAR(160);

CREATE INDEX IF NOT EXISTS idx_skill_run_request_id ON skill_run(request_id);
CREATE INDEX IF NOT EXISTS idx_agent_run_request_id ON agent_run(request_id);
CREATE INDEX IF NOT EXISTS idx_workflow_run_request_id ON workflow_run(request_id);
