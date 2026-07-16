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
