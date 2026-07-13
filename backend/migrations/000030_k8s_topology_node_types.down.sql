DELETE FROM topology_node_type
WHERE type_key IN ('k8s_endpoint', 'k8s_node', 'k8s_pvc');
