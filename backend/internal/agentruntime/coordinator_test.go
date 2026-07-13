package agentruntime

import (
	"context"
	"encoding/json"
	"testing"

	"aiops-platform/backend/internal/skillframework"
)

func TestCoordinatorKnowledgeQuestionDoesNotCallProductionDataSource(t *testing.T) {
	runtime, err := NewRuntime(nil, newAgentMemoryAudit(), Limits{}, CoordinatorAgent{})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}

	output, err := runtime.Run(context.Background(), RunInput{
		Actor:   adminActor(),
		Name:    "coordinator_agent",
		Context: AgentContext{UserID: 1, Query: "如何排查 Java 线程池耗尽？"},
	})
	if err != nil {
		t.Fatalf("run coordinator: %v", err)
	}
	plan := decodeCoordinatorPlan(t, output.Result.Structured)
	if output.Skills != 0 {
		t.Fatalf("coordinator should not call skills for knowledge planning, skills=%d", output.Skills)
	}
	if plan.Intent != IntentKnowledge || plan.Workflow != WorkflowKnowledgeQA {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if len(plan.Agents) != 1 || plan.Agents[0] != "knowledge_agent" {
		t.Fatalf("knowledge question should only select knowledge_agent, got %+v", plan.Agents)
	}
}

func TestCoordinatorK8sQuestionSelectsK8sWorkflow(t *testing.T) {
	runtime, err := NewRuntime(nil, newAgentMemoryAudit(), Limits{}, CoordinatorAgent{})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}

	output, err := runtime.Run(context.Background(), RunInput{
		Actor: adminActor(),
		Name:  "coordinator_agent",
		Context: AgentContext{
			UserID: 1,
			Query:  "prod namespace payment pod payment-api-0 一直重启，帮我诊断",
			Variables: map[string]any{
				"dataSourceId": float64(7),
			},
		},
	})
	if err != nil {
		t.Fatalf("run coordinator: %v", err)
	}
	plan := decodeCoordinatorPlan(t, output.Result.Structured)
	if plan.Intent != IntentK8sDiagnosis || plan.Workflow != WorkflowK8sDiagnosis {
		t.Fatalf("expected k8s workflow, got %+v", plan)
	}
	if len(plan.Agents) != 1 || plan.Agents[0] != "kubernetes_agent" {
		t.Fatalf("expected kubernetes_agent, got %+v", plan.Agents)
	}
	if plan.Scope["namespace"] == "" || plan.Scope["podName"] == "" {
		t.Fatalf("expected namespace and podName extracted, got %+v", plan.Scope)
	}
}

func TestCoordinatorComponentQuestionsSelectBuiltinComponentWorkflows(t *testing.T) {
	cases := []struct {
		name     string
		query    string
		intent   string
		workflow string
	}{
		{
			name:     "nacos",
			query:    "Nacos namespace prod group DEFAULT_GROUP 服务注册异常，serviceName payment-api",
			intent:   IntentNacosDiagnosis,
			workflow: WorkflowNacosDiagnosis,
		},
		{
			name:     "redis",
			query:    "Redis cluster 内存高并且连接池打满，帮我诊断",
			intent:   IntentRedisDiagnosis,
			workflow: WorkflowRedisDiagnosis,
		},
		{
			name:     "tidb",
			query:    "TiDB 慢 SQL 和锁等待变多，检查执行计划回退",
			intent:   IntentTiDBDiagnosis,
			workflow: WorkflowTiDBDiagnosis,
		},
		{
			name:     "nginx",
			query:    "Nginx 504 upstream timed out，分析网关和上游状态",
			intent:   IntentNginxDiagnosis,
			workflow: WorkflowNginxDiagnosis,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			plan := BuildCoordinatorPlan(AgentContext{
				UserID: 1,
				Query:  tt.query,
				Scope:  map[string]any{"dataSourceId": float64(9)},
			})
			if plan.Intent != tt.intent || plan.Workflow != tt.workflow {
				t.Fatalf("unexpected plan for %s: %+v", tt.name, plan)
			}
			if len(plan.Agents) == 0 {
				t.Fatalf("expected component plan to select support agents: %+v", plan)
			}
		})
	}
}

func TestCoordinatorNamespaceOnlyStillSelectsK8sWorkflow(t *testing.T) {
	plan := BuildCoordinatorPlan(AgentContext{
		UserID: 1,
		Query:  "namespace prod pod payment-api-0 crashloop",
		Scope:  map[string]any{"dataSourceId": float64(7)},
	})
	if plan.Intent != IntentK8sDiagnosis || plan.Workflow != WorkflowK8sDiagnosis {
		t.Fatalf("namespace-only k8s query should not be classified as nacos: %+v", plan)
	}
}

func TestCoordinatorPlanMatchesJSONSchema(t *testing.T) {
	plan := BuildCoordinatorPlan(AgentContext{
		UserID: 1,
		Query:  "pod payment-api-0 in namespace prod is crashlooping",
		Scope:  map[string]any{"dataSourceId": float64(1)},
	})
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	if err := skillframework.ValidateJSONSchema(CoordinatorPlanSchema(), raw); err != nil {
		t.Fatalf("schema validation failed: %v raw=%s", err, raw)
	}
}

func decodeCoordinatorPlan(t *testing.T, raw json.RawMessage) CoordinatorPlan {
	t.Helper()
	var plan CoordinatorPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatalf("decode plan: %v raw=%s", err, raw)
	}
	return plan
}
