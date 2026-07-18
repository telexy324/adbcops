package agentruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestLinuxServerAgentBuildsTypedEvidenceBackedReport(t *testing.T) {
	runtime := newSpecialistRuntime(t, LinuxServerAgent{},
		namedOutputSkill{name: "diagnose_linux_host_health", output: json.RawMessage(`{
			"partial":false,
			"status":"healthy",
			"facts":[
				{"type":"FACT","summary":"CPU evidence collected","evidenceRef":"linux.host.7.cpu","evidence":{"collector":"cpu","status":"success","data":{"cpu_usage_percent":92,"load_per_cpu":1.2}}},
				{"type":"FACT","summary":"Memory evidence collected","evidenceRef":"linux.host.7.memory","evidence":{"collector":"memory","status":"success","data":{"mem_total":1000,"mem_available":600,"swap_used_percent":0}}},
				{"type":"RULE","summary":"collector coverage is complete","evidenceRef":"linux.host.7.diagnosis","evidence":{"status":"healthy"}}
			],
			"missingEvidence":[]
		}`)},
		namedOutputSkill{name: "search_knowledge", output: json.RawMessage(`{"count":2,"chunks":[{"id":1},{"id":2}]}`)},
	)

	output, err := runtime.Run(context.Background(), RunInput{
		Actor: adminActor(), Name: "linux_server_agent",
		Context: AgentContext{
			UserID: 1, Query: "why is CPU high",
			Variables: map[string]any{
				"hostId":   float64(7),
				"topology": map[string]any{"nodeKey": "host:7", "services": []string{"payment-api"}},
			},
			Evidence: []Evidence{{Key: "ev-topology-7", Summary: "payment-api runs on host 7", Source: "topology"}},
		},
	})
	if err != nil {
		t.Fatalf("run Linux Server Agent: %v", err)
	}
	if output.Skills != 2 || output.RunID == 0 {
		t.Fatalf("agent run/skills not recorded: %+v", output)
	}
	assertEvidenceBacked(t, output.Result)
	var report linuxHostReport
	if err := json.Unmarshal(output.Result.Structured, &report); err != nil {
		t.Fatal(err)
	}
	if report.HealthLevel != "critical" || report.HealthScore != 30 {
		t.Fatalf("CPU rule did not produce critical health: %+v", report)
	}
	if !hasLinuxReportType(report.Facts, "FACT") || !hasLinuxReportType(report.RuleFindings, "RULE") || !hasLinuxReportType(report.Knowledge, "KNOWLEDGE") || !hasLinuxReportType(report.Hypotheses, "HYPOTHESIS") {
		t.Fatalf("report does not distinguish four result types: %+v", report)
	}
	if report.Topology == nil || report.ConfidenceExplanation.EvidenceCount == 0 || len(report.ConfidenceExplanation.Factors) == 0 {
		t.Fatalf("topology or confidence explanation missing: %+v", report)
	}
	for _, suggestion := range report.Suggestions {
		lower := strings.ToLower(suggestion)
		if strings.Contains(lower, "systemctl") || strings.Contains(lower, "sudo ") || strings.Contains(lower, "rm -") {
			t.Fatalf("suggestion contains executable action: %q", suggestion)
		}
	}
}

func TestLinuxServerAgentKeepsUnknownAndCapsConfidence(t *testing.T) {
	runtime := newSpecialistRuntime(t, LinuxServerAgent{}, namedOutputSkill{
		name: "diagnose_linux_host_health",
		output: json.RawMessage(`{
			"partial":true,
			"status":"unknown",
			"facts":[{"type":"FACT","summary":"connection failed; health remains unknown","evidenceRef":"linux.host.9.collection_failure","evidence":{"status":"unknown"}}],
			"missingEvidence":["linux SSH connection","cpu: missing commands: nproc"]
		}`),
	})
	output, err := runtime.Run(context.Background(), RunInput{
		Actor: adminActor(), Name: "linux_server_agent",
		Context: AgentContext{UserID: 1, Query: "host health", Variables: map[string]any{"hostId": float64(9), "includeKnowledge": false}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var report linuxHostReport
	if json.Unmarshal(output.Result.Structured, &report) != nil {
		t.Fatalf("invalid report: %s", output.Result.Structured)
	}
	if report.HealthLevel != "unknown" || report.HealthScore != 0 || report.Confidence > 0.45 || report.ConfidenceExplanation.MissingEvidenceCount < 2 {
		t.Fatalf("unknown health was normalized incorrectly: %+v", report)
	}
}

func TestLinuxServerAgentDoesNotPromoteUnreferencedConclusionToFact(t *testing.T) {
	runtime := newSpecialistRuntime(t, LinuxServerAgent{}, namedOutputSkill{
		name: "diagnose_linux_host_health",
		output: json.RawMessage(`{
			"partial":false,"status":"healthy",
			"facts":[{"type":"FACT","summary":"unsupported conclusion","evidence":{}}],
			"missingEvidence":[]
		}`),
	})
	output, err := runtime.Run(context.Background(), RunInput{
		Actor: adminActor(), Name: "linux_server_agent",
		Context: AgentContext{UserID: 1, Variables: map[string]any{"hostId": float64(10), "includeKnowledge": false}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var report linuxHostReport
	_ = json.Unmarshal(output.Result.Structured, &report)
	if len(report.Facts) != 0 || report.HealthLevel != "unknown" || len(report.Hypotheses) == 0 {
		t.Fatalf("unreferenced conclusion was promoted to FACT: %+v", report)
	}
}

func hasLinuxReportType(items []linuxReportItem, expected string) bool {
	for _, item := range items {
		if item.Type == expected {
			return true
		}
	}
	return false
}
