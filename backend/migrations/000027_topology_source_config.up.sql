CREATE TABLE IF NOT EXISTS topology_source_config (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(120) NOT NULL UNIQUE,
    source_type VARCHAR(50) NOT NULL,
    data_source_id BIGINT REFERENCES data_source(id),
    enabled BOOLEAN NOT NULL DEFAULT true,
    priority INT NOT NULL DEFAULT 50,
    schedule VARCHAR(120),
    scope JSONB,
    mapping_rules JSONB,
    stale_after_seconds INT NOT NULL DEFAULT 900,
    delete_after_seconds INT NOT NULL DEFAULT 604800,
    last_sync_at TIMESTAMP,
    next_sync_at TIMESTAMP,
    created_by BIGINT REFERENCES app_user(id),
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now(),
    CHECK (source_type IN (
        'manual',
        'kubernetes',
        'trace_service_graph',
        'cmdb',
        'edge_agent',
        'nacos',
        'redis',
        'tidb',
        'nginx',
        'generic_http'
    )),
    CHECK (priority >= 0 AND priority <= 100),
    CHECK (stale_after_seconds > 0),
    CHECK (delete_after_seconds >= stale_after_seconds)
);

CREATE INDEX IF NOT EXISTS idx_topology_source_config_type_enabled
ON topology_source_config(source_type, enabled);

CREATE INDEX IF NOT EXISTS idx_topology_source_config_data_source
ON topology_source_config(data_source_id);
