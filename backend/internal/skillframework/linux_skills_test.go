package skillframework

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"aiops-platform/backend/internal/model"
	linuxserver "aiops-platform/backend/internal/tool/linuxserver"
	"aiops-platform/backend/internal/toolregistry"
)

func TestLinuxSkillsRegisterChapter185Definitions(t *testing.T) {
	skills := LinuxSkills(&fakeLinuxCollector{})
	if len(skills) != 23 {
		t.Fatalf("LinuxSkills() count = %d, want 23", len(skills))
	}
	seen := map[string]bool{}
	for _, skill := range skills {
		definition := skill.Definition()
		if seen[definition.Name] {
			t.Fatalf("duplicate Linux skill %s", definition.Name)
		}
		seen[definition.Name] = true
		if !definition.ReadOnly || len(definition.RequiredTools) != 1 || definition.RequiredTools[0] != "linux_server" {
			t.Fatalf("unsafe Linux skill definition: %+v", definition)
		}
		if !json.Valid(definition.InputSchema) || !json.Valid(definition.OutputSchema) {
			t.Fatalf("invalid schema for %s", definition.Name)
		}
		lowerSchema := strings.ToLower(string(definition.InputSchema))
		if strings.Contains(lowerSchema, "command") || strings.Contains(lowerSchema, "argv") || strings.Contains(lowerSchema, "executable") {
			t.Fatalf("Linux skill exposes command execution input: %s", definition.InputSchema)
		}
	}
	for _, name := range []string{"get_linux_network_status", "get_linux_process_status", "get_linux_system_log_summary", "get_linux_security_summary", "batch_diagnose_linux_hosts"} {
		if !seen[name] {
			t.Fatalf("chapter 185 skill %s was not registered", name)
		}
	}
}

func TestLinuxSensitiveReadPermissionAndSafeCollectorBoundary(t *testing.T) {
	collector := &fakeLinuxCollector{}
	registry, err := NewRegistry(toolregistry.NewBuiltinRegistry(), nil, LinuxSkills(collector)...)
	if err != nil {
		t.Fatal(err)
	}
	_, err = registry.Execute(context.Background(), ExecuteInput{
		Actor: userActor(), Name: "get_linux_network_status", Payload: json.RawMessage(`{"hostId":1}`),
	})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("sensitive Linux skill error = %v, want permission denied", err)
	}
	_, err = registry.Execute(context.Background(), ExecuteInput{
		Actor: userActor(), Name: "get_linux_cpu_status", Payload: json.RawMessage(`{"hostId":1,"topN":5,"command":"rm","argv":["-rf"]}`),
	})
	if err != nil {
		t.Fatalf("safe Linux skill execute: %v", err)
	}
	if collector.lastCollector != linuxserver.CollectorCPU || strings.Contains(string(collector.lastParameters), "command") || strings.Contains(string(collector.lastParameters), "argv") {
		t.Fatalf("untrusted command input crossed collector boundary: collector=%s parameters=%s", collector.lastCollector, collector.lastParameters)
	}
}

func TestEveryLinuxSkillReturnsFactsAndEvidenceRefs(t *testing.T) {
	collector := &fakeLinuxCollector{}
	for _, skill := range LinuxSkills(collector) {
		payload := json.RawMessage(`{"hostId":7,"topN":5,"sinceHours":24,"service":"nginx.service"}`)
		if skill.Definition().Name == "batch_diagnose_linux_hosts" {
			payload = json.RawMessage(`{"hostIds":[7,8]}`)
		}
		output, err := skill.Execute(ContextWithActor(context.Background(), adminActor()), payload)
		if err != nil {
			t.Fatalf("%s Execute() error = %v", skill.Definition().Name, err)
		}
		var decoded struct {
			Facts []struct {
				Type        string `json:"type"`
				EvidenceRef string `json:"evidenceRef"`
			} `json:"facts"`
		}
		if json.Unmarshal(output, &decoded) != nil || len(decoded.Facts) == 0 {
			t.Fatalf("%s returned no facts: %s", skill.Definition().Name, output)
		}
		for _, fact := range decoded.Facts {
			if (fact.Type != "FACT" && fact.Type != "RULE") || fact.EvidenceRef == "" {
				t.Fatalf("%s returned fact without type/evidenceRef: %s", skill.Definition().Name, output)
			}
		}
	}
}

func TestLinuxConnectionFailureNeverReturnsHealthy(t *testing.T) {
	collector := &fakeLinuxCollector{collectErr: errors.New("dial failed with password=secret")}
	skill := linuxSkillByName(t, collector, "diagnose_linux_host_health")
	output, err := skill.Execute(ContextWithActor(context.Background(), adminActor()), json.RawMessage(`{"hostId":9}`))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Partial bool     `json:"partial"`
		Status  string   `json:"status"`
		Missing []string `json:"missingEvidence"`
	}
	if json.Unmarshal(output, &decoded) != nil {
		t.Fatalf("invalid output: %s", output)
	}
	if !decoded.Partial || decoded.Status != "unknown" || len(decoded.Missing) == 0 {
		t.Fatalf("connection failure output = %s", output)
	}
	if strings.Contains(string(output), "password=secret") || strings.Contains(string(output), `"status":"healthy"`) {
		t.Fatalf("connection failure leaked credential or returned healthy: %s", output)
	}
}

func TestLinuxMissingCommandBecomesMissingEvidence(t *testing.T) {
	collector := &fakeLinuxCollector{result: &linuxserver.LinuxCollectResult{
		Collector: linuxserver.CollectorDiskIO, Status: linuxserver.CommandStatusPartial,
		Data:     json.RawMessage(`{"capability":"proc_diskstats"}`),
		Warnings: []string{"iostat: unsupported", "iostat unavailable; using /proc/diskstats basic counters"},
	}}
	skill := linuxSkillByName(t, collector, "get_linux_disk_io_status")
	output, err := skill.Execute(ContextWithActor(context.Background(), userActor()), json.RawMessage(`{"hostId":4}`))
	if err != nil {
		t.Fatal(err)
	}
	if !jsonContainsString(output, "missingEvidence") || !jsonContainsString(output, "iostat unavailable") {
		t.Fatalf("missing command did not enter missingEvidence: %s", output)
	}
}

func linuxSkillByName(t *testing.T, collector LinuxCollector, name string) Skill {
	t.Helper()
	for _, skill := range LinuxSkills(collector) {
		if skill.Definition().Name == name {
			return skill
		}
	}
	t.Fatalf("Linux skill %s not found", name)
	return nil
}

type fakeLinuxCollector struct {
	result         *linuxserver.LinuxCollectResult
	collectErr     error
	testResult     *linuxserver.LinuxConnectionTestResult
	testErr        error
	lastCollector  string
	lastParameters json.RawMessage
}

func (f *fakeLinuxCollector) TestConnection(context.Context, *model.AppUser, int64) (*linuxserver.LinuxConnectionTestResult, error) {
	if f.testResult != nil || f.testErr != nil {
		return f.testResult, f.testErr
	}
	return &linuxserver.LinuxConnectionTestResult{Status: linuxserver.CommandStatusSuccess, ServerVersion: "SSH-2.0-test", AuthMethod: "password"}, nil
}

func (f *fakeLinuxCollector) Collect(_ context.Context, _ *model.AppUser, _ int64, collector string, parameters json.RawMessage) (*linuxserver.LinuxCollectResult, error) {
	f.lastCollector = collector
	f.lastParameters = append(json.RawMessage(nil), parameters...)
	if f.collectErr != nil {
		return nil, f.collectErr
	}
	if f.result != nil {
		copy := *f.result
		if copy.Collector == "" {
			copy.Collector = collector
		}
		return &copy, nil
	}
	return &linuxserver.LinuxCollectResult{
		Collector: collector, CommandVersion: "1.0.0", Status: linuxserver.CommandStatusSuccess,
		Data: json.RawMessage(`{"available":true}`),
	}, nil
}
