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
    ('host_group', 'Host Group', 'infrastructure', '["environment","name"]'::jsonb, '["name","system_name"]'::jsonb, '{{name}}', '["environment","system_name"]'::jsonb, true),
    ('process', 'Process', 'runtime', '["environment","identity"]'::jsonb, '["name","command_name","executable"]'::jsonb, '{{name}}', '["environment","command_name","version"]'::jsonb, true)
ON CONFLICT (type_key) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    category = EXCLUDED.category,
    identity_fields = EXCLUDED.identity_fields,
    searchable_fields = EXCLUDED.searchable_fields,
    label_template = EXCLUDED.label_template,
    detail_fields = EXCLUDED.detail_fields,
    built_in = true,
    updated_at = now();
