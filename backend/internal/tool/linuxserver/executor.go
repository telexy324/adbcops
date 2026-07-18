package linuxserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	CommandStatusSuccess     = "success"
	CommandStatusPartial     = "partial"
	CommandStatusUnsupported = "unsupported"
	CommandStatusFailed      = "failed"
	CommandStatusTimeout     = "timeout"
)

var (
	ErrRunnerCommandNotFound = errors.New("runner command not found")
	ErrRunnerPermission      = errors.New("runner permission denied")
)

type CommandRunner interface {
	Run(ctx context.Context, executable string, args []string, stdout, stderr io.Writer) error
}

type CommandRequest struct {
	Key        string
	Parameters json.RawMessage
	Platform   LinuxPlatformInfo
}

type CommandResult struct {
	Key            string   `json:"key"`
	CommandVersion string   `json:"commandVersion"`
	Status         string   `json:"status"`
	Output         string   `json:"output,omitempty"`
	Warnings       []string `json:"warnings,omitempty"`
	DurationMS     int64    `json:"durationMs"`
	Truncated      bool     `json:"truncated"`
}

type Executor struct {
	catalog *Catalog
	runner  CommandRunner
}

func NewExecutor(catalog *Catalog, runner CommandRunner) (*Executor, error) {
	if catalog == nil || runner == nil {
		return nil, ErrInvalidDefinition
	}
	return &Executor{catalog: catalog, runner: runner}, nil
}

func (e *Executor) Execute(ctx context.Context, request CommandRequest) (*CommandResult, error) {
	definition, err := e.catalog.Get(request.Key)
	if err != nil {
		return nil, err
	}
	plan, err := e.catalog.Plan(request.Key, request.Parameters)
	if err != nil {
		return nil, err
	}
	capability := EvaluateCommandCapability(request.Platform, definition)
	result := &CommandResult{
		Key: plan.Key, CommandVersion: plan.Version, Status: capability.Status,
		Warnings: append([]string(nil), capability.Warnings...),
	}
	if !capability.Runnable {
		if len(capability.MissingCommands) > 0 {
			result.Warnings = append(result.Warnings, "missing commands: "+strings.Join(capability.MissingCommands, ", "))
		}
		return result, nil
	}

	startedAt := time.Now()
	commandContext, cancel := context.WithTimeout(ctx, time.Duration(plan.TimeoutSeconds)*time.Second)
	defer cancel()
	output := newBoundedBuffer(plan.MaxOutputBytes)
	runErr := e.runner.Run(commandContext, plan.Executable, append([]string(nil), plan.Args...), output, output)
	result.DurationMS = time.Since(startedAt).Milliseconds()
	result.Output = strings.ToValidUTF8(output.String(), "�")
	result.Truncated = output.Truncated()
	result.Output, result.Truncated = limitRows(result.Output, plan.MaxRows, result.Truncated)

	switch {
	case commandContext.Err() == context.DeadlineExceeded:
		result.Status = CommandStatusTimeout
		result.Warnings = append(result.Warnings, "command timed out")
	case errors.Is(runErr, ErrRunnerCommandNotFound):
		result.Status = CommandStatusUnsupported
		result.Warnings = append(result.Warnings, "command is unavailable")
	case errors.Is(runErr, ErrRunnerPermission):
		result.Status = CommandStatusPartial
		result.Warnings = append(result.Warnings, "permission denied; output may be incomplete")
	case runErr != nil:
		result.Status = CommandStatusFailed
		result.Warnings = append(result.Warnings, "command failed")
	case result.Truncated || capability.Status == CapabilityPartial:
		result.Status = CommandStatusPartial
	default:
		result.Status = CommandStatusSuccess
	}
	return result, nil
}

// OSRunner executes only the executable and argv selected by Catalog.Plan.
// It deliberately never invokes a shell.
type OSRunner struct{}

func (OSRunner) Run(ctx context.Context, executable string, args []string, stdout, stderr io.Writer) error {
	command := exec.CommandContext(ctx, executable, args...)
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return ErrRunnerCommandNotFound
		}
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			switch exitError.ExitCode() {
			case 126:
				return ErrRunnerPermission
			case 127:
				return ErrRunnerCommandNotFound
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("execute catalog command: %w", err)
	}
	return nil
}

type boundedBuffer struct {
	mu        sync.Mutex
	data      []byte
	max       int64
	truncated bool
}

func newBoundedBuffer(max int64) *boundedBuffer {
	return &boundedBuffer{data: make([]byte, 0, minInt64(max, 4096)), max: max}
}

func (b *boundedBuffer) Write(payload []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	originalLength := len(payload)
	remaining := b.max - int64(len(b.data))
	if remaining <= 0 {
		b.truncated = b.truncated || originalLength > 0
		return originalLength, nil
	}
	if int64(len(payload)) > remaining {
		payload = payload[:remaining]
		b.truncated = true
	}
	b.data = append(b.data, payload...)
	return originalLength, nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(append([]byte(nil), b.data...))
}

func (b *boundedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

func limitRows(output string, maxRows int, alreadyTruncated bool) (string, bool) {
	if maxRows <= 0 || output == "" {
		return output, alreadyTruncated
	}
	trimmedSuffix := strings.HasSuffix(output, "\n")
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	if len(lines) <= maxRows {
		return output, alreadyTruncated
	}
	limited := strings.Join(lines[:maxRows], "\n")
	if trimmedSuffix {
		limited += "\n"
	}
	return limited, true
}

func minInt64(left, right int64) int {
	if left < right {
		return int(left)
	}
	return int(right)
}
