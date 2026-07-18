DROP INDEX IF EXISTS uq_linux_host_environment_address;

-- This rollback requires operators to resolve any duplicate disabled rows
-- created after the up migration before applying it.
CREATE UNIQUE INDEX uq_linux_host_environment_address
ON linux_host(environment, host, port) NULLS NOT DISTINCT;
