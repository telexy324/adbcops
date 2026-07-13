DROP INDEX IF EXISTS idx_topology_type_audit_type;
DROP INDEX IF EXISTS idx_topology_relation_type_key_enabled;
DROP INDEX IF EXISTS idx_topology_node_type_key_enabled;

ALTER TABLE topology_edge
DROP COLUMN IF EXISTS relation_type_id;

ALTER TABLE topology_node
DROP COLUMN IF EXISTS node_type_id;

DROP TABLE IF EXISTS topology_type_audit;
DROP TABLE IF EXISTS topology_relation_type;
DROP TABLE IF EXISTS topology_node_type;
