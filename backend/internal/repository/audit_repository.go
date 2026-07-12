package repository

import (
	"context"
	"fmt"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
)

type AuditLogRepository interface {
	CreateAuditLog(ctx context.Context, log *model.AuditLog) error
	ListAuditLogs(ctx context.Context, filters AuditLogFilters) ([]model.AuditLog, error)
}

type AuditLogFilters struct {
	Limit     int
	RequestID string
	UserID    *int64
	Action    string
	Resource  string
}

type GORMAuditLogRepository struct {
	db *gorm.DB
}

func NewAuditLogRepository(db *gorm.DB) *GORMAuditLogRepository {
	return &GORMAuditLogRepository{db: db}
}

func (r *GORMAuditLogRepository) CreateAuditLog(ctx context.Context, log *model.AuditLog) error {
	if err := r.db.WithContext(ctx).Create(log).Error; err != nil {
		return fmt.Errorf("create audit log: %w", err)
	}
	return nil
}

func (r *GORMAuditLogRepository) ListAuditLogs(ctx context.Context, filters AuditLogFilters) ([]model.AuditLog, error) {
	limit := filters.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := r.db.WithContext(ctx).Order("created_at DESC, id DESC").Limit(limit)
	if filters.RequestID != "" {
		query = query.Where("request_id = ?", filters.RequestID)
	}
	if filters.UserID != nil {
		query = query.Where("user_id = ?", *filters.UserID)
	}
	if filters.Action != "" {
		query = query.Where("action = ?", filters.Action)
	}
	if filters.Resource != "" {
		query = query.Where("resource = ?", filters.Resource)
	}
	var logs []model.AuditLog
	if err := query.Find(&logs).Error; err != nil {
		return nil, fmt.Errorf("list audit logs: %w", err)
	}
	return logs, nil
}
