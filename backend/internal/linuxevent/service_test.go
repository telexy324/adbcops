package linuxevent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
	"aiops-platform/backend/internal/timeline"
)

func TestHostUnreachableUsesStableFingerprintAndMerges(t *testing.T) {
	repository := newMemoryRepository()
	service := NewService(repository, repository, repository)
	firstTime := time.Date(2026, 7, 18, 1, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	secondTime := firstTime.Add(time.Minute)
	input := RecordInput{
		HostID: 21, HostName: "payment-app-01", Host: "10.0.0.21", Environment: "prod",
		EventType: model.EventTypeLinuxHostUnreachable, Summary: "host cannot be reached",
		ObservedAt: &firstTime, Collector: "connection", Content: json.RawMessage(`{"reason":"timeout"}`),
	}
	first, err := service.Record(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	input.ObservedAt = &secondTime
	second, err := service.Record(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Event.ID != second.Event.ID || second.Event.OccurrenceCount != 2 {
		t.Fatalf("host unreachable was not merged: first=%+v second=%+v", first.Event, second.Event)
	}
	if first.Event.Fingerprint == nil || second.Event.Fingerprint == nil || *first.Event.Fingerprint != *second.Event.Fingerprint {
		t.Fatalf("fingerprint is not stable: first=%+v second=%+v", first.Event.Fingerprint, second.Event.Fingerprint)
	}
	if second.Event.EventTime.Location() != time.UTC || second.Event.LastSeenAt.Location() != time.UTC || second.Evidence.ObservedAt.Location() != time.UTC {
		t.Fatalf("time fields are not UTC: event=%v last=%v evidence=%v", second.Event.EventTime, second.Event.LastSeenAt, second.Evidence.ObservedAt)
	}
}

func TestHostKeyChangedCannotBeDowngraded(t *testing.T) {
	repository := newMemoryRepository()
	service := NewService(repository, repository, repository)
	result, err := service.Record(context.Background(), RecordInput{
		HostID: 7, EventType: model.EventTypeLinuxSSHHostKeyChanged, Severity: "info",
		Status: model.EventStatusResolved, Summary: "host key changed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Event.Severity == nil || *result.Event.Severity != "high" || result.Event.Status != model.EventStatusFiring || result.Event.ResolvedAt != nil {
		t.Fatalf("host key event was downgraded: %+v", result.Event)
	}
}

func TestRuleFindingLinksEvidenceTimelinePayloadAndIncident(t *testing.T) {
	repository := newMemoryRepository()
	service := NewService(repository, repository, repository)
	incidentID := int64(9)
	result, err := service.Record(context.Background(), RecordInput{
		HostID: 21, HostName: "payment-app-01", EventType: model.EventTypeLinuxMemoryPressure,
		Summary: "MemAvailable is low", Collector: "memory", CommandVersion: "1.0.0",
		FindingType: FindingTypeRule, IncidentID: &incidentID, Confidence: floatPtr(.98),
		Content: json.RawMessage(`{
			"memAvailablePercent":6.2,
			"nested":{"stdout":"secret raw line","swapUsedPercent":84},
			"rawCommandOutput":"do not retain"
		}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(result.Event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["findingType"] != FindingTypeRule || payload["evidenceKey"] != result.Evidence.EvidenceKey {
		t.Fatalf("rule finding does not reference evidence: %+v", payload)
	}
	if len(repository.incidentEvents[incidentID]) != 1 || repository.incidentEvidence[incidentID][0] != result.Evidence.EvidenceKey {
		t.Fatalf("incident relations missing: events=%+v evidence=%+v", repository.incidentEvents, repository.incidentEvidence)
	}
	if string(result.Evidence.Content) == "" || containsRawOutput(result.Evidence.Content) {
		t.Fatalf("raw command output leaked into evidence: %s", result.Evidence.Content)
	}
	var content map[string]any
	_ = json.Unmarshal(result.Evidence.Content, &content)
	if content["memAvailablePercent"] != 6.2 {
		t.Fatalf("structured evidence was removed: %+v", content)
	}
	from, to := result.Event.EventTime.Add(-time.Minute), result.Event.EventTime.Add(time.Minute)
	timelineResult, err := timeline.NewService(repository, repository).Build(context.Background(), timeline.Query{
		From: &from, To: &to, IncludeEvidence: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(timelineResult.Items) != 1 || len(timelineResult.Items[0].Evidence) != 1 ||
		timelineResult.Items[0].Evidence[0].EvidenceKey != result.Evidence.EvidenceKey {
		t.Fatalf("linux evidence not available in timeline: %+v", timelineResult)
	}
}

func containsRawOutput(raw []byte) bool {
	text := string(raw)
	for _, needle := range []string{"rawCommandOutput", "stdout", "secret raw line", "do not retain"} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

type memoryRepository struct {
	nextEventID      int64
	eventsByFP       map[string]*model.OpsEvent
	evidence         map[string]*model.EvidenceRecord
	incidentEvents   map[int64][]int64
	incidentEvidence map[int64][]string
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		eventsByFP: map[string]*model.OpsEvent{}, evidence: map[string]*model.EvidenceRecord{},
		incidentEvents: map[int64][]int64{}, incidentEvidence: map[int64][]string{},
	}
}

func (r *memoryRepository) UpsertOpsEvent(_ context.Context, event *model.OpsEvent) (*model.OpsEvent, error) {
	fingerprint := *event.Fingerprint
	if existing := r.eventsByFP[fingerprint]; existing != nil {
		existing.EventTime = event.EventTime
		existing.LastSeenAt = event.LastSeenAt
		existing.Payload = append([]byte(nil), event.Payload...)
		existing.OccurrenceCount++
		copy := *existing
		return &copy, nil
	}
	r.nextEventID++
	copy := *event
	copy.ID = r.nextEventID
	r.eventsByFP[fingerprint] = &copy
	return &copy, nil
}

func (r *memoryRepository) CreateEvidence(_ context.Context, evidence *model.EvidenceRecord) error {
	copy := *evidence
	r.evidence[evidence.EvidenceKey] = &copy
	return nil
}

func (r *memoryRepository) LinkIncidentEvents(_ context.Context, incidentID int64, eventIDs []int64) error {
	r.incidentEvents[incidentID] = append(r.incidentEvents[incidentID], eventIDs...)
	return nil
}

func (r *memoryRepository) LinkIncidentEvidence(_ context.Context, incidentID int64, keys []string) error {
	r.incidentEvidence[incidentID] = append(r.incidentEvidence[incidentID], keys...)
	return nil
}

func (r *memoryRepository) FindOpsEventByID(_ context.Context, id int64) (*model.OpsEvent, error) {
	for _, event := range r.eventsByFP {
		if event.ID == id {
			copy := *event
			return &copy, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *memoryRepository) ListOpsEvents(_ context.Context, filters repository.EventFilters) ([]model.OpsEvent, error) {
	result := []model.OpsEvent{}
	for _, event := range r.eventsByFP {
		if filters.From != nil && event.EventTime.Before(*filters.From) {
			continue
		}
		if filters.To != nil && event.EventTime.After(*filters.To) {
			continue
		}
		result = append(result, *event)
	}
	return result, nil
}

func (r *memoryRepository) FindEvidenceByKey(_ context.Context, key string) (*model.EvidenceRecord, error) {
	evidence := r.evidence[key]
	if evidence == nil {
		return nil, repository.ErrNotFound
	}
	copy := *evidence
	return &copy, nil
}

func floatPtr(value float64) *float64 { return &value }
