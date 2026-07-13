package agentruntime

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"aiops-platform/backend/internal/skillframework"
)

const (
	IntentKnowledge       = "knowledge"
	IntentLogAnalysis     = "log_analysis"
	IntentMetricsAnalysis = "metrics_analysis"
	IntentK8sDiagnosis    = "k8s_diagnosis"
	IntentAlertAnalysis   = "alert_analysis"
	IntentNacosDiagnosis  = "nacos_diagnosis"
	IntentRedisDiagnosis  = "redis_diagnosis"
	IntentTiDBDiagnosis   = "tidb_diagnosis"
	IntentNginxDiagnosis  = "nginx_diagnosis"
	IntentGeneralRCA      = "general_rca"

	WorkflowKnowledgeQA     = "knowledge_qa_workflow"
	WorkflowLogAnalysis     = "log_analysis_workflow"
	WorkflowMetricsAnalysis = "metrics_analysis_workflow"
	WorkflowK8sDiagnosis    = "k8s_diagnosis_workflow"
	WorkflowAlertDiagnosis  = "alert_diagnosis_workflow"
	WorkflowNacosDiagnosis  = "nacos_diagnosis_workflow"
	WorkflowRedisDiagnosis  = "redis_diagnosis_workflow"
	WorkflowTiDBDiagnosis   = "tidb_diagnosis_workflow"
	WorkflowNginxDiagnosis  = "nginx_diagnosis_workflow"
	WorkflowGeneralRCA      = "general_rca_workflow"
)

type CoordinatorAgent struct{}

type CoordinatorPlan struct {
	Intent            string         `json:"intent"`
	Scope             map[string]any `json:"scope"`
	Workflow          string         `json:"workflow"`
	Agents            []string       `json:"agents"`
	Reason            string         `json:"reason"`
	MissingParameters []string       `json:"missingParameters"`
}

func (CoordinatorAgent) Name() string {
	return "coordinator_agent"
}

func (CoordinatorAgent) Description() string {
	return "Classifies intent, extracts analysis scope, and selects read-only workflow and specialist agents."
}

func (CoordinatorAgent) Analyze(_ context.Context, input AgentContext, runtime *RunContext) (*AgentResult, error) {
	if err := runtime.Step("classify intent and select workflow"); err != nil {
		return nil, err
	}
	plan := BuildCoordinatorPlan(input)
	raw, err := json.Marshal(plan)
	if err != nil {
		return nil, ErrInvalidInput
	}
	if err := skillframework.ValidateJSONSchema(CoordinatorPlanSchema(), raw); err != nil {
		return nil, err
	}
	return &AgentResult{
		Summary:    "selected " + plan.Workflow + " for " + plan.Intent,
		Facts:      []Fact{{Summary: "coordinator selected agents: " + strings.Join(plan.Agents, ", ")}},
		Hypotheses: []Hypothesis{{Summary: plan.Reason, Confidence: coordinatorConfidence(plan)}},
		Structured: raw,
		Confidence: coordinatorConfidence(plan),
	}, nil
}

func BuildCoordinatorPlan(input AgentContext) CoordinatorPlan {
	scope := extractCoordinatorScope(input)
	intent := classifyIntent(input.Query, scope)
	workflow, agents := selectWorkflowAndAgents(intent)
	missing := missingParameters(intent, scope)
	return CoordinatorPlan{
		Intent:            intent,
		Scope:             scope,
		Workflow:          workflow,
		Agents:            agents,
		Reason:            coordinatorReason(intent, workflow, missing),
		MissingParameters: missing,
	}
}

func CoordinatorPlanSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","required":["intent","scope","workflow","agents","reason","missingParameters"],"properties":{"intent":{"type":"string"},"scope":{"type":"object"},"workflow":{"type":"string"},"agents":{"type":"array","items":{"type":"string"}},"reason":{"type":"string"},"missingParameters":{"type":"array","items":{"type":"string"}}}}`)
}

func extractCoordinatorScope(input AgentContext) map[string]any {
	scope := map[string]any{}
	for key, value := range input.Scope {
		if !isZeroVariable(value) {
			scope[key] = value
		}
	}
	for key, value := range input.Variables {
		if !isZeroVariable(value) {
			scope[key] = value
		}
	}
	query := input.Query
	extractTextValue(scope, query, "namespace", regexp.MustCompile(`(?i)(?:namespace|ns|命名空间)\s*[:=：]?\s*([a-z0-9][a-z0-9._-]*)`))
	extractTextValue(scope, query, "group", regexp.MustCompile(`(?i)(?:group|分组)\s*[:=：]?\s*([a-z0-9][a-z0-9._-]*)`))
	extractTextValue(scope, query, "podName", regexp.MustCompile(`(?i)(?:pod|podName)\s*[:=：]?\s*([a-z0-9][a-z0-9._-]*)`))
	extractTextValue(scope, query, "component", regexp.MustCompile(`(?i)(?:component|service|svc|组件|服务)\s*[:=：]?\s*([a-z0-9][a-z0-9._-]*)`))
	extractTextValue(scope, query, "environment", regexp.MustCompile(`(?i)(?:env|environment|环境)\s*[:=：]?\s*([a-z0-9][a-z0-9._-]*)`))
	extractTextValue(scope, query, "serviceName", regexp.MustCompile(`(?i)(?:serviceName|服务名|service)\s*[:=：]?\s*([a-z0-9][a-z0-9._-]*)`))
	extractTextValue(scope, query, "dataId", regexp.MustCompile(`(?i)(?:dataId|data-id|配置)\s*[:=：]?\s*([a-z0-9][a-z0-9._/-]*)`))
	extractTextValue(scope, query, "statusCode", regexp.MustCompile(`(?i)\b(499|502|503|504)\b`))
	if strings.Contains(strings.ToLower(query), "ingress") || strings.Contains(query, "入口") {
		scope["resourceKind"] = "ingress"
	}
	return scope
}

func extractTextValue(scope map[string]any, text, key string, pattern *regexp.Regexp) {
	if _, exists := scope[key]; exists {
		return
	}
	matches := pattern.FindStringSubmatch(text)
	if len(matches) == 2 && strings.TrimSpace(matches[1]) != "" {
		scope[key] = strings.TrimSpace(matches[1])
	}
}

func classifyIntent(query string, scope map[string]any) string {
	text := strings.ToLower(query)
	switch {
	case hasAny(text, "nacos", "服务注册", "注册中心", "配置推送", "配置监听"):
		return IntentNacosDiagnosis
	case hasAny(text, "redis", "sentinel", "cluster slots", "slowlog", "缓存", "主从", "集群槽", "连接池") ||
		(hasAny(text, "内存", "memory") && hasAny(text, "缓存", "keyspace")):
		return IntentRedisDiagnosis
	case hasAny(text, "tidb", "tikv", "pd ", "慢 sql", "slow sql", "processlist", "锁等待", "执行计划", "热点 region", "hot region"):
		return IntentTiDBDiagnosis
	case hasAny(text, "nginx", "upstream", "网关", "反向代理", "499", "502", "503", "504"):
		return IntentNginxDiagnosis
	case hasAny(text, "k8s", "kubernetes", "pod", "namespace", "ingress", "deployment", "container", "容器", "命名空间", "重启", "驱逐"):
		return IntentK8sDiagnosis
	case hasAny(text, "alert", "告警", "报警", "alertmanager"):
		return IntentAlertAnalysis
	case hasAny(text, "log", "日志", "报错", "error", "exception", "traceid", "requestid"):
		return IntentLogAnalysis
	case hasAny(text, "metric", "metrics", "prometheus", "cpu", "memory", "latency", "qps", "指标", "延迟", "内存"):
		return IntentMetricsAnalysis
	case hasProductionScope(scope):
		return IntentGeneralRCA
	default:
		return IntentKnowledge
	}
}

func selectWorkflowAndAgents(intent string) (string, []string) {
	switch intent {
	case IntentK8sDiagnosis:
		return WorkflowK8sDiagnosis, []string{"kubernetes_agent"}
	case IntentLogAnalysis:
		return WorkflowLogAnalysis, []string{"log_agent"}
	case IntentMetricsAnalysis:
		return WorkflowMetricsAnalysis, []string{"metrics_agent"}
	case IntentAlertAnalysis:
		return WorkflowAlertDiagnosis, []string{"log_agent", "metrics_agent", "kubernetes_agent"}
	case IntentNacosDiagnosis:
		return WorkflowNacosDiagnosis, []string{"knowledge_agent", "log_agent"}
	case IntentRedisDiagnosis:
		return WorkflowRedisDiagnosis, []string{"knowledge_agent", "metrics_agent", "log_agent"}
	case IntentTiDBDiagnosis:
		return WorkflowTiDBDiagnosis, []string{"knowledge_agent", "metrics_agent", "log_agent"}
	case IntentNginxDiagnosis:
		return WorkflowNginxDiagnosis, []string{"knowledge_agent", "log_agent", "metrics_agent", "kubernetes_agent"}
	case IntentGeneralRCA:
		return WorkflowGeneralRCA, []string{"knowledge_agent", "log_agent", "metrics_agent", "kubernetes_agent"}
	default:
		return WorkflowKnowledgeQA, []string{"knowledge_agent"}
	}
}

func missingParameters(intent string, scope map[string]any) []string {
	missing := []string{}
	require := func(keys ...string) {
		for _, key := range keys {
			if value, ok := scope[key]; !ok || isZeroVariable(value) {
				missing = append(missing, key)
			}
		}
	}
	switch intent {
	case IntentK8sDiagnosis:
		require("dataSourceId", "namespace")
		if _, ok := scope["podName"]; !ok && scope["resourceKind"] != "ingress" {
			missing = append(missing, "podName")
		}
	case IntentLogAnalysis:
		require("dataSourceId", "from", "to")
	case IntentMetricsAnalysis:
		require("dataSourceId", "query")
	case IntentAlertAnalysis, IntentGeneralRCA:
		if _, ok := scope["dataSourceId"]; !ok {
			missing = append(missing, "dataSourceId")
		}
	case IntentNacosDiagnosis:
		require("dataSourceId")
		if _, ok := scope["namespace"]; !ok {
			missing = append(missing, "namespace")
		}
	case IntentRedisDiagnosis, IntentTiDBDiagnosis, IntentNginxDiagnosis:
		require("dataSourceId")
	}
	return missing
}

func coordinatorReason(intent, workflow string, missing []string) string {
	reason := "intent " + intent + " maps to read-only workflow " + workflow
	if len(missing) > 0 {
		reason += "; missing " + strings.Join(missing, ", ")
	}
	return reason
}

func coordinatorConfidence(plan CoordinatorPlan) float64 {
	if len(plan.MissingParameters) > 0 {
		return 0.55
	}
	if plan.Intent == IntentKnowledge {
		return 0.75
	}
	return 0.7
}

func hasAny(text string, keywords ...string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func hasProductionScope(scope map[string]any) bool {
	for _, key := range []string{"dataSourceId", "namespace", "podName", "from", "to", "promql", "query"} {
		if value, ok := scope[key]; ok && !isZeroVariable(value) {
			return true
		}
	}
	return false
}
