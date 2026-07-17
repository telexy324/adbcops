package qualityevaluation

import (
	"context"
	"errors"
	"testing"

	"aiops-platform/backend/internal/model"
)

func TestAggregateResultsHardGateOverridesHighScore(t *testing.T) {
	maxScore, fullScore, unsafe := 100.0, 100.0, FindingUnsafe
	profile := &model.KBQualityProfile{
		TotalScore:   100,
		PassScore:    80,
		WarningScore: 60,
		Criteria: []model.KBQualityCriterion{{
			CriterionKey: "safety",
			Rules:        []model.KBQualityRule{{RuleKey: "dangerous_command", HardGate: true}},
		}},
	}
	output := aggregateResults(profile, []model.KBQualityRuleResult{{
		CriterionKey:  "safety",
		RuleKey:       "dangerous_command",
		Score:         &fullScore,
		MaxScore:      &maxScore,
		FindingStatus: &unsafe,
	}})

	if output.ContentScore != 100 {
		t.Fatalf("expected score 100, got %.2f", output.ContentScore)
	}
	if output.GateStatus != "blocked" {
		t.Fatalf("hard gate must block a high score, got %q", output.GateStatus)
	}
	if len(output.HardGateViolations) != 1 || output.HardGateViolations[0] != "dangerous_command" {
		t.Fatalf("unexpected hard gate violations: %#v", output.HardGateViolations)
	}
}

func TestValidFindingStatusRejectsUnknownValue(t *testing.T) {
	if validFindingStatus("approved_by_guess") {
		t.Fatal("unknown manual finding status must be rejected")
	}
}

func TestOverrideRequiresReason(t *testing.T) {
	service := NewService(nil)
	score := 10.0
	_, err := service.Override(context.Background(), &model.AppUser{ID: 1, Role: model.RoleAdmin}, 1, OverrideInput{RuleResultID: 1, Score: &score, Status: FindingPresent, Comment: "   "})
	if !errors.Is(err, ErrOverrideReason) {
		t.Fatalf("expected override reason error, got %v", err)
	}
}

func TestPublishedEvaluationIsImmutable(t *testing.T) {
	err := ensureEvaluationMutable(&model.KBQualityEvaluation{ReviewStatus: "published"})
	if !errors.Is(err, ErrPublishedImmutable) {
		t.Fatalf("expected immutable error, got %v", err)
	}
}

func TestAggregateResultsPendingHardGateIsBlocked(t *testing.T) {
	status := FindingManualConfirmationRequired
	profile := &model.KBQualityProfile{TotalScore: 100, PassScore: 80, WarningScore: 60, Criteria: []model.KBQualityCriterion{{CriterionKey: "safety", Rules: []model.KBQualityRule{{RuleKey: "semantic_gate", HardGate: true}}}}}
	output := aggregateResults(profile, []model.KBQualityRuleResult{{CriterionKey: "safety", RuleKey: "semantic_gate", FindingStatus: &status}})
	if output.GateStatus != "blocked" || len(output.HardGateViolations) != 1 {
		t.Fatalf("pending hard gate was bypassed: %+v", output)
	}
}

func TestEvaluationFingerprintIncludesModeAndCriteria(t *testing.T) {
	first := evaluationFingerprint("hybrid", []string{"accuracy"})
	if first == evaluationFingerprint("llm", []string{"accuracy"}) || first == evaluationFingerprint("hybrid", []string{"safety"}) {
		t.Fatal("evaluation cache fingerprint ignored mode or criteria")
	}
	selected := map[string]struct{}{"safety": {}, "accuracy": {}}
	ordered := sortedSelectedCriteria(selected)
	if len(ordered) != 2 || ordered[0] != "accuracy" || ordered[1] != "safety" {
		t.Fatalf("selected criteria are not stable: %#v", ordered)
	}
}

func TestNotApplicableRuleIsNotPendingForLLM(t *testing.T) {
	status := FindingNotApplicable
	results := []model.KBQualityRuleResult{{CriterionKey: "optional", RuleKey: "optional_rule", FindingStatus: &status}}
	if keys := pendingRuleKeys(results); len(keys) != 0 {
		t.Fatalf("not-applicable rule was queued for LLM: %#v", keys)
	}
	profile := &model.KBQualityProfile{TotalScore: 10, PassScore: 8, WarningScore: 7}
	output := aggregateResults(profile, results)
	if output.PendingRuleCount != 0 {
		t.Fatalf("not-applicable rule counted as pending: %+v", output)
	}
}
