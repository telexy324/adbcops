package nacos

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"aiops-platform/backend/internal/model"
)

func TestListServicesAndInstancesUsesAllowedScope(t *testing.T) {
	client := &recordingClient{
		handler: func(r *http.Request) (*http.Response, error) {
			switch r.URL.Path {
			case "/nacos/v1/ns/service/list":
				return jsonResponse(map[string]interface{}{
					"count": 2,
					"doms":  []string{"DEFAULT_GROUP@@orders", "DEFAULT_GROUP@@payments"},
				}), nil
			case "/nacos/v1/ns/instance/list":
				return jsonResponse(map[string]interface{}{
					"hosts": []map[string]interface{}{
						{
							"ip":          "10.0.0.12",
							"port":        8848,
							"healthy":     true,
							"enabled":     true,
							"ephemeral":   true,
							"clusterName": "DEFAULT",
							"metadata":    map[string]string{"zone": "az-a"},
						},
					},
				}), nil
			default:
				return response(http.StatusNotFound, `{"error":"not found"}`), nil
			}
		},
	}

	service := NewService(testRepository{dataSource: testDataSource(t, "https://nacos.example.test")}, nil, client)
	actor := &model.AppUser{ID: 1, Username: "ops"}

	services, err := service.ListServices(context.Background(), actor, ListServicesInput{
		DataSourceID: 1,
		QueryScope:   QueryScope{Namespace: "prod", Group: "DEFAULT_GROUP"},
		PageSize:     500,
	})
	if err != nil {
		t.Fatalf("ListServices() error = %v", err)
	}
	if services.PageSize != maxPageSize || len(services.Services) != 2 {
		t.Fatalf("unexpected service result: %+v", services)
	}
	if client.requests[0].RawQuery != "groupName=DEFAULT_GROUP&namespaceId=prod&pageNo=1&pageSize=200" {
		t.Fatalf("unexpected service query: %s", client.requests[0].RawQuery)
	}

	healthyOnly := true
	instances, err := service.ListInstances(context.Background(), actor, ListInstancesInput{
		DataSourceID: 1,
		QueryScope:   QueryScope{Namespace: "prod", Group: "DEFAULT_GROUP"},
		ServiceName:  "orders",
		HealthyOnly:  &healthyOnly,
	})
	if err != nil {
		t.Fatalf("ListInstances() error = %v", err)
	}
	if len(instances.Instances) != 1 || instances.Instances[0].IP != "10.0.0.12" {
		t.Fatalf("unexpected instance result: %+v", instances)
	}
	if client.requests[1].RawQuery != "groupName=DEFAULT_GROUP&healthyOnly=true&namespaceId=prod&serviceName=orders" {
		t.Fatalf("unexpected instance query: %s", client.requests[1].RawQuery)
	}
}

func TestRejectsUnauthorizedNamespaceOrGroup(t *testing.T) {
	service := NewService(testRepository{dataSource: testDataSource(t, "https://nacos.example.test")}, nil, nil)
	actor := &model.AppUser{ID: 1, Username: "ops"}

	_, err := service.ListServices(context.Background(), actor, ListServicesInput{
		DataSourceID: 1,
		QueryScope:   QueryScope{Namespace: "staging", Group: "DEFAULT_GROUP"},
	})
	if !errors.Is(err, ErrScopeNotAllowed) {
		t.Fatalf("expected ErrScopeNotAllowed for namespace, got %v", err)
	}

	_, err = service.ListServices(context.Background(), actor, ListServicesInput{
		DataSourceID: 1,
		QueryScope:   QueryScope{Namespace: "prod", Group: "SECRET_GROUP"},
	})
	if !errors.Is(err, ErrScopeNotAllowed) {
		t.Fatalf("expected ErrScopeNotAllowed for group, got %v", err)
	}
}

func TestConfigMetadataDoesNotReturnContent(t *testing.T) {
	client := &recordingClient{handler: func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/nacos/v1/cs/configs/metadata" {
			return response(http.StatusNotFound, `{"error":"not found"}`), nil
		}
		return jsonResponse(map[string]interface{}{
			"dataId":           "orders.yaml",
			"group":            "DEFAULT_GROUP",
			"tenant":           "prod",
			"md5":              "abc",
			"lastModifiedTime": "2026-07-13T10:00:00Z",
			"content":          "db.password=secret",
			"value":            "another-secret",
			"config":           "raw-body",
		}), nil
	}}

	service := NewService(testRepository{dataSource: testDataSource(t, "https://nacos.example.test")}, nil, client)
	actor := &model.AppUser{ID: 1, Username: "ops"}

	result, err := service.GetConfigMetadata(context.Background(), actor, ConfigMetadataInput{
		DataSourceID: 1,
		QueryScope:   QueryScope{Namespace: "prod", Group: "DEFAULT_GROUP"},
		DataID:       "orders.yaml",
	})
	if err != nil {
		t.Fatalf("GetConfigMetadata() error = %v", err)
	}
	if result.Metadata["content"] != "" || result.Metadata["value"] != "" || result.Metadata["config"] != "" {
		t.Fatalf("metadata leaked config body: %+v", result.Metadata)
	}
	if result.Metadata["md5"] != "abc" || result.Metadata["dataId"] != "orders.yaml" {
		t.Fatalf("unexpected sanitized metadata: %+v", result.Metadata)
	}
}

func TestCredentialIsAppliedWithoutLoggingValue(t *testing.T) {
	var authHeader string
	client := &recordingClient{handler: func(r *http.Request) (*http.Response, error) {
		authHeader = r.Header.Get("Authorization")
		return jsonResponse(map[string]interface{}{"doms": []string{"orders"}}), nil
	}}

	credential := base64.StdEncoding.EncodeToString([]byte(`{"bearerToken":"nacos-token"}`))
	service := NewService(testRepository{dataSource: testDataSourceWithCredential(t, "https://nacos.example.test", credential)}, fakeSecrets{}, client)
	_, err := service.ListServices(context.Background(), &model.AppUser{ID: 1}, ListServicesInput{DataSourceID: 1})
	if err != nil {
		t.Fatalf("ListServices() error = %v", err)
	}
	if authHeader != "Bearer nacos-token" {
		t.Fatalf("unexpected auth header: %q", authHeader)
	}
}

type recordedRequest struct {
	Path          string
	RawQuery      string
	Authorization string
}

type recordingClient struct {
	requests []recordedRequest
	handler  func(req *http.Request) (*http.Response, error)
}

func (c *recordingClient) Do(req *http.Request) (*http.Response, error) {
	c.requests = append(c.requests, recordedRequest{
		Path:          req.URL.Path,
		RawQuery:      req.URL.RawQuery,
		Authorization: req.Header.Get("Authorization"),
	})
	return c.handler(req)
}

func jsonResponse(value interface{}) *http.Response {
	raw, _ := json.Marshal(value)
	return response(http.StatusOK, string(raw))
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       ioReadCloser{Reader: strings.NewReader(body)},
		Header:     make(http.Header),
		Request:    &http.Request{URL: &url.URL{}},
	}
}

type ioReadCloser struct {
	*strings.Reader
}

func (c ioReadCloser) Close() error {
	return nil
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

type fakeSecrets struct{}

func (fakeSecrets) Decrypt(value string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func testDataSource(t *testing.T, baseURL string) *model.DataSource {
	t.Helper()
	return testDataSourceWithCredential(t, baseURL, "")
}

func testDataSourceWithCredential(t *testing.T, baseURL string, credential string) *model.DataSource {
	t.Helper()
	rawConfig, err := json.Marshal(Config{
		BaseURL:           baseURL,
		Namespace:         "prod",
		DefaultGroup:      "DEFAULT_GROUP",
		AllowedNamespaces: []string{"prod"},
		AllowedGroups:     []string{"DEFAULT_GROUP"},
	})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	dataSource := &model.DataSource{
		ID:         1,
		Name:       "nacos-prod",
		SourceType: model.DataSourceTypeNacos,
		Config:     rawConfig,
		Enabled:    true,
		ReadOnly:   true,
	}
	if credential != "" {
		credentialID := int64(10)
		dataSource.CredentialID = &credentialID
		dataSource.Credential = &model.CredentialSecret{ID: credentialID, EncryptedPayload: credential}
	}
	return dataSource
}
