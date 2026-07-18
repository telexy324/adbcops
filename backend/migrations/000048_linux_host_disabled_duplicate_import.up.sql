DROP INDEX IF EXISTS uq_linux_host_environment_address;

-- Active hosts retain the environment + host + port invariant. The explicit
-- create_as_disabled import strategy may preserve a conflicting row for later
-- operator review without making it eligible for collection.
CREATE UNIQUE INDEX uq_linux_host_environment_address
ON linux_host(environment, host, port) NULLS NOT DISTINCT
WHERE enabled = true AND deleted_at IS NULL;
