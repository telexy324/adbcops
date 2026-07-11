package topology

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

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

func TestSyncK8sGeneratesDeploymentPodServiceIngressRelations(t *testing.T) {
	repo := newMemoryTopologyRepository()
	reader := fakeK8sReader{resources: map[string][]k8ssvc.ResourceItem{
		"deployments": {rawK8sItem(t, "Deployment", deploymentFixture())},
		"pods":        {rawK8sItem(t, "Pod", podFixture())},
		"services":    {rawK8sItem(t, "Service", serviceFixture())},
		"ingresses":   {rawK8sItem(t, "Ingress", ingressFixture())},
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
	if result.Nodes != 4 {
		t.Fatalf("expected 4 nodes, got %+v", result)
	}
	if !repo.hasEdge(model.TopologyEdgeTypeOwns) || !repo.hasEdge(model.TopologyEdgeTypeSelects) || !repo.hasEdge(model.TopologyEdgeTypeRoutesTo) || !repo.hasEdge(model.TopologyEdgeTypeDependsOn) {
		t.Fatalf("expected deployment/pod/service/ingress edges, got %+v", repo.edges)
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

func podFixture() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "payment-api-0", Namespace: "payment", Labels: map[string]string{"app": "payment-api"}},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.10"},
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

func ptr[T any](value T) *T {
	return &value
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

type memoryTopologyRepository struct {
	nodes map[string]model.TopologyNode
	edges map[string]model.TopologyEdge
}

func newMemoryTopologyRepository() *memoryTopologyRepository {
	return &memoryTopologyRepository{
		nodes: map[string]model.TopologyNode{},
		edges: map[string]model.TopologyEdge{},
	}
}

func (r *memoryTopologyRepository) UpsertNode(_ context.Context, node *model.TopologyNode) error {
	r.nodes[node.NodeKey] = *node
	return nil
}

func (r *memoryTopologyRepository) UpsertEdge(_ context.Context, edge *model.TopologyEdge) error {
	r.edges[edge.EdgeKey] = *edge
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

func (r *memoryTopologyRepository) hasEdge(edgeType string) bool {
	for _, edge := range r.edges {
		if edge.EdgeType == edgeType {
			return true
		}
	}
	return false
}
