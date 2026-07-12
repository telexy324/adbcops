CREATE TABLE IF NOT EXISTS incident (
    id BIGSERIAL PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    severity VARCHAR(30) NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'open',
    environment VARCHAR(80),
    system_name VARCHAR(120),
    component_name VARCHAR(120),
    summary TEXT,
    analysis_task_id BIGINT,
    created_by BIGINT,
    resolved_at TIMESTAMP,
    closed_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT now(),
    updated_at TIMESTAMP DEFAULT now()
);

CREATE TABLE IF NOT EXISTS incident_event (
    id BIGSERIAL PRIMARY KEY,
    incident_id BIGINT NOT NULL,
    event_id BIGINT NOT NULL,
    created_at TIMESTAMP DEFAULT now(),
    UNIQUE (incident_id, event_id)
);

CREATE TABLE IF NOT EXISTS incident_evidence (
    id BIGSERIAL PRIMARY KEY,
    incident_id BIGINT NOT NULL,
    evidence_key VARCHAR(100) NOT NULL,
    created_at TIMESTAMP DEFAULT now(),
    UNIQUE (incident_id, evidence_key)
);

CREATE TABLE IF NOT EXISTS incident_root_cause_candidate (
    id BIGSERIAL PRIMARY KEY,
    incident_id BIGINT NOT NULL,
    summary TEXT NOT NULL,
    score NUMERIC(6,4) NOT NULL DEFAULT 0,
    details JSONB,
    confirmed BOOLEAN NOT NULL DEFAULT false,
    confirmed_by BIGINT,
    confirmed_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT now(),
    updated_at TIMESTAMP DEFAULT now()
);

CREATE TABLE IF NOT EXISTS incident_activity (
    id BIGSERIAL PRIMARY KEY,
    incident_id BIGINT NOT NULL,
    actor_id BIGINT,
    action VARCHAR(80) NOT NULL,
    detail JSONB,
    created_at TIMESTAMP DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_incident_status ON incident(status);
CREATE INDEX IF NOT EXISTS idx_incident_scope ON incident(environment, system_name, component_name);
CREATE INDEX IF NOT EXISTS idx_incident_analysis_task ON incident(analysis_task_id);
CREATE INDEX IF NOT EXISTS idx_incident_event_event ON incident_event(event_id);
CREATE INDEX IF NOT EXISTS idx_incident_evidence_key ON incident_evidence(evidence_key);
CREATE INDEX IF NOT EXISTS idx_incident_root_cause_incident ON incident_root_cause_candidate(incident_id);
CREATE INDEX IF NOT EXISTS idx_incident_activity_incident_created ON incident_activity(incident_id, created_at DESC);
