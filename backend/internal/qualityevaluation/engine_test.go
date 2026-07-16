package qualityevaluation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"aiops-platform/backend/internal/document"
	"aiops-platform/backend/internal/model"
)

func TestPlaintextCredentialTriggersBlockedWithRedactedBlockEvidence(t *testing.T) {
	profile := profileWithRules(model.KBQualityRule{RuleKey: "sensitive_credential_exposed", Name: "Secrets", RuleType: "safety", MaxScore: 10, HardGate: true})
	output := EvaluateDeterministic(EngineInput{Profile: profile, Version: version(), Blocks: []model.KBDocumentBlock{block("b-secret", 1, "redis-cli -a password=SuperSecret123")}, Now: time.Now()})
	if output.GateStatus != "blocked" || len(output.HardGateViolations) != 1 {
		t.Fatalf("credential did not block: %+v", output)
	}
	var evidence []Evidence
	if err := json.Unmarshal(output.Results[0].Evidence, &evidence); err != nil {
		t.Fatal(err)
	}
	if len(evidence) != 1 || evidence[0].BlockID != "b-secret" {
		t.Fatalf("missing block evidence: %+v", evidence)
	}
	if evidence[0].Quote != "[REDACTED sensitive credential]" || strings.Contains(string(output.Results[0].Evidence), "SuperSecret") {
		t.Fatalf("secret leaked in evidence: %s", output.Results[0].Evidence)
	}
	genericEvidence := blockEvidence(block("b-generic", 2, "password=AnotherSecret"), "Referenced by another rule.")
	if genericEvidence.Quote != "[REDACTED sensitive credential]" {
		t.Fatalf("generic evidence leaked credential: %+v", genericEvidence)
	}
}

func TestDestructiveCommandRequiresAdjacentWarning(t *testing.T) {
	profile := profileWithRules(model.KBQualityRule{RuleKey: "destructive_command_without_warning", Name: "Danger", RuleType: "safety", MaxScore: 10, HardGate: true})
	unsafe := EvaluateDeterministic(EngineInput{Profile: profile, Version: version(), Blocks: []model.KBDocumentBlock{block("b-command", 1, "rm -rf /var/lib/app/cache")}})
	if unsafe.GateStatus != "blocked" || status(unsafe.Results[0]) != FindingUnsafe {
		t.Fatalf("unsafe command passed: %+v", unsafe)
	}
	safe := EvaluateDeterministic(EngineInput{Profile: profile, Version: version(), Blocks: []model.KBDocumentBlock{
		block("b-warning", 1, "警告：该操作会删除缓存，请先确认回滚方案。"), block("b-command", 2, "rm -rf /var/lib/app/cache"),
	}})
	if safe.GateStatus != "pass" || len(safe.HardGateViolations) != 0 {
		t.Fatalf("adjacent warning was ignored: %+v", safe)
	}
}

func TestFieldSectionPatternMetadataAndFreshnessRules(t *testing.T) {
	rules := []model.KBQualityRule{
		{RuleKey: "owner", Name: "Owner", RuleType: "field_presence", Required: true, MaxScore: 10, DetectorConfig: raw(`{"field":"owner"}`)},
		{RuleKey: "verification", Name: "Verification", RuleType: "section_presence", Required: true, MaxScore: 10, DetectorConfig: raw(`{"section":"verification"}`)},
		{RuleKey: "ticket", Name: "Ticket", RuleType: "pattern", Required: true, MaxScore: 10, DetectorConfig: raw(`{"pattern":"CHG-[0-9]+"}`)},
		{RuleKey: "metadata_traceable", Name: "Version", RuleType: "metadata", Required: true, MaxScore: 10},
		{RuleKey: "expired_document", Name: "Fresh", RuleType: "freshness", Required: true, MaxScore: 10, HardGate: true},
	}
	profile := profileWithRules(rules...)
	profile.TotalScore, profile.PassScore, profile.WarningScore = 50, 40, 35
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	currentVersion := version()
	currentVersion.ReviewDueAt = &due
	input := EngineInput{Profile: profile, Version: currentVersion, Now: time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC), Schema: document.DocumentSchemaExtraction{Fields: map[string]document.ExtractedField{
		"owner":        {Name: "owner", Values: []string{"ops"}, Evidence: []document.SchemaEvidence{{BlockKey: "b-owner", Text: "Owner: ops"}}},
		"verification": {Name: "verification", Values: []string{"check status"}, Evidence: []document.SchemaEvidence{{BlockKey: "b-verify", Text: "Verify status"}}},
	}}, Blocks: []model.KBDocumentBlock{block("b-owner", 1, "Owner: ops"), block("b-verify", 2, "Verification: systemctl status app"), block("b-ticket", 3, "Approval ticket CHG-1024")}}
	output := EvaluateDeterministic(input)
	if output.GateStatus != "blocked" || status(output.Results[4]) != FindingOutdated {
		t.Fatalf("expired document did not block: %+v", output)
	}
	for _, result := range output.Results {
		var evidence []Evidence
		if err := json.Unmarshal(result.Evidence, &evidence); err != nil || len(evidence) == 0 || evidence[0].BlockID == "" {
			t.Fatalf("rule %s lacks evidence: %s (%v)", result.RuleKey, result.Evidence, err)
		}
	}
	for index := 0; index < 4; index++ {
		if status(output.Results[index]) != FindingPresent {
			t.Fatalf("rule %s failed: %+v", output.Results[index].RuleKey, output.Results[index])
		}
	}
}

func TestSemanticRuleIsNotFabricated(t *testing.T) {
	profile := profileWithRules(model.KBQualityRule{RuleKey: "semantic_quality", Name: "Semantic", RuleType: "semantic", MaxScore: 10})
	output := EvaluateDeterministic(EngineInput{Profile: profile, Version: version(), Blocks: []model.KBDocumentBlock{block("b-1", 1, "Some content")}})
	if output.PendingRuleCount != 1 || output.Results[0].Score != nil || status(output.Results[0]) != FindingManualConfirmationRequired {
		t.Fatalf("semantic conclusion was fabricated: %+v", output)
	}
}

func TestCredentialPlaceholderIsAllowed(t *testing.T) {
	profile := profileWithRules(model.KBQualityRule{RuleKey: "sensitive_credential_exposed", Name: "Secrets", RuleType: "safety", MaxScore: 10, HardGate: true})
	output := EvaluateDeterministic(EngineInput{Profile: profile, Version: version(), Blocks: []model.KBDocumentBlock{block("b-placeholder", 1, "password=${REDIS_PASSWORD}")}})
	if output.GateStatus != "pass" {
		t.Fatalf("credential placeholder was blocked: %+v", output)
	}
}

func profileWithRules(rules ...model.KBQualityRule) *model.KBQualityProfile {
	for index := range rules {
		rules[index].OrderNo = index + 1
		if rules[index].Severity == "" {
			rules[index].Severity = "high"
		}
	}
	return &model.KBQualityProfile{ID: 1, TotalScore: 10, PassScore: 8, WarningScore: 7, Criteria: []model.KBQualityCriterion{{CriterionKey: "test", Name: "Test", MaxScore: 10, Weight: 1, Rules: rules}}}
}

func version() *model.KBDocumentVersion {
	return &model.KBDocumentVersion{ID: 1, Version: "v1.0", FileHash: strings.Repeat("a", 64), UpdatedAt: time.Now()}
}
func block(key string, order int, text string) model.KBDocumentBlock {
	return model.KBDocumentBlock{BlockKey: key, OrderNo: order, TextContent: text, SectionPath: raw(`[]`)}
}
func raw(value string) json.RawMessage { return json.RawMessage(value) }
func status(result model.KBQualityRuleResult) string {
	if result.FindingStatus == nil {
		return ""
	}
	return *result.FindingStatus
}
