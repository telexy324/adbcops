CREATE TABLE IF NOT EXISTS ops_event (
    id BIGSERIAL PRIMARY KEY,
    event_time TIMESTAMP NOT NULL,
    source_type VARCHAR(50) NOT NULL,
    source_id VARCHAR(255),
    event_type VARCHAR(100) NOT NULL,
    severity VARCHAR(30),
    status VARCHAR(30) NOT NULL DEFAULT 'firing',
    environment VARCHAR(50),
    system_name VARCHAR(100),
    component_name VARCHAR(100),
    cluster VARCHAR(120),
    namespace VARCHAR(120),
    resource_kind VARCHAR(80),
    resource_name VARCHAR(255),
    host VARCHAR(255),
    trace_id VARCHAR(255),
    fingerprint VARCHAR(255),
    summary TEXT NOT NULL,
    payload JSONB,
    occurrence_count INTEGER NOT NULL DEFAULT 1,
    first_seen_at TIMESTAMP NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMP NOT NULL DEFAULT now(),
    resolved_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT now(),
    updated_at TIMESTAMP DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ops_event_time ON ops_event(event_time);
CREATE INDEX IF NOT EXISTS idx_ops_event_fingerprint ON ops_event(fingerprint);
CREATE UNIQUE INDEX IF NOT EXISTS idx_ops_event_fingerprint_unique ON ops_event(fingerprint);
CREATE INDEX IF NOT EXISTS idx_ops_event_resource ON ops_event(environment, system_name, component_name, resource_name);
CREATE INDEX IF NOT EXISTS idx_ops_event_status ON ops_event(status);
