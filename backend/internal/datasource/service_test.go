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
