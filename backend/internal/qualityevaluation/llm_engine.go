package qualityevaluation

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	llmsvc "aiops-platform/backend/internal/llm"
	"aiops-platform/backend/internal/model"
)

const (
	maxBlocksPerLLMBatch = 12
	maxLLMBatchTextBytes = 16000
)

type LLMRunConfig struct {
	BaseURL     string
	Provider    string
	APIKey      string
	AppKey      string
	APISecret   string
	Model       string
	Temperature float64
}

type LLMEvaluationOutput struct {
	Results            []model.KBQualityRuleResult
	CriterionScores    map[string]CriterionScore
	Calls              int
	FailedCalls        int
	ValidationWarnings []string
}

type CriterionScore struct {
	Score    float64 `json:"score"`
	MaxScore float64 `json:"maxScore"`
}

type llmRuleResponse struct {
	RuleKey         string        `json:"ruleKey"`
	Score           float64       `json:"score"`
	MaxScore        float64       `json:"maxScore"`
	Status          string        `json:"status"`
	Confidence      float64       `json:"confidence"`
	Evidence        []llmEvidence `json:"evidence"`
	DeductionReason string        `json:"deductionReason"`
	Suggestion      string        `json:"suggestion"`
}

type llmEvidence struct {
	BlockID string `json:"blockId"`
	Quote   string `json:"quote"`
	Reason  string `json:"reason"`
}

type llmCriterionResponse struct {
	CriterionKey string            `json:"criterionKey"`
	RuleResults  []llmRuleResponse `json:"ruleResults"`
}

func EvaluateWithLLM(ctx context.Context, client llmsvc.Client, config LLMRunConfig, profile *model.KBQualityProfile, blocks []model.KBDocumentBlock, pending map[string]struct{}) LLMEvaluationOutput {
	output := LLMEvaluationOutput{Results: []model.KBQualityRuleResult{}, CriterionScores: map[string]CriterionScore{}, ValidationWarnings: []string{}}
	if client == nil || profile == nil {
		return output
	}
	for _, criterion := range profile.Criteria {
		rules := []model.KBQualityRule{}
		for _, rule := range criterion.Rules {
			key := criterion.CriterionKey + "\x00" + rule.RuleKey
			if _, ok := pending[key]; ok && rule.RuleType != "manual" {
				rules = append(rules, rule)
			}
		}
		if len(rules) == 0 {
			continue
		}
		relevant := selectRelevantBlocks(criterion, rules, blocks)
		batches := splitBlockBatches(relevant)
		if isCrossSectionCriterion(criterion, rules) && len(blocks) > maxBlocksPerLLMBatch {
			if anchors := crossSectionAnchorBlocks(blocks); len(anchors) > 0 {
				batches = append([][]model.KBDocumentBlock{anchors}, batches...)
			}
		}
		if len(batches) == 0 {
			batches = [][]model.KBDocumentBlock{{}}
		}
		mapped := map[string][]model.KBQualityRuleResult{}
		for batchIndex, batch := range batches {
			output.Calls++
			response, err := callCriterionBatch(ctx, client, config, criterion, rules, batch, batchIndex+1, len(batches))
			if err != nil {
				output.FailedCalls++
				output.ValidationWarnings = append(output.ValidationWarnings, fmt.Sprintf("criterion %s batch %d: %v", criterion.CriterionKey, batchIndex+1, err))
				continue
			}
			batchBlocks := map[string]model.KBDocumentBlock{}
			for _, block := range batch {
				batchBlocks[block.BlockKey] = block
			}
			validated, warnings := validateLLMResponse(response, criterion, rules, batchBlocks)
			output.ValidationWarnings = append(output.ValidationWarnings, warnings...)
			for _, result := range validated {
				mapped[result.RuleKey] = append(mapped[result.RuleKey], result)
			}
		}
		for _, rule := range rules {
			values := mapped[rule.RuleKey]
			if len(values) == 0 {
				continue
			}
			result := reduceLLMRuleResults(criterion.CriterionKey, rule, values)
			output.Results = append(output.Results, result)
			score := output.CriterionScores[criterion.CriterionKey]
			if result.Score != nil {
				score.Score += *result.Score
			}
			if result.MaxScore != nil {
				score.MaxScore += *result.MaxScore
			}
			output.CriterionScores[criterion.CriterionKey] = score
		}
	}
	return output
}

func callCriterionBatch(ctx context.Context, client llmsvc.Client, config LLMRunConfig, criterion model.KBQualityCriterion, rules []model.KBQualityRule, blocks []model.KBDocumentBlock, batch, totalBatches int) (*llmCriterionResponse, error) {
	rulePayload := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		rulePayload = append(rulePayload, map[string]any{"ruleKey": rule.RuleKey, "name": rule.Name, "description": rule.Description, "maxScore": rule.MaxScore, "hardGate": rule.HardGate, "llmInstruction": rule.LLMInstruction})
	}
	blockPayload := make([]map[string]any, 0, len(blocks))
	for _, block := range blocks {
		blockPayload = append(blockPayload, map[string]any{"blockId": block.BlockKey, "type": block.BlockType, "sectionPath": decodeSectionPath(block.SectionPath), "page": block.PageNo, "text": safePromptText(block)})
	}
	payload, _ := json.Marshal(map[string]any{"criterion": map[string]any{"criterionKey": criterion.CriterionKey, "name": criterion.Name, "description": criterion.Description}, "rules": rulePayload, "mapBatch": map[string]int{"index": batch, "total": totalBatches}, "blocks": blockPayload})
	system := `You evaluate operational-document quality. Return one JSON object only. Never infer a finding without evidence. Every scored rule must cite at least one supplied blockId and an exact quote from that block. If evidence is insufficient, omit that rule from ruleResults. Scores must be within [0,maxScore]. Allowed status: present, missing, partial, conflicting, outdated, unsafe. Output schema: {"criterionKey":"string","ruleResults":[{"ruleKey":"string","score":0,"maxScore":0,"status":"present","confidence":0.0,"evidence":[{"blockId":"string","quote":"exact supplied text","reason":"string"}],"deductionReason":"string","suggestion":"string"}]}. This is a map phase; do not invent evidence from blocks outside this batch.`
	if totalBatches > 1 {
		system += " The document is long and split into map batches. Report only observations supported in this batch; local code will reduce evidence across batches."
	}
	if isCrossSectionCriterion(criterion, rules) {
		system += " Perform a cross-section consistency check for conflicting environments, parameters, versions, action/rollback targets, and critical steps."
	}
	result, err := client.Chat(ctx, llmsvc.ChatRequest{BaseURL: config.BaseURL, Provider: config.Provider, APIKey: config.APIKey, AppKey: config.AppKey, APISecret: config.APISecret, Model: config.Model, Temperature: config.Temperature, Messages: []llmsvc.ChatMessage{{Role: "system", Content: system}, {Role: "user", Content: string(payload)}}})
	if err != nil {
		return nil, fmt.Errorf("call llm: %w", err)
	}
	if result == nil || strings.TrimSpace(result.Content) == "" {
		return nil, fmt.Errorf("llm returned empty content")
	}
	content := extractJSONObject(result.Content)
	var response llmCriterionResponse
	if json.Unmarshal([]byte(content), &response) != nil {
		return nil, fmt.Errorf("llm response is not valid JSON")
	}
	return &response, nil
}

func validateLLMResponse(response *llmCriterionResponse, criterion model.KBQualityCriterion, rules []model.KBQualityRule, blocks map[string]model.KBDocumentBlock) ([]model.KBQualityRuleResult, []string) {
	if response == nil || response.CriterionKey != criterion.CriterionKey {
		return nil, []string{"criterion key mismatch"}
	}
	ruleByKey := map[string]model.KBQualityRule{}
	for _, rule := range rules {
		ruleByKey[rule.RuleKey] = rule
	}
	results, warnings, seen := []model.KBQualityRuleResult{}, []string{}, map[string]struct{}{}
	for _, candidate := range response.RuleResults {
		rule, ok := ruleByKey[candidate.RuleKey]
		if !ok {
			warnings = append(warnings, "unknown rule key: "+candidate.RuleKey)
			continue
		}
		if _, exists := seen[candidate.RuleKey]; exists {
			warnings = append(warnings, "duplicate rule key: "+candidate.RuleKey)
			continue
		}
		seen[candidate.RuleKey] = struct{}{}
		if candidate.Score < 0 || candidate.Score > rule.MaxScore || mathAbs(candidate.MaxScore-rule.MaxScore) > .005 {
			warnings = append(warnings, "invalid score for rule: "+candidate.RuleKey)
			continue
		}
		if !allowedLLMStatus(candidate.Status) || candidate.Confidence < 0 || candidate.Confidence > 1 {
			warnings = append(warnings, "invalid status or confidence for rule: "+candidate.RuleKey)
			continue
		}
		evidence := []Evidence{}
		validEvidence := true
		for _, item := range candidate.Evidence {
			block, exists := blocks[item.BlockID]
			if !exists || strings.TrimSpace(item.Quote) == "" || strings.TrimSpace(item.Reason) == "" {
				validEvidence = false
				break
			}
			safeText := safePromptText(block)
			if !strings.Contains(normalizeEvidenceText(safeText), normalizeEvidenceText(item.Quote)) {
				validEvidence = false
				break
			}
			value := blockEvidence(block, item.Reason)
			value.Quote = truncate(item.Quote, 240)
			if containsCredential(block) {
				value.Quote = "[REDACTED sensitive credential]"
			}
			evidence = append(evidence, value)
		}
		if !validEvidence || len(evidence) == 0 {
			warnings = append(warnings, "invalid or missing evidence for rule: "+candidate.RuleKey)
			continue
		}
		score, maxScore, status, confidence := candidate.Score, rule.MaxScore, candidate.Status, candidate.Confidence
		result := model.KBQualityRuleResult{CriterionKey: criterion.CriterionKey, RuleKey: rule.RuleKey, Score: &score, MaxScore: &maxScore, FindingStatus: &status, Confidence: &confidence, Evidence: marshal(evidence), Source: "llm"}
		if strings.TrimSpace(candidate.DeductionReason) != "" {
			result.DeductionReason = stringPtr(candidate.DeductionReason)
		}
		if strings.TrimSpace(candidate.Suggestion) != "" {
			result.Suggestion = stringPtr(candidate.Suggestion)
		}
		results = append(results, result)
	}
	return results, warnings
}

func reduceLLMRuleResults(criterionKey string, rule model.KBQualityRule, values []model.KBQualityRuleResult) model.KBQualityRuleResult {
	selected := values[0]
	evidenceByKey := map[string]Evidence{}
	allEvidence := []Evidence{}
	for _, value := range values {
		if value.Score != nil && (selected.Score == nil || *value.Score < *selected.Score) {
			selected = value
		}
		var evidence []Evidence
		_ = json.Unmarshal(value.Evidence, &evidence)
		for _, item := range evidence {
			key := item.BlockID + "\x00" + item.Quote
			if _, exists := evidenceByKey[key]; !exists {
				evidenceByKey[key] = item
				allEvidence = append(allEvidence, item)
			}
		}
	}
	selected.CriterionKey, selected.RuleKey, selected.Evidence = criterionKey, rule.RuleKey, marshal(allEvidence)
	return selected
}

func selectRelevantBlocks(criterion model.KBQualityCriterion, rules []model.KBQualityRule, blocks []model.KBDocumentBlock) []model.KBDocumentBlock {
	keywords := criterionKeywords(criterion, rules)
	type scored struct {
		block model.KBDocumentBlock
		score int
	}
	ranked := make([]scored, 0, len(blocks))
	for _, block := range blocks {
		text := strings.ToLower(block.TextContent + " " + strings.Join(decodeSectionPath(block.SectionPath), " "))
		score := 0
		for _, keyword := range keywords {
			if strings.Contains(text, keyword) {
				score += 4
			}
		}
		if block.BlockType == "heading" {
			score += 2
		}
		if block.BlockType == "command_block" || dangerousPattern.MatchString(block.TextContent) {
			score += 5
		}
		if score > 0 {
			ranked = append(ranked, scored{block: block, score: score})
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score == ranked[j].score {
			return ranked[i].block.OrderNo < ranked[j].block.OrderNo
		}
		return ranked[i].score > ranked[j].score
	})
	result := make([]model.KBDocumentBlock, 0, len(ranked))
	selected := make(map[string]struct{}, len(ranked))
	for _, item := range ranked {
		result = append(result, item.block)
		selected[item.block.BlockKey] = struct{}{}
	}
	for _, block := range sortedBlocks(blocks) {
		if _, ok := selected[block.BlockKey]; ok {
			continue
		}
		result = append(result, block)
	}
	return result
}

func splitBlockBatches(blocks []model.KBDocumentBlock) [][]model.KBDocumentBlock {
	batches, current, size := [][]model.KBDocumentBlock{}, []model.KBDocumentBlock{}, 0
	for _, block := range blocks {
		textSize := len(safePromptText(block))
		if len(current) > 0 && (len(current) >= maxBlocksPerLLMBatch || size+textSize > maxLLMBatchTextBytes) {
			batches = append(batches, current)
			current, size = nil, 0
		}
		current = append(current, block)
		size += textSize
	}
	if len(current) > 0 {
		batches = append(batches, current)
	}
	return batches
}

func crossSectionAnchorBlocks(blocks []model.KBDocumentBlock) []model.KBDocumentBlock {
	if len(blocks) <= maxBlocksPerLLMBatch {
		return append([]model.KBDocumentBlock(nil), blocks...)
	}
	selected := map[string]model.KBDocumentBlock{}
	add := func(block model.KBDocumentBlock) {
		if len(selected) < maxBlocksPerLLMBatch {
			selected[block.BlockKey] = block
		}
	}
	add(blocks[0])
	add(blocks[len(blocks)-1])
	for _, block := range blocks {
		if block.BlockType == "heading" || block.BlockType == "command_block" || dangerousPattern.MatchString(block.TextContent) {
			add(block)
		}
	}
	remaining := maxBlocksPerLLMBatch - len(selected)
	if remaining > 0 {
		step := float64(len(blocks)-1) / float64(remaining+1)
		for index := 1; index <= remaining; index++ {
			add(blocks[int(float64(index)*step)])
		}
	}
	result := make([]model.KBDocumentBlock, 0, len(selected))
	for _, block := range selected {
		result = append(result, block)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].OrderNo < result[j].OrderNo })
	return result
}

func criterionKeywords(criterion model.KBQualityCriterion, rules []model.KBQualityRule) []string {
	values := []string{strings.ToLower(criterion.CriterionKey), strings.ToLower(criterion.Name)}
	mapping := map[string][]string{"safety": {"risk", "warning", "rollback", "approval", "风险", "警告", "回滚", "审批"}, "operability": {"step", "prerequisite", "escalation", "步骤", "前置", "升级"}, "verifiability": {"verify", "expected", "failure", "验证", "预期", "失败"}, "accuracy": {"version", "environment", "parameter", "版本", "环境", "参数"}}
	values = append(values, mapping[criterion.CriterionKey]...)
	for _, rule := range rules {
		values = append(values, strings.ToLower(rule.Name), strings.ToLower(rule.RuleKey))
	}
	return values
}
func isCrossSectionCriterion(criterion model.KBQualityCriterion, rules []model.KBQualityRule) bool {
	if criterion.CriterionKey == "accuracy" || criterion.ScoringMethod == "llm" {
		return true
	}
	for _, rule := range rules {
		if rule.RuleType == "consistency" || rule.RuleType == "cross_reference" {
			return true
		}
	}
	return false
}
func safePromptText(block model.KBDocumentBlock) string {
	if containsCredential(block) {
		return "[REDACTED sensitive credential]"
	}
	return truncate(block.TextContent, 4000)
}
func decodeSectionPath(value []byte) []string {
	result := []string{}
	_ = json.Unmarshal(value, &result)
	return result
}
func extractJSONObject(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "```json")
	value = strings.TrimPrefix(value, "```")
	value = strings.TrimSuffix(value, "```")
	start, end := strings.Index(value, "{"), strings.LastIndex(value, "}")
	if start >= 0 && end >= start {
		return value[start : end+1]
	}
	return value
}
func normalizeEvidenceText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}
func allowedLLMStatus(value string) bool {
	switch value {
	case FindingPresent, FindingMissing, FindingPartial, FindingConflicting, FindingOutdated, FindingUnsafe:
		return true
	}
	return false
}
func mathAbs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
