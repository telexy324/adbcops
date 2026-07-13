ALTER TABLE data_source
DROP CONSTRAINT IF EXISTS chk_data_source_type;

ALTER TABLE data_source
ADD CONSTRAINT chk_data_source_type
CHECK (source_type IN (
    'elasticsearch',
    'opensearch',
    'prometheus',
    'kubernetes',
    'ssh',
    'http',
    'nacos',
    'redis',
    'tidb',
    'nginx'
));
