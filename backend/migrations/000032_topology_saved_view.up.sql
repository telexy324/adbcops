CREATE TABLE IF NOT EXISTS topology_saved_view (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(120) NOT NULL,
    description TEXT,
    owner_id BIGINT NOT NULL REFERENCES app_user(id),
    visibility VARCHAR(30) NOT NULL DEFAULT 'private',
    center_node_id BIGINT REFERENCES topology_node(id),
    query_config JSONB NOT NULL,
    display_config JSONB NOT NULL,
    layout_data JSONB,
    is_default BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now(),
    CHECK (visibility IN ('private', 'team', 'public'))
);

CREATE INDEX IF NOT EXISTS idx_topology_saved_view_owner
ON topology_saved_view(owner_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_topology_saved_view_visibility
ON topology_saved_view(visibility, updated_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_topology_saved_view_default_public
ON topology_saved_view(is_default)
WHERE is_default = true AND visibility = 'public';
