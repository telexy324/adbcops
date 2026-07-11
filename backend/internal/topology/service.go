package topology

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	k8ssvc "aiops-platform/backend/internal/k8s"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

var (
	ErrInvalidInput = errors.New("invalid input")
	ErrForbidden    = errors.New("topology access forbidden")
)

type Repository interface {
	UpsertNode(ctx context.Context, node *model.TopologyNode) error
	UpsertEdge(ctx context.Context, edge *model.TopologyEdge) error
	ListNodes(ctx context.Context, filters repository.TopologyFilters) ([]model.TopologyNode, error)
	ListEdges(ctx context.Context, filters repository.TopologyFilters) ([]model.TopologyEdge, error)
}

type K8sReader interface {
	Resources(ctx context.Context, actor *model.AppUser, input k8ssvc.ResourceInput) (*k8ssvc.ResourceResult, error)
}

type Service struct {
	repository Repository
	k8sReader  K8sReader
}

type NodeInput struct {
	NodeKey     string          `json:"nodeKey"`
	Kind        string          `json:"kind"`
	Name        string          `json:"name"`
	DisplayName *string         `json:"displayName"`
	Environment string          `json:"environment"`
	Cluster     string          `json:"cluster"`
	Namespace   string          `json:"namespace"`
	Labels      json.RawMessage `json:"labels"`
	Properties  json.RawMessage `json:"properties"`
	SourceType  string          `json:"sourceType"`
	SourceRef   json.RawMessage `json:"sourceRef"`
}

type EdgeInput struct {
	EdgeKey     string          `json:"edgeKey"`
	FromNodeKey string          `json:"fromNodeKey"`
	ToNodeKey   string          `json:"toNodeKey"`
	EdgeType    string          `json:"edgeType"`
	Confidence  *float64        `json:"confidence"`
	Properties  json.RawMessage `json:"properties"`
	SourceType  string          `json:"sourceType"`
	SourceRef   json.RawMessage `json:"sourceRef"`
}

type Query struct {
	Environment string
	Cluster     string
	Namespace   string
	Kind        string
	Limit       int
}

type Graph struct {
	Nodes []model.TopologyNode `json:"nodes"`
	Edges []model.TopologyEdge `json:"edges"`
}

type SyncK8sInput struct {
	DataSourceID int64  `json:"dataSourceId"`
	Environment  string `json:"environment"`
	Cluster      string `json:"cluster"`
	Namespace    string `json:"namespace"`
	Limit        int    `json:"limit"`
}

type SyncResult struct {
	Nodes int `json:"nodes"`
	Edges int `json:"edges"`
}

func NewService(repository Repository, k8sReader K8sReader) *Service {
	return &Service{repository: repository, k8sReader: k8sReader}
}

func (s *Service) UpsertNode(ctx context.Context, input NodeInput) (*model.TopologyNode, error) {
	node, err := normalizeNode(input)
	if err != nil {
		return nil, err
	}
	if err := s.repository.UpsertNode(ctx, node); err != nil {
		return nil, err
	}
	return node, nil
}

func (s *Service) UpsertEdge(ctx context.Context, input EdgeInput) (*model.TopologyEdge, error) {
	edge, err := normalizeEdge(input)
	if err != nil {
		return nil, err
	}
	if err := s.repository.UpsertEdge(ctx, edge); err != nil {
		return nil, err
	}
	return edge, nil
}

func (s *Service) Graph(ctx context.Context, query Query) (*Graph, error) {
	filters := repository.TopologyFilters{
		Environment: strings.TrimSpace(query.Environment),
		Cluster:     strings.TrimSpace(query.Cluster),
		Namespace:   strings.TrimSpace(query.Namespace),
		Kind:        strings.TrimSpace(query.Kind),
		Limit:       query.Limit,
	}
	nodes, err := s.repository.ListNodes(ctx, filters)
	if err != nil {
		return nil, err
	}
	edges, err := s.repository.ListEdges(ctx, filters)
	if err != nil {
		return nil, err
	}
	return &Graph{Nodes: nodes, Edges: edges}, nil
}

func (s *Service) SyncK8s(ctx context.Context, actor *model.AppUser, input SyncK8sInput) (*SyncResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	if s.k8sReader == nil {
		return nil, ErrInvalidInput
	}
	namespace := strings.TrimSpace(input.Namespace)
	cluster := strings.TrimSpace(input.Cluster)
	if input.DataSourceID <= 0 || namespace == "" {
		return nil, ErrInvalidInput
	}
	limit := input.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}

	deployments, err := s.readK8sResource(ctx, actor, input.DataSourceID, namespace, "deployments", limit)
	if err != nil {
		return nil, err
	}
	pods, err := s.readK8sResource(ctx, actor, input.DataSourceID, namespace, "pods", limit)
	if err != nil {
		return nil, err
	}
	services, err := s.readK8sResource(ctx, actor, input.DataSourceID, namespace, "services", limit)
	if err != nil {
		return nil, err
	}
	ingresses, err := s.readK8sResource(ctx, actor, input.DataSourceID, namespace, "ingresses", limit)
	if err != nil {
		return nil, err
	}

	builder := newK8sGraphBuilder(input.Environment, cluster, namespace)
	for _, item := range deployments.Items {
		var deployment appsv1.Deployment
		if decodeK8s(item.Raw, &deployment) == nil {
			builder.addDeployment(&deployment)
		}
	}
	for _, item := range pods.Items {
		var pod corev1.Pod
		if decodeK8s(item.Raw, &pod) == nil {
			builder.addPod(&pod)
		}
	}
	for _, item := range services.Items {
		var service corev1.Service
		if decodeK8s(item.Raw, &service) == nil {
			builder.addService(&service)
		}
	}
	for _, item := range ingresses.Items {
		var ingress networkingv1.Ingress
		if decodeK8s(item.Raw, &ingress) == nil {
			builder.addIngress(&ingress)
		}
	}
	builder.linkK8sResources()

	for index := range builder.nodes {
		if err := s.repository.UpsertNode(ctx, &builder.nodes[index]); err != nil {
			return nil, err
		}
	}
	for index := range builder.edges {
		if err := s.repository.UpsertEdge(ctx, &builder.edges[index]); err != nil {
			return nil, err
		}
	}
	return &SyncResult{Nodes: len(builder.nodes), Edges: len(builder.edges)}, nil
}

func (s *Service) readK8sResource(ctx context.Context, actor *model.AppUser, dataSourceID int64, namespace, resource string, limit int) (*k8ssvc.ResourceResult, error) {
	return s.k8sReader.Resources(ctx, actor, k8ssvc.ResourceInput{
		DataSourceID: dataSourceID,
		Namespace:    namespace,
		Resource:     resource,
		Limit:        limit,
	})
}

func normalizeNode(input NodeInput) (*model.TopologyNode, error) {
	kind := strings.TrimSpace(input.Kind)
	name := strings.TrimSpace(input.Name)
	nodeKey := strings.TrimSpace(input.NodeKey)
	if kind == "" || name == "" {
		return nil, ErrInvalidInput
	}
	if nodeKey == "" {
		nodeKey = strings.ToLower(kind + ":" + name)
	}
	labelsJSON := validJSONOrEmpty(input.Labels)
	propertiesJSON := validJSONOrEmpty(input.Properties)
	sourceRef := validJSONOrEmpty(input.SourceRef)
	if labelsJSON == nil || propertiesJSON == nil || sourceRef == nil {
		return nil, ErrInvalidInput
	}
	sourceType := strings.TrimSpace(input.SourceType)
	if sourceType == "" {
		sourceType = model.TopologySourceManual
	}
	return &model.TopologyNode{
		NodeKey:     nodeKey,
		Kind:        kind,
		Name:        name,
		DisplayName: cleanStringPtr(input.DisplayName),
		Environment: cleanString(input.Environment),
		Cluster:     cleanString(input.Cluster),
		Namespace:   cleanString(input.Namespace),
		Labels:      labelsJSON,
		Properties:  propertiesJSON,
		SourceType:  sourceType,
		SourceRef:   sourceRef,
	}, nil
}

func normalizeEdge(input EdgeInput) (*model.TopologyEdge, error) {
	from := strings.TrimSpace(input.FromNodeKey)
	to := strings.TrimSpace(input.ToNodeKey)
	edgeType := strings.TrimSpace(input.EdgeType)
	if from == "" || to == "" || edgeType == "" {
		return nil, ErrInvalidInput
	}
	if input.Confidence != nil && (*input.Confidence < 0 || *input.Confidence > 1) {
		return nil, ErrInvalidInput
	}
	edgeKey := strings.TrimSpace(input.EdgeKey)
	if edgeKey == "" {
		edgeKey = edgeKeyFor(from, to, edgeType)
	}
	propertiesJSON := validJSONOrEmpty(input.Properties)
	sourceRef := validJSONOrEmpty(input.SourceRef)
	if propertiesJSON == nil || sourceRef == nil {
		return nil, ErrInvalidInput
	}
	sourceType := strings.TrimSpace(input.SourceType)
	if sourceType == "" {
		sourceType = model.TopologySourceManual
	}
	return &model.TopologyEdge{
		EdgeKey:     edgeKey,
		FromNodeKey: from,
		ToNodeKey:   to,
		EdgeType:    edgeType,
		Confidence:  input.Confidence,
		Properties:  propertiesJSON,
		SourceType:  sourceType,
		SourceRef:   sourceRef,
	}, nil
}

func validJSONOrEmpty(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte(`{}`)
	}
	if !json.Valid(raw) {
		return nil
	}
	return raw
}

func cleanString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func cleanStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	return cleanString(*value)
}

func edgeKeyFor(from, to, edgeType string) string {
	return strings.ToLower(from + "->" + to + ":" + edgeType)
}

func decodeK8s(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		return ErrInvalidInput
	}
	return json.Unmarshal(raw, target)
}

type k8sGraphBuilder struct {
	environment string
	cluster     string
	namespace   string
	nodes       []model.TopologyNode
	edges       []model.TopologyEdge
	deployments []appsv1.Deployment
	pods        []corev1.Pod
	services    []corev1.Service
}

func newK8sGraphBuilder(environment, cluster, namespace string) *k8sGraphBuilder {
	return &k8sGraphBuilder{
		environment: strings.TrimSpace(environment),
		cluster:     strings.TrimSpace(cluster),
		namespace:   strings.TrimSpace(namespace),
	}
}

func (b *k8sGraphBuilder) addDeployment(deployment *appsv1.Deployment) {
	b.deployments = append(b.deployments, *deployment)
	b.nodes = append(b.nodes, b.node(model.TopologyNodeKindK8sDeployment, deployment.Name, deployment.Labels, map[string]any{
		"replicas":      deployment.Status.Replicas,
		"readyReplicas": deployment.Status.ReadyReplicas,
	}, deployment))
}

func (b *k8sGraphBuilder) addPod(pod *corev1.Pod) {
	b.pods = append(b.pods, *pod)
	b.nodes = append(b.nodes, b.node(model.TopologyNodeKindK8sPod, pod.Name, pod.Labels, map[string]any{
		"phase":    string(pod.Status.Phase),
		"podIp":    pod.Status.PodIP,
		"nodeName": pod.Spec.NodeName,
	}, pod))
}

func (b *k8sGraphBuilder) addService(service *corev1.Service) {
	b.services = append(b.services, *service)
	b.nodes = append(b.nodes, b.node(model.TopologyNodeKindK8sService, service.Name, service.Labels, map[string]any{
		"type":      string(service.Spec.Type),
		"clusterIp": service.Spec.ClusterIP,
	}, service))
}

func (b *k8sGraphBuilder) addIngress(ingress *networkingv1.Ingress) {
	b.nodes = append(b.nodes, b.node(model.TopologyNodeKindK8sIngress, ingress.Name, ingress.Labels, map[string]any{
		"class": ingressClass(ingress),
		"hosts": ingressHosts(ingress),
	}, ingress))
	for _, serviceName := range ingressServiceBackends(ingress) {
		b.edges = append(b.edges, b.edge(
			k8sNodeKey(b.cluster, ingress.Namespace, model.TopologyNodeKindK8sIngress, ingress.Name),
			k8sNodeKey(b.cluster, ingress.Namespace, model.TopologyNodeKindK8sService, serviceName),
			model.TopologyEdgeTypeRoutesTo,
			ingress,
		))
	}
}

func (b *k8sGraphBuilder) linkK8sResources() {
	for dIndex := range b.deployments {
		deployment := &b.deployments[dIndex]
		selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
		if err != nil || selector.Empty() {
			continue
		}
		for pIndex := range b.pods {
			pod := &b.pods[pIndex]
			if pod.Namespace == deployment.Namespace && selector.Matches(labels.Set(pod.Labels)) {
				b.edges = append(b.edges, b.edge(
					k8sNodeKey(b.cluster, deployment.Namespace, model.TopologyNodeKindK8sDeployment, deployment.Name),
					k8sNodeKey(b.cluster, pod.Namespace, model.TopologyNodeKindK8sPod, pod.Name),
					model.TopologyEdgeTypeOwns,
					deployment,
				))
			}
		}
	}
	for sIndex := range b.services {
		service := &b.services[sIndex]
		if len(service.Spec.Selector) == 0 {
			continue
		}
		selector := labels.SelectorFromSet(labels.Set(service.Spec.Selector))
		for pIndex := range b.pods {
			pod := &b.pods[pIndex]
			if pod.Namespace == service.Namespace && selector.Matches(labels.Set(pod.Labels)) {
				b.edges = append(b.edges, b.edge(
					k8sNodeKey(b.cluster, service.Namespace, model.TopologyNodeKindK8sService, service.Name),
					k8sNodeKey(b.cluster, pod.Namespace, model.TopologyNodeKindK8sPod, pod.Name),
					model.TopologyEdgeTypeSelects,
					service,
				))
			}
		}
		for dIndex := range b.deployments {
			deployment := &b.deployments[dIndex]
			deploymentSelector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
			if err != nil || deploymentSelector.Empty() {
				continue
			}
			if deployment.Namespace == service.Namespace && selectorsOverlap(selector, deploymentSelector) {
				b.edges = append(b.edges, b.edge(
					k8sNodeKey(b.cluster, service.Namespace, model.TopologyNodeKindK8sService, service.Name),
					k8sNodeKey(b.cluster, deployment.Namespace, model.TopologyNodeKindK8sDeployment, deployment.Name),
					model.TopologyEdgeTypeDependsOn,
					service,
				))
			}
		}
	}
}

func (b *k8sGraphBuilder) node(kind, name string, labelMap map[string]string, properties map[string]any, object metav1.Object) model.TopologyNode {
	labelsJSON, _ := json.Marshal(labelMap)
	propertiesJSON, _ := json.Marshal(properties)
	sourceRef, _ := json.Marshal(map[string]any{
		"uid":             string(object.GetUID()),
		"resourceVersion": object.GetResourceVersion(),
	})
	return model.TopologyNode{
		NodeKey:     k8sNodeKey(b.cluster, object.GetNamespace(), kind, name),
		Kind:        kind,
		Name:        name,
		Environment: cleanString(b.environment),
		Cluster:     cleanString(b.cluster),
		Namespace:   cleanString(object.GetNamespace()),
		Labels:      labelsJSON,
		Properties:  propertiesJSON,
		SourceType:  model.TopologySourceK8s,
		SourceRef:   sourceRef,
	}
}

func (b *k8sGraphBuilder) edge(from, to, edgeType string, object metav1.Object) model.TopologyEdge {
	confidence := 1.0
	sourceRef, _ := json.Marshal(map[string]any{
		"kind":      k8sObjectKind(object),
		"namespace": object.GetNamespace(),
		"name":      object.GetName(),
		"uid":       string(object.GetUID()),
	})
	return model.TopologyEdge{
		EdgeKey:     edgeKeyFor(from, to, edgeType),
		FromNodeKey: from,
		ToNodeKey:   to,
		EdgeType:    edgeType,
		Confidence:  &confidence,
		Properties:  []byte(`{}`),
		SourceType:  model.TopologySourceK8s,
		SourceRef:   sourceRef,
	}
}

func k8sObjectKind(object metav1.Object) string {
	switch object.(type) {
	case *appsv1.Deployment:
		return "Deployment"
	case *corev1.Pod:
		return "Pod"
	case *corev1.Service:
		return "Service"
	case *networkingv1.Ingress:
		return "Ingress"
	default:
		return ""
	}
}

func k8sNodeKey(cluster, namespace, kind, name string) string {
	return fmt.Sprintf("k8s:%s:%s:%s:%s", strings.TrimSpace(cluster), namespace, kind, name)
}

func ingressClass(ingress *networkingv1.Ingress) string {
	if ingress.Spec.IngressClassName == nil {
		return ""
	}
	return *ingress.Spec.IngressClassName
}

func ingressHosts(ingress *networkingv1.Ingress) []string {
	hosts := []string{}
	for _, rule := range ingress.Spec.Rules {
		if strings.TrimSpace(rule.Host) != "" {
			hosts = append(hosts, rule.Host)
		}
	}
	return hosts
}

func ingressServiceBackends(ingress *networkingv1.Ingress) []string {
	seen := map[string]struct{}{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		seen[name] = struct{}{}
	}
	if ingress.Spec.DefaultBackend != nil && ingress.Spec.DefaultBackend.Service != nil {
		add(ingress.Spec.DefaultBackend.Service.Name)
	}
	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			if path.Backend.Service != nil {
				add(path.Backend.Service.Name)
			}
		}
	}
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	return result
}

func selectorsOverlap(left, right labels.Selector) bool {
	requirements, selectable := left.Requirements()
	if !selectable {
		return false
	}
	candidate := labels.Set{}
	for _, requirement := range requirements {
		values := requirement.Values().List()
		if len(values) > 0 {
			candidate[requirement.Key()] = values[0]
		}
	}
	return right.Matches(candidate)
}
