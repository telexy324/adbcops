package model

import "time"

const (
	AuditActionAPI        = "api"
	AuditActionManagement = "management"
	AuditActionSecurity   = "security"
)

type AuditLog struct {
	ID         int64     `gorm:"column:id;primaryKey" json:"id"`
	RequestID  string    `gorm:"column:request_id;size:160;not null" json:"requestId"`
	UserID     *int64    `gorm:"column:user_id" json:"userId,omitempty"`
	Username   *string   `gorm:"column:username;size:120" json:"username,omitempty"`
	Method     string    `gorm:"column:method;size:12;not null" json:"method"`
	Path       string    `gorm:"column:path;size:300;not null" json:"path"`
	Route      string    `gorm:"column:route;size:300" json:"route,omitempty"`
	Action     string    `gorm:"column:action;size:60;not null" json:"action"`
	Resource   string    `gorm:"column:resource;size:120;not null" json:"resource"`
	StatusCode int       `gorm:"column:status_code;not null" json:"statusCode"`
	ClientIP   string    `gorm:"column:client_ip;size:80" json:"clientIp,omitempty"`
	UserAgent  string    `gorm:"column:user_agent;size:300" json:"userAgent,omitempty"`
	Metadata   []byte    `gorm:"column:metadata;type:jsonb" json:"metadata,omitempty"`
	ErrorCount int       `gorm:"column:error_count;not null" json:"errorCount"`
	DurationMS int64     `gorm:"column:duration_ms;not null" json:"durationMs"`
	CreatedAt  time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
}

func (AuditLog) TableName() string {
	return "audit_log"
}
