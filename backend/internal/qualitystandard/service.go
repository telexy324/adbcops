package qualitystandard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

var (
	ErrInvalidStandard    = errors.New("invalid quality standard")
	ErrPublishedImmutable = errors.New("published quality standard is immutable; create a new version")
	ErrInvalidTransition  = errors.New("invalid quality standard status transition")
)

type Repository interface {
	ListStructuredQualityStandards(context.Context) ([]model.KBStructuredQualityStandard, error)
	FindStructuredQualityStandard(context.Context, int64) (*model.KBStructuredQualityStandard, error)
	CreateStructuredQualityStandard(context.Context, *model.KBStructuredQualityStandard) error
	ReplaceStructuredQualityStandard(context.Context, *model.KBStructuredQualityStandard) error
	UpdateStructuredQualityStandardStatus(context.Context, int64, string, *int64) (*model.KBStructuredQualityStandard, error)
	FindQualityProfile(context.Context, int64) (*model.KBQualityProfile, error)
	CreateQualityProfile(context.Context, *model.KBQualityProfile) error
	ReplaceQualityProfile(context.Context, *model.KBQualityProfile) error
}

type Service struct{ repository Repository }

func NewService(repository Repository) *Service { return &Service{repository: repository} }

type ValidationResult struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors"`
}

func (s *Service) List(ctx context.Context) ([]model.KBStructuredQualityStandard, error) {
	return s.repository.ListStructuredQualityStandards(ctx)
}

func (s *Service) Get(ctx context.Context, id int64) (*model.KBStructuredQualityStandard, error) {
	return s.repository.FindStructuredQualityStandard(ctx, id)
}

func (s *Service) Create(ctx context.Context, actorID int64, standard *model.KBStructuredQualityStandard) (*model.KBStructuredQualityStandard, error) {
	if standard == nil {
		return nil, ErrInvalidStandard
	}
	sanitizeStandard(standard)
	standard.CreatedBy = &actorID
	standard.Status = model.QualityStandardDraft
	for i := range standard.Profiles {
		standard.Profiles[i].Status = model.QualityStandardDraft
	}
	if result := Validate(standard); !result.Valid {
		return nil, fmt.Errorf("%w: %s", ErrInvalidStandard, strings.Join(result.Errors, "; "))
	}
	if err := s.repository.CreateStructuredQualityStandard(ctx, standard); err != nil {
		return nil, err
	}
	return s.Get(ctx, standard.ID)
}

func (s *Service) Update(ctx context.Context, id int64, standard *model.KBStructuredQualityStandard) (*model.KBStructuredQualityStandard, error) {
	existing, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing.Status == model.QualityStandardPublished {
		return nil, ErrPublishedImmutable
	}
	if standard == nil {
		return nil, ErrInvalidStandard
	}
	sanitizeStandard(standard)
	standard.ID = id
	standard.CreatedBy = existing.CreatedBy
	standard.Status = existing.Status
	if result := Validate(standard); !result.Valid {
		return nil, fmt.Errorf("%w: %s", ErrInvalidStandard, strings.Join(result.Errors, "; "))
	}
	if err := s.repository.ReplaceStructuredQualityStandard(ctx, standard); err != nil {
		if errors.Is(err, repository.ErrImmutable) {
			return nil, ErrPublishedImmutable
		}
		return nil, err
	}
	return s.Get(ctx, id)
}

func (s *Service) ValidateStored(ctx context.Context, id int64) (ValidationResult, error) {
	standard, err := s.Get(ctx, id)
	if err != nil {
		return ValidationResult{}, err
	}
	return Validate(standard), nil
}

func (s *Service) Publish(ctx context.Context, id, actorID int64) (*model.KBStructuredQualityStandard, error) {
	standard, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if standard.Status != model.QualityStandardDraft {
		return nil, ErrInvalidTransition
	}
	if result := Validate(standard); !result.Valid {
		return nil, fmt.Errorf("%w: %s", ErrInvalidStandard, strings.Join(result.Errors, "; "))
	}
	return s.repository.UpdateStructuredQualityStandardStatus(ctx, id, model.QualityStandardPublished, &actorID)
}

func (s *Service) Deprecate(ctx context.Context, id int64) (*model.KBStructuredQualityStandard, error) {
	standard, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if standard.Status != model.QualityStandardPublished {
		return nil, ErrInvalidTransition
	}
	return s.repository.UpdateStructuredQualityStandardStatus(ctx, id, model.QualityStandardDeprecated, standard.ApprovedBy)
}

func (s *Service) CreateProfile(ctx context.Context, profile *model.KBQualityProfile) (*model.KBQualityProfile, error) {
	standard, err := s.Get(ctx, profile.StandardID)
	if err != nil {
		return nil, err
	}
	if standard.Status == model.QualityStandardPublished {
		return nil, ErrPublishedImmutable
	}
	sanitizeProfile(profile)
	profile.Status = model.QualityStandardDraft
	test := *standard
	test.Profiles = append(test.Profiles, *profile)
	if result := Validate(&test); !result.Valid {
		return nil, fmt.Errorf("%w: %s", ErrInvalidStandard, strings.Join(result.Errors, "; "))
	}
	if err := s.repository.CreateQualityProfile(ctx, profile); err != nil {
		return nil, err
	}
	return s.repository.FindQualityProfile(ctx, profile.ID)
}

func (s *Service) GetProfile(ctx context.Context, id int64) (*model.KBQualityProfile, error) {
	return s.repository.FindQualityProfile(ctx, id)
}

func (s *Service) UpdateProfile(ctx context.Context, id int64, profile *model.KBQualityProfile) (*model.KBQualityProfile, error) {
	existing, err := s.GetProfile(ctx, id)
	if err != nil {
		return nil, err
	}
	standard, err := s.Get(ctx, existing.StandardID)
	if err != nil {
		return nil, err
	}
	if standard.Status == model.QualityStandardPublished {
		return nil, ErrPublishedImmutable
	}
	sanitizeProfile(profile)
	profile.ID, profile.StandardID, profile.Status = id, existing.StandardID, existing.Status
	replaced := false
	for index := range standard.Profiles {
		if standard.Profiles[index].ID == id {
			standard.Profiles[index] = *profile
			replaced = true
		}
	}
	if !replaced {
		return nil, repository.ErrNotFound
	}
	if result := Validate(standard); !result.Valid {
		return nil, fmt.Errorf("%w: %s", ErrInvalidStandard, strings.Join(result.Errors, "; "))
	}
	if err := s.repository.ReplaceQualityProfile(ctx, profile); err != nil {
		if errors.Is(err, repository.ErrImmutable) {
			return nil, ErrPublishedImmutable
		}
		return nil, err
	}
	return s.GetProfile(ctx, id)
}

func (s *Service) CloneProfile(ctx context.Context, id int64, key, name string) (*model.KBQualityProfile, error) {
	profile, err := s.GetProfile(ctx, id)
	if err != nil {
		return nil, err
	}
	clone := *profile
	clone.ID, clone.ProfileKey, clone.Name = 0, strings.TrimSpace(key), strings.TrimSpace(name)
	clone.Criteria = append([]model.KBQualityCriterion(nil), profile.Criteria...)
	for i := range clone.Criteria {
		clone.Criteria[i].ID = 0
		clone.Criteria[i].Rules = append([]model.KBQualityRule(nil), profile.Criteria[i].Rules...)
		for j := range clone.Criteria[i].Rules {
			clone.Criteria[i].Rules[j].ID = 0
			clone.Criteria[i].Rules[j].CriterionID = 0
		}
	}
	return s.CreateProfile(ctx, &clone)
}

func Validate(standard *model.KBStructuredQualityStandard) ValidationResult {
	errs := make([]string, 0)
	if standard == nil {
		return ValidationResult{Errors: []string{"standard is required"}}
	}
	if strings.TrimSpace(standard.Name) == "" {
		errs = append(errs, "name is required")
	}
	if strings.TrimSpace(standard.Version) == "" {
		errs = append(errs, "version is required")
	}
	if len(standard.Profiles) == 0 {
		errs = append(errs, "at least one profile is required")
	}
	profileKeys := map[string]struct{}{}
	for pi := range standard.Profiles {
		p := &standard.Profiles[pi]
		prefix := fmt.Sprintf("profile[%d]", pi)
		if p.ProfileKey == "" || p.Name == "" {
			errs = append(errs, prefix+" key and name are required")
		}
		if _, exists := profileKeys[p.ProfileKey]; exists {
			errs = append(errs, prefix+" profile key must be unique")
		}
		profileKeys[p.ProfileKey] = struct{}{}
		if !validJSONArray(p.ApplicableDocTypes) {
			errs = append(errs, prefix+" applicableDocTypes must be a JSON array")
		}
		if p.TotalScore <= 0 || p.WarningScore < 0 || p.WarningScore > p.PassScore || p.PassScore > p.TotalScore {
			errs = append(errs, prefix+" score thresholds are invalid")
		}
		weight, score := 0.0, 0.0
		criterionKeys, ruleKeys := map[string]struct{}{}, map[string]struct{}{}
		for ci := range p.Criteria {
			c := &p.Criteria[ci]
			cp := fmt.Sprintf("%s criterion[%d]", prefix, ci)
			if c.CriterionKey == "" || c.Name == "" || c.OrderNo <= 0 {
				errs = append(errs, cp+" key, name and positive order are required")
			}
			if _, exists := criterionKeys[c.CriterionKey]; exists {
				errs = append(errs, cp+" criterion key must be unique")
			}
			criterionKeys[c.CriterionKey] = struct{}{}
			if !oneOf(c.ScoringMethod, "rule", "llm", "hybrid", "manual") {
				errs = append(errs, cp+" scoringMethod is invalid")
			}
			weight += c.Weight
			score += c.MaxScore
			for ri := range c.Rules {
				r := &c.Rules[ri]
				rp := fmt.Sprintf("%s rule[%d]", cp, ri)
				if r.RuleKey == "" || r.Name == "" || r.OrderNo <= 0 {
					errs = append(errs, rp+" key, name and positive order are required")
				}
				if _, exists := ruleKeys[r.RuleKey]; exists {
					errs = append(errs, rp+" rule key must be unique within profile")
				}
				ruleKeys[r.RuleKey] = struct{}{}
				if !oneOf(r.RuleType, "field_presence", "section_presence", "pattern", "metadata", "consistency", "freshness", "semantic", "safety", "cross_reference", "manual") {
					errs = append(errs, rp+" ruleType is invalid")
				}
				if r.MaxScore < 0 {
					errs = append(errs, rp+" maxScore cannot be negative")
				}
				if r.HardGate && (r.Description == nil || strings.TrimSpace(*r.Description) == "") {
					errs = append(errs, rp+" hard gate requires an explanation")
				}
				if r.HardGate && !validJSONObject(r.EvidenceRequirement) {
					errs = append(errs, rp+" hard gate requires evidenceRequirement")
				}
			}
		}
		if math.Abs(weight-1) > 0.0001 {
			errs = append(errs, fmt.Sprintf("%s criterion weights must total 1.0000", prefix))
		}
		if math.Abs(score-p.TotalScore) > 0.005 {
			errs = append(errs, fmt.Sprintf("%s criterion max scores must total %.2f", prefix, p.TotalScore))
		}
	}
	return ValidationResult{Valid: len(errs) == 0, Errors: errs}
}

func sanitizeStandard(s *model.KBStructuredQualityStandard) {
	s.ID = 0
	s.Name = strings.TrimSpace(s.Name)
	s.Version = strings.TrimSpace(s.Version)
	s.ApprovedBy = nil
	for i := range s.Profiles {
		sanitizeProfile(&s.Profiles[i])
	}
}

func sanitizeProfile(p *model.KBQualityProfile) {
	p.ID = 0
	p.ProfileKey = strings.TrimSpace(p.ProfileKey)
	p.Name = strings.TrimSpace(p.Name)
	for i := range p.Criteria {
		p.Criteria[i].ID = 0
		p.Criteria[i].ProfileID = 0
		p.Criteria[i].CriterionKey = strings.TrimSpace(p.Criteria[i].CriterionKey)
		for j := range p.Criteria[i].Rules {
			p.Criteria[i].Rules[j].ID = 0
			p.Criteria[i].Rules[j].CriterionID = 0
			p.Criteria[i].Rules[j].RuleKey = strings.TrimSpace(p.Criteria[i].Rules[j].RuleKey)
			if p.Criteria[i].Rules[j].Severity == "" {
				p.Criteria[i].Rules[j].Severity = "medium"
			}
		}
	}
}

func validJSONArray(value json.RawMessage) bool {
	var v []any
	return len(value) > 0 && json.Unmarshal(value, &v) == nil && v != nil
}
func validJSONObject(value json.RawMessage) bool {
	var v map[string]any
	return len(value) > 0 && json.Unmarshal(value, &v) == nil && len(v) > 0
}
func oneOf(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}
