package linuxserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func TestExecutorUsesCatalogArgvAndTruncatesRows(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{output: numberedLines(40)}
	executor, err := NewExecutor(NewBuiltinCatalog(), runner)
	if err != nil {
		t.Fatal(err)
	}
	platform := DetectPlatform("ID=ubuntu\nVERSION_ID=22.04", "Linux", []string{"ps"})
	result, err := executor.Execute(context.Background(), CommandRequest{
		Key: "process.top_cpu", Parameters: json.RawMessage(`{"topN":10}`), Platform: platform,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if runner.executable != "ps" || len(runner.args) != 3 || runner.args[0] != "-eo" {
		t.Fatalf("runner received %s %q", runner.executable, runner.args)
	}
	if result.Status != CommandStatusPartial || !result.Truncated || len(strings.Split(strings.TrimSpace(result.Output), "\n")) != 11 {
		t.Fatalf("result = %+v", result)
	}
}

func TestExecutorTruncatesOutputBytes(t *testing.T) {
	t.Parallel()
	definition := testDefinition("test.bytes", "uname", 1024, 100)
	catalog, err := NewCatalog(definition)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{output: strings.Repeat("x", 2048)}
	executor, _ := NewExecutor(catalog, runner)
	platform := DetectPlatform("ID=debian\nVERSION_ID=12", "Linux", []string{"uname"})
	result, err := executor.Execute(context.Background(), CommandRequest{Key: definition.Key, Platform: platform})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Output) != 1024 || !result.Truncated || result.Status != CommandStatusPartial {
		t.Fatalf("byte limited result length=%d result=%+v", len(result.Output), result)
	}
}

func TestExecutorReturnsPartialAndUnsupportedCapabilities(t *testing.T) {
	t.Parallel()
	executor, _ := NewExecutor(NewBuiltinCatalog(), &fakeRunner{})
	partialPlatform := DetectPlatform("ID=ubuntu\nVERSION_ID=22.04", "Linux", []string{"uname"})
	partial, err := executor.Execute(context.Background(), CommandRequest{Key: "diskio.iostat", Platform: partialPlatform})
	if err != nil || partial.Status != CommandStatusPartial || len(partial.Warnings) == 0 {
		t.Fatalf("partial result = %+v, error = %v", partial, err)
	}
	unsupportedPlatform := DetectPlatform("ID=freebsd\nVERSION_ID=14", "FreeBSD", []string{"iostat"})
	unsupported, err := executor.Execute(context.Background(), CommandRequest{Key: "diskio.iostat", Platform: unsupportedPlatform})
	if err != nil || unsupported.Status != CommandStatusUnsupported || len(unsupported.Warnings) == 0 {
		t.Fatalf("unsupported result = %+v, error = %v", unsupported, err)
	}
}

func TestExecutorMapsPermissionMissingAndTimeout(t *testing.T) {
	t.Parallel()
	definition := testDefinition("test.runner", "uname", 1024, 100)
	catalog, _ := NewCatalog(definition)
	platform := DetectPlatform("ID=rhel\nVERSION_ID=9", "Linux", []string{"uname"})
	for _, tt := range []struct {
		name       string
		runner     *fakeRunner
		context    func() (context.Context, context.CancelFunc)
		wantStatus string
	}{
		{name: "permission", runner: &fakeRunner{err: ErrRunnerPermission}, context: backgroundContext, wantStatus: CommandStatusPartial},
		{name: "missing", runner: &fakeRunner{err: ErrRunnerCommandNotFound}, context: backgroundContext, wantStatus: CommandStatusUnsupported},
		{name: "timeout", runner: &fakeRunner{waitForContext: true}, context: shortContext, wantStatus: CommandStatusTimeout},
	} {
		t.Run(tt.name, func(t *testing.T) {
			executor, _ := NewExecutor(catalog, tt.runner)
			ctx, cancel := tt.context()
			defer cancel()
			result, err := executor.Execute(ctx, CommandRequest{Key: definition.Key, Platform: platform})
			if err != nil || result.Status != tt.wantStatus {
				t.Fatalf("result = %+v, error = %v, want status %s", result, err, tt.wantStatus)
			}
		})
	}
}

func TestExecutorRejectsUnknownCommandInsteadOfRunningInput(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	executor, _ := NewExecutor(NewBuiltinCatalog(), runner)
	platform := DetectPlatform("ID=ubuntu\nVERSION_ID=22.04", "Linux", []string{"id"})
	_, err := executor.Execute(context.Background(), CommandRequest{Key: "id", Platform: platform})
	if !errors.Is(err, ErrCommandNotFound) || runner.called {
		t.Fatalf("Execute(unknown) error = %v, runner called = %v", err, runner.called)
	}
}

func TestOSRunnerExecutesCatalogArgvDirectly(t *testing.T) {
	catalog := NewBuiltinCatalog()
	executor, err := NewExecutor(catalog, OSRunner{})
	if err != nil {
		t.Fatal(err)
	}
	platform := DetectPlatform("ID=ubuntu\nVERSION_ID=22.04", "Linux", []string{"uname"})
	result, err := executor.Execute(context.Background(), CommandRequest{Key: "system.uname", Platform: platform})
	if err != nil {
		t.Fatalf("Execute(OSRunner) error = %v", err)
	}
	if result.Status != CommandStatusSuccess || strings.TrimSpace(result.Output) == "" {
		t.Fatalf("OSRunner result = %+v", result)
	}
}

type fakeRunner struct {
	output         string
	err            error
	waitForContext bool
	called         bool
	executable     string
	args           []string
}

func (r *fakeRunner) Run(ctx context.Context, executable string, args []string, stdout, _ io.Writer) error {
	r.called = true
	r.executable = executable
	r.args = append([]string(nil), args...)
	if r.waitForContext {
		<-ctx.Done()
		return ctx.Err()
	}
	if r.output != "" {
		_, _ = io.WriteString(stdout, r.output)
	}
	return r.err
}

func testDefinition(key, executable string, maxBytes int64, maxRows int) LinuxCommandDefinition {
	return LinuxCommandDefinition{
		Key: key, Version: "1.0.0", Description: "test definition", Executable: executable,
		AllowedParameters: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{}}`),
		RiskLevel:         RiskSafeRead, TimeoutSeconds: 1, MaxOutputBytes: maxBytes, MaxRows: maxRows,
		RequiredCommands: []string{executable}, SupportedOS: []string{"rhel", "debian"}, EnabledByDefault: true,
	}
}

func numberedLines(count int) string {
	var builder strings.Builder
	for index := 0; index < count; index++ {
		_, _ = fmt.Fprintf(&builder, "line-%d\n", index)
	}
	return builder.String()
}

func backgroundContext() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

func shortContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 20*time.Millisecond)
}
