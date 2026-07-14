DROP INDEX IF EXISTS idx_topology_node_deleted_at;

ALTER TABLE topology_node
DROP COLUMN IF EXISTS deleted_at;
