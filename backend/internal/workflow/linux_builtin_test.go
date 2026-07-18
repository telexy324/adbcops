package workflow

import (
	"strings"
	"testing"
)

func TestLinuxBuiltinWorkflowsValidateAndExposeParallelCollectors(t *testing.T) {
	definitions := BuiltinDefinitions()
	for _, name := range []string{
		"linux_basic_host_diagnosis_workflow",
		"linux_cpu_diagnosis_workflow",
		"linux_memory_diagnosis_workflow",
		"linux_disk_diagnosis_workflow",
		"linux_network_diagnosis_workflow",
		"linux_batch_health_workflow",
	} {
		definition := workflowByName(t, definitions, name)
		result := Validate(definition, builtinTestAgents{}, builtinTestSkills{})
		if !result.Valid {
			t.Fatalf("%s invalid: %+v", name, result.Errors)
		}
	}

	basic := workflowByName(t, definitions, "linux_basic_host_diagnosis_workflow")
	parallel := 0
	mergeParents := 0
	for _, edge := range basic.Edges {
		if edge.From == "load_host_profile" && strings.HasPrefix(edge.To, "collect_") {
			parallel++
		}
		if edge.To == "merge_collectors" && strings.HasPrefix(edge.From, "collect_") {
			mergeParents++
		}
	}
	if parallel != 9 || mergeParents != 9 {
		t.Fatalf("basic Linux collectors are not a nine-way parallel fan-out/fan-in: parallel=%d merge=%d", parallel, mergeParents)
	}
}

func TestLinuxBuiltinWorkflowsContainNoCommandExecutionConfig(t *testing.T) {
	for _, definition := range BuiltinDefinitions() {
		if !strings.HasPrefix(definition.Name, "linux_") {
			continue
		}
		for _, node := range definition.Nodes {
			config := strings.ToLower(string(node.Config))
			if strings.Contains(config, "command") || strings.Contains(config, "argv") || strings.Contains(config, "executable") {
				t.Fatalf("%s node %s exposes command execution: %s", definition.Name, node.ID, node.Config)
			}
		}
	}
}

func workflowByName(t *testing.T, definitions []Definition, name string) Definition {
	t.Helper()
	for _, definition := range definitions {
		if definition.Name == name {
			return definition
		}
	}
	t.Fatalf("workflow %s not found", name)
	return Definition{}
}
