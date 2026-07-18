package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"aiops-platform/backend/internal/model"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

var (
	ErrConflictingLinuxHostCredentials = errors.New("linux host cannot use a direct credential and a credential group together")
	ErrLinuxResourceConflict           = errors.New("linux resource conflicts with an existing record")
	ErrCredentialGroupReferenced       = errors.New("credential group is referenced by a linux host")
)

type LinuxHostRepository interface {
	ListLinuxHosts(ctx context.Context, includeDeleted bool) ([]model.LinuxHost, error)
	FindLinuxHostByID(ctx context.Context, id int64) (*model.LinuxHost, error)
	CreateLinuxHost(ctx context.Context, host *model.LinuxHost, credential *model.CredentialSecret) error
	UpdateLinuxHost(ctx context.Context, id int64, updates LinuxHostUpdates, credential *model.CredentialSecret) (*model.LinuxHost, error)
	SetLinuxHostEnabled(ctx context.Context, id int64, enabled bool) (*model.LinuxHost, error)
	SoftDeleteLinuxHost(ctx context.Context, id int64) error
	ListCredentialGroups(ctx context.Context, enabledOnly bool) ([]model.CredentialGroup, error)
	FindCredentialGroupByID(ctx context.Context, id int64) (*model.CredentialGroup, error)
	CreateCredentialGroup(ctx context.Context, group *model.CredentialGroup, credential *model.CredentialSecret) error
	UpdateCredentialGroup(ctx context.Context, id int64, updates CredentialGroupUpdates, credential *model.CredentialSecret) (*model.CredentialGroup, error)
	DeleteCredentialGroup(ctx context.Context, id int64) error
	ListLinuxHostGroups(ctx context.Context) ([]model.LinuxHostGroup, error)
	FindLinuxHostGroupByID(ctx context.Context, id int64) (*model.LinuxHostGroup, error)
	CreateLinuxHostGroup(ctx context.Context, group *model.LinuxHostGroup) error
	AddLinuxHostGroupMember(ctx context.Context, member *model.LinuxHostGroupMember) error
	RemoveLinuxHostGroupMember(ctx context.Context, groupID, hostID int64) error
	ListLinuxHostProfiles(ctx context.Context, enabledOnly bool) ([]model.LinuxHostProfile, error)
	FindLinuxHostProfileByID(ctx context.Context, id int64) (*model.LinuxHostProfile, error)
}

type LinuxHostUpdates struct {
	Name                  *string
	Host                  *string
	Port                  *int
	Environment           *string
	EnvironmentSet        bool
	SystemName            *string
	SystemNameSet         bool
	ComponentName         *string
	ComponentNameSet      bool
	Username              *string
	UsernameSet           bool
	AuthType              *string
	CredentialGroupID     *int64
	CredentialGroupIDSet  bool
	ClearDirectCredential bool
	HostKeyPolicy         *string
	HostKeyFingerprint    *string
	HostKeyFingerprintSet bool
	ProfileID             *int64
	ProfileIDSet          bool
	Tags                  []byte
	TagsSet               bool
	Attributes            []byte
	AttributesSet         bool
	Enabled               *bool
}

type CredentialGroupUpdates struct {
	Name           *string
	CredentialType *string
	Username       *string
	Scope          []byte
	ScopeSet       bool
	Enabled        *bool
}

type GORMLinuxHostRepository struct {
	db *gorm.DB
}

func NewLinuxHostRepository(db *gorm.DB) *GORMLinuxHostRepository {
	return &GORMLinuxHostRepository{db: db}
}

// ListLinuxHosts intentionally never preloads Credential. CredentialID is also
// excluded from JSON by the model so host reads cannot expose secret references.
func (r *GORMLinuxHostRepository) ListLinuxHosts(ctx context.Context, includeDeleted bool) ([]model.LinuxHost, error) {
	var hosts []model.LinuxHost
	query := r.db.WithContext(ctx).Order("id ASC")
	if !includeDeleted {
		query = query.Where("deleted_at IS NULL")
	}
	if err := query.Find(&hosts).Error; err != nil {
		return nil, fmt.Errorf("list linux hosts: %w", err)
	}
	return hosts, nil
}

func (r *GORMLinuxHostRepository) FindLinuxHostByID(ctx context.Context, id int64) (*model.LinuxHost, error) {
	var host model.LinuxHost
	if err := r.db.WithContext(ctx).Where("deleted_at IS NULL").First(&host, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &host, nil
}

func (r *GORMLinuxHostRepository) CreateLinuxHost(ctx context.Context, host *model.LinuxHost, credential *model.CredentialSecret) error {
	if credential != nil && host.CredentialGroupID != nil {
		return ErrConflictingLinuxHostCredentials
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if credential != nil {
			if err := tx.Create(credential).Error; err != nil {
				return fmt.Errorf("create linux host credential: %w", err)
			}
			host.CredentialID = &credential.ID
		}
		if err := tx.Omit("Credential").Create(host).Error; err != nil {
			return fmt.Errorf("create linux host: %w", mapLinuxMutationError(err, false))
		}
		return nil
	})
}

func (r *GORMLinuxHostRepository) UpdateLinuxHost(ctx context.Context, id int64, updates LinuxHostUpdates, credential *model.CredentialSecret) (*model.LinuxHost, error) {
	var updated model.LinuxHost
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		values := map[string]any{"updated_at": time.Now().UTC()}
		if updates.Name != nil {
			values["name"] = *updates.Name
		}
		if updates.Host != nil {
			values["host"] = *updates.Host
		}
		if updates.Port != nil {
			values["port"] = *updates.Port
		}
		if updates.EnvironmentSet {
			values["environment"] = updates.Environment
		}
		if updates.SystemNameSet {
			values["system_name"] = updates.SystemName
		}
		if updates.ComponentNameSet {
			values["component_name"] = updates.ComponentName
		}
		if updates.UsernameSet {
			values["username"] = updates.Username
		}
		if updates.AuthType != nil {
			values["auth_type"] = *updates.AuthType
		}
		if updates.CredentialGroupIDSet {
			values["credential_group_id"] = updates.CredentialGroupID
		}
		if updates.ClearDirectCredential {
			values["credential_id"] = nil
		}
		if credential != nil {
			if err := tx.Create(credential).Error; err != nil {
				return fmt.Errorf("create linux host credential: %w", err)
			}
			values["credential_id"] = credential.ID
			values["credential_group_id"] = nil
		}
		if updates.HostKeyPolicy != nil {
			values["host_key_policy"] = *updates.HostKeyPolicy
		}
		if updates.HostKeyFingerprintSet {
			values["host_key_fingerprint"] = updates.HostKeyFingerprint
		}
		if updates.ProfileIDSet {
			values["profile_id"] = updates.ProfileID
		}
		if updates.TagsSet {
			values["tags"] = updates.Tags
		}
		if updates.AttributesSet {
			values["attributes"] = updates.Attributes
		}
		if updates.Enabled != nil {
			values["enabled"] = *updates.Enabled
		}
		result := tx.Model(&model.LinuxHost{}).Where("id = ? AND deleted_at IS NULL", id).Updates(values)
		if result.Error != nil {
			return fmt.Errorf("update linux host: %w", mapLinuxMutationError(result.Error, false))
		}
		if result.RowsAffected != 1 {
			return ErrNotFound
		}
		if err := tx.Where("deleted_at IS NULL").First(&updated, id).Error; err != nil {
			return mapRepositoryError(err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (r *GORMLinuxHostRepository) SetLinuxHostEnabled(ctx context.Context, id int64, enabled bool) (*model.LinuxHost, error) {
	return r.UpdateLinuxHost(ctx, id, LinuxHostUpdates{Enabled: &enabled}, nil)
}

func (r *GORMLinuxHostRepository) SoftDeleteLinuxHost(ctx context.Context, id int64) error {
	now := time.Now().UTC()
	result := r.db.WithContext(ctx).Model(&model.LinuxHost{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Updates(map[string]any{"deleted_at": now, "enabled": false, "updated_at": now})
	if result.Error != nil {
		return fmt.Errorf("soft delete linux host: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}

func (r *GORMLinuxHostRepository) ListCredentialGroups(ctx context.Context, enabledOnly bool) ([]model.CredentialGroup, error) {
	var groups []model.CredentialGroup
	query := r.db.WithContext(ctx).Order("id ASC")
	if enabledOnly {
		query = query.Where("enabled = ?", true)
	}
	if err := query.Find(&groups).Error; err != nil {
		return nil, fmt.Errorf("list credential groups: %w", err)
	}
	markCredentialGroupsConfigured(groups)
	return groups, nil
}

func (r *GORMLinuxHostRepository) FindCredentialGroupByID(ctx context.Context, id int64) (*model.CredentialGroup, error) {
	var group model.CredentialGroup
	if err := r.db.WithContext(ctx).First(&group, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	group.CredentialConfigured = group.CredentialID > 0
	return &group, nil
}

func (r *GORMLinuxHostRepository) CreateCredentialGroup(ctx context.Context, group *model.CredentialGroup, credential *model.CredentialSecret) error {
	if err := model.ValidateCredentialGroupScope(group.Scope); err != nil {
		return err
	}
	if credential == nil {
		return errors.New("credential group requires an encrypted credential secret")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(credential).Error; err != nil {
			return fmt.Errorf("create credential group secret: %w", err)
		}
		group.CredentialID = credential.ID
		if err := tx.Omit("Credential").Create(group).Error; err != nil {
			return fmt.Errorf("create credential group: %w", mapLinuxMutationError(err, false))
		}
		group.CredentialConfigured = true
		return nil
	})
}

func (r *GORMLinuxHostRepository) UpdateCredentialGroup(ctx context.Context, id int64, updates CredentialGroupUpdates, credential *model.CredentialSecret) (*model.CredentialGroup, error) {
	if updates.ScopeSet {
		if err := model.ValidateCredentialGroupScope(updates.Scope); err != nil {
			return nil, err
		}
	}
	var updated model.CredentialGroup
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		values := map[string]any{"updated_at": time.Now().UTC(), "version": gorm.Expr("version + 1")}
		if updates.Name != nil {
			values["name"] = *updates.Name
		}
		if updates.CredentialType != nil {
			values["credential_type"] = *updates.CredentialType
		}
		if updates.Username != nil {
			values["username"] = *updates.Username
		}
		if updates.ScopeSet {
			values["scope"] = updates.Scope
		}
		if updates.Enabled != nil {
			values["enabled"] = *updates.Enabled
		}
		if credential != nil {
			if err := tx.Create(credential).Error; err != nil {
				return fmt.Errorf("create credential group secret: %w", err)
			}
			values["credential_id"] = credential.ID
		}
		result := tx.Model(&model.CredentialGroup{}).Where("id = ?", id).Updates(values)
		if result.Error != nil {
			return fmt.Errorf("update credential group: %w", mapLinuxMutationError(result.Error, false))
		}
		if result.RowsAffected != 1 {
			return ErrNotFound
		}
		if err := tx.First(&updated, id).Error; err != nil {
			return mapRepositoryError(err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	updated.CredentialConfigured = updated.CredentialID > 0
	return &updated, nil
}

func (r *GORMLinuxHostRepository) DeleteCredentialGroup(ctx context.Context, id int64) error {
	result := r.db.WithContext(ctx).Delete(&model.CredentialGroup{}, id)
	if result.Error != nil {
		return fmt.Errorf("delete credential group: %w", mapLinuxMutationError(result.Error, true))
	}
	if result.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}

func (r *GORMLinuxHostRepository) ListLinuxHostGroups(ctx context.Context) ([]model.LinuxHostGroup, error) {
	var groups []model.LinuxHostGroup
	if err := r.db.WithContext(ctx).Order("id ASC").Find(&groups).Error; err != nil {
		return nil, fmt.Errorf("list linux host groups: %w", err)
	}
	return groups, nil
}

func (r *GORMLinuxHostRepository) FindLinuxHostGroupByID(ctx context.Context, id int64) (*model.LinuxHostGroup, error) {
	var group model.LinuxHostGroup
	if err := r.db.WithContext(ctx).First(&group, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &group, nil
}

func (r *GORMLinuxHostRepository) CreateLinuxHostGroup(ctx context.Context, group *model.LinuxHostGroup) error {
	if err := r.db.WithContext(ctx).Omit("Members").Create(group).Error; err != nil {
		return fmt.Errorf("create linux host group: %w", err)
	}
	return nil
}

func (r *GORMLinuxHostRepository) AddLinuxHostGroupMember(ctx context.Context, member *model.LinuxHostGroupMember) error {
	if err := r.db.WithContext(ctx).Create(member).Error; err != nil {
		return fmt.Errorf("add linux host group member: %w", err)
	}
	return nil
}

func (r *GORMLinuxHostRepository) RemoveLinuxHostGroupMember(ctx context.Context, groupID, hostID int64) error {
	result := r.db.WithContext(ctx).Where("group_id = ? AND host_id = ?", groupID, hostID).Delete(&model.LinuxHostGroupMember{})
	if result.Error != nil {
		return fmt.Errorf("remove linux host group member: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}

func (r *GORMLinuxHostRepository) ListLinuxHostProfiles(ctx context.Context, enabledOnly bool) ([]model.LinuxHostProfile, error) {
	var profiles []model.LinuxHostProfile
	query := r.db.WithContext(ctx).Order("id ASC")
	if enabledOnly {
		query = query.Where("enabled = ?", true)
	}
	if err := query.Find(&profiles).Error; err != nil {
		return nil, fmt.Errorf("list linux host profiles: %w", err)
	}
	return profiles, nil
}

func (r *GORMLinuxHostRepository) FindLinuxHostProfileByID(ctx context.Context, id int64) (*model.LinuxHostProfile, error) {
	var profile model.LinuxHostProfile
	if err := r.db.WithContext(ctx).First(&profile, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &profile, nil
}

func markCredentialGroupsConfigured(groups []model.CredentialGroup) {
	for i := range groups {
		groups[i].CredentialConfigured = groups[i].CredentialID > 0
	}
}

func mapLinuxMutationError(err error, credentialGroupDelete bool) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return fmt.Errorf("%w: %s", ErrLinuxResourceConflict, postgresError.ConstraintName)
		case "23503":
			if credentialGroupDelete {
				return fmt.Errorf("%w: %s", ErrCredentialGroupReferenced, postgresError.ConstraintName)
			}
		}
	}
	return err
}
