package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

var (
	ErrInvalidInput       = errors.New("invalid input")
	ErrEvidenceRefMissing = errors.New("evidence reference missing")
)

type Repository interface {
	CreateEvidence(ctx context.Context, evidence *model.EvidenceRecord) error
	FindEvidenceByID(ctx context.Context, id int64) (*model.EvidenceRecord, error)
	FindEvidenceByKey(ctx context.Context, key string) (*model.EvidenceRecord, error)
	ListEvidence(ctx context.Context, filters repository.EvidenceFilters) ([]model.EvidenceRecord, error)
	MissingEvidenceKeys(ctx context.Context, keys []string) ([]string, error)
}

type Service struct {
	repository Repository
}

type CreateInput struct {
	EvidenceKey string          `json:"evidenceKey"`
	SourceType  string          `json:"sourceType"`
	SourceRef   json.RawMessage `json:"sourceRef"`
	ObservedAt  *time.Time      `json:"observedAt"`
	Title       *string         `json:"title"`
	Summary     string          `json:"summary"`
	Content     json.RawMessage `json:"content"`
	Confidence  *float64        `json:"confidence"`
	Sensitivity string          `json:"sensitivity"`
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
	summary := strings.TrimSpace(input.Summary)
	if sourceType == "" || summary == "" {
		return nil, ErrInvalidInput
	}
	sourceRef := validJSONOrEmpty(input.SourceRef)
	content := validJSONOrEmpty(input.Content)
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
	}, nil
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
