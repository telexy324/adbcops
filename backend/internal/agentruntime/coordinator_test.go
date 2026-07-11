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
