package timeline

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

var ErrInvalidInput = errors.New("invalid input")

type EventRepository interface {
	FindOpsEventByID(ctx context.Context, id int64) (*model.OpsEvent, error)
	ListOpsEvents(ctx context.Context, filters repository.EventFilters) ([]model.OpsEvent, error)
}

type EvidenceRepository interface {
	FindEvidenceByKey(ctx context.Context, key string) (*model.EvidenceRecord, error)
}

type Service struct {
	events   EventRepository
	evidence EvidenceRepository
}

type Query struct {
	Limit               int
	SourceType          string
	Environment         string
	SystemName          string
	ComponentName       string
	Namespace           string
	ResourceName        string
	From                *time.Time
	To                  *time.Time
	AnchorEventID       int64
	BeforeMinutes       int
	AfterMinutes        int
	IncludeEvidence     bool
	MaxEvidencePerEvent int
}

type Result struct {
	From            time.Time      `json:"from"`
	To              time.Time      `json:"to"`
	Timezone        string         `json:"timezone"`
	AnchorEventID   *int64         `json:"anchorEventId,omitempty"`
	Items           []Item         `json:"items"`
	SourceCounts    map[string]int `json:"sourceCounts"`
	EvidenceMissing []string       `json:"evidenceMissing,omitempty"`
}

type Item struct {
	EventID        int64                  `json:"eventId"`
	Time           time.Time              `json:"time"`
	SourceType     string                 `json:"sourceType"`
	EventType      string                 `json:"eventType"`
	Severity       *string                `json:"severity,omitempty"`
	Status         string                 `json:"status"`
	Environment    *string                `json:"environment,omitempty"`
	SystemName     *string                `json:"systemName,omitempty"`
	ComponentName  *string                `json:"componentName,omitempty"`
	Namespace      *string                `json:"namespace,omitempty"`
	ResourceKind   *string                `json:"resourceKind,omitempty"`
	ResourceName   *string                `json:"resourceName,omitempty"`
	Summary        string                 `json:"summary"`
	EvidenceKeys   []string               `json:"evidenceKeys,omitempty"`
	Evidence       []EvidenceSummary      `json:"evidence,omitempty"`
	PayloadSummary map[string]interface{} `json:"payloadSummary,omitempty"`
}

type EvidenceSummary struct {
	EvidenceKey string   `json:"evidenceKey"`
	SourceType  string   `json:"sourceType"`
	Title       *string  `json:"title,omitempty"`
	Summary     string   `json:"summary"`
	Sensitivity *string  `json:"sensitivity,omitempty"`
	Confidence  *float64 `json:"confidence,omitempty"`
}

func NewService(events EventRepository, evidence EvidenceRepository) *Service {
	return &Service{events: events, evidence: evidence}
}

func (s *Service) Build(ctx context.Context, query Query) (*Result, error) {
	window, anchorID, err := s.resolveWindow(ctx, query)
	if err != nil {
		return nil, err
	}
	limit := query.Limit
	if limit <= 0 || limit > 1000 {
		limit = 300
	}
	events, err := s.events.ListOpsEvents(ctx, repository.EventFilters{
		Limit:         limit,
		SourceType:    strings.TrimSpace(query.SourceType),
		Environment:   strings.TrimSpace(query.Environment),
		SystemName:    strings.TrimSpace(query.SystemName),
		ComponentName: strings.TrimSpace(query.ComponentName),
		Namespace:     strings.TrimSpace(query.Namespace),
		ResourceName:  strings.TrimSpace(query.ResourceName),
		From:          &window.from,
		To:            &window.to,
	})
	if err != nil {
		return nil, err
	}
	sortEventsStable(events)

	result := &Result{
		From:          window.from,
		To:            window.to,
		Timezone:      "UTC",
		AnchorEventID: anchorID,
		Items:         make([]Item, 0, len(events)),
		SourceCounts:  map[string]int{},
	}
	maxEvidence := query.MaxEvidencePerEvent
	if maxEvidence <= 0 || maxEvidence > 20 {
		maxEvidence = 5
	}
	missingEvidence := map[string]struct{}{}
	for _, event := range events {
		item := eventToItem(event)
		result.SourceCounts[item.SourceType]++
		if query.IncludeEvidence && s.evidence != nil {
			item.Evidence = s.resolveEvidence(ctx, item.EvidenceKeys, maxEvidence, missingEvidence)
		}
		result.Items = append(result.Items, item)
	}
	for key := range missingEvidence {
		result.EvidenceMissing = append(result.EvidenceMissing, key)
	}
	sort.Strings(result.EvidenceMissing)
	return result, nil
}

type window struct {
	from time.Time
	to   time.Time
}

func (s *Service) resolveWindow(ctx context.Context, query Query) (window, *int64, error) {
	if query.AnchorEventID > 0 {
		event, err := s.events.FindOpsEventByID(ctx, query.AnchorEventID)
		if err != nil {
			return window{}, nil, err
		}
		before := query.BeforeMinutes
		after := query.AfterMinutes
		if before <= 0 {
			before = 30
		}
		if after <= 0 {
			after = 30
		}
		if before > 24*60 || after > 24*60 {
			return window{}, nil, ErrInvalidInput
		}
		anchor := event.EventTime.UTC()
		id := event.ID
		return window{from: anchor.Add(-time.Duration(before) * time.Minute), to: anchor.Add(time.Duration(after) * time.Minute)}, &id, nil
	}
	if query.From == nil || query.To == nil {
		return window{}, nil, ErrInvalidInput
	}
	from := query.From.UTC()
	to := query.To.UTC()
	if to.Before(from) {
		return window{}, nil, ErrInvalidInput
	}
	return window{from: from, to: to}, nil, nil
}

func eventToItem(event model.OpsEvent) Item {
	keys, payloadSummary := evidenceKeysFromPayload(event.Payload)
	return Item{
		EventID:        event.ID,
		Time:           event.EventTime.UTC(),
		SourceType:     event.SourceType,
		EventType:      event.EventType,
		Severity:       event.Severity,
		Status:         event.Status,
		Environment:    event.Environment,
		SystemName:     event.SystemName,
		ComponentName:  event.ComponentName,
		Namespace:      event.Namespace,
		ResourceKind:   event.ResourceKind,
		ResourceName:   event.ResourceName,
		Summary:        event.Summary,
		EvidenceKeys:   keys,
		PayloadSummary: payloadSummary,
	}
}

func (s *Service) resolveEvidence(ctx context.Context, keys []string, maxEvidence int, missing map[string]struct{}) []EvidenceSummary {
	result := []EvidenceSummary{}
	for _, key := range keys {
		if len(result) >= maxEvidence {
			break
		}
		record, err := s.evidence.FindEvidenceByKey(ctx, key)
		if err != nil {
			missing[key] = struct{}{}
			continue
		}
		result = append(result, EvidenceSummary{
			EvidenceKey: record.EvidenceKey,
			SourceType:  record.SourceType,
			Title:       record.Title,
			Summary:     record.Summary,
			Sensitivity: record.Sensitivity,
			Confidence:  record.Confidence,
		})
	}
	return result
}

func evidenceKeysFromPayload(raw []byte) ([]string, map[string]interface{}) {
	if len(raw) == 0 {
		return nil, nil
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, nil
	}
	keys := normalizeEvidenceKeysFromAny(payload["evidenceKeys"])
	keys = append(keys, normalizeEvidenceKeysFromAny(payload["evidenceKey"])...)
	keys = append(keys, normalizeEvidenceKeysFromAny(payload["evidence_refs"])...)
	keys = uniqueStrings(keys)
	summary := map[string]interface{}{}
	for _, key := range []string{"message", "reason", "count", "errorCount", "durationMs"} {
		if value, ok := payload[key]; ok {
			summary[key] = value
		}
	}
	if len(summary) == 0 {
		summary = nil
	}
	return keys, summary
}

func normalizeEvidenceKeysFromAny(value interface{}) []string {
	switch typed := value.(type) {
	case string:
		return []string{strings.TrimSpace(typed)}
	case []interface{}:
		keys := make([]string, 0, len(typed))
		for _, item := range typed {
			if key, ok := item.(string); ok {
				keys = append(keys, strings.TrimSpace(key))
			}
		}
		return keys
	default:
		return nil
	}
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

func sortEventsStable(events []model.OpsEvent) {
	sort.SliceStable(events, func(i, j int) bool {
		left := events[i]
		right := events[j]
		if !left.EventTime.Equal(right.EventTime) {
			return left.EventTime.UTC().Before(right.EventTime.UTC())
		}
		if left.SourceType != right.SourceType {
			return left.SourceType < right.SourceType
		}
		if left.EventType != right.EventType {
			return left.EventType < right.EventType
		}
		return left.ID < right.ID
	})
}
