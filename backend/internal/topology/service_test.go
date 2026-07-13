package topology

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	k8ssvc "aiops-platform/backend/internal/k8s"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestUpsertManualTopologyGraph(t *testing.T) {
	repo := newMemoryTopologyRepository()
	service := NewService(repo, nil)

	if _, err := service.UpsertNode(context.Background(), NodeInput{Kind: "service", Name: "payment-api"}); err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	if _, err := service.UpsertNode(context.Background(), NodeInput{Kind: "database", Name: "payment-db"}); err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	if _, err := service.UpsertEdge(context.Background(), EdgeInput{
		FromNodeKey: "service:payment-api",
		ToNodeKey:   "database:payment-db",
		EdgeType:    model.TopologyEdgeTypeDependsOn,
	}); err != nil {
		t.Fatalf("upsert edge: %v", err)
	}

	graph, err := service.Graph(context.Background(), Query{})
	if err != nil {
		t.Fatalf("query graph: %v", err)
	}
	if len(graph.Nodes) != 2 || len(graph.Edges) != 1 {
		t.Fatalf("unexpected graph: %+v", graph)
	}
}

func TestTopologyTypeCatalogValidatesEnabledTypes(t *testing.T) {
	repo := newMemoryTopologyRepository()
	service := NewService(repo, nil)
	nodeType := repo.nodeTypes["service"]
	nodeType.Enabled = false
	repo.nodeTypes["service"] = nodeType

	_, err := service.UpsertNode(context.Background(), NodeInput{Kind: "service", Name: "payment-api"})
	if !errors.Is(err, ErrTopologyTypeDisabled) {
		t.Fatalf("expected disabled node type error, got %v", err)
	}
}

func TestTopologyRelationTypeRejectsInvalidSourceTarget(t *testing.T) {
	repo := newMemoryTopologyRepository()
	service := NewService(repo, nil)
	_, err := service.CreateRelationType(context.Background(), RelationTypeInput{
		TypeKey:            "service_to_database",
		DisplayName:        "Service To Database",
		Semantics:          model.TopologyRelationSemanticsHardDep,
		FailurePropagation: model.TopologyFailurePropagationDstToSrc,
		DefaultDirection:   "downstream",
		AllowedSourceTypes: json.RawMessage(`["service"]`),
		AllowedTargetTypes: json.RawMessage(`["database"]`),
	})
	if err != nil {
		t.Fatalf("create relation type: %v", err)
	}
	paymentAPI, err := service.UpsertNode(context.Background(), NodeInput{Kind: "service", Name: "payment-api"})
	if err != nil {
		t.Fatalf("upsert service node: %v", err)
	}
	paymentWorker, err := service.UpsertNode(context.Background(), NodeInput{Kind: "service", Name: "payment-worker"})
	if err != nil {
		t.Fatalf("upsert worker node: %v", err)
	}
	_, err = service.UpsertEdge(context.Background(), EdgeInput{
		FromNodeKey: paymentAPI.NodeKey,
		ToNodeKey:   paymentWorker.NodeKey,
		EdgeType:    "service_to_database",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid source/target combination, got %v", err)
	}
}

func TestUpdateRelationTypeAuditsPropagationChange(t *testing.T) {
	repo := newMemoryTopologyRepository()
	service := NewService(repo, nil)
	relation, err := service.CreateRelationType(context.Background(), RelationTypeInput{
		TypeKey:            "custom_depends_on",
		DisplayName:        "Custom Depends On",
		Semantics:          model.TopologyRelationSemanticsHardDep,
		FailurePropagation: model.TopologyFailurePropagationDstToSrc,
		DefaultDirection:   "downstream",
	})
	if err != nil {
		t.Fatalf("create relation type: %v", err)
	}
	actorID := int64(42)
	if _, err := service.UpdateRelationType(context.Background(), relation.ID, &actorID, RelationTypeInput{
		TypeKey:            "custom_depends_on",
		DisplayName:        "Custom Depends On",
		Semantics:          model.TopologyRelationSemanticsRuntimeDep,
		FailurePropagation: model.TopologyFailurePropagationBoth,
		DefaultDirection:   "both",
	}); err != nil {
		t.Fatalf("update relation type: %v", err)
	}
	if len(repo.audits) != 1 || repo.audits[0].ActorID == nil || *repo.audits[0].ActorID != actorID {
		t.Fatalf("expected propagation audit, got %+v", repo.audits)
	}
}

func TestEdgeResolverFusesMultiSourceConfidenceWithoutDuplicateEdge(t *testing.T) {
	repo := newMemoryTopologyRepository()
	service := NewService(repo, nil)
	if _, err := service.UpsertNode(context.Background(), NodeInput{NodeKey: "service:payment-api", Kind: "service", Name: "payment-api"}); err != nil {
		t.Fatalf("upsert service node: %v", err)
	}
	if _, err := service.UpsertNode(context.Background(), NodeInput{NodeKey: "database:payment-db", Kind: "database", Name: "payment-db"}); err != nil {
		t.Fatalf("upsert database node: %v", err)
	}

	edge, err := service.UpsertEdge(context.Background(), EdgeInput{
		FromNodeKey:     "service:payment-api",
		ToNodeKey:       "database:payment-db",
		EdgeType:        model.TopologyEdgeTypeDependsOn,
		SourceType:      model.TopologySourceTypeCMDB,
		SourceRecordKey: ptr("cmdb-rel-1"),
		Confidence:      ptr(0.5),
	})
	if err != nil {
		t.Fatalf("upsert cmdb edge: %v", err)
	}
	edge, err = service.UpsertEdge(context.Background(), EdgeInput{
		FromNodeKey:     "service:payment-api",
		ToNodeKey:       "database:payment-db",
		EdgeType:        model.TopologyEdgeTypeDependsOn,
		SourceType:      model.TopologySourceTypeTraceServiceGraph,
		SourceRecordKey: ptr("trace-rel-1"),
		Confidence:      ptr(0.8),
	})
	if err != nil {
		t.Fatalf("upsert trace edge: %v", err)
	}

	if len(repo.edges) != 1 {
		t.Fatalf("expected one deduplicated edge, got %d", len(repo.edges))
	}
	observations, err := repo.ListTopologyEdgeObservations(context.Background(), edge.ID)
	if err != nil {
		t.Fatalf("list observations: %v", err)
	}
	if len(observations) != 2 {
		t.Fatalf("expected two source observations, got %+v", observations)
	}
	if edge.ResolvedConfidence == nil || math.Abs(*edge.ResolvedConfidence-0.9) > 0.0001 {
		t.Fatalf("expected fused confidence 0.9, got %+v", edge.ResolvedConfidence)
	}
}

func TestObservationRelationDoesNotPropagateFailure(t *testing.T) {
	repo := newMemoryTopologyRepository()
	service := NewService(repo, nil)

	relation, err := service.CreateRelationType(context.Background(), RelationTypeInput{
		TypeKey:            "observed_with",
		DisplayName:        "Observed With",
		Semantics:          model.TopologyRelationSemanticsObservation,
		FailurePropagation: model.TopologyFailurePropagationNone,
		DefaultDirection:   "downstream",
	})
	if err != nil {
		t.Fatalf("create observation relation: %v", err)
	}
	if relation.PropagatesFailure || relation.FailurePropagation != model.TopologyFailurePropagationNone {
		t.Fatalf("observation relation should not propagate failure: %+v", relation)
	}
}

func TestEdgeResolverMarksExpiredTraceStaleAndKeepsManualActive(t *testing.T) {
	repo := newMemoryTopologyRepository()
	service := NewService(repo, nil)
	if _, err := service.UpsertNode(context.Background(), NodeInput{NodeKey: "service:api", Kind: "service", Name: "api"}); err != nil {
		t.Fatalf("upsert service node: %v", err)
	}
	if _, err := service.UpsertNode(context.Background(), NodeInput{NodeKey: "database:db", Kind: "database", Name: "db"}); err != nil {
		t.Fatalf("upsert database node: %v", err)
	}

	expiredAt := time.Now().Add(-time.Hour)
	edge, err := service.UpsertEdge(context.Background(), EdgeInput{
		FromNodeKey:     "service:api",
		ToNodeKey:       "database:db",
		EdgeType:        model.TopologyEdgeTypeDependsOn,
		SourceType:      model.TopologySourceTypeTraceServiceGraph,
		SourceRecordKey: ptr("trace-expired"),
		Confidence:      ptr(0.6),
		ExpiresAt:       &expiredAt,
	})
	if err != nil {
		t.Fatalf("upsert expired trace edge: %v", err)
	}
	if edge.Status != edgeStatusStale {
		t.Fatalf("expected expired trace edge to be stale, got %+v", edge)
	}

	edge, err = service.UpsertEdge(context.Background(), EdgeInput{
		FromNodeKey:     "service:api",
		ToNodeKey:       "database:db",
		EdgeType:        model.TopologyEdgeTypeDependsOn,
		SourceType:      model.TopologySourceManual,
		SourceRecordKey: ptr("manual-confirmed"),
		Confidence:      ptr(1.0),
		ExpiresAt:       &expiredAt,
	})
	if err != nil {
		t.Fatalf("upsert manual edge: %v", err)
	}
	if edge.Status != edgeStatusActive {
		t.Fatalf("expected manual edge to stay active, got %+v", edge)
	}
}

func TestCreateSourceConfigValidatesDataSourceAndSchedule(t *testing.T) {
	repo := newMemoryTopologyRepository()
	repo.dataSources[1] = model.DataSource{ID: 1, SourceType: model.DataSourceTypeKubernetes, Enabled: true}
	service := NewService(repo, nil)

	source, err := service.CreateSourceConfig(context.Background(), SourceConfigInput{
		Name:         "prod-k8s-topology",
		SourceType:   model.TopologySourceTypeKubernetes,
		DataSourceID: ptr(int64(1)),
		Schedule:     ptr("*/5 * * * *"),
		Scope:        json.RawMessage(`{"environment":"prod","allowedNamespaces":["pay"]}`),
	})
	if err != nil {
		t.Fatalf("create source config: %v", err)
	}
	if source.Priority != 80 || source.StaleAfterSeconds != 900 || source.DeleteAfterSeconds != 604800 {
		t.Fatalf("unexpected source defaults: %+v", source)
	}
	result, err := service.TestSourceConfig(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("test source config: %v", err)
	}
	if !result.OK || result.SourceType != model.TopologySourceTypeKubernetes {
		t.Fatalf("unexpected test result: %+v", result)
	}
}

func TestCreateSourceConfigRejectsInvalidDataSourceType(t *testing.T) {
	repo := newMemoryTopologyRepository()
	repo.dataSources[1] = model.DataSource{ID: 1, SourceType: model.DataSourceTypeRedis, Enabled: true}
	service := NewService(repo, nil)

	_, err := service.CreateSourceConfig(context.Background(), SourceConfigInput{
		Name:         "bad-k8s-topology",
		SourceType:   model.TopologySourceTypeKubernetes,
		DataSourceID: ptr(int64(1)),
		Schedule:     ptr("*/5 * * * *"),
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected incompatible data source error, got %v", err)
	}
}

func TestCreateSourceConfigRejectsSensitiveFields(t *testing.T) {
	repo := newMemoryTopologyRepository()
	service := NewService(repo, nil)

	_, err := service.CreateSourceConfig(context.Background(), SourceConfigInput{
		Name:       "manual-secret",
		SourceType: model.TopologySourceTypeManual,
		Scope:      json.RawMessage(`{"token":"should-not-be-here"}`),
	})
	if !errors.Is(err, ErrSensitiveConfig) {
		t.Fatalf("expected sensitive config error, got %v", err)
	}
}

func TestCreateSourceConfigRejectsUnsupportedSourceAndBadSchedule(t *testing.T) {
	repo := newMemoryTopologyRepository()
	service := NewService(repo, nil)

	_, err := service.CreateSourceConfig(context.Background(), SourceConfigInput{
		Name:       "unsupported",
		SourceType: "not_real",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid source type, got %v", err)
	}
	_, err = service.CreateSourceConfig(context.Background(), SourceConfigInput{
		Name:       "bad-schedule",
		SourceType: model.TopologySourceTypeManual,
		Schedule:   ptr("@reboot"),
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid schedule, got %v", err)
	}
}

func TestPreviewMappingBuildsNodesAndEdgesWithoutWriting(t *testing.T) {
	repo := newMemoryTopologyRepository()
	service := NewService(repo, nil)
	source, err := service.CreateSourceConfig(context.Background(), SourceConfigInput{
		Name:       "cmdb-topology",
		SourceType: model.TopologySourceTypeManual,
		MappingRules: json.RawMessage(`{
			"nodeMappings": [
				{
					"name": "app-node",
					"entityPath": "$.data.apps[*]",
					"targetNodeType": "service",
					"externalKeyTemplate": "prod:service:cmdb:{{id}}",
					"nameTemplate": "{{name}}",
					"attributes": {"owner": "{{owner}}"},
					"aliases": ["{{shortName}}"]
				}
			],
			"edgeMappings": [
				{
					"name": "app-db",
					"entityPath": "$.data.dependencies[*]",
					"sourceLookup": {"nodeType": "service", "externalKeyTemplate": "prod:service:cmdb:{{sourceId}}"},
					"targetLookup": {"nodeType": "database", "externalKeyTemplate": "prod:database:cmdb:{{targetId}}"},
					"relationType": "depends_on",
					"confidence": 0.95
				}
			]
		}`),
	})
	if err != nil {
		t.Fatalf("create source config: %v", err)
	}
	result, err := service.PreviewSourceMapping(context.Background(), source.ID, MappingPreviewInput{
		SampleData: json.RawMessage(`{
			"data": {
				"apps": [{"id": "payment-api", "name": "payment-api", "shortName": "pay-api", "owner": "ops"}],
				"dependencies": [{"sourceId": "payment-api", "targetId": "pay-db"}]
			}
		}`),
	})
	if err != nil {
		t.Fatalf("preview mapping: %v", err)
	}
	if len(result.Nodes) != 1 || result.Nodes[0].NodeKey != "prod:service:cmdb:payment-api" || result.Nodes[0].Aliases[0] != "pay-api" {
		t.Fatalf("unexpected preview nodes: %+v", result)
	}
	if len(result.Edges) != 1 || result.Edges[0].ToNodeKey != "prod:database:cmdb:pay-db" {
		t.Fatalf("unexpected preview edges: %+v", result)
	}
	if len(repo.nodes) != 0 || len(repo.edges) != 0 {
		t.Fatalf("preview should not write graph, nodes=%+v edges=%+v", repo.nodes, repo.edges)
	}
}

func TestPreviewMappingReportsUnresolvedAndRejectsSensitiveFields(t *testing.T) {
	repo := newMemoryTopologyRepository()
	service := NewService(repo, nil)
	result, err := service.PreviewMapping(context.Background(), MappingPreviewInput{
		MappingRules: json.RawMessage(`{
			"nodeMappings": [{
				"name": "app-node",
				"entityPath": "$.items[*]",
				"targetNodeType": "service",
				"externalKeyTemplate": "prod:service:{{missing}}",
				"nameTemplate": "{{name}}"
			}]
		}`),
		SampleData: json.RawMessage(`{"items":[{"name":"payment-api"}]}`),
	})
	if err != nil {
		t.Fatalf("preview mapping: %v", err)
	}
	if len(result.Unresolved) == 0 || len(result.Nodes) != 0 {
		t.Fatalf("expected unresolved preview, got %+v", result)
	}
	_, err = service.PreviewMapping(context.Background(), MappingPreviewInput{
		MappingRules: json.RawMessage(`{
			"nodeMappings": [{
				"name": "bad-node",
				"entityPath": "$.items[*]",
				"targetNodeType": "service",
				"externalKeyTemplate": "prod:service:{{id}}",
				"nameTemplate": "{{name}}",
				"attributes": {"apiSecret": "{{secret}}"}
			}]
		}`),
		SampleData: json.RawMessage(`{"items":[{"id":"a","name":"a","secret":"x"}]}`),
	})
	if !errors.Is(err, ErrSensitiveConfig) {
		t.Fatalf("expected sensitive config error, got %v", err)
	}
}

func TestFindNodeUsesAliasAndReportsAmbiguity(t *testing.T) {
	repo := newMemoryTopologyRepository()
	service := NewService(repo, nil)
	prod, err := service.UpsertNode(context.Background(), NodeInput{NodeKey: "prod:service:cmdb:payment-api", Kind: "service", Name: "payment-api", Environment: "prod"})
	if err != nil {
		t.Fatalf("upsert prod node: %v", err)
	}
	if _, err := service.UpsertNode(context.Background(), NodeInput{NodeKey: "test:service:cmdb:payment-api", Kind: "service", Name: "payment-api", Environment: "test"}); err != nil {
		t.Fatalf("upsert test node: %v", err)
	}
	if _, err := service.AddNodeAlias(context.Background(), prod.ID, AliasInput{Alias: "pay-api", Environment: "prod"}); err != nil {
		t.Fatalf("add alias: %v", err)
	}
	aliasResult, err := service.FindNode(context.Background(), FindNodeInput{Query: "pay-api", Environment: "prod"})
	if err != nil {
		t.Fatalf("find alias: %v", err)
	}
	if !aliasResult.Matched || aliasResult.Node.NodeKey != "prod:service:cmdb:payment-api" {
		t.Fatalf("unexpected alias result: %+v", aliasResult)
	}
	ambiguous, err := service.FindNode(context.Background(), FindNodeInput{Query: "payment-api"})
	if err != nil {
		t.Fatalf("find ambiguous: %v", err)
	}
	if !ambiguous.Ambiguous || len(ambiguous.Candidates) != 2 {
		t.Fatalf("expected ambiguous candidates, got %+v", ambiguous)
	}
}

func TestNodeMergeProtectsLockedFieldsAndRecordsConflict(t *testing.T) {
	repo := newMemoryTopologyRepository()
	service := NewService(repo, nil)
	created, err := service.UpsertNode(context.Background(), NodeInput{
		NodeKey:        "prod:service:cmdb:payment-api",
		Kind:           "service",
		Name:           "payment-api",
		Environment:    "prod",
		SourceType:     model.TopologySourceTypeManual,
		SourcePriority: 100,
		LockedFields:   json.RawMessage(`["name"]`),
	})
	if err != nil {
		t.Fatalf("upsert locked node: %v", err)
	}
	updated, err := service.UpsertNode(context.Background(), NodeInput{
		NodeKey:        created.NodeKey,
		Kind:           "service",
		Name:           "payment-api-renamed",
		Environment:    "prod",
		SourceType:     model.TopologySourceTypeTraceServiceGraph,
		SourcePriority: 70,
	})
	if err != nil {
		t.Fatalf("upsert lower priority node: %v", err)
	}
	if updated.Name != "payment-api" {
		t.Fatalf("locked name should be preserved, got %+v", updated)
	}
	if len(repo.conflicts) != 0 {
		t.Fatalf("locked field should suppress conflict, got %+v", repo.conflicts)
	}
	repo.nodes[created.NodeKey] = model.TopologyNode{
		ID:             created.ID,
		NodeKey:        created.NodeKey,
		Kind:           "service",
		Name:           "payment-api",
		SourceType:     model.TopologySourceTypeCMDB,
		SourcePriority: 90,
	}
	if _, err := service.UpsertNode(context.Background(), NodeInput{
		NodeKey:        created.NodeKey,
		Kind:           "service",
		Name:           "payment-api-from-trace",
		SourceType:     model.TopologySourceTypeTraceServiceGraph,
		SourcePriority: 70,
	}); err != nil {
		t.Fatalf("upsert conflicting node: %v", err)
	}
	if len(repo.conflicts) != 1 {
		t.Fatalf("expected one conflict, got %+v", repo.conflicts)
	}
}

func TestSyncK8sGeneratesDeploymentPodServiceIngressRelations(t *testing.T) {
	repo := newMemoryTopologyRepository()
	reader := fakeK8sReader{resources: map[string][]k8ssvc.ResourceItem{
		"namespaces":             {rawK8sItem(t, "Namespace", namespaceFixture())},
		"nodes":                  {rawK8sItem(t, "Node", nodeFixture())},
		"deployments":            {rawK8sItem(t, "Deployment", deploymentFixture())},
		"pods":                   {rawK8sItem(t, "Pod", podFixture())},
		"services":               {rawK8sItem(t, "Service", serviceFixture())},
		"ingresses":              {rawK8sItem(t, "Ingress", ingressFixture())},
		"endpoints":              {rawK8sItem(t, "Endpoints", endpointFixture())},
		"persistentvolumeclaims": {rawK8sItem(t, "PersistentVolumeClaim", pvcFixture())},
	}}
	service := NewService(repo, reader)

	result, err := service.SyncK8s(context.Background(), &model.AppUser{ID: 1}, SyncK8sInput{
		DataSourceID: 1,
		Environment:  "prod",
		Cluster:      "prod-a",
		Namespace:    "payment",
	})
	if err != nil {
		t.Fatalf("sync k8s topology: %v", err)
	}
	if result.Nodes != 9 {
		t.Fatalf("expected cluster/namespace/workload/pod/node/service/endpoint/ingress/pvc nodes, got %+v", result)
	}
	for _, edgeType := range []string{
		model.TopologyEdgeTypeOwns,
		model.TopologyEdgeTypeSelects,
		model.TopologyEdgeTypeRoutesTo,
		model.TopologyEdgeTypeDependsOn,
		model.TopologyEdgeTypeRunsOn,
		model.TopologyEdgeTypeStoresIn,
	} {
		if !repo.hasEdge(edgeType) {
			t.Fatalf("expected edge type %s, got %+v", edgeType, repo.edges)
		}
	}
}

func TestSyncK8sHonorsTopologyNamespaceScope(t *testing.T) {
	repo := newMemoryTopologyRepository()
	dataSourceID := int64(1)
	repo.sourceConfigs[1] = model.TopologySourceConfig{
		ID:           1,
		Name:         "prod-k8s",
		SourceType:   model.TopologySourceTypeKubernetes,
		DataSourceID: &dataSourceID,
		Enabled:      true,
		Scope:        []byte(`{"allowedNamespaces":["payment"]}`),
	}
	service := NewService(repo, fakeK8sReader{resources: map[string][]k8ssvc.ResourceItem{}})

	_, err := service.SyncK8s(context.Background(), &model.AppUser{ID: 1}, SyncK8sInput{
		DataSourceID: dataSourceID,
		Cluster:      "prod-a",
		Namespace:    "risk",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected namespace scope forbidden error, got %v", err)
	}
}

func TestSyncTraceServiceGraphMergesNodesAndAppliesConfidenceAndTTL(t *testing.T) {
	repo := newMemoryTopologyRepository()
	service := NewService(repo, nil)
	service.SetTraceGraphReader(fakeTraceGraphReader{graph: &TraceServiceGraph{Edges: []TraceServiceGraphEdge{{
		Source:       "payment-api",
		Target:       "orders-api",
		RequestCount: 2,
	}}}})
	if _, err := service.UpsertNode(context.Background(), NodeInput{
		NodeKey:     "k8s:prod-a:payment:k8s_service:payment-api",
		Kind:        model.TopologyNodeKindK8sService,
		Name:        "payment-api",
		Environment: "prod",
		Cluster:     "prod-a",
		Namespace:   "payment",
		SourceType:  model.TopologySourceTypeKubernetes,
	}); err != nil {
		t.Fatalf("upsert existing k8s service: %v", err)
	}

	result, err := service.SyncTraceServiceGraph(context.Background(), &model.AppUser{ID: 1}, TraceServiceGraphInput{
		DataSourceID:    1,
		Environment:     "prod",
		Cluster:         "prod-a",
		Namespace:       "payment",
		MinRequestCount: 10,
		TTLSeconds:      60,
	})
	if err != nil {
		t.Fatalf("sync trace graph: %v", err)
	}
	if result.Nodes != 1 || result.Edges != 1 {
		t.Fatalf("expected one new target node and one edge, got %+v", result)
	}
	var edge model.TopologyEdge
	for _, candidate := range repo.edges {
		edge = candidate
	}
	if edge.FromNodeKey != "k8s:prod-a:payment:k8s_service:payment-api" {
		t.Fatalf("expected trace to merge with existing k8s service, got %+v", edge)
	}
	if edge.Confidence == nil || *edge.Confidence >= 0.8 {
		t.Fatalf("expected low traffic edge to have reduced confidence, got %+v", edge.Confidence)
	}
	if edge.StaleAt == nil || !edge.StaleAt.After(time.Now()) {
		t.Fatalf("expected trace TTL to set stale_at in the future, got %+v", edge.StaleAt)
	}
}

func TestSyncTraceServiceGraphEmptyResultDoesNotDeleteExistingEdges(t *testing.T) {
	repo := newMemoryTopologyRepository()
	service := NewService(repo, nil)
	service.SetTraceGraphReader(fakeTraceGraphReader{graph: &TraceServiceGraph{Edges: []TraceServiceGraphEdge{{
		Source:       "payment-api",
		Target:       "orders-api",
		RequestCount: 20,
	}}}})
	if _, err := service.SyncTraceServiceGraph(context.Background(), &model.AppUser{ID: 1}, TraceServiceGraphInput{DataSourceID: 1}); err != nil {
		t.Fatalf("initial trace sync: %v", err)
	}
	if len(repo.edges) != 1 {
		t.Fatalf("expected one edge before empty sync, got %+v", repo.edges)
	}

	service.SetTraceGraphReader(fakeTraceGraphReader{graph: &TraceServiceGraph{}})
	result, err := service.SyncTraceServiceGraph(context.Background(), &model.AppUser{ID: 1}, TraceServiceGraphInput{DataSourceID: 1})
	if err != nil {
		t.Fatalf("empty trace sync: %v", err)
	}
	if result.Edges != 0 || len(repo.edges) != 1 {
		t.Fatalf("empty trace sync should not delete existing edges, result=%+v edges=%+v", result, repo.edges)
	}
}

func TestSyncComponentTopologyBuildsMiddlewareGraph(t *testing.T) {
	repo := newMemoryTopologyRepository()
	service := NewService(repo, nil)
	service.SetComponentTopologyReader(fakeComponentTopologyReader{facts: &ComponentTopologyFacts{
		Nodes: []ComponentTopologyNode{
			{Kind: "redis_cluster", Identity: "redis-prod", Name: "redis-prod"},
			{Kind: "redis_instance", Identity: "10.0.0.1:6379", Endpoint: "10.0.0.1:6379", Name: "redis-a"},
			{Kind: "redis_instance", Identity: "10.0.0.2:6379", Endpoint: "10.0.0.2:6379", Name: "redis-b"},
		},
		Edges: []ComponentTopologyEdge{
			{FromIdentity: "redis-prod", FromKind: "redis_cluster", ToIdentity: "10.0.0.1:6379", ToKind: "redis_instance", Relation: model.TopologyEdgeTypeMemberOf, Confidence: 0.95},
			{FromIdentity: "10.0.0.1:6379", FromKind: "redis_instance", ToIdentity: "10.0.0.2:6379", ToKind: "redis_instance", Relation: model.TopologyEdgeTypeReplicatesTo, Confidence: 0.9},
		},
	}})

	result, err := service.SyncComponentTopology(context.Background(), &model.AppUser{ID: 1}, ComponentTopologyInput{
		Component:    model.TopologySourceTypeRedis,
		DataSourceID: 1,
		Environment:  "prod",
	})
	if err != nil {
		t.Fatalf("sync redis topology: %v", err)
	}
	if result.Nodes != 3 || result.Edges != 2 {
		t.Fatalf("unexpected sync result: %+v", result)
	}
	cluster := repo.nodes["prod:redis_cluster:redis:redis-prod"]
	if cluster.NodeKey == "" || cluster.SourceType != model.TopologySourceTypeRedis {
		t.Fatalf("expected stable redis cluster identity and source, got %+v", cluster)
	}
	if !repo.hasEdge(model.TopologyEdgeTypeReplicatesTo) {
		t.Fatalf("expected redis replication edge, got %+v", repo.edges)
	}
	for _, edge := range repo.edges {
		if edge.SourceType != model.TopologySourceTypeRedis || edge.Confidence == nil || *edge.Confidence <= 0 {
			t.Fatalf("expected automatic edge source/confidence, got %+v", edge)
		}
	}
}

func TestSyncComponentTopologyMarksLogInferredEdgesAsObservation(t *testing.T) {
	repo := newMemoryTopologyRepository()
	service := NewService(repo, nil)
	service.SetComponentTopologyReader(fakeComponentTopologyReader{facts: &ComponentTopologyFacts{
		Nodes: []ComponentTopologyNode{
			{Kind: "nginx", Identity: "gw-1", Name: "gw-1"},
			{Kind: "service", Identity: "payment-api", Name: "payment-api"},
		},
		Edges: []ComponentTopologyEdge{
			{FromIdentity: "gw-1", FromKind: "nginx", ToIdentity: "payment-api", ToKind: "service", Observation: true, Properties: map[string]any{"source": "access_log"}},
		},
	}})

	if _, err := service.SyncComponentTopology(context.Background(), &model.AppUser{ID: 1}, ComponentTopologyInput{
		Component:    model.TopologySourceTypeNginx,
		DataSourceID: 1,
		Environment:  "prod",
	}); err != nil {
		t.Fatalf("sync nginx topology: %v", err)
	}
	if !repo.hasEdge(model.TopologyEdgeTypeObservedWith) {
		t.Fatalf("expected observation edge for log inferred relation, got %+v", repo.edges)
	}
	for _, edge := range repo.edges {
		if edge.EdgeType == model.TopologyEdgeTypeObservedWith && edge.ResolvedConfidence != nil && *edge.ResolvedConfidence > 0.6 {
			t.Fatalf("expected observation confidence to stay conservative, got %+v", edge)
		}
	}
}

func TestSyncComponentTopologyDefaultReaderRequiresReadOnlyDataSource(t *testing.T) {
	repo := newMemoryTopologyRepository()
	repo.dataSources[1] = model.DataSource{
		ID:         1,
		SourceType: model.DataSourceTypeRedis,
		Enabled:    true,
		ReadOnly:   false,
		Config:     []byte(`{"topology":{"nodes":[],"edges":[]}}`),
	}
	service := NewService(repo, nil)

	_, err := service.SyncComponentTopology(context.Background(), &model.AppUser{ID: 1}, ComponentTopologyInput{
		Component:    model.TopologySourceTypeRedis,
		DataSourceID: 1,
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected non-read-only data source to be rejected, got %v", err)
	}
}

func TestTopologyTraversalDetectsCycleAndHonorsHops(t *testing.T) {
	repo := newMemoryTopologyRepository()
	seedManualGraph(t, repo,
		[]string{"svc:a", "svc:b", "svc:c"},
		[][3]string{
			{"svc:a", "svc:b", model.TopologyEdgeTypeDependsOn},
			{"svc:b", "svc:c", model.TopologyEdgeTypeDependsOn},
			{"svc:c", "svc:a", model.TopologyEdgeTypeDependsOn},
		},
	)
	service := NewService(repo, nil)

	result, err := service.Downstream(context.Background(), TraversalQuery{NodeKey: "svc:a", Hops: 2, MaxNodes: 10})
	if err != nil {
		t.Fatalf("query downstream: %v", err)
	}
	if len(result.Nodes) != 3 || len(result.Edges) != 2 {
		t.Fatalf("expected 2-hop subgraph, got %+v", result)
	}
	if !result.CycleDetected {
		t.Fatalf("expected cycle detection, got %+v", result)
	}
}

func TestTopologyTraversalEnforcesMaxNodes(t *testing.T) {
	repo := newMemoryTopologyRepository()
	seedManualGraph(t, repo,
		[]string{"svc:a", "svc:b", "svc:c"},
		[][3]string{
			{"svc:a", "svc:b", model.TopologyEdgeTypeDependsOn},
			{"svc:b", "svc:c", model.TopologyEdgeTypeDependsOn},
		},
	)
	service := NewService(repo, nil)

	_, err := service.Downstream(context.Background(), TraversalQuery{NodeKey: "svc:a", Hops: 2, MaxNodes: 2})
	if !errors.Is(err, ErrNodeLimitExceeded) {
		t.Fatalf("expected max node limit error, got %v", err)
	}
}

func TestCommonDependencies(t *testing.T) {
	repo := newMemoryTopologyRepository()
	seedManualGraph(t, repo,
		[]string{"svc:a", "svc:b", "svc:shared", "svc:only-a"},
		[][3]string{
			{"svc:a", "svc:shared", model.TopologyEdgeTypeDependsOn},
			{"svc:b", "svc:shared", model.TopologyEdgeTypeDependsOn},
			{"svc:a", "svc:only-a", model.TopologyEdgeTypeDependsOn},
		},
	)
	service := NewService(repo, nil)

	result, err := service.CommonDependencies(context.Background(), CommonDependencyQuery{
		NodeKeys: []string{"svc:a", "svc:b"},
		Hops:     1,
		MaxNodes: 10,
	})
	if err != nil {
		t.Fatalf("query common dependencies: %v", err)
	}
	if len(result.CommonNodes) != 1 || result.CommonNodes[0].NodeKey != "svc:shared" {
		t.Fatalf("unexpected common dependencies: %+v", result)
	}
}

func TestUpstreamAndBlastRadius(t *testing.T) {
	repo := newMemoryTopologyRepository()
	seedManualGraph(t, repo,
		[]string{"svc:frontend", "svc:api", "svc:db"},
		[][3]string{
			{"svc:frontend", "svc:api", model.TopologyEdgeTypeDependsOn},
			{"svc:api", "svc:db", model.TopologyEdgeTypeDependsOn},
		},
	)
	service := NewService(repo, nil)

	upstream, err := service.Upstream(context.Background(), TraversalQuery{NodeKey: "svc:db", Hops: 2, MaxNodes: 10})
	if err != nil {
		t.Fatalf("query upstream: %v", err)
	}
	if len(upstream.Nodes) != 3 {
		t.Fatalf("expected frontend/api/db upstream graph, got %+v", upstream)
	}
	blastRadius, err := service.BlastRadius(context.Background(), TraversalQuery{NodeKey: "svc:frontend", Hops: 2, MaxNodes: 10})
	if err != nil {
		t.Fatalf("query blast radius: %v", err)
	}
	if blastRadius.Direction != "blast_radius" || len(blastRadius.Nodes) != 3 {
		t.Fatalf("unexpected blast radius: %+v", blastRadius)
	}
}

func rawK8sItem(t *testing.T, kind string, value any) k8ssvc.ResourceItem {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %s: %v", kind, err)
	}
	return k8ssvc.ResourceItem{Kind: kind, Raw: raw}
}

func deploymentFixture() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "payment-api", Namespace: "payment", Labels: map[string]string{"app": "payment-api"}},
		Spec:       appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "payment-api"}}},
	}
}

func namespaceFixture() *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "payment"},
		Status:     corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
	}
}

func nodeFixture() *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-a", Labels: map[string]string{"nodepool": "default"}},
		Spec:       corev1.NodeSpec{ProviderID: "kind://worker-a"},
	}
}

func podFixture() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "payment-api-0", Namespace: "payment", Labels: map[string]string{"app": "payment-api"}},
		Spec: corev1.PodSpec{
			NodeName: "worker-a",
			Volumes: []corev1.Volume{{
				Name: "data",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "payment-data"},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.10"},
	}
}

func serviceFixture() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "payment-api", Namespace: "payment"},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: map[string]string{"app": "payment-api"},
		},
	}
}

func ingressFixture() *networkingv1.Ingress {
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "payment-ingress", Namespace: "payment"},
		Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{
			Host: "payment.example",
			IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{
				Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
					Name: "payment-api",
					Port: networkingv1.ServiceBackendPort{Number: 80},
				}},
				PathType: ptr(networkingv1.PathTypePrefix),
				Path:     "/",
			}}}},
		}}},
	}
}

func endpointFixture() *corev1.Endpoints {
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{Name: "payment-api", Namespace: "payment"},
		Subsets: []corev1.EndpointSubset{{
			Addresses: []corev1.EndpointAddress{{
				IP:        "10.0.0.10",
				TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: "payment-api-0", Namespace: "payment"},
			}},
		}},
	}
}

func pvcFixture() *corev1.PersistentVolumeClaim {
	storageClass := "standard"
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "payment-data", Namespace: "payment"},
		Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: &storageClass, VolumeName: "pv-payment-data"},
		Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}
}

func ptr[T any](value T) *T {
	return &value
}

func sameStringPtr(left, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func seedManualGraph(t *testing.T, repo *memoryTopologyRepository, nodeKeys []string, edges [][3]string) {
	t.Helper()
	for _, nodeKey := range nodeKeys {
		repo.nodes[nodeKey] = model.TopologyNode{NodeKey: nodeKey, Kind: "service", Name: nodeKey, SourceType: model.TopologySourceManual}
	}
	for _, edge := range edges {
		input, err := normalizeEdge(EdgeInput{
			FromNodeKey: edge[0],
			ToNodeKey:   edge[1],
			EdgeType:    edge[2],
		})
		if err != nil {
			t.Fatalf("normalize edge: %v", err)
		}
		repo.edges[input.EdgeKey] = *input
	}
}

type fakeK8sReader struct {
	resources map[string][]k8ssvc.ResourceItem
}

func (r fakeK8sReader) Resources(_ context.Context, _ *model.AppUser, input k8ssvc.ResourceInput) (*k8ssvc.ResourceResult, error) {
	return &k8ssvc.ResourceResult{DataSourceID: input.DataSourceID, Resource: input.Resource, Namespace: input.Namespace, Items: r.resources[input.Resource]}, nil
}

type fakeTraceGraphReader struct {
	graph *TraceServiceGraph
	err   error
}

func (r fakeTraceGraphReader) ReadTraceServiceGraph(_ context.Context, _ *model.AppUser, _ TraceServiceGraphInput) (*TraceServiceGraph, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.graph, nil
}

type fakeComponentTopologyReader struct {
	facts *ComponentTopologyFacts
	err   error
}

func (r fakeComponentTopologyReader) ReadComponentTopology(_ context.Context, _ *model.AppUser, _ ComponentTopologyInput) (*ComponentTopologyFacts, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.facts, nil
}

type memoryTopologyRepository struct {
	nodes            map[string]model.TopologyNode
	edges            map[string]model.TopologyEdge
	edgeObservations map[int64][]model.TopologyEdgeObservation
	nextTypeID       int64
	nodeTypes        map[string]model.TopologyNodeType
	relationTypes    map[string]model.TopologyRelationType
	sourceConfigs    map[int64]model.TopologySourceConfig
	dataSources      map[int64]model.DataSource
	aliases          map[int64]model.TopologyNodeAlias
	conflicts        []model.TopologyConflict
	audits           []model.TopologyTypeAudit
}

func newMemoryTopologyRepository() *memoryTopologyRepository {
	repo := &memoryTopologyRepository{
		nodes:            map[string]model.TopologyNode{},
		edges:            map[string]model.TopologyEdge{},
		edgeObservations: map[int64][]model.TopologyEdgeObservation{},
		nextTypeID:       1,
		nodeTypes:        map[string]model.TopologyNodeType{},
		relationTypes:    map[string]model.TopologyRelationType{},
		sourceConfigs:    map[int64]model.TopologySourceConfig{},
		dataSources:      map[int64]model.DataSource{},
		aliases:          map[int64]model.TopologyNodeAlias{},
	}
	for _, key := range []string{
		"service",
		"database",
		model.TopologyNodeKindK8sDeployment,
		model.TopologyNodeKindK8sPod,
		model.TopologyNodeKindK8sService,
		model.TopologyNodeKindK8sIngress,
		model.TopologyNodeKindK8sEndpoint,
		model.TopologyNodeKindK8sNode,
		model.TopologyNodeKindK8sPVC,
		"application",
		"nacos",
		"nacos_service",
		"service_instance",
		"redis_cluster",
		"redis_instance",
		"tidb_cluster",
		"tidb",
		"tikv",
		"pd",
		"nginx",
	} {
		repo.seedNodeType(key)
	}
	for _, key := range []string{
		model.TopologyEdgeTypeOwns,
		model.TopologyEdgeTypeSelects,
		model.TopologyEdgeTypeRoutesTo,
		model.TopologyEdgeTypeDependsOn,
		model.TopologyEdgeTypeRunsOn,
		model.TopologyEdgeTypeStoresIn,
		model.TopologyEdgeTypeCalls,
		model.TopologyEdgeTypeMemberOf,
		model.TopologyEdgeTypeReplicatesTo,
		model.TopologyEdgeTypeConnectsTo,
		model.TopologyEdgeTypeRegisteredIn,
		model.TopologyEdgeTypeObservedWith,
		model.TopologyEdgeTypeExposes,
	} {
		repo.seedRelationType(key)
	}
	return repo
}

func (r *memoryTopologyRepository) UpsertNode(_ context.Context, node *model.TopologyNode) error {
	if node.ID == 0 {
		if existing, ok := r.nodes[node.NodeKey]; ok {
			node.ID = existing.ID
		} else {
			node.ID = r.nextID()
		}
	}
	r.nodes[node.NodeKey] = *node
	return nil
}

func (r *memoryTopologyRepository) UpsertEdge(_ context.Context, edge *model.TopologyEdge) error {
	if existing, ok := r.edges[edge.EdgeKey]; ok {
		edge.ID = existing.ID
	} else if edge.ID == 0 {
		edge.ID = r.nextID()
	}
	r.edges[edge.EdgeKey] = *edge
	return nil
}

func (r *memoryTopologyRepository) FindEdgeByKey(_ context.Context, edgeKey string) (*model.TopologyEdge, error) {
	edge, ok := r.edges[edgeKey]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &edge, nil
}

func (r *memoryTopologyRepository) UpdateTopologyEdge(_ context.Context, edge *model.TopologyEdge) error {
	if _, ok := r.edges[edge.EdgeKey]; !ok {
		return repository.ErrNotFound
	}
	r.edges[edge.EdgeKey] = *edge
	return nil
}

func (r *memoryTopologyRepository) UpsertTopologyEdgeObservation(_ context.Context, observation *model.TopologyEdgeObservation) error {
	if observation.ID == 0 {
		observation.ID = r.nextID()
	}
	existing := r.edgeObservations[observation.EdgeID]
	for index := range existing {
		if existing[index].SourceType == observation.SourceType && sameStringPtr(existing[index].SourceRecordKey, observation.SourceRecordKey) {
			observation.ID = existing[index].ID
			existing[index] = *observation
			r.edgeObservations[observation.EdgeID] = existing
			return nil
		}
	}
	r.edgeObservations[observation.EdgeID] = append(existing, *observation)
	return nil
}

func (r *memoryTopologyRepository) ListTopologyEdgeObservations(_ context.Context, edgeID int64) ([]model.TopologyEdgeObservation, error) {
	observations := append([]model.TopologyEdgeObservation{}, r.edgeObservations[edgeID]...)
	return observations, nil
}

func (r *memoryTopologyRepository) FindNodeByKey(_ context.Context, nodeKey string) (*model.TopologyNode, error) {
	node, ok := r.nodes[nodeKey]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &node, nil
}

func (r *memoryTopologyRepository) FindNodeByID(_ context.Context, id int64) (*model.TopologyNode, error) {
	for _, node := range r.nodes {
		if node.ID == id {
			return &node, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *memoryTopologyRepository) FindTopologyNodes(_ context.Context, filters repository.TopologyNodeLookupFilters) ([]model.TopologyNode, error) {
	result := []model.TopologyNode{}
	seen := map[string]struct{}{}
	for _, node := range r.nodes {
		if filters.Environment != "" && (node.Environment == nil || *node.Environment != filters.Environment) {
			continue
		}
		if len(filters.Kinds) > 0 && !stringInSlice(node.Kind, filters.Kinds) {
			continue
		}
		matched := node.NodeKey == filters.Query || node.Name == filters.Query || (node.DisplayName != nil && *node.DisplayName == filters.Query)
		if !matched {
			for _, alias := range r.aliases {
				if alias.NodeID == node.ID && alias.Alias == filters.Query {
					if filters.Environment == "" || alias.Environment == nil || *alias.Environment == filters.Environment {
						matched = true
						break
					}
				}
			}
		}
		if matched {
			if _, ok := seen[node.NodeKey]; !ok {
				result = append(result, node)
				seen[node.NodeKey] = struct{}{}
			}
		}
	}
	return result, nil
}

func (r *memoryTopologyRepository) CreateTopologyNodeAlias(_ context.Context, alias *model.TopologyNodeAlias) error {
	if alias.ID == 0 {
		alias.ID = r.nextID()
	}
	r.aliases[alias.ID] = *alias
	return nil
}

func (r *memoryTopologyRepository) DeleteTopologyNodeAlias(_ context.Context, id int64) error {
	if _, ok := r.aliases[id]; !ok {
		return repository.ErrNotFound
	}
	delete(r.aliases, id)
	return nil
}

func (r *memoryTopologyRepository) ListTopologyNodeAliases(_ context.Context, nodeID int64) ([]model.TopologyNodeAlias, error) {
	result := []model.TopologyNodeAlias{}
	for _, alias := range r.aliases {
		if alias.NodeID == nodeID {
			result = append(result, alias)
		}
	}
	return result, nil
}

func (r *memoryTopologyRepository) CreateTopologyConflict(_ context.Context, conflict *model.TopologyConflict) error {
	conflict.ID = int64(len(r.conflicts) + 1)
	r.conflicts = append(r.conflicts, *conflict)
	return nil
}

func (r *memoryTopologyRepository) ListNodes(_ context.Context, _ repository.TopologyFilters) ([]model.TopologyNode, error) {
	result := make([]model.TopologyNode, 0, len(r.nodes))
	for _, node := range r.nodes {
		result = append(result, node)
	}
	return result, nil
}

func (r *memoryTopologyRepository) ListEdges(_ context.Context, _ repository.TopologyFilters) ([]model.TopologyEdge, error) {
	result := make([]model.TopologyEdge, 0, len(r.edges))
	for _, edge := range r.edges {
		result = append(result, edge)
	}
	return result, nil
}

func (r *memoryTopologyRepository) ListTopologyNodeTypes(_ context.Context) ([]model.TopologyNodeType, error) {
	result := make([]model.TopologyNodeType, 0, len(r.nodeTypes))
	for _, item := range r.nodeTypes {
		result = append(result, item)
	}
	return result, nil
}

func (r *memoryTopologyRepository) FindTopologyNodeTypeByKey(_ context.Context, typeKey string) (*model.TopologyNodeType, error) {
	nodeType, ok := r.nodeTypes[typeKey]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &nodeType, nil
}

func (r *memoryTopologyRepository) FindTopologyNodeTypeByID(_ context.Context, id int64) (*model.TopologyNodeType, error) {
	for _, nodeType := range r.nodeTypes {
		if nodeType.ID == id {
			return &nodeType, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *memoryTopologyRepository) CreateTopologyNodeType(_ context.Context, nodeType *model.TopologyNodeType) error {
	nodeType.ID = r.nextID()
	r.nodeTypes[nodeType.TypeKey] = *nodeType
	return nil
}

func (r *memoryTopologyRepository) UpdateTopologyNodeType(_ context.Context, nodeType *model.TopologyNodeType) error {
	r.nodeTypes[nodeType.TypeKey] = *nodeType
	return nil
}

func (r *memoryTopologyRepository) ListTopologyRelationTypes(_ context.Context) ([]model.TopologyRelationType, error) {
	result := make([]model.TopologyRelationType, 0, len(r.relationTypes))
	for _, item := range r.relationTypes {
		result = append(result, item)
	}
	return result, nil
}

func (r *memoryTopologyRepository) FindTopologyRelationTypeByKey(_ context.Context, typeKey string) (*model.TopologyRelationType, error) {
	relationType, ok := r.relationTypes[typeKey]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &relationType, nil
}

func (r *memoryTopologyRepository) FindTopologyRelationTypeByID(_ context.Context, id int64) (*model.TopologyRelationType, error) {
	for _, relationType := range r.relationTypes {
		if relationType.ID == id {
			return &relationType, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *memoryTopologyRepository) CreateTopologyRelationType(_ context.Context, relationType *model.TopologyRelationType) error {
	relationType.ID = r.nextID()
	r.relationTypes[relationType.TypeKey] = *relationType
	return nil
}

func (r *memoryTopologyRepository) UpdateTopologyRelationType(_ context.Context, relationType *model.TopologyRelationType) error {
	r.relationTypes[relationType.TypeKey] = *relationType
	return nil
}

func (r *memoryTopologyRepository) CreateTopologyTypeAudit(_ context.Context, audit *model.TopologyTypeAudit) error {
	audit.ID = int64(len(r.audits) + 1)
	r.audits = append(r.audits, *audit)
	return nil
}

func (r *memoryTopologyRepository) ListTopologySourceConfigs(_ context.Context) ([]model.TopologySourceConfig, error) {
	result := make([]model.TopologySourceConfig, 0, len(r.sourceConfigs))
	for _, item := range r.sourceConfigs {
		result = append(result, item)
	}
	return result, nil
}

func (r *memoryTopologyRepository) FindTopologySourceConfigByID(_ context.Context, id int64) (*model.TopologySourceConfig, error) {
	source, ok := r.sourceConfigs[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &source, nil
}

func (r *memoryTopologyRepository) CreateTopologySourceConfig(_ context.Context, source *model.TopologySourceConfig) error {
	source.ID = r.nextID()
	r.sourceConfigs[source.ID] = *source
	return nil
}

func (r *memoryTopologyRepository) UpdateTopologySourceConfig(_ context.Context, id int64, updates repository.TopologySourceConfigUpdates) (*model.TopologySourceConfig, error) {
	source, ok := r.sourceConfigs[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	if updates.Name != nil {
		source.Name = *updates.Name
	}
	if updates.SourceType != nil {
		source.SourceType = *updates.SourceType
	}
	if updates.DataSourceIDSet {
		source.DataSourceID = updates.DataSourceID
	}
	if updates.Enabled != nil {
		source.Enabled = *updates.Enabled
	}
	if updates.Priority != nil {
		source.Priority = *updates.Priority
	}
	if updates.ScheduleSet {
		source.Schedule = updates.Schedule
	}
	if updates.ScopeSet {
		source.Scope = updates.Scope
	}
	if updates.MappingRulesSet {
		source.MappingRules = updates.MappingRules
	}
	if updates.StaleAfterSeconds != nil {
		source.StaleAfterSeconds = *updates.StaleAfterSeconds
	}
	if updates.DeleteAfterSeconds != nil {
		source.DeleteAfterSeconds = *updates.DeleteAfterSeconds
	}
	r.sourceConfigs[id] = source
	return &source, nil
}

func (r *memoryTopologyRepository) DeleteTopologySourceConfig(_ context.Context, id int64) error {
	if _, ok := r.sourceConfigs[id]; !ok {
		return repository.ErrNotFound
	}
	delete(r.sourceConfigs, id)
	return nil
}

func (r *memoryTopologyRepository) FindDataSourceByID(_ context.Context, id int64) (*model.DataSource, error) {
	dataSource, ok := r.dataSources[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &dataSource, nil
}

func (r *memoryTopologyRepository) hasEdge(edgeType string) bool {
	for _, edge := range r.edges {
		if edge.EdgeType == edgeType {
			return true
		}
	}
	return false
}

func (r *memoryTopologyRepository) seedNodeType(typeKey string) {
	r.nodeTypes[typeKey] = model.TopologyNodeType{
		ID:          r.nextID(),
		TypeKey:     typeKey,
		DisplayName: typeKey,
		Enabled:     true,
		BuiltIn:     true,
	}
}

func (r *memoryTopologyRepository) seedRelationType(typeKey string) {
	r.relationTypes[typeKey] = model.TopologyRelationType{
		ID:                 r.nextID(),
		TypeKey:            typeKey,
		DisplayName:        typeKey,
		Semantics:          model.TopologyRelationSemanticsRuntimeDep,
		FailurePropagation: model.TopologyFailurePropagationBoth,
		DefaultDirection:   "both",
		PropagatesFailure:  true,
		AllowedSourceTypes: []byte(`[]`),
		AllowedTargetTypes: []byte(`[]`),
		Style:              []byte(`{}`),
		Enabled:            true,
		BuiltIn:            true,
	}
}

func (r *memoryTopologyRepository) nextID() int64 {
	id := r.nextTypeID
	r.nextTypeID++
	return id
}
