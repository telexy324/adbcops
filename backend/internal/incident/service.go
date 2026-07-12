package incident

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

var (
	ErrInvalidInput = errors.New("invalid input")
	ErrForbidden    = errors.New("incident access forbidden")
)

type Repository interface {
	CreateIncident(ctx context.Context, incident *model.Incident) error
	UpdateIncident(ctx context.Context, id int64, updates repository.IncidentUpdates) (*model.Incident, error)
	FindIncidentByID(ctx context.Context, id int64) (*model.Incident, error)
	ListIncidents(ctx context.Context, filters repository.IncidentFilters) ([]model.Incident, error)
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

type AnalysisRepository interface {
	FindAnalysisTaskByID(ctx context.Context, id int64) (*model.AnalysisTask, error)
}

type Service struct {
	repository Repository
	analysis   AnalysisRepository
}

type CreateInput struct {
	Title          string                 `json:"title"`
	Severity       string                 `json:"severity"`
	Status         string                 `json:"status"`
	Environment    string                 `json:"environment"`
	SystemName     string                 `json:"systemName"`
	ComponentName  string                 `json:"componentName"`
	Summary        string                 `json:"summary"`
	AnalysisTaskID *int64                 `json:"analysisTaskId"`
	EventIDs       []int64                `json:"eventIds"`
	EvidenceKeys   []string               `json:"evidenceKeys"`
	RootCauses     []RootCauseCandidateIn `json:"rootCauses"`
}

type UpdateInput struct {
	Title          *string `json:"title"`
	Severity       *string `json:"severity"`
	Status         *string `json:"status"`
	Environment    *string `json:"environment"`
	EnvironmentSet bool
	SystemName     *string `json:"systemName"`
	SystemSet      bool
	ComponentName  *string `json:"componentName"`
	ComponentSet   bool
	Summary        *string `json:"summary"`
	SummarySet     bool
}

type RootCauseCandidateIn struct {
	Summary string          `json:"summary"`
	Score   float64         `json:"score"`
	Details json.RawMessage `json:"details"`
}

type PromoteInput struct {
	AnalysisTaskID int64                  `json:"analysisTaskId"`
	Title          string                 `json:"title"`
	Severity       string                 `json:"severity"`
	EventIDs       []int64                `json:"eventIds"`
	EvidenceKeys   []string               `json:"evidenceKeys"`
	RootCauses     []RootCauseCandidateIn `json:"rootCauses"`
}

type Query struct {
	Limit       int
	Status      string
	Severity    string
	Environment string
	SystemName  string
}

type Detail struct {
	Incident   *model.Incident                    `json:"incident"`
	Events     []model.IncidentEvent              `json:"events"`
	Evidence   []model.IncidentEvidence           `json:"evidence"`
	RootCauses []model.IncidentRootCauseCandidate `json:"rootCauses"`
	Activities []model.IncidentActivity           `json:"activities"`
}

func NewService(repository Repository, analysis AnalysisRepository) *Service {
	return &Service{repository: repository, analysis: analysis}
}

func (s *Service) Create(ctx context.Context, actor *model.AppUser, input CreateInput) (*Detail, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	incident, causes, err := normalizeCreate(input, actor.ID)
	if err != nil {
		return nil, err
	}
	if err := s.repository.CreateIncident(ctx, incident); err != nil {
		return nil, err
	}
	if err := s.addRelations(ctx, incident.ID, input.EventIDs, input.EvidenceKeys, causes); err != nil {
		return nil, err
	}
	_ = s.recordActivity(ctx, incident.ID, &actor.ID, model.IncidentActivityCreate, map[string]any{"title": incident.Title})
	return s.Get(ctx, actor, incident.ID)
}

func (s *Service) PromoteAnalysis(ctx context.Context, actor *model.AppUser, input PromoteInput) (*Detail, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	if input.AnalysisTaskID <= 0 || s.analysis == nil {
		return nil, ErrInvalidInput
	}
	task, err := s.analysis.FindAnalysisTaskByID(ctx, input.AnalysisTaskID)
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = task.Question
	}
	summary := ""
	if task.Summary != nil {
		summary = *task.Summary
	}
	create := CreateInput{
		Title:          title,
		Severity:       input.Severity,
		Status:         model.IncidentStatusOpen,
		Summary:        summary,
		AnalysisTaskID: &input.AnalysisTaskID,
		EventIDs:       input.EventIDs,
		EvidenceKeys:   input.EvidenceKeys,
		RootCauses:     input.RootCauses,
	}
	detail, err := s.Create(ctx, actor, create)
	if err != nil {
		return nil, err
	}
	_ = s.recordActivity(ctx, detail.Incident.ID, &actor.ID, "promote_analysis", map[string]any{"analysisTaskId": input.AnalysisTaskID})
	return detail, nil
}

func (s *Service) Update(ctx context.Context, actor *model.AppUser, id int64, input UpdateInput) (*Detail, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	if id <= 0 {
		return nil, ErrInvalidInput
	}
	updates, lifecycle, err := normalizeUpdate(input)
	if err != nil {
		return nil, err
	}
	incident, err := s.repository.UpdateIncident(ctx, id, updates)
	if err != nil {
		return nil, err
	}
	action := model.IncidentActivityUpdate
	if lifecycle {
		action = model.IncidentActivityLifecycle
	}
	_ = s.recordActivity(ctx, incident.ID, &actor.ID, action, map[string]any{"status": incident.Status})
	return s.Get(ctx, actor, incident.ID)
}

func (s *Service) List(ctx context.Context, actor *model.AppUser, query Query) ([]model.Incident, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	return s.repository.ListIncidents(ctx, repository.IncidentFilters{
		Limit:       query.Limit,
		Status:      strings.TrimSpace(query.Status),
		Severity:    strings.TrimSpace(query.Severity),
		Environment: strings.TrimSpace(query.Environment),
		SystemName:  strings.TrimSpace(query.SystemName),
	})
}

func (s *Service) Get(ctx context.Context, actor *model.AppUser, id int64) (*Detail, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	if id <= 0 {
		return nil, ErrInvalidInput
	}
	incident, err := s.repository.FindIncidentByID(ctx, id)
	if err != nil {
		return nil, err
	}
	events, _ := s.repository.ListIncidentEvents(ctx, id)
	evidence, _ := s.repository.ListIncidentEvidence(ctx, id)
	rootCauses, _ := s.repository.ListRootCauseCandidates(ctx, id)
	activities, _ := s.repository.ListIncidentActivities(ctx, id)
	return &Detail{Incident: incident, Events: events, Evidence: evidence, RootCauses: rootCauses, Activities: activities}, nil
}

func (s *Service) ConfirmRootCause(ctx context.Context, actor *model.AppUser, incidentID int64, candidateID int64) (*Detail, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	if incidentID <= 0 || candidateID <= 0 {
		return nil, ErrInvalidInput
	}
	candidate, err := s.repository.ConfirmRootCauseCandidate(ctx, incidentID, candidateID, actor.ID)
	if err != nil {
		return nil, err
	}
	_ = s.recordActivity(ctx, incidentID, &actor.ID, model.IncidentActivityConfirmRootCause, map[string]any{"candidateId": candidate.ID, "summary": candidate.Summary})
	return s.Get(ctx, actor, incidentID)
}

func (s *Service) addRelations(ctx context.Context, incidentID int64, eventIDs []int64, evidenceKeys []string, causes []model.IncidentRootCauseCandidate) error {
	if err := s.repository.LinkIncidentEvents(ctx, incidentID, uniqueInt64(eventIDs)); err != nil {
		return err
	}
	if err := s.repository.LinkIncidentEvidence(ctx, incidentID, uniqueStrings(evidenceKeys)); err != nil {
		return err
	}
	for i := range causes {
		causes[i].IncidentID = incidentID
	}
	return s.repository.CreateRootCauseCandidates(ctx, causes)
}

func (s *Service) recordActivity(ctx context.Context, incidentID int64, actorID *int64, action string, detail map[string]any) error {
	raw, _ := json.Marshal(detail)
	return s.repository.CreateIncidentActivity(ctx, &model.IncidentActivity{IncidentID: incidentID, ActorID: actorID, Action: action, Detail: raw})
}

func normalizeCreate(input CreateInput, actorID int64) (*model.Incident, []model.IncidentRootCauseCandidate, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, nil, ErrInvalidInput
	}
	severity := normalizeSeverity(input.Severity)
	status := normalizeStatus(input.Status)
	if severity == "" || status == "" {
		return nil, nil, ErrInvalidInput
	}
	createdBy := actorID
	causes, err := normalizeRootCauses(input.RootCauses)
	if err != nil {
		return nil, nil, err
	}
	return &model.Incident{
		Title:          title,
		Severity:       severity,
		Status:         status,
		Environment:    cleanString(input.Environment),
		SystemName:     cleanString(input.SystemName),
		ComponentName:  cleanString(input.ComponentName),
		Summary:        cleanString(input.Summary),
		AnalysisTaskID: input.AnalysisTaskID,
		CreatedBy:      &createdBy,
	}, causes, nil
}

func normalizeUpdate(input UpdateInput) (repository.IncidentUpdates, bool, error) {
	updates := repository.IncidentUpdates{}
	lifecycle := false
	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		if title == "" {
			return updates, false, ErrInvalidInput
		}
		updates.Title = &title
	}
	if input.Severity != nil {
		severity := normalizeSeverity(*input.Severity)
		if severity == "" {
			return updates, false, ErrInvalidInput
		}
		updates.Severity = &severity
	}
	if input.Status != nil {
		status := normalizeStatus(*input.Status)
		if status == "" {
			return updates, false, ErrInvalidInput
		}
		updates.Status = &status
		lifecycle = true
		now := time.Now().UTC()
		if status == model.IncidentStatusResolved {
			updates.ResolvedAt = &now
		}
		if status == model.IncidentStatusClosed {
			updates.ClosedAt = &now
		}
	}
	if input.EnvironmentSet {
		updates.Environment = cleanStringPtr(input.Environment)
		updates.EnvironmentSet = true
	}
	if input.SystemSet {
		updates.SystemName = cleanStringPtr(input.SystemName)
		updates.SystemSet = true
	}
	if input.ComponentSet {
		updates.ComponentName = cleanStringPtr(input.ComponentName)
		updates.ComponentSet = true
	}
	if input.SummarySet {
		updates.Summary = cleanStringPtr(input.Summary)
		updates.SummarySet = true
	}
	return updates, lifecycle, nil
}

func normalizeRootCauses(inputs []RootCauseCandidateIn) ([]model.IncidentRootCauseCandidate, error) {
	result := make([]model.IncidentRootCauseCandidate, 0, len(inputs))
	for _, input := range inputs {
		summary := strings.TrimSpace(input.Summary)
		if summary == "" || input.Score < 0 || input.Score > 1 {
			return nil, ErrInvalidInput
		}
		details := input.Details
		if len(details) == 0 {
			details = []byte(`{}`)
		}
		if !json.Valid(details) {
			return nil, ErrInvalidInput
		}
		result = append(result, model.IncidentRootCauseCandidate{Summary: summary, Score: input.Score, Details: details})
	}
	return result, nil
}

func normalizeSeverity(value string) string {
	switch strings.TrimSpace(value) {
	case "":
		return model.IncidentSeverityWarning
	case model.IncidentSeverityCritical, model.IncidentSeverityWarning, model.IncidentSeverityInfo:
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

func normalizeStatus(value string) string {
	switch strings.TrimSpace(value) {
	case "":
		return model.IncidentStatusOpen
	case model.IncidentStatusOpen, model.IncidentStatusMitigating, model.IncidentStatusResolved, model.IncidentStatusClosed:
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

func cleanString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func cleanStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	return cleanString(*value)
}

func uniqueInt64(values []int64) []int64 {
	seen := map[int64]struct{}{}
	result := []int64{}
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
