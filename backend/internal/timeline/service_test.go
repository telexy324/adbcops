package timeline

import (
	"context"
	"testing"
	"time"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

func TestBuildTimelineUsesUTCAndStableSortForSameTime(t *testing.T) {
	instant := time.Date(2026, 7, 12, 10, 0, 0, 0, time.FixedZone("CST", 8*3600))
	repo := &memoryEventRepository{events: []model.OpsEvent{
		event(4, instant, model.EventSourceK8sEvent, "BackOff"),
		event(3, instant, model.EventSourceMetricAnomaly, "cpu"),
		event(1, instant, model.EventSourceAlert, "firing"),
		event(2, instant, model.EventSourceAlert, "resolved"),
	}}
	service := NewService(repo, nil)
	from := instant.Add(-time.Minute)
	to := instant.Add(time.Minute)

	result, err := service.Build(context.Background(), Query{From: &from, To: &to})
	if err != nil {
		t.Fatalf("build timeline: %v", err)
	}
	if result.Timezone != "UTC" || result.From.Location() != time.UTC || result.To.Location() != time.UTC {
		t.Fatalf("expected UTC window, got %+v", result)
	}
	if len(result.Items) != 4 {
		t.Fatalf("expected 4 items, got %+v", result.Items)
	}
	gotIDs := []int64{result.Items[0].EventID, result.Items[1].EventID, result.Items[2].EventID, result.Items[3].EventID}
	wantIDs := []int64{1, 2, 3, 4}
	for i := range wantIDs {
		if gotIDs[i] != wantIDs[i] {
			t.Fatalf("stable sort mismatch: got %v want %v", gotIDs, wantIDs)
		}
	}
	if result.SourceCounts[model.EventSourceAlert] != 2 || result.SourceCounts[model.EventSourceMetricAnomaly] != 1 || result.SourceCounts[model.EventSourceK8sEvent] != 1 {
		t.Fatalf("unexpected source counts: %+v", result.SourceCounts)
	}
	for _, item := range result.Items {
		if item.Time.Location() != time.UTC {
			t.Fatalf("expected item time in UTC, got %s", item.Time.Location())
		}
	}
}

func TestBuildTimelineAroundAnchorWindow(t *testing.T) {
	anchorTime := time.Date(2026, 7, 12, 2, 0, 0, 0, time.UTC)
	repo := &memoryEventRepository{events: []model.OpsEvent{
		event(1, anchorTime.Add(-31*time.Minute), "alert", "outside_before"),
		event(2, anchorTime, "alert", "anchor"),
		event(3, anchorTime.Add(29*time.Minute), "k8s_event", "inside_after"),
	}}
	service := NewService(repo, nil)

	result, err := service.Build(context.Background(), Query{AnchorEventID: 2, BeforeMinutes: 30, AfterMinutes: 30})
	if err != nil {
		t.Fatalf("build anchor timeline: %v", err)
	}
	if result.AnchorEventID == nil || *result.AnchorEventID != 2 {
		t.Fatalf("expected anchor id 2, got %+v", result.AnchorEventID)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected only events inside anchor window, got %+v", result.Items)
	}
}

func TestBuildTimelineAssociatesEvidence(t *testing.T) {
	now := time.Date(2026, 7, 12, 2, 0, 0, 0, time.UTC)
	eventWithEvidence := event(1, now, "log_anomaly", "spike")
	eventWithEvidence.Payload = []byte(`{"evidenceKeys":["ev_log","ev_missing"],"errorCount":42}`)
	events := &memoryEventRepository{events: []model.OpsEvent{eventWithEvidence}}
	evidence := &memoryEvidenceRepository{records: map[string]model.EvidenceRecord{
		"ev_log": {EvidenceKey: "ev_log", SourceType: "log_anomaly", Summary: "error log sample"},
	}}
	service := NewService(events, evidence)
	from := now.Add(-time.Minute)
	to := now.Add(time.Minute)

	result, err := service.Build(context.Background(), Query{From: &from, To: &to, IncludeEvidence: true})
	if err != nil {
		t.Fatalf("build timeline: %v", err)
	}
	if len(result.Items) != 1 || len(result.Items[0].Evidence) != 1 {
		t.Fatalf("expected one evidence summary, got %+v", result)
	}
	if result.Items[0].Evidence[0].EvidenceKey != "ev_log" {
		t.Fatalf("unexpected evidence: %+v", result.Items[0].Evidence)
	}
	if len(result.EvidenceMissing) != 1 || result.EvidenceMissing[0] != "ev_missing" {
		t.Fatalf("expected missing evidence ev_missing, got %+v", result.EvidenceMissing)
	}
	if result.Items[0].PayloadSummary["errorCount"] == nil {
		t.Fatalf("expected payload summary, got %+v", result.Items[0].PayloadSummary)
	}
}

func event(id int64, when time.Time, sourceType, eventType string) model.OpsEvent {
	return model.OpsEvent{
		ID:              id,
		EventTime:       when,
		SourceType:      sourceType,
		EventType:       eventType,
		Status:          model.EventStatusObserved,
		Summary:         sourceType + ":" + eventType,
		OccurrenceCount: 1,
	}
}

type memoryEventRepository struct {
	events []model.OpsEvent
}

func (r *memoryEventRepository) FindOpsEventByID(_ context.Context, id int64) (*model.OpsEvent, error) {
	for _, event := range r.events {
		if event.ID == id {
			copied := event
			return &copied, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *memoryEventRepository) ListOpsEvents(_ context.Context, filters repository.EventFilters) ([]model.OpsEvent, error) {
	result := []model.OpsEvent{}
	for _, event := range r.events {
		if filters.SourceType != "" && event.SourceType != filters.SourceType {
			continue
		}
		if filters.From != nil && event.EventTime.UTC().Before(filters.From.UTC()) {
			continue
		}
		if filters.To != nil && event.EventTime.UTC().After(filters.To.UTC()) {
			continue
		}
		result = append(result, event)
	}
	return result, nil
}

type memoryEvidenceRepository struct {
	records map[string]model.EvidenceRecord
}

func (r *memoryEvidenceRepository) FindEvidenceByKey(_ context.Context, key string) (*model.EvidenceRecord, error) {
	record, ok := r.records[key]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &record, nil
}
