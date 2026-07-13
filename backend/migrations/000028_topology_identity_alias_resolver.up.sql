ALTER TABLE topology_node
ADD COLUMN IF NOT EXISTS source_priority INT NOT NULL DEFAULT 0,
ADD COLUMN IF NOT EXISTS locked_fields JSONB,
ADD COLUMN IF NOT EXISTS resolved_attributes JSONB;

CREATE TABLE IF NOT EXISTS topology_node_alias (
    id BIGSERIAL PRIMARY KEY,
    node_id BIGINT NOT NULL REFERENCES topology_node(id) ON DELETE CASCADE,
    alias VARCHAR(255) NOT NULL,
    alias_type VARCHAR(50),
    environment VARCHAR(50),
    source_type VARCHAR(50),
    confidence NUMERIC(5,4),
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    UNIQUE(node_id, alias)
);

CREATE INDEX IF NOT EXISTS idx_topology_alias_alias
ON topology_node_alias(alias);

CREATE INDEX IF NOT EXISTS idx_topology_alias_scope
ON topology_node_alias(environment, alias);

CREATE TABLE IF NOT EXISTS topology_conflict (
    id BIGSERIAL PRIMARY KEY,
    conflict_type VARCHAR(50) NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'open',
    node_id BIGINT REFERENCES topology_node(id),
    edge_id BIGINT REFERENCES topology_edge(id),
    source_config_id BIGINT REFERENCES topology_source_config(id),
    description TEXT NOT NULL,
    candidates JSONB,
    resolution JSONB,
    resolved_by BIGINT REFERENCES app_user(id),
    resolved_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_topology_conflict_status
ON topology_conflict(status, conflict_type);
