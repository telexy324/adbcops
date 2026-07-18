package linuxhost

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"aiops-platform/backend/internal/credential"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

func TestCreateHostEncryptsPasswordAndNeverReturnsIt(t *testing.T) {
	store := newFakeRepository()
	manager := testCredentialManager(t)
	service := NewService(store, manager, "v1")
	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	password := "plain-password"
	username := "ops"

	created, err := service.CreateHost(context.Background(), admin, HostInput{
		Name: "prod-1", Host: "10.20.30.40", Port: 22, Username: &username,
		AuthType: model.LinuxAuthTypePassword, Password: &password, Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateHost() error = %v", err)
	}
	secret := store.secrets[*store.hosts[created.ID].CredentialID]
	if strings.Contains(secret.EncryptedPayload, password) {
		t.Fatalf("encrypted payload contains plaintext: %s", secret.EncryptedPayload)
	}
	plaintext, err := manager.Decrypt(secret.EncryptedPayload)
	if err != nil {
		t.Fatalf("decrypt credential: %v", err)
	}
	if !strings.Contains(plaintext, password) {
		t.Fatalf("decrypted credential = %s", plaintext)
	}
	response, _ := json.Marshal(created)
	for _, forbidden := range []string{password, `"password":`, "credentialId", "encryptedPayload"} {
		if strings.Contains(string(response), forbidden) {
			t.Fatalf("host response leaked %q: %s", forbidden, response)
		}
	}
}

func TestCreateHostEncryptsPrivateKeyAndPassphrase(t *testing.T) {
	store := newFakeRepository()
	manager := testCredentialManager(t)
	service := NewService(store, manager, "v1")
	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	username, privateKey, passphrase := "ops", "-----BEGIN PRIVATE KEY-----test", "key-passphrase"

	created, err := service.CreateHost(context.Background(), admin, HostInput{
		Name: "prod-key", Host: "server.example.com", Username: &username,
		AuthType: model.LinuxAuthTypePrivateKey, PrivateKey: &privateKey,
		PrivateKeyPassphrase: &passphrase, Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateHost() error = %v", err)
	}
	secret := store.secrets[*store.hosts[created.ID].CredentialID]
	plaintext, err := manager.Decrypt(secret.EncryptedPayload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plaintext, privateKey) || !strings.Contains(plaintext, passphrase) {
		t.Fatalf("decrypted credential missing private key fields: %s", plaintext)
	}
	response, _ := json.Marshal(created)
	if strings.Contains(string(response), privateKey) || strings.Contains(string(response), passphrase) {
		t.Fatalf("host response leaked private key credential: %s", response)
	}
}

func TestHostCredentialGroupConflictAndScopeValidation(t *testing.T) {
	store := newFakeRepository()
	store.groups[7] = &model.CredentialGroup{
		ID: 7, CredentialType: model.LinuxAuthTypePassword, Username: "shared", CredentialID: 9,
		Scope: json.RawMessage(`{"environments":["prod"]}`), Enabled: true,
	}
	service := NewService(store, testCredentialManager(t), "v1")
	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	password := "secret"
	groupID := int64(7)
	prod, dev := "prod", "dev"

	_, err := service.CreateHost(context.Background(), admin, HostInput{
		Name: "conflict", Host: "10.0.0.1", Environment: &prod,
		AuthType: model.LinuxAuthTypePassword, Password: &password, CredentialGroupID: &groupID,
	})
	if !errors.Is(err, ErrCredentialConflict) {
		t.Fatalf("conflicting CreateHost() error = %v", err)
	}
	_, err = service.CreateHost(context.Background(), admin, HostInput{
		Name: "outside", Host: "10.0.0.2", Environment: &dev,
		AuthType: model.LinuxAuthTypePassword, CredentialGroupID: &groupID,
	})
	if !errors.Is(err, ErrCredentialGroupScope) {
		t.Fatalf("out-of-scope CreateHost() error = %v", err)
	}
	created, err := service.CreateHost(context.Background(), admin, HostInput{
		Name: "inside", Host: "10.0.0.3", Environment: &prod,
		AuthType: model.LinuxAuthTypePassword, CredentialGroupID: &groupID, Enabled: true,
	})
	if err != nil || created.CredentialGroupID == nil || !created.CredentialConfigured {
		t.Fatalf("group CreateHost() = %+v, error = %v", created, err)
	}
}

func TestNormalUserCannotConfigureHostOrCredentialGroup(t *testing.T) {
	service := NewService(newFakeRepository(), testCredentialManager(t), "v1")
	user := &model.AppUser{ID: 2, Role: model.RoleUser}
	password, username := "secret", "ops"
	_, err := service.CreateHost(context.Background(), user, HostInput{
		Name: "forbidden", Host: "10.0.0.4", Username: &username,
		AuthType: model.LinuxAuthTypePassword, Password: &password,
	})
	if !errors.Is(err, ErrAdminRequired) {
		t.Fatalf("CreateHost(user) error = %v", err)
	}
	_, err = service.CreateCredentialGroup(context.Background(), user, CredentialGroupInput{
		Name: "forbidden", AuthType: model.LinuxAuthTypePassword, Username: username, Password: &password,
	})
	if !errors.Is(err, ErrAdminRequired) {
		t.Fatalf("CreateCredentialGroup(user) error = %v", err)
	}
}

func TestUpdateHostReplacesCredentialWithoutReturningIt(t *testing.T) {
	store := newFakeRepository()
	manager := testCredentialManager(t)
	service := NewService(store, manager, "v1")
	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	username, oldPassword := "ops", "old-password"
	created, err := service.CreateHost(context.Background(), admin, HostInput{
		Name: "before", Host: "10.0.0.8", Username: &username,
		AuthType: model.LinuxAuthTypePassword, Password: &oldPassword, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	originalCredentialID := *store.hosts[created.ID].CredentialID
	name, newPassword := "after", "new-password"
	updated, err := service.UpdateHost(context.Background(), admin, created.ID, HostUpdateInput{
		Name: &name, Password: &newPassword,
	})
	if err != nil {
		t.Fatalf("UpdateHost() error = %v", err)
	}
	if updated.Name != name || *store.hosts[created.ID].CredentialID == originalCredentialID {
		t.Fatalf("updated host = %+v", updated)
	}
	response, _ := json.Marshal(updated)
	if strings.Contains(string(response), newPassword) || strings.Contains(string(response), `"password":`) {
		t.Fatalf("updated host response leaked credential: %s", response)
	}
}

func TestUserListsOnlyEnabledHosts(t *testing.T) {
	store := newFakeRepository()
	store.hosts[1] = &model.LinuxHost{ID: 1, Name: "enabled", Host: "10.0.0.1", Enabled: true}
	store.hosts[2] = &model.LinuxHost{ID: 2, Name: "disabled", Host: "10.0.0.2", Enabled: false}
	service := NewService(store, testCredentialManager(t), "v1")
	user := &model.AppUser{ID: 2, Role: model.RoleUser}
	views, err := service.ListHosts(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].ID != 1 {
		t.Fatalf("user host list = %+v", views)
	}
}

func TestHostAttributesRejectCredentialFields(t *testing.T) {
	service := NewService(newFakeRepository(), testCredentialManager(t), "v1")
	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	username, password := "ops", "secret"
	_, err := service.CreateHost(context.Background(), admin, HostInput{
		Name: "bad-attributes", Host: "10.0.0.9", Username: &username,
		AuthType: model.LinuxAuthTypePassword, Password: &password,
		Attributes: json.RawMessage(`{"nested":{"private_key":"must-not-be-stored"}}`),
	})
	if !errors.Is(err, ErrSensitiveAttribute) {
		t.Fatalf("CreateHost(sensitive attributes) error = %v", err)
	}
}

func TestDeleteHostAlwaysSoftDeletesAndPreservesRecord(t *testing.T) {
	store := newFakeRepository()
	store.hosts[5] = &model.LinuxHost{ID: 5, Name: "historical", Host: "10.0.0.5", Enabled: true}
	service := NewService(store, testCredentialManager(t), "v1")
	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}

	if err := service.DeleteHost(context.Background(), admin, 5); err != nil {
		t.Fatalf("DeleteHost() error = %v", err)
	}
	host, exists := store.hosts[5]
	if !exists || host.DeletedAt == nil || host.Enabled {
		t.Fatalf("soft-deleted host = %+v, exists = %v", host, exists)
	}
	if _, err := store.FindLinuxHostByID(context.Background(), 5); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("FindLinuxHostByID(deleted) error = %v", err)
	}
}

func TestCredentialGroupResponseDoesNotExposeCredential(t *testing.T) {
	store := newFakeRepository()
	service := NewService(store, testCredentialManager(t), "v1")
	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	password := "shared-password"
	created, err := service.CreateCredentialGroup(context.Background(), admin, CredentialGroupInput{
		Name: "prod shared", AuthType: model.LinuxAuthTypePassword, Username: "ops",
		Password: &password, Scope: json.RawMessage(`{"environments":["prod"]}`), Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateCredentialGroup() error = %v", err)
	}
	response, _ := json.Marshal(created)
	for _, forbidden := range []string{password, `"password":`, "credentialId", "encryptedPayload"} {
		if strings.Contains(string(response), forbidden) {
			t.Fatalf("credential group response leaked %q: %s", forbidden, response)
		}
	}
}

func testCredentialManager(t *testing.T) *credential.Manager {
	t.Helper()
	manager, err := credential.NewManager("task217-test-master-key-with-at-least-32-characters", "v1")
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

type fakeRepository struct {
	hosts      map[int64]*model.LinuxHost
	groups     map[int64]*model.CredentialGroup
	secrets    map[int64]*model.CredentialSecret
	nextHost   int64
	nextGroup  int64
	nextSecret int64
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		hosts: map[int64]*model.LinuxHost{}, groups: map[int64]*model.CredentialGroup{},
		secrets: map[int64]*model.CredentialSecret{}, nextHost: 1, nextGroup: 1, nextSecret: 1,
	}
}

func (f *fakeRepository) ListLinuxHosts(_ context.Context, _ bool) ([]model.LinuxHost, error) {
	result := make([]model.LinuxHost, 0, len(f.hosts))
	for _, host := range f.hosts {
		if host.DeletedAt == nil {
			result = append(result, *host)
		}
	}
	return result, nil
}

func (f *fakeRepository) FindLinuxHostByID(_ context.Context, id int64) (*model.LinuxHost, error) {
	host, ok := f.hosts[id]
	if !ok || host.DeletedAt != nil {
		return nil, repository.ErrNotFound
	}
	copy := *host
	return &copy, nil
}

func (f *fakeRepository) CreateLinuxHost(_ context.Context, host *model.LinuxHost, secret *model.CredentialSecret) error {
	host.ID = f.nextHost
	f.nextHost++
	if secret != nil {
		secret.ID = f.nextSecret
		f.nextSecret++
		copySecret := *secret
		f.secrets[secret.ID] = &copySecret
		host.CredentialID = &secret.ID
	}
	copyHost := *host
	f.hosts[host.ID] = &copyHost
	return nil
}

func (f *fakeRepository) UpdateLinuxHost(_ context.Context, id int64, updates repository.LinuxHostUpdates, secret *model.CredentialSecret) (*model.LinuxHost, error) {
	host, ok := f.hosts[id]
	if !ok || host.DeletedAt != nil {
		return nil, repository.ErrNotFound
	}
	if updates.Name != nil {
		host.Name = *updates.Name
	}
	if updates.Host != nil {
		host.Host = *updates.Host
	}
	if updates.Port != nil {
		host.Port = *updates.Port
	}
	if updates.EnvironmentSet {
		host.Environment = updates.Environment
	}
	if updates.SystemNameSet {
		host.SystemName = updates.SystemName
	}
	if updates.ComponentNameSet {
		host.ComponentName = updates.ComponentName
	}
	if updates.UsernameSet {
		host.Username = updates.Username
	}
	if updates.AuthType != nil {
		host.AuthType = *updates.AuthType
	}
	if updates.CredentialGroupIDSet {
		host.CredentialGroupID = updates.CredentialGroupID
	}
	if updates.ClearDirectCredential {
		host.CredentialID = nil
	}
	if secret != nil {
		secret.ID = f.nextSecret
		f.nextSecret++
		copySecret := *secret
		f.secrets[secret.ID] = &copySecret
		host.CredentialID = &secret.ID
		host.CredentialGroupID = nil
	}
	if updates.Enabled != nil {
		host.Enabled = *updates.Enabled
	}
	copy := *host
	return &copy, nil
}

func (f *fakeRepository) SetLinuxHostEnabled(ctx context.Context, id int64, enabled bool) (*model.LinuxHost, error) {
	return f.UpdateLinuxHost(ctx, id, repository.LinuxHostUpdates{Enabled: &enabled}, nil)
}

func (f *fakeRepository) SoftDeleteLinuxHost(_ context.Context, id int64) error {
	host, ok := f.hosts[id]
	if !ok || host.DeletedAt != nil {
		return repository.ErrNotFound
	}
	now := time.Now().UTC()
	host.DeletedAt = &now
	host.Enabled = false
	return nil
}

func (f *fakeRepository) ListCredentialGroups(_ context.Context, _ bool) ([]model.CredentialGroup, error) {
	result := make([]model.CredentialGroup, 0, len(f.groups))
	for _, group := range f.groups {
		result = append(result, *group)
	}
	return result, nil
}

func (f *fakeRepository) FindCredentialGroupByID(_ context.Context, id int64) (*model.CredentialGroup, error) {
	group, ok := f.groups[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	copy := *group
	return &copy, nil
}

func (f *fakeRepository) CreateCredentialGroup(_ context.Context, group *model.CredentialGroup, secret *model.CredentialSecret) error {
	group.ID = f.nextGroup
	f.nextGroup++
	secret.ID = f.nextSecret
	f.nextSecret++
	copySecret := *secret
	f.secrets[secret.ID] = &copySecret
	group.CredentialID = secret.ID
	group.CredentialConfigured = true
	copy := *group
	f.groups[group.ID] = &copy
	return nil
}

func (f *fakeRepository) UpdateCredentialGroup(_ context.Context, id int64, updates repository.CredentialGroupUpdates, secret *model.CredentialSecret) (*model.CredentialGroup, error) {
	group, ok := f.groups[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	if updates.Name != nil {
		group.Name = *updates.Name
	}
	if updates.CredentialType != nil {
		group.CredentialType = *updates.CredentialType
	}
	if updates.Username != nil {
		group.Username = *updates.Username
	}
	if updates.ScopeSet {
		group.Scope = updates.Scope
	}
	if updates.Enabled != nil {
		group.Enabled = *updates.Enabled
	}
	if secret != nil {
		secret.ID = f.nextSecret
		f.nextSecret++
		copySecret := *secret
		f.secrets[secret.ID] = &copySecret
		group.CredentialID = secret.ID
	}
	group.Version++
	copy := *group
	return &copy, nil
}

func (f *fakeRepository) DeleteCredentialGroup(_ context.Context, id int64) error {
	if _, ok := f.groups[id]; !ok {
		return repository.ErrNotFound
	}
	delete(f.groups, id)
	return nil
}
