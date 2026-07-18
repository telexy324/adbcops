package linuxevent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"aiops-platform/backend/internal/model"
)

var ErrInvalidInput = errors.New("invalid linux event input")

const (
	FindingTypeFact = "FACT"
	FindingTypeRule = "RULE"
)

type EventRepository interface {
	UpsertOpsEvent(ctx context.Context, event *model.OpsEvent) (*model.OpsEvent, error)
}

type EvidenceRepository interface {
	CreateEvidence(ctx context.Context, evidence *model.EvidenceRecord) error
}

type IncidentRepository interface {
	LinkIncidentEvents(ctx context.Context, incidentID int64, eventIDs []int64) error
	LinkIncidentEvidence(ctx context.Context, incidentID int64, keys []string) error
}

type Service struct {
	events    EventRepository
	evidence  EvidenceRepository
	incidents IncidentRepository
}

type RecordInput struct {
	HostID           int64           `json:"hostId"`
	HostName         string          `json:"hostName"`
	Host             string          `json:"host"`
	Environment      string          `json:"environment"`
	SystemName       string          `json:"systemName"`
	ComponentName    string          `json:"componentName"`
	EventType        string          `json:"eventType"`
	Severity         string          `json:"severity"`
	Status           string          `json:"status"`
	ResourceIdentity string          `json:"resourceIdentity"`
	ObservedAt       *time.Time      `json:"observedAt"`
	Summary          string          `json:"summary"`
	Collector        string          `json:"collector"`
	CommandVersion   string          `json:"commandVersion"`
	FindingType      string          `json:"findingType"`
	Content          json.RawMessage `json:"content"`
	Confidence       *float64        `json:"confidence"`
	Sensitivity      string          `json:"sensitivity"`
	IncidentID       *int64          `json:"incidentId"`
}

type RecordResult struct {
	Event    *model.OpsEvent       `json:"event"`
	Evidence *model.EvidenceRecord `json:"evidence"`
}

func NewService(events EventRepository, evidence EvidenceRepository, incidents IncidentRepository) *Service {
	return &Service{events: events, evidence: evidence, incidents: incidents}
}

func (s *Service) Record(ctx context.Context, input RecordInput) (*RecordResult, error) {
	if s.events == nil || s.evidence == nil {
		return nil, ErrInvalidInput
	}
	normalized, err := normalize(input)
	if err != nil {
		return nil, err
	}
	evidence := buildEvidence(normalized)
	if err := s.evidence.CreateEvidence(ctx, evidence); err != nil {
		return nil, fmt.Errorf("create linux evidence: %w", err)
	}
	event := buildEvent(normalized, evidence.EvidenceKey)
	stored, err := s.events.UpsertOpsEvent(ctx, event)
	if err != nil {
		return nil, fmt.Errorf("upsert linux event: %w", err)
	}
	if normalized.IncidentID != nil {
		if s.incidents == nil {
			return nil, ErrInvalidInput
		}
		if err := s.incidents.LinkIncidentEvents(ctx, *normalized.IncidentID, []int64{stored.ID}); err != nil {
			return nil, fmt.Errorf("link linux event to incident: %w", err)
		}
		if err := s.incidents.LinkIncidentEvidence(ctx, *normalized.IncidentID, []string{evidence.EvidenceKey}); err != nil {
			return nil, fmt.Errorf("link linux evidence to incident: %w", err)
		}
	}
	return &RecordResult{Event: stored, Evidence: evidence}, nil
}

func Fingerprint(eventType, environment string, hostID int64, resourceIdentity string) string {
	canonical := strings.Join([]string{
		strings.ToLower(strings.TrimSpace(eventType)),
		strings.ToLower(strings.TrimSpace(environment)),
		fmt.Sprintf("%d", hostID),
		strings.ToLower(strings.TrimSpace(resourceIdentity)),
	}, "|")
	sum := sha256.Sum256([]byte(canonical))
	return "linux_" + hex.EncodeToString(sum[:])[:40]
}

func normalize(input RecordInput) (RecordInput, error) {
	input.EventType = strings.TrimSpace(input.EventType)
	input.Summary = strings.TrimSpace(input.Summary)
	input.HostName = strings.TrimSpace(input.HostName)
	input.Host = strings.TrimSpace(input.Host)
	input.Environment = strings.TrimSpace(input.Environment)
	input.SystemName = strings.TrimSpace(input.SystemName)
	input.ComponentName = strings.TrimSpace(input.ComponentName)
	input.ResourceIdentity = strings.TrimSpace(input.ResourceIdentity)
	input.Collector = strings.TrimSpace(input.Collector)
	input.CommandVersion = strings.TrimSpace(input.CommandVersion)
	input.FindingType = strings.ToUpper(strings.TrimSpace(input.FindingType))
	if input.HostID <= 0 || input.Summary == "" || !validLinuxEventType(input.EventType) {
		return RecordInput{}, ErrInvalidInput
	}
	if input.FindingType == "" {
		input.FindingType = FindingTypeRule
	}
	if input.FindingType != FindingTypeFact && input.FindingType != FindingTypeRule {
		return RecordInput{}, ErrInvalidInput
	}
	if input.Confidence != nil && (*input.Confidence < 0 || *input.Confidence > 1) {
		return RecordInput{}, ErrInvalidInput
	}
	if input.IncidentID != nil && *input.IncidentID <= 0 {
		return RecordInput{}, ErrInvalidInput
	}
	if input.ObservedAt == nil || input.ObservedAt.IsZero() {
		now := time.Now().UTC()
		input.ObservedAt = &now
	} else {
		observed := input.ObservedAt.UTC()
		input.ObservedAt = &observed
	}
	input.Status = normalizeStatus(input.Status)
	if input.Status == "" {
		return RecordInput{}, ErrInvalidInput
	}
	input.Severity = strings.ToLower(strings.TrimSpace(input.Severity))
	if input.Severity == "" {
		input.Severity = "warning"
	}
	if input.EventType == model.EventTypeLinuxSSHHostKeyChanged {
		input.Severity = "high"
		input.Status = model.EventStatusFiring
	}
	if input.ResourceIdentity == "" {
		input.ResourceIdentity = firstNonEmpty(input.Collector, input.HostName, input.Host, fmt.Sprintf("host-%d", input.HostID))
	}
	if input.Collector == "" {
		input.Collector = collectorForEvent(input.EventType)
	}
	content, err := sanitizeContent(input.Content)
	if err != nil {
		return RecordInput{}, ErrInvalidInput
	}
	input.Content = content
	input.Sensitivity = strings.ToLower(strings.TrimSpace(input.Sensitivity))
	if input.Sensitivity == "" {
		input.Sensitivity = model.EvidenceSensitivityInternal
	}
	if !validSensitivity(input.Sensitivity) {
		return RecordInput{}, ErrInvalidInput
	}
	return input, nil
}

func buildEvidence(input RecordInput) *model.EvidenceRecord {
	observed := input.ObservedAt.UTC()
	sourceRef, _ := json.Marshal(map[string]any{
		"hostId": input.HostID, "collector": input.Collector, "commandVersion": input.CommandVersion,
	})
	title := input.HostName
	if title == "" {
		title = firstNonEmpty(input.Host, fmt.Sprintf("Linux host %d", input.HostID))
	}
	title += " " + input.Collector + " evidence"
	sensitivity := input.Sensitivity
	return &model.EvidenceRecord{
		EvidenceKey: evidenceKey(input, observed), SourceType: model.EventSourceLinuxServer,
		SourceRef: sourceRef, ObservedAt: &observed, Title: &title, Summary: input.Summary,
		Content: input.Content, Confidence: input.Confidence, Sensitivity: &sensitivity,
	}
}

func buildEvent(input RecordInput, evidenceKey string) *model.OpsEvent {
	observed := input.ObservedAt.UTC()
	sourceID := fmt.Sprintf("%d", input.HostID)
	resourceKind := "linux_host"
	fingerprint := Fingerprint(input.EventType, input.Environment, input.HostID, input.ResourceIdentity)
	payload, _ := json.Marshal(map[string]any{
		"hostId": input.HostID, "collector": input.Collector, "commandVersion": input.CommandVersion,
		"findingType": input.FindingType, "resourceIdentity": input.ResourceIdentity,
		"evidenceKey": evidenceKey, "evidenceKeys": []string{evidenceKey},
	})
	var resolvedAt *time.Time
	if input.Status == model.EventStatusResolved {
		resolvedAt = &observed
	}
	return &model.OpsEvent{
		EventTime: observed, SourceType: model.EventSourceLinuxServer, SourceID: &sourceID,
		EventType: input.EventType, Severity: stringPtr(input.Severity), Status: input.Status,
		Environment: stringPtr(input.Environment), SystemName: stringPtr(input.SystemName),
		ComponentName: stringPtr(input.ComponentName), ResourceKind: &resourceKind,
		ResourceName: stringPtr(input.HostName), Host: stringPtr(input.Host), Fingerprint: &fingerprint,
		Summary: input.Summary, Payload: payload, OccurrenceCount: 1,
		FirstSeenAt: observed, LastSeenAt: observed, ResolvedAt: resolvedAt,
	}
}

func evidenceKey(input RecordInput, observed time.Time) string {
	contentHash := sha256.Sum256(input.Content)
	seed := fmt.Sprintf("%d|%s|%s|%d|%x", input.HostID, input.EventType, input.Collector, observed.UnixNano(), contentHash[:8])
	sum := sha256.Sum256([]byte(seed))
	return "linux_ev_" + hex.EncodeToString(sum[:])[:32]
}

func sanitizeContent(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	value = stripRawOutput(value)
	return json.Marshal(value)
}

func stripRawOutput(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		cleaned := make(map[string]any, len(typed))
		for key, child := range typed {
			if rawOutputKey(key) {
				continue
			}
			cleaned[key] = stripRawOutput(child)
		}
		return cleaned
	case []any:
		for i := range typed {
			typed[i] = stripRawOutput(typed[i])
		}
		return typed
	default:
		return value
	}
}

func rawOutputKey(key string) bool {
	normalized := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(key)))
	switch normalized {
	case "raw", "rawoutput", "rawcommandoutput", "commandoutput", "stdout", "stderr":
		return true
	default:
		return false
	}
}

func validLinuxEventType(value string) bool {
	_, ok := linuxEventTypes[value]
	return ok
}

var linuxEventTypes = map[string]struct{}{
	model.EventTypeLinuxHostUnreachable: {}, model.EventTypeLinuxSSHAuthFailed: {},
	model.EventTypeLinuxSSHHostKeyChanged: {}, model.EventTypeLinuxCPUPressure: {},
	model.EventTypeLinuxMemoryPressure: {}, model.EventTypeLinuxSwapPressure: {},
	model.EventTypeLinuxFilesystemPressure: {}, model.EventTypeLinuxInodePressure: {},
	model.EventTypeLinuxDiskIOPressure: {}, model.EventTypeLinuxNetworkError: {},
	model.EventTypeLinuxProcessZombie: {}, model.EventTypeLinuxProcessDState: {},
	model.EventTypeLinuxServiceFailed: {}, model.EventTypeLinuxTimeUnsynchronized: {},
	model.EventTypeLinuxKernelOOM: {}, model.EventTypeLinuxKernelIOError: {},
	model.EventTypeLinuxFilesystemReadOnly: {},
}

func collectorForEvent(eventType string) string {
	value := strings.TrimPrefix(eventType, "linux_")
	if strings.HasPrefix(value, "ssh_") || value == "host_unreachable" {
		return "connection"
	}
	return strings.TrimSuffix(strings.TrimSuffix(value, "_pressure"), "_error")
}

func normalizeStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", model.EventStatusFiring:
		return model.EventStatusFiring
	case model.EventStatusObserved:
		return model.EventStatusObserved
	case model.EventStatusResolved:
		return model.EventStatusResolved
	default:
		return ""
	}
}

func validSensitivity(value string) bool {
	switch value {
	case model.EvidenceSensitivityPublic, model.EvidenceSensitivityInternal,
		model.EvidenceSensitivityConfidential, model.EvidenceSensitivityRestricted:
		return true
	default:
		return false
	}
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
