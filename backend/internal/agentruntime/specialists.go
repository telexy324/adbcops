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
