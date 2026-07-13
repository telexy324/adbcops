CREATE TABLE IF NOT EXISTS topology_node_type (
    id BIGSERIAL PRIMARY KEY,
    type_key VARCHAR(80) NOT NULL UNIQUE,
    display_name VARCHAR(120) NOT NULL,
    category VARCHAR(80),
    icon VARCHAR(120),
    default_color VARCHAR(50),
    identity_fields JSONB,
    searchable_fields JSONB,
    label_template TEXT,
    detail_fields JSONB,
    enabled BOOLEAN NOT NULL DEFAULT true,
    built_in BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS topology_relation_type (
    id BIGSERIAL PRIMARY KEY,
    type_key VARCHAR(80) NOT NULL UNIQUE,
    display_name VARCHAR(120) NOT NULL,
    semantics VARCHAR(50) NOT NULL,
    failure_propagation VARCHAR(30) NOT NULL DEFAULT 'none',
    default_direction VARCHAR(30) NOT NULL DEFAULT 'both',
    propagates_failure BOOLEAN NOT NULL DEFAULT false,
    allowed_source_types JSONB,
    allowed_target_types JSONB,
    style JSONB,
    enabled BOOLEAN NOT NULL DEFAULT true,
    built_in BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now(),
    CHECK (semantics IN (
        'hard_dep',
        'runtime_dep',
        'traffic',
        'ownership',
        'containment',
        'configuration',
        'annotation',
        'observation'
    )),
    CHECK (failure_propagation IN (
        'none',
        'src_to_dst',
        'dst_to_src',
        'both'
    ))
);

CREATE TABLE IF NOT EXISTS topology_type_audit (
    id BIGSERIAL PRIMARY KEY,
    type_kind VARCHAR(30) NOT NULL,
    type_id BIGINT NOT NULL,
    action VARCHAR(80) NOT NULL,
    before_value JSONB,
    after_value JSONB,
    actor_id BIGINT REFERENCES app_user(id),
    created_at TIMESTAMP NOT NULL DEFAULT now()
);

ALTER TABLE topology_node
ADD COLUMN IF NOT EXISTS node_type_id BIGINT REFERENCES topology_node_type(id);

ALTER TABLE topology_edge
ADD COLUMN IF NOT EXISTS relation_type_id BIGINT REFERENCES topology_relation_type(id);

CREATE INDEX IF NOT EXISTS idx_topology_node_type_key_enabled
ON topology_node_type(type_key, enabled);

CREATE INDEX IF NOT EXISTS idx_topology_relation_type_key_enabled
ON topology_relation_type(type_key, enabled);

CREATE INDEX IF NOT EXISTS idx_topology_type_audit_type
ON topology_type_audit(type_kind, type_id, created_at);

INSERT INTO topology_node_type (
    type_key,
    display_name,
    category,
    identity_fields,
    searchable_fields,
    label_template,
    detail_fields,
    built_in
)
VALUES
    ('system', 'System', 'business', '["environment","system_name"]'::jsonb, '["name","display_name","system_name"]'::jsonb, '{{name}}', '["environment","system_name"]'::jsonb, true),
    ('application', 'Application', 'business', '["environment","name"]'::jsonb, '["name","display_name"]'::jsonb, '{{name}}', '["environment"]'::jsonb, true),
    ('service', 'Service', 'business', '["environment","name"]'::jsonb, '["name","display_name"]'::jsonb, '{{name}}', '["environment","namespace"]'::jsonb, true),
    ('api', 'API', 'business', '["environment","name"]'::jsonb, '["name","path"]'::jsonb, '{{name}}', '["environment"]'::jsonb, true),
    ('cluster', 'Cluster', 'platform', '["environment","cluster"]'::jsonb, '["name","cluster"]'::jsonb, '{{name}}', '["environment","cluster"]'::jsonb, true),
    ('namespace', 'Namespace', 'platform', '["cluster","namespace"]'::jsonb, '["name","namespace"]'::jsonb, '{{name}}', '["cluster","namespace"]'::jsonb, true),
    ('workload', 'Workload', 'platform', '["cluster","namespace","name"]'::jsonb, '["name","namespace"]'::jsonb, '{{name}}', '["cluster","namespace"]'::jsonb, true),
    ('pod', 'Pod', 'platform', '["cluster","namespace","name"]'::jsonb, '["name","namespace","pod_ip"]'::jsonb, '{{name}}', '["cluster","namespace","pod_ip"]'::jsonb, true),
    ('container', 'Container', 'platform', '["cluster","namespace","pod","name"]'::jsonb, '["name","image"]'::jsonb, '{{name}}', '["image"]'::jsonb, true),
    ('node', 'Node', 'platform', '["cluster","name"]'::jsonb, '["name","host_ip"]'::jsonb, '{{name}}', '["cluster","host_ip"]'::jsonb, true),
    ('host', 'Host', 'infrastructure', '["environment","host"]'::jsonb, '["name","host","ip"]'::jsonb, '{{name}}', '["environment","ip"]'::jsonb, true),
    ('edge_agent', 'Edge Agent', 'infrastructure', '["environment","agent_id"]'::jsonb, '["name","agent_id","host"]'::jsonb, '{{name}}', '["environment","host"]'::jsonb, true),
    ('ingress', 'Ingress', 'traffic', '["cluster","namespace","name"]'::jsonb, '["name","host"]'::jsonb, '{{name}}', '["cluster","namespace","host"]'::jsonb, true),
    ('load_balancer', 'Load Balancer', 'traffic', '["environment","name"]'::jsonb, '["name","vip"]'::jsonb, '{{name}}', '["environment","vip"]'::jsonb, true),
    ('nginx', 'Nginx', 'middleware', '["environment","name"]'::jsonb, '["name","server_name"]'::jsonb, '{{name}}', '["environment","server_name"]'::jsonb, true),
    ('nacos', 'Nacos', 'middleware', '["environment","namespace","group","name"]'::jsonb, '["name","namespace","group"]'::jsonb, '{{name}}', '["environment","namespace","group"]'::jsonb, true),
    ('nacos_service', 'Nacos Service', 'middleware', '["environment","namespace","group","service_name"]'::jsonb, '["name","service_name","namespace","group"]'::jsonb, '{{name}}', '["environment","namespace","group"]'::jsonb, true),
    ('service_instance', 'Service Instance', 'middleware', '["environment","endpoint"]'::jsonb, '["name","endpoint","ip"]'::jsonb, '{{name}}', '["environment","endpoint"]'::jsonb, true),
    ('redis', 'Redis', 'middleware', '["environment","endpoint"]'::jsonb, '["name","endpoint"]'::jsonb, '{{name}}', '["environment","endpoint"]'::jsonb, true),
    ('redis_cluster', 'Redis Cluster', 'middleware', '["environment","cluster_name"]'::jsonb, '["name","cluster_name"]'::jsonb, '{{name}}', '["environment","cluster_name"]'::jsonb, true),
    ('redis_instance', 'Redis Instance', 'middleware', '["environment","endpoint"]'::jsonb, '["name","endpoint"]'::jsonb, '{{name}}', '["environment","endpoint"]'::jsonb, true),
    ('tidb_cluster', 'TiDB Cluster', 'database', '["environment","cluster_name"]'::jsonb, '["name","cluster_name"]'::jsonb, '{{name}}', '["environment","cluster_name"]'::jsonb, true),
    ('tidb', 'TiDB', 'database', '["environment","address"]'::jsonb, '["name","address"]'::jsonb, '{{name}}', '["environment","address"]'::jsonb, true),
    ('tikv', 'TiKV', 'database', '["environment","address"]'::jsonb, '["name","address"]'::jsonb, '{{name}}', '["environment","address"]'::jsonb, true),
    ('pd', 'PD', 'database', '["environment","address"]'::jsonb, '["name","address"]'::jsonb, '{{name}}', '["environment","address"]'::jsonb, true),
    ('database', 'Database', 'database', '["environment","name"]'::jsonb, '["name","schema"]'::jsonb, '{{name}}', '["environment"]'::jsonb, true),
    ('database_schema', 'Database Schema', 'database', '["environment","cluster","schema"]'::jsonb, '["name","schema"]'::jsonb, '{{name}}', '["environment","cluster"]'::jsonb, true),
    ('mq', 'MQ', 'middleware', '["environment","name"]'::jsonb, '["name"]'::jsonb, '{{name}}', '["environment"]'::jsonb, true),
    ('topic', 'Topic', 'middleware', '["environment","name"]'::jsonb, '["name"]'::jsonb, '{{name}}', '["environment"]'::jsonb, true),
    ('config_center', 'Config Center', 'middleware', '["environment","name"]'::jsonb, '["name"]'::jsonb, '{{name}}', '["environment"]'::jsonb, true),
    ('external_api', 'External API', 'external', '["environment","endpoint"]'::jsonb, '["name","endpoint"]'::jsonb, '{{name}}', '["environment","endpoint"]'::jsonb, true),
    ('third_party_service', 'Third Party Service', 'external', '["environment","name"]'::jsonb, '["name"]'::jsonb, '{{name}}', '["environment"]'::jsonb, true),
    ('storage', 'Storage', 'infrastructure', '["environment","name"]'::jsonb, '["name"]'::jsonb, '{{name}}', '["environment"]'::jsonb, true),
    ('pvc', 'PVC', 'platform', '["cluster","namespace","name"]'::jsonb, '["name","namespace"]'::jsonb, '{{name}}', '["cluster","namespace"]'::jsonb, true),
    ('network_device', 'Network Device', 'infrastructure', '["environment","name"]'::jsonb, '["name","ip"]'::jsonb, '{{name}}', '["environment","ip"]'::jsonb, true),
    ('virtual_ip', 'Virtual IP', 'infrastructure', '["environment","vip"]'::jsonb, '["name","vip"]'::jsonb, '{{name}}', '["environment","vip"]'::jsonb, true),
    ('k8s_deployment', 'K8s Deployment', 'platform', '["cluster","namespace","name"]'::jsonb, '["name","namespace"]'::jsonb, '{{name}}', '["cluster","namespace"]'::jsonb, true),
    ('k8s_pod', 'K8s Pod', 'platform', '["cluster","namespace","name"]'::jsonb, '["name","namespace","pod_ip"]'::jsonb, '{{name}}', '["cluster","namespace","pod_ip"]'::jsonb, true),
    ('k8s_service', 'K8s Service', 'platform', '["cluster","namespace","name"]'::jsonb, '["name","namespace"]'::jsonb, '{{name}}', '["cluster","namespace"]'::jsonb, true),
    ('k8s_ingress', 'K8s Ingress', 'traffic', '["cluster","namespace","name"]'::jsonb, '["name","host"]'::jsonb, '{{name}}', '["cluster","namespace","host"]'::jsonb, true),
    ('manual', 'Manual', 'custom', '["environment","name"]'::jsonb, '["name","display_name"]'::jsonb, '{{name}}', '["environment"]'::jsonb, true)
ON CONFLICT (type_key) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    category = EXCLUDED.category,
    identity_fields = EXCLUDED.identity_fields,
    searchable_fields = EXCLUDED.searchable_fields,
    label_template = EXCLUDED.label_template,
    detail_fields = EXCLUDED.detail_fields,
    built_in = true,
    updated_at = now();

INSERT INTO topology_relation_type (
    type_key,
    display_name,
    semantics,
    failure_propagation,
    default_direction,
    propagates_failure,
    built_in
)
VALUES
    ('contains', 'Contains', 'containment', 'both', 'both', true, true),
    ('belongs_to', 'Belongs To', 'containment', 'dst_to_src', 'upstream', true, true),
    ('deploys', 'Deploys', 'runtime_dep', 'dst_to_src', 'downstream', true, true),
    ('deployed_on', 'Deployed On', 'runtime_dep', 'dst_to_src', 'upstream', true, true),
    ('runs_on', 'Runs On', 'runtime_dep', 'dst_to_src', 'upstream', true, true),
    ('owns', 'Owns', 'ownership', 'none', 'both', false, true),
    ('routes_to', 'Routes To', 'traffic', 'dst_to_src', 'downstream', true, true),
    ('calls', 'Calls', 'traffic', 'dst_to_src', 'downstream', true, true),
    ('depends_on', 'Depends On', 'hard_dep', 'dst_to_src', 'downstream', true, true),
    ('hard_depends_on', 'Hard Depends On', 'hard_dep', 'dst_to_src', 'downstream', true, true),
    ('connects_to', 'Connects To', 'runtime_dep', 'dst_to_src', 'downstream', true, true),
    ('selects', 'Selects', 'runtime_dep', 'src_to_dst', 'downstream', true, true),
    ('exposes', 'Exposes', 'traffic', 'dst_to_src', 'upstream', true, true),
    ('stores_in', 'Stores In', 'hard_dep', 'dst_to_src', 'downstream', true, true),
    ('reads_from', 'Reads From', 'hard_dep', 'dst_to_src', 'downstream', true, true),
    ('writes_to', 'Writes To', 'hard_dep', 'dst_to_src', 'downstream', true, true),
    ('replicates_to', 'Replicates To', 'runtime_dep', 'both', 'both', true, true),
    ('member_of', 'Member Of', 'containment', 'dst_to_src', 'upstream', true, true),
    ('configured_by', 'Configured By', 'configuration', 'dst_to_src', 'upstream', true, true),
    ('registered_in', 'Registered In', 'configuration', 'dst_to_src', 'upstream', true, true),
    ('monitored_by', 'Monitored By', 'observation', 'none', 'upstream', false, true),
    ('discovered_from', 'Discovered From', 'observation', 'none', 'upstream', false, true),
    ('associated_with', 'Associated With', 'annotation', 'none', 'both', false, true),
    ('observed_with', 'Observed With', 'observation', 'none', 'both', false, true)
ON CONFLICT (type_key) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    semantics = EXCLUDED.semantics,
    failure_propagation = EXCLUDED.failure_propagation,
    default_direction = EXCLUDED.default_direction,
    propagates_failure = EXCLUDED.propagates_failure,
    built_in = true,
    updated_at = now();

UPDATE topology_node
SET node_type_id = topology_node_type.id
FROM topology_node_type
WHERE topology_node.node_type_id IS NULL
  AND topology_node.kind = topology_node_type.type_key;

UPDATE topology_edge
SET relation_type_id = topology_relation_type.id
FROM topology_relation_type
WHERE topology_edge.relation_type_id IS NULL
  AND topology_edge.edge_type = topology_relation_type.type_key;
