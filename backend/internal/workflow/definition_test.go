package workflow

import (
	"errors"
	"testing"

	"aiops-platform/backend/internal/agentruntime"
	"aiops-platform/backend/internal/skillframework"
)

func TestValidateAcceptsValidDAG(t *testing.T) {
	result := Validate(validDefinition(), fakeAgentCatalog{"knowledge_agent": true}, fakeSkillCatalog{"search_knowledge": true})
	if !result.Valid {
		t.Fatalf("expected valid graph, got errors=%+v warnings=%+v", result.Errors, result.Warnings)
	}
}

func TestValidateRejectsCycle(t *testing.T) {
	definition := validDefinition()
	definition.Edges = append(definition.Edges, Edge{From: "agent", To: "skill"})
	definition.Edges = append(definition.Edges, Edge{From: "skill", To: "agent"})
	result := Validate(definition, fakeAgentCatalog{"knowledge_agent": true}, fakeSkillCatalog{"search_knowledge": true})
	if result.Valid || !containsError(result.Errors, "DAG") {
		t.Fatalf("expected DAG error, got %+v", result)
	}
}

func TestValidateRejectsIsolatedNode(t *testing.T) {
	definition := validDefinition()
	definition.Nodes = append(definition.Nodes, Node{ID: "isolated", Type: NodeTypeAgent, AgentName: "knowledge_agent"})
	result := Validate(definition, fakeAgentCatalog{"knowledge_agent": true}, fakeSkillCatalog{"search_knowledge": true})
	if result.Valid || !containsError(result.Errors, "no incoming") {
		t.Fatalf("expected isolated node error, got %+v", result)
	}
}

func TestValidateRejectsUnknownAgentAndSkill(t *testing.T) {
	result := Validate(validDefinition(), fakeAgentCatalog{}, fakeSkillCatalog{})
	if result.Valid || !containsError(result.Errors, "unknown agent") || !containsError(result.Errors, "unknown skill") {
		t.Fatalf("expected unknown reference errors, got %+v", result)
	}
}

func TestValidateRejectsOversizedWorkflow(t *testing.T) {
	definition := validDefinition()
	for index := 0; index < maxWorkflowNodes; index++ {
		definition.Nodes = append(definition.Nodes, Node{ID: "extra" + string(rune(index+65)), Type: NodeTypeMerge})
	}
	result := Validate(definition, fakeAgentCatalog{"knowledge_agent": true}, fakeSkillCatalog{"search_knowledge": true})
	if result.Valid || !containsError(result.Errors, "node limit exceeded") {
		t.Fatalf("expected node limit error, got %+v", result)
	}
}

func validDefinition() Definition {
	return Definition{
		Name:    "knowledge_qa_workflow",
		Version: "v1",
		Nodes: []Node{
			{ID: "start", Type: NodeTypeStart},
			{ID: "agent", Type: NodeTypeAgent, AgentName: "knowledge_agent"},
			{ID: "skill", Type: NodeTypeSkill, SkillName: "search_knowledge"},
			{ID: "end", Type: NodeTypeEnd},
		},
		Edges: []Edge{
			{From: "start", To: "agent"},
			{From: "agent", To: "skill"},
			{From: "skill", To: "end"},
		},
	}
}

func containsError(errorsList []string, fragment string) bool {
	for _, message := range errorsList {
		if len(message) >= len(fragment) {
			for index := 0; index+len(fragment) <= len(message); index++ {
				if message[index:index+len(fragment)] == fragment {
					return true
				}
			}
		}
	}
	return false
}

type fakeAgentCatalog map[string]bool

func (c fakeAgentCatalog) Get(name string) (agentruntime.AgentDefinition, error) {
	if c[name] {
		return agentruntime.AgentDefinition{Name: name, Enabled: true}, nil
	}
	return agentruntime.AgentDefinition{}, errors.New("not found")
}

type fakeSkillCatalog map[string]bool

func (c fakeSkillCatalog) Get(name string) (skillframework.SkillDefinition, error) {
	if c[name] {
		return skillframework.SkillDefinition{Name: name, Enabled: true}, nil
	}
	return skillframework.SkillDefinition{}, errors.New("not found")
}
