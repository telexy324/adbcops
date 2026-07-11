package user

import (
	"context"
	"errors"
	"testing"
	"time"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

func TestCreateDefaultsToEnabledNormalUser(t *testing.T) {
	store := newFakeRepository()
	service := newTestService(t, store)

	created, err := service.Create(context.Background(), CreateInput{
		Username: "operator",
		Password: "operator-password",
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Role != model.RoleUser || !created.Enabled {
		t.Fatalf("created user = %+v", created)
	}
	if bcrypt.CompareHashAndPassword([]byte(created.PasswordHash), []byte("operator-password")) != nil {
		t.Fatal("created user password was not hashed correctly")
	}
}

func TestSetEnabledRejectsLastEnabledAdmin(t *testing.T) {
	store := newFakeRepository()
	service := newTestService(t, store)
	admin := store.addUser("admin", model.RoleAdmin, true)

	if _, err := service.SetEnabled(context.Background(), admin.ID, false); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("SetEnabled() error = %v, want ErrLastAdmin", err)
	}

	store.addUser("admin2", model.RoleAdmin, true)
	if _, err := service.SetEnabled(context.Background(), admin.ID, false); err != nil {
		t.Fatalf("SetEnabled() with second admin error = %v", err)
	}
	if store.usersByID[admin.ID].Enabled {
		t.Fatal("admin was not disabled")
	}
}

func TestUpdateRejectsDemotingLastEnabledAdmin(t *testing.T) {
	store := newFakeRepository()
	service := newTestService(t, store)
	admin := store.addUser("admin", model.RoleAdmin, true)
	role := model.RoleUser

	if _, err := service.Update(context.Background(), admin.ID, UpdateInput{Role: &role}); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("Update() error = %v, want ErrLastAdmin", err)
	}
}

func newTestService(t *testing.T, store *fakeRepository) *Service {
	t.Helper()
	service, err := NewService(store, bcrypt.MinCost)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

type fakeRepository struct {
	nextID          int64
	usersByID       map[int64]*model.AppUser
	usersByUsername map[string]*model.AppUser
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		nextID:          1,
		usersByID:       make(map[int64]*model.AppUser),
		usersByUsername: make(map[string]*model.AppUser),
	}
}

func (f *fakeRepository) addUser(username, role string, enabled bool) *model.AppUser {
	user := &model.AppUser{Username: username, Role: role, Enabled: enabled}
	_ = f.CreateUser(context.Background(), user)
	return user
}

func (f *fakeRepository) FindByID(_ context.Context, id int64) (*model.AppUser, error) {
	user, ok := f.usersByID[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return user, nil
}

func (f *fakeRepository) FindByUsername(_ context.Context, username string) (*model.AppUser, error) {
	user, ok := f.usersByUsername[username]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return user, nil
}

func (f *fakeRepository) CreateUser(_ context.Context, user *model.AppUser) error {
	user.ID = f.nextID
	f.nextID++
	f.usersByID[user.ID] = user
	f.usersByUsername[user.Username] = user
	return nil
}

func (f *fakeRepository) ListUsers(_ context.Context) ([]model.AppUser, error) {
	users := make([]model.AppUser, 0, len(f.usersByID))
	for _, user := range f.usersByID {
		users = append(users, *user)
	}
	return users, nil
}

func (f *fakeRepository) UpdateUser(_ context.Context, userID int64, updates repository.UserUpdates) (*model.AppUser, error) {
	user, ok := f.usersByID[userID]
	if !ok {
		return nil, repository.ErrNotFound
	}
	if updates.Role != nil && user.Role == model.RoleAdmin && *updates.Role != model.RoleAdmin && user.Enabled {
		if f.enabledAdminCount() <= 1 {
			return nil, repository.ErrLastAdmin
		}
	}
	if updates.DisplayNameSet {
		user.DisplayName = updates.DisplayName
	}
	if updates.Role != nil {
		user.Role = *updates.Role
	}
	return user, nil
}

func (f *fakeRepository) SetUserEnabled(_ context.Context, userID int64, enabled bool) (*model.AppUser, error) {
	user, ok := f.usersByID[userID]
	if !ok {
		return nil, repository.ErrNotFound
	}
	if !enabled && user.Enabled && user.Role == model.RoleAdmin && f.enabledAdminCount() <= 1 {
		return nil, repository.ErrLastAdmin
	}
	user.Enabled = enabled
	return user, nil
}

func (f *fakeRepository) UpdatePassword(_ context.Context, userID int64, passwordHash string, changedAt time.Time) error {
	user, ok := f.usersByID[userID]
	if !ok {
		return repository.ErrNotFound
	}
	user.PasswordHash = passwordHash
	user.PasswordChangedAt = &changedAt
	return nil
}

func (f *fakeRepository) enabledAdminCount() int {
	count := 0
	for _, user := range f.usersByID {
		if user.Role == model.RoleAdmin && user.Enabled {
			count++
		}
	}
	return count
}
