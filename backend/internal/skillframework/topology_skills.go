package skillframework

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"unicode"

	datasourcesvc "aiops-platform/backend/internal/datasource"
	"aiops-platform/backend/internal/model"
	topologysvc "aiops-platform/backend/internal/topology"
)

const (
	topologySkillMaxDepth = 5
	topologySkillMaxNodes = 500
	topologySkillMaxEdges = 1000
)

type TopologySkillService interface {
	FindNode(ctx context.Context, input topologysvc.FindNodeInput) (*topologysvc.FindNodeResult, error)
	ExpandTopology(ctx context.Context, query topologysvc.ExpandTopologyQuery) (*topologysvc.ExpandTopologyResult, error)
	ExplainPath(ctx context.Context, query topologysvc.ExplainPathQuery) (*topologysvc.ExplainPathResult, error)
	ListNodeAliases(ctx context.Context, nodeID int64) ([]model.TopologyNodeAlias, error)
	ListSourceConfigs(ctx context.Context) ([]model.TopologySourceConfig, error)
}

type TopologyDataSourceLister interface {
	List(ctx context.Context, actor *model.AppUser) ([]datasourcesvc.DataSourceView, error)
}

func TopologySkills(topology TopologySkillService, dataSources TopologyDataSourceLister) []Skill {
	base := topologySkillBase{topology: topology, dataSources: dataSources}
	return []Skill{
		FindTopologyNodeSkill{base: base},
		ExpandTopologySkill{base: base},
		FindDependenciesSkill{base: base},
		ExplainTopologyPathSkill{base: base},
		GetTopologyDataSourceBindingsSkill{base: base},
	}
}

type topologySkillBase struct {
	topology    TopologySkillService
	dataSources TopologyDataSourceLister
}

type topologyNodeReference struct {
	NodeKey     string `json:"nodeKey"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	Kind        string `json:"kind"`
	Category    string `json:"category,omitempty"`
	HostID      int64  `json:"hostId,omitempty"`
	Environment string `json:"environment,omitempty"`
	Cluster     string `json:"cluster,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	SourceType  string `json:"sourceType"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

type topologyEdgeReference struct {
	EdgeKey        string  `json:"edgeKey"`
	FromNodeKey    string  `json:"fromNodeKey"`
	ToNodeKey      string  `json:"toNodeKey"`
	EdgeType       string  `json:"edgeType"`
	Confidence     float64 `json:"confidence"`
	Status         string  `json:"status"`
	LastObservedAt string  `json:"lastObservedAt,omitempty"`
	StaleAt        string  `json:"staleAt,omitempty"`
	UpdatedAt      string  `json:"updatedAt,omitempty"`
}

type topologyBinding struct {
	DataSourceID int64    `json:"dataSourceId"`
	SourceType   string   `json:"sourceType"`
	Environment  string   `json:"environment,omitempty"`
	Source       string   `json:"source"`
	Sources      []string `json:"sources"`
	Confidence   float64  `json:"confidence"`
	UpdatedAt    string   `json:"updatedAt,omitempty"`
	Status       string   `json:"status"`
	Accepted     bool     `json:"accepted"`
}

type FindTopologyNodeSkill struct{ base topologySkillBase }

func (s FindTopologyNodeSkill) Definition() SkillDefinition {
	return topologySkillDefinition(
		"find_topology_node",
		"Resolve a topology node through node key, name, alias or CMDB identifier without mutating topology.",
		json.RawMessage(`{"type":"object","required":["query"],"properties":{"query":{"type":"string","minLength":1},"environment":{"type":"string"},"nodeTypes":{"type":"array","items":{"type":"string"}},"limit":{"type":"integer","minimum":1,"maximum":50}}}`),
	)
}

func (s FindTopologyNodeSkill) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var request struct {
		Query       string   `json:"query"`
		Environment string   `json:"environment"`
		NodeTypes   []string `json:"nodeTypes"`
		Limit       int      `json:"limit"`
	}
	if err := json.Unmarshal(input, &request); err != nil || strings.TrimSpace(request.Query) == "" {
		return nil, ErrInvalidInput
	}
	if s.base.topology == nil {
		return partialError("topology", "topology service is not configured"), nil
	}
	if allowed, checked := s.base.environmentAllowed(ctx, request.Environment); checked && !allowed {
		return topologyPermissionOutput(request.Environment), nil
	}
	limit := clamp(request.Limit, 10, 50)
	result, err := s.base.topology.FindNode(ctx, topologysvc.FindNodeInput{
		Query: request.Query, Environment: request.Environment, NodeTypes: request.NodeTypes, Limit: limit,
	})
	if err != nil {
		return partialError("topology", err.Error()), nil
	}
	candidates := s.base.filterNodeReferences(ctx, result.Candidates)
	matched := result.Matched && len(candidates) == 1
	ambiguous := len(candidates) > 1
	var node *topologyNodeReference
	if matched {
		node = &candidates[0]
	}
	missing := []string{}
	if len(candidates) == 0 {
		missing = append(missing, "no accessible topology node matched the query")
	} else if ambiguous {
		missing = append(missing, "topology node is ambiguous; provide environment or nodeKey")
	}
	return json.Marshal(map[string]any{
		"partial": false, "matched": matched, "ambiguous": ambiguous, "node": node, "candidates": candidates,
		"missingEvidence": missing, "evidenceReferences": nodeEvidenceReferences(candidates),
	})
}

type ExpandTopologySkill struct{ base topologySkillBase }

func (s ExpandTopologySkill) Definition() SkillDefinition {
	return topologySkillDefinition(
		"expand_topology",
		"Expand an upstream, downstream or bidirectional topology neighborhood under strict traversal limits.",
		topologyExpandInputSchema(),
	)
}

func (s ExpandTopologySkill) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	request, err := decodeExpandRequest(input)
	if err != nil {
		return nil, err
	}
	if s.base.topology == nil {
		return partialError("topology", "topology service is not configured"), nil
	}
	if allowed, checked := s.base.environmentAllowed(ctx, request.Environment); checked && !allowed {
		return topologyPermissionOutput(request.Environment), nil
	}
	result, err := s.base.topology.ExpandTopology(ctx, request.toQuery())
	if err != nil {
		return partialError("topology", err.Error()), nil
	}
	nodes, edges, paths := s.base.filterExpansion(ctx, result)
	missing := []string{}
	if len(paths) == 0 {
		missing = append(missing, "no accessible topology relationships found")
	}
	return json.Marshal(map[string]any{
		"partial": false, "rootKey": result.RootKey, "direction": result.Direction, "depth": result.Depth,
		"nodes": nodes, "edges": edges, "paths": paths, "cycleDetected": result.CycleDetected,
		"truncated": result.Truncated, "limits": request.limits(), "missingEvidence": missing,
		"evidenceReferences": append([]string{result.EvidenceKey}, pathEvidenceReferences(paths)...),
	})
}

type FindDependenciesSkill struct{ base topologySkillBase }

func (s FindDependenciesSkill) Definition() SkillDefinition {
	return topologySkillDefinition(
		"find_dependencies",
		"Find direct and multi-hop dependencies and classify them as databases, middleware, downstream services or infrastructure.",
		topologyExpandInputSchema(),
	)
}

func (s FindDependenciesSkill) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	request, err := decodeExpandRequest(input)
	if err != nil {
		return nil, err
	}
	if request.Direction == "" {
		request.Direction = "downstream"
	}
	if s.base.topology == nil {
		return partialError("topology", "topology service is not configured"), nil
	}
	if allowed, checked := s.base.environmentAllowed(ctx, request.Environment); checked && !allowed {
		return topologyPermissionOutput(request.Environment), nil
	}
	result, err := s.base.topology.ExpandTopology(ctx, request.toQuery())
	if err != nil {
		return partialError("topology", err.Error()), nil
	}
	nodes, edges, paths := s.base.filterExpansion(ctx, result)
	hops := map[string]int{}
	for _, path := range paths {
		if current, exists := hops[path.TargetNodeKey]; !exists || path.Hops < current {
			hops[path.TargetNodeKey] = path.Hops
		}
	}
	dependencies := make([]map[string]any, 0, len(nodes))
	for _, node := range nodes {
		if node.NodeKey == result.RootKey {
			continue
		}
		dependencies = append(dependencies, map[string]any{
			"node": node, "dependencyType": classifyTopologyNode(node), "hops": hops[node.NodeKey],
			"evidenceReference": "topology-node:" + node.NodeKey,
		})
	}
	sort.Slice(dependencies, func(i, j int) bool {
		left, _ := dependencies[i]["hops"].(int)
		right, _ := dependencies[j]["hops"].(int)
		if left == right {
			return dependencies[i]["node"].(topologyNodeReference).NodeKey < dependencies[j]["node"].(topologyNodeReference).NodeKey
		}
		return left < right
	})
	missing := []string{}
	if len(dependencies) == 0 {
		missing = append(missing, "no accessible dependencies found")
	}
	return json.Marshal(map[string]any{
		"partial": false, "rootKey": result.RootKey, "dependencies": dependencies, "edges": edges,
		"truncated": result.Truncated, "limits": request.limits(), "missingEvidence": missing,
		"evidenceReferences": append([]string{result.EvidenceKey}, pathEvidenceReferences(paths)...),
	})
}

type ExplainTopologyPathSkill struct{ base topologySkillBase }

func (s ExplainTopologyPathSkill) Definition() SkillDefinition {
	return topologySkillDefinition(
		"explain_topology_path",
		"Explain bounded topology paths between two nodes with confidence and evidence references.",
		json.RawMessage(`{"type":"object","required":["fromNodeKey","toNodeKey"],"properties":{"fromNodeKey":{"type":"string","minLength":1},"toNodeKey":{"type":"string","minLength":1},"direction":{"type":"string","enum":["upstream","downstream","both"]},"maxDepth":{"type":"integer","minimum":1,"maximum":5},"maxPaths":{"type":"integer","minimum":1,"maximum":50},"onlyPropagating":{"type":"boolean"},"semantics":{"type":"array","items":{"type":"string"}},"observedNodeKeys":{"type":"array","items":{"type":"string"}},"environment":{"type":"string"},"cluster":{"type":"string"},"namespace":{"type":"string"}}}`),
	)
}

func (s ExplainTopologyPathSkill) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var request struct {
		FromNodeKey      string   `json:"fromNodeKey"`
		ToNodeKey        string   `json:"toNodeKey"`
		Direction        string   `json:"direction"`
		MaxDepth         int      `json:"maxDepth"`
		MaxPaths         int      `json:"maxPaths"`
		OnlyPropagating  bool     `json:"onlyPropagating"`
		Semantics        []string `json:"semantics"`
		ObservedNodeKeys []string `json:"observedNodeKeys"`
		Environment      string   `json:"environment"`
		Cluster          string   `json:"cluster"`
		Namespace        string   `json:"namespace"`
	}
	if err := json.Unmarshal(input, &request); err != nil || strings.TrimSpace(request.FromNodeKey) == "" || strings.TrimSpace(request.ToNodeKey) == "" {
		return nil, ErrInvalidInput
	}
	if s.base.topology == nil {
		return partialError("topology", "topology service is not configured"), nil
	}
	if allowed, checked := s.base.environmentAllowed(ctx, request.Environment); checked && !allowed {
		return topologyPermissionOutput(request.Environment), nil
	}
	request.MaxDepth = clamp(request.MaxDepth, 2, topologySkillMaxDepth)
	request.MaxPaths = clamp(request.MaxPaths, 10, 50)
	result, err := s.base.topology.ExplainPath(ctx, topologysvc.ExplainPathQuery{
		FromNodeKey: request.FromNodeKey, ToNodeKey: request.ToNodeKey, Direction: request.Direction,
		MaxDepth: request.MaxDepth, MaxPaths: request.MaxPaths, OnlyPropagating: request.OnlyPropagating,
		Semantics: request.Semantics, ObservedNodeKeys: request.ObservedNodeKeys, Environment: request.Environment,
		Cluster: request.Cluster, Namespace: request.Namespace,
	})
	if err != nil {
		return partialError("topology", err.Error()), nil
	}
	paths := s.base.filterAccessiblePaths(ctx, result.Paths)
	missing := []string{}
	if len(paths) == 0 {
		missing = append(missing, "no topology path found")
	}
	return json.Marshal(map[string]any{
		"partial": false, "fromNodeKey": result.FromNodeKey, "toNodeKey": result.ToNodeKey,
		"paths": paths, "truncated": result.Truncated,
		"limits":             map[string]int{"maxDepth": request.MaxDepth, "maxPaths": request.MaxPaths},
		"missingEvidence":    missing,
		"evidenceReferences": append([]string{result.EvidenceKey}, pathEvidenceReferences(paths)...),
	})
}

type GetTopologyDataSourceBindingsSkill struct{ base topologySkillBase }

func (s GetTopologyDataSourceBindingsSkill) Definition() SkillDefinition {
	return topologySkillDefinition(
		"get_topology_data_source_bindings",
		"Resolve accessible data sources for a topology node using explicit configuration, source references, aliases, CMDB identifiers and labels.",
		json.RawMessage(`{"type":"object","required":["nodeKey"],"properties":{"nodeKey":{"type":"string","minLength":1},"environment":{"type":"string"},"explicitDataSourceIds":{"type":"array","items":{"type":"integer"}},"minimumConfidence":{"type":"number","minimum":0.5,"maximum":1}}}`),
	)
}

func (s GetTopologyDataSourceBindingsSkill) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var request struct {
		NodeKey              string  `json:"nodeKey"`
		Environment          string  `json:"environment"`
		ExplicitDataSourceID []int64 `json:"explicitDataSourceIds"`
		MinimumConfidence    float64 `json:"minimumConfidence"`
	}
	if err := json.Unmarshal(input, &request); err != nil || strings.TrimSpace(request.NodeKey) == "" {
		return nil, ErrInvalidInput
	}
	if s.base.topology == nil || s.base.dataSources == nil {
		return partialError("topology_bindings", "topology or data source service is not configured"), nil
	}
	if allowed, checked := s.base.environmentAllowed(ctx, request.Environment); checked && !allowed {
		return topologyPermissionOutput(request.Environment), nil
	}
	found, err := s.base.topology.FindNode(ctx, topologysvc.FindNodeInput{
		Query: request.NodeKey, Environment: request.Environment, Limit: 10,
	})
	if err != nil {
		return partialError("topology", err.Error()), nil
	}
	nodes := s.base.filterAccessibleNodes(ctx, found.Candidates)
	if len(nodes) != 1 {
		missing := "topology node not found"
		if len(nodes) > 1 {
			missing = "topology node is ambiguous; provide an exact nodeKey and environment"
		}
		return json.Marshal(map[string]any{
			"partial": false, "bindings": []topologyBinding{}, "candidates": []topologyBinding{},
			"missingEvidence": []string{missing}, "evidenceReferences": []string{},
		})
	}
	node := nodes[0]
	views, err := s.base.accessibleDataSources(ctx)
	if err != nil {
		return partialError("data_sources", err.Error()), nil
	}
	aliases, _ := s.base.topology.ListNodeAliases(ctx, node.ID)
	sourceConfigs, _ := s.base.topology.ListSourceConfigs(ctx)
	threshold := request.MinimumConfidence
	if threshold <= 0 {
		threshold = 0.8
	}
	candidates := resolveTopologyBindings(node, aliases, sourceConfigs, views, request.ExplicitDataSourceID, threshold)
	bindings := make([]topologyBinding, 0, len(candidates))
	conflicts := []string{}
	for _, candidate := range candidates {
		if candidate.Accepted {
			bindings = append(bindings, candidate)
		}
		if candidate.Status == "conflict" {
			conflicts = appendUniqueStrings(conflicts, candidate.SourceType)
		}
	}
	missing := []string{}
	if len(bindings) == 0 {
		missing = append(missing, "no reliable accessible data source binding")
	}
	if len(conflicts) > 0 {
		missing = append(missing, "conflicting data source bindings: "+strings.Join(conflicts, ", "))
	}
	return json.Marshal(map[string]any{
		"partial": false, "node": toTopologyNodeReference(node), "bindings": bindings, "candidates": candidates,
		"conflicts": conflicts, "missingEvidence": missing,
		"evidenceReferences": []string{"topology-node:" + node.NodeKey},
	})
}

type topologyExpandRequest struct {
	NodeKey          string   `json:"nodeKey"`
	Depth            int      `json:"depth"`
	Direction        string   `json:"direction"`
	MaxNodes         int      `json:"maxNodes"`
	MaxEdges         int      `json:"maxEdges"`
	OnlyPropagating  bool     `json:"onlyPropagating"`
	Semantics        []string `json:"semantics"`
	ObservedNodeKeys []string `json:"observedNodeKeys"`
	Environment      string   `json:"environment"`
	Cluster          string   `json:"cluster"`
	Namespace        string   `json:"namespace"`
}

func decodeExpandRequest(input json.RawMessage) (topologyExpandRequest, error) {
	var request topologyExpandRequest
	if err := json.Unmarshal(input, &request); err != nil || strings.TrimSpace(request.NodeKey) == "" {
		return request, ErrInvalidInput
	}
	request.Depth = clamp(request.Depth, 2, topologySkillMaxDepth)
	request.MaxNodes = clamp(request.MaxNodes, 200, topologySkillMaxNodes)
	request.MaxEdges = clamp(request.MaxEdges, 500, topologySkillMaxEdges)
	return request, nil
}

func (r topologyExpandRequest) toQuery() topologysvc.ExpandTopologyQuery {
	return topologysvc.ExpandTopologyQuery{
		NodeKey: r.NodeKey, Depth: r.Depth, Direction: r.Direction, MaxNodes: r.MaxNodes, MaxEdges: r.MaxEdges,
		OnlyPropagating: r.OnlyPropagating, Semantics: r.Semantics, ObservedNodeKeys: r.ObservedNodeKeys,
		Environment: r.Environment, Cluster: r.Cluster, Namespace: r.Namespace,
	}
}

func (r topologyExpandRequest) limits() map[string]int {
	return map[string]int{"depth": r.Depth, "maxNodes": r.MaxNodes, "maxEdges": r.MaxEdges}
}

func topologySkillDefinition(name, description string, inputSchema json.RawMessage) SkillDefinition {
	return SkillDefinition{
		Name: name, Version: "v1", Description: description, InputSchema: inputSchema,
		OutputSchema: partialOutputSchema(), RiskLevel: model.SkillRiskSafeRead, ReadOnly: true, TimeoutSecond: 20,
	}
}

func topologyExpandInputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","required":["nodeKey"],"properties":{"nodeKey":{"type":"string","minLength":1},"depth":{"type":"integer","minimum":1,"maximum":5},"direction":{"type":"string","enum":["upstream","downstream","both"]},"maxNodes":{"type":"integer","minimum":1,"maximum":500},"maxEdges":{"type":"integer","minimum":1,"maximum":1000},"onlyPropagating":{"type":"boolean"},"semantics":{"type":"array","items":{"type":"string"}},"observedNodeKeys":{"type":"array","items":{"type":"string"}},"environment":{"type":"string"},"cluster":{"type":"string"},"namespace":{"type":"string"}}}`)
}

func (b topologySkillBase) accessibleDataSources(ctx context.Context) ([]datasourcesvc.DataSourceView, error) {
	if b.dataSources == nil {
		return nil, nil
	}
	views, err := b.dataSources.List(ctx, ActorFromContext(ctx))
	if err != nil {
		return nil, err
	}
	result := make([]datasourcesvc.DataSourceView, 0, len(views))
	for _, view := range views {
		if view.Enabled && view.ReadOnly && supportedTopologyDataSourceType(view.SourceType) {
			result = append(result, view)
		}
	}
	return result, nil
}

func (b topologySkillBase) environmentAllowed(ctx context.Context, environment string) (bool, bool) {
	environment = normalizeTopologyEnvironment(environment)
	actor := ActorFromContext(ctx)
	if environment == "" || actor == nil || actor.Role == model.RoleAdmin || b.dataSources == nil {
		return true, false
	}
	views, err := b.accessibleDataSources(ctx)
	if err != nil {
		return false, false
	}
	for _, view := range views {
		if normalizeTopologyEnvironment(stringFromPointer(view.Environment)) == environment {
			return true, true
		}
	}
	return false, true
}

func (b topologySkillBase) filterAccessibleNodes(ctx context.Context, nodes []model.TopologyNode) []model.TopologyNode {
	actor := ActorFromContext(ctx)
	if actor == nil || actor.Role == model.RoleAdmin || b.dataSources == nil {
		return nodes
	}
	views, err := b.accessibleDataSources(ctx)
	if err != nil {
		return nil
	}
	environments := map[string]struct{}{}
	for _, view := range views {
		if environment := normalizeTopologyEnvironment(stringFromPointer(view.Environment)); environment != "" {
			environments[environment] = struct{}{}
		}
	}
	result := make([]model.TopologyNode, 0, len(nodes))
	for _, node := range nodes {
		if node.Environment == nil {
			continue
		}
		if _, ok := environments[normalizeTopologyEnvironment(*node.Environment)]; ok {
			result = append(result, node)
		}
	}
	return result
}

func (b topologySkillBase) filterNodeReferences(ctx context.Context, nodes []model.TopologyNode) []topologyNodeReference {
	nodes = b.filterAccessibleNodes(ctx, nodes)
	result := make([]topologyNodeReference, 0, len(nodes))
	for _, node := range nodes {
		result = append(result, toTopologyNodeReference(node))
	}
	return result
}

func (b topologySkillBase) filterExpansion(ctx context.Context, result *topologysvc.ExpandTopologyResult) ([]topologyNodeReference, []topologyEdgeReference, []topologysvc.TopologyPath) {
	allowedNodes := b.filterAccessibleNodes(ctx, result.Nodes)
	allowed := map[string]struct{}{}
	nodes := make([]topologyNodeReference, 0, len(allowedNodes))
	for _, node := range allowedNodes {
		allowed[node.NodeKey] = struct{}{}
		nodes = append(nodes, toTopologyNodeReference(node))
	}
	edges := make([]topologyEdgeReference, 0, len(result.Edges))
	for _, edge := range result.Edges {
		if _, ok := allowed[edge.FromNodeKey]; !ok {
			continue
		}
		if _, ok := allowed[edge.ToNodeKey]; !ok {
			continue
		}
		edges = append(edges, toTopologyEdgeReference(edge))
	}
	paths := make([]topologysvc.TopologyPath, 0, len(result.Paths))
	for _, path := range result.Paths {
		pathAllowed := true
		for _, nodeKey := range path.NodeKeys {
			if _, ok := allowed[nodeKey]; !ok {
				pathAllowed = false
				break
			}
		}
		if pathAllowed {
			paths = append(paths, path)
		}
	}
	return nodes, edges, paths
}

func (b topologySkillBase) filterAccessiblePaths(ctx context.Context, paths []topologysvc.TopologyPath) []topologysvc.TopologyPath {
	actor := ActorFromContext(ctx)
	if actor == nil || actor.Role == model.RoleAdmin || b.dataSources == nil || b.topology == nil {
		return paths
	}
	views, err := b.accessibleDataSources(ctx)
	if err != nil {
		return nil
	}
	environments := map[string]struct{}{}
	for _, view := range views {
		if environment := normalizeTopologyEnvironment(stringFromPointer(view.Environment)); environment != "" {
			environments[environment] = struct{}{}
		}
	}
	nodeAllowed := map[string]bool{}
	result := make([]topologysvc.TopologyPath, 0, len(paths))
	for _, path := range paths {
		allowed := true
		for _, nodeKey := range path.NodeKeys {
			value, checked := nodeAllowed[nodeKey]
			if !checked {
				value = b.nodeKeyAllowed(ctx, nodeKey, environments)
				nodeAllowed[nodeKey] = value
			}
			if !value {
				allowed = false
				break
			}
		}
		if allowed {
			result = append(result, path)
		}
	}
	return result
}

func (b topologySkillBase) nodeKeyAllowed(ctx context.Context, nodeKey string, environments map[string]struct{}) bool {
	found, err := b.topology.FindNode(ctx, topologysvc.FindNodeInput{Query: nodeKey, Limit: 5})
	if err != nil || found == nil {
		return false
	}
	for _, node := range found.Candidates {
		if node.NodeKey != nodeKey || node.Environment == nil {
			continue
		}
		if _, ok := environments[normalizeTopologyEnvironment(*node.Environment)]; ok {
			return true
		}
	}
	return false
}

func resolveTopologyBindings(
	node model.TopologyNode,
	aliases []model.TopologyNodeAlias,
	sourceConfigs []model.TopologySourceConfig,
	views []datasourcesvc.DataSourceView,
	explicitIDs []int64,
	threshold float64,
) []topologyBinding {
	accessible := map[int64]datasourcesvc.DataSourceView{}
	for _, view := range views {
		accessible[view.ID] = view
	}
	type score struct {
		confidence float64
		source     string
		sources    []string
	}
	scores := map[int64]score{}
	add := func(id int64, confidence float64, source string) {
		if _, ok := accessible[id]; !ok || id <= 0 {
			return
		}
		current := scores[id]
		if confidence > current.confidence {
			current.confidence = confidence
			current.source = source
		}
		current.sources = appendUniqueStrings(current.sources, source)
		scores[id] = current
	}
	for _, id := range explicitIDs {
		add(id, 1, "explicit_input")
	}
	for _, id := range dataSourceIDsFromJSON(node.SourceRef) {
		add(id, 1, "node_source_ref")
	}
	for _, raw := range [][]byte{node.Properties, node.ResolvedAttributes, node.Labels} {
		for _, id := range dataSourceIDsFromJSON(raw) {
			add(id, 1, "node_explicit_config")
		}
	}
	sourceConfigID := integerFromJSON(node.SourceRef, "sourceConfigId")
	for _, config := range sourceConfigs {
		if !config.Enabled || config.DataSourceID == nil {
			continue
		}
		if config.ID == sourceConfigID || sourceConfigMatchesNode(config, node, aliases) {
			add(*config.DataSourceID, 0.95, "topology_source_config")
		}
	}
	identities := topologyNodeIdentities(node, aliases)
	for _, view := range views {
		if !bindingEnvironmentCompatible(node, view) {
			continue
		}
		labels := []string{view.Name, stringFromPointer(view.SystemName), stringFromPointer(view.ComponentName)}
		best := 0.0
		source := ""
		for _, identity := range identities {
			for _, label := range labels {
				confidence := topologyLabelConfidence(identity, label)
				if confidence > best {
					best = confidence
					source = "data_source_label"
				}
			}
		}
		if configExplicitlyReferencesNode(view.Config, node, aliases) && best < 0.9 {
			best = 0.9
			source = "data_source_explicit_config"
		}
		if cmdbIdentifierMatchesConfig(node, view.Config) && best < 0.88 {
			best = 0.88
			source = "cmdb_identifier"
		}
		if best > 0 {
			add(view.ID, best, source)
		}
	}
	candidates := make([]topologyBinding, 0, len(scores))
	for id, value := range scores {
		view := accessible[id]
		sort.Strings(value.sources)
		candidates = append(candidates, topologyBinding{
			DataSourceID: id, SourceType: view.SourceType, Environment: stringFromPointer(view.Environment),
			Source: value.source, Sources: value.sources, Confidence: value.confidence, UpdatedAt: view.UpdatedAt,
			Status: "candidate",
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].SourceType == candidates[j].SourceType {
			if candidates[i].Confidence == candidates[j].Confidence {
				return candidates[i].DataSourceID < candidates[j].DataSourceID
			}
			return candidates[i].Confidence > candidates[j].Confidence
		}
		return candidates[i].SourceType < candidates[j].SourceType
	})
	byType := map[string][]int{}
	for index := range candidates {
		if candidates[index].Confidence >= threshold {
			byType[candidates[index].SourceType] = append(byType[candidates[index].SourceType], index)
		} else {
			candidates[index].Status = "low_confidence"
		}
	}
	for _, indexes := range byType {
		best := candidates[indexes[0]].Confidence
		tied := []int{}
		for _, index := range indexes {
			if best-candidates[index].Confidence < 0.05 {
				tied = append(tied, index)
			}
		}
		if len(tied) > 1 {
			for _, index := range tied {
				candidates[index].Status = "conflict"
			}
			continue
		}
		candidates[indexes[0]].Status = "resolved"
		candidates[indexes[0]].Accepted = true
	}
	return candidates
}

func sourceConfigMatchesNode(config model.TopologySourceConfig, node model.TopologyNode, aliases []model.TopologyNodeAlias) bool {
	if config.DataSourceID == nil {
		return false
	}
	var scope map[string]any
	if json.Unmarshal(config.Scope, &scope) != nil {
		return false
	}
	identities := topologyNodeIdentities(node, aliases)
	for _, key := range []string{"nodeKey", "nodeKeys", "component", "components", "name", "names", "cmdbId", "cmdbIds"} {
		for _, value := range stringsFromAny(scope[key]) {
			for _, identity := range identities {
				if normalizeTopologyLabel(value) == normalizeTopologyLabel(identity) {
					return true
				}
			}
		}
	}
	return false
}

func configExplicitlyReferencesNode(raw json.RawMessage, node model.TopologyNode, aliases []model.TopologyNodeAlias) bool {
	var config map[string]any
	if json.Unmarshal(raw, &config) != nil {
		return false
	}
	identities := topologyNodeIdentities(node, aliases)
	for _, key := range []string{"topologyNodeKey", "topologyNodeKeys", "nodeKey", "nodeKeys", "componentName", "serviceName"} {
		for _, value := range stringsFromAny(config[key]) {
			for _, identity := range identities {
				if normalizeTopologyLabel(value) == normalizeTopologyLabel(identity) {
					return true
				}
			}
		}
	}
	return false
}

func cmdbIdentifierMatchesConfig(node model.TopologyNode, raw json.RawMessage) bool {
	ids := append(stringsFromJSON(node.Properties, "cmdbId", "cmdb_id", "externalId", "external_id"),
		stringsFromJSON(node.ResolvedAttributes, "cmdbId", "cmdb_id", "externalId", "external_id")...)
	if len(ids) == 0 {
		return false
	}
	var config map[string]any
	if json.Unmarshal(raw, &config) != nil {
		return false
	}
	for _, key := range []string{"cmdbId", "cmdbIds", "externalId", "externalIds"} {
		for _, configured := range stringsFromAny(config[key]) {
			for _, id := range ids {
				if strings.EqualFold(strings.TrimSpace(configured), strings.TrimSpace(id)) {
					return true
				}
			}
		}
	}
	return false
}

func dataSourceIDsFromJSON(raw []byte) []int64 {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	result := []int64{}
	var walk func(any, string)
	walk = func(current any, key string) {
		switch typed := current.(type) {
		case map[string]any:
			for childKey, child := range typed {
				walk(child, childKey)
			}
		case []any:
			for _, child := range typed {
				walk(child, key)
			}
		case float64:
			lower := strings.ToLower(key)
			if lower == "datasourceid" || lower == "datasourceids" {
				id := int64(typed)
				if id > 0 && typed == float64(id) {
					result = append(result, id)
				}
			}
		case string:
			lower := strings.ToLower(key)
			if lower == "datasourceid" || lower == "datasourceids" {
				if id, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64); err == nil && id > 0 {
					result = append(result, id)
				}
			}
		}
	}
	walk(value, "")
	return uniqueInt64s(result)
}

func integerFromJSON(raw []byte, key string) int64 {
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return 0
	}
	for _, candidate := range stringsFromAny(value[key]) {
		if parsed, err := strconv.ParseInt(candidate, 10, 64); err == nil {
			return parsed
		}
	}
	if numeric, ok := value[key].(float64); ok {
		return int64(numeric)
	}
	return 0
}

func topologyNodeIdentities(node model.TopologyNode, aliases []model.TopologyNodeAlias) []string {
	values := []string{node.NodeKey, node.Name, stringFromPointer(node.DisplayName)}
	for _, alias := range aliases {
		values = append(values, alias.Alias)
	}
	values = append(values, stringsFromJSON(node.Properties, "cmdbId", "cmdb_id", "externalId", "external_id", "serviceName", "componentName")...)
	values = append(values, stringsFromJSON(node.Labels, "app", "app.kubernetes.io/name", "service", "component")...)
	return appendUniqueStrings(nil, values...)
}

func stringsFromJSON(raw []byte, keys ...string) []string {
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	result := []string{}
	for _, key := range keys {
		result = append(result, stringsFromAny(value[key])...)
	}
	return result
}

func stringsFromAny(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case float64:
		return []string{strconv.FormatInt(int64(typed), 10)}
	case []any:
		result := []string{}
		for _, item := range typed {
			result = append(result, stringsFromAny(item)...)
		}
		return result
	}
	return nil
}

func topologyLabelConfidence(left, right string) float64 {
	left = normalizeTopologyLabel(left)
	right = normalizeTopologyLabel(right)
	if left == "" || right == "" {
		return 0
	}
	if left == right {
		return 0.85
	}
	if len(left) >= 5 && len(right) >= 5 && (strings.Contains(left, right) || strings.Contains(right, left)) {
		return 0.65
	}
	return 0
}

func bindingEnvironmentCompatible(node model.TopologyNode, view datasourcesvc.DataSourceView) bool {
	nodeEnvironment := normalizeTopologyEnvironment(stringFromPointer(node.Environment))
	viewEnvironment := normalizeTopologyEnvironment(stringFromPointer(view.Environment))
	return nodeEnvironment == "" || viewEnvironment == "" || nodeEnvironment == viewEnvironment
}

func supportedTopologyDataSourceType(sourceType string) bool {
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case model.DataSourceTypeElasticsearch, model.DataSourceTypeOpenSearch, model.DataSourceTypePrometheus,
		model.DataSourceTypeTiDB, model.DataSourceTypeRedis, model.DataSourceTypeNacos, model.DataSourceTypeNginx,
		model.DataSourceTypeKubernetes, model.DataSourceTypeLinuxServer, model.DataSourceTypeLinuxGroup:
		return true
	default:
		return false
	}
}

func classifyTopologyNode(node topologyNodeReference) string {
	text := strings.ToLower(strings.Join([]string{node.Kind, node.Name, node.SourceType}, " "))
	switch {
	case containsAnyTopology(text, "database", "db", "mysql", "postgres", "tidb", "tikv"):
		return "database"
	case containsAnyTopology(text, "redis", "nacos", "nginx", "kafka", "rabbitmq", "middleware", "cache"):
		return "middleware"
	case containsAnyTopology(text, "host", "node", "pod", "deployment", "ingress", "k8s", "kubernetes", "linux", "process"):
		return "infrastructure"
	default:
		return "downstream_service"
	}
}

func containsAnyTopology(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}

func toTopologyNodeReference(node model.TopologyNode) topologyNodeReference {
	result := topologyNodeReference{
		NodeKey: node.NodeKey, Name: node.Name, DisplayName: stringFromPointer(node.DisplayName),
		Kind: node.Kind, HostID: integerFromJSON(node.SourceRef, "hostId"),
		Environment: stringFromPointer(node.Environment), Cluster: stringFromPointer(node.Cluster),
		Namespace: stringFromPointer(node.Namespace), SourceType: node.SourceType,
	}
	if !node.UpdatedAt.IsZero() {
		result.UpdatedAt = node.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	return result
}

func toTopologyEdgeReference(edge model.TopologyEdge) topologyEdgeReference {
	confidence := 0.5
	if edge.ResolvedConfidence != nil {
		confidence = *edge.ResolvedConfidence
	} else if edge.Confidence != nil {
		confidence = *edge.Confidence
	}
	result := topologyEdgeReference{
		EdgeKey: edge.EdgeKey, FromNodeKey: edge.FromNodeKey, ToNodeKey: edge.ToNodeKey,
		EdgeType: edge.EdgeType, Confidence: confidence, Status: edge.Status,
	}
	if edge.LastObservedAt != nil {
		result.LastObservedAt = edge.LastObservedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	if edge.StaleAt != nil {
		result.StaleAt = edge.StaleAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	if !edge.UpdatedAt.IsZero() {
		result.UpdatedAt = edge.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	return result
}

func normalizeTopologyLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, value)
}

func normalizeTopologyEnvironment(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "production", "prd", "生产", "生产环境":
		return "prod"
	case "stage", "预发", "预发环境":
		return "staging"
	case "qa", "测试", "测试环境":
		return "test"
	case "development", "开发", "开发环境":
		return "dev"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func stringFromPointer(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func clamp(value, fallback, maximum int) int {
	if value <= 0 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func appendUniqueStrings(values []string, additions ...string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values)+len(additions))
	for _, value := range append(values, additions...) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func uniqueInt64s(values []int64) []int64 {
	seen := map[int64]struct{}{}
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func nodeEvidenceReferences(nodes []topologyNodeReference) []string {
	result := make([]string, 0, len(nodes))
	for _, node := range nodes {
		result = append(result, "topology-node:"+node.NodeKey)
	}
	return result
}

func pathEvidenceReferences(paths []topologysvc.TopologyPath) []string {
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		result = append(result, path.EvidenceKey)
	}
	return appendUniqueStrings(nil, result...)
}

func topologyPermissionOutput(environment string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{
		"partial": false, "forbidden": true, "environment": environment,
		"missingEvidence":    []string{"environment is outside the actor's accessible data source scope"},
		"evidenceReferences": []string{},
	})
	return raw
}
