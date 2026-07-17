DROP INDEX IF EXISTS idx_kb_quality_evaluation_one_published;
DROP INDEX IF EXISTS idx_kb_quality_evaluation_fingerprint;

ALTER TABLE kb_quality_evaluation
DROP COLUMN IF EXISTS request_fingerprint,
DROP COLUMN IF EXISTS selected_criteria,
DROP COLUMN IF EXISTS mode;
