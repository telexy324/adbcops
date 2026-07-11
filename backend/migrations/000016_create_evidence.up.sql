CREATE TABLE IF NOT EXISTS evidence (
    id BIGSERIAL PRIMARY KEY,
    evidence_key VARCHAR(100) NOT NULL UNIQUE,
    source_type VARCHAR(50) NOT NULL,
    source_ref JSONB,
    observed_at TIMESTAMP,
    title VARCHAR(255),
    summary TEXT NOT NULL,
    content JSONB,
    confidence NUMERIC(5,4),
    sensitivity VARCHAR(30) DEFAULT 'internal',
    created_at TIMESTAMP DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_evidence_source_type ON evidence(source_type);
CREATE INDEX IF NOT EXISTS idx_evidence_observed_at ON evidence(observed_at);
CREATE INDEX IF NOT EXISTS idx_evidence_sensitivity ON evidence(sensitivity);
