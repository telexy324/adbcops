CREATE TABLE IF NOT EXISTS topology_node (
    id BIGSERIAL PRIMARY KEY,
    node_key VARCHAR(255) NOT NULL UNIQUE,
    kind VARCHAR(60) NOT NULL,
    name VARCHAR(255) NOT NULL,
    display_name VARCHAR(255),
    environment VARCHAR(80),
    cluster VARCHAR(120),
    namespace VARCHAR(120),
    labels JSONB,
    properties JSONB,
    source_type VARCHAR(50) NOT NULL,
    source_ref JSONB,
    created_at TIMESTAMP DEFAULT now(),
    updated_at TIMESTAMP DEFAULT now()
);

CREATE TABLE IF NOT EXISTS topology_edge (
    id BIGSERIAL PRIMARY KEY,
    edge_key VARCHAR(255) NOT NULL UNIQUE,
    from_node_key VARCHAR(255) NOT NULL,
    to_node_key VARCHAR(255) NOT NULL,
    edge_type VARCHAR(80) NOT NULL,
    confidence NUMERIC(5,4),
    properties JSONB,
    source_type VARCHAR(50) NOT NULL,
    source_ref JSONB,
    created_at TIMESTAMP DEFAULT now(),
    updated_at TIMESTAMP DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_topology_node_kind ON topology_node(kind);
CREATE INDEX IF NOT EXISTS idx_topology_node_scope ON topology_node(environment, cluster, namespace);
CREATE INDEX IF NOT EXISTS idx_topology_edge_from ON topology_edge(from_node_key);
CREATE INDEX IF NOT EXISTS idx_topology_edge_to ON topology_edge(to_node_key);
CREATE INDEX IF NOT EXISTS idx_topology_edge_type ON topology_edge(edge_type);
