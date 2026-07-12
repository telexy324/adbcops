package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type KnowledgeAgent struct{}

func (KnowledgeAgent) Name() string {
	return "knowledge_agent"
}

func (KnowledgeAgent) Description() string {
	return "Searches published knowledge and turns citations into evidence-backed facts."
}

func (KnowledgeAgent) Analyze(ctx context.Context, input AgentContext, runtime *RunContext) (*AgentResult, error) {
	if err := runtime.Step("search knowledge"); err != nil {
		return nil, err
	}
	query := firstNonEmpty(stringVariable(input, "query"), input.Query)
	if strings.TrimSpace(query) == "" {
		return needsScopeResult("knowledge_agent", "query is required to search knowledge"), nil
	}
	payload := map[string]any{"query": query, "limit": intVariable(input, "limit", 5)}
	result, err := executeJSONSkill(ctx, runtime, "search_knowledge", payload)
	if err != nil {
		return nil, err
	}
	return skillResult("knowledge_agent", "search_knowledge", result, "knowledge search completed", 0.72), nil
}

type LogAgent struct{}

func (LogAgent) Name() string {
	return "log_agent"
}

func (LogAgent) Description() string {
	return "Queries logs and extracts log evidence for incident hypotheses."
}

func (LogAgent) Analyze(ctx context.Context, input AgentContext, runtime *RunContext) (*AgentResult, error) {
	payload, missing := baseTimeWindowPayload(input, "dataSourceId", "from", "to")
	if len(missing) > 0 {
		return needsScopeResult("log_agent", "missing log scope: "+strings.Join(missing, ", ")), nil
	}
	copyOptional(input, payload, "index", "keyword", "queryString", "level", "size", "allowLargeRange")
	if payload["keyword"] == nil && payload["queryString"] == nil && strings.TrimSpace(input.Query) != "" {
		payload["keyword"] = input.Query
	}
	if err := runtime.Step("query logs"); err != nil {
		return nil, err
	}
	result, err := executeJSONSkill(ctx, runtime, "query_logs", payload)
	if err != nil {
		return nil, err
	}
	return skillResult("log_agent", "query_logs", result, "log query completed", 0.68), nil
}

type MetricsAgent struct{}

func (MetricsAgent) Name() string {
	return "metrics_agent"
}

func (MetricsAgent) Description() string {
	return "Queries metrics and summarizes metric evidence for anomalies."
}

func (MetricsAgent) Analyze(ctx context.Context, input AgentContext, runtime *RunContext) (*AgentResult, error) {
	payload := map[string]any{}
	dataSourceID, ok := int64Variable(input, "dataSourceId")
	if !ok {
		return needsScopeResult("metrics_agent", "missing metrics scope: dataSourceId"), nil
	}
	query := firstNonEmpty(stringVariable(input, "promql"), stringVariable(input, "query"))
	if query == "" {
		return needsScopeResult("metrics_agent", "missing metrics scope: query"), nil
	}
	payload["dataSourceId"] = dataSourceID
	payload["query"] = query
	copyOptional(input, payload, "range", "start", "end", "stepSeconds", "maxSeries", "maxPoints")
	if err := runtime.Step("query metrics"); err != nil {
		return nil, err
	}
	result, err := executeJSONSkill(ctx, runtime, "query_metrics", payload)
	if err != nil {
		return nil, err
	}
	return skillResult("metrics_agent", "query_metrics", result, "metrics query completed", 0.66), nil
}

type KubernetesAgent struct{}

func (KubernetesAgent) Name() string {
	return "kubernetes_agent"
}

func (KubernetesAgent) Description() string {
	return "Collects Kubernetes pod context and deterministic rule findings."
}

func (KubernetesAgent) Analyze(ctx context.Context, input AgentContext, runtime *RunContext) (*AgentResult, error) {
	payload := map[string]any{}
	missing := []string{}
	dataSourceID, ok := int64Variable(input, "dataSourceId")
	if !ok {
		missing = append(missing, "dataSourceId")
	} else {
		payload["dataSourceId"] = dataSourceID
	}
	namespace := stringVariable(input, "namespace")
	if namespace == "" {
		missing = append(missing, "namespace")
	} else {
		payload["namespace"] = namespace
	}
	podName := firstNonEmpty(stringVariable(input, "podName"), stringVariable(input, "pod"))
	if podName == "" {
		missing = append(missing, "podName")
	} else {
		payload["podName"] = podName
	}
	if len(missing) > 0 {
		return needsScopeResult("kubernetes_agent", "missing kubernetes scope: "+strings.Join(missing, ", ")), nil
	}
	copyOptional(input, payload, "includeNode", "includePreviousLogs", "logTailLines", "logMaxBytes")
	if err := runtime.Step("collect pod context"); err != nil {
		return nil, err
	}
	result, err := executeJSONSkill(ctx, runtime, "get_pod_context", payload)
	if err != nil {
		return nil, err
	}
	if err := runtime.Step("run kubernetes diagnostic rules"); err != nil {
		return nil, err
	}
	rules, err := executeJSONSkill(ctx, runtime, "run_k8s_diagnostic_rules", payload)
	if err != nil {
		return nil, err
	}
	return combinedSkillResult("kubernetes_agent", []skillEvidence{
		{Skill: "get_pod_context", Result: result},
		{Skill: "run_k8s_diagnostic_rules", Result: rules},
	}, "kubernetes context and diagnostic rules completed", 0.7), nil
}

type ChangeAgent struct{}

func (ChangeAgent) Name() string {
	return "change_agent"
}

func (ChangeAgent) Description() string {
	return "Queries recent release, configuration and Git changes without blocking on partial source failures."
}

func (ChangeAgent) Analyze(ctx context.Context, input AgentContext, runtime *RunContext) (*AgentResult, error) {
	dataSourceID, ok := int64Variable(input, "dataSourceId")
	if !ok {
		return needsScopeResult("change_agent", "missing change scope: dataSourceId"), nil
	}
	payload := map[string]any{
		"dataSourceId": dataSourceID,
	}
	copyOptional(input, payload, "from", "to", "environment", "systemName", "component", "limit")
	if payload["from"] == nil && payload["to"] == nil {
		now := time.Now().UTC()
		payload["from"] = now.Add(-2 * time.Hour).Format(time.RFC3339)
		payload["to"] = now.Format(time.RFC3339)
	}
	if err := runtime.Step("query recent changes"); err != nil {
		return nil, err
	}
	result, err := executeJSONSkill(ctx, runtime, "query_recent_changes", payload)
	if err != nil {
		return nil, err
	}
	return skillResult("change_agent", "query_recent_changes", result, "recent changes query completed", 0.62), nil
}

type IncidentAgent struct{}

func (IncidentAgent) Name() string {
	return "incident_agent"
}

func (IncidentAgent) Description() string {
	return "Builds an evidence-referenced incident report from timeline and correlation results."
}

func (IncidentAgent) Analyze(ctx context.Context, input AgentContext, runtime *RunContext) (*AgentResult, error) {
	targetEventID, ok := int64Variable(input, "targetEventId")
	if !ok {
		return needsScopeResult("incident_agent", "missing incident scope: targetEventId"), nil
	}
	basePayload := map[string]any{
		"targetEventId": targetEventID,
	}
	copyOptional(input, basePayload, "from", "to", "beforeMinutes", "afterMinutes", "environment", "systemName", "componentName", "namespace", "resourceName", "limit")
	if err := runtime.Step("build incident timeline"); err != nil {
		return nil, err
	}
	timelinePayload := copyMap(basePayload)
	timelinePayload["anchorEventId"] = targetEventID
	timelinePayload["includeEvidence"] = true
	timelineResult, err := executeJSONSkill(ctx, runtime, "build_incident_timeline", timelinePayload)
	if err != nil {
		return nil, err
	}
	if err := runtime.Step("correlate incident events"); err != nil {
		return nil, err
	}
	correlationPayload := copyMap(basePayload)
	if _, exists := correlationPayload["includeTopology"]; !exists {
		correlationPayload["includeTopology"] = true
	}
	copyOptional(input, correlationPayload, "includeTopology")
	correlationResult, err := executeJSONSkill(ctx, runtime, "correlate_incident_events", correlationPayload)
	if err != nil {
		return nil, err
	}
	report := buildIncidentReport(input, timelineResult.Output, correlationResult.Output)
	structured, _ := json.Marshal(report)
	evidenceRefs := uniqueStringsForAgent(report.EvidenceKeys)
	facts := []Fact{}
	for _, key := range firstN(evidenceRefs, 3) {
		facts = append(facts, Fact{Summary: "incident report references evidence " + key, EvidenceKey: key})
	}
	if len(facts) == 0 {
		facts = append(facts, Fact{Summary: "incident report has missing evidence and requires additional collection"})
	}
	confidence := report.Confidence
	return &AgentResult{
		Summary:      report.Summary,
		Facts:        facts,
		Hypotheses:   report.Hypotheses,
		EvidenceRefs: evidenceRefs,
		Structured:   structured,
		Confidence:   confidence,
	}, nil
}

type incidentReport struct {
	Summary             string                    `json:"summary"`
	TimelineSummary     string                    `json:"timelineSummary"`
	RootCauseCandidates []incidentReportCandidate `json:"rootCauseCandidates"`
	EvidenceKeys        []string                  `json:"evidenceKeys"`
	CounterEvidenceKeys []string                  `json:"counterEvidenceKeys"`
	MissingEvidence     []string                  `json:"missingEvidence"`
	Hypotheses          []Hypothesis              `json:"hypotheses"`
	Confidence          float64                   `json:"confidence"`
}

type incidentReportCandidate struct {
	Summary             string   `json:"summary"`
	Score               float64  `json:"score"`
	EvidenceKeys        []string `json:"evidenceKeys"`
	CounterEvidenceKeys []string `json:"counterEvidenceKeys"`
	Reason              string   `json:"reason"`
}

func buildIncidentReport(input AgentContext, timelineOutput json.RawMessage, correlationOutput json.RawMessage) incidentReport {
	timelineEvidence := evidenceKeysFromSkillOutput(timelineOutput)
	missingEvidence := missingEvidenceFromSkillOutput(timelineOutput)
	candidates := candidatesFromCorrelation(correlationOutput, timelineEvidence)
	evidenceKeys := append([]string{}, timelineEvidence...)
	for _, candidate := range candidates {
		evidenceKeys = append(evidenceKeys, candidate.EvidenceKeys...)
	}
	evidenceKeys = uniqueStringsForAgent(evidenceKeys)
	counterEvidence := []string{}
	for _, candidate := range candidates {
		counterEvidence = append(counterEvidence, candidate.CounterEvidenceKeys...)
	}
	counterEvidence = uniqueStringsForAgent(counterEvidence)
	confidence := incidentConfidence(candidates, missingEvidence, evidenceKeys)
	summary := firstNonEmpty(strings.TrimSpace(input.Query), "incident analysis report")
	if len(candidates) > 0 {
		summary = "Most likely root cause: " + candidates[0].Summary
	}
	hypotheses := make([]Hypothesis, 0, len(candidates))
	for _, candidate := range candidates {
		hypotheses = append(hypotheses, Hypothesis{Summary: candidate.Summary + " (" + candidate.Reason + ")", Confidence: candidate.Score})
	}
	if len(hypotheses) == 0 {
		hypotheses = append(hypotheses, Hypothesis{Summary: "insufficient evidence to rank root cause candidates", Confidence: 0.2})
	}
	return incidentReport{
		Summary:             summary,
		TimelineSummary:     timelineSummaryFromSkillOutput(timelineOutput),
		RootCauseCandidates: candidates,
		EvidenceKeys:        evidenceKeys,
		CounterEvidenceKeys: counterEvidence,
		MissingEvidence:     uniqueStringsForAgent(missingEvidence),
		Hypotheses:          hypotheses,
		Confidence:          confidence,
	}
}

func evidenceKeysFromSkillOutput(raw json.RawMessage) []string {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return uniqueStringsForAgent(collectStringsByKey(value, map[string]bool{"evidenceKeys": true, "evidenceKey": true, "evidence_refs": true}))
}

func missingEvidenceFromSkillOutput(raw json.RawMessage) []string {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return uniqueStringsForAgent(collectStringsByKey(value, map[string]bool{"evidenceMissing": true, "missingEvidence": true}))
}

func candidatesFromCorrelation(raw json.RawMessage, timelineEvidence []string) []incidentReportCandidate {
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil
	}
	correlation, _ := body["correlation"].(map[string]any)
	rawCandidates, _ := correlation["candidates"].([]any)
	result := []incidentReportCandidate{}
	for _, rawCandidate := range rawCandidates {
		candidateMap, _ := rawCandidate.(map[string]any)
		eventMap, _ := candidateMap["event"].(map[string]any)
		summary, _ := eventMap["summary"].(string)
		if summary == "" {
			summary, _ = candidateMap["reason"].(string)
		}
		score, _ := numericValue(candidateMap["score"])
		reason, _ := candidateMap["reason"].(string)
		evidenceKeys := uniqueStringsForAgent(collectStringsByKey(candidateMap, map[string]bool{"evidenceKeys": true, "evidenceKey": true, "evidence_refs": true}))
		counter := counterEvidenceForCandidate(timelineEvidence, evidenceKeys)
		result = append(result, incidentReportCandidate{Summary: summary, Score: score, Reason: reason, EvidenceKeys: evidenceKeys, CounterEvidenceKeys: counter})
	}
	return result
}

func counterEvidenceForCandidate(timelineEvidence []string, candidateEvidence []string) []string {
	candidateSet := map[string]struct{}{}
	for _, key := range candidateEvidence {
		candidateSet[key] = struct{}{}
	}
	result := []string{}
	for _, key := range timelineEvidence {
		if _, ok := candidateSet[key]; !ok {
			result = append(result, key)
		}
	}
	if len(result) > 5 {
		result = result[:5]
	}
	return uniqueStringsForAgent(result)
}

func timelineSummaryFromSkillOutput(raw json.RawMessage) string {
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return "timeline unavailable"
	}
	timeline, _ := body["timeline"].(map[string]any)
	items, _ := timeline["items"].([]any)
	return fmt.Sprintf("timeline contains %d event(s)", len(items))
}

func collectStringsByKey(value any, names map[string]bool) []string {
	result := []string{}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if names[key] {
				result = append(result, stringsFromAny(child)...)
				continue
			}
			result = append(result, collectStringsByKey(child, names)...)
		}
	case []any:
		for _, child := range typed {
			result = append(result, collectStringsByKey(child, names)...)
		}
	}
	return result
}

func stringsFromAny(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{strings.TrimSpace(typed)}
	case []any:
		result := []string{}
		for _, item := range typed {
			if text, ok := item.(string); ok {
				result = append(result, strings.TrimSpace(text))
			}
		}
		return result
	default:
		return nil
	}
}

func incidentConfidence(candidates []incidentReportCandidate, missingEvidence []string, evidenceKeys []string) float64 {
	if len(candidates) == 0 || len(evidenceKeys) == 0 {
		return 0.3
	}
	confidence := candidates[0].Score
	if len(missingEvidence) > 0 && confidence > 0.65 {
		confidence = 0.65
	}
	if confidence > 0.9 {
		confidence = 0.9
	}
	if confidence < 0.1 {
		confidence = 0.1
	}
	return confidence
}

func copyMap(input map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range input {
		result[key] = value
	}
	return result
}

func firstN(values []string, n int) []string {
	if len(values) <= n {
		return values
	}
	return values[:n]
}

func uniqueStringsForAgent(values []string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

type skillEvidence struct {
	Skill  string
	Result *skillExecutionOutput
}

type skillExecutionOutput struct {
	SkillName string
	RunID     int64
	Output    json.RawMessage
}

func executeJSONSkill(ctx context.Context, runtime *RunContext, name string, payload map[string]any) (*skillExecutionOutput, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, ErrInvalidInput
	}
	result, err := runtime.ExecuteSkill(ctx, name, raw)
	if err != nil {
		return nil, err
	}
	return &skillExecutionOutput{SkillName: result.SkillName, RunID: result.RunID, Output: result.Output}, nil
}

func skillResult(agentName, skillName string, result *skillExecutionOutput, summary string, confidence float64) *AgentResult {
	return combinedSkillResult(agentName, []skillEvidence{{Skill: skillName, Result: result}}, summary, confidence)
}

func combinedSkillResult(agentName string, evidence []skillEvidence, summary string, confidence float64) *AgentResult {
	facts := make([]Fact, 0, len(evidence))
	refs := make([]string, 0, len(evidence))
	for _, item := range evidence {
		ref := skillEvidenceRef(item.Skill, item.Result.RunID)
		refs = append(refs, ref)
		facts = append(facts, Fact{
			Summary:     summarizeSkillOutput(item.Skill, item.Result.Output),
			EvidenceKey: ref,
		})
	}
	return &AgentResult{
		Summary:      summary,
		Facts:        facts,
		Hypotheses:   []Hypothesis{{Summary: fmt.Sprintf("%s produced %d evidence item(s)", agentName, len(evidence)), Confidence: confidence}},
		EvidenceRefs: refs,
		Confidence:   confidence,
	}
}

func needsScopeResult(agentName, message string) *AgentResult {
	return &AgentResult{
		Summary: message,
		Hypotheses: []Hypothesis{{
			Summary:    fmt.Sprintf("%s needs more scope before calling production data sources", agentName),
			Confidence: 0.3,
		}},
		Confidence: 0.3,
	}
}

func summarizeSkillOutput(skillName string, output json.RawMessage) string {
	if len(output) == 0 {
		return skillName + " returned no output"
	}
	var body map[string]any
	if err := json.Unmarshal(output, &body); err != nil {
		return skillName + " returned non-object output"
	}
	if partial, _ := body["partial"].(bool); partial {
		if errBody, ok := body["error"].(map[string]any); ok {
			if message, _ := errBody["message"].(string); message != "" {
				return fmt.Sprintf("%s returned partial error: %s", skillName, message)
			}
		}
		return skillName + " returned partial result"
	}
	if count, ok := numericValue(body["count"]); ok {
		return fmt.Sprintf("%s returned %.0f item(s)", skillName, count)
	}
	for _, key := range []string{"total", "errorCount"} {
		if value, ok := numericValue(body[key]); ok {
			return fmt.Sprintf("%s %s=%.0f", skillName, key, value)
		}
	}
	return skillName + " returned structured evidence"
}

func skillEvidenceRef(skillName string, runID int64) string {
	if runID > 0 {
		return fmt.Sprintf("skill:%s:%d", skillName, runID)
	}
	return fmt.Sprintf("skill:%s:%d", skillName, time.Now().UnixNano())
}

func baseTimeWindowPayload(input AgentContext, required ...string) (map[string]any, []string) {
	payload := map[string]any{}
	missing := []string{}
	for _, key := range required {
		value, ok := variable(input, key)
		if !ok || isZeroVariable(value) {
			missing = append(missing, key)
			continue
		}
		payload[key] = value
	}
	return payload, missing
}

func copyOptional(input AgentContext, payload map[string]any, keys ...string) {
	for _, key := range keys {
		value, ok := variable(input, key)
		if ok && !isZeroVariable(value) {
			payload[key] = value
		}
	}
}

func variable(input AgentContext, key string) (any, bool) {
	if input.Variables != nil {
		if value, ok := input.Variables[key]; ok {
			return value, true
		}
	}
	if input.Scope != nil {
		if value, ok := input.Scope[key]; ok {
			return value, true
		}
	}
	return nil, false
}

func stringVariable(input AgentContext, key string) string {
	value, ok := variable(input, key)
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func intVariable(input AgentContext, key string, fallback int) int {
	value, ok := variable(input, key)
	if !ok {
		return fallback
	}
	if number, ok := numericValue(value); ok {
		return int(number)
	}
	return fallback
}

func int64Variable(input AgentContext, key string) (int64, bool) {
	value, ok := variable(input, key)
	if !ok {
		return 0, false
	}
	if number, ok := numericValue(value); ok {
		return int64(number), int64(number) > 0
	}
	return 0, false
}

func numericValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case float64:
		return typed, true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func isZeroVariable(value any) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) == ""
	}
	return false
}
