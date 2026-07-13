package correlation

import (
	"context"
	"testing"
	"time"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

func TestAnalyzeReturnsExplainableScoreDetails(t *testing.T) {
	now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	events := &memoryEventRepository{events: []model.OpsEvent{
		opsEvent(1, now, "alert", "latency_high", "payment api latency high", "payment-api", `{"evidenceKeys":["ev_target"]}`),
		opsEvent(2, now.Add(-2*time.Minute), "release", "deploy", "payment api deployment finished", "payment-api", `{"evidenceKeys":["ev_release"]}`),
	}}
	service := NewService(events, nil)

	result, err := service.Analyze(context.Background(), Query{TargetEventID: 1, BeforeMinutes: 30, AfterMinutes: 10})
	if err != nil {
		t.Fatalf("analyze correlation: %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("expected one candidate, got %+v", result.Candidates)
	}
	candidate := result.Candidates[0]
	if len(candidate.ScoreDetails) != 5 {
		t.Fatalf("expected five score details, got %+v", candidate.ScoreDetails)
	}
	for _, detail := range candidate.ScoreDetails {
		if detail.Name == "" || detail.Explanation == "" || detail.Weight <= 0 {
			t.Fatalf("score detail is not explainable: %+v", detail)
		}
	}
	if candidate.Score <= 0.5 {
		t.Fatalf("expected meaningful score, got %+v", candidate)
	}
}

func TestAnalyzeUsesTopologySignal(t *testing.T) {
	now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	events := &memoryEventRepository{events: []model.OpsEvent{
		opsEvent(1, now, "alert", "pod_unready", "payment pod unavailable", "payment-api", `{"evidenceKeys":["ev_pod"]}`),
		opsEvent(2, now.Add(-3*time.Minute), "k8s_event", "deployment_rollout", "payment deployment rollout", "payment-deploy", `{"evidenceKeys":["ev_deploy"]}`),
	}}
	topology := &memoryTopologyRepository{
		nodes: []model.TopologyNode{
			{NodeKey: "k8s:prod:payment:k8s_pod:payment-api", Name: "payment-api"},
			{NodeKey: "k8s:prod:payment:k8s_deployment:payment-deploy", Name: "payment-deploy"},
		},
		edges: []model.TopologyEdge{{
			EdgeKey:     "deploy-pod",
			FromNodeKey: "k8s:prod:payment:k8s_deployment:payment-deploy",
			ToNodeKey:   "k8s:prod:payment:k8s_pod:payment-api",
			EdgeType:    model.TopologyEdgeTypeOwns,
		}},
	}
	service := NewService(events, topology)

	result, err := service.Analyze(context.Background(), Query{TargetEventID: 1, IncludeTopology: true})
	if err != nil {
		t.Fatalf("analyze correlation: %v", err)
	}
	if !result.TopologyUsed || len(result.Candidates) != 1 {
		t.Fatalf("expected topology scoring, got %+v", result)
	}
	topologyDetail := findDetail(result.Candidates[0].ScoreDetails, "topology")
	if topologyDetail.Score < 0.8 {
		t.Fatalf("expected direct topology score, got %+v", topologyDetail)
	}
}

func TestAnalyzeCapsHighConfidenceWithoutEvidence(t *testing.T) {
	now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	events := &memoryEventRepository{events: []model.OpsEvent{
		opsEvent(1, now, "alert", "latency_high", "payment api latency high", "payment-api", `{"evidenceKeys":["ev_target"]}`),
		opsEvent(2, now.Add(-1*time.Minute), "alert", "latency_high", "payment api latency high", "payment-api", `{}`),
	}}
	service := NewService(events, nil)

	result, err := service.Analyze(context.Background(), Query{TargetEventID: 1, BeforeMinutes: 30, AfterMinutes: 10})
	if err != nil {
		t.Fatalf("analyze correlation: %v", err)
	}
	candidate := result.Candidates[0]
	if candidate.EvidenceAvailable {
		t.Fatalf("expected no evidence on candidate: %+v", candidate)
	}
	if candidate.Confidence == "high" || candidate.Score >= 0.7 {
		t.Fatalf("candidate without evidence must not be high confidence: %+v", candidate)
	}
}

func TestAnalyzeAppliesCandidateLimitAfterScoringAndStableTieBreak(t *testing.T) {
	now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	events := &memoryEventRepository{events: []model.OpsEvent{
		opsEvent(1, now, "alert", "latency_high", "payment api latency high", "payment-api", `{"evidenceKeys":["ev_target"]}`),
		opsEvent(3, now.Add(-2*time.Minute), "release", "deploy", "payment deploy", "payment-api", `{"evidenceKeys":["ev_3"]}`),
		opsEvent(2, now.Add(-2*time.Minute), "release", "deploy", "payment deploy", "payment-api", `{"evidenceKeys":["ev_2"]}`),
		opsEvent(4, now.Add(-30*time.Minute), "manual_note", "note", "payment note", "payment-api", `{"evidenceKeys":["ev_4"]}`),
	}}
	service := NewService(events, nil)

	result, err := service.Analyze(context.Background(), Query{TargetEventID: 1, BeforeMinutes: 60, AfterMinutes: 10, Limit: 2})
	if err != nil {
		t.Fatalf("analyze correlation: %v", err)
	}
	if len(result.Candidates) != 2 {
		t.Fatalf("expected top 2 candidates, got %+v", result.Candidates)
	}
	if result.Candidates[0].Event.ID != 2 || result.Candidates[1].Event.ID != 3 {
		t.Fatalf("expected stable id tie-break after score/time, got %+v", result.Candidates)
	}
	for _, candidate := range result.Candidates {
		if len(candidate.ScoreDetails) != 5 || candidate.Reason == "" {
			t.Fatalf("candidate must remain explainable after limiting: %+v", candidate)
		}
	}
}

func findDetail(details []ScoreDetail, name string) ScoreDetail {
	for _, detail := range details {
		if detail.Name == name {
			return detail
		}
	}
	return ScoreDetail{}
}

func opsEvent(id int64, at time.Time, sourceType, eventType, summary, resourceName, payload string) model.OpsEvent {
	environment := "prod"
	system := "payment"
	component := resourceName
	namespace := "payment"
	return model.OpsEvent{
		ID:              id,
		EventTime:       at,
		SourceType:      sourceType,
		EventType:       eventType,
		Status:          model.EventStatusObserved,
		Environment:     &environment,
		SystemName:      &system,
		ComponentName:   &component,
		Namespace:       &namespace,
		ResourceName:    &resourceName,
		Summary:         summary,
		Payload:         []byte(payload),
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
		if filters.Environment != "" && deref(event.Environment) != filters.Environment {
			continue
		}
		if filters.SystemName != "" && deref(event.SystemName) != filters.SystemName {
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

type memoryTopologyRepository struct {
	nodes []model.TopologyNode
	edges []model.TopologyEdge
}

func (r *memoryTopologyRepository) ListNodes(_ context.Context, _ repository.TopologyFilters) ([]model.TopologyNode, error) {
	return r.nodes, nil
}

func (r *memoryTopologyRepository) ListEdges(_ context.Context, _ repository.TopologyFilters) ([]model.TopologyEdge, error) {
	return r.edges, nil
}
