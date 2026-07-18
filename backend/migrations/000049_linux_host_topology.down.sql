DELETE FROM topology_node_type
WHERE type_key IN ('host_group', 'process')
  AND built_in = true
  AND NOT EXISTS (
      SELECT 1 FROM topology_node
      WHERE topology_node.node_type_id = topology_node_type.id
  );
