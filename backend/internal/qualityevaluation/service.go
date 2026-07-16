package qualityevaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"aiops-platform/backend/internal/document"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

var (
	ErrInvalidEvaluation   = errors.New("invalid quality evaluation request")
	ErrProfileNotPublished = errors.New("quality profile is not published")
	ErrDocumentNotParsed   = errors.New("document version has not passed parsing")
	ErrUnsupportedMode     = errors.New("only deterministic evaluation mode is available")
	ErrForbidden           = errors.New("quality evaluation access forbidden")
)

type Repository interface {
	FindDocumentVersionByID(context.Context, int64) (*model.KBDocumentVersion, error)
	FindDocumentByID(context.Context, int64) (*model.KBDocument, error)
	ListDocumentVersionBlocks(context.Context, int64) ([]model.KBDocumentBlock, error)
	FindQualityProfile(context.Context, int64) (*model.KBQualityProfile, error)
	FindStructuredQualityStandard(context.Context, int64) (*model.KBStructuredQualityStandard, error)
	FindLatestQualityEvaluation(context.Context, int64, int64, string) (*model.KBQualityEvaluation, error)
	CreateQualityEvaluation(context.Context, *model.KBQualityEvaluation) error
	FindQualityEvaluation(context.Context, int64) (*model.KBQualityEvaluation, error)
	ListQualityRuleResults(context.Context, int64) ([]model.KBQualityRuleResult, error)
}

type Service struct {
	repository Repository
	now        func() time.Time
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository, now: time.Now}
}

type CreateInput struct {
	DocumentVersionID int64    `json:"documentVersionId"`
	QualityProfileID  int64    `json:"qualityProfileId"`
	Mode              string   `json:"mode"`
	SelectedCriteria  []string `json:"selectedCriteria"`
	Force             bool     `json:"force"`
}

func (s *Service) Create(ctx context.Context, actor *model.AppUser, input CreateInput) (*model.KBQualityEvaluation, error) {
	if actor == nil || input.DocumentVersionID <= 0 || input.QualityProfileID <= 0 {
		return nil, ErrInvalidEvaluation
	}
	mode := strings.ToLower(strings.TrimSpace(input.Mode))
	if mode == "" {
		mode = "deterministic"
	}
	if mode != "deterministic" {
		return nil, ErrUnsupportedMode
	}
	version, documentRow, err := s.loadAccessibleVersion(ctx, actor, input.DocumentVersionID)
	if err != nil {
		return nil, err
	}
	var parseQuality document.ParseQuality
	if len(version.ParseQuality) == 0 || json.Unmarshal(version.ParseQuality, &parseQuality) != nil || !parseQuality.ParseSuccess {
		return nil, ErrDocumentNotParsed
	}
	profile, err := s.repository.FindQualityProfile(ctx, input.QualityProfileID)
	if err != nil {
		return nil, err
	}
	standard, err := s.repository.FindStructuredQualityStandard(ctx, profile.StandardID)
	if err != nil {
		return nil, err
	}
	if profile.Status != model.QualityStandardPublished || standard.Status != model.QualityStandardPublished {
		return nil, ErrProfileNotPublished
	}
	if !input.Force {
		cached, cacheErr := s.repository.FindLatestQualityEvaluation(ctx, version.ID, profile.ID, "deterministic")
		if cacheErr == nil {
			return cached, nil
		}
		if !errors.Is(cacheErr, repository.ErrNotFound) {
			return nil, cacheErr
		}
	}
	selected, available := map[string]struct{}{}, map[string]struct{}{}
	for _, criterion := range profile.Criteria {
		available[criterion.CriterionKey] = struct{}{}
	}
	for _, key := range input.SelectedCriteria {
		key = strings.TrimSpace(key)
		if _, ok := available[key]; !ok {
			return nil, fmt.Errorf("%w: unknown criterion %q", ErrInvalidEvaluation, key)
		}
		selected[key] = struct{}{}
	}
	blocks, err := s.repository.ListDocumentVersionBlocks(ctx, version.ID)
	if err != nil {
		return nil, err
	}
	if len(blocks) == 0 {
		return nil, ErrDocumentNotParsed
	}
	schema := document.DocumentSchemaExtraction{Fields: map[string]document.ExtractedField{}}
	if len(version.DocumentSchema) > 0 && json.Unmarshal(version.DocumentSchema, &schema) != nil {
		return nil, ErrDocumentNotParsed
	}
	output := EvaluateDeterministic(EngineInput{Profile: profile, Version: version, ParseQuality: parseQuality, Schema: schema, Blocks: blocks, SelectedCriteria: selected, Now: s.now()})
	parseScore, totalScore, level, now := parseQualityScore(parseQuality), output.ContentScore, output.Level, s.now()
	summary := fmt.Sprintf("Deterministic evaluation assessed %d rules; %d rules await semantic or human evaluation; %d hard-gate violations.", output.AssessedRuleCount, output.PendingRuleCount, len(output.HardGateViolations))
	result := marshal(map[string]any{"hardGateViolations": output.HardGateViolations, "assessedRuleCount": output.AssessedRuleCount, "pendingRuleCount": output.PendingRuleCount, "documentId": documentRow.ID})
	evaluation := &model.KBQualityEvaluation{DocumentVersionID: version.ID, QualityProfileID: profile.ID, QualityProfileVersion: standard.Version, ParseScore: &parseScore, ContentScore: &output.ContentScore, TotalScore: &totalScore, GateStatus: output.GateStatus, Level: &level, Source: "deterministic", Summary: &summary, Result: result, Status: "completed", CompletedAt: &now, RuleResults: output.Results}
	if err := s.repository.CreateQualityEvaluation(ctx, evaluation); err != nil {
		return nil, err
	}
	return evaluation, nil
}

func (s *Service) Get(ctx context.Context, actor *model.AppUser, id int64) (*model.KBQualityEvaluation, error) {
	if actor == nil || id <= 0 {
		return nil, ErrInvalidEvaluation
	}
	evaluation, err := s.repository.FindQualityEvaluation(ctx, id)
	if err != nil {
		return nil, err
	}
	if _, _, err := s.loadAccessibleVersion(ctx, actor, evaluation.DocumentVersionID); err != nil {
		return nil, err
	}
	return evaluation, nil
}

func (s *Service) RuleResults(ctx context.Context, actor *model.AppUser, id int64) ([]model.KBQualityRuleResult, error) {
	if _, err := s.Get(ctx, actor, id); err != nil {
		return nil, err
	}
	return s.repository.ListQualityRuleResults(ctx, id)
}

func (s *Service) loadAccessibleVersion(ctx context.Context, actor *model.AppUser, versionID int64) (*model.KBDocumentVersion, *model.KBDocument, error) {
	version, err := s.repository.FindDocumentVersionByID(ctx, versionID)
	if err != nil {
		return nil, nil, err
	}
	documentRow, err := s.repository.FindDocumentByID(ctx, version.DocumentID)
	if err != nil {
		return nil, nil, err
	}
	if actor.Role != model.RoleAdmin && (documentRow.CreatedBy == nil || *documentRow.CreatedBy != actor.ID) {
		return nil, nil, ErrForbidden
	}
	return version, documentRow, nil
}

func parseQualityScore(quality document.ParseQuality) float64 {
	switch quality.Level {
	case document.ParseQualityExcellent:
		return 100
	case document.ParseQualityGood:
		return 90
	case document.ParseQualityWarning:
		return 70
	default:
		return 0
	}
}
