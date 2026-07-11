CREATE TABLE IF NOT EXISTS skill_run (
    id BIGSERIAL PRIMARY KEY,
    workflow_run_id BIGINT,
    node_run_id BIGINT,
    skill_name VARCHAR(120) NOT NULL,
    tool_name VARCHAR(120),
    input_summary JSONB,
    output_summary JSONB,
    status VARCHAR(30),
    error_message TEXT,
    started_at TIMESTAMP,
    finished_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_skill_run_skill_name ON skill_run(skill_name);
CREATE INDEX IF NOT EXISTS idx_skill_run_status ON skill_run(status);
CREATE INDEX IF NOT EXISTS idx_skill_run_started_at ON skill_run(started_at);
