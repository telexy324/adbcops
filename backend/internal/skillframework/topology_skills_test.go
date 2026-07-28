package skillframework

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	datasourcesvc "aiops-platform/backend/internal/datasource"
	"aiops-platform/backend/internal/model"
	topologysvc "aiops-platform/backend/internal/topology"
)

func TestTopologySkillsAreRegisteredReadOnly(t *testing.T) {
	registry, err := NewRegistry(nil, nil, TopologySkills(&fakeTopologySkillService{}, fakeTopologyDataSources{})...)
	if err != nil {
		t.Fatalf("register topology skills: %v", err)
	}
	for _, name := range []string{
		"find_topology_node",
		"expand_topology",
		"find_dependencies",
		"explain_topology_path",
		"get_topology_data_source_bindings",
	} {
		definition, err := registry.Get(name)
		if err != nil {
			t.Fatalf("get %s: %v", name, err)
		}
		if !definition.ReadOnly || definition.RiskLevel != model.SkillRiskSafeRead {
			t.Fatalf("%s must be a safe read-only skill: %+v", name, definition)
		}
	}
}

func TestTopologyNodeReferenceExposesOnlySafeLinuxHostID(t *testing.T) {
	reference := toTopologyNodeReference(model.TopologyNode{
		NodeKey: "host:9", Name: "orders-host", Kind: model.TopologyNodeKindHost,
		SourceType: model.TopologySourceTypeLinuxServer,
		SourceRef:  []byte(`{"hostId":9,"password":"must-not-leak"}`),
	})
	raw, err := json.Marshal(reference)
	if err != nil {
		t.Fatalf("marshal node reference: %v", err)
	}
	if reference.HostID != 9 || !strings.Contains(string(raw), `"hostId":9`) || strings.Contains(string(raw), "password") {
		t.Fatalf("Linux host reference is missing its safe identity or leaked source metadata: %s", raw)
	}
}

func TestFindDependenciesReturnsDirectAndSecondHopClassifications(t *testing.T) {
	prod := "prod"
	confidence := 0.9
	topology := &fakeTopologySkillService{
		expansion: &topologysvc.ExpandTopologyResult{
			RootKey: "svc:order", Direction: "downstream", Depth: 2, EvidenceKey: "topology:expand:order",
			Nodes: []model.TopologyNode{
				{NodeKey: "svc:order", Name: "order-service", Kind: "service", Environment: &prod, SourceType: "cmdb"},
				{NodeKey: "db:order", Name: "order-tidb", Kind: "database", Environment: &prod, SourceType: model.DataSourceTypeTiDB},
				{NodeKey: "cache:order", Name: "order-redis", Kind: "middleware", Environment: &prod, SourceType: model.DataSourceTypeRedis},
			},
			Edges: []model.TopologyEdge{
				{EdgeKey: "e1", FromNodeKey: "svc:order", ToNodeKey: "db:order", EdgeType: model.TopologyEdgeTypeStoresIn, Confidence: &confidence},
				{EdgeKey: "e2", FromNodeKey: "db:order", ToNodeKey: "cache:order", EdgeType: model.TopologyEdgeTypeDependsOn, Confidence: &confidence},
			},
			Paths: []topologysvc.TopologyPath{
				{TargetNodeKey: "db:order", Hops: 1, NodeKeys: []string{"svc:order", "db:order"}, EvidenceKey: "path:db"},
				{TargetNodeKey: "cache:order", Hops: 2, NodeKeys: []string{"svc:order", "db:order", "cache:order"}, EvidenceKey: "path:cache"},
			},
		},
	}
	registry := newTopologySkillRegistry(t, topology, fakeTopologyDataSources{views: []datasourcesvc.DataSourceView{
		{ID: 1, SourceType: model.DataSourceTypePrometheus, Environment: &prod, Enabled: true, ReadOnly: true},
	}})
	result, err := registry.Execute(context.Background(), ExecuteInput{
		Actor: userActor(), Name: "find_dependencies",
		Payload: json.RawMessage(`{"nodeKey":"svc:order","depth":2,"maxNodes":50,"maxEdges":100}`),
	})
	if err != nil {
		t.Fatalf("execute find_dependencies: %v", err)
	}
	var output struct {
		Dependencies []struct {
			Node struct {
				NodeKey string `json:"nodeKey"`
			} `json:"node"`
			DependencyType string `json:"dependencyType"`
			Hops           int    `json:"hops"`
		} `json:"dependencies"`
		Limits map[string]int `json:"limits"`
	}
	if err := json.Unmarshal(result.Output, &output); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if len(output.Dependencies) != 2 ||
		output.Dependencies[0].DependencyType != "database" || output.Dependencies[0].Hops != 1 ||
		output.Dependencies[1].DependencyType != "middleware" || output.Dependencies[1].Hops != 2 {
		t.Fatalf("unexpected dependency classification: %+v", output.Dependencies)
	}
	if topology.lastExpand.Depth != 2 || topology.lastExpand.MaxNodes != 50 || topology.lastExpand.MaxEdges != 100 {
		t.Fatalf("limits were not forwarded: %+v", topology.lastExpand)
	}
}

func TestTopologyBindingResolvesExplicitAndLabelSources(t *testing.T) {
	prod := "prod"
	now := time.Date(2026, 7, 28, 4, 0, 0, 0, time.UTC)
	node := model.TopologyNode{
		ID: 10, NodeKey: "svc:order", Name: "order-service", Kind: "service", Environment: &prod,
		SourceType: model.TopologySourceTypeCMDB, SourceRef: []byte(`{"dataSourceId":1}`),
		Properties: []byte(`{"cmdbId":"CMDB-ORDER-01"}`), UpdatedAt: now,
	}
	topology := &fakeTopologySkillService{
		find:    &topologysvc.FindNodeResult{Matched: true, Node: &node, Candidates: []model.TopologyNode{node}},
		aliases: []model.TopologyNodeAlias{{NodeID: 10, Alias: "订单服务"}},
	}
	registry := newTopologySkillRegistry(t, topology, fakeTopologyDataSources{views: []datasourcesvc.DataSourceView{
		{ID: 1, Name: "order-logs", SourceType: model.DataSourceTypeElasticsearch, Environment: &prod, ComponentName: stringTestPointer("order-service"), Enabled: true, ReadOnly: true, UpdatedAt: now.Format(time.RFC3339)},
		{ID: 2, Name: "order-metrics", SourceType: model.DataSourceTypePrometheus, Environment: &prod, ComponentName: stringTestPointer("order-service"), Enabled: true, ReadOnly: true, UpdatedAt: now.Format(time.RFC3339)},
	}})
	result, err := registry.Execute(context.Background(), ExecuteInput{
		Actor: userActor(), Name: "get_topology_data_source_bindings",
		Payload: json.RawMessage(`{"nodeKey":"svc:order"}`),
	})
	if err != nil {
		t.Fatalf("execute bindings: %v", err)
	}
	var output struct {
		Bindings        []topologyBinding `json:"bindings"`
		MissingEvidence []string          `json:"missingEvidence"`
	}
	if err := json.Unmarshal(result.Output, &output); err != nil {
		t.Fatalf("decode bindings: %v", err)
	}
	if len(output.Bindings) != 2 || len(output.MissingEvidence) != 0 {
		t.Fatalf("expected Elasticsearch and Prometheus bindings: %+v", output)
	}
	if output.Bindings[0].DataSourceID != 1 || output.Bindings[0].Source != "node_source_ref" ||
		output.Bindings[0].Confidence != 1 || output.Bindings[0].Environment != "prod" || output.Bindings[0].UpdatedAt == "" {
		t.Fatalf("explicit binding metadata is incomplete: %+v", output.Bindings[0])
	}
}

func TestTopologyBindingDoesNotAdoptConflictsOrLowConfidence(t *testing.T) {
	prod := "prod"
	node := model.TopologyNode{
		ID: 10, NodeKey: "svc:order", Name: "order-service", Kind: "service", Environment: &prod,
		SourceType: model.TopologySourceTypeCMDB,
	}
	topology := &fakeTopologySkillService{
		find: &topologysvc.FindNodeResult{Matched: true, Node: &node, Candidates: []model.TopologyNode{node}},
	}
	registry := newTopologySkillRegistry(t, topology, fakeTopologyDataSources{views: []datasourcesvc.DataSourceView{
		{ID: 1, Name: "logs-a", SourceType: model.DataSourceTypeElasticsearch, Environment: &prod, ComponentName: stringTestPointer("order-service"), Enabled: true, ReadOnly: true},
		{ID: 2, Name: "logs-b", SourceType: model.DataSourceTypeElasticsearch, Environment: &prod, ComponentName: stringTestPointer("order-service"), Enabled: true, ReadOnly: true},
		{ID: 3, Name: "shared-order-service-archive", SourceType: model.DataSourceTypePrometheus, Environment: &prod, Enabled: true, ReadOnly: true},
	}})
	result, err := registry.Execute(context.Background(), ExecuteInput{
		Actor: userActor(), Name: "get_topology_data_source_bindings",
		Payload: json.RawMessage(`{"nodeKey":"svc:order"}`),
	})
	if err != nil {
		t.Fatalf("execute bindings: %v", err)
	}
	var output struct {
		Bindings        []topologyBinding `json:"bindings"`
		Candidates      []topologyBinding `json:"candidates"`
		Conflicts       []string          `json:"conflicts"`
		MissingEvidence []string          `json:"missingEvidence"`
	}
	if err := json.Unmarshal(result.Output, &output); err != nil {
		t.Fatalf("decode bindings: %v", err)
	}
	if len(output.Bindings) != 0 || len(output.Conflicts) != 1 || len(output.MissingEvidence) == 0 {
		t.Fatalf("conflicts and low confidence must not be accepted: %+v", output)
	}
	statuses := map[int64]string{}
	for _, candidate := range output.Candidates {
		statuses[candidate.DataSourceID] = candidate.Status
	}
	if statuses[1] != "conflict" || statuses[2] != "conflict" || statuses[3] != "low_confidence" {
		t.Fatalf("unexpected candidate statuses: %+v", statuses)
	}
}

func TestTopologyBindingSupportsAliasCMDBAndExplicitConfiguration(t *testing.T) {
	prod := "prod"
	redisID := int64(4)
	node := model.TopologyNode{
		ID: 10, NodeKey: "svc:order", Name: "order-service", Kind: "service", Environment: &prod,
		SourceType: model.TopologySourceTypeCMDB,
		Properties: []byte(`{"cmdbId":"CMDB-ORDER-01","dataSourceIds":[7]}`),
	}
	candidates := resolveTopologyBindings(
		node,
		[]model.TopologyNodeAlias{{NodeID: 10, Alias: "订单服务"}},
		[]model.TopologySourceConfig{{
			ID: 3, SourceType: model.TopologySourceTypeRedis, DataSourceID: &redisID,
			Enabled: true, Scope: []byte(`{"nodeKeys":["svc:order"]}`),
		}},
		[]datasourcesvc.DataSourceView{
			{ID: 4, Name: "redis-prod", SourceType: model.DataSourceTypeRedis, Environment: &prod, Enabled: true, ReadOnly: true},
			{ID: 5, Name: "nacos-prod", SourceType: model.DataSourceTypeNacos, Environment: &prod, ComponentName: stringTestPointer("订单服务"), Enabled: true, ReadOnly: true},
			{ID: 6, Name: "tidb-prod", SourceType: model.DataSourceTypeTiDB, Environment: &prod, Config: json.RawMessage(`{"cmdbIds":["CMDB-ORDER-01"]}`), Enabled: true, ReadOnly: true},
			{ID: 7, Name: "k8s-prod", SourceType: model.DataSourceTypeKubernetes, Environment: &prod, Enabled: true, ReadOnly: true},
		},
		nil,
		0.8,
	)
	resolved := map[int64]topologyBinding{}
	for _, candidate := range candidates {
		if candidate.Accepted {
			resolved[candidate.DataSourceID] = candidate
		}
	}
	if len(resolved) != 4 {
		t.Fatalf("expected four independently resolved binding sources: %+v", candidates)
	}
	if resolved[4].Source != "topology_source_config" ||
		resolved[5].Source != "data_source_label" ||
		resolved[6].Source != "cmdb_identifier" ||
		resolved[7].Source != "node_explicit_config" {
		t.Fatalf("unexpected binding provenance: %+v", resolved)
	}
}

func TestTopologyBindingExcludesInaccessibleDataSourcesAndAuditsEvidence(t *testing.T) {
	prod := "prod"
	node := model.TopologyNode{
		ID: 10, NodeKey: "svc:order", Name: "order-service", Kind: "service", Environment: &prod,
		SourceType: model.TopologySourceTypeCMDB, SourceRef: []byte(`{"dataSourceIds":[1,99]}`),
	}
	topology := &fakeTopologySkillService{
		find: &topologysvc.FindNodeResult{Matched: true, Node: &node, Candidates: []model.TopologyNode{node}},
	}
	audit := newMemoryAudit()
	registry, err := NewRegistry(nil, audit, TopologySkills(topology, fakeTopologyDataSources{views: []datasourcesvc.DataSourceView{
		{ID: 1, Name: "order-logs", SourceType: model.DataSourceTypeElasticsearch, Environment: &prod, Enabled: true, ReadOnly: true},
	}})...)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	result, err := registry.Execute(context.Background(), ExecuteInput{
		Actor: userActor(), Name: "get_topology_data_source_bindings",
		Payload: json.RawMessage(`{"nodeKey":"svc:order","explicitDataSourceIds":[99]}`),
	})
	if err != nil {
		t.Fatalf("execute bindings: %v", err)
	}
	var output struct {
		Bindings           []topologyBinding `json:"bindings"`
		Candidates         []topologyBinding `json:"candidates"`
		EvidenceReferences []string          `json:"evidenceReferences"`
	}
	if err := json.Unmarshal(result.Output, &output); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if len(output.Bindings) != 1 || output.Bindings[0].DataSourceID != 1 || len(output.Candidates) != 1 {
		t.Fatalf("inaccessible data source leaked into bindings: %+v", output)
	}
	if len(output.EvidenceReferences) != 1 || output.EvidenceReferences[0] != "topology-node:svc:order" {
		t.Fatalf("missing evidence reference: %+v", output.EvidenceReferences)
	}
	runs, err := registry.ListRuns(context.Background(), 10)
	if err != nil || len(runs) != 1 || len(runs[0].InputSummary) == 0 || len(runs[0].OutputSummary) == 0 {
		t.Fatalf("skill invocation was not audited: runs=%+v err=%v", runs, err)
	}
}

func TestTopologyBindingReturnsMissingEvidenceWithoutFabrication(t *testing.T) {
	prod := "prod"
	node := model.TopologyNode{ID: 10, NodeKey: "svc:order", Name: "order-service", Environment: &prod}
	registry := newTopologySkillRegistry(t,
		&fakeTopologySkillService{find: &topologysvc.FindNodeResult{Matched: true, Node: &node, Candidates: []model.TopologyNode{node}}},
		fakeTopologyDataSources{views: []datasourcesvc.DataSourceView{
			{ID: 8, Name: "unrelated", SourceType: model.DataSourceTypePrometheus, Environment: &prod, Enabled: true, ReadOnly: true},
		}},
	)
	result, err := registry.Execute(context.Background(), ExecuteInput{
		Actor: userActor(), Name: "get_topology_data_source_bindings", Payload: json.RawMessage(`{"nodeKey":"svc:order"}`),
	})
	if err != nil {
		t.Fatalf("execute bindings: %v", err)
	}
	var output struct {
		Bindings        []topologyBinding `json:"bindings"`
		MissingEvidence []string          `json:"missingEvidence"`
	}
	if err := json.Unmarshal(result.Output, &output); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if len(output.Bindings) != 0 || len(output.MissingEvidence) == 0 {
		t.Fatalf("missing binding must be explicit and must not be fabricated: %+v", output)
	}
}

type fakeTopologySkillService struct {
	find        *topologysvc.FindNodeResult
	expansion   *topologysvc.ExpandTopologyResult
	explanation *topologysvc.ExplainPathResult
	aliases     []model.TopologyNodeAlias
	configs     []model.TopologySourceConfig
	lastExpand  topologysvc.ExpandTopologyQuery
}

func (f *fakeTopologySkillService) FindNode(context.Context, topologysvc.FindNodeInput) (*topologysvc.FindNodeResult, error) {
	if f.find == nil {
		return &topologysvc.FindNodeResult{}, nil
	}
	return f.find, nil
}

func (f *fakeTopologySkillService) ExpandTopology(_ context.Context, query topologysvc.ExpandTopologyQuery) (*topologysvc.ExpandTopologyResult, error) {
	f.lastExpand = query
	if f.expansion == nil {
		return &topologysvc.ExpandTopologyResult{RootKey: query.NodeKey, Direction: query.Direction, Depth: query.Depth}, nil
	}
	return f.expansion, nil
}

func (f *fakeTopologySkillService) ExplainPath(context.Context, topologysvc.ExplainPathQuery) (*topologysvc.ExplainPathResult, error) {
	if f.explanation == nil {
		return &topologysvc.ExplainPathResult{}, nil
	}
	return f.explanation, nil
}

func (f *fakeTopologySkillService) ListNodeAliases(context.Context, int64) ([]model.TopologyNodeAlias, error) {
	return f.aliases, nil
}

func (f *fakeTopologySkillService) ListSourceConfigs(context.Context) ([]model.TopologySourceConfig, error) {
	return f.configs, nil
}

type fakeTopologyDataSources struct {
	views []datasourcesvc.DataSourceView
}

func (f fakeTopologyDataSources) List(context.Context, *model.AppUser) ([]datasourcesvc.DataSourceView, error) {
	return f.views, nil
}

func newTopologySkillRegistry(t *testing.T, topology TopologySkillService, dataSources TopologyDataSourceLister) *Registry {
	t.Helper()
	registry, err := NewRegistry(nil, nil, TopologySkills(topology, dataSources)...)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	return registry
}

func stringTestPointer(value string) *string { return &value }
