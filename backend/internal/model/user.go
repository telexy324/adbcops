package model

import "time"

const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// AppUser maps to the app_user table. PasswordHash must never be serialized.
type AppUser struct {
	ID                int64      `gorm:"column:id;primaryKey" json:"id"`
	Username          string     `gorm:"column:username;size:100;not null;uniqueIndex" json:"username"`
	PasswordHash      string     `gorm:"column:password_hash;not null" json:"-"`
	DisplayName       *string    `gorm:"column:display_name;size:120" json:"displayName,omitempty"`
	Role              string     `gorm:"column:role;size:30;not null" json:"role"`
	Enabled           bool       `gorm:"column:enabled;not null" json:"enabled"`
	PasswordChangedAt *time.Time `gorm:"column:password_changed_at" json:"passwordChangedAt,omitempty"`
	LastLoginAt       *time.Time `gorm:"column:last_login_at" json:"lastLoginAt,omitempty"`
	CreatedAt         time.Time  `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt         time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (AppUser) TableName() string {
	return "app_user"
}

// LoginAudit records every successful and failed login attempt.
type LoginAudit struct {
	ID            int64     `gorm:"column:id;primaryKey"`
	UserID        *int64    `gorm:"column:user_id"`
	Username      string    `gorm:"column:username;size:100"`
	Success       bool      `gorm:"column:success;not null"`
	ClientIP      string    `gorm:"column:client_ip;size:100"`
	UserAgent     string    `gorm:"column:user_agent"`
	FailureReason *string   `gorm:"column:failure_reason"`
	CreatedAt     time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (LoginAudit) TableName() string {
	return "login_audit"
}
