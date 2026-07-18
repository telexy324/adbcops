package skillframework

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"aiops-platform/backend/internal/model"
	linuxserver "aiops-platform/backend/internal/tool/linuxserver"
)

const maxLinuxBatchSkillHosts = 100

type LinuxCollector interface {
	TestConnection(context.Context, *model.AppUser, int64) (*linuxserver.LinuxConnectionTestResult, error)
	Collect(context.Context, *model.AppUser, int64, string, json.RawMessage) (*linuxserver.LinuxCollectResult, error)
}

type linuxSkillSpec struct {
	name        string
	description string
	collector   string
	risk        string
	diagnosis   []string
}

var linuxSkillSpecs = []linuxSkillSpec{
	{"test_linux_server_connection", "Test a saved Linux host connection without exposing credentials.", "", model.SkillRiskSafeRead, nil},
	{"get_linux_system_overview", "Collect Linux operating system and uptime overview.", linuxserver.CollectorSystemOverview, model.SkillRiskSafeRead, nil},
	{"get_linux_cpu_status", "Collect Linux CPU, load and bounded top process status.", linuxserver.CollectorCPU, model.SkillRiskSafeRead, nil},
	{"get_linux_memory_status", "Collect Linux memory, swap and bounded top process status.", linuxserver.CollectorMemory, model.SkillRiskSafeRead, nil},
	{"get_linux_filesystem_status", "Collect Linux filesystem capacity, inode and mount status.", linuxserver.CollectorFilesystem, model.SkillRiskSafeRead, nil},
	{"get_linux_disk_io_status", "Collect Linux disk IO status with proc fallback.", linuxserver.CollectorDiskIO, model.SkillRiskSafeRead, nil},
	{"get_linux_network_status", "Collect Linux interfaces, routes, DNS and listening port details.", linuxserver.CollectorNetwork, model.SkillRiskSensitiveRead, nil},
	{"get_linux_process_status", "Collect bounded Linux process status without full command lines.", linuxserver.CollectorProcess, model.SkillRiskSensitiveRead, nil},
	{"get_linux_service_status", "Collect Linux systemd state and an optional validated unit.", linuxserver.CollectorSystemd, model.SkillRiskSafeRead, nil},
	{"get_linux_time_sync_status", "Collect Linux time synchronization status.", linuxserver.CollectorTimeSync, model.SkillRiskSafeRead, nil},
	{"get_linux_kernel_event_summary", "Collect a bounded Linux kernel event summary.", linuxserver.CollectorKernelEvents, model.SkillRiskSensitiveRead, nil},
	{"get_linux_system_log_summary", "Collect a bounded and sanitized Linux system log summary.", linuxserver.CollectorSystemLogs, model.SkillRiskSensitiveRead, nil},
	{"get_linux_security_summary", "Summarize Linux security signals from bounded read-only collectors.", "", model.SkillRiskSensitiveRead, []string{linuxserver.CollectorSystemLogs, linuxserver.CollectorNetwork, linuxserver.CollectorKernelEvents}},
	{"diagnose_linux_host_health", "Diagnose overall Linux host health from independent collector evidence.", "", model.SkillRiskSensitiveRead, []string{linuxserver.CollectorSystemOverview, linuxserver.CollectorCPU, linuxserver.CollectorMemory, linuxserver.CollectorFilesystem, linuxserver.CollectorSystemd, linuxserver.CollectorTimeSync}},
	{"diagnose_linux_cpu_pressure", "Diagnose Linux CPU pressure.", "", model.SkillRiskSafeRead, []string{linuxserver.CollectorCPU}},
	{"diagnose_linux_memory_pressure", "Diagnose Linux memory pressure.", "", model.SkillRiskSafeRead, []string{linuxserver.CollectorMemory}},
	{"diagnose_linux_disk_capacity", "Diagnose Linux filesystem capacity and inode pressure.", "", model.SkillRiskSafeRead, []string{linuxserver.CollectorFilesystem}},
	{"diagnose_linux_disk_io", "Diagnose Linux disk IO pressure.", "", model.SkillRiskSafeRead, []string{linuxserver.CollectorDiskIO}},
	{"diagnose_linux_network", "Diagnose Linux network state.", "", model.SkillRiskSensitiveRead, []string{linuxserver.CollectorNetwork}},
	{"diagnose_linux_process", "Diagnose Linux process state.", "", model.SkillRiskSensitiveRead, []string{linuxserver.CollectorProcess}},
	{"diagnose_linux_service", "Diagnose Linux systemd service state.", "", model.SkillRiskSafeRead, []string{linuxserver.CollectorSystemd}},
	{"diagnose_linux_time_sync", "Diagnose Linux time synchronization.", "", model.SkillRiskSafeRead, []string{linuxserver.CollectorTimeSync}},
	{"batch_diagnose_linux_hosts", "Diagnose a bounded set of saved Linux hosts.", "", model.SkillRiskSensitiveRead, nil},
}

func LinuxSkills(collector LinuxCollector) []Skill {
	skills := make([]Skill, 0, len(linuxSkillSpecs))
	for _, spec := range linuxSkillSpecs {
		skills = append(skills, LinuxSkill{spec: spec, collector: collector})
	}
	return skills
}

type LinuxSkill struct {
	spec      linuxSkillSpec
	collector LinuxCollector
}

type linuxSkillRequest struct {
	HostID     int64   `json:"hostId"`
	HostIDs    []int64 `json:"hostIds"`
	TopN       int     `json:"topN"`
	SinceHours int     `json:"sinceHours"`
	Service    string  `json:"service"`
}

func (s LinuxSkill) Definition() SkillDefinition {
	inputSchema := json.RawMessage(`{"type":"object","required":["hostId"],"properties":{"hostId":{"type":"integer"},"topN":{"type":"integer"},"sinceHours":{"type":"integer"},"service":{"type":"string"}}}`)
	if s.spec.name == "batch_diagnose_linux_hosts" {
		inputSchema = json.RawMessage(`{"type":"object","required":["hostIds"],"properties":{"hostIds":{"type":"array","items":{"type":"integer"}},"topN":{"type":"integer"},"sinceHours":{"type":"integer"}}}`)
	}
	return SkillDefinition{
		Name: s.spec.name, Version: "v1", Description: s.spec.description,
		InputSchema: inputSchema, OutputSchema: linuxOutputSchema(), RiskLevel: s.spec.risk,
		ReadOnly: true, TimeoutSecond: 60, RequiredTools: []string{"linux_server"},
	}
}

func (s LinuxSkill) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var request linuxSkillRequest
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, ErrInvalidInput
	}
	if s.collector == nil {
		return linuxFailureOutput(s.spec.name, request.HostID, "linux collector service is not configured"), nil
	}
	if s.spec.name == "batch_diagnose_linux_hosts" {
		return s.batchDiagnose(ctx, request)
	}
	if request.HostID <= 0 {
		return nil, ErrInvalidInput
	}
	if s.spec.name == "test_linux_server_connection" {
		return s.testConnection(ctx, request.HostID)
	}
	if s.spec.collector != "" {
		return s.collectOne(ctx, request, s.spec.collector)
	}
	return s.diagnose(ctx, request, s.spec.diagnosis)
}

func (s LinuxSkill) testConnection(ctx context.Context, hostID int64) (json.RawMessage, error) {
	result, err := s.collector.TestConnection(ctx, ActorFromContext(ctx), hostID)
	status := "unknown"
	evidence := any(map[string]any{"status": status})
	missing := []string{}
	if result != nil {
		status = result.Status
		evidence = result
	}
	if err != nil {
		missing = append(missing, "linux SSH connection: "+safeLinuxSkillError(err))
	}
	summary := "Linux SSH connection status is " + status
	return linuxMarshalOutput(s.spec.name, err != nil || status != linuxserver.CommandStatusSuccess, []map[string]any{
		linuxFact("FACT", summary, linuxEvidenceRef(hostID, "connection"), evidence),
	}, missing, status)
}

func (s LinuxSkill) collectOne(ctx context.Context, request linuxSkillRequest, collectorName string) (json.RawMessage, error) {
	result, err := s.collector.Collect(ctx, ActorFromContext(ctx), request.HostID, collectorName, linuxParameters(request, collectorName))
	if err != nil {
		return linuxFailureOutput(s.spec.name, request.HostID, safeLinuxSkillError(err)), nil
	}
	missing := linuxMissingEvidence(result)
	status := "unknown"
	if result != nil {
		status = result.Status
	}
	fact := linuxFact("FACT", fmt.Sprintf("Linux %s collector returned %s", collectorName, status), linuxEvidenceRef(request.HostID, collectorName), result)
	return linuxMarshalOutput(s.spec.name, status != linuxserver.CommandStatusSuccess || len(missing) > 0, []map[string]any{fact}, missing, status)
}

func (s LinuxSkill) diagnose(ctx context.Context, request linuxSkillRequest, collectors []string) (json.RawMessage, error) {
	facts := make([]map[string]any, 0, len(collectors)+1)
	missing := []string{}
	overall := "healthy"
	for _, collectorName := range collectors {
		result, err := s.collector.Collect(ctx, ActorFromContext(ctx), request.HostID, collectorName, linuxParameters(request, collectorName))
		if err != nil {
			overall = "unknown"
			message := collectorName + ": " + safeLinuxSkillError(err)
			missing = append(missing, message)
			facts = append(facts, linuxFact("FACT", "Linux collector failed; health remains unknown", linuxEvidenceRef(request.HostID, collectorName), map[string]any{"collector": collectorName, "status": "unknown", "error": safeLinuxSkillError(err)}))
			continue
		}
		if result == nil {
			overall = "unknown"
			missing = append(missing, collectorName+": collector returned no evidence")
			facts = append(facts, linuxFact("FACT", "Linux collector returned no evidence; health remains unknown", linuxEvidenceRef(request.HostID, collectorName), map[string]any{"collector": collectorName, "status": "unknown"}))
			continue
		}
		facts = append(facts, linuxFact("FACT", fmt.Sprintf("Linux %s evidence collected with status %s", collectorName, result.Status), linuxEvidenceRef(request.HostID, collectorName), result))
		missing = append(missing, linuxMissingEvidence(result)...)
		switch result.Status {
		case linuxserver.CommandStatusUnsupported, linuxserver.CommandStatusFailed, linuxserver.CommandStatusTimeout:
			overall = "unknown"
		case linuxserver.CommandStatusPartial:
			if overall == "healthy" {
				overall = "warning"
			}
		}
	}
	if len(collectors) == 0 {
		overall = "unknown"
		missing = append(missing, "no collectors configured")
	}
	if s.spec.name == "get_linux_security_summary" {
		missing = append(missing, "failed login summary collector", "current login user summary collector", "critical system file permission rules")
		if overall == "healthy" {
			overall = "warning"
		}
	}
	facts = append(facts, linuxFact("RULE", "Linux diagnosis status is "+overall, linuxEvidenceRef(request.HostID, "diagnosis"), map[string]any{"status": overall, "collectors": collectors}))
	return linuxMarshalOutput(s.spec.name, len(missing) > 0 || overall != "healthy", facts, uniqueLinuxStrings(missing), overall)
}

func (s LinuxSkill) batchDiagnose(ctx context.Context, request linuxSkillRequest) (json.RawMessage, error) {
	if len(request.HostIDs) == 0 || len(request.HostIDs) > maxLinuxBatchSkillHosts {
		return nil, ErrInvalidInput
	}
	seen := map[int64]bool{}
	facts := []map[string]any{}
	missing := []string{}
	status := "healthy"
	for _, hostID := range request.HostIDs {
		if hostID <= 0 || seen[hostID] {
			return nil, ErrInvalidInput
		}
		seen[hostID] = true
		result, err := s.collector.Collect(ctx, ActorFromContext(ctx), hostID, linuxserver.CollectorSystemOverview, linuxParameters(request, linuxserver.CollectorSystemOverview))
		if err != nil {
			status = "unknown"
			missing = append(missing, fmt.Sprintf("host %d connection: %s", hostID, safeLinuxSkillError(err)))
			facts = append(facts, linuxFact("FACT", "Linux host collection failed; health remains unknown", linuxEvidenceRef(hostID, linuxserver.CollectorSystemOverview), map[string]any{"hostId": hostID, "status": "unknown"}))
			continue
		}
		if result == nil {
			status = "unknown"
			missing = append(missing, fmt.Sprintf("host %d overview: collector returned no evidence", hostID))
			facts = append(facts, linuxFact("FACT", "Linux host collector returned no evidence; health remains unknown", linuxEvidenceRef(hostID, linuxserver.CollectorSystemOverview), map[string]any{"hostId": hostID, "status": "unknown"}))
			continue
		}
		facts = append(facts, linuxFact("FACT", fmt.Sprintf("Linux host %d overview collected with status %s", hostID, result.Status), linuxEvidenceRef(hostID, linuxserver.CollectorSystemOverview), result))
		missing = append(missing, linuxMissingEvidence(result)...)
		if result.Status != linuxserver.CommandStatusSuccess {
			status = "unknown"
		}
	}
	facts = append(facts, linuxFact("RULE", "Batch Linux diagnosis status is "+status, "linux.batch.diagnosis", map[string]any{"status": status, "hostCount": len(request.HostIDs)}))
	return linuxMarshalOutput(s.spec.name, len(missing) > 0 || status != "healthy", facts, uniqueLinuxStrings(missing), status)
}

func linuxParameters(request linuxSkillRequest, collector string) json.RawMessage {
	parameters := map[string]any{}
	if (collector == linuxserver.CollectorCPU || collector == linuxserver.CollectorMemory || collector == linuxserver.CollectorProcess) && request.TopN > 0 {
		parameters["topN"] = request.TopN
	}
	if (collector == linuxserver.CollectorKernelEvents || collector == linuxserver.CollectorSystemLogs) && request.SinceHours > 0 {
		parameters["sinceHours"] = request.SinceHours
	}
	if (collector == linuxserver.CollectorSystemd || collector == linuxserver.CollectorSystemLogs) && strings.TrimSpace(request.Service) != "" {
		parameters["service"] = strings.TrimSpace(request.Service)
	}
	payload, _ := json.Marshal(parameters)
	return payload
}

func linuxMissingEvidence(result *linuxserver.LinuxCollectResult) []string {
	if result == nil {
		return []string{"collector returned no evidence"}
	}
	missing := []string{}
	for _, warning := range result.Warnings {
		normalized := strings.ToLower(warning)
		if strings.Contains(normalized, "missing command") || strings.Contains(normalized, "unavailable") || strings.Contains(normalized, "unsupported") || strings.Contains(normalized, "permission denied") {
			missing = append(missing, result.Collector+": "+warning)
		}
	}
	if result.Status == linuxserver.CommandStatusUnsupported && len(missing) == 0 {
		missing = append(missing, result.Collector+": required command is unavailable")
	}
	return missing
}

func linuxFact(factType, summary, evidenceRef string, evidence any) map[string]any {
	return map[string]any{"type": factType, "summary": summary, "evidenceRef": evidenceRef, "evidence": evidence}
}

func linuxEvidenceRef(hostID int64, collector string) string {
	return fmt.Sprintf("linux.host.%d.%s", hostID, collector)
}

func linuxMarshalOutput(skill string, partial bool, facts []map[string]any, missing []string, status string) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"partial": partial, "skill": skill, "status": status,
		"facts": facts, "missingEvidence": uniqueLinuxStrings(missing),
	})
}

func linuxFailureOutput(skill string, hostID int64, message string) json.RawMessage {
	output, _ := linuxMarshalOutput(skill, true, []map[string]any{
		linuxFact("FACT", "Linux collection failed; health remains unknown", linuxEvidenceRef(hostID, "collection_failure"), map[string]any{"status": "unknown", "error": message}),
	}, []string{"linux collector connection: " + message}, "unknown")
	return output
}

func safeLinuxSkillError(_ error) string {
	return "Linux collector request failed"
}

func uniqueLinuxStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func linuxOutputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","required":["partial","skill","status","facts","missingEvidence"],"properties":{"partial":{"type":"boolean"},"skill":{"type":"string"},"status":{"type":"string"},"facts":{"type":"array","items":{"type":"object"}},"missingEvidence":{"type":"array","items":{"type":"string"}}}}`)
}
