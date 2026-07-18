package repository

import (
	"context"
	"errors"
	"fmt"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
)

var ErrConflictingLinuxHostCredentials = errors.New("linux host cannot use a direct credential and a credential group together")

type LinuxHostRepository interface {
	ListLinuxHosts(ctx context.Context, includeDeleted bool) ([]model.LinuxHost, error)
	FindLinuxHostByID(ctx context.Context, id int64) (*model.LinuxHost, error)
	CreateLinuxHost(ctx context.Context, host *model.LinuxHost, credential *model.CredentialSecret) error
	ListCredentialGroups(ctx context.Context, enabledOnly bool) ([]model.CredentialGroup, error)
	FindCredentialGroupByID(ctx context.Context, id int64) (*model.CredentialGroup, error)
	CreateCredentialGroup(ctx context.Context, group *model.CredentialGroup, credential *model.CredentialSecret) error
	ListLinuxHostGroups(ctx context.Context) ([]model.LinuxHostGroup, error)
	FindLinuxHostGroupByID(ctx context.Context, id int64) (*model.LinuxHostGroup, error)
	CreateLinuxHostGroup(ctx context.Context, group *model.LinuxHostGroup) error
	AddLinuxHostGroupMember(ctx context.Context, member *model.LinuxHostGroupMember) error
	RemoveLinuxHostGroupMember(ctx context.Context, groupID, hostID int64) error
	ListLinuxHostProfiles(ctx context.Context, enabledOnly bool) ([]model.LinuxHostProfile, error)
	FindLinuxHostProfileByID(ctx context.Context, id int64) (*model.LinuxHostProfile, error)
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
			return fmt.Errorf("create linux host: %w", err)
		}
		return nil
	})
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
			return fmt.Errorf("create credential group: %w", err)
		}
		group.CredentialConfigured = true
		return nil
	})
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
