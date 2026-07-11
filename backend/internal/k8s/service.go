package k8s

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"aiops-platform/backend/internal/model"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	ErrForbidden           = errors.New("kubernetes access forbidden")
	ErrInvalidInput        = errors.New("invalid input")
	ErrUnsupportedSource   = errors.New("unsupported kubernetes data source")
	ErrNamespaceNotAllowed = errors.New("namespace is not allowed")
	ErrNoAllowedNamespaces = errors.New("allowed namespaces are required")
	ErrUnsupportedResource = errors.New("unsupported kubernetes resource")
	ErrDataSourceDisabled  = errors.New("data source disabled")
)

type Repository interface {
	FindDataSourceByID(ctx context.Context, id int64) (*model.DataSource, error)
}

type SecretManager interface {
	Decrypt(value string) (string, error)
}

type ClientFactory interface {
	ClientFor(ctx context.Context, dataSource *model.DataSource, config Config, credential Credential) (kubernetes.Interface, error)
}

type PodLogReader interface {
	ReadPodLog(ctx context.Context, client kubernetes.Interface, namespace, podName, container string, previous bool, tailLines int64, maxBytes int64) (string, error)
}

type Service struct {
	repository Repository
	secrets    SecretManager
	factory    ClientFactory
	logReader  PodLogReader
}

type Config struct {
	APIServer         string   `json:"apiServer"`
	AllowedNamespaces []string `json:"allowedNamespaces"`
	InsecureSkipTLS   bool     `json:"insecureSkipTlsVerify"`
	TimeoutMs         int      `json:"timeoutMs"`
}

type Credential struct {
	Kubeconfig  string `json:"kubeconfig"`
	BearerToken string `json:"bearerToken"`
	CAData      string `json:"caData"`
}

type TestResult struct {
	OK                bool     `json:"ok"`
	AllowedNamespaces []string `json:"allowedNamespaces"`
	Message           string   `json:"message"`
}

type ResourceInput struct {
	DataSourceID int64
	Namespace    string
	Resource     string
	Name         string
	Limit        int
}

type ResourceResult struct {
	DataSourceID int64          `json:"dataSourceId"`
	Resource     string         `json:"resource"`
	Namespace    string         `json:"namespace,omitempty"`
	Items        []ResourceItem `json:"items"`
}

type ResourceItem struct {
	Kind      string          `json:"kind"`
	Namespace string          `json:"namespace,omitempty"`
	Name      string          `json:"name"`
	Status    string          `json:"status,omitempty"`
	Raw       json.RawMessage `json:"raw"`
}

type PodDiagnosisInput struct {
	DataSourceID        int64
	Namespace           string
	PodName             string
	IncludeNode         bool
	LogTailLines        int
	LogMaxBytes         int
	IncludePreviousLogs bool
}

type PodDiagnosisResult struct {
	DataSourceID int64                 `json:"dataSourceId"`
	Namespace    string                `json:"namespace"`
	Pod          PodSummary            `json:"pod"`
	Owner        []OwnerSummary        `json:"owner"`
	Events       []EventSummary        `json:"events"`
	Logs         []PodLogSummary       `json:"logs"`
	Services     []ServiceSummary      `json:"services"`
	Endpoints    []EndpointSummary     `json:"endpoints"`
	Ingresses    []IngressSummary      `json:"ingresses,omitempty"`
	Node         *NodeSummary          `json:"node,omitempty"`
	Rules        []RuleFinding         `json:"rules"`
	Limits       PodDiagnosisLogLimits `json:"limits"`
}

type PodDiagnosisLogLimits struct {
	TailLines int `json:"tailLines"`
	MaxBytes  int `json:"maxBytes"`
}

type PodSummary struct {
	Name              string             `json:"name"`
	Namespace         string             `json:"namespace"`
	Phase             string             `json:"phase"`
	NodeName          string             `json:"nodeName,omitempty"`
	PodIP             string             `json:"podIp,omitempty"`
	HostIP            string             `json:"hostIp,omitempty"`
	Labels            map[string]string  `json:"labels,omitempty"`
	RestartPolicy     string             `json:"restartPolicy,omitempty"`
	Containers        []ContainerSummary `json:"containers"`
	InitContainers    []ContainerSummary `json:"initContainers,omitempty"`
	Conditions        []ConditionSummary `json:"conditions,omitempty"`
	CreationTimestamp string             `json:"creationTimestamp,omitempty"`
}

type ContainerSummary struct {
	Name         string `json:"name"`
	Image        string `json:"image,omitempty"`
	Ready        bool   `json:"ready"`
	RestartCount int32  `json:"restartCount"`
	State        string `json:"state,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Message      string `json:"message,omitempty"`
	ExitCode     int32  `json:"exitCode,omitempty"`
	LastState    string `json:"lastState,omitempty"`
	LastReason   string `json:"lastReason,omitempty"`
	LastExitCode int32  `json:"lastExitCode,omitempty"`
}

type ConditionSummary struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

type OwnerSummary struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	UID  string `json:"uid,omitempty"`
}

type EventSummary struct {
	Type           string `json:"type,omitempty"`
	Reason         string `json:"reason,omitempty"`
	Message        string `json:"message,omitempty"`
	Count          int32  `json:"count,omitempty"`
	FirstTimestamp string `json:"firstTimestamp,omitempty"`
	LastTimestamp  string `json:"lastTimestamp,omitempty"`
}

type PodLogSummary struct {
	Container string `json:"container"`
	Previous  bool   `json:"previous"`
	Lines     int    `json:"lines"`
	Bytes     int    `json:"bytes"`
	Truncated bool   `json:"truncated"`
	Content   string `json:"content"`
}

type ServiceSummary struct {
	Name      string            `json:"name"`
	Type      string            `json:"type,omitempty"`
	Selector  map[string]string `json:"selector,omitempty"`
	ClusterIP string            `json:"clusterIp,omitempty"`
	Ports     []string          `json:"ports,omitempty"`
}

type EndpointSummary struct {
	Name      string   `json:"name"`
	Addresses []string `json:"addresses,omitempty"`
	Ports     []string `json:"ports,omitempty"`
}

type IngressSummary struct {
	Name     string              `json:"name"`
	Class    string              `json:"class,omitempty"`
	Hosts    []string            `json:"hosts,omitempty"`
	Backends []IngressBackendRef `json:"backends,omitempty"`
}

type IngressBackendRef struct {
	Service string `json:"service"`
	Port    string `json:"port,omitempty"`
}

type NodeSummary struct {
	Name       string             `json:"name"`
	Conditions []ConditionSummary `json:"conditions,omitempty"`
}

type RuleFinding struct {
	ID           string   `json:"id"`
	Severity     string   `json:"severity"`
	Category     string   `json:"category"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	EvidenceKeys []string `json:"evidenceKeys"`
	Suggestion   string   `json:"suggestion,omitempty"`
}

func NewService(repository Repository, secrets SecretManager, factory ClientFactory) *Service {
	if factory == nil {
		factory = realClientFactory{}
	}
	return &Service{repository: repository, secrets: secrets, factory: factory, logReader: clientPodLogReader{}}
}

func (s *Service) Test(ctx context.Context, actor *model.AppUser, dataSourceID int64) (*TestResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	dataSource, config, credential, err := s.load(ctx, dataSourceID)
	if err != nil {
		return nil, err
	}
	client, err := s.factory.ClientFor(ctx, dataSource, config, credential)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}
	ns := config.AllowedNamespaces[0]
	if _, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		return nil, fmt.Errorf("test kubernetes client: %w", err)
	}
	return &TestResult{OK: true, AllowedNamespaces: config.AllowedNamespaces, Message: "kubernetes client can read allowed namespace"}, nil
}

func (s *Service) Resources(ctx context.Context, actor *model.AppUser, input ResourceInput) (*ResourceResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	dataSource, config, credential, err := s.load(ctx, input.DataSourceID)
	if err != nil {
		return nil, err
	}
	resource := strings.ToLower(strings.TrimSpace(input.Resource))
	namespace := strings.TrimSpace(input.Namespace)
	if resource == "" {
		return nil, ErrInvalidInput
	}
	if resource != "namespaces" {
		if namespace == "" {
			return nil, ErrInvalidInput
		}
		if !namespaceAllowed(namespace, config.AllowedNamespaces) {
			return nil, ErrNamespaceNotAllowed
		}
	}
	client, err := s.factory.ClientFor(ctx, dataSource, config, credential)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}
	items, err := readResources(ctx, client, resource, namespace, strings.TrimSpace(input.Name), input.Limit, config.AllowedNamespaces)
	if err != nil {
		return nil, err
	}
	return &ResourceResult{DataSourceID: dataSource.ID, Resource: resource, Namespace: namespace, Items: items}, nil
}

func (s *Service) DiagnosePod(ctx context.Context, actor *model.AppUser, input PodDiagnosisInput) (*PodDiagnosisResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	namespace := strings.TrimSpace(input.Namespace)
	podName := strings.TrimSpace(input.PodName)
	if namespace == "" || podName == "" {
		return nil, ErrInvalidInput
	}
	dataSource, config, credential, err := s.load(ctx, input.DataSourceID)
	if err != nil {
		return nil, err
	}
	if !namespaceAllowed(namespace, config.AllowedNamespaces) {
		return nil, ErrNamespaceNotAllowed
	}
	client, err := s.factory.ClientFor(ctx, dataSource, config, credential)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}
	pod, err := client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	tailLines, maxBytes := normalizeLogLimits(input.LogTailLines, input.LogMaxBytes)
	result := &PodDiagnosisResult{
		DataSourceID: dataSource.ID,
		Namespace:    namespace,
		Pod:          summarizePod(pod),
		Owner:        summarizeOwners(pod.OwnerReferences),
		Limits:       PodDiagnosisLogLimits{TailLines: tailLines, MaxBytes: maxBytes},
	}
	result.Events, err = collectPodEvents(ctx, client, pod)
	if err != nil {
		return nil, err
	}
	result.Services, result.Endpoints, err = collectPodServicesAndEndpoints(ctx, client, pod)
	if err != nil {
		return nil, err
	}
	result.Ingresses, err = collectIngressesForServices(ctx, client, namespace, result.Services)
	if err != nil {
		return nil, err
	}
	result.Logs, err = s.collectPodLogs(ctx, client, pod, input.IncludePreviousLogs, int64(tailLines), int64(maxBytes))
	if err != nil {
		return nil, err
	}
	if input.IncludeNode && pod.Spec.NodeName != "" {
		node, err := client.CoreV1().Nodes().Get(ctx, pod.Spec.NodeName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		result.Node = summarizeNode(node)
	}
	result.Rules = EvaluatePodRules(result)
	return result, nil
}

func (s *Service) load(ctx context.Context, dataSourceID int64) (*model.DataSource, Config, Credential, error) {
	if dataSourceID <= 0 {
		return nil, Config{}, Credential{}, ErrInvalidInput
	}
	dataSource, err := s.repository.FindDataSourceByID(ctx, dataSourceID)
	if err != nil {
		return nil, Config{}, Credential{}, err
	}
	if !dataSource.Enabled {
		return nil, Config{}, Credential{}, ErrDataSourceDisabled
	}
	if dataSource.SourceType != model.DataSourceTypeKubernetes {
		return nil, Config{}, Credential{}, ErrUnsupportedSource
	}
	config, err := parseConfig(dataSource.Config)
	if err != nil {
		return nil, Config{}, Credential{}, err
	}
	credential, err := s.loadCredential(dataSource)
	if err != nil {
		return nil, Config{}, Credential{}, err
	}
	return dataSource, config, credential, nil
}

func parseConfig(raw []byte) (Config, error) {
	var config Config
	if err := json.Unmarshal(raw, &config); err != nil {
		return Config{}, ErrInvalidInput
	}
	normalized := make([]string, 0, len(config.AllowedNamespaces))
	seen := map[string]struct{}{}
	for _, ns := range config.AllowedNamespaces {
		ns = strings.TrimSpace(ns)
		if ns == "" || ns == "*" {
			return Config{}, ErrNoAllowedNamespaces
		}
		if _, ok := seen[ns]; ok {
			continue
		}
		seen[ns] = struct{}{}
		normalized = append(normalized, ns)
	}
	if len(normalized) == 0 {
		return Config{}, ErrNoAllowedNamespaces
	}
	config.AllowedNamespaces = normalized
	if config.TimeoutMs <= 0 {
		config.TimeoutMs = 10000
	}
	return config, nil
}

func (s *Service) loadCredential(dataSource *model.DataSource) (Credential, error) {
	if dataSource.Credential == nil || dataSource.Credential.EncryptedPayload == "" || s.secrets == nil {
		return Credential{}, nil
	}
	plaintext, err := s.secrets.Decrypt(dataSource.Credential.EncryptedPayload)
	if err != nil {
		return Credential{}, fmt.Errorf("decrypt kubernetes credential: %w", err)
	}
	var credential Credential
	if err := json.Unmarshal([]byte(plaintext), &credential); err != nil {
		return Credential{}, ErrInvalidInput
	}
	return credential, nil
}

func readResources(ctx context.Context, client kubernetes.Interface, resource, namespace, name string, limit int, allowedNamespaces []string) ([]ResourceItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	switch resource {
	case "pods":
		if name != "" {
			pod, err := client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return nil, err
			}
			return []ResourceItem{podItem(pod)}, nil
		}
		list, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{Limit: int64(limit)})
		if err != nil {
			return nil, err
		}
		items := make([]ResourceItem, 0, len(list.Items))
		for index := range list.Items {
			items = append(items, podItem(&list.Items[index]))
		}
		return items, nil
	case "services":
		list, err := client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{Limit: int64(limit)})
		if err != nil {
			return nil, err
		}
		items := make([]ResourceItem, 0, len(list.Items))
		for index := range list.Items {
			items = append(items, objectItem("Service", &list.Items[index], ""))
		}
		return items, nil
	case "events":
		list, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{Limit: int64(limit)})
		if err != nil {
			return nil, err
		}
		items := make([]ResourceItem, 0, len(list.Items))
		for index := range list.Items {
			items = append(items, objectItem("Event", &list.Items[index], list.Items[index].Reason))
		}
		return items, nil
	case "deployments":
		list, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{Limit: int64(limit)})
		if err != nil {
			return nil, err
		}
		items := make([]ResourceItem, 0, len(list.Items))
		for index := range list.Items {
			items = append(items, deploymentItem(&list.Items[index]))
		}
		return items, nil
	case "ingresses":
		if name != "" {
			ingress, err := client.NetworkingV1().Ingresses(namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return nil, err
			}
			return []ResourceItem{objectItem("Ingress", ingress, "")}, nil
		}
		list, err := client.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{Limit: int64(limit)})
		if err != nil {
			return nil, err
		}
		items := make([]ResourceItem, 0, len(list.Items))
		for index := range list.Items {
			items = append(items, objectItem("Ingress", &list.Items[index], ""))
		}
		return items, nil
	case "namespaces":
		items := make([]ResourceItem, 0, len(allowedNamespaces))
		for _, ns := range allowedNamespaces {
			namespaceObject, err := client.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
			if err != nil {
				return nil, err
			}
			items = append(items, objectItem("Namespace", namespaceObject, string(namespaceObject.Status.Phase)))
		}
		return items, nil
	default:
		return nil, ErrUnsupportedResource
	}
}

func normalizeLogLimits(tailLines, maxBytes int) (int, int) {
	if tailLines <= 0 || tailLines > 2000 {
		tailLines = 200
	}
	if maxBytes <= 0 || maxBytes > 1024*1024 {
		maxBytes = 64 * 1024
	}
	return tailLines, maxBytes
}

func summarizePod(pod *corev1.Pod) PodSummary {
	summary := PodSummary{
		Name:              pod.Name,
		Namespace:         pod.Namespace,
		Phase:             string(pod.Status.Phase),
		NodeName:          pod.Spec.NodeName,
		PodIP:             pod.Status.PodIP,
		HostIP:            pod.Status.HostIP,
		Labels:            copyStringMap(pod.Labels),
		RestartPolicy:     string(pod.Spec.RestartPolicy),
		Containers:        summarizeContainers(pod.Spec.Containers, pod.Status.ContainerStatuses),
		InitContainers:    summarizeContainers(pod.Spec.InitContainers, pod.Status.InitContainerStatuses),
		Conditions:        summarizePodConditions(pod.Status.Conditions),
		CreationTimestamp: pod.CreationTimestamp.Time.Format(time.RFC3339),
	}
	return summary
}

func summarizeContainers(containers []corev1.Container, statuses []corev1.ContainerStatus) []ContainerSummary {
	statusByName := make(map[string]corev1.ContainerStatus, len(statuses))
	for _, status := range statuses {
		statusByName[status.Name] = status
	}
	result := make([]ContainerSummary, 0, len(containers))
	for _, container := range containers {
		item := ContainerSummary{Name: container.Name, Image: container.Image}
		if status, ok := statusByName[container.Name]; ok {
			item.Ready = status.Ready
			item.RestartCount = status.RestartCount
			item.State, item.Reason, item.Message, item.ExitCode = summarizeContainerState(status.State)
			item.LastState, item.LastReason, _, item.LastExitCode = summarizeContainerState(status.LastTerminationState)
		}
		result = append(result, item)
	}
	return result
}

func summarizeContainerState(state corev1.ContainerState) (string, string, string, int32) {
	switch {
	case state.Waiting != nil:
		return "waiting", state.Waiting.Reason, state.Waiting.Message, 0
	case state.Running != nil:
		return "running", "", "", 0
	case state.Terminated != nil:
		return "terminated", state.Terminated.Reason, state.Terminated.Message, state.Terminated.ExitCode
	default:
		return "", "", "", 0
	}
}

func summarizePodConditions(conditions []corev1.PodCondition) []ConditionSummary {
	result := make([]ConditionSummary, 0, len(conditions))
	for _, condition := range conditions {
		result = append(result, ConditionSummary{
			Type:    string(condition.Type),
			Status:  string(condition.Status),
			Reason:  condition.Reason,
			Message: condition.Message,
		})
	}
	return result
}

func summarizeOwners(references []metav1.OwnerReference) []OwnerSummary {
	result := make([]OwnerSummary, 0, len(references))
	for _, reference := range references {
		result = append(result, OwnerSummary{Kind: reference.Kind, Name: reference.Name, UID: string(reference.UID)})
	}
	return result
}

func collectPodEvents(ctx context.Context, client kubernetes.Interface, pod *corev1.Pod) ([]EventSummary, error) {
	list, err := client.CoreV1().Events(pod.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	result := make([]EventSummary, 0, len(list.Items))
	for _, event := range list.Items {
		if event.InvolvedObject.Kind != "Pod" || event.InvolvedObject.Name != pod.Name || event.InvolvedObject.Namespace != pod.Namespace {
			continue
		}
		result = append(result, EventSummary{
			Type:           event.Type,
			Reason:         event.Reason,
			Message:        event.Message,
			Count:          event.Count,
			FirstTimestamp: event.FirstTimestamp.Time.Format(time.RFC3339),
			LastTimestamp:  event.LastTimestamp.Time.Format(time.RFC3339),
		})
	}
	return result, nil
}

func collectPodServicesAndEndpoints(ctx context.Context, client kubernetes.Interface, pod *corev1.Pod) ([]ServiceSummary, []EndpointSummary, error) {
	services, err := client.CoreV1().Services(pod.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, err
	}
	serviceSummaries := make([]ServiceSummary, 0)
	endpointSummaries := make([]EndpointSummary, 0)
	for index := range services.Items {
		service := &services.Items[index]
		if len(service.Spec.Selector) == 0 || !labels.SelectorFromSet(service.Spec.Selector).Matches(labels.Set(pod.Labels)) {
			continue
		}
		serviceSummaries = append(serviceSummaries, summarizeService(service))
		endpoints, err := client.CoreV1().Endpoints(pod.Namespace).Get(ctx, service.Name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, nil, err
		}
		endpointSummaries = append(endpointSummaries, summarizeEndpoint(endpoints))
	}
	return serviceSummaries, endpointSummaries, nil
}

func summarizeService(service *corev1.Service) ServiceSummary {
	ports := make([]string, 0, len(service.Spec.Ports))
	for _, port := range service.Spec.Ports {
		ports = append(ports, fmt.Sprintf("%s:%d/%s", port.Name, port.Port, port.Protocol))
	}
	return ServiceSummary{
		Name:      service.Name,
		Type:      string(service.Spec.Type),
		Selector:  copyStringMap(service.Spec.Selector),
		ClusterIP: service.Spec.ClusterIP,
		Ports:     ports,
	}
}

func summarizeEndpoint(endpoint *corev1.Endpoints) EndpointSummary {
	summary := EndpointSummary{Name: endpoint.Name}
	for _, subset := range endpoint.Subsets {
		for _, address := range subset.Addresses {
			summary.Addresses = append(summary.Addresses, address.IP)
		}
		for _, port := range subset.Ports {
			summary.Ports = append(summary.Ports, fmt.Sprintf("%s:%d/%s", port.Name, port.Port, port.Protocol))
		}
	}
	return summary
}

func collectIngressesForServices(ctx context.Context, client kubernetes.Interface, namespace string, services []ServiceSummary) ([]IngressSummary, error) {
	if len(services) == 0 {
		return nil, nil
	}
	serviceNames := make(map[string]struct{}, len(services))
	for _, service := range services {
		serviceNames[service.Name] = struct{}{}
	}
	list, err := client.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	result := make([]IngressSummary, 0)
	for index := range list.Items {
		ingress := &list.Items[index]
		summary := summarizeIngress(ingress)
		if ingressReferencesServices(summary, serviceNames) {
			result = append(result, summary)
		}
	}
	return result, nil
}

func summarizeIngress(ingress *networkingv1.Ingress) IngressSummary {
	summary := IngressSummary{Name: ingress.Name}
	if ingress.Spec.IngressClassName != nil {
		summary.Class = *ingress.Spec.IngressClassName
	}
	if ingress.Spec.DefaultBackend != nil && ingress.Spec.DefaultBackend.Service != nil {
		summary.Backends = append(summary.Backends, summarizeIngressBackend(*ingress.Spec.DefaultBackend.Service))
	}
	for _, rule := range ingress.Spec.Rules {
		if rule.Host != "" {
			summary.Hosts = append(summary.Hosts, rule.Host)
		}
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			if path.Backend.Service != nil {
				summary.Backends = append(summary.Backends, summarizeIngressBackend(*path.Backend.Service))
			}
		}
	}
	return summary
}

func summarizeIngressBackend(backend networkingv1.IngressServiceBackend) IngressBackendRef {
	port := backend.Port.Name
	if port == "" && backend.Port.Number > 0 {
		port = fmt.Sprintf("%d", backend.Port.Number)
	}
	return IngressBackendRef{Service: backend.Name, Port: port}
}

func ingressReferencesServices(ingress IngressSummary, serviceNames map[string]struct{}) bool {
	for _, backend := range ingress.Backends {
		if _, ok := serviceNames[backend.Service]; ok {
			return true
		}
	}
	return false
}

func (s *Service) collectPodLogs(ctx context.Context, client kubernetes.Interface, pod *corev1.Pod, includePrevious bool, tailLines, maxBytes int64) ([]PodLogSummary, error) {
	result := make([]PodLogSummary, 0, len(pod.Spec.Containers)*2)
	for _, container := range pod.Spec.Containers {
		current, err := s.readLimitedPodLog(ctx, client, pod.Namespace, pod.Name, container.Name, false, tailLines, maxBytes)
		if err != nil {
			return nil, err
		}
		result = append(result, current)
		if includePrevious {
			previous, err := s.readLimitedPodLog(ctx, client, pod.Namespace, pod.Name, container.Name, true, tailLines, maxBytes)
			if err != nil {
				return nil, err
			}
			result = append(result, previous)
		}
	}
	return result, nil
}

func (s *Service) readLimitedPodLog(ctx context.Context, client kubernetes.Interface, namespace, podName, container string, previous bool, tailLines, maxBytes int64) (PodLogSummary, error) {
	content, err := s.logReader.ReadPodLog(ctx, client, namespace, podName, container, previous, tailLines, maxBytes)
	if err != nil {
		return PodLogSummary{}, err
	}
	limited, truncated := limitLogContent(content, int(tailLines), int(maxBytes))
	return PodLogSummary{
		Container: container,
		Previous:  previous,
		Lines:     countLogLines(limited),
		Bytes:     len([]byte(limited)),
		Truncated: truncated,
		Content:   limited,
	}, nil
}

func limitLogContent(content string, tailLines, maxBytes int) (string, bool) {
	truncated := false
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if tailLines > 0 && len(lines) > tailLines {
		lines = lines[len(lines)-tailLines:]
		truncated = true
	}
	limited := strings.Join(lines, "\n")
	if content != "" && strings.HasSuffix(content, "\n") {
		limited += "\n"
	}
	raw := []byte(limited)
	if maxBytes > 0 && len(raw) > maxBytes {
		raw = raw[len(raw)-maxBytes:]
		truncated = true
	}
	return string(raw), truncated
}

func countLogLines(content string) int {
	content = strings.TrimSuffix(content, "\n")
	if content == "" {
		return 0
	}
	return strings.Count(content, "\n") + 1
}

func summarizeNode(node *corev1.Node) *NodeSummary {
	summary := &NodeSummary{Name: node.Name}
	for _, condition := range node.Status.Conditions {
		summary.Conditions = append(summary.Conditions, ConditionSummary{
			Type:    string(condition.Type),
			Status:  string(condition.Status),
			Reason:  condition.Reason,
			Message: condition.Message,
		})
	}
	return summary
}

func EvaluatePodRules(result *PodDiagnosisResult) []RuleFinding {
	if result == nil {
		return nil
	}
	findings := make([]RuleFinding, 0)
	findings = append(findings, evaluateCrashLoopBackOff(result)...)
	findings = append(findings, evaluateOOMKilled(result)...)
	findings = append(findings, evaluateImagePullBackOff(result)...)
	findings = append(findings, evaluatePending(result)...)
	findings = append(findings, evaluateServiceEndpoint(result)...)
	findings = append(findings, evaluateIngress(result)...)
	return findings
}

func evaluateCrashLoopBackOff(result *PodDiagnosisResult) []RuleFinding {
	findings := make([]RuleFinding, 0)
	for _, container := range allContainers(result.Pod) {
		if strings.EqualFold(container.Reason, "CrashLoopBackOff") || strings.Contains(container.Message, "CrashLoopBackOff") || hasEventReason(result.Events, "BackOff") {
			findings = append(findings, RuleFinding{
				ID:           "k8s.pod.crash_loop_backoff",
				Severity:     "critical",
				Category:     "pod",
				Title:        "Pod container is in CrashLoopBackOff",
				Description:  fmt.Sprintf("container %s is repeatedly restarting or has BackOff events", container.Name),
				EvidenceKeys: []string{evidenceKey("pod.container", container.Name, "reason"), "pod.events.BackOff"},
				Suggestion:   "查看 previous logs、启动参数、配置依赖和最近变更。",
			})
		}
	}
	return findings
}

func evaluateOOMKilled(result *PodDiagnosisResult) []RuleFinding {
	findings := make([]RuleFinding, 0)
	for _, container := range allContainers(result.Pod) {
		if strings.EqualFold(container.LastReason, "OOMKilled") || strings.EqualFold(container.Reason, "OOMKilled") || containsAnyEvent(result.Events, "OOMKilled", "out of memory") {
			findings = append(findings, RuleFinding{
				ID:           "k8s.pod.oom_killed",
				Severity:     "critical",
				Category:     "pod",
				Title:        "Container was OOMKilled",
				Description:  fmt.Sprintf("container %s was terminated due to memory pressure", container.Name),
				EvidenceKeys: []string{evidenceKey("pod.container", container.Name, "lastReason"), "pod.events.OOMKilled"},
				Suggestion:   "检查内存使用趋势、limit 设置、启动后内存峰值和泄漏风险。",
			})
		}
	}
	return findings
}

func evaluateImagePullBackOff(result *PodDiagnosisResult) []RuleFinding {
	findings := make([]RuleFinding, 0)
	for _, container := range allContainers(result.Pod) {
		if isImagePullReason(container.Reason) || containsAnyEvent(result.Events, "ImagePullBackOff", "ErrImagePull", "pull image") {
			findings = append(findings, RuleFinding{
				ID:           "k8s.pod.image_pull_backoff",
				Severity:     "high",
				Category:     "pod",
				Title:        "Container image cannot be pulled",
				Description:  fmt.Sprintf("container %s image pull is failing", container.Name),
				EvidenceKeys: []string{evidenceKey("pod.container", container.Name, "reason"), "pod.events.ImagePull"},
				Suggestion:   "检查镜像地址、tag、仓库权限、镜像拉取 Secret 和节点到仓库网络。",
			})
		}
	}
	return findings
}

func evaluatePending(result *PodDiagnosisResult) []RuleFinding {
	if !strings.EqualFold(result.Pod.Phase, "Pending") && !hasPodCondition(result.Pod.Conditions, "PodScheduled", "False") {
		return nil
	}
	return []RuleFinding{{
		ID:           "k8s.pod.pending",
		Severity:     "high",
		Category:     "scheduling",
		Title:        "Pod is pending or unscheduled",
		Description:  "pod is not scheduled or still pending",
		EvidenceKeys: []string{"pod.phase", "pod.conditions.PodScheduled", "pod.events.FailedScheduling"},
		Suggestion:   "检查资源请求、节点污点/亲和性、PVC 绑定和调度事件。",
	}}
}

func evaluateServiceEndpoint(result *PodDiagnosisResult) []RuleFinding {
	findings := make([]RuleFinding, 0)
	endpointByName := make(map[string]EndpointSummary, len(result.Endpoints))
	for _, endpoint := range result.Endpoints {
		endpointByName[endpoint.Name] = endpoint
	}
	for _, service := range result.Services {
		endpoint, ok := endpointByName[service.Name]
		if !ok || len(endpoint.Addresses) == 0 {
			findings = append(findings, RuleFinding{
				ID:           "k8s.service.no_ready_endpoint",
				Severity:     "high",
				Category:     "service",
				Title:        "Service has no ready endpoint for this Pod selector",
				Description:  fmt.Sprintf("service %s matches the pod labels but has no ready endpoint addresses", service.Name),
				EvidenceKeys: []string{evidenceKey("service", service.Name, "selector"), evidenceKey("endpoint", service.Name, "addresses")},
				Suggestion:   "检查 Service selector、Pod readiness、EndpointSlice/Endpoints 和容器健康检查。",
			})
		}
	}
	return findings
}

func evaluateIngress(result *PodDiagnosisResult) []RuleFinding {
	if len(result.Ingresses) == 0 {
		return nil
	}
	noEndpointServices := map[string]struct{}{}
	endpointByName := make(map[string]EndpointSummary, len(result.Endpoints))
	for _, endpoint := range result.Endpoints {
		endpointByName[endpoint.Name] = endpoint
	}
	for _, service := range result.Services {
		endpoint, ok := endpointByName[service.Name]
		if !ok || len(endpoint.Addresses) == 0 {
			noEndpointServices[service.Name] = struct{}{}
		}
	}
	findings := make([]RuleFinding, 0)
	for _, ingress := range result.Ingresses {
		for _, backend := range ingress.Backends {
			if _, ok := noEndpointServices[backend.Service]; !ok {
				continue
			}
			findings = append(findings, RuleFinding{
				ID:           "k8s.ingress.backend_no_endpoint",
				Severity:     "high",
				Category:     "ingress",
				Title:        "Ingress backend service has no ready endpoint",
				Description:  fmt.Sprintf("ingress %s routes to service %s, but the service has no ready endpoints", ingress.Name, backend.Service),
				EvidenceKeys: []string{evidenceKey("ingress", ingress.Name, "backend"), evidenceKey("endpoint", backend.Service, "addresses")},
				Suggestion:   "检查 Ingress backend、Service selector、Pod readiness 和 Endpoint 状态。",
			})
		}
	}
	return findings
}

func allContainers(pod PodSummary) []ContainerSummary {
	result := make([]ContainerSummary, 0, len(pod.InitContainers)+len(pod.Containers))
	result = append(result, pod.InitContainers...)
	result = append(result, pod.Containers...)
	return result
}

func evidenceKey(parts ...string) string {
	return strings.Join(parts, ".")
}

func hasEventReason(events []EventSummary, reason string) bool {
	for _, event := range events {
		if strings.EqualFold(event.Reason, reason) {
			return true
		}
	}
	return false
}

func containsAnyEvent(events []EventSummary, needles ...string) bool {
	for _, event := range events {
		haystack := strings.ToLower(event.Reason + " " + event.Message)
		for _, needle := range needles {
			if strings.Contains(haystack, strings.ToLower(needle)) {
				return true
			}
		}
	}
	return false
}

func isImagePullReason(reason string) bool {
	return strings.EqualFold(reason, "ImagePullBackOff") || strings.EqualFold(reason, "ErrImagePull")
}

func hasPodCondition(conditions []ConditionSummary, conditionType string, status string) bool {
	for _, condition := range conditions {
		if condition.Type == conditionType && condition.Status == status {
			return true
		}
	}
	return false
}

func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func podItem(pod *corev1.Pod) ResourceItem {
	return objectItem("Pod", pod, string(pod.Status.Phase))
}

func deploymentItem(deployment *appsv1.Deployment) ResourceItem {
	return objectItem("Deployment", deployment, fmt.Sprintf("ready=%d/%d", deployment.Status.ReadyReplicas, deployment.Status.Replicas))
}

func objectItem(kind string, object runtime.Object, status string) ResourceItem {
	raw, _ := json.Marshal(object)
	meta, _ := object.(metav1.Object)
	item := ResourceItem{Kind: kind, Status: status, Raw: raw}
	if meta != nil {
		item.Namespace = meta.GetNamespace()
		item.Name = meta.GetName()
	}
	return item
}

func namespaceAllowed(namespace string, allowed []string) bool {
	for _, candidate := range allowed {
		if namespace == candidate {
			return true
		}
	}
	return false
}

type realClientFactory struct{}

type clientPodLogReader struct{}

func (clientPodLogReader) ReadPodLog(ctx context.Context, client kubernetes.Interface, namespace, podName, container string, previous bool, tailLines int64, maxBytes int64) (string, error) {
	options := &corev1.PodLogOptions{Container: container, Previous: previous}
	if tailLines > 0 {
		options.TailLines = &tailLines
	}
	raw, err := client.CoreV1().Pods(namespace).GetLogs(podName, options).DoRaw(ctx)
	if err != nil {
		return "", err
	}
	if maxBytes > 0 && int64(len(raw)) > maxBytes {
		raw = raw[int64(len(raw))-maxBytes:]
	}
	return string(bytes.TrimRight(raw, "\x00")), nil
}

func (realClientFactory) ClientFor(_ context.Context, _ *model.DataSource, config Config, credential Credential) (kubernetes.Interface, error) {
	var restConfig *rest.Config
	var err error
	if strings.TrimSpace(credential.Kubeconfig) != "" {
		restConfig, err = clientcmd.RESTConfigFromKubeConfig([]byte(credential.Kubeconfig))
		if err != nil {
			return nil, err
		}
	} else {
		if strings.TrimSpace(config.APIServer) == "" {
			return nil, ErrInvalidInput
		}
		restConfig = &rest.Config{Host: strings.TrimSpace(config.APIServer), BearerToken: strings.TrimSpace(credential.BearerToken)}
		if config.InsecureSkipTLS {
			restConfig.TLSClientConfig.Insecure = true
		}
		if strings.TrimSpace(credential.CAData) != "" {
			restConfig.TLSClientConfig.CAData = []byte(strings.TrimSpace(credential.CAData))
		}
	}
	restConfig.Timeout = time.Duration(config.TimeoutMs) * time.Millisecond
	return kubernetes.NewForConfig(restConfig)
}
