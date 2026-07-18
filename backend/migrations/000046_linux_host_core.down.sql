DROP TABLE IF EXISTS linux_host_group_member;
DROP TABLE IF EXISTS linux_host_group;
DROP TABLE IF EXISTS linux_host;
DROP TABLE IF EXISTS linux_host_profile;
DROP TABLE IF EXISTS credential_group;

ALTER TABLE data_source DROP CONSTRAINT IF EXISTS chk_data_source_type;
ALTER TABLE data_source ADD CONSTRAINT chk_data_source_type CHECK (
    source_type IN (
        'elasticsearch', 'opensearch', 'prometheus', 'kubernetes', 'ssh', 'http',
        'nacos', 'redis', 'tidb', 'nginx'
    )
);
