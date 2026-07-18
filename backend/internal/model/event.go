package model

import "time"

const (
	EventSourceAlertmanager  = "alertmanager"
	EventSourceAlert         = "alert"
	EventSourceLogAnomaly    = "log_anomaly"
	EventSourceMetricAnomaly = "metric_anomaly"
	EventSourceK8sEvent      = "k8s_event"
	EventSourceRelease       = "release"
	EventSourceConfigChange  = "config_change"
	EventSourceGitChange     = "git_change"
	EventSourceDBChange      = "database_change"
	EventSourceManualNote    = "manual_note"
	EventSourceLinuxSSH      = "linux_ssh"

	EventTypeLinuxSSHHostKeyChanged = "linux_ssh_host_key_changed"

	EventStatusFiring   = "firing"
	EventStatusResolved = "resolved"
	EventStatusObserved = "observed"
)

type OpsEvent struct {
	ID              int64      `gorm:"column:id;primaryKey" json:"id"`
	EventTime       time.Time  `gorm:"column:event_time;not null" json:"eventTime"`
	SourceType      string     `gorm:"column:source_type;size:50;not null" json:"sourceType"`
	SourceID        *string    `gorm:"column:source_id;size:255" json:"sourceId,omitempty"`
	EventType       string     `gorm:"column:event_type;size:100;not null" json:"eventType"`
	Severity        *string    `gorm:"column:severity;size:30" json:"severity,omitempty"`
	Status          string     `gorm:"column:status;size:30;not null" json:"status"`
	Environment     *string    `gorm:"column:environment;size:50" json:"environment,omitempty"`
	SystemName      *string    `gorm:"column:system_name;size:100" json:"systemName,omitempty"`
	ComponentName   *string    `gorm:"column:component_name;size:100" json:"componentName,omitempty"`
	Cluster         *string    `gorm:"column:cluster;size:120" json:"cluster,omitempty"`
	Namespace       *string    `gorm:"column:namespace;size:120" json:"namespace,omitempty"`
	ResourceKind    *string    `gorm:"column:resource_kind;size:80" json:"resourceKind,omitempty"`
	ResourceName    *string    `gorm:"column:resource_name;size:255" json:"resourceName,omitempty"`
	Host            *string    `gorm:"column:host;size:255" json:"host,omitempty"`
	TraceID         *string    `gorm:"column:trace_id;size:255" json:"traceId,omitempty"`
	Fingerprint     *string    `gorm:"column:fingerprint;size:255" json:"fingerprint,omitempty"`
	Summary         string     `gorm:"column:summary;not null" json:"summary"`
	Payload         []byte     `gorm:"column:payload;type:jsonb" json:"payload,omitempty"`
	OccurrenceCount int        `gorm:"column:occurrence_count;not null" json:"occurrenceCount"`
	FirstSeenAt     time.Time  `gorm:"column:first_seen_at;not null" json:"firstSeenAt"`
	LastSeenAt      time.Time  `gorm:"column:last_seen_at;not null" json:"lastSeenAt"`
	ResolvedAt      *time.Time `gorm:"column:resolved_at" json:"resolvedAt,omitempty"`
	CreatedAt       time.Time  `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt       time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (OpsEvent) TableName() string {
	return "ops_event"
}
