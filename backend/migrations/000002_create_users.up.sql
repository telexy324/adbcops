CREATE TABLE app_user (
    id BIGSERIAL PRIMARY KEY,
    username VARCHAR(100) NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    display_name VARCHAR(120),
    role VARCHAR(30) NOT NULL DEFAULT 'user',
    enabled BOOLEAN NOT NULL DEFAULT true,
    password_changed_at TIMESTAMP,
    last_login_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now(),
    CONSTRAINT chk_app_user_role CHECK (role IN ('admin', 'user'))
);

CREATE TABLE login_audit (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT REFERENCES app_user(id),
    username VARCHAR(100),
    success BOOLEAN NOT NULL,
    client_ip VARCHAR(100),
    user_agent TEXT,
    failure_reason TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT now()
);

CREATE INDEX idx_login_audit_username_created_at
ON login_audit(username, created_at DESC);

CREATE INDEX idx_login_audit_user_id_created_at
ON login_audit(user_id, created_at DESC);
