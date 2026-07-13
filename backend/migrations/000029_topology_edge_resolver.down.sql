DROP INDEX IF EXISTS idx_topology_edge_observation_expiry;
DROP INDEX IF EXISTS idx_topology_edge_observation_edge;
DROP INDEX IF EXISTS idx_topology_edge_status;
DROP TABLE IF EXISTS topology_edge_observation;

ALTER TABLE topology_edge
DROP COLUMN IF EXISTS deleted_at,
DROP COLUMN IF EXISTS stale_at,
DROP COLUMN IF EXISTS last_observed_at,
DROP COLUMN IF EXISTS first_observed_at,
DROP COLUMN IF EXISTS resolved_confidence,
DROP COLUMN IF EXISTS source_priority,
DROP COLUMN IF EXISTS status;
