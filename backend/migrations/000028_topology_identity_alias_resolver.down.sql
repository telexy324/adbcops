DROP INDEX IF EXISTS idx_topology_conflict_status;
DROP TABLE IF EXISTS topology_conflict;

DROP INDEX IF EXISTS idx_topology_alias_scope;
DROP INDEX IF EXISTS idx_topology_alias_alias;
DROP TABLE IF EXISTS topology_node_alias;

ALTER TABLE topology_node
DROP COLUMN IF EXISTS resolved_attributes,
DROP COLUMN IF EXISTS locked_fields,
DROP COLUMN IF EXISTS source_priority;
