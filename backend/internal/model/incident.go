package model

import "time"

const (
	IncidentStatusOpen       = "open"
	IncidentStatusMitigating = "mitigating"
	IncidentStatusResolved   = "resolved"
	IncidentStatusClosed     = "closed"

	IncidentSeverityCritical = "critical"
	IncidentSeverityWarning  = "warning"
	IncidentSeverityInfo     = "info"

	IncidentActivityCreate           = "create"
	IncidentActivityUpdate           = "update"
	IncidentActivityLifecycle        = "lifecycle"
	IncidentActivityConfirmRootCause = "confirm_root_cause"
)

type Incident struct {
	ID             int64      `gorm:"column:id;primaryKey" json:"id"`
	Title          string     `gorm:"column:title;size:255;not null" json:"title"`
	Severity       string     `gorm:"column:severity;size:30;not null" json:"severity"`
	Status         string     `gorm:"column:status;size:30;not null" json:"status"`
	Environment    *string    `gorm:"column:environment;size:80" json:"environment,omitempty"`
	SystemName     *string    `gorm:"column:system_name;size:120" json:"systemName,omitempty"`
	ComponentName  *string    `gorm:"column:component_name;size:120" json:"componentName,omitempty"`
	Summary        *string    `gorm:"column:summary" json:"summary,omitempty"`
	AnalysisTaskID *int64     `gorm:"column:analysis_task_id" json:"analysisTaskId,omitempty"`
	CreatedBy      *int64     `gorm:"column:created_by" json:"createdBy,omitempty"`
	ResolvedAt     *time.Time `gorm:"column:resolved_at" json:"resolvedAt,omitempty"`
	ClosedAt       *time.Time `gorm:"column:closed_at" json:"closedAt,omitempty"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (Incident) TableName() string {
	return "incident"
}

type IncidentEvent struct {
	ID         int64     `gorm:"column:id;primaryKey" json:"id"`
	IncidentID int64     `gorm:"column:incident_id;not null" json:"incidentId"`
	EventID    int64     `gorm:"column:event_id;not null" json:"eventId"`
	CreatedAt  time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
}

func (IncidentEvent) TableName() string {
	return "incident_event"
}

type IncidentEvidence struct {
	ID          int64     `gorm:"column:id;primaryKey" json:"id"`
	IncidentID  int64     `gorm:"column:incident_id;not null" json:"incidentId"`
	EvidenceKey string    `gorm:"column:evidence_key;size:100;not null" json:"evidenceKey"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
}

func (IncidentEvidence) TableName() string {
	return "incident_evidence"
}

type IncidentRootCauseCandidate struct {
	ID          int64      `gorm:"column:id;primaryKey" json:"id"`
	IncidentID  int64      `gorm:"column:incident_id;not null" json:"incidentId"`
	Summary     string     `gorm:"column:summary;not null" json:"summary"`
	Score       float64    `gorm:"column:score;not null" json:"score"`
	Details     []byte     `gorm:"column:details;type:jsonb" json:"details,omitempty"`
	Confirmed   bool       `gorm:"column:confirmed;not null" json:"confirmed"`
	ConfirmedBy *int64     `gorm:"column:confirmed_by" json:"confirmedBy,omitempty"`
	ConfirmedAt *time.Time `gorm:"column:confirmed_at" json:"confirmedAt,omitempty"`
	CreatedAt   time.Time  `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt   time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (IncidentRootCauseCandidate) TableName() string {
	return "incident_root_cause_candidate"
}

type IncidentActivity struct {
	ID         int64     `gorm:"column:id;primaryKey" json:"id"`
	IncidentID int64     `gorm:"column:incident_id;not null" json:"incidentId"`
	ActorID    *int64    `gorm:"column:actor_id" json:"actorId,omitempty"`
	Action     string    `gorm:"column:action;size:80;not null" json:"action"`
	Detail     []byte    `gorm:"column:detail;type:jsonb" json:"detail,omitempty"`
	CreatedAt  time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
}

func (IncidentActivity) TableName() string {
	return "incident_activity"
}
