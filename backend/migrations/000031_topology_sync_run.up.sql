CREATE TABLE IF NOT EXISTS topology_sync_run (
    id BIGSERIAL PRIMARY KEY,
    source_config_id BIGINT NOT NULL REFERENCES topology_source_config(id) ON DELETE CASCADE,
    trigger_type VARCHAR(30) NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'pending',
    discovered_nodes INT NOT NULL DEFAULT 0,
    discovered_edges INT NOT NULL DEFAULT 0,
    created_nodes INT NOT NULL DEFAULT 0,
    updated_nodes INT NOT NULL DEFAULT 0,
    stale_nodes INT NOT NULL DEFAULT 0,
    created_edges INT NOT NULL DEFAULT 0,
    updated_edges INT NOT NULL DEFAULT 0,
    stale_edges INT NOT NULL DEFAULT 0,
    conflict_count INT NOT NULL DEFAULT 0,
    warning_count INT NOT NULL DEFAULT 0,
    error_message TEXT,
    detail JSONB,
    started_at TIMESTAMP,
    finished_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_topology_sync_run_source_created
ON topology_sync_run(source_config_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_topology_sync_run_status
ON topology_sync_run(status);
