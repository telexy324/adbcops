package k8s

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"aiops-platform/backend/internal/model"
	corev1 "k8s.io/api/core/v1"
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
