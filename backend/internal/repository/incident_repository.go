package repository

import (
	"context"
	"fmt"
	"time"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type IncidentRepository interface {
	CreateIncident(ctx context.Context, incident *model.Incident) error
	UpdateIncident(ctx context.Context, id int64, updates IncidentUpdates) (*model.Incident, error)
	FindIncidentByID(ctx context.Context, id int64) (*model.Incident, error)
	ListIncidents(ctx context.Context, filters IncidentFilters) ([]model.Incident, error)
	LinkIncidentEvents(ctx context.Context, incidentID int64, eventIDs []int64) error
	LinkIncidentEvidence(ctx context.Context, incidentID int64, keys []string) error
	CreateRootCauseCandidates(ctx context.Context, candidates []model.IncidentRootCauseCandidate) error
	ListRootCauseCandidates(ctx context.Context, incidentID int64) ([]model.IncidentRootCauseCandidate, error)
	ConfirmRootCauseCandidate(ctx context.Context, incidentID int64, candidateID int64, actorID int64) (*model.IncidentRootCauseCandidate, error)
	ListIncidentEvents(ctx context.Context, incidentID int64) ([]model.IncidentEvent, error)
	ListIncidentEvidence(ctx context.Context, incidentID int64) ([]model.IncidentEvidence, error)
	CreateIncidentActivity(ctx context.Context, activity *model.IncidentActivity) error
	ListIncidentActivities(ctx context.Context, incidentID int64) ([]model.IncidentActivity, error)
}

type IncidentUpdates struct {
	Title          *string
	Severity       *string
	Status         *string
	Environment    *string
	EnvironmentSet bool
	SystemName     *string
	SystemSet      bool
	ComponentName  *string
	ComponentSet   bool
	Summary        *string
	SummarySet     bool
	ResolvedAt     *time.Time
	ClosedAt       *time.Time
}

type IncidentFilters struct {
	Limit       int
	Status      string
	Severity    string
	Environment string
	SystemName  string
}

type GORMIncidentRepository struct {
	db *gorm.DB
}

func NewIncidentRepository(db *gorm.DB) *GORMIncidentRepository {
	return &GORMIncidentRepository{db: db}
}

func (r *GORMIncidentRepository) CreateIncident(ctx context.Context, incident *model.Incident) error {
	if err := r.db.WithContext(ctx).Create(incident).Error; err != nil {
		return fmt.Errorf("create incident: %w", err)
	}
	return nil
}

func (r *GORMIncidentRepository) UpdateIncident(ctx context.Context, id int64, updates IncidentUpdates) (*model.Incident, error) {
	values := map[string]any{"updated_at": time.Now().UTC()}
	if updates.Title != nil {
		values["title"] = *updates.Title
	}
	if updates.Severity != nil {
		values["severity"] = *updates.Severity
	}
	if updates.Status != nil {
		values["status"] = *updates.Status
	}
	if updates.EnvironmentSet {
		values["environment"] = updates.Environment
	}
	if updates.SystemSet {
		values["system_name"] = updates.SystemName
	}
	if updates.ComponentSet {
		values["component_name"] = updates.ComponentName
	}
	if updates.SummarySet {
		values["summary"] = updates.Summary
	}
	if updates.ResolvedAt != nil {
		values["resolved_at"] = *updates.ResolvedAt
	}
	if updates.ClosedAt != nil {
		values["closed_at"] = *updates.ClosedAt
	}
	result := r.db.WithContext(ctx).Model(&model.Incident{}).Where("id = ?", id).Updates(values)
	if result.Error != nil {
		return nil, fmt.Errorf("update incident: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, ErrNotFound
	}
	return r.FindIncidentByID(ctx, id)
}

func (r *GORMIncidentRepository) FindIncidentByID(ctx context.Context, id int64) (*model.Incident, error) {
	var incident model.Incident
	if err := r.db.WithContext(ctx).First(&incident, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &incident, nil
}

func (r *GORMIncidentRepository) ListIncidents(ctx context.Context, filters IncidentFilters) ([]model.Incident, error) {
	limit := filters.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := r.db.WithContext(ctx).Order("updated_at DESC, id DESC").Limit(limit)
	if filters.Status != "" {
		query = query.Where("status = ?", filters.Status)
	}
	if filters.Severity != "" {
		query = query.Where("severity = ?", filters.Severity)
	}
	if filters.Environment != "" {
		query = query.Where("environment = ?", filters.Environment)
	}
	if filters.SystemName != "" {
		query = query.Where("system_name = ?", filters.SystemName)
	}
	var incidents []model.Incident
	if err := query.Find(&incidents).Error; err != nil {
		return nil, fmt.Errorf("list incidents: %w", err)
	}
	return incidents, nil
}

func (r *GORMIncidentRepository) LinkIncidentEvents(ctx context.Context, incidentID int64, eventIDs []int64) error {
	rows := make([]model.IncidentEvent, 0, len(eventIDs))
	for _, id := range eventIDs {
		if id > 0 {
			rows = append(rows, model.IncidentEvent{IncidentID: incidentID, EventID: id})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

func (r *GORMIncidentRepository) LinkIncidentEvidence(ctx context.Context, incidentID int64, keys []string) error {
	rows := make([]model.IncidentEvidence, 0, len(keys))
	for _, key := range keys {
		if key != "" {
			rows = append(rows, model.IncidentEvidence{IncidentID: incidentID, EvidenceKey: key})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

func (r *GORMIncidentRepository) CreateRootCauseCandidates(ctx context.Context, candidates []model.IncidentRootCauseCandidate) error {
	if len(candidates) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&candidates).Error
}

func (r *GORMIncidentRepository) ListRootCauseCandidates(ctx context.Context, incidentID int64) ([]model.IncidentRootCauseCandidate, error) {
	var candidates []model.IncidentRootCauseCandidate
	if err := r.db.WithContext(ctx).Where("incident_id = ?", incidentID).Order("confirmed DESC, score DESC, id ASC").Find(&candidates).Error; err != nil {
		return nil, fmt.Errorf("list root cause candidates: %w", err)
	}
	return candidates, nil
}

func (r *GORMIncidentRepository) ConfirmRootCauseCandidate(ctx context.Context, incidentID int64, candidateID int64, actorID int64) (*model.IncidentRootCauseCandidate, error) {
	now := time.Now().UTC()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.IncidentRootCauseCandidate{}).Where("incident_id = ?", incidentID).Updates(map[string]any{"confirmed": false, "confirmed_by": nil, "confirmed_at": nil}).Error; err != nil {
			return err
		}
		result := tx.Model(&model.IncidentRootCauseCandidate{}).Where("id = ? AND incident_id = ?", candidateID, incidentID).Updates(map[string]any{
			"confirmed":    true,
			"confirmed_by": actorID,
			"confirmed_at": now,
			"updated_at":   now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	var candidate model.IncidentRootCauseCandidate
	if err := r.db.WithContext(ctx).First(&candidate, candidateID).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &candidate, nil
}

func (r *GORMIncidentRepository) ListIncidentEvents(ctx context.Context, incidentID int64) ([]model.IncidentEvent, error) {
	var rows []model.IncidentEvent
	return rows, r.db.WithContext(ctx).Where("incident_id = ?", incidentID).Find(&rows).Error
}

func (r *GORMIncidentRepository) ListIncidentEvidence(ctx context.Context, incidentID int64) ([]model.IncidentEvidence, error) {
	var rows []model.IncidentEvidence
	return rows, r.db.WithContext(ctx).Where("incident_id = ?", incidentID).Find(&rows).Error
}

func (r *GORMIncidentRepository) CreateIncidentActivity(ctx context.Context, activity *model.IncidentActivity) error {
	return r.db.WithContext(ctx).Create(activity).Error
}

func (r *GORMIncidentRepository) ListIncidentActivities(ctx context.Context, incidentID int64) ([]model.IncidentActivity, error) {
	var rows []model.IncidentActivity
	return rows, r.db.WithContext(ctx).Where("incident_id = ?", incidentID).Order("created_at ASC, id ASC").Find(&rows).Error
}
