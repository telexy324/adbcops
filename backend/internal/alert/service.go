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
)

const (
	maxAlertsPerWebhook = 100
)

var (
	ErrInvalidInput = errors.New("invalid input")
)

type Repository interface {
	UpsertOpsEvent(ctx context.Context, event *model.OpsEvent) (*model.OpsEvent, error)
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
