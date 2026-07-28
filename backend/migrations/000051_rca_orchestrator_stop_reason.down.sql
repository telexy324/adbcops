DROP INDEX IF EXISTS idx_rca_run_stop_reason;

ALTER TABLE rca_run
    DROP COLUMN IF EXISTS stop_reason;
