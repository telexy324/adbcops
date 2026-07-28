CREATE TABLE IF NOT EXISTS rca_run (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES app_user(id),
    conversation_id BIGINT REFERENCES conversation(id) ON DELETE SET NULL,
    incident_id BIGINT REFERENCES incident(id) ON DELETE SET NULL,
    workflow_run_id BIGINT REFERENCES workflow_run(id) ON DELETE SET NULL,
    status VARCHAR(30) NOT NULL,
    query TEXT NOT NULL,
    scope JSONB NOT NULL DEFAULT '{}'::jsonb,
    current_round INTEGER NOT NULL DEFAULT 0,
    max_rounds INTEGER NOT NULL DEFAULT 3 CHECK (max_rounds BETWEEN 1 AND 10),
    timeout_at TIMESTAMP,
    cancel_requested_at TIMESTAMP,
    error_code VARCHAR(80),
    error_message TEXT,
    started_at TIMESTAMP,
    finished_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now(),
    CONSTRAINT chk_rca_run_status CHECK (status IN ('pending', 'running', 'partial_success', 'success', 'failed', 'cancelled', 'timed_out'))
);

CREATE INDEX IF NOT EXISTS idx_rca_run_user_created ON rca_run(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_rca_run_status_timeout ON rca_run(status, timeout_at);

CREATE TABLE IF NOT EXISTS rca_round (
    id BIGSERIAL PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES rca_run(id) ON DELETE CASCADE,
    round_number INTEGER NOT NULL CHECK (round_number > 0),
    status VARCHAR(30) NOT NULL,
    input_hypotheses JSONB NOT NULL DEFAULT '[]'::jsonb,
    new_evidence_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    rejected_hypotheses JSONB NOT NULL DEFAULT '[]'::jsonb,
    next_actions JSONB NOT NULL DEFAULT '[]'::jsonb,
    error_code VARCHAR(80),
    started_at TIMESTAMP,
    finished_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now(),
    UNIQUE(run_id, round_number),
    CONSTRAINT chk_rca_round_status CHECK (status IN ('pending', 'running', 'partial_success', 'success', 'failed', 'cancelled', 'timed_out'))
);

CREATE INDEX IF NOT EXISTS idx_rca_round_run_number ON rca_round(run_id, round_number);

CREATE TABLE IF NOT EXISTS rca_action (
    id BIGSERIAL PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES rca_run(id) ON DELETE CASCADE,
    round_id BIGINT NOT NULL REFERENCES rca_round(id) ON DELETE CASCADE,
    action_key VARCHAR(160) NOT NULL,
    skill_name VARCHAR(120) NOT NULL,
    status VARCHAR(30) NOT NULL,
    input JSONB NOT NULL DEFAULT '{}'::jsonb,
    output JSONB,
    evidence_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    error_code VARCHAR(80),
    error_message TEXT,
    sensitive_read BOOLEAN NOT NULL DEFAULT true,
    attempt INTEGER NOT NULL DEFAULT 1 CHECK (attempt > 0),
    started_at TIMESTAMP,
    finished_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now(),
    UNIQUE(run_id, action_key),
    CONSTRAINT chk_rca_action_status CHECK (status IN ('pending', 'running', 'partial_success', 'success', 'failed', 'skipped', 'cancelled', 'timed_out'))
);

CREATE INDEX IF NOT EXISTS idx_rca_action_round ON rca_action(round_id, id);
CREATE INDEX IF NOT EXISTS idx_rca_action_run_status ON rca_action(run_id, status);

CREATE TABLE IF NOT EXISTS rca_root_cause_candidate (
    id BIGSERIAL PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES rca_run(id) ON DELETE CASCADE,
    round_id BIGINT NOT NULL REFERENCES rca_round(id) ON DELETE CASCADE,
    summary TEXT NOT NULL,
    confidence NUMERIC(5,4) NOT NULL CHECK (confidence BETWEEN 0 AND 1),
    evidence_ids JSONB NOT NULL,
    rejected BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now(),
    CONSTRAINT chk_rca_candidate_evidence CHECK (jsonb_typeof(evidence_ids) = 'array' AND jsonb_array_length(evidence_ids) > 0)
);

CREATE INDEX IF NOT EXISTS idx_rca_candidate_run ON rca_root_cause_candidate(run_id, confidence DESC);

ALTER TABLE evidence
    ADD COLUMN IF NOT EXISTS rca_run_id BIGINT REFERENCES rca_run(id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS rca_round_id BIGINT REFERENCES rca_round(id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS rca_action_id BIGINT REFERENCES rca_action(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS evidence_kind VARCHAR(40),
    ADD COLUMN IF NOT EXISTS entity JSONB,
    ADD COLUMN IF NOT EXISTS window_start TIMESTAMP,
    ADD COLUMN IF NOT EXISTS window_end TIMESTAMP,
    ADD COLUMN IF NOT EXISTS source_skill VARCHAR(120),
    ADD COLUMN IF NOT EXISTS data_source_id BIGINT REFERENCES data_source(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS owner_user_id BIGINT REFERENCES app_user(id) ON DELETE SET NULL;

ALTER TABLE evidence DROP CONSTRAINT IF EXISTS chk_evidence_kind;
ALTER TABLE evidence ADD CONSTRAINT chk_evidence_kind
    CHECK (evidence_kind IS NULL OR evidence_kind IN ('fact', 'rule', 'knowledge', 'model_hypothesis'));
ALTER TABLE evidence DROP CONSTRAINT IF EXISTS chk_evidence_window;
ALTER TABLE evidence ADD CONSTRAINT chk_evidence_window
    CHECK (window_start IS NULL OR window_end IS NULL OR window_start <= window_end);

CREATE INDEX IF NOT EXISTS idx_evidence_rca_run_round ON evidence(rca_run_id, rca_round_id, id);
CREATE INDEX IF NOT EXISTS idx_evidence_owner_data_source ON evidence(owner_user_id, data_source_id);
