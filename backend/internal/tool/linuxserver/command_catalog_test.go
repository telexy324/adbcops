package linuxserver

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinCatalogContainsOnlyFixedReadOnlyCommands(t *testing.T) {
	t.Parallel()
	catalog := NewBuiltinCatalog()
	definitions := catalog.List()
	if len(definitions) < 30 {
		t.Fatalf("definition count = %d, want at least 30", len(definitions))
	}
	for _, definition := range definitions {
		if definition.Version != "1.0.0" || !definition.EnabledByDefault {
			t.Errorf("invalid version/default state for %s: %+v", definition.Key, definition)
		}
		executable := filepath.Base(definition.Executable)
		if executable == "sh" || executable == "bash" || executable == "eval" {
			t.Errorf("catalog contains shell executable: %+v", definition)
		}
		for _, argument := range definition.ArgsTemplate {
			if strings.Contains(argument, "sh -c") || strings.Contains(argument, "bash -c") {
				t.Errorf("catalog contains shell invocation: %s %v", definition.Executable, definition.ArgsTemplate)
			}
		}
	}
	for _, forbiddenKey := range []string{"command", "shell", "exec", "run"} {
		if _, err := catalog.Get(forbiddenKey); !errors.Is(err, ErrCommandNotFound) {
			t.Fatalf("generic command key %q is available", forbiddenKey)
		}
	}
}

func TestCatalogBuildsArgvWithoutShellParsing(t *testing.T) {
	t.Parallel()
	catalog := NewBuiltinCatalog()
	plan, err := catalog.Plan("logs.service", json.RawMessage(`{"service":"nginx.service","sinceHours":6}`))
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	want := []string{"-u", "nginx.service", "--since", "-6h", "--no-pager"}
	if plan.Executable != "journalctl" || strings.Join(plan.Args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("plan = %s %q, want journalctl %q", plan.Executable, plan.Args, want)
	}
	processPlan, err := catalog.Plan("process.top_cpu", nil)
	if err != nil {
		t.Fatal(err)
	}
	if processPlan.MaxRows != 21 {
		t.Fatalf("default topN rows = %d, want header + 20", processPlan.MaxRows)
	}
	processPlan, err = catalog.Plan("process.top_cpu", json.RawMessage(`{"topN":100}`))
	if err != nil || processPlan.MaxRows != 101 {
		t.Fatalf("explicit topN plan = %+v, error = %v", processPlan, err)
	}
}

func TestEveryBuiltinDefinitionCanBuildAPlan(t *testing.T) {
	t.Parallel()
	catalog := NewBuiltinCatalog()
	for _, definition := range catalog.List() {
		parameters := json.RawMessage(`{}`)
		switch definition.Key {
		case "systemd.show":
			parameters = json.RawMessage(`{"service":"nginx.service"}`)
		case "logs.service":
			parameters = json.RawMessage(`{"service":"nginx.service","sinceHours":24}`)
		case "platform.which":
			parameters = json.RawMessage(`{"command":"uname"}`)
		}
		plan, err := catalog.Plan(definition.Key, parameters)
		if err != nil {
			t.Errorf("Plan(%s) error = %v", definition.Key, err)
			continue
		}
		if plan.Executable == "" || plan.Version == "" || plan.TimeoutSeconds <= 0 || plan.MaxOutputBytes <= 0 || plan.MaxRows <= 0 {
			t.Errorf("Plan(%s) is incomplete: %+v", definition.Key, plan)
		}
	}
}

func TestCatalogRejectsShellMetacharactersAndUnknownParameters(t *testing.T) {
	t.Parallel()
	catalog := NewBuiltinCatalog()
	for _, value := range []string{
		"nginx;id", "nginx|id", "nginx&id", "nginx`id`", "nginx$(id)",
		"nginx>file", "nginx<file", "nginx\nservice", "nginx\rservice",
	} {
		parameters, _ := json.Marshal(map[string]any{"service": value})
		if _, err := catalog.Plan("systemd.show", parameters); !errors.Is(err, ErrInvalidParameters) {
			t.Errorf("service %q error = %v, want ErrInvalidParameters", value, err)
		}
	}
	for _, parameters := range []json.RawMessage{
		json.RawMessage(`{"service":"nginx.service","command":"id"}`),
		json.RawMessage(`{"service":"../nginx"}`),
		json.RawMessage(`{"topN":101}`),
		json.RawMessage(`{"topN":1.5}`),
		json.RawMessage(`{"topN":10} trailing`),
	} {
		key := "systemd.show"
		if strings.Contains(string(parameters), "topN") {
			key = "process.top_cpu"
		}
		if _, err := catalog.Plan(key, parameters); !errors.Is(err, ErrInvalidParameters) {
			t.Errorf("parameters %s error = %v", parameters, err)
		}
	}
}

func TestCatalogRejectsShellDefinitions(t *testing.T) {
	t.Parallel()
	definition := LinuxCommandDefinition{
		Key: "bad", Version: "1", Description: "bad", Executable: "bash",
		ArgsTemplate:      []string{"-c", "id"},
		AllowedParameters: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{}}`),
		RiskLevel:         RiskSafeRead, TimeoutSeconds: 1, MaxOutputBytes: 1024, MaxRows: 1,
		EnabledByDefault: true,
	}
	if _, err := NewCatalog(definition); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("NewCatalog(shell) error = %v", err)
	}
}

func TestCatalogRejectsNonAllowlistedAndMutatingDefinitions(t *testing.T) {
	t.Parallel()
	for _, definition := range []LinuxCommandDefinition{
		testCatalogDefinition("rm", []string{"-rf", "/tmp/example"}),
		testCatalogDefinition("systemctl", []string{"restart", "nginx"}),
		testCatalogDefinition("ip", []string{"route", "delete", "default"}),
	} {
		if _, err := NewCatalog(definition); !errors.Is(err, ErrInvalidDefinition) {
			t.Errorf("NewCatalog(%s %q) error = %v", definition.Executable, definition.ArgsTemplate, err)
		}
	}
}

func testCatalogDefinition(executable string, args []string) LinuxCommandDefinition {
	return LinuxCommandDefinition{
		Key: "test.definition", Version: "1", Description: "test", Executable: executable, ArgsTemplate: args,
		AllowedParameters: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{}}`),
		RiskLevel:         RiskSafeRead, TimeoutSeconds: 1, MaxOutputBytes: 1024, MaxRows: 1,
		EnabledByDefault: true,
	}
}
