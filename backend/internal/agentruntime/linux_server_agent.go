package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

type LinuxServerAgent struct{}

func (LinuxServerAgent) Name() string { return "linux_server_agent" }

func (LinuxServerAgent) Description() string {
	return "Builds an evidence-backed Linux host health report from read-only Skills, rules, knowledge and topology context."
}

func (LinuxServerAgent) Analyze(ctx context.Context, input AgentContext, runtime *RunContext) (*AgentResult, error) {
	hostID, ok := int64Variable(input, "hostId")
	if !ok || hostID <= 0 {
		return needsScopeResult("linux_server_agent", "missing Linux host scope: hostId"), nil
	}
	if err := runtime.Step("run Linux host diagnosis Skill"); err != nil {
		return nil, err
	}
	payload := map[string]any{"hostId": hostID}
	copyOptional(input, payload, "topN", "sinceHours", "service")
	diagnosis, err := executeJSONSkill(ctx, runtime, "diagnose_linux_host_health", payload)
	if err != nil {
		return nil, err
	}
	report := buildLinuxHostReport(hostID, input, diagnosis)

	includeKnowledge := boolVariableDefault(input, "includeKnowledge", true)
	if includeKnowledge {
		if err := runtime.Step("search Linux operations knowledge"); err != nil {
			return nil, err
		}
		query := firstNonEmpty(strings.TrimSpace(input.Query), "Linux host health diagnosis")
		knowledge, knowledgeErr := executeJSONSkill(ctx, runtime, "search_knowledge", map[string]any{"query": query, "limit": intVariable(input, "knowledgeLimit", 5)})
		if knowledgeErr != nil {
			report.MissingEvidence = append(report.MissingEvidence, "knowledge search unavailable")
		} else {
			appendLinuxKnowledge(&report, knowledge)
		}
	}
	appendLinuxTopology(&report, input)
	report.finalize()

	structured, _ := json.Marshal(report)
	facts := make([]Fact, 0, len(report.Facts))
	for _, item := range report.Facts {
		if item.EvidenceRef != "" {
			facts = append(facts, Fact{Summary: item.Summary, EvidenceKey: item.EvidenceRef})
		}
	}
	hypotheses := make([]Hypothesis, 0, len(report.Hypotheses))
	for _, item := range report.Hypotheses {
		hypotheses = append(hypotheses, Hypothesis{Summary: item.Summary, Confidence: item.Confidence})
	}
	return &AgentResult{
		Summary: fmt.Sprintf("Linux host %d health is %s (score %d)", hostID, report.HealthLevel, report.HealthScore),
		Facts:   facts, Hypotheses: hypotheses, EvidenceRefs: report.EvidenceRefs,
		Structured: structured, Confidence: report.Confidence,
	}, nil
}

type linuxReportItem struct {
	Type        string  `json:"type"`
	Summary     string  `json:"summary"`
	EvidenceRef string  `json:"evidenceRef,omitempty"`
	Severity    string  `json:"severity,omitempty"`
	Confidence  float64 `json:"confidence,omitempty"`
}

type linuxConfidenceExplanation struct {
	EvidenceCount        int      `json:"evidenceCount"`
	MissingEvidenceCount int      `json:"missingEvidenceCount"`
	Partial              bool     `json:"partial"`
	Factors              []string `json:"factors"`
}

type linuxHostReport struct {
	Host                  map[string]any             `json:"host"`
	HealthScore           int                        `json:"healthScore"`
	HealthLevel           string                     `json:"healthLevel"`
	Facts                 []linuxReportItem          `json:"facts"`
	RuleFindings          []linuxReportItem          `json:"ruleFindings"`
	Knowledge             []linuxReportItem          `json:"knowledge"`
	Hypotheses            []linuxReportItem          `json:"hypotheses"`
	ResourceSummary       map[string]any             `json:"resourceSummary"`
	Topology              any                        `json:"topology,omitempty"`
	EvidenceRefs          []string                   `json:"evidenceRefs"`
	MissingEvidence       []string                   `json:"missingEvidence"`
	Suggestions           []string                   `json:"suggestions"`
	RiskTips              []string                   `json:"riskTips"`
	Confidence            float64                    `json:"confidence"`
	ConfidenceExplanation linuxConfidenceExplanation `json:"confidenceExplanation"`
	partial               bool
}

type linuxDiagnosisOutput struct {
	Partial         bool             `json:"partial"`
	Status          string           `json:"status"`
	Facts           []linuxSkillFact `json:"facts"`
	MissingEvidence []string         `json:"missingEvidence"`
}

type linuxSkillFact struct {
	Type        string          `json:"type"`
	Summary     string          `json:"summary"`
	EvidenceRef string          `json:"evidenceRef"`
	Evidence    json.RawMessage `json:"evidence"`
}

func buildLinuxHostReport(hostID int64, input AgentContext, diagnosis *skillExecutionOutput) linuxHostReport {
	report := linuxHostReport{
		Host: map[string]any{"id": hostID}, HealthLevel: "unknown", ResourceSummary: map[string]any{},
		Facts: []linuxReportItem{}, RuleFindings: []linuxReportItem{}, Knowledge: []linuxReportItem{}, Hypotheses: []linuxReportItem{},
		EvidenceRefs: []string{}, MissingEvidence: []string{}, Suggestions: []string{
			"Review the evidence and missing-evidence list before changing the host.",
			"Use an approved maintenance workflow for any configuration or service change.",
		},
		RiskTips: []string{
			"Do not infer normal health from a failed collector or insufficient permission.",
			"Service restart, time changes and broad filesystem scans require separate approval.",
		},
	}
	for _, key := range []string{"hostName", "environment", "systemName", "componentName", "profileName"} {
		if value, ok := variable(input, key); ok && !isZeroVariable(value) {
			report.Host[key] = value
		}
	}
	var output linuxDiagnosisOutput
	if diagnosis == nil || json.Unmarshal(diagnosis.Output, &output) != nil {
		report.MissingEvidence = append(report.MissingEvidence, "Linux diagnosis output")
		report.Hypotheses = append(report.Hypotheses, linuxReportItem{Type: "HYPOTHESIS", Summary: "Host health cannot be determined because diagnosis output is unavailable", Confidence: 0.1})
		return report
	}
	report.partial = output.Partial
	report.HealthLevel = normalizeLinuxHealth(output.Status)
	report.MissingEvidence = append(report.MissingEvidence, output.MissingEvidence...)
	skillRef := skillEvidenceRef("diagnose_linux_host_health", diagnosis.RunID)
	report.EvidenceRefs = append(report.EvidenceRefs, skillRef)
	for _, fact := range output.Facts {
		ref := strings.TrimSpace(fact.EvidenceRef)
		if ref != "" {
			report.EvidenceRefs = append(report.EvidenceRefs, ref)
		}
		switch strings.ToUpper(strings.TrimSpace(fact.Type)) {
		case "FACT":
			if ref == "" {
				report.Hypotheses = append(report.Hypotheses, linuxReportItem{Type: "HYPOTHESIS", Summary: fact.Summary, Confidence: 0.2})
				continue
			}
			report.Facts = append(report.Facts, linuxReportItem{Type: "FACT", Summary: fact.Summary, EvidenceRef: ref})
			evaluateLinuxCollectorEvidence(&report, ref, fact.Evidence)
		case "RULE":
			report.RuleFindings = append(report.RuleFindings, linuxReportItem{Type: "RULE", Summary: fact.Summary, EvidenceRef: ref, Severity: report.HealthLevel})
		default:
			report.Hypotheses = append(report.Hypotheses, linuxReportItem{Type: "HYPOTHESIS", Summary: fact.Summary, EvidenceRef: ref, Confidence: 0.2})
		}
	}
	for _, evidence := range input.Evidence {
		if strings.TrimSpace(evidence.Key) != "" {
			report.EvidenceRefs = append(report.EvidenceRefs, evidence.Key)
		}
	}
	if len(report.Facts) == 0 {
		report.HealthLevel = "unknown"
		report.MissingEvidence = append(report.MissingEvidence, "evidence-backed Linux facts")
	}
	return report
}

func evaluateLinuxCollectorEvidence(report *linuxHostReport, evidenceRef string, raw json.RawMessage) {
	var result struct {
		Collector string         `json:"collector"`
		Status    string         `json:"status"`
		Data      map[string]any `json:"data"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Collector == "" {
		return
	}
	report.ResourceSummary[result.Collector] = map[string]any{"status": result.Status, "data": result.Data}
	if result.Status != "success" {
		report.HealthLevel = "unknown"
		return
	}
	switch result.Collector {
	case "cpu":
		usage, usageOK := linuxNumber(result.Data["cpu_usage_percent"])
		load, loadOK := linuxNumber(result.Data["load_per_cpu"])
		if usageOK && usage >= 90 || loadOK && load >= 1.5 {
			report.addRule("CPU pressure is critical in the current snapshot", "critical", evidenceRef)
		} else if usageOK && usage >= 80 || loadOK && load >= 1.0 {
			report.addRule("CPU pressure is warning in the current snapshot", "warning", evidenceRef)
		}
	case "memory":
		total, totalOK := linuxNumber(result.Data["mem_total"])
		available, availableOK := linuxNumber(result.Data["mem_available"])
		swap, swapOK := linuxNumber(result.Data["swap_used_percent"])
		if totalOK && availableOK && total > 0 && available/total < 0.05 {
			report.addRule("Available memory is below 5 percent", "critical", evidenceRef)
		} else if totalOK && availableOK && total > 0 && available/total < 0.10 {
			report.addRule("Available memory is below 10 percent", "warning", evidenceRef)
		}
		if swapOK && swap >= 80 {
			report.addRule("Swap usage is at least 80 percent", "warning", evidenceRef)
		}
	case "filesystem":
		filesystems, _ := result.Data["filesystems"].([]any)
		for _, rawFilesystem := range filesystems {
			filesystem, _ := rawFilesystem.(map[string]any)
			fsType, _ := filesystem["filesystem_type"].(string)
			if linuxPseudoFilesystem(fsType) {
				continue
			}
			used, usedOK := linuxNumber(filesystem["used_percent"])
			inode, inodeOK := linuxNumber(filesystem["inode_used_percent"])
			mount, _ := filesystem["mountpoint"].(string)
			if usedOK && used >= 95 || inodeOK && inode >= 95 {
				report.addRule("Filesystem "+mount+" capacity or inode usage is critical", "critical", evidenceRef)
			} else if usedOK && used >= 85 || inodeOK && inode >= 85 {
				report.addRule("Filesystem "+mount+" capacity or inode usage is warning", "warning", evidenceRef)
			}
		}
	case "systemd":
		state, _ := result.Data["system_state"].(string)
		failed, _ := result.Data["failed_services"].([]any)
		if state == "degraded" || len(failed) > 0 {
			report.addRule("systemd reports degraded state or failed units", "warning", evidenceRef)
		}
	}
}

func (r *linuxHostReport) addRule(summary, severity, evidenceRef string) {
	r.RuleFindings = append(r.RuleFindings, linuxReportItem{Type: "RULE", Summary: summary, Severity: severity, EvidenceRef: evidenceRef})
	if linuxSeverityRank(severity) > linuxSeverityRank(r.HealthLevel) && r.HealthLevel != "unknown" {
		r.HealthLevel = severity
	}
}

func appendLinuxKnowledge(report *linuxHostReport, result *skillExecutionOutput) {
	if result == nil {
		return
	}
	ref := skillEvidenceRef("search_knowledge", result.RunID)
	report.EvidenceRefs = append(report.EvidenceRefs, ref)
	var body map[string]any
	if json.Unmarshal(result.Output, &body) != nil {
		report.MissingEvidence = append(report.MissingEvidence, "knowledge search result")
		return
	}
	count, _ := linuxNumber(body["count"])
	if count <= 0 {
		report.MissingEvidence = append(report.MissingEvidence, "relevant Linux knowledge")
		return
	}
	report.Knowledge = append(report.Knowledge, linuxReportItem{Type: "KNOWLEDGE", Summary: fmt.Sprintf("Knowledge search returned %.0f relevant item(s)", count), EvidenceRef: ref})
}

func appendLinuxTopology(report *linuxHostReport, input AgentContext) {
	topology, ok := variable(input, "topology")
	if !ok || topology == nil {
		report.MissingEvidence = append(report.MissingEvidence, "host topology context")
		return
	}
	report.Topology = topology
	for _, evidence := range input.Evidence {
		if strings.EqualFold(evidence.Source, "topology") && strings.TrimSpace(evidence.Key) != "" {
			report.Facts = append(report.Facts, linuxReportItem{Type: "FACT", Summary: firstNonEmpty(evidence.Summary, "Host topology context is available"), EvidenceRef: evidence.Key})
			return
		}
	}
	report.Knowledge = append(report.Knowledge, linuxReportItem{Type: "KNOWLEDGE", Summary: "Topology context was supplied without a persisted Evidence reference"})
}

func (r *linuxHostReport) finalize() {
	r.EvidenceRefs = uniqueStringsForAgent(r.EvidenceRefs)
	r.MissingEvidence = uniqueStringsForAgent(r.MissingEvidence)
	if r.HealthLevel == "healthy" && r.partial {
		r.HealthLevel = "unknown"
	}
	switch r.HealthLevel {
	case "healthy":
		r.HealthScore = 100
	case "warning":
		r.HealthScore = 70
	case "critical":
		r.HealthScore = 30
	default:
		r.HealthLevel = "unknown"
		r.HealthScore = 0
	}
	confidence := 0.30 + math.Min(float64(len(r.Facts))*0.07, 0.28)
	factors := []string{fmt.Sprintf("%d evidence-backed fact(s)", len(r.Facts))}
	if len(r.Knowledge) > 0 {
		confidence += 0.08
		factors = append(factors, "knowledge evidence available")
	}
	if r.Topology != nil {
		confidence += 0.08
		factors = append(factors, "topology context available")
	}
	if len(r.MissingEvidence) > 0 {
		confidence -= math.Min(float64(len(r.MissingEvidence))*0.05, 0.25)
		factors = append(factors, fmt.Sprintf("%d missing evidence item(s)", len(r.MissingEvidence)))
	}
	if r.HealthLevel == "unknown" {
		confidence = math.Min(confidence, 0.45)
		factors = append(factors, "unknown health caps confidence at 0.45")
	}
	r.Confidence = math.Round(math.Max(0.1, math.Min(0.9, confidence))*100) / 100
	r.ConfidenceExplanation = linuxConfidenceExplanation{EvidenceCount: len(r.EvidenceRefs), MissingEvidenceCount: len(r.MissingEvidence), Partial: r.partial, Factors: factors}
	if len(r.Hypotheses) == 0 {
		summary := "No evidence-backed root cause hypothesis is available"
		if len(r.RuleFindings) > 0 {
			summary = "Observed rule findings may explain the reported host symptom"
		}
		r.Hypotheses = append(r.Hypotheses, linuxReportItem{Type: "HYPOTHESIS", Summary: summary, Confidence: math.Min(r.Confidence, 0.6)})
	}
}

func normalizeLinuxHealth(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "healthy", "warning", "critical", "unknown":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}

func linuxSeverityRank(value string) int {
	switch value {
	case "critical":
		return 3
	case "warning":
		return 2
	case "healthy":
		return 1
	default:
		return 0
	}
}

func linuxNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func linuxPseudoFilesystem(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "tmpfs", "devtmpfs", "overlay", "squashfs":
		return true
	default:
		return false
	}
}

func boolVariableDefault(input AgentContext, key string, fallback bool) bool {
	value, ok := variable(input, key)
	if !ok {
		return fallback
	}
	parsed, ok := value.(bool)
	if !ok {
		return fallback
	}
	return parsed
}
