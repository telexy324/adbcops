package repository

import (
	"context"
	"fmt"
	"time"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type EventRepository interface {
	UpsertOpsEvent(ctx context.Context, event *model.OpsEvent) (*model.OpsEvent, error)
	FindOpsEventByFingerprint(ctx context.Context, fingerprint string) (*model.OpsEvent, error)
	FindOpsEventByID(ctx context.Context, id int64) (*model.OpsEvent, error)
	ListOpsEvents(ctx context.Context, filters EventFilters) ([]model.OpsEvent, error)
}

type EventFilters struct {
	Limit         int
	SourceType    string
	Status        string
	Environment   string
	SystemName    string
	ComponentName string
	Namespace     string
	ResourceName  string
	From          *time.Time
	To            *time.Time
}

type GORMEventRepository struct {
	db *gorm.DB
}

func NewEventRepository(db *gorm.DB) *GORMEventRepository {
	return &GORMEventRepository{db: db}
}

func (r *GORMEventRepository) UpsertOpsEvent(ctx context.Context, event *model.OpsEvent) (*model.OpsEvent, error) {
	now := time.Now().UTC()
	if event.FirstSeenAt.IsZero() {
		event.FirstSeenAt = now
	}
	event.LastSeenAt = now
	event.UpdatedAt = now
	if event.OccurrenceCount <= 0 {
		event.OccurrenceCount = 1
	}
	if event.Status == model.EventStatusResolved && event.ResolvedAt == nil {
		event.ResolvedAt = &now
	}
	if event.Fingerprint == nil || *event.Fingerprint == "" {
		if err := r.db.WithContext(ctx).Create(event).Error; err != nil {
			return nil, fmt.Errorf("create ops event: %w", err)
		}
		return event, nil
	}
	assignments := clause.Assignments(map[string]any{
		"event_time":       event.EventTime,
		"source_type":      event.SourceType,
		"source_id":        event.SourceID,
		"event_type":       event.EventType,
		"severity":         event.Severity,
		"status":           event.Status,
		"environment":      event.Environment,
		"system_name":      event.SystemName,
		"component_name":   event.ComponentName,
		"cluster":          event.Cluster,
		"namespace":        event.Namespace,
		"resource_kind":    event.ResourceKind,
		"resource_name":    event.ResourceName,
		"host":             event.Host,
		"trace_id":         event.TraceID,
		"summary":          event.Summary,
		"payload":          event.Payload,
		"last_seen_at":     now,
		"updated_at":       now,
		"occurrence_count": gorm.Expr("ops_event.occurrence_count + 1"),
		"resolved_at":      event.ResolvedAt,
	})
	if event.Status != model.EventStatusResolved {
		assignments = append(assignments, clause.Assignment{Column: clause.Column{Name: "resolved_at"}, Value: nil})
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "fingerprint"}},
		DoUpdates: assignments,
	}).Create(event).Error; err != nil {
		return nil, fmt.Errorf("upsert ops event: %w", err)
	}
	return r.FindOpsEventByFingerprint(ctx, *event.Fingerprint)
}

func (r *GORMEventRepository) FindOpsEventByFingerprint(ctx context.Context, fingerprint string) (*model.OpsEvent, error) {
	var event model.OpsEvent
	if err := r.db.WithContext(ctx).Where("fingerprint = ?", fingerprint).First(&event).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &event, nil
}

func (r *GORMEventRepository) FindOpsEventByID(ctx context.Context, id int64) (*model.OpsEvent, error) {
	var event model.OpsEvent
	if err := r.db.WithContext(ctx).First(&event, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &event, nil
}

func (r *GORMEventRepository) ListOpsEvents(ctx context.Context, filters EventFilters) ([]model.OpsEvent, error) {
	limit := filters.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := r.db.WithContext(ctx).Order("event_time DESC, id DESC").Limit(limit)
	if filters.SourceType != "" {
		query = query.Where("source_type = ?", filters.SourceType)
	}
	if filters.Status != "" {
		query = query.Where("status = ?", filters.Status)
	}
	if filters.Environment != "" {
		query = query.Where("environment = ?", filters.Environment)
	}
	if filters.SystemName != "" {
		query = query.Where("system_name = ?", filters.SystemName)
	}
	if filters.ComponentName != "" {
		query = query.Where("component_name = ?", filters.ComponentName)
	}
	if filters.Namespace != "" {
		query = query.Where("namespace = ?", filters.Namespace)
	}
	if filters.ResourceName != "" {
		query = query.Where("resource_name = ?", filters.ResourceName)
	}
	if filters.From != nil {
		query = query.Where("event_time >= ?", *filters.From)
	}
	if filters.To != nil {
		query = query.Where("event_time <= ?", *filters.To)
	}
	var events []model.OpsEvent
	if err := query.Find(&events).Error; err != nil {
		return nil, fmt.Errorf("list ops events: %w", err)
	}
	return events, nil
}
