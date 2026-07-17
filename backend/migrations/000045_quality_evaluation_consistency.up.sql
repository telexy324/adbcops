ALTER TABLE kb_quality_evaluation
ADD COLUMN mode VARCHAR(30) NOT NULL DEFAULT 'deterministic'
    CHECK (mode IN ('deterministic', 'hybrid', 'llm')),
ADD COLUMN selected_criteria JSONB NOT NULL DEFAULT '[]'::jsonb,
ADD COLUMN request_fingerprint VARCHAR(64);

UPDATE kb_quality_evaluation
SET mode = CASE
    WHEN source = 'deterministic' THEN 'deterministic'
    WHEN source = 'llm' THEN 'llm'
    ELSE 'hybrid'
END;

WITH ranked AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY document_version_id
               ORDER BY published_at DESC NULLS LAST, id DESC
           ) AS position
    FROM kb_quality_evaluation
    WHERE review_status = 'published'
)
UPDATE kb_quality_evaluation evaluation
SET review_status = 'superseded'
FROM ranked
WHERE evaluation.id = ranked.id
  AND ranked.position > 1;

CREATE INDEX idx_kb_quality_evaluation_fingerprint
ON kb_quality_evaluation(document_version_id, quality_profile_id, request_fingerprint, created_at DESC)
WHERE request_fingerprint IS NOT NULL;

CREATE UNIQUE INDEX idx_kb_quality_evaluation_one_published
ON kb_quality_evaluation(document_version_id)
WHERE review_status = 'published';
