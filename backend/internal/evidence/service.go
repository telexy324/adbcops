package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"aiops-platform/backend/internal/auditutil"
	datasourcesvc "aiops-platform/backend/internal/datasource"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

var (
	ErrInvalidInput       = errors.New("invalid input")
	ErrEvidenceRefMissing = errors.New("evidence reference missing")
	ErrForbidden          = errors.New("evidence access forbidden")
)

type Repository interface {
	CreateEvidence(ctx context.Context, evidence *model.EvidenceRecord) error
	FindEvidenceByID(ctx context.Context, id int64) (*model.EvidenceRecord, error)
	FindEvidenceByKey(ctx context.Context, key string) (*model.EvidenceRecord, error)
	ListEvidence(ctx context.Context, filters repository.EvidenceFilters) ([]model.EvidenceRecord, error)
	MissingEvidenceKeys(ctx context.Context, keys []string) ([]string, error)
}

type Service struct {
	repository  Repository
	dataSources DataSourceLister
}

type DataSourceLister interface {
	List(ctx context.Context, actor *model.AppUser) ([]datasourcesvc.DataSourceView, error)
}

type CreateInput struct {
	EvidenceKey  string          `json:"evidenceKey"`
	SourceType   string          `json:"sourceType"`
	SourceRef    json.RawMessage `json:"sourceRef"`
	ObservedAt   *time.Time      `json:"observedAt"`
	Title        *string         `json:"title"`
	Summary      string          `json:"summary"`
	Content      json.RawMessage `json:"content"`
	Confidence   *float64        `json:"confidence"`
	Sensitivity  string          `json:"sensitivity"`
	RCARunID     *int64          `json:"rcaRunId"`
	RCARoundID   *int64          `json:"rcaRoundId"`
	RCAActionID  *int64          `json:"rcaActionId"`
	EvidenceKind string          `json:"evidenceKind"`
	Entity       json.RawMessage `json:"entity"`
	WindowStart  *time.Time      `json:"windowStart"`
	WindowEnd    *time.Time      `json:"windowEnd"`
	SourceSkill  string          `json:"sourceSkill"`
	DataSourceID *int64          `json:"dataSourceId"`
	OwnerUserID  *int64          `json:"ownerUserId"`
}

type Query struct {
	Limit       int
	SourceType  string
	Sensitivity string
	From        *time.Time
	To          *time.Time
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) WithDataSourceLister(dataSources DataSourceLister) *Service {
	s.dataSources = dataSources
	return s
}

func (s *Service) Create(ctx context.Context, input CreateInput) (*model.EvidenceRecord, error) {
	record, err := normalize(input)
	if err != nil {
		return nil, err
	}
	if err := s.repository.CreateEvidence(ctx, record); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *Service) List(ctx context.Context, query Query) ([]model.EvidenceRecord, error) {
	return s.repository.ListEvidence(ctx, repository.EvidenceFilters{
		Limit:       query.Limit,
		SourceType:  strings.TrimSpace(query.SourceType),
		Sensitivity: strings.TrimSpace(query.Sensitivity),
		From:        query.From,
		To:          query.To,
	})
}

func (s *Service) ListAuthorized(ctx context.Context, actor *model.AppUser, query Query) ([]model.EvidenceRecord, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	filters := repository.EvidenceFilters{
		Limit: query.Limit, SourceType: strings.TrimSpace(query.SourceType),
		Sensitivity: strings.TrimSpace(query.Sensitivity), From: query.From, To: query.To,
		IncludeAllRCA: actor.Role == model.RoleAdmin,
	}
	if actor.Role != model.RoleAdmin {
		filters.OwnerUserID = &actor.ID
		ids, err := s.accessibleDataSourceIDs(ctx, actor)
		if err != nil {
			return nil, err
		}
		filters.AllowedDataSourceIDs = ids
	}
	return s.repository.ListEvidence(ctx, filters)
}

func (s *Service) GetByID(ctx context.Context, id int64) (*model.EvidenceRecord, error) {
	if id <= 0 {
		return nil, ErrInvalidInput
	}
	return s.repository.FindEvidenceByID(ctx, id)
}

func (s *Service) GetByKey(ctx context.Context, key string) (*model.EvidenceRecord, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, ErrInvalidInput
	}
	return s.repository.FindEvidenceByKey(ctx, key)
}

func (s *Service) GetByIDAuthorized(ctx context.Context, actor *model.AppUser, id int64) (*model.EvidenceRecord, error) {
	record, err := s.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeRecord(ctx, actor, record); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *Service) GetByKeyAuthorized(ctx context.Context, actor *model.AppUser, key string) (*model.EvidenceRecord, error) {
	record, err := s.GetByKey(ctx, key)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeRecord(ctx, actor, record); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *Service) ValidateReferences(ctx context.Context, keys []string) error {
	unique := normalizeKeys(keys)
	if len(unique) == 0 {
		return nil
	}
	missing, err := s.repository.MissingEvidenceKeys(ctx, unique)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return errors.Join(ErrEvidenceRefMissing, errors.New(strings.Join(missing, ",")))
	}
	return nil
}

func normalize(input CreateInput) (*model.EvidenceRecord, error) {
	sourceType := strings.TrimSpace(input.SourceType)
	summary := truncateString(auditutil.SanitizeText(strings.TrimSpace(input.Summary)), 2048)
	if sourceType == "" || summary == "" {
		return nil, ErrInvalidInput
	}
	sourceRef := sanitizeJSON(input.SourceRef, 8<<10)
	content := sanitizeJSON(input.Content, 64<<10)
	if sourceRef == nil || content == nil {
		return nil, ErrInvalidInput
	}
	sensitivity := strings.TrimSpace(input.Sensitivity)
	if sensitivity == "" {
		sensitivity = model.EvidenceSensitivityInternal
	}
	if !validSensitivity(sensitivity) {
		return nil, ErrInvalidInput
	}
	if input.Confidence != nil && (*input.Confidence < 0 || *input.Confidence > 1) {
		return nil, ErrInvalidInput
	}
	if input.WindowStart != nil && input.WindowEnd != nil && input.WindowStart.After(*input.WindowEnd) {
		return nil, ErrInvalidInput
	}
	kind := strings.TrimSpace(input.EvidenceKind)
	if kind != "" && !validEvidenceKind(kind) {
		return nil, ErrInvalidInput
	}
	entity := sanitizeJSON(input.Entity, 8<<10)
	if entity == nil {
		return nil, ErrInvalidInput
	}
	sourceSkill := strings.TrimSpace(input.SourceSkill)
	if input.RCARunID != nil {
		if input.RCARoundID == nil || input.OwnerUserID == nil || kind == "" || sourceSkill == "" {
			return nil, ErrInvalidInput
		}
	} else if input.RCARoundID != nil || input.RCAActionID != nil {
		return nil, ErrInvalidInput
	}
	key := strings.TrimSpace(input.EvidenceKey)
	if key == "" {
		key = generatedEvidenceKey(sourceType, sourceRef, summary)
	}
	if len(key) > 100 {
		key = key[:100]
	}
	return &model.EvidenceRecord{
		EvidenceKey: key,
		SourceType:  sourceType,
		SourceRef:   sourceRef,
		ObservedAt:  input.ObservedAt,
		Title:       cleanStringPtr(input.Title),
		Summary:     summary,
		Content:     content,
		Confidence:  input.Confidence,
		Sensitivity: &sensitivity,
		RCARunID:    input.RCARunID, RCARoundID: input.RCARoundID, RCAActionID: input.RCAActionID,
		EvidenceKind: cleanValuePtr(kind), Entity: entity, WindowStart: input.WindowStart, WindowEnd: input.WindowEnd,
		SourceSkill: cleanValuePtr(sourceSkill), DataSourceID: input.DataSourceID, OwnerUserID: input.OwnerUserID,
	}, nil
}

func sanitizeJSON(raw json.RawMessage, maxBytes int) []byte {
	valid := validJSONOrEmpty(raw)
	if valid == nil {
		return nil
	}
	return auditutil.SanitizeJSON(valid, maxBytes)
}

func validJSONOrEmpty(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte(`{}`)
	}
	if !json.Valid(raw) {
		return nil
	}
	return raw
}

func generatedEvidenceKey(sourceType string, sourceRef []byte, summary string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(sourceType) + "|" + string(sourceRef) + "|" + summary))
	return "ev_" + hex.EncodeToString(sum[:])[:32]
}

func cleanStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cleaned := strings.TrimSpace(*value)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}

func validSensitivity(value string) bool {
	switch value {
	case model.EvidenceSensitivityPublic,
		model.EvidenceSensitivityInternal,
		model.EvidenceSensitivityConfidential,
		model.EvidenceSensitivityRestricted:
		return true
	default:
		return false
	}
}

func validEvidenceKind(value string) bool {
	switch value {
	case model.EvidenceKindFact, model.EvidenceKindRule, model.EvidenceKindKnowledge, model.EvidenceKindModelHypothesis:
		return true
	default:
		return false
	}
}

func cleanValuePtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func truncateString(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum])
}

func (s *Service) authorizeRecord(ctx context.Context, actor *model.AppUser, record *model.EvidenceRecord) error {
	if actor == nil {
		return ErrForbidden
	}
	if record.RCARunID == nil || actor.Role == model.RoleAdmin {
		return nil
	}
	if record.OwnerUserID == nil || *record.OwnerUserID != actor.ID {
		return ErrForbidden
	}
	if record.DataSourceID == nil {
		return nil
	}
	ids, err := s.accessibleDataSourceIDs(ctx, actor)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if id == *record.DataSourceID {
			return nil
		}
	}
	return ErrForbidden
}

func (s *Service) accessibleDataSourceIDs(ctx context.Context, actor *model.AppUser) ([]int64, error) {
	if s.dataSources == nil {
		return nil, nil
	}
	views, err := s.dataSources.List(ctx, actor)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for _, view := range views {
		if view.Enabled && view.ReadOnly {
			ids = append(ids, view.ID)
		}
	}
	return ids, nil
}

func normalizeKeys(keys []string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	return result
}
