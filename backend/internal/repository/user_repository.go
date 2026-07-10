package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
)

var ErrNotFound = errors.New("record not found")

// UserRepository owns database access for users and login audits.
type UserRepository interface {
	FindByID(ctx context.Context, id int64) (*model.AppUser, error)
	FindByUsername(ctx context.Context, username string) (*model.AppUser, error)
	CreateUser(ctx context.Context, user *model.AppUser) error
	RecordLogin(ctx context.Context, audit *model.LoginAudit, lastLoginAt *time.Time) error
	UpdatePassword(ctx context.Context, userID int64, passwordHash string, changedAt time.Time) error
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

func mapRepositoryError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}
