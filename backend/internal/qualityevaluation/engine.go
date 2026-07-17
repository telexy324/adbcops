package qualityevaluation

import (
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"aiops-platform/backend/internal/document"
	"aiops-platform/backend/internal/model"
)

const (
	FindingPresent                    = "present"
	FindingMissing                    = "missing"
	FindingPartial                    = "partial"
	FindingConflicting                = "conflicting"
	FindingOutdated                   = "outdated"
	FindingUnsafe                     = "unsafe"
	FindingNotApplicable              = "not_applicable"
	FindingManualConfirmationRequired = "manual_confirmation_required"
)

type Evidence struct {
	BlockID     string   `json:"blockId"`
	SectionPath []string `json:"sectionPath,omitempty"`
	Page        *int     `json:"page,omitempty"`
	Quote       string   `json:"quote"`
	Reason      string   `json:"reason"`
}

type EngineInput struct {
	Profile          *model.KBQualityProfile
	Version          *model.KBDocumentVersion
	ParseQuality     document.ParseQuality
	Schema           document.DocumentSchemaExtraction
	Blocks           []model.KBDocumentBlock
	SelectedCriteria map[string]struct{}
	Now              time.Time
}

type EngineOutput struct {
	ContentScore       float64                     `json:"contentScore"`
	GateStatus         string                      `json:"gateStatus"`
	Level              string                      `json:"level"`
	HardGateViolations []string                    `json:"hardGateViolations"`
	AssessedRuleCount  int                         `json:"assessedRuleCount"`
	PendingRuleCount   int                         `json:"pendingRuleCount"`
	Results            []model.KBQualityRuleResult `json:"-"`
}

type ruleOutcome struct {
	assessed   bool
	passed     bool
	status     string
	confidence float64
	evidence   []Evidence
	reason     string
	suggestion string
}

var (
	credentialPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:password|passwd|pwd|token|api[_-]?key|secret|access[_-]?key)\s*[:=]\s*([^\s,;]+)`),
		regexp.MustCompile(`(?i)(?:authorization\s*:\s*)?bearer\s+([a-z0-9._~+/=-]{8,})`),
		regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s/:@]+:([^\s/@]+)@`),
		regexp.MustCompile(`(?i)(?:^|\s)(?:-a|--password(?:=|\s+))\s*([^\s,;]+)`),
		regexp.MustCompile(`(?i)(?:^|\s)-u\s+[^\s:]+:([^\s,;]+)`),
		regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----`),
	}
	dangerousPattern = regexp.MustCompile(`(?i)\b(rm\s+-rf|kubectl\s+delete|drop\s+(?:table|database)|truncate\s+table|redis-cli\s+flushall|shutdown|reboot|mkfs(?:\.[a-z0-9]+)?|dd\s+if=)\b`)
	warningPattern   = regexp.MustCompile(`(?i)(warning|caution|danger|risk|approval|change ticket|警告|注意|危险|风险|审批|工单)`)
	approvalPattern  = regexp.MustCompile(`(?i)(approval|approved|change ticket|four-eyes|审批|批准|工单|双人复核)`)
	prodPattern      = regexp.MustCompile(`(?i)(\bprod(?:uction)?\b|生产环境)`)
	testPattern      = regexp.MustCompile(`(?i)(\btest(?:ing)?\b|测试环境)`)
)

func EvaluateDeterministic(input EngineInput) EngineOutput {
	if input.Now.IsZero() {
		input.Now = time.Now()
	}
	output := EngineOutput{HardGateViolations: []string{}, Results: []model.KBQualityRuleResult{}}
	if input.Profile == nil {
		output.GateStatus, output.Level = "blocked", "blocked"
		return output
	}
	blocks := sortedBlocks(input.Blocks)
	earned, assessedMax := 0.0, 0.0
	for _, criterion := range input.Profile.Criteria {
		if len(input.SelectedCriteria) > 0 {
			if _, ok := input.SelectedCriteria[criterion.CriterionKey]; !ok {
				continue
			}
		}
		for _, rule := range criterion.Rules {
			outcome := evaluateRule(rule, input, blocks)
			result := toRuleResult(criterion.CriterionKey, rule, outcome)
			output.Results = append(output.Results, result)
			if outcome.assessed {
				output.AssessedRuleCount++
				assessedMax += rule.MaxScore
				if result.Score != nil {
					earned += *result.Score
				}
				if rule.HardGate && !outcome.passed {
					output.HardGateViolations = append(output.HardGateViolations, rule.RuleKey)
				}
			} else {
				if outcome.status != FindingNotApplicable {
					output.PendingRuleCount++
					if rule.HardGate {
						output.HardGateViolations = append(output.HardGateViolations, rule.RuleKey)
					}
				}
			}
		}
	}
	if assessedMax > 0 {
		output.ContentScore = roundScore(earned / assessedMax * input.Profile.TotalScore)
	}
	switch {
	case len(output.HardGateViolations) > 0 || output.ContentScore < input.Profile.WarningScore:
		output.GateStatus, output.Level = "blocked", "blocked"
	case output.PendingRuleCount > 0 || output.ContentScore < input.Profile.PassScore:
		output.GateStatus, output.Level = "warning", "warning"
	default:
		output.GateStatus, output.Level = "pass", "pass"
	}
	return output
}

func evaluateRule(rule model.KBQualityRule, input EngineInput, blocks []model.KBDocumentBlock) ruleOutcome {
	switch rule.RuleType {
	case "field_presence":
		return evaluateField(rule, input, blocks)
	case "section_presence":
		return evaluateSection(rule, input, blocks)
	case "pattern":
		return evaluatePattern(rule, blocks)
	case "metadata":
		return evaluateMetadata(rule, input, blocks)
	case "freshness":
		return evaluateFreshness(rule, input, blocks)
	case "safety":
		return evaluateSafety(rule, blocks)
	case "manual":
		if rule.RuleKey == "parse_failed" {
			evidence := []Evidence{fallbackEvidence(blocks, "Document parse quality was checked.")}
			if input.ParseQuality.ParseSuccess {
				return pass(evidence)
			}
			return fail(FindingUnsafe, 1, evidence, "Document parsing failed.", "Fix parsing errors before quality review.")
		}
	case "consistency":
		if rule.RuleKey == "production_test_environment_confusion" {
			return evaluateEnvironmentConfusion(blocks)
		}
		if rule.RuleKey == "contradictory_critical_steps" {
			return evaluateCriticalContradiction(blocks)
		}
	}
	return ruleOutcome{status: FindingManualConfirmationRequired, confidence: 1, evidence: []Evidence{fallbackEvidence(blocks, "This rule requires semantic or human evaluation.")}}
}

func evaluateField(rule model.KBQualityRule, input EngineInput, blocks []model.KBDocumentBlock) ruleOutcome {
	config := detectorConfig(rule)
	fields := configStrings(config, "fields")
	if len(fields) == 0 {
		if value := configString(config, "field"); value != "" {
			fields = []string{value}
		}
	}
	if len(fields) == 0 {
		fields = fallbackFields(rule.RuleKey)
	}
	if len(fields) == 0 {
		return manual(blocks, "Field detector is not configured.")
	}
	missing := []string{}
	evidence := []Evidence{}
	for _, name := range fields {
		field, ok := input.Schema.Fields[name]
		if !ok || len(field.Values) == 0 {
			missing = append(missing, name)
			continue
		}
		for _, source := range field.Evidence {
			evidence = append(evidence, schemaEvidence(source, blocks, "Required field is present."))
		}
	}
	if len(evidence) == 0 {
		evidence = []Evidence{fallbackEvidence(blocks, "Document was checked for required fields.")}
	}
	if len(missing) == 0 {
		return pass(evidence)
	}
	if !rule.Required {
		return notApplicable(evidence, "Optional field is absent.")
	}
	status := FindingMissing
	if len(missing) < len(fields) {
		status = FindingPartial
	}
	return fail(status, .99, evidence, "Missing fields: "+strings.Join(missing, ", "), "Add the missing structured fields.")
}

func evaluateSection(rule model.KBQualityRule, input EngineInput, blocks []model.KBDocumentBlock) ruleOutcome {
	config := detectorConfig(rule)
	sections := configStrings(config, "sections")
	if len(sections) == 0 {
		if value := configString(config, "section"); value != "" {
			sections = []string{value}
		}
	}
	if len(sections) == 0 {
		sections = fallbackSections(rule.RuleKey, input.Schema.DocumentType)
	}
	if len(sections) == 0 {
		return manual(blocks, "Section detector is not configured.")
	}
	missing, evidence := []string{}, []Evidence{}
	for _, section := range sections {
		if field, ok := input.Schema.Fields[section]; ok && len(field.Values) > 0 {
			for _, source := range field.Evidence {
				evidence = append(evidence, schemaEvidence(source, blocks, "Required section is present."))
			}
			continue
		}
		matched := findBlock(blocks, func(block model.KBDocumentBlock) bool {
			return containsFold(block.TextContent, section) || sectionPathContains(block.SectionPath, section)
		})
		if matched == nil {
			missing = append(missing, section)
		} else {
			evidence = append(evidence, blockEvidence(*matched, "Required section is present."))
		}
	}
	if len(evidence) == 0 {
		evidence = []Evidence{fallbackEvidence(blocks, "Document outline was checked for required sections.")}
	}
	if len(missing) == 0 {
		return pass(evidence)
	}
	if !rule.Required {
		return notApplicable(evidence, "Optional section is absent.")
	}
	status := FindingMissing
	if len(missing) < len(sections) {
		status = FindingPartial
	}
	return fail(status, .98, evidence, "Missing sections: "+strings.Join(missing, ", "), "Add the missing sections and supporting content.")
}

func evaluatePattern(rule model.KBQualityRule, blocks []model.KBDocumentBlock) ruleOutcome {
	config := detectorConfig(rule)
	expression := configString(config, "pattern")
	if expression == "" {
		return manual(blocks, "Pattern detector is not configured.")
	}
	if len(expression) > 1000 {
		return manual(blocks, "Pattern is too long for deterministic execution.")
	}
	pattern, err := regexp.Compile(expression)
	if err != nil {
		return manual(blocks, "Pattern is invalid and requires correction.")
	}
	matched := findBlock(blocks, func(block model.KBDocumentBlock) bool { return pattern.MatchString(block.TextContent) })
	negate, _ := config["must_not_match"].(bool)
	if negate {
		if matched == nil {
			return pass([]Evidence{fallbackEvidence(blocks, "Forbidden pattern was not found.")})
		}
		return fail(FindingUnsafe, 1, []Evidence{blockEvidence(*matched, "Forbidden pattern matched.")}, "A forbidden pattern was found.", "Remove or replace the unsafe content.")
	}
	if matched != nil {
		return pass([]Evidence{blockEvidence(*matched, "Required pattern matched.")})
	}
	if !rule.Required {
		return notApplicable([]Evidence{fallbackEvidence(blocks, "Optional pattern was not found.")}, "Optional pattern was not found.")
	}
	return fail(FindingMissing, .99, []Evidence{fallbackEvidence(blocks, "Document was checked for the required pattern.")}, "Required pattern was not found.", "Add content matching the configured requirement.")
}

func evaluateMetadata(rule model.KBQualityRule, input EngineInput, blocks []model.KBDocumentBlock) ruleOutcome {
	config := detectorConfig(rule)
	fields := configStrings(config, "fields")
	if len(fields) == 0 {
		if value := configString(config, "field"); value != "" {
			fields = []string{value}
		}
	}
	if len(fields) == 0 {
		fields = fallbackMetadata(rule.RuleKey)
	}
	missing := []string{}
	for _, field := range fields {
		if !metadataPresent(field, input.Version) {
			missing = append(missing, field)
		}
	}
	evidence := []Evidence{fallbackEvidence(blocks, "Document version metadata was checked.")}
	if len(missing) == 0 {
		return pass(evidence)
	}
	if !rule.Required {
		return notApplicable(evidence, "Optional metadata is absent.")
	}
	return fail(FindingMissing, 1, evidence, "Missing metadata: "+strings.Join(missing, ", "), "Complete the document version metadata.")
}

func evaluateFreshness(rule model.KBQualityRule, input EngineInput, blocks []model.KBDocumentBlock) ruleOutcome {
	deadline := input.Version.ReviewDueAt
	if deadline == nil {
		deadline = input.Version.ValidUntil
	}
	if deadline == nil {
		if maxAge := configNumber(detectorConfig(rule), "max_age_days"); maxAge > 0 {
			value := input.Version.UpdatedAt.Add(time.Duration(maxAge*24) * time.Hour)
			deadline = &value
		}
	}
	evidence := []Evidence{fallbackEvidence(blocks, "Review due date and validity were checked.")}
	if deadline == nil {
		if rule.Required {
			return fail(FindingMissing, .98, evidence, "No review due date or validity deadline is set.", "Set a review due date.")
		}
		return notApplicable(evidence, "No freshness policy applies.")
	}
	evidence[0].Quote = deadline.Format(time.RFC3339)
	if input.Now.After(*deadline) {
		return fail(FindingOutdated, 1, evidence, "Document review deadline has expired.", "Review and publish a new document version.")
	}
	return pass(evidence)
}

func evaluateSafety(rule model.KBQualityRule, blocks []model.KBDocumentBlock) ruleOutcome {
	switch rule.RuleKey {
	case "sensitive_credential_exposed":
		if block := findBlock(blocks, containsCredential); block != nil {
			evidence := blockEvidence(*block, "A plaintext credential-like value was detected.")
			evidence.Quote = "[REDACTED sensitive credential]"
			return fail(FindingUnsafe, 1, []Evidence{evidence}, "Plaintext credential exposure detected.", "Remove the secret and rotate the credential.")
		}
		return pass([]Evidence{fallbackEvidence(blocks, "No plaintext credential pattern was detected.")})
	case "destructive_command_without_warning":
		if block := unsafeCommandWithoutContext(blocks, false); block != nil {
			return fail(FindingUnsafe, 1, []Evidence{blockEvidence(*block, "Destructive command has no adjacent risk warning.")}, "Destructive command is missing a risk warning.", "Add an adjacent warning, impact statement, and rollback guidance.")
		}
		return pass([]Evidence{fallbackEvidence(blocks, "Destructive commands include warning context or are absent.")})
	case "high_risk_action_without_approval":
		if block := unsafeCommandWithoutContext(blocks, true); block != nil {
			return fail(FindingUnsafe, 1, []Evidence{blockEvidence(*block, "High-risk command has no adjacent approval requirement.")}, "High-risk action is missing an approval requirement.", "Add approval or change-ticket requirements next to the command.")
		}
		return pass([]Evidence{fallbackEvidence(blocks, "High-risk actions include approval context or are absent.")})
	}
	if block := findBlock(blocks, containsCredential); block != nil {
		evidence := blockEvidence(*block, "Sensitive content was detected.")
		evidence.Quote = "[REDACTED sensitive credential]"
		return fail(FindingUnsafe, 1, []Evidence{evidence}, "Sensitive information was found.", "Remove sensitive values.")
	}
	if block := unsafeCommandWithoutContext(blocks, false); block != nil {
		return fail(FindingUnsafe, .99, []Evidence{blockEvidence(*block, "Risk control is missing near a dangerous command.")}, "Risk controls are incomplete.", "Add warning and rollback context.")
	}
	return pass([]Evidence{fallbackEvidence(blocks, "No deterministic safety violation was detected.")})
}

func evaluateEnvironmentConfusion(blocks []model.KBDocumentBlock) ruleOutcome {
	if block := findBlock(blocks, func(block model.KBDocumentBlock) bool {
		return prodPattern.MatchString(block.TextContent) && testPattern.MatchString(block.TextContent)
	}); block != nil {
		return fail(FindingConflicting, .95, []Evidence{blockEvidence(*block, "Production and test environments appear in the same instruction.")}, "Production/test environment confusion detected.", "Separate commands and parameters by environment.")
	}
	return pass([]Evidence{fallbackEvidence(blocks, "No same-block production/test conflict was detected.")})
}

func evaluateCriticalContradiction(blocks []model.KBDocumentBlock) ruleOutcome {
	for _, block := range blocks {
		text := strings.ToLower(block.TextContent)
		if (strings.Contains(text, "start") && strings.Contains(text, "stop")) || (strings.Contains(text, "启用") && strings.Contains(text, "禁用")) {
			return fail(FindingConflicting, .8, []Evidence{blockEvidence(block, "Opposing critical actions appear in the same block.")}, "Potentially contradictory critical steps detected.", "Clarify conditions and ordering for opposing actions.")
		}
	}
	return pass([]Evidence{fallbackEvidence(blocks, "No deterministic critical-step contradiction was detected.")})
}

func toRuleResult(criterionKey string, rule model.KBQualityRule, outcome ruleOutcome) model.KBQualityRuleResult {
	status, confidence, maxScore := outcome.status, outcome.confidence, rule.MaxScore
	result := model.KBQualityRuleResult{CriterionKey: criterionKey, RuleKey: rule.RuleKey, MaxScore: &maxScore, FindingStatus: &status, Confidence: &confidence, Evidence: marshal(outcome.evidence), Source: "deterministic"}
	if outcome.assessed {
		score := rule.MaxScore
		if !outcome.passed {
			score = 0
			if rule.Deduction != nil {
				score = math.Max(0, rule.MaxScore-*rule.Deduction)
			}
		}
		result.Score = &score
	}
	if outcome.reason != "" {
		result.DeductionReason = stringPtr(outcome.reason)
	}
	if outcome.suggestion != "" {
		result.Suggestion = stringPtr(outcome.suggestion)
	}
	return result
}

func pass(evidence []Evidence) ruleOutcome {
	return ruleOutcome{assessed: true, passed: true, status: FindingPresent, confidence: 1, evidence: evidence}
}
func fail(status string, confidence float64, evidence []Evidence, reason, suggestion string) ruleOutcome {
	return ruleOutcome{assessed: true, status: status, confidence: confidence, evidence: evidence, reason: reason, suggestion: suggestion}
}
func manual(blocks []model.KBDocumentBlock, reason string) ruleOutcome {
	return ruleOutcome{status: FindingManualConfirmationRequired, confidence: 1, evidence: []Evidence{fallbackEvidence(blocks, reason)}}
}
func notApplicable(evidence []Evidence, reason string) ruleOutcome {
	return ruleOutcome{status: FindingNotApplicable, confidence: 1, evidence: evidence, reason: reason}
}

func sortedBlocks(values []model.KBDocumentBlock) []model.KBDocumentBlock {
	result := append([]model.KBDocumentBlock(nil), values...)
	sort.SliceStable(result, func(i, j int) bool { return result[i].OrderNo < result[j].OrderNo })
	return result
}
func findBlock(blocks []model.KBDocumentBlock, predicate func(model.KBDocumentBlock) bool) *model.KBDocumentBlock {
	for index := range blocks {
		if predicate(blocks[index]) {
			return &blocks[index]
		}
	}
	return nil
}
func unsafeCommandWithoutContext(blocks []model.KBDocumentBlock, requireApproval bool) *model.KBDocumentBlock {
	for index := range blocks {
		if !dangerousPattern.MatchString(blocks[index].TextContent) {
			continue
		}
		start, end := index-1, index+1
		if start < 0 {
			start = 0
		}
		if end >= len(blocks) {
			end = len(blocks) - 1
		}
		context := ""
		for cursor := start; cursor <= end; cursor++ {
			context += " " + blocks[cursor].TextContent
		}
		if requireApproval {
			if !approvalPattern.MatchString(context) {
				return &blocks[index]
			}
		} else if !warningPattern.MatchString(context) {
			return &blocks[index]
		}
	}
	return nil
}
func containsCredential(block model.KBDocumentBlock) bool {
	for _, pattern := range credentialPatterns {
		match := pattern.FindStringSubmatch(block.TextContent)
		if len(match) == 0 {
			continue
		}
		if strings.Contains(strings.ToUpper(match[0]), "PRIVATE KEY") {
			return true
		}
		value := strings.ToLower(strings.Join(match[1:], ""))
		if value != "" && !credentialPlaceholder(value) {
			return true
		}
	}
	return false
}
func credentialPlaceholder(value string) bool {
	return strings.Contains(value, "${") || strings.Contains(value, "{{") || strings.Contains(value, "<") || strings.Contains(value, "***") || strings.Contains(value, "replace-with") || strings.Contains(value, "example")
}
func blockEvidence(block model.KBDocumentBlock, reason string) Evidence {
	var path []string
	_ = json.Unmarshal(block.SectionPath, &path)
	quote := truncate(block.TextContent, 240)
	if containsCredential(block) {
		quote = "[REDACTED sensitive credential]"
	}
	return Evidence{BlockID: block.BlockKey, SectionPath: path, Page: block.PageNo, Quote: quote, Reason: reason}
}
func fallbackEvidence(blocks []model.KBDocumentBlock, reason string) Evidence {
	if len(blocks) > 0 {
		return blockEvidence(blocks[0], reason)
	}
	return Evidence{BlockID: "document-root", Quote: "", Reason: reason}
}
func schemaEvidence(source document.SchemaEvidence, blocks []model.KBDocumentBlock, reason string) Evidence {
	if source.BlockKey != "" {
		if block := findBlock(blocks, func(value model.KBDocumentBlock) bool { return value.BlockKey == source.BlockKey }); block != nil {
			return blockEvidence(*block, reason)
		}
	}
	quote := truncate(source.Text, 240)
	if containsCredential(model.KBDocumentBlock{TextContent: source.Text}) {
		quote = "[REDACTED sensitive credential]"
	}
	return Evidence{BlockID: "document-root", Page: source.Page, Quote: quote, Reason: reason}
}
func sectionPathContains(value []byte, expected string) bool {
	var path []string
	_ = json.Unmarshal(value, &path)
	for _, item := range path {
		if containsFold(item, expected) {
			return true
		}
	}
	return false
}
func containsFold(value, expected string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(expected))
}
func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
func detectorConfig(rule model.KBQualityRule) map[string]any {
	result := map[string]any{}
	_ = json.Unmarshal(rule.DetectorConfig, &result)
	return result
}
func configString(config map[string]any, key string) string {
	value, _ := config[key].(string)
	return strings.TrimSpace(value)
}
func configStrings(config map[string]any, key string) []string {
	raw, ok := config[key].([]any)
	if !ok {
		return nil
	}
	result := []string{}
	for _, item := range raw {
		if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
			result = append(result, strings.TrimSpace(value))
		}
	}
	return result
}
func configNumber(config map[string]any, key string) int {
	value, _ := config[key].(float64)
	return int(value)
}
func fallbackFields(key string) []string {
	switch key {
	case "empty_document":
		return []string{"title"}
	}
	return nil
}
func fallbackSections(key, docType string) []string {
	switch key {
	case "document_structure_complete":
		if docType == "runbook" {
			return []string{"steps", "verification", "rollback"}
		}
	case "missing_rollback_for_change_plan":
		return []string{"rollback"}
	case "missing_verification_for_runbook", "verification_complete":
		return []string{"verification"}
	}
	return nil
}
func fallbackMetadata(key string) []string {
	if key == "metadata_traceable" {
		return []string{"version"}
	}
	return nil
}
func metadataPresent(field string, version *model.KBDocumentVersion) bool {
	if version == nil {
		return false
	}
	switch field {
	case "version":
		return strings.TrimSpace(version.Version) != ""
	case "parser_name":
		return version.ParserName != nil && strings.TrimSpace(*version.ParserName) != ""
	case "language":
		return version.Language != nil && strings.TrimSpace(*version.Language) != ""
	case "review_due_at":
		return version.ReviewDueAt != nil
	case "valid_until":
		return version.ValidUntil != nil
	case "file_hash":
		return strings.TrimSpace(version.FileHash) != ""
	}
	return false
}
func marshal(value any) json.RawMessage { data, _ := json.Marshal(value); return data }
func stringPtr(value string) *string    { return &value }
func roundScore(value float64) float64  { return math.Round(value*100) / 100 }
