package linuxserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Tool struct {
	catalog    *Catalog
	collectors *CollectorRegistry
	pool       *ConnectionPool
	parser     OutputParser
	now        func() time.Time
	sleep      func(context.Context, time.Duration) error
}

func NewTool(catalog *Catalog, pool *ConnectionPool, parser OutputParser) (*Tool, error) {
	if catalog == nil || pool == nil {
		return nil, ErrInvalidDefinition
	}
	if parser == nil {
		parser = SafeOutputParser{}
	}
	if err := validateParser(parser); err != nil {
		return nil, err
	}
	return &Tool{
		catalog: catalog, collectors: NewCollectorRegistry(), pool: pool, parser: parser,
		now: time.Now, sleep: sleepContext,
	}, nil
}

func NewDefaultTool() *Tool {
	tool, err := NewTool(NewBuiltinCatalog(), NewConnectionPool(nil, PoolOptions{}), SafeOutputParser{})
	if err != nil {
		panic(err)
	}
	return tool
}

func (t *Tool) Test(ctx context.Context, conn LinuxServerConnection) (*LinuxConnectionTestResult, error) {
	startedAt := t.now()
	lease, err := t.pool.Acquire(ctx, conn, true)
	if err != nil {
		return &LinuxConnectionTestResult{
			Status: CommandStatusFailed, LatencyMs: t.now().Sub(startedAt).Milliseconds(), AuthMethod: conn.AuthType,
			ErrorCode: connectionErrorCode(err), Message: safeConnectionMessage(err),
		}, err
	}
	defer lease.Release(false)
	observation := lease.Client().HostKey()
	confirmationRequired := strings.TrimSpace(conn.HostKeyFingerprint) == ""
	return &LinuxConnectionTestResult{
		Status: CommandStatusSuccess, LatencyMs: t.now().Sub(startedAt).Milliseconds(),
		ServerVersion: lease.Client().ServerVersion(), AuthMethod: conn.AuthType,
		HostKeyAlgorithm: observation.Algorithm, HostKeyFingerprint: observation.Fingerprint,
		ConfirmationRequired: confirmationRequired,
	}, nil
}

func (t *Tool) DetectPlatform(ctx context.Context, conn LinuxServerConnection) (*LinuxPlatformInfo, error) {
	lease, err := t.pool.Acquire(ctx, conn, false)
	if err != nil {
		return nil, err
	}
	discard := false
	defer func() { lease.Release(discard) }()
	platform, failed := t.detectWithClient(ctx, lease.Client())
	discard = failed
	if failed {
		return nil, errors.New("detect linux platform failed")
	}
	return &platform, nil
}

func (t *Tool) Collect(ctx context.Context, conn LinuxServerConnection, request LinuxCollectRequest) (*LinuxCollectResult, error) {
	if strings.TrimSpace(request.Collector) == "" {
		return nil, ErrCollectorNotFound
	}
	if request.TimeStart != nil && request.TimeEnd != nil && request.TimeStart.After(*request.TimeEnd) {
		return nil, ErrInvalidParameters
	}
	definition, values, err := t.collectors.get(request.Collector, request.Parameters)
	if err != nil {
		return nil, err
	}
	lease, err := t.pool.Acquire(ctx, conn, false)
	if err != nil {
		return nil, err
	}
	discard := false
	defer func() { lease.Release(discard) }()
	platform, failed := t.detectWithClient(ctx, lease.Client())
	if failed {
		discard = true
		return nil, errors.New("detect linux platform failed")
	}
	executor, _ := NewExecutor(t.catalog, lease.Client())
	results := map[string]*CommandResult{}
	versions := map[string]bool{}
	maxDelay := time.Duration(0)
	for _, command := range definition.commands(values) {
		if command.DelayBefore > 0 {
			if err := t.sleep(ctx, command.DelayBefore); err != nil {
				results[command.Alias] = &CommandResult{Key: command.Key, Status: CommandStatusTimeout, Warnings: []string{"sampling interrupted"}}
				discard = true
				break
			}
			if command.DelayBefore > maxDelay {
				maxDelay = command.DelayBefore
			}
		}
		result, executeErr := executor.Execute(ctx, CommandRequest{Key: command.Key, Parameters: command.Parameters, Platform: platform})
		if executeErr != nil {
			return nil, executeErr
		}
		results[command.Alias] = result
		versions[result.CommandVersion] = true
		if result.Status == CommandStatusTimeout {
			discard = true
		}
	}
	status, warnings, truncated, duration := aggregateCollectorStatus(results)
	duration += maxDelay
	parsed, parserWarnings := definition.parse(results, maxDelay)
	if len(parserWarnings) > 0 {
		warnings = append(warnings, parserWarnings...)
		if status == CommandStatusSuccess {
			status = CommandStatusPartial
		}
	}
	data, err := json.Marshal(parsed)
	if err != nil {
		return nil, fmt.Errorf("marshal collector result: %w", err)
	}
	return &LinuxCollectResult{
		Collector: request.Collector, CommandVersion: combineVersions(versions), Status: status, Data: data,
		Warnings: warnings, CollectedAt: t.now().UTC(), DurationMs: duration.Milliseconds(), Truncated: truncated,
	}, nil
}

func combineVersions(versions map[string]bool) string {
	values := make([]string, 0, len(versions))
	for version := range versions {
		if version != "" {
			values = append(values, version)
		}
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}

func (t *Tool) Close() error { return t.pool.Close() }

func (t *Tool) detectWithClient(ctx context.Context, client RemoteClient) (LinuxPlatformInfo, bool) {
	osRelease, osStatus := t.executeDetectionCommand(ctx, client, "system.os_release", nil)
	kernel, kernelStatus := t.executeDetectionCommand(ctx, client, "system.uname", nil)
	if osStatus == CommandStatusTimeout || kernelStatus == CommandStatusTimeout {
		return LinuxPlatformInfo{}, true
	}
	commands := uniqueCatalogExecutables(t.catalog.List())
	availableSet := map[string]bool{}
	if osStatus == CommandStatusSuccess {
		availableSet["cat"] = true
	}
	if kernelStatus == CommandStatusSuccess {
		availableSet["uname"] = true
	}
	for _, command := range commands {
		parameters, _ := json.Marshal(map[string]string{"command": command})
		_, status := t.executeDetectionCommand(ctx, client, "platform.which", parameters)
		if status == CommandStatusSuccess {
			availableSet[command] = true
		}
		if status == CommandStatusTimeout || ctx.Err() != nil {
			return LinuxPlatformInfo{}, true
		}
	}
	available := make([]string, 0, len(availableSet))
	for command := range availableSet {
		available = append(available, command)
	}
	sort.Strings(available)
	platform := DetectPlatform(osRelease, kernel, available)
	if osStatus != CommandStatusSuccess {
		platform.Status = CapabilityPartial
		platform.Warnings = append(platform.Warnings, "operating system release metadata is unavailable")
	}
	return platform, false
}

func (t *Tool) executeDetectionCommand(ctx context.Context, client RemoteClient, key string, parameters json.RawMessage) (string, string) {
	plan, err := t.catalog.Plan(key, parameters)
	if err != nil {
		return "", CommandStatusFailed
	}
	timeoutContext, cancel := context.WithTimeout(ctx, time.Duration(plan.TimeoutSeconds)*time.Second)
	defer cancel()
	buffer := newBoundedBuffer(plan.MaxOutputBytes)
	err = client.Run(timeoutContext, plan.Executable, plan.Args, buffer, buffer)
	if timeoutContext.Err() == context.DeadlineExceeded {
		return buffer.String(), CommandStatusTimeout
	}
	if errors.Is(err, ErrRunnerCommandNotFound) {
		return buffer.String(), CommandStatusUnsupported
	}
	if err != nil {
		return buffer.String(), CommandStatusFailed
	}
	return buffer.String(), CommandStatusSuccess
}

func uniqueCatalogExecutables(definitions []LinuxCommandDefinition) []string {
	set := map[string]struct{}{}
	for _, definition := range definitions {
		set[definition.Executable] = struct{}{}
	}
	result := make([]string, 0, len(set))
	for executable := range set {
		result = append(result, executable)
	}
	sort.Strings(result)
	return result
}

func connectionErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrAuthenticationFailed):
		return ErrorAuthFailed
	case errors.Is(err, ErrHostKeyMismatch), errors.Is(err, ErrHostKeyConfirmationRequired):
		return ErrorHostKeyMismatch
	case strings.Contains(err.Error(), ErrorDNSFailed):
		return ErrorDNSFailed
	case strings.Contains(err.Error(), ErrorConnectTimeout), errors.Is(err, context.DeadlineExceeded):
		return ErrorConnectTimeout
	case strings.Contains(err.Error(), ErrorConnectionRefused):
		return ErrorConnectionRefused
	default:
		return ErrorUnknown
	}
}

func safeConnectionMessage(err error) string {
	switch connectionErrorCode(err) {
	case ErrorAuthFailed:
		return "SSH authentication failed"
	case ErrorHostKeyMismatch:
		return "SSH host key verification failed"
	case ErrorDNSFailed:
		return "host name resolution failed"
	case ErrorConnectTimeout:
		return "SSH connection timed out"
	case ErrorConnectionRefused:
		return "SSH connection was refused"
	default:
		return fmt.Sprintf("SSH connection failed (%s)", ErrorUnknown)
	}
}
