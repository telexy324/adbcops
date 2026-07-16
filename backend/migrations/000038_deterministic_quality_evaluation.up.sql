CREATE TABLE kb_quality_evaluation (
    id BIGSERIAL PRIMARY KEY,
    document_version_id BIGINT NOT NULL REFERENCES kb_document_version(id) ON DELETE CASCADE,
    quality_profile_id BIGINT NOT NULL REFERENCES kb_quality_profile(id),
    quality_profile_version VARCHAR(50) NOT NULL,
    parse_score NUMERIC(8,2),
    content_score NUMERIC(8,2),
    retrieval_score NUMERIC(8,2),
    total_score NUMERIC(8,2),
    gate_status VARCHAR(30) NOT NULL CHECK (gate_status IN ('pass', 'warning', 'blocked')),
    level VARCHAR(30),
    source VARCHAR(50) NOT NULL CHECK (source IN ('deterministic', 'llm', 'hybrid', 'manual')),
    model_config_id BIGINT REFERENCES llm_config(id),
    summary TEXT,
    result JSONB,
    status VARCHAR(30) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'completed', 'failed')),
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    completed_at TIMESTAMP
);

CREATE TABLE kb_quality_rule_result (
    id BIGSERIAL PRIMARY KEY,
    evaluation_id BIGINT NOT NULL REFERENCES kb_quality_evaluation(id) ON DELETE CASCADE,
    criterion_key VARCHAR(120) NOT NULL,
    rule_key VARCHAR(160) NOT NULL,
    score NUMERIC(8,2),
    max_score NUMERIC(8,2),
    finding_status VARCHAR(50) CHECK (finding_status IN ('present', 'missing', 'partial', 'conflicting', 'outdated', 'unsafe', 'not_applicable', 'manual_confirmation_required')),
    confidence NUMERIC(5,4),
    evidence JSONB NOT NULL DEFAULT '[]'::jsonb,
    deduction_reason TEXT,
    suggestion TEXT,
    source VARCHAR(50) NOT NULL CHECK (source IN ('deterministic', 'llm', 'manual')),
    manually_overridden BOOLEAN NOT NULL DEFAULT false,
    overridden_by BIGINT REFERENCES app_user(id),
    override_comment TEXT,
    UNIQUE(evaluation_id, criterion_key, rule_key)
);

CREATE INDEX idx_kb_quality_evaluation_document
ON kb_quality_evaluation(document_version_id, created_at DESC);

CREATE INDEX idx_kb_quality_evaluation_profile
ON kb_quality_evaluation(quality_profile_id, created_at DESC);

CREATE INDEX idx_kb_quality_rule_result_evaluation
ON kb_quality_rule_result(evaluation_id, criterion_key, rule_key);
