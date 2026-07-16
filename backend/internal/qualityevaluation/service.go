package qualityevaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"aiops-platform/backend/internal/document"
	llmsvc "aiops-platform/backend/internal/llm"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

var (
	ErrInvalidEvaluation   = errors.New("invalid quality evaluation request")
	ErrProfileNotPublished = errors.New("quality profile is not published")
	ErrDocumentNotParsed   = errors.New("document version has not passed parsing")
	ErrUnsupportedMode     = errors.New("quality evaluation mode must be deterministic, hybrid, or llm")
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
	FindDefaultEnabledLLMConfigByPurpose(context.Context, string) (*model.LLMConfig, error)
}

type SecretManager interface{ Decrypt(string) (string, error) }

type Service struct {
	repository Repository
	now        func() time.Time
	secrets    SecretManager
	llmClient  llmsvc.Client
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository, now: time.Now}
}

func (s *Service) WithLLM(secrets SecretManager, client llmsvc.Client) *Service {
	s.secrets, s.llmClient = secrets, client
	return s
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
	if mode != "deterministic" && mode != "hybrid" && mode != "llm" {
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
	source := "deterministic"
	if mode != "deterministic" {
		source = "hybrid"
	}
	if !input.Force {
		cached, cacheErr := s.repository.FindLatestQualityEvaluation(ctx, version.ID, profile.ID, source)
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
	degraded, validationWarnings := []string{}, []string{}
	criterionScores := criterionScoresFromResults(output.Results)
	var modelConfigID *int64
	llmCalls, llmFailedCalls := 0, 0
	if mode != "deterministic" {
		pending := pendingRuleKeys(output.Results)
		config, configErr := s.repository.FindDefaultEnabledLLMConfigByPurpose(ctx, model.LLMPurposeChat)
		if configErr != nil || s.llmClient == nil || s.secrets == nil {
			degraded = append(degraded, "llm")
			if configErr != nil && !errors.Is(configErr, repository.ErrNotFound) {
				validationWarnings = append(validationWarnings, "load llm config: "+configErr.Error())
			}
		} else {
			llmConfig, credentialErr := s.llmRunConfig(config)
			if credentialErr != nil {
				degraded = append(degraded, "llm")
				validationWarnings = append(validationWarnings, "decrypt llm credential: "+credentialErr.Error())
			} else {
				modelConfigID = &config.ID
				llmOutput := EvaluateWithLLM(ctx, s.llmClient, llmConfig, profile, blocks, pending)
				llmCalls, llmFailedCalls = llmOutput.Calls, llmOutput.FailedCalls
				validationWarnings = append(validationWarnings, llmOutput.ValidationWarnings...)
				if llmOutput.FailedCalls > 0 || len(llmOutput.Results) == 0 && len(pending) > 0 {
					degraded = append(degraded, "llm")
				}
				output, criterionScores = mergeLLMResults(profile, output, llmOutput.Results)
			}
		}
	}
	parseScore, totalScore, level, now := parseQualityScore(parseQuality), output.ContentScore, output.Level, s.now()
	summary := fmt.Sprintf("%s evaluation assessed %d rules; %d rules await semantic or human evaluation; %d hard-gate violations.", source, output.AssessedRuleCount, output.PendingRuleCount, len(output.HardGateViolations))
	result := marshal(map[string]any{"hardGateViolations": output.HardGateViolations, "assessedRuleCount": output.AssessedRuleCount, "pendingRuleCount": output.PendingRuleCount, "criterionScores": criterionScores, "degradedComponents": degraded, "validationWarnings": validationWarnings, "llmCalls": llmCalls, "llmFailedCalls": llmFailedCalls, "documentId": documentRow.ID})
	evaluation := &model.KBQualityEvaluation{DocumentVersionID: version.ID, QualityProfileID: profile.ID, QualityProfileVersion: standard.Version, ParseScore: &parseScore, ContentScore: &output.ContentScore, TotalScore: &totalScore, GateStatus: output.GateStatus, Level: &level, Source: source, ModelConfigID: modelConfigID, Summary: &summary, Result: result, Status: "completed", CompletedAt: &now, RuleResults: output.Results}
	if err := s.repository.CreateQualityEvaluation(ctx, evaluation); err != nil {
		return nil, err
	}
	return evaluation, nil
}

func (s *Service) llmRunConfig(config *model.LLMConfig) (LLMRunConfig, error) {
	result := LLMRunConfig{BaseURL: config.BaseURL, Provider: config.Provider, Model: config.Model, Temperature: config.Temperature}
	var err error
	if config.APIKeyRef != nil && *config.APIKeyRef != "" {
		result.APIKey, err = s.secrets.Decrypt(*config.APIKeyRef)
		if err != nil {
			return LLMRunConfig{}, err
		}
	}
	if config.AppKeyRef != nil && *config.AppKeyRef != "" {
		result.AppKey, err = s.secrets.Decrypt(*config.AppKeyRef)
		if err != nil {
			return LLMRunConfig{}, err
		}
	}
	if config.APISecretRef != nil && *config.APISecretRef != "" {
		result.APISecret, err = s.secrets.Decrypt(*config.APISecretRef)
		if err != nil {
			return LLMRunConfig{}, err
		}
	}
	return result, nil
}

func pendingRuleKeys(results []model.KBQualityRuleResult) map[string]struct{} {
	values := map[string]struct{}{}
	for _, result := range results {
		if result.Score == nil {
			values[result.CriterionKey+"\x00"+result.RuleKey] = struct{}{}
		}
	}
	return values
}

func mergeLLMResults(profile *model.KBQualityProfile, deterministic EngineOutput, llmResults []model.KBQualityRuleResult) (EngineOutput, map[string]CriterionScore) {
	byKey := map[string]model.KBQualityRuleResult{}
	for _, result := range deterministic.Results {
		byKey[result.CriterionKey+"\x00"+result.RuleKey] = result
	}
	for _, result := range llmResults {
		key := result.CriterionKey + "\x00" + result.RuleKey
		if existing, ok := byKey[key]; ok && existing.Score == nil {
			byKey[key] = result
		}
	}
	merged := EngineOutput{Results: []model.KBQualityRuleResult{}, HardGateViolations: append([]string(nil), deterministic.HardGateViolations...)}
	hardGates := map[string]bool{}
	for _, criterion := range profile.Criteria {
		for _, rule := range criterion.Rules {
			hardGates[criterion.CriterionKey+"\x00"+rule.RuleKey] = rule.HardGate
		}
	}
	earned, maxScore := 0.0, 0.0
	for _, criterion := range profile.Criteria {
		for _, rule := range criterion.Rules {
			key := criterion.CriterionKey + "\x00" + rule.RuleKey
			result, ok := byKey[key]
			if !ok {
				continue
			}
			merged.Results = append(merged.Results, result)
			if result.Score == nil {
				merged.PendingRuleCount++
				continue
			}
			merged.AssessedRuleCount++
			earned += *result.Score
			if result.MaxScore != nil {
				maxScore += *result.MaxScore
			}
			if hardGates[key] && result.FindingStatus != nil && *result.FindingStatus != FindingPresent && !containsStringValue(merged.HardGateViolations, result.RuleKey) {
				merged.HardGateViolations = append(merged.HardGateViolations, result.RuleKey)
			}
		}
	}
	if maxScore > 0 {
		merged.ContentScore = roundScore(earned / maxScore * profile.TotalScore)
	}
	switch {
	case len(merged.HardGateViolations) > 0 || merged.ContentScore < profile.WarningScore:
		merged.GateStatus, merged.Level = "blocked", "blocked"
	case merged.ContentScore < profile.PassScore:
		merged.GateStatus, merged.Level = "warning", "warning"
	default:
		merged.GateStatus, merged.Level = "pass", "pass"
	}
	return merged, criterionScoresFromResults(merged.Results)
}

func criterionScoresFromResults(results []model.KBQualityRuleResult) map[string]CriterionScore {
	values := map[string]CriterionScore{}
	for _, result := range results {
		if result.Score == nil || result.MaxScore == nil {
			continue
		}
		value := values[result.CriterionKey]
		value.Score += *result.Score
		value.MaxScore += *result.MaxScore
		values[result.CriterionKey] = value
	}
	return values
}

func containsStringValue(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
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
