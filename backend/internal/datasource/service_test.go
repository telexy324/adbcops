package datasource

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

func TestCreateEncryptsCredentialAndRejectsSensitiveConfig(t *testing.T) {
	store := newFakeRepository()
	secrets := &fakeSecrets{}
	service := NewService(store, secrets, "v1")
	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}

	created, err := service.Create(context.Background(), admin, SaveInput{
		Name:       "prod-es",
		SourceType: model.DataSourceTypeElasticsearch,
		Config:     json.RawMessage(`{"baseUrl":"https://es.example","index":"logs-*"}`),
		Credential: json.RawMessage(`{"username":"elastic","password":"secret-password"}`),
		Enabled:    true,
		ReadOnly:   true,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !created.CredentialConfigured {
		t.Fatal("created data source did not report credentialConfigured")
	}
	secret := store.credentials[*store.sources[created.ID].CredentialID]
	if strings.Contains(secret.EncryptedPayload, "secret-password") {
		t.Fatalf("encrypted payload leaked plaintext: %s", secret.EncryptedPayload)
	}
	_, err = service.Create(context.Background(), admin, SaveInput{
		Name:       "bad",
		SourceType: model.DataSourceTypeHTTP,
		Config:     json.RawMessage(`{"baseUrl":"https://example","apiToken":"must-not-be-here"}`),
		Enabled:    true,
		ReadOnly:   true,
	})
	if err != ErrSensitiveConfig {
		t.Fatalf("Create(sensitive config) error = %v, want ErrSensitiveConfig", err)
	}
}

func TestCreateRejectsUnsafeEndpointsForSSRF(t *testing.T) {
	service := NewService(newFakeRepository(), &fakeSecrets{}, "v1")
	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	cases := []struct {
		name       string
		sourceType string
		config     json.RawMessage
	}{
		{name: "localhost", sourceType: model.DataSourceTypeHTTP, config: json.RawMessage(`{"baseUrl":"http://localhost:8080/api"}`)},
		{name: "loopback", sourceType: model.DataSourceTypePrometheus, config: json.RawMessage(`{"baseUrl":"http://127.0.0.1:9090"}`)},
		{name: "private", sourceType: model.DataSourceTypeElasticsearch, config: json.RawMessage(`{"baseUrl":"http://10.0.0.5:9200"}`)},
		{name: "metadata", sourceType: model.DataSourceTypeHTTP, config: json.RawMessage(`{"baseUrl":"http://169.254.169.254/latest/meta-data"}`)},
		{name: "kubernetes api server loopback", sourceType: model.DataSourceTypeKubernetes, config: json.RawMessage(`{"apiServer":"https://[::1]:6443","allowedNamespaces":["default"]}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.Create(context.Background(), admin, SaveInput{
				Name:       tc.name,
				SourceType: tc.sourceType,
				Config:     tc.config,
				Enabled:    true,
				ReadOnly:   true,
			})
			if err != ErrUnsafeEndpoint {
				t.Fatalf("Create() error = %v, want ErrUnsafeEndpoint", err)
			}
		})
	}
}

func TestCreateAcceptsKubernetesPrivateIPAddress(t *testing.T) {
	service := NewService(newFakeRepository(), &fakeSecrets{}, "v1")
	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	for _, apiServer := range []string{
		"https://10.20.30.40:6443",
		"https://192.168.10.5:6443",
		"https://[fd00::10]:6443",
	} {
		t.Run(apiServer, func(t *testing.T) {
			_, err := service.Create(context.Background(), admin, SaveInput{
				Name:       "private-ip-k8s",
				SourceType: model.DataSourceTypeKubernetes,
				Config:     json.RawMessage(`{"apiServer":"` + apiServer + `","allowedNamespaces":["default"]}`),
				Enabled:    true,
				ReadOnly:   true,
			})
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}
		})
	}
}

func TestUpdateKubernetesPrivateIPAddressWithoutReplacingCredential(t *testing.T) {
	store := newFakeRepository()
	service := NewService(store, &fakeSecrets{}, "v1")
	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	created, err := service.Create(context.Background(), admin, SaveInput{
		Name:       "prod-k8s",
		SourceType: model.DataSourceTypeKubernetes,
		Config:     json.RawMessage(`{"apiServer":"https://10.20.30.40:6443","allowedNamespaces":["default"]}`),
		Credential: json.RawMessage(`{"bearerToken":"secret-token"}`),
		Enabled:    true,
		ReadOnly:   true,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	originalCredentialID := store.sources[created.ID].CredentialID
	name := "prod-k8s-updated"
	sourceType := model.DataSourceTypeKubernetes
	enabled := true
	updated, err := service.Update(context.Background(), admin, created.ID, UpdateInput{
		Name:       &name,
		SourceType: &sourceType,
		Config:     json.RawMessage(`{"apiServer":"https://192.168.10.5:6443","allowedNamespaces":["default","payments"]}`),
		Enabled:    &enabled,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Name != name {
		t.Fatalf("updated name = %q, want %q", updated.Name, name)
	}
	if store.sources[created.ID].CredentialID == nil || originalCredentialID == nil || *store.sources[created.ID].CredentialID != *originalCredentialID {
		t.Fatal("empty credential update replaced the existing credential")
	}
}

func TestCreateAcceptsComponentDataSourceTypesAsReadOnly(t *testing.T) {
	service := NewService(newFakeRepository(), &fakeSecrets{}, "v1")
	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	cases := []struct {
		sourceType string
		config     json.RawMessage
	}{
		{sourceType: model.DataSourceTypeNacos, config: json.RawMessage(`{"baseUrl":"https://nacos.example.com","allowedNamespaces":["prod"],"allowedGroups":["DEFAULT_GROUP"]}`)},
		{sourceType: model.DataSourceTypeRedis, config: json.RawMessage(`{"mode":"cluster","endpoints":["redis.example.com:6379"],"allowValueRead":false}`)},
		{sourceType: model.DataSourceTypeTiDB, config: json.RawMessage(`{"dsn":"readonly@tcp(tidb.example.com:4000)/information_schema","explainAnalyzeEnabled":false}`)},
		{sourceType: model.DataSourceTypeNginx, config: json.RawMessage(`{"baseUrl":"https://nginx.example.com","maskClientIp":true,"configContentEnabled":false}`)},
	}
	for _, tc := range cases {
		t.Run(tc.sourceType, func(t *testing.T) {
			created, err := service.Create(context.Background(), admin, SaveInput{
				Name:       tc.sourceType,
				SourceType: tc.sourceType,
				Config:     tc.config,
				Enabled:    true,
				ReadOnly:   false,
			})
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			if !created.ReadOnly {
				t.Fatalf("created data source is not read-only: %+v", created)
			}
		})
	}
}

func TestUserListsOnlyEnabledSanitizedDataSources(t *testing.T) {
	store := newFakeRepository()
	service := NewService(store, &fakeSecrets{}, "v1")
	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	user := &model.AppUser{ID: 2, Role: model.RoleUser}
	enabled, err := service.Create(context.Background(), admin, SaveInput{
		Name:       "enabled",
		SourceType: model.DataSourceTypePrometheus,
		Config:     json.RawMessage(`{"baseUrl":"https://prom.example"}`),
		Credential: json.RawMessage(`{"bearerToken":"secret-token"}`),
		Enabled:    true,
		ReadOnly:   true,
	})
	if err != nil {
		t.Fatalf("Create(enabled) error = %v", err)
	}
	_, err = service.Create(context.Background(), admin, SaveInput{
		Name:       "disabled",
		SourceType: model.DataSourceTypeHTTP,
		Config:     json.RawMessage(`{"baseUrl":"https://disabled.example"}`),
		Enabled:    false,
		ReadOnly:   true,
	})
	if err != nil {
		t.Fatalf("Create(disabled) error = %v", err)
	}

	list, err := service.List(context.Background(), user)
	if err != nil {
		t.Fatalf("List(user) error = %v", err)
	}
	if len(list) != 1 || list[0].ID != enabled.ID {
		t.Fatalf("user list = %+v, want only enabled data source", list)
	}
	encoded, _ := json.Marshal(list[0])
	if strings.Contains(string(encoded), "secret-token") || strings.Contains(string(encoded), "EncryptedPayload") {
		t.Fatalf("user response leaked credential: %s", encoded)
	}
}

func TestAdminOnlyMutationsAndTestUsesDecryptedCredential(t *testing.T) {
	store := newFakeRepository()
	secrets := &fakeSecrets{}
	service := NewService(store, secrets, "v1")
	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	user := &model.AppUser{ID: 2, Role: model.RoleUser}
	created, err := service.Create(context.Background(), admin, SaveInput{
		Name:       "http",
		SourceType: model.DataSourceTypeHTTP,
		Config:     json.RawMessage(`{"baseUrl":"https://example.com"}`),
		Credential: json.RawMessage(`{"header":"Authorization","value":"Bearer secret"}`),
		Enabled:    true,
		ReadOnly:   true,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := service.Update(context.Background(), user, created.ID, UpdateInput{Name: ptr("new")}); err != ErrAdminRequired {
		t.Fatalf("Update(user) error = %v, want ErrAdminRequired", err)
	}
	result, err := service.Test(context.Background(), admin, created.ID)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	if !result.OK || !result.CredentialConfigured || secrets.lastDecrypted == "" {
		t.Fatalf("result=%+v lastDecrypted=%q", result, secrets.lastDecrypted)
	}
}

type fakeRepository struct {
	nextSourceID     int64
	nextCredentialID int64
	sources          map[int64]*model.DataSource
	credentials      map[int64]*model.CredentialSecret
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		nextSourceID:     1,
		nextCredentialID: 1,
		sources:          make(map[int64]*model.DataSource),
		credentials:      make(map[int64]*model.CredentialSecret),
	}
}

func (f *fakeRepository) ListDataSources(_ context.Context, enabledOnly bool) ([]model.DataSource, error) {
	var sources []model.DataSource
	for _, source := range f.sources {
		if enabledOnly && !source.Enabled {
			continue
		}
		sources = append(sources, *source)
	}
	return sources, nil
}

func (f *fakeRepository) FindDataSourceByID(_ context.Context, id int64) (*model.DataSource, error) {
	source, ok := f.sources[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	if source.CredentialID != nil {
		credential := f.credentials[*source.CredentialID]
		source.Credential = credential
	}
	return source, nil
}

func (f *fakeRepository) CreateDataSource(_ context.Context, source *model.DataSource, credential *model.CredentialSecret) error {
	if credential != nil {
		credential.ID = f.nextCredentialID
		f.nextCredentialID++
		f.credentials[credential.ID] = credential
		source.CredentialID = &credential.ID
	}
	source.ID = f.nextSourceID
	f.nextSourceID++
	f.sources[source.ID] = source
	return nil
}

func (f *fakeRepository) UpdateDataSource(_ context.Context, id int64, updates repository.DataSourceUpdates, credential *model.CredentialSecret) (*model.DataSource, error) {
	source, ok := f.sources[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	if updates.Name != nil {
		source.Name = *updates.Name
	}
	if updates.SourceType != nil {
		source.SourceType = *updates.SourceType
	}
	if updates.ConfigSet {
		source.Config = updates.Config
	}
	if updates.Enabled != nil {
		source.Enabled = *updates.Enabled
	}
	if credential != nil {
		credential.ID = f.nextCredentialID
		f.nextCredentialID++
		f.credentials[credential.ID] = credential
		source.CredentialID = &credential.ID
	}
	return source, nil
}

func (f *fakeRepository) DeleteDataSource(_ context.Context, id int64) error {
	if _, ok := f.sources[id]; !ok {
		return repository.ErrNotFound
	}
	delete(f.sources, id)
	return nil
}

type fakeSecrets struct {
	lastDecrypted string
}

func (f *fakeSecrets) Encrypt(plaintext string) (string, error) {
	return "encrypted:" + base64.RawURLEncoding.EncodeToString([]byte(plaintext)), nil
}

func (f *fakeSecrets) Decrypt(value string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "encrypted:"))
	if err != nil {
		return "", err
	}
	f.lastDecrypted = string(decoded)
	return f.lastDecrypted, nil
}

func ptr[T any](value T) *T {
	return &value
}
