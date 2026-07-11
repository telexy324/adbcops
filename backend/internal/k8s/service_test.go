package k8s

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"aiops-platform/backend/internal/model"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

func TestResourcesRejectsUnauthorizedNamespace(t *testing.T) {
	service, _ := newTestService(t,
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-0", Namespace: "prod"}},
	)

	_, err := service.Resources(context.Background(), testActor(), ResourceInput{
		DataSourceID: 1,
		Resource:     "pods",
		Namespace:    "dev",
	})
	if !errors.Is(err, ErrNamespaceNotAllowed) {
		t.Fatalf("expected ErrNamespaceNotAllowed, got %v", err)
	}
}

func TestResourcesUsesReadOnlyKubernetesVerbs(t *testing.T) {
	service, client := newTestService(t,
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-0", Namespace: "prod"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
	)

	result, err := service.Resources(context.Background(), testActor(), ResourceInput{
		DataSourceID: 1,
		Resource:     "pods",
		Namespace:    "prod",
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("read pods: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].Name != "api-0" {
		t.Fatalf("unexpected items: %+v", result.Items)
	}
	for _, action := range client.Actions() {
		verb := action.GetVerb()
		if verb != "get" && verb != "list" {
			t.Fatalf("expected read-only action, got verb=%s resource=%s", verb, action.GetResource().Resource)
		}
	}
}

func TestTestReadsFirstAllowedNamespace(t *testing.T) {
	service, _ := newTestService(t,
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-0", Namespace: "prod"}},
	)

	result, err := service.Test(context.Background(), testActor(), 1)
	if err != nil {
		t.Fatalf("test kubernetes data source: %v", err)
	}
	if !result.OK || len(result.AllowedNamespaces) != 1 || result.AllowedNamespaces[0] != "prod" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestEmptyAllowedNamespacesRejected(t *testing.T) {
	dataSource := testDataSource(t, Config{APIServer: "https://kubernetes.example.test"})
	service := NewService(testRepository{dataSource: dataSource}, nil, testClientFactory{client: fake.NewSimpleClientset()})

	_, err := service.Test(context.Background(), testActor(), 1)
	if !errors.Is(err, ErrNoAllowedNamespaces) {
		t.Fatalf("expected ErrNoAllowedNamespaces, got %v", err)
	}
}

func TestDiagnosePodCollectsContextWithLimitedLogsAndNoSecret(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-0",
			Namespace: "prod",
			Labels:    map[string]string{"app": "api"},
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "ReplicaSet", Name: "api-74d9f8", UID: "rs-uid"},
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "node-a",
			Containers: []corev1.Container{
				{Name: "app", Image: "repo/api:v1"},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "app", Ready: true, RestartCount: 2, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
			},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "db-password", Namespace: "prod"},
		Data:       map[string][]byte{"password": []byte("top-secret")},
	}
	serviceObject := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod"},
		Spec: corev1.ServiceSpec{
			Selector:  map[string]string{"app": "api"},
			ClusterIP: "10.0.0.1",
			Ports:     []corev1.ServicePort{{Name: "http", Port: 8080, Protocol: corev1.ProtocolTCP}},
		},
	}
	endpoint := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod"},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{{IP: "10.1.1.5"}},
				Ports:     []corev1.EndpointPort{{Name: "http", Port: 8080, Protocol: corev1.ProtocolTCP}},
			},
		},
	}
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: "api-0.1", Namespace: "prod"},
		InvolvedObject: corev1.ObjectReference{
			Kind:      "Pod",
			Namespace: "prod",
			Name:      "api-0",
		},
		Type:    corev1.EventTypeWarning,
		Reason:  "BackOff",
		Message: "Back-off restarting failed container",
		Count:   3,
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}}
	k8sService, client := newTestService(t, pod, secret, serviceObject, endpoint, event, node)
	k8sService.logReader = testLogReader{content: "line-1\nline-2\nline-3\n"}

	result, err := k8sService.DiagnosePod(context.Background(), testActor(), PodDiagnosisInput{
		DataSourceID:        1,
		Namespace:           "prod",
		PodName:             "api-0",
		IncludeNode:         true,
		LogTailLines:        2,
		LogMaxBytes:         14,
		IncludePreviousLogs: true,
	})
	if err != nil {
		t.Fatalf("diagnose pod: %v", err)
	}
	if result.Pod.Name != "api-0" || result.Node == nil || result.Node.Name != "node-a" {
		t.Fatalf("unexpected pod/node summary: %+v", result)
	}
	if len(result.Events) != 1 || result.Events[0].Reason != "BackOff" {
		t.Fatalf("unexpected events: %+v", result.Events)
	}
	if len(result.Services) != 1 || result.Services[0].Name != "api" || len(result.Endpoints) != 1 {
		t.Fatalf("unexpected service/endpoint: services=%+v endpoints=%+v", result.Services, result.Endpoints)
	}
	if len(result.Logs) != 2 {
		t.Fatalf("expected current and previous logs, got %+v", result.Logs)
	}
	for _, log := range result.Logs {
		if log.Lines > 2 || log.Bytes > 14 || !log.Truncated {
			t.Fatalf("log limit not enforced: %+v", log)
		}
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if strings.Contains(string(payload), "top-secret") || strings.Contains(string(payload), "db-password") {
		t.Fatalf("diagnosis leaked secret data: %s", string(payload))
	}
	for _, action := range client.Actions() {
		if action.GetResource().Resource == "secrets" {
			t.Fatalf("diagnosis should not read secrets, got action %+v", action)
		}
	}
}

func TestDiagnosePodRulesFromFakeClientDataset(t *testing.T) {
	className := "nginx"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-0",
			Namespace: "prod",
			Labels:    map[string]string{"app": "api"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app", Image: "repo/api:v1"},
			},
			InitContainers: []corev1.Container{
				{Name: "init", Image: "repo/init:missing"},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: "Unschedulable"},
			},
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "app",
					RestartCount: 5,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff", Message: "back-off restarting failed container"},
					},
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137},
					},
				},
			},
			InitContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "init",
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "failed to pull image"},
					},
				},
			},
		},
	}
	serviceObject := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod"},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "api"},
			Ports:    []corev1.ServicePort{{Name: "http", Port: 8080, Protocol: corev1.ProtocolTCP}},
		},
	}
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "api-ingress", Namespace: "prod"},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &className,
			Rules: []networkingv1.IngressRule{
				{
					Host: "api.example.test",
					IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{
							{
								Path: "/",
								Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
									Name: "api",
									Port: networkingv1.ServiceBackendPort{Number: 8080},
								}},
							},
						},
					}},
				},
			},
		},
	}
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: "api-0.1", Namespace: "prod"},
		InvolvedObject: corev1.ObjectReference{
			Kind:      "Pod",
			Namespace: "prod",
			Name:      "api-0",
		},
		Type:    corev1.EventTypeWarning,
		Reason:  "FailedScheduling",
		Message: "0/3 nodes are available",
		Count:   1,
	}
	k8sService, _ := newTestService(t, pod, serviceObject, ingress, event)
	k8sService.logReader = testLogReader{content: "boot failed\n"}

	result, err := k8sService.DiagnosePod(context.Background(), testActor(), PodDiagnosisInput{
		DataSourceID:        1,
		Namespace:           "prod",
		PodName:             "api-0",
		IncludePreviousLogs: true,
	})
	if err != nil {
		t.Fatalf("diagnose pod: %v", err)
	}
	expected := []string{
		"k8s.pod.crash_loop_backoff",
		"k8s.pod.oom_killed",
		"k8s.pod.image_pull_backoff",
		"k8s.pod.pending",
		"k8s.service.no_ready_endpoint",
		"k8s.ingress.backend_no_endpoint",
	}
	for _, id := range expected {
		finding := findRule(result.Rules, id)
		if finding == nil {
			t.Fatalf("expected rule %s in %+v", id, result.Rules)
		}
		if len(finding.EvidenceKeys) == 0 {
			t.Fatalf("rule %s has no evidence keys", id)
		}
	}
}

func newTestService(t *testing.T, objects ...runtime.Object) (*Service, *fake.Clientset) {
	t.Helper()
	client := fake.NewSimpleClientset(objects...)
	dataSource := testDataSource(t, Config{APIServer: "https://kubernetes.example.test", AllowedNamespaces: []string{"prod"}})
	service := NewService(testRepository{dataSource: dataSource}, nil, testClientFactory{client: client})
	return service, client
}

type testRepository struct {
	dataSource *model.DataSource
}

func (r testRepository) FindDataSourceByID(_ context.Context, id int64) (*model.DataSource, error) {
	if r.dataSource != nil && r.dataSource.ID == id {
		return r.dataSource, nil
	}
	return nil, errors.New("not found")
}

type testClientFactory struct {
	client kubernetes.Interface
}

func (f testClientFactory) ClientFor(context.Context, *model.DataSource, Config, Credential) (kubernetes.Interface, error) {
	return f.client, nil
}

type testLogReader struct {
	content string
}

func (r testLogReader) ReadPodLog(context.Context, kubernetes.Interface, string, string, string, bool, int64, int64) (string, error) {
	return r.content, nil
}

func findRule(findings []RuleFinding, id string) *RuleFinding {
	for index := range findings {
		if findings[index].ID == id {
			return &findings[index]
		}
	}
	return nil
}

func testDataSource(t *testing.T, config Config) *model.DataSource {
	t.Helper()
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return &model.DataSource{
		ID:         1,
		Name:       "cluster-a",
		SourceType: model.DataSourceTypeKubernetes,
		Config:     raw,
		Enabled:    true,
		ReadOnly:   true,
	}
}

func testActor() *model.AppUser {
	return &model.AppUser{ID: 10, Username: "operator", Role: model.RoleUser, Enabled: true}
}
