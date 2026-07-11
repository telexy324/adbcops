package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrNotFound  = errors.New("record not found")
	ErrLastAdmin = errors.New("cannot disable or demote the last enabled admin")
)

// UserRepository owns database access for users and login audits.
type UserRepository interface {
	FindByID(ctx context.Context, id int64) (*model.AppUser, error)
	FindByUsername(ctx context.Context, username string) (*model.AppUser, error)
	CreateUser(ctx context.Context, user *model.AppUser) error
	RecordLogin(ctx context.Context, audit *model.LoginAudit, lastLoginAt *time.Time) error
	UpdatePassword(ctx context.Context, userID int64, passwordHash string, changedAt time.Time) error
}

type UserUpdates struct {
	DisplayName    *string
	DisplayNameSet bool
	Role           *string
}

type GORMUserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) *GORMUserRepository {
	return &GORMUserRepository{db: db}
}

func (r *GORMUserRepository) FindByID(ctx context.Context, id int64) (*model.AppUser, error) {
	var user model.AppUser
	if err := r.db.WithContext(ctx).First(&user, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &user, nil
}

func (r *GORMUserRepository) FindByUsername(ctx context.Context, username string) (*model.AppUser, error) {
	var user model.AppUser
	if err := r.db.WithContext(ctx).Where("username = ?", username).First(&user).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &user, nil
}

func (r *GORMUserRepository) CreateUser(ctx context.Context, user *model.AppUser) error {
	if err := r.db.WithContext(ctx).Create(user).Error; err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

func (r *GORMUserRepository) ListUsers(ctx context.Context) ([]model.AppUser, error) {
	var users []model.AppUser
	if err := r.db.WithContext(ctx).Order("id ASC").Find(&users).Error; err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return users, nil
}

func (r *GORMUserRepository) UpdateUser(ctx context.Context, userID int64, updates UserUpdates) (*model.AppUser, error) {
	var updated model.AppUser
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user model.AppUser
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, userID).Error; err != nil {
			return mapRepositoryError(err)
		}
		if updates.Role != nil && user.Role == model.RoleAdmin && *updates.Role != model.RoleAdmin && user.Enabled {
			if err := ensureMoreThanOneEnabledAdmin(tx); err != nil {
				return err
			}
		}

		values := map[string]any{"updated_at": time.Now().UTC()}
		if updates.DisplayNameSet {
			values["display_name"] = updates.DisplayName
		}
		if updates.Role != nil {
			values["role"] = *updates.Role
		}
		if len(values) > 1 {
			if err := tx.Model(&model.AppUser{}).Where("id = ?", userID).Updates(values).Error; err != nil {
				return fmt.Errorf("update user: %w", err)
			}
		}
		if err := tx.First(&updated, userID).Error; err != nil {
			return mapRepositoryError(err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (r *GORMUserRepository) SetUserEnabled(ctx context.Context, userID int64, enabled bool) (*model.AppUser, error) {
	var updated model.AppUser
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user model.AppUser
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, userID).Error; err != nil {
			return mapRepositoryError(err)
		}
		if !enabled && user.Enabled && user.Role == model.RoleAdmin {
			if err := ensureMoreThanOneEnabledAdmin(tx); err != nil {
				return err
			}
		}
		now := time.Now().UTC()
		if err := tx.Model(&model.AppUser{}).
			Where("id = ?", userID).
			Updates(map[string]any{"enabled": enabled, "updated_at": now}).Error; err != nil {
			return fmt.Errorf("set user enabled: %w", err)
		}
		if err := tx.First(&updated, userID).Error; err != nil {
			return mapRepositoryError(err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (r *GORMUserRepository) RecordLogin(ctx context.Context, audit *model.LoginAudit, lastLoginAt *time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if lastLoginAt != nil && audit.UserID != nil {
			result := tx.Model(&model.AppUser{}).
				Where("id = ?", *audit.UserID).
				Updates(map[string]any{"last_login_at": *lastLoginAt, "updated_at": *lastLoginAt})
			if result.Error != nil {
				return fmt.Errorf("update last login: %w", result.Error)
			}
			if result.RowsAffected != 1 {
				return ErrNotFound
			}
		}
		if err := tx.Create(audit).Error; err != nil {
			return fmt.Errorf("create login audit: %w", err)
		}
		return nil
	})
}

func (r *GORMUserRepository) UpdatePassword(ctx context.Context, userID int64, passwordHash string, changedAt time.Time) error {
	result := r.db.WithContext(ctx).Model(&model.AppUser{}).
		Where("id = ?", userID).
		Updates(map[string]any{
			"password_hash":       passwordHash,
			"password_changed_at": changedAt,
			"updated_at":          changedAt,
		})
	if result.Error != nil {
		return fmt.Errorf("update password: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}

func ensureMoreThanOneEnabledAdmin(tx *gorm.DB) error {
	var admins []model.AppUser
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("role = ? AND enabled = ?", model.RoleAdmin, true).
		Find(&admins).Error; err != nil {
		return fmt.Errorf("count enabled admins: %w", err)
	}
	if len(admins) <= 1 {
		return ErrLastAdmin
	}
	return nil
}

func mapRepositoryError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}
