package alert

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

const (
	maxAlertsPerWebhook = 100
)

var (
	ErrInvalidInput = errors.New("invalid input")
)

type Repository interface {
	UpsertOpsEvent(ctx context.Context, event *model.OpsEvent) (*model.OpsEvent, error)
	FindOpsEventByID(ctx context.Context, id int64) (*model.OpsEvent, error)
	ListOpsEvents(ctx context.Context, filters repository.EventFilters) ([]model.OpsEvent, error)
}

type Service struct {
	repository Repository
}

type AlertmanagerWebhook struct {
	Receiver          string              `json:"receiver"`
	Status            string              `json:"status"`
	Alerts            []AlertmanagerAlert `json:"alerts"`
	GroupLabels       map[string]string   `json:"groupLabels"`
	CommonLabels      map[string]string   `json:"commonLabels"`
	CommonAnnotations map[string]string   `json:"commonAnnotations"`
	ExternalURL       string              `json:"externalURL"`
	Version           string              `json:"version"`
	GroupKey          string              `json:"groupKey"`
	TruncatedAlerts   int                 `json:"truncatedAlerts"`
}

type AlertmanagerAlert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}

type WebhookResult struct {
	Received int               `json:"received"`
	Events   []OpsEventSummary `json:"events"`
}

type OpsEventSummary struct {
	ID              int64      `json:"id"`
	Fingerprint     string     `json:"fingerprint"`
	Status          string     `json:"status"`
	Severity        string     `json:"severity,omitempty"`
	Summary         string     `json:"summary"`
	OccurrenceCount int        `json:"occurrenceCount"`
	ResolvedAt      *time.Time `json:"resolvedAt,omitempty"`
}

type NormalizedEventInput struct {
	EventTime     *time.Time     `json:"eventTime"`
	SourceType    string         `json:"sourceType"`
	SourceID      string         `json:"sourceId"`
	EventType     string         `json:"eventType"`
	Severity      string         `json:"severity"`
	Status        string         `json:"status"`
	Environment   string         `json:"environment"`
	SystemName    string         `json:"systemName"`
	ComponentName string         `json:"componentName"`
	Cluster       string         `json:"cluster"`
	Namespace     string         `json:"namespace"`
	ResourceKind  string         `json:"resourceKind"`
	ResourceName  string         `json:"resourceName"`
	Host          string         `json:"host"`
	TraceID       string         `json:"traceId"`
	Fingerprint   string         `json:"fingerprint"`
	Summary       string         `json:"summary"`
	Payload       map[string]any `json:"payload"`
}

type EventQuery struct {
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

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) ReceiveAlertmanager(ctx context.Context, webhook AlertmanagerWebhook) (*WebhookResult, error) {
	if len(webhook.Alerts) == 0 || len(webhook.Alerts) > maxAlertsPerWebhook {
		return nil, ErrInvalidInput
	}
	result := &WebhookResult{Received: len(webhook.Alerts), Events: make([]OpsEventSummary, 0, len(webhook.Alerts))}
	for _, alert := range webhook.Alerts {
		event, err := buildOpsEvent(webhook, alert)
		if err != nil {
			return nil, err
		}
		saved, err := s.repository.UpsertOpsEvent(ctx, event)
		if err != nil {
			return nil, err
		}
		result.Events = append(result.Events, toSummary(saved))
	}
	return result, nil
}

func (s *Service) CreateManualEvent(ctx context.Context, input NormalizedEventInput) (*model.OpsEvent, error) {
	event, err := NormalizeEvent(input)
	if err != nil {
		return nil, err
	}
	return s.repository.UpsertOpsEvent(ctx, event)
}

func (s *Service) ListEvents(ctx context.Context, query EventQuery) ([]model.OpsEvent, error) {
	return s.repository.ListOpsEvents(ctx, repository.EventFilters{
		Limit:         query.Limit,
		SourceType:    strings.TrimSpace(query.SourceType),
		Status:        strings.TrimSpace(query.Status),
		Environment:   strings.TrimSpace(query.Environment),
		SystemName:    strings.TrimSpace(query.SystemName),
		ComponentName: strings.TrimSpace(query.ComponentName),
		Namespace:     strings.TrimSpace(query.Namespace),
		ResourceName:  strings.TrimSpace(query.ResourceName),
		From:          query.From,
		To:            query.To,
	})
}

func (s *Service) GetEvent(ctx context.Context, id int64) (*model.OpsEvent, error) {
	if id <= 0 {
		return nil, ErrInvalidInput
	}
	return s.repository.FindOpsEventByID(ctx, id)
}

func NormalizeEvent(input NormalizedEventInput) (*model.OpsEvent, error) {
	sourceType := normalizeString(input.SourceType)
	eventType := normalizeString(input.EventType)
	summary := normalizeString(input.Summary)
	if !validSourceType(sourceType) || eventType == "" || summary == "" {
		return nil, ErrInvalidInput
	}
	status := normalizeEventStatus(input.Status)
	if status == "" {
		return nil, ErrInvalidInput
	}
	eventTime := time.Now().UTC()
	if input.EventTime != nil && !input.EventTime.IsZero() {
		eventTime = input.EventTime.UTC()
	}
	payload, err := json.Marshal(input.Payload)
	if err != nil {
		return nil, ErrInvalidInput
	}
	fingerprint := strings.TrimSpace(input.Fingerprint)
	if fingerprint == "" {
		fingerprint = generatedEventFingerprint(sourceType, eventType, input)
	}
	var resolvedAt *time.Time
	if status == model.EventStatusResolved {
		resolvedAt = &eventTime
	}
	return &model.OpsEvent{
		EventTime:     eventTime,
		SourceType:    sourceType,
		SourceID:      stringPtr(input.SourceID),
		EventType:     eventType,
		Severity:      stringPtr(input.Severity),
		Status:        status,
		Environment:   stringPtr(input.Environment),
		SystemName:    stringPtr(input.SystemName),
		ComponentName: stringPtr(input.ComponentName),
		Cluster:       stringPtr(input.Cluster),
		Namespace:     stringPtr(input.Namespace),
		ResourceKind:  stringPtr(input.ResourceKind),
		ResourceName:  stringPtr(input.ResourceName),
		Host:          stringPtr(input.Host),
		TraceID:       stringPtr(input.TraceID),
		Fingerprint:   stringPtr(fingerprint),
		Summary:       summary,
		Payload:       payload,
		ResolvedAt:    resolvedAt,
	}, nil
}

func buildOpsEvent(webhook AlertmanagerWebhook, alert AlertmanagerAlert) (*model.OpsEvent, error) {
	labels := mergeLabels(webhook.CommonLabels, alert.Labels)
	annotations := mergeLabels(webhook.CommonAnnotations, alert.Annotations)
	status := normalizeStatus(firstNonEmpty(alert.Status, webhook.Status))
	alertName := normalizeString(firstNonEmpty(labels["alertname"], labels["alert_name"], "alert"))
	if status == "" || alertName == "" || !validMap(labels) || !validMap(annotations) {
		return nil, ErrInvalidInput
	}
	eventTime := alert.StartsAt
	if status == model.EventStatusResolved && !alert.EndsAt.IsZero() {
		eventTime = alert.EndsAt
	}
	if eventTime.IsZero() {
		eventTime = time.Now().UTC()
	}
	fingerprint := strings.TrimSpace(alert.Fingerprint)
	if fingerprint == "" {
		fingerprint = generatedFingerprint(labels)
	}
	payload, err := json.Marshal(map[string]any{
		"receiver":          webhook.Receiver,
		"groupLabels":       webhook.GroupLabels,
		"commonLabels":      webhook.CommonLabels,
		"commonAnnotations": webhook.CommonAnnotations,
		"externalURL":       webhook.ExternalURL,
		"groupKey":          webhook.GroupKey,
		"alert":             alert,
	})
	if err != nil {
		return nil, ErrInvalidInput
	}
	var resolvedAt *time.Time
	if status == model.EventStatusResolved {
		resolved := alert.EndsAt
		if resolved.IsZero() {
			resolved = eventTime
		}
		resolvedAt = &resolved
	}
	return &model.OpsEvent{
		EventTime:     eventTime.UTC(),
		SourceType:    model.EventSourceAlertmanager,
		SourceID:      stringPtr(firstNonEmpty(webhook.GroupKey, webhook.Receiver)),
		EventType:     alertName,
		Severity:      stringPtr(normalizeString(firstNonEmpty(labels["severity"], labels["priority"]))),
		Status:        status,
		Environment:   stringPtr(firstNonEmpty(labels["environment"], labels["env"])),
		SystemName:    stringPtr(firstNonEmpty(labels["system_name"], labels["system"], labels["app"])),
		ComponentName: stringPtr(firstNonEmpty(labels["component_name"], labels["component"], labels["service"])),
		Cluster:       stringPtr(labels["cluster"]),
		Namespace:     stringPtr(firstNonEmpty(labels["namespace"], labels["kubernetes_namespace"])),
		ResourceKind:  stringPtr(firstNonEmpty(labels["resource_kind"], labels["kind"])),
		ResourceName:  stringPtr(resourceName(labels)),
		Host:          stringPtr(firstNonEmpty(labels["host"], labels["instance"], labels["node"])),
		TraceID:       stringPtr(firstNonEmpty(labels["trace_id"], labels["traceId"])),
		Fingerprint:   stringPtr(fingerprint),
		Summary:       summary(alertName, annotations),
		Payload:       payload,
		ResolvedAt:    resolvedAt,
	}, nil
}

func generatedFingerprint(labels map[string]string) string {
	keys := []string{
		firstNonEmpty(labels["alertname"], labels["alert_name"]),
		firstNonEmpty(labels["environment"], labels["env"]),
		firstNonEmpty(labels["system_name"], labels["system"], labels["app"]),
		firstNonEmpty(labels["component_name"], labels["component"], labels["service"]),
		resourceName(labels),
	}
	joined := strings.Join(keys, "|")
	if strings.Trim(joined, "|") == "" {
		labelKeys := make([]string, 0, len(labels))
		for key := range labels {
			labelKeys = append(labelKeys, key)
		}
		sort.Strings(labelKeys)
		parts := make([]string, 0, len(labelKeys))
		for _, key := range labelKeys {
			parts = append(parts, key+"="+labels[key])
		}
		joined = strings.Join(parts, "|")
	}
	sum := sha256.Sum256([]byte(joined))
	return hex.EncodeToString(sum[:])
}

func generatedEventFingerprint(sourceType, eventType string, input NormalizedEventInput) string {
	resourceIdentity := firstNonEmpty(input.ResourceName, input.Host, input.Namespace, input.SourceID)
	joined := strings.Join([]string{
		sourceType,
		eventType,
		input.Environment,
		input.SystemName,
		input.ComponentName,
		input.Cluster,
		input.Namespace,
		input.ResourceKind,
		resourceIdentity,
	}, "|")
	sum := sha256.Sum256([]byte(strings.ToLower(joined)))
	return hex.EncodeToString(sum[:])
}

func mergeLabels(base, override map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(override))
	for key, value := range base {
		merged[key] = strings.TrimSpace(value)
	}
	for key, value := range override {
		merged[key] = strings.TrimSpace(value)
	}
	return merged
}

func normalizeStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case model.EventStatusFiring, "":
		return model.EventStatusFiring
	case model.EventStatusResolved:
		return model.EventStatusResolved
	default:
		return ""
	}
}

func normalizeEventStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", model.EventStatusObserved:
		return model.EventStatusObserved
	case model.EventStatusFiring:
		return model.EventStatusFiring
	case model.EventStatusResolved:
		return model.EventStatusResolved
	default:
		return ""
	}
}

func validSourceType(value string) bool {
	switch value {
	case model.EventSourceAlert,
		model.EventSourceAlertmanager,
		model.EventSourceLogAnomaly,
		model.EventSourceMetricAnomaly,
		model.EventSourceK8sEvent,
		model.EventSourceRelease,
		model.EventSourceConfigChange,
		model.EventSourceGitChange,
		model.EventSourceDBChange,
		model.EventSourceManualNote:
		return true
	default:
		return false
	}
}

func normalizeString(value string) string {
	return strings.TrimSpace(value)
}

func summary(alertName string, annotations map[string]string) string {
	for _, key := range []string{"summary", "description", "message"} {
		if value := strings.TrimSpace(annotations[key]); value != "" {
			return value
		}
	}
	return alertName
}

func resourceName(labels map[string]string) string {
	return firstNonEmpty(labels["resource_name"], labels["pod"], labels["container"], labels["deployment"], labels["service"], labels["instance"], labels["node"])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func stringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func validMap(values map[string]string) bool {
	for key, value := range values {
		if !utf8.ValidString(key) || !utf8.ValidString(value) {
			return false
		}
	}
	return true
}

func toSummary(event *model.OpsEvent) OpsEventSummary {
	summary := OpsEventSummary{
		ID:              event.ID,
		Status:          event.Status,
		Summary:         event.Summary,
		OccurrenceCount: event.OccurrenceCount,
		ResolvedAt:      event.ResolvedAt,
	}
	if event.Fingerprint != nil {
		summary.Fingerprint = *event.Fingerprint
	}
	if event.Severity != nil {
		summary.Severity = *event.Severity
	}
	return summary
}
