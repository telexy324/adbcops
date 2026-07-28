DROP INDEX IF EXISTS idx_evidence_owner_data_source;
DROP INDEX IF EXISTS idx_evidence_rca_run_round;

ALTER TABLE evidence DROP CONSTRAINT IF EXISTS chk_evidence_window;
ALTER TABLE evidence DROP CONSTRAINT IF EXISTS chk_evidence_kind;
ALTER TABLE evidence
    DROP COLUMN IF EXISTS owner_user_id,
    DROP COLUMN IF EXISTS data_source_id,
    DROP COLUMN IF EXISTS source_skill,
    DROP COLUMN IF EXISTS window_end,
    DROP COLUMN IF EXISTS window_start,
    DROP COLUMN IF EXISTS entity,
    DROP COLUMN IF EXISTS evidence_kind,
    DROP COLUMN IF EXISTS rca_action_id,
    DROP COLUMN IF EXISTS rca_round_id,
    DROP COLUMN IF EXISTS rca_run_id;

DROP TABLE IF EXISTS rca_root_cause_candidate;
DROP TABLE IF EXISTS rca_action;
DROP TABLE IF EXISTS rca_round;
DROP TABLE IF EXISTS rca_run;
