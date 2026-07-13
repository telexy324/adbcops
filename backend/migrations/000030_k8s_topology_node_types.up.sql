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
    ('k8s_endpoint', 'K8s Endpoint', 'platform', '["cluster","namespace","name"]'::jsonb, '["name","namespace","ip"]'::jsonb, '{{name}}', '["cluster","namespace"]'::jsonb, true),
    ('k8s_node', 'K8s Node', 'platform', '["cluster","name"]'::jsonb, '["name","provider_id"]'::jsonb, '{{name}}', '["cluster","provider_id"]'::jsonb, true),
    ('k8s_pvc', 'K8s PVC', 'platform', '["cluster","namespace","name"]'::jsonb, '["name","namespace","volume_name"]'::jsonb, '{{name}}', '["cluster","namespace","volume_name"]'::jsonb, true)
ON CONFLICT (type_key) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    category = EXCLUDED.category,
    identity_fields = EXCLUDED.identity_fields,
    searchable_fields = EXCLUDED.searchable_fields,
    label_template = EXCLUDED.label_template,
    detail_fields = EXCLUDED.detail_fields,
    built_in = true,
    updated_at = now();
