package alert

import (
	"context"
	"testing"
	"time"

	"aiops-platform/backend/internal/model"
)

func TestReceiveAlertmanagerDeduplicatesByFingerprint(t *testing.T) {
	repository := newTestRepository()
	service := NewService(repository)
	webhook := AlertmanagerWebhook{Receiver: "default", Status: model.EventStatusFiring, Alerts: []AlertmanagerAlert{testAlert(model.EventStatusFiring, "fp-1")}}

	first, err := service.ReceiveAlertmanager(context.Background(), webhook)
	if err != nil {
		t.Fatalf("first webhook: %v", err)
	}
	second, err := service.ReceiveAlertmanager(context.Background(), webhook)
	if err != nil {
		t.Fatalf("second webhook: %v", err)
	}
	if first.Events[0].ID != second.Events[0].ID {
		t.Fatalf("expected same event id, got %d and %d", first.Events[0].ID, second.Events[0].ID)
	}
	if second.Events[0].OccurrenceCount != 2 {
		t.Fatalf("occurrence count = %d, want 2", second.Events[0].OccurrenceCount)
	}
	if second.Events[0].Status != model.EventStatusFiring {
		t.Fatalf("status = %s, want firing", second.Events[0].Status)
	}
}

func TestReceiveAlertmanagerRecognizesResolvedStatus(t *testing.T) {
	repository := newTestRepository()
	service := NewService(repository)
	firing := AlertmanagerWebhook{Receiver: "default", Status: model.EventStatusFiring, Alerts: []AlertmanagerAlert{testAlert(model.EventStatusFiring, "fp-2")}}
	resolvedAlert := testAlert(model.EventStatusResolved, "fp-2")
	resolvedAt := time.Date(2026, 7, 12, 11, 0, 0, 0, time.UTC)
	resolvedAlert.EndsAt = resolvedAt
	resolved := AlertmanagerWebhook{Receiver: "default", Status: model.EventStatusResolved, Alerts: []AlertmanagerAlert{resolvedAlert}}

	if _, err := service.ReceiveAlertmanager(context.Background(), firing); err != nil {
		t.Fatalf("firing webhook: %v", err)
	}
	result, err := service.ReceiveAlertmanager(context.Background(), resolved)
	if err != nil {
		t.Fatalf("resolved webhook: %v", err)
	}
	event := result.Events[0]
	if event.Status != model.EventStatusResolved {
		t.Fatalf("status = %s, want resolved", event.Status)
	}
	if event.ResolvedAt == nil || !event.ResolvedAt.Equal(resolvedAt) {
		t.Fatalf("resolvedAt = %v, want %v", event.ResolvedAt, resolvedAt)
	}
	if event.OccurrenceCount != 2 {
		t.Fatalf("occurrence count = %d, want 2", event.OccurrenceCount)
	}
}

func TestGeneratedFingerprintUsesStableLabels(t *testing.T) {
	repository := newTestRepository()
	service := NewService(repository)
	alert := testAlert(model.EventStatusFiring, "")
	first := AlertmanagerWebhook{Receiver: "default", Alerts: []AlertmanagerAlert{alert}}
	second := AlertmanagerWebhook{Receiver: "default", Alerts: []AlertmanagerAlert{alert}}

	firstResult, err := service.ReceiveAlertmanager(context.Background(), first)
	if err != nil {
		t.Fatalf("first webhook: %v", err)
	}
	secondResult, err := service.ReceiveAlertmanager(context.Background(), second)
	if err != nil {
		t.Fatalf("second webhook: %v", err)
	}
	if firstResult.Events[0].Fingerprint == "" || firstResult.Events[0].Fingerprint != secondResult.Events[0].Fingerprint {
		t.Fatalf("fingerprints not stable: %+v %+v", firstResult.Events[0], secondResult.Events[0])
	}
}

func testAlert(status string, fingerprint string) AlertmanagerAlert {
	return AlertmanagerAlert{
		Status: status,
		Labels: map[string]string{
			"alertname":   "HighErrorRate",
			"severity":    "critical",
			"environment": "prod",
			"system":      "payment",
			"service":     "payment-api",
			"pod":         "payment-api-0",
			"namespace":   "prod",
		},
		Annotations: map[string]string{"summary": "payment api error rate is high"},
		StartsAt:    time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC),
		Fingerprint: fingerprint,
	}
}

type testRepository struct {
	nextID int64
	byFP   map[string]*model.OpsEvent
}

func newTestRepository() *testRepository {
	return &testRepository{nextID: 1, byFP: map[string]*model.OpsEvent{}}
}

func (r *testRepository) UpsertOpsEvent(_ context.Context, event *model.OpsEvent) (*model.OpsEvent, error) {
	fingerprint := ""
	if event.Fingerprint != nil {
		fingerprint = *event.Fingerprint
	}
	if existing := r.byFP[fingerprint]; existing != nil {
		existing.EventTime = event.EventTime
		existing.Status = event.Status
		existing.Summary = event.Summary
		existing.Payload = event.Payload
		existing.OccurrenceCount++
		existing.ResolvedAt = event.ResolvedAt
		return existing, nil
	}
	event.ID = r.nextID
	r.nextID++
	event.OccurrenceCount = 1
	r.byFP[fingerprint] = event
	return event, nil
}
