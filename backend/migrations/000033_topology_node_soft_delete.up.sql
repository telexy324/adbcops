ALTER TABLE topology_node
ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP;

CREATE INDEX IF NOT EXISTS idx_topology_node_deleted_at
ON topology_node(deleted_at);
