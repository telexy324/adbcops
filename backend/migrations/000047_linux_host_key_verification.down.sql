DROP INDEX IF EXISTS idx_linux_host_key_review;

ALTER TABLE linux_host
    DROP CONSTRAINT IF EXISTS chk_linux_host_key_candidate,
    DROP CONSTRAINT IF EXISTS chk_linux_host_key_status,
    DROP COLUMN IF EXISTS host_key_confirmed_by,
    DROP COLUMN IF EXISTS host_key_confirmed_at,
    DROP COLUMN IF EXISTS host_key_observed_at,
    DROP COLUMN IF EXISTS pending_host_key_fingerprint,
    DROP COLUMN IF EXISTS pending_host_key_algorithm,
    DROP COLUMN IF EXISTS host_key_status;
