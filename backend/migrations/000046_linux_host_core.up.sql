ALTER TABLE data_source DROP CONSTRAINT IF EXISTS chk_data_source_type;
ALTER TABLE data_source ADD CONSTRAINT chk_data_source_type CHECK (
    source_type IN (
        'elasticsearch', 'opensearch', 'prometheus', 'kubernetes', 'ssh', 'http',
        'nacos', 'redis', 'tidb', 'nginx', 'linux_server', 'linux_server_group'
    )
);

CREATE TABLE credential_group (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(120) NOT NULL UNIQUE,
    credential_type VARCHAR(50) NOT NULL CHECK (credential_type IN ('password', 'private_key')),
    username VARCHAR(255) NOT NULL,
    credential_id BIGINT NOT NULL REFERENCES credential_secret(id),
    scope JSONB,
    enabled BOOLEAN NOT NULL DEFAULT true,
    version INT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by BIGINT REFERENCES app_user(id),
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now(),
    CHECK (
        scope IS NULL OR (
            jsonb_typeof(scope) = 'object'
            AND scope - ARRAY['environments', 'systemNames'] = '{}'::jsonb
            AND (NOT scope ? 'environments' OR jsonb_typeof(scope -> 'environments') = 'array')
            AND (NOT scope ? 'systemNames' OR jsonb_typeof(scope -> 'systemNames') = 'array')
        )
    )
);

CREATE INDEX idx_credential_group_enabled ON credential_group(enabled, name);

CREATE TABLE linux_host_profile (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(120) NOT NULL UNIQUE,
    description TEXT,
    collectors JSONB NOT NULL,
    critical_services JSONB,
    expected_listening_ports JSONB,
    filesystem_rules JSONB,
    process_rules JSONB,
    custom_thresholds JSONB,
    built_in BOOLEAN NOT NULL DEFAULT false,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_by BIGINT REFERENCES app_user(id),
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now(),
    CHECK (jsonb_typeof(collectors) = 'array'),
    CHECK (critical_services IS NULL OR jsonb_typeof(critical_services) = 'array'),
    CHECK (expected_listening_ports IS NULL OR jsonb_typeof(expected_listening_ports) = 'array'),
    CHECK (filesystem_rules IS NULL OR jsonb_typeof(filesystem_rules) = 'object'),
    CHECK (process_rules IS NULL OR jsonb_typeof(process_rules) = 'object'),
    CHECK (custom_thresholds IS NULL OR jsonb_typeof(custom_thresholds) = 'object')
);

CREATE TABLE linux_host (
    id BIGSERIAL PRIMARY KEY,
    data_source_id BIGINT REFERENCES data_source(id),
    name VARCHAR(120) NOT NULL,
    host VARCHAR(255) NOT NULL,
    port INT NOT NULL DEFAULT 22 CHECK (port BETWEEN 1 AND 65535),
    environment VARCHAR(50),
    system_name VARCHAR(100),
    component_name VARCHAR(100),
    username VARCHAR(255),
    auth_type VARCHAR(50) NOT NULL CHECK (auth_type IN ('password', 'private_key')),
    credential_id BIGINT REFERENCES credential_secret(id),
    credential_group_id BIGINT REFERENCES credential_group(id),
    host_key_policy VARCHAR(50) NOT NULL DEFAULT 'strict'
        CHECK (host_key_policy IN ('strict', 'trust_on_first_use', 'insecure_skip_verify')),
    host_key_algorithm VARCHAR(100),
    host_key_fingerprint VARCHAR(255),
    profile_id BIGINT REFERENCES linux_host_profile(id),
    tags JSONB,
    attributes JSONB,
    enabled BOOLEAN NOT NULL DEFAULT true,
    connection_status VARCHAR(30) NOT NULL DEFAULT 'unknown',
    last_test_at TIMESTAMP,
    last_success_at TIMESTAMP,
    last_error_code VARCHAR(80),
    last_error_message TEXT,
    machine_identity_hash VARCHAR(255),
    detected_platform JSONB,
    created_by BIGINT REFERENCES app_user(id),
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now(),
    deleted_at TIMESTAMP,
    CONSTRAINT chk_linux_host_credential_source CHECK (
        credential_id IS NULL OR credential_group_id IS NULL
    )
);

CREATE UNIQUE INDEX uq_linux_host_environment_address
ON linux_host(environment, host, port) NULLS NOT DISTINCT;

CREATE INDEX idx_linux_host_active_scope
ON linux_host(enabled, environment, system_name, component_name)
WHERE deleted_at IS NULL;

CREATE INDEX idx_linux_host_credential_group
ON linux_host(credential_group_id)
WHERE credential_group_id IS NOT NULL AND deleted_at IS NULL;

CREATE TABLE linux_host_group (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(120) NOT NULL,
    description TEXT,
    environment VARCHAR(50),
    system_name VARCHAR(100),
    tags JSONB,
    created_by BIGINT REFERENCES app_user(id),
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_linux_host_group_name_environment
ON linux_host_group(name, environment) NULLS NOT DISTINCT;

CREATE TABLE linux_host_group_member (
    group_id BIGINT NOT NULL REFERENCES linux_host_group(id) ON DELETE CASCADE,
    host_id BIGINT NOT NULL REFERENCES linux_host(id) ON DELETE CASCADE,
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    PRIMARY KEY(group_id, host_id)
);

INSERT INTO linux_host_profile (
    name, description, collectors, critical_services, expected_listening_ports,
    filesystem_rules, process_rules, custom_thresholds, built_in, enabled
)
VALUES
    ('generic_linux', 'Generic Linux host baseline', '["system_overview","cpu","memory","filesystem","network","process","systemd","time_sync","kernel_log"]'::jsonb, '[]'::jsonb, '[]'::jsonb, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, true, true),
    ('java_application', 'Java application server baseline', '["system_overview","cpu","memory","filesystem","network","process","systemd","time_sync","kernel_log"]'::jsonb, '[]'::jsonb, '[]'::jsonb, '{}'::jsonb, '{"processNames":["java"]}'::jsonb, '{}'::jsonb, true, true),
    ('nginx_server', 'Nginx server baseline', '["system_overview","cpu","memory","filesystem","network","process","systemd","time_sync","kernel_log"]'::jsonb, '["nginx"]'::jsonb, '[80,443]'::jsonb, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, true, true),
    ('redis_server', 'Redis server baseline', '["system_overview","cpu","memory","filesystem","disk_io","network","process","systemd","time_sync","kernel_log"]'::jsonb, '["redis","redis-server"]'::jsonb, '[6379]'::jsonb, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, true, true),
    ('tidb_server', 'TiDB server baseline', '["system_overview","cpu","memory","filesystem","disk_io","network","process","systemd","time_sync","kernel_log"]'::jsonb, '[]'::jsonb, '[4000,10080]'::jsonb, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, true, true),
    ('nacos_server', 'Nacos server baseline', '["system_overview","cpu","memory","filesystem","network","process","systemd","time_sync","kernel_log"]'::jsonb, '[]'::jsonb, '[8848,9848,9849]'::jsonb, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, true, true),
    ('kubernetes_node', 'Kubernetes node baseline', '["system_overview","cpu","memory","filesystem","disk_io","network","process","systemd","time_sync","kernel_log"]'::jsonb, '["kubelet","containerd"]'::jsonb, '[10250]'::jsonb, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, true, true),
    ('database_server', 'Generic database server baseline', '["system_overview","cpu","memory","filesystem","disk_io","network","process","systemd","time_sync","kernel_log"]'::jsonb, '[]'::jsonb, '[]'::jsonb, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, true, true)
ON CONFLICT (name) DO NOTHING;
