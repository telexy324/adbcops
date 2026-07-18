ALTER TABLE linux_host
    ADD COLUMN host_key_status VARCHAR(30) NOT NULL DEFAULT 'unverified',
    ADD COLUMN pending_host_key_algorithm VARCHAR(100),
    ADD COLUMN pending_host_key_fingerprint VARCHAR(255),
    ADD COLUMN host_key_observed_at TIMESTAMP,
    ADD COLUMN host_key_confirmed_at TIMESTAMP,
    ADD COLUMN host_key_confirmed_by BIGINT REFERENCES app_user(id),
    ADD CONSTRAINT chk_linux_host_key_status CHECK (
        host_key_status IN ('unverified', 'pending', 'trusted', 'mismatch')
    ),
    ADD CONSTRAINT chk_linux_host_key_candidate CHECK (
        (pending_host_key_algorithm IS NULL AND pending_host_key_fingerprint IS NULL)
        OR
        (pending_host_key_algorithm IS NOT NULL AND pending_host_key_fingerprint IS NOT NULL)
    );

UPDATE linux_host
SET host_key_status = 'trusted'
WHERE host_key_fingerprint IS NOT NULL;

CREATE INDEX idx_linux_host_key_review
ON linux_host(host_key_status, enabled)
WHERE deleted_at IS NULL AND host_key_status IN ('pending', 'mismatch');
