CREATE TABLE IF NOT EXISTS agent_run (
    id BIGSERIAL PRIMARY KEY,
    workflow_run_id BIGINT,
    agent_name VARCHAR(120) NOT NULL,
    input_summary TEXT,
    output JSONB,
    model_name VARCHAR(120),
    token_usage JSONB,
    status VARCHAR(30),
    error_message TEXT,
    started_at TIMESTAMP,
    finished_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_agent_run_agent_name ON agent_run(agent_name);
CREATE INDEX IF NOT EXISTS idx_agent_run_status ON agent_run(status);
CREATE INDEX IF NOT EXISTS idx_agent_run_started_at ON agent_run(started_at);
