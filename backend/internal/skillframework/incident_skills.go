package skillframework

import (
	"context"
	"encoding/json"
	"time"

	correlationsvc "aiops-platform/backend/internal/correlation"
	"aiops-platform/backend/internal/model"
	timelinesvc "aiops-platform/backend/internal/timeline"
)

type TimelineBuilder interface {
	Build(ctx context.Context, query timelinesvc.Query) (*timelinesvc.Result, error)
}

type CorrelationAnalyzer interface {
	Analyze(ctx context.Context, query correlationsvc.Query) (*correlationsvc.Result, error)
}

func IncidentAnalysisSkills(timeline TimelineBuilder, correlation CorrelationAnalyzer) []Skill {
	return []Skill{
		BuildIncidentTimelineSkill{timeline: timeline},
		CorrelateIncidentEventsSkill{correlation: correlation},
	}
}

type BuildIncidentTimelineSkill struct {
	timeline TimelineBuilder
}

func (s BuildIncidentTimelineSkill) Definition() SkillDefinition {
	return SkillDefinition{
		Name:          "build_incident_timeline",
		Version:       "v1",
		Description:   "Build an evidence-aware incident timeline around a target event or explicit time window.",
		InputSchema:   json.RawMessage(`{"type":"object","properties":{"anchorEventId":{"type":"integer"},"from":{"type":"string"},"to":{"type":"string"},"beforeMinutes":{"type":"integer"},"afterMinutes":{"type":"integer"},"environment":{"type":"string"},"systemName":{"type":"string"},"componentName":{"type":"string"},"namespace":{"type":"string"},"resourceName":{"type":"string"},"limit":{"type":"integer"},"includeEvidence":{"type":"boolean"},"maxEvidencePerEvent":{"type":"integer"}}}`),
		OutputSchema:  partialOutputSchema(),
		RiskLevel:     model.SkillRiskSensitiveRead,
		ReadOnly:      true,
		TimeoutSecond: 20,
		RequiredTools: nil,
	}
}

func (s BuildIncidentTimelineSkill) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var request struct {
		AnchorEventID       int64  `json:"anchorEventId"`
		From                string `json:"from"`
		To                  string `json:"to"`
		BeforeMinutes       int    `json:"beforeMinutes"`
		AfterMinutes        int    `json:"afterMinutes"`
		Environment         string `json:"environment"`
		SystemName          string `json:"systemName"`
		ComponentName       string `json:"componentName"`
		Namespace           string `json:"namespace"`
		ResourceName        string `json:"resourceName"`
		Limit               int    `json:"limit"`
		IncludeEvidence     bool   `json:"includeEvidence"`
		MaxEvidencePerEvent int    `json:"maxEvidencePerEvent"`
	}
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, ErrInvalidInput
	}
	var from *time.Time
	if request.From != "" {
		parsed, err := parseSkillTime(request.From)
		if err != nil {
			return nil, ErrInvalidInput
		}
		from = &parsed
	}
	var to *time.Time
	if request.To != "" {
		parsed, err := parseSkillTime(request.To)
		if err != nil {
			return nil, ErrInvalidInput
		}
		to = &parsed
	}
	if s.timeline == nil {
		return partialError("timeline", "timeline service is not configured"), nil
	}
	result, err := s.timeline.Build(ctx, timelinesvc.Query{
		Limit:               request.Limit,
		Environment:         request.Environment,
		SystemName:          request.SystemName,
		ComponentName:       request.ComponentName,
		Namespace:           request.Namespace,
		ResourceName:        request.ResourceName,
		From:                from,
		To:                  to,
		AnchorEventID:       request.AnchorEventID,
		BeforeMinutes:       request.BeforeMinutes,
		AfterMinutes:        request.AfterMinutes,
		IncludeEvidence:     request.IncludeEvidence,
		MaxEvidencePerEvent: request.MaxEvidencePerEvent,
	})
	if err != nil {
		return partialError("timeline", err.Error()), nil
	}
	return json.Marshal(map[string]any{"partial": false, "timeline": result})
}

type CorrelateIncidentEventsSkill struct {
	correlation CorrelationAnalyzer
}

func (s CorrelateIncidentEventsSkill) Definition() SkillDefinition {
	return SkillDefinition{
		Name:          "correlate_incident_events",
		Version:       "v1",
		Description:   "Rank incident root cause candidates with explainable correlation scoring.",
		InputSchema:   json.RawMessage(`{"type":"object","required":["targetEventId"],"properties":{"targetEventId":{"type":"integer"},"from":{"type":"string"},"to":{"type":"string"},"beforeMinutes":{"type":"integer"},"afterMinutes":{"type":"integer"},"environment":{"type":"string"},"systemName":{"type":"string"},"componentName":{"type":"string"},"namespace":{"type":"string"},"resourceName":{"type":"string"},"limit":{"type":"integer"},"includeTopology":{"type":"boolean"}}}`),
		OutputSchema:  partialOutputSchema(),
		RiskLevel:     model.SkillRiskSensitiveRead,
		ReadOnly:      true,
		TimeoutSecond: 20,
		RequiredTools: nil,
	}
}

func (s CorrelateIncidentEventsSkill) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var request struct {
		TargetEventID   int64  `json:"targetEventId"`
		From            string `json:"from"`
		To              string `json:"to"`
		BeforeMinutes   int    `json:"beforeMinutes"`
		AfterMinutes    int    `json:"afterMinutes"`
		Environment     string `json:"environment"`
		SystemName      string `json:"systemName"`
		ComponentName   string `json:"componentName"`
		Namespace       string `json:"namespace"`
		ResourceName    string `json:"resourceName"`
		Limit           int    `json:"limit"`
		IncludeTopology bool   `json:"includeTopology"`
	}
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, ErrInvalidInput
	}
	if request.TargetEventID <= 0 {
		return nil, ErrInvalidInput
	}
	var from *time.Time
	if request.From != "" {
		parsed, err := parseSkillTime(request.From)
		if err != nil {
			return nil, ErrInvalidInput
		}
		from = &parsed
	}
	var to *time.Time
	if request.To != "" {
		parsed, err := parseSkillTime(request.To)
		if err != nil {
			return nil, ErrInvalidInput
		}
		to = &parsed
	}
	if s.correlation == nil {
		return partialError("correlation", "correlation service is not configured"), nil
	}
	result, err := s.correlation.Analyze(ctx, correlationsvc.Query{
		TargetEventID:   request.TargetEventID,
		From:            from,
		To:              to,
		BeforeMinutes:   request.BeforeMinutes,
		AfterMinutes:    request.AfterMinutes,
		Environment:     request.Environment,
		SystemName:      request.SystemName,
		ComponentName:   request.ComponentName,
		Namespace:       request.Namespace,
		ResourceName:    request.ResourceName,
		Limit:           request.Limit,
		IncludeTopology: request.IncludeTopology,
	})
	if err != nil {
		return partialError("correlation", err.Error()), nil
	}
	return json.Marshal(map[string]any{"partial": false, "correlation": result})
}
