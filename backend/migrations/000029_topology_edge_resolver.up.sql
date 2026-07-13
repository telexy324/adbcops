ALTER TABLE topology_edge
ADD COLUMN IF NOT EXISTS status VARCHAR(30) NOT NULL DEFAULT 'active',
ADD COLUMN IF NOT EXISTS source_priority INT NOT NULL DEFAULT 0,
ADD COLUMN IF NOT EXISTS resolved_confidence NUMERIC(5,4),
ADD COLUMN IF NOT EXISTS first_observed_at TIMESTAMP,
ADD COLUMN IF NOT EXISTS last_observed_at TIMESTAMP,
ADD COLUMN IF NOT EXISTS stale_at TIMESTAMP,
ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP;

CREATE TABLE IF NOT EXISTS topology_edge_observation (
    id BIGSERIAL PRIMARY KEY,
    edge_id BIGINT NOT NULL REFERENCES topology_edge(id) ON DELETE CASCADE,
    source_config_id BIGINT REFERENCES topology_source_config(id),
    source_type VARCHAR(50) NOT NULL,
    source_record_key VARCHAR(255),
    source_priority INT NOT NULL DEFAULT 0,
    observed_attributes JSONB,
    confidence NUMERIC(5,4),
    observed_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP,
    raw_ref JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    UNIQUE(edge_id, source_type, source_record_key)
);

CREATE INDEX IF NOT EXISTS idx_topology_edge_status
ON topology_edge(status);

CREATE INDEX IF NOT EXISTS idx_topology_edge_observation_edge
ON topology_edge_observation(edge_id);

CREATE INDEX IF NOT EXISTS idx_topology_edge_observation_expiry
ON topology_edge_observation(expires_at);
