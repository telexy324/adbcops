ALTER TABLE kb_quality_evaluation
ADD COLUMN review_status VARCHAR(30) NOT NULL DEFAULT 'draft'
    CHECK (review_status IN ('draft', 'published', 'superseded')),
ADD COLUMN published_by BIGINT REFERENCES app_user(id),
ADD COLUMN published_at TIMESTAMP,
ADD COLUMN supersedes_evaluation_id BIGINT REFERENCES kb_quality_evaluation(id);

CREATE TABLE kb_quality_evaluation_override (
    id BIGSERIAL PRIMARY KEY,
    evaluation_id BIGINT NOT NULL REFERENCES kb_quality_evaluation(id) ON DELETE CASCADE,
    rule_result_id BIGINT NOT NULL REFERENCES kb_quality_rule_result(id) ON DELETE CASCADE,
    previous_score NUMERIC(8,2),
    overridden_score NUMERIC(8,2),
    previous_status VARCHAR(50),
    overridden_status VARCHAR(50),
    comment TEXT NOT NULL CHECK (length(trim(comment)) > 0),
    created_by BIGINT NOT NULL REFERENCES app_user(id),
    created_at TIMESTAMP NOT NULL DEFAULT now()
);

CREATE INDEX idx_kb_quality_evaluation_supersedes
ON kb_quality_evaluation(supersedes_evaluation_id);

CREATE INDEX idx_kb_quality_override_evaluation
ON kb_quality_evaluation_override(evaluation_id, created_at DESC);
