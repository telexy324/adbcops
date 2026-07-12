package incident

import (
	"context"
	"encoding/json"
	"testing"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

func TestPromoteAnalysisCreatesIncidentWithRelations(t *testing.T) {
	repo := newMemoryIncidentRepository()
	analysis := &memoryAnalysisRepository{task: model.AnalysisTask{ID: 7, UserID: 1, Question: "payment api latency", Summary: strPtr("latency analysis")}}
	service := NewService(repo, analysis)

	detail, err := service.PromoteAnalysis(context.Background(), &model.AppUser{ID: 42}, PromoteInput{
		AnalysisTaskID: 7,
		Severity:       model.IncidentSeverityCritical,
		EventIDs:       []int64{10, 10, 11},
		EvidenceKeys:   []string{"ev_a", "ev_a", "ev_b"},
		RootCauses: []RootCauseCandidateIn{{
			Summary: "recent release changed timeout",
			Score:   0.82,
			Details: json.RawMessage(`{"source":"correlation"}`),
		}},
	})
	if err != nil {
		t.Fatalf("promote analysis: %v", err)
	}
	if detail.Incident.ID == 0 || detail.Incident.AnalysisTaskID == nil || *detail.Incident.AnalysisTaskID != 7 {
		t.Fatalf("expected incident linked to analysis task, got %+v", detail.Incident)
	}
	if len(detail.Events) != 2 || len(detail.Evidence) != 2 || len(detail.RootCauses) != 1 {
		t.Fatalf("unexpected relations: %+v", detail)
	}
	if !repo.hasActivity(detail.Incident.ID, "promote_analysis") {
		t.Fatalf("expected promote_analysis activity, got %+v", repo.activities)
	}
}

func TestConfirmRootCauseWritesActivityAudit(t *testing.T) {
	repo := newMemoryIncidentRepository()
	service := NewService(repo, nil)
	detail, err := service.Create(context.Background(), &model.AppUser{ID: 1}, CreateInput{
		Title:    "payment outage",
		Severity: model.IncidentSeverityWarning,
		RootCauses: []RootCauseCandidateIn{
			{Summary: "db slow", Score: 0.7},
			{Summary: "release regression", Score: 0.8},
		},
	})
	if err != nil {
		t.Fatalf("create incident: %v", err)
	}
	candidateID := detail.RootCauses[1].ID

	confirmed, err := service.ConfirmRootCause(context.Background(), &model.AppUser{ID: 99}, detail.Incident.ID, candidateID)
	if err != nil {
		t.Fatalf("confirm root cause: %v", err)
	}
	if !confirmed.RootCauses[1].Confirmed || confirmed.RootCauses[1].ConfirmedBy == nil || *confirmed.RootCauses[1].ConfirmedBy != 99 {
		t.Fatalf("expected confirmed candidate with actor, got %+v", confirmed.RootCauses)
	}
	if !repo.hasActivity(detail.Incident.ID, model.IncidentActivityConfirmRootCause) {
		t.Fatalf("expected confirm root cause activity, got %+v", repo.activities)
	}
}

func TestMatchHistoryReturnsAdvisoryOnlyAndDoesNotConfirmRootCause(t *testing.T) {
	repo := newMemoryIncidentRepository()
	service := NewService(repo, nil)
	current, err := service.Create(context.Background(), &model.AppUser{ID: 1}, CreateInput{
		Title:         "payment api timeout",
		Severity:      model.IncidentSeverityWarning,
		Tags:          []string{"payment", "timeout"},
		ErrorTemplate: "upstream timeout waiting for dependency",
		RootCauses: []RootCauseCandidateIn{{
			Summary: "candidate awaiting confirmation",
			Score:   0.6,
		}},
	})
	if err != nil {
		t.Fatalf("create current incident: %v", err)
	}
	historical := model.Incident{
		ID:            99,
		Title:         "payment dependency timeout",
		Severity:      model.IncidentSeverityWarning,
		Status:        model.IncidentStatusClosed,
		Tags:          []byte(`["payment","timeout"]`),
		ErrorTemplate: strPtr("upstream timeout waiting for dependency"),
	}
	repo.incidents[historical.ID] = historical
	repo.similar = []repository.IncidentSimilarityResult{{
		Incident: historical,
		Score:    0.77,
		Reasons:  []string{"text similarity via pg_trgm"},
	}}

	result, err := service.MatchHistory(context.Background(), &model.AppUser{ID: 2}, current.Incident.ID, MatchQuery{Limit: 5})
	if err != nil {
		t.Fatalf("match history: %v", err)
	}
	if len(result) != 1 || !result[0].AdvisoryOnly || result[0].Notice == "" {
		t.Fatalf("expected advisory-only match, got %+v", result)
	}
	for _, candidate := range repo.causes[current.Incident.ID] {
		if candidate.Confirmed {
			t.Fatalf("history match must not auto-confirm root cause: %+v", candidate)
		}
	}
}

type memoryIncidentRepository struct {
	nextIncidentID int64
	nextCauseID    int64
	incidents      map[int64]model.Incident
	events         map[int64][]model.IncidentEvent
	evidence       map[int64][]model.IncidentEvidence
	causes         map[int64][]model.IncidentRootCauseCandidate
	activities     map[int64][]model.IncidentActivity
	similar        []repository.IncidentSimilarityResult
}

func newMemoryIncidentRepository() *memoryIncidentRepository {
	return &memoryIncidentRepository{
		nextIncidentID: 1,
		nextCauseID:    1,
		incidents:      map[int64]model.Incident{},
		events:         map[int64][]model.IncidentEvent{},
		evidence:       map[int64][]model.IncidentEvidence{},
		causes:         map[int64][]model.IncidentRootCauseCandidate{},
		activities:     map[int64][]model.IncidentActivity{},
	}
}

func (r *memoryIncidentRepository) CreateIncident(_ context.Context, incident *model.Incident) error {
	incident.ID = r.nextIncidentID
	r.nextIncidentID++
	r.incidents[incident.ID] = *incident
	return nil
}

func (r *memoryIncidentRepository) UpdateIncident(_ context.Context, id int64, updates repository.IncidentUpdates) (*model.Incident, error) {
	incident, ok := r.incidents[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	if updates.Title != nil {
		incident.Title = *updates.Title
	}
	if updates.Severity != nil {
		incident.Severity = *updates.Severity
	}
	if updates.Status != nil {
		incident.Status = *updates.Status
	}
	if updates.ResolvedAt != nil {
		incident.ResolvedAt = updates.ResolvedAt
	}
	if updates.ClosedAt != nil {
		incident.ClosedAt = updates.ClosedAt
	}
	r.incidents[id] = incident
	return &incident, nil
}

func (r *memoryIncidentRepository) FindIncidentByID(_ context.Context, id int64) (*model.Incident, error) {
	incident, ok := r.incidents[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &incident, nil
}

func (r *memoryIncidentRepository) ListIncidents(context.Context, repository.IncidentFilters) ([]model.Incident, error) {
	result := []model.Incident{}
	for _, incident := range r.incidents {
		result = append(result, incident)
	}
	return result, nil
}

func (r *memoryIncidentRepository) LinkIncidentEvents(_ context.Context, incidentID int64, eventIDs []int64) error {
	seen := map[int64]struct{}{}
	for _, row := range r.events[incidentID] {
		seen[row.EventID] = struct{}{}
	}
	for _, id := range eventIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		r.events[incidentID] = append(r.events[incidentID], model.IncidentEvent{IncidentID: incidentID, EventID: id})
	}
	return nil
}

func (r *memoryIncidentRepository) LinkIncidentEvidence(_ context.Context, incidentID int64, keys []string) error {
	seen := map[string]struct{}{}
	for _, row := range r.evidence[incidentID] {
		seen[row.EvidenceKey] = struct{}{}
	}
	for _, key := range keys {
		if _, ok := seen[key]; ok {
			continue
		}
		r.evidence[incidentID] = append(r.evidence[incidentID], model.IncidentEvidence{IncidentID: incidentID, EvidenceKey: key})
	}
	return nil
}

func (r *memoryIncidentRepository) CreateRootCauseCandidates(_ context.Context, candidates []model.IncidentRootCauseCandidate) error {
	for _, candidate := range candidates {
		candidate.ID = r.nextCauseID
		r.nextCauseID++
		r.causes[candidate.IncidentID] = append(r.causes[candidate.IncidentID], candidate)
	}
	return nil
}

func (r *memoryIncidentRepository) ListRootCauseCandidates(_ context.Context, incidentID int64) ([]model.IncidentRootCauseCandidate, error) {
	return r.causes[incidentID], nil
}

func (r *memoryIncidentRepository) ConfirmRootCauseCandidate(_ context.Context, incidentID int64, candidateID int64, actorID int64) (*model.IncidentRootCauseCandidate, error) {
	rows := r.causes[incidentID]
	for i := range rows {
		rows[i].Confirmed = false
		rows[i].ConfirmedBy = nil
		if rows[i].ID == candidateID {
			rows[i].Confirmed = true
			rows[i].ConfirmedBy = &actorID
		}
	}
	r.causes[incidentID] = rows
	for i := range rows {
		if rows[i].ID == candidateID {
			return &rows[i], nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *memoryIncidentRepository) ListIncidentEvents(_ context.Context, incidentID int64) ([]model.IncidentEvent, error) {
	return r.events[incidentID], nil
}

func (r *memoryIncidentRepository) ListIncidentEvidence(_ context.Context, incidentID int64) ([]model.IncidentEvidence, error) {
	return r.evidence[incidentID], nil
}

func (r *memoryIncidentRepository) CreateIncidentActivity(_ context.Context, activity *model.IncidentActivity) error {
	r.activities[activity.IncidentID] = append(r.activities[activity.IncidentID], *activity)
	return nil
}

func (r *memoryIncidentRepository) ListIncidentActivities(_ context.Context, incidentID int64) ([]model.IncidentActivity, error) {
	return r.activities[incidentID], nil
}

func (r *memoryIncidentRepository) SearchSimilarIncidents(_ context.Context, input repository.IncidentSimilaritySearch) ([]repository.IncidentSimilarityResult, error) {
	if input.IncidentID <= 0 || input.Text == "" {
		return nil, nil
	}
	return r.similar, nil
}

func (r *memoryIncidentRepository) hasActivity(incidentID int64, action string) bool {
	for _, activity := range r.activities[incidentID] {
		if activity.Action == action {
			return true
		}
	}
	return false
}

type memoryAnalysisRepository struct {
	task model.AnalysisTask
}

func (r *memoryAnalysisRepository) FindAnalysisTaskByID(_ context.Context, id int64) (*model.AnalysisTask, error) {
	if id != r.task.ID {
		return nil, repository.ErrNotFound
	}
	return &r.task, nil
}

func strPtr(value string) *string {
	return &value
}
