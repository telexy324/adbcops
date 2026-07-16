package qualitystandard

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"aiops-platform/backend/internal/model"
)

func TestValidateAcceptsBalancedStandard(t *testing.T) {
	result := Validate(validStandard())
	if !result.Valid {
		t.Fatalf("expected valid standard, got %v", result.Errors)
	}
}

func TestValidateRejectsScoreAndWeightTotals(t *testing.T) {
	standard := validStandard()
	standard.Profiles[0].Criteria[0].Weight = 0.8
	standard.Profiles[0].Criteria[0].MaxScore = 90
	result := Validate(standard)
	if result.Valid || !containsError(result.Errors, "weights must total") || !containsError(result.Errors, "max scores must total") {
		t.Fatalf("expected total validation errors, got %v", result.Errors)
	}
}

func TestValidateRejectsDuplicateRuleKeyAcrossCriteria(t *testing.T) {
	standard := validStandard()
	second := standard.Profiles[0].Criteria[0]
	second.CriterionKey, second.Name, second.OrderNo = "safety", "Safety", 2
	second.Rules = append([]model.KBQualityRule(nil), second.Rules...)
	standard.Profiles[0].Criteria[0].Weight, standard.Profiles[0].Criteria[0].MaxScore = 0.5, 50
	second.Weight, second.MaxScore = 0.5, 50
	standard.Profiles[0].Criteria = append(standard.Profiles[0].Criteria, second)
	result := Validate(standard)
	if result.Valid || !containsError(result.Errors, "rule key must be unique") {
		t.Fatalf("expected duplicate rule error, got %v", result.Errors)
	}
}

func TestValidateHardGateRequiresExplanationAndEvidence(t *testing.T) {
	standard := validStandard()
	standard.Profiles[0].Criteria[0].Rules[0].HardGate = true
	standard.Profiles[0].Criteria[0].Rules[0].Description = nil
	standard.Profiles[0].Criteria[0].Rules[0].EvidenceRequirement = nil
	result := Validate(standard)
	if result.Valid || !containsError(result.Errors, "requires an explanation") || !containsError(result.Errors, "requires evidenceRequirement") {
		t.Fatalf("expected hard gate errors, got %v", result.Errors)
	}
}

func TestUpdatePublishedStandardIsRejected(t *testing.T) {
	repository := &stubRepository{standard: validStandard()}
	repository.standard.ID = 9
	repository.standard.Status = model.QualityStandardPublished
	_, err := NewService(repository).Update(context.Background(), 9, validStandard())
	if !errors.Is(err, ErrPublishedImmutable) {
		t.Fatalf("expected immutable error, got %v", err)
	}
	if repository.replaced {
		t.Fatal("published standard reached repository replacement")
	}
}

func validStandard() *model.KBStructuredQualityStandard {
	description := "missing evidence blocks publication"
	return &model.KBStructuredQualityStandard{Name: "ops", Version: "v1.0", Status: model.QualityStandardDraft, Profiles: []model.KBQualityProfile{{
		ProfileKey: "default", Name: "Default", ApplicableDocTypes: json.RawMessage(`["runbook"]`), TotalScore: 100, PassScore: 80, WarningScore: 70, Status: model.QualityStandardDraft,
		Criteria: []model.KBQualityCriterion{{CriterionKey: "complete", Name: "Complete", Weight: 1, MaxScore: 100, ScoringMethod: "rule", OrderNo: 1,
			Rules: []model.KBQualityRule{{RuleKey: "required_sections", Name: "Required sections", Description: &description, RuleType: "section_presence", Severity: "high", MaxScore: 100, Required: true, EvidenceRequirement: json.RawMessage(`{"required":"block"}`), OrderNo: 1}}}},
	}}}
}

func containsError(errors []string, part string) bool {
	for _, value := range errors {
		if strings.Contains(value, part) {
			return true
		}
	}
	return false
}

type stubRepository struct {
	standard *model.KBStructuredQualityStandard
	replaced bool
}

func (s *stubRepository) ListStructuredQualityStandards(context.Context) ([]model.KBStructuredQualityStandard, error) {
	return nil, nil
}
func (s *stubRepository) FindStructuredQualityStandard(context.Context, int64) (*model.KBStructuredQualityStandard, error) {
	return s.standard, nil
}
func (s *stubRepository) CreateStructuredQualityStandard(context.Context, *model.KBStructuredQualityStandard) error {
	return nil
}
func (s *stubRepository) ReplaceStructuredQualityStandard(context.Context, *model.KBStructuredQualityStandard) error {
	s.replaced = true
	return nil
}
func (s *stubRepository) UpdateStructuredQualityStandardStatus(context.Context, int64, string, *int64) (*model.KBStructuredQualityStandard, error) {
	return s.standard, nil
}
func (s *stubRepository) FindQualityProfile(context.Context, int64) (*model.KBQualityProfile, error) {
	return nil, nil
}
func (s *stubRepository) CreateQualityProfile(context.Context, *model.KBQualityProfile) error {
	return nil
}
func (s *stubRepository) ReplaceQualityProfile(context.Context, *model.KBQualityProfile) error {
	return nil
}
