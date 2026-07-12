package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/skillframework"
	"aiops-platform/backend/internal/toolregistry"
)

func TestBuiltinAgentsIncludeSpecialists(t *testing.T) {
	runtime, err := NewRuntime(nil, nil, Limits{}, BuiltinAgents()...)
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	names := map[string]bool{}
	for _, definition := range runtime.List() {
		names[definition.Name] = true
	}
	for _, name := range []string{"knowledge_agent", "log_agent", "metrics_agent", "kubernetes_agent", "change_agent"} {
		if !names[name] {
			t.Fatalf("missing builtin agent %s in %+v", name, names)
		}
	}
}

func TestKnowledgeAgentReturnsEvidenceBackedResult(t *testing.T) {
	runtime := newSpecialistRuntime(t, KnowledgeAgent{}, namedOutputSkill{
		name:   "search_knowledge",
		output: json.RawMessage(`{"count":1,"chunks":[{"id":1,"content":"restart policy guide"}]}`),
	})

	output, err := runtime.Run(context.Background(), RunInput{
		Actor: adminActor(),
		Name:  "knowledge_agent",
		Context: AgentContext{
			UserID: 1,
			Query:  "pod restart troubleshooting",
		},
	})
	if err != nil {
		t.Fatalf("run knowledge agent: %v", err)
	}
	assertEvidenceBacked(t, output.Result)
	if output.Skills != 1 {
		t.Fatalf("skills = %d, want 1", output.Skills)
	}
}

func TestKubernetesAgentReturnsFactsFromTwoSkills(t *testing.T) {
	runtime := newSpecialistRuntime(t, KubernetesAgent{},
		namedOutputSkill{name: "get_pod_context", output: json.RawMessage(`{"partial":false,"podContext":{"pod":{"name":"payment-api-0"}}}`)},
		namedOutputSkill{name: "run_k8s_diagnostic_rules", output: json.RawMessage(`{"partial":false,"rules":[{"name":"restart","severity":"warning"}]}`)},
	)

	output, err := runtime.Run(context.Background(), RunInput{
		Actor: adminActor(),
		Name:  "kubernetes_agent",
		Context: AgentContext{
			UserID: 1,
			Query:  "pod restarting",
			Variables: map[string]any{
				"dataSourceId": float64(2),
				"namespace":    "prod",
				"podName":      "payment-api-0",
			},
		},
	})
	if err != nil {
		t.Fatalf("run kubernetes agent: %v", err)
	}
	assertEvidenceBacked(t, output.Result)
	if len(output.Result.Facts) != 2 || output.Skills != 2 {
		t.Fatalf("unexpected output: %+v", output)
	}
}

func TestSpecialistMissingScopeDoesNotCallProductionSkill(t *testing.T) {
	runtime := newSpecialistRuntime(t, MetricsAgent{}, namedOutputSkill{
		name:   "query_metrics",
		output: json.RawMessage(`{"partial":false}`),
	})

	output, err := runtime.Run(context.Background(), RunInput{
		Actor:   adminActor(),
		Name:    "metrics_agent",
		Context: AgentContext{UserID: 1, Query: "cpu high"},
	})
	if err != nil {
		t.Fatalf("run metrics agent: %v", err)
	}
	if output.Skills != 0 || len(output.Result.Hypotheses) == 0 {
		t.Fatalf("expected missing-scope hypothesis without skill call, got %+v", output)
	}
}

func TestChangeAgentCallsRecentChangesSkillWithScope(t *testing.T) {
	runtime := newSpecialistRuntime(t, ChangeAgent{}, namedOutputSkill{
		name:   "query_recent_changes",
		output: json.RawMessage(`{"partial":true,"changes":{"count":1,"partial":true}}`),
	})

	output, err := runtime.Run(context.Background(), RunInput{
		Actor: adminActor(),
		Name:  "change_agent",
		Context: AgentContext{
			UserID: 1,
			Variables: map[string]any{
				"dataSourceId": float64(5),
			},
		},
	})
	if err != nil {
		t.Fatalf("run change agent: %v", err)
	}
	assertEvidenceBacked(t, output.Result)
	if output.Skills != 1 {
		t.Fatalf("skills = %d, want 1", output.Skills)
	}
}

func TestRunRejectsMissingEvidenceReference(t *testing.T) {
	runtime := newSpecialistRuntime(t, badEvidenceAgent{})

	_, err := runtime.Run(context.Background(), RunInput{
		Actor:   adminActor(),
		Name:    "bad_evidence_agent",
		Context: AgentContext{UserID: 1, Query: "bad evidence"},
	})
	if !errors.Is(err, ErrEvidenceRefMissing) {
		t.Fatalf("expected ErrEvidenceRefMissing, got %v", err)
	}
}

func assertEvidenceBacked(t *testing.T, result *AgentResult) {
	t.Helper()
	if result == nil || len(result.Facts) == 0 || len(result.Hypotheses) == 0 || len(result.EvidenceRefs) == 0 {
		t.Fatalf("expected fact/hypothesis/evidence refs, got %+v", result)
	}
	refs := map[string]bool{}
	for _, ref := range result.EvidenceRefs {
		refs[ref] = true
	}
	for _, fact := range result.Facts {
		if fact.EvidenceKey == "" || !refs[fact.EvidenceKey] {
			t.Fatalf("fact is not backed by evidence ref: %+v refs=%+v", fact, refs)
		}
	}
}

func newSpecialistRuntime(t *testing.T, agent Agent, skills ...skillframework.Skill) *Runtime {
	t.Helper()
	registry, err := skillframework.NewRegistry(toolregistry.NewBuiltinRegistry(), newSkillMemoryAudit(), skills...)
	if err != nil {
		t.Fatalf("skill registry: %v", err)
	}
	runtime, err := NewRuntime(registry, newAgentMemoryAudit(), Limits{}, agent)
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	return runtime
}

type namedOutputSkill struct {
	name   string
	output json.RawMessage
}

func (s namedOutputSkill) Definition() skillframework.SkillDefinition {
	return skillframework.SkillDefinition{
		Name:          s.name,
		Version:       "v1",
		Description:   "specialist test skill",
		InputSchema:   json.RawMessage(`{"type":"object"}`),
		OutputSchema:  json.RawMessage(`{"type":"object"}`),
		RiskLevel:     model.SkillRiskSafeRead,
		ReadOnly:      true,
		TimeoutSecond: 5,
	}
}

func (s namedOutputSkill) Execute(context.Context, json.RawMessage) (json.RawMessage, error) {
	return s.output, nil
}

type badEvidenceAgent struct{}

func (badEvidenceAgent) Name() string {
	return "bad_evidence_agent"
}

func (badEvidenceAgent) Description() string {
	return "returns a fact with a missing evidence reference"
}

func (badEvidenceAgent) Analyze(context.Context, AgentContext, *RunContext) (*AgentResult, error) {
	return &AgentResult{
		Summary: "bad",
		Facts: []Fact{{
			Summary:     "this fact points to a missing reference",
			EvidenceKey: "missing-ref",
		}},
		Confidence: 0.1,
	}, nil
}
