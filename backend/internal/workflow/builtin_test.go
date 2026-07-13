package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"aiops-platform/backend/internal/agentruntime"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/skillframework"
	"aiops-platform/backend/internal/toolregistry"
)

func TestBuiltinDefinitionsValidate(t *testing.T) {
	definitions := BuiltinDefinitions()
	if len(definitions) != 23 {
		t.Fatalf("builtin workflow count = %d, want 23", len(definitions))
	}
	assertBuiltinWorkflowNames(t, definitions, []string{
		"nacos_diagnosis_workflow",
		"nacos_registration_diagnosis_workflow",
		"nacos_config_delivery_diagnosis_workflow",
		"redis_diagnosis_workflow",
		"redis_memory_diagnosis_workflow",
		"redis_connection_pool_diagnosis_workflow",
		"redis_replication_diagnosis_workflow",
		"redis_cluster_diagnosis_workflow",
		"tidb_diagnosis_workflow",
		"tidb_performance_diagnosis_workflow",
		"tidb_connection_pressure_diagnosis_workflow",
		"tidb_lock_contention_diagnosis_workflow",
		"tidb_plan_regression_diagnosis_workflow",
		"nginx_diagnosis_workflow",
		"nginx_499_diagnosis_workflow",
		"nginx_502_diagnosis_workflow",
		"nginx_503_diagnosis_workflow",
		"nginx_504_diagnosis_workflow",
	})
	agents := builtinTestAgents{}
	skills := builtinTestSkills{}
	for _, definition := range definitions {
		result := Validate(definition, agents, skills)
		if !result.Valid {
			t.Fatalf("%s should validate, errors=%+v warnings=%+v", definition.Name, result.Errors, result.Warnings)
		}
	}
}

func assertBuiltinWorkflowNames(t *testing.T, definitions []Definition, expected []string) {
	t.Helper()
	names := map[string]bool{}
	for _, definition := range definitions {
		names[definition.Name] = true
	}
	for _, name := range expected {
		if !names[name] {
			t.Fatalf("missing builtin workflow %s in %+v", name, names)
		}
	}
}

func TestBuiltinDefinitionsValidateAgainstRegisteredCatalogs(t *testing.T) {
	skills := append(skillframework.BuiltinSkills(), skillframework.LogAndKnowledgeSkills(nil, nil)...)
	skills = append(skills, skillframework.K8sAndMetricsSkills(nil, nil)...)
	skillRegistry, err := skillframework.NewRegistry(toolregistry.NewBuiltinRegistry(), nil, skills...)
	if err != nil {
		t.Fatalf("skill registry: %v", err)
	}
	agentRuntime, err := agentruntime.NewRuntime(skillRegistry, nil, agentruntime.Limits{}, agentruntime.BuiltinAgents()...)
	if err != nil {
		t.Fatalf("agent runtime: %v", err)
	}
	for _, definition := range BuiltinDefinitions() {
		result := Validate(definition, agentRuntime, skillRegistry)
		if !result.Valid {
			t.Fatalf("%s should validate against registered catalogs, errors=%+v", definition.Name, result.Errors)
		}
	}
}

func TestBuiltinWorkflowProducesCompleteRunRecord(t *testing.T) {
	definition := BuiltinDefinitions()[0]
	repo := newExecutorMemoryRepo(definition)
	executor := NewExecutor(repo, builtinTestAgents{}, builtinTestSkills{}, time.Second)

	run, err := executor.Run(context.Background(), ExecutorInput{
		Actor:      adminWorkflowActor(),
		WorkflowID: 1,
		Input:      json.RawMessage(`{"query":"how to troubleshoot timeout"}`),
	})
	if err != nil {
		t.Fatalf("run builtin workflow: %v", err)
	}
	if run.Status != model.WorkflowRunStatusSuccess || len(run.NodeRuns) != len(definition.Nodes) {
		t.Fatalf("unexpected run record: status=%s nodes=%d want=%d", run.Status, len(run.NodeRuns), len(definition.Nodes))
	}
	for _, node := range run.NodeRuns {
		if node.Status == "" || node.StartedAt == nil || node.FinishedAt == nil {
			t.Fatalf("node run is incomplete: %+v", node)
		}
	}
}

type builtinTestAgents struct{}

func (builtinTestAgents) Get(name string) (agentruntime.AgentDefinition, error) {
	switch name {
	case "coordinator_agent", "knowledge_agent", "log_agent", "metrics_agent", "kubernetes_agent", "echo_agent":
		return agentruntime.AgentDefinition{Name: name, Enabled: true}, nil
	default:
		return agentruntime.AgentDefinition{}, errors.New("agent not found")
	}
}

func (builtinTestAgents) Run(context.Context, agentruntime.RunInput) (*agentruntime.RunOutput, error) {
	return &agentruntime.RunOutput{Result: &agentruntime.AgentResult{Summary: "agent ok", Confidence: 0.8}}, nil
}

type builtinTestSkills struct{}

func (builtinTestSkills) Get(name string) (skillframework.SkillDefinition, error) {
	switch name {
	case "search_knowledge",
		"query_logs",
		"aggregate_log_templates",
		"extract_log_entities",
		"get_pod_context",
		"get_ingress_context",
		"run_k8s_diagnostic_rules",
		"query_metrics",
		"compare_metric_baseline",
		"echo_safe":
		return skillframework.SkillDefinition{Name: name, Enabled: true}, nil
	default:
		for _, skill := range skillframework.ComponentDiagnosisSkills() {
			if skill.Definition().Name == name {
				return skillframework.SkillDefinition{Name: name, Enabled: true}, nil
			}
		}
		return skillframework.SkillDefinition{}, errors.New("skill not found")
	}
}

func (builtinTestSkills) Execute(_ context.Context, input skillframework.ExecuteInput) (*skillframework.ExecuteResult, error) {
	return &skillframework.ExecuteResult{SkillName: input.Name, Output: json.RawMessage(`{"ok":true}`)}, nil
}
