DROP TABLE IF EXISTS kb_quality_evaluation_override;

DROP INDEX IF EXISTS idx_kb_quality_evaluation_supersedes;

ALTER TABLE kb_quality_evaluation
DROP COLUMN IF EXISTS supersedes_evaluation_id,
DROP COLUMN IF EXISTS published_at,
DROP COLUMN IF EXISTS published_by,
DROP COLUMN IF EXISTS review_status;
