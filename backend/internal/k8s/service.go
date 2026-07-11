package k8s

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"aiops-platform/backend/internal/model"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

type Service struct {
	repository Repository
	secrets    SecretManager
	factory    ClientFactory
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

func NewService(repository Repository, secrets SecretManager, factory ClientFactory) *Service {
	if factory == nil {
		factory = realClientFactory{}
	}
	return &Service{repository: repository, secrets: secrets, factory: factory}
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
