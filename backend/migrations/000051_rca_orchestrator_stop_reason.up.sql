ALTER TABLE rca_run
    ADD COLUMN IF NOT EXISTS stop_reason VARCHAR(80);

CREATE INDEX IF NOT EXISTS idx_rca_run_stop_reason
    ON rca_run(stop_reason)
    WHERE stop_reason IS NOT NULL;
