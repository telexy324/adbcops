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

const (
	CollectorSystemOverview = "system_overview"
	CollectorCPU            = "cpu"
	CollectorMemory         = "memory"
	CollectorFilesystem     = "filesystem"
	CollectorDiskIO         = "disk_io"
	CollectorNetwork        = "network"
	CollectorProcess        = "process"
	CollectorSystemd        = "systemd"
	CollectorTimeSync       = "time_sync"
	CollectorKernelEvents   = "kernel_events"
	CollectorSystemLogs     = "system_logs"
)

var ErrCollectorNotFound = errors.New("linux collector not found")

type collectorCommand struct {
	Alias       string
	Key         string
	Parameters  json.RawMessage
	DelayBefore time.Duration
}

type collectorDefinition struct {
	name     string
	schema   json.RawMessage
	commands func(map[string]any) []collectorCommand
	parse    func(map[string]*CommandResult, time.Duration) (any, []string)
}

type CollectorRegistry struct {
	definitions map[string]collectorDefinition
}

func NewCollectorRegistry() *CollectorRegistry {
	noParameters := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{}}`)
	topN := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"topN":{"type":"integer","minimum":1,"maximum":100,"default":20}}}`)
	since := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"sinceHours":{"type":"integer","minimum":1,"maximum":168,"default":24}}}`)
	service := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"service":{"type":"string","pattern":"^[a-zA-Z0-9_.@:-]+$","maxLength":120}}}`)
	serviceSince := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"service":{"type":"string","pattern":"^[a-zA-Z0-9_.@:-]+$","maxLength":120},"sinceHours":{"type":"integer","minimum":1,"maximum":168,"default":24}}}`)
	registry := &CollectorRegistry{definitions: map[string]collectorDefinition{}}
	registry.add(collectorDefinition{CollectorSystemOverview, noParameters, fixedCommands(
		"hostname=system.hostname", "os=system.os_release", "uname=system.uname", "uptime=system.uptime",
		"boot=system.boot_time", "cpu=cpu.count", "mem=memory.meminfo", "time=time.timedatectl", "lscpu=cpu.lscpu",
	), parseSystemOverview})
	registry.add(collectorDefinition{CollectorCPU, topN, func(values map[string]any) []collectorCommand {
		params := mustJSON(map[string]any{"topN": values["topN"]})
		return []collectorCommand{{Alias: "count", Key: "cpu.count"}, {Alias: "load", Key: "cpu.loadavg"},
			{Alias: "stat1", Key: "cpu.stat"}, {Alias: "stat2", Key: "cpu.stat", DelayBefore: time.Second},
			{Alias: "top", Key: "process.top_cpu", Parameters: params}}
	}, parseCPU})
	registry.add(collectorDefinition{CollectorMemory, topN, func(values map[string]any) []collectorCommand {
		params := mustJSON(map[string]any{"topN": values["topN"]})
		return []collectorCommand{{Alias: "meminfo", Key: "memory.meminfo"}, {Alias: "free", Key: "memory.free"},
			{Alias: "swap", Key: "memory.swap"}, {Alias: "top", Key: "process.top_memory", Parameters: params}}
	}, parseMemory})
	registry.add(collectorDefinition{CollectorFilesystem, noParameters, fixedCommands(
		"bytes=filesystem.df_bytes", "inodes=filesystem.df_inodes", "mounts=filesystem.findmnt", "blocks=filesystem.lsblk",
	), parseFilesystem})
	registry.add(collectorDefinition{CollectorDiskIO, noParameters, func(map[string]any) []collectorCommand {
		return []collectorCommand{{Alias: "iostat", Key: "diskio.iostat"}, {Alias: "disk1", Key: "diskio.proc"},
			{Alias: "disk2", Key: "diskio.proc", DelayBefore: time.Second}}
	}, parseDiskIO})
	registry.add(collectorDefinition{CollectorNetwork, noParameters, fixedCommands(
		"addresses=network.address", "routes=network.route", "summary=network.socket_summary",
		"listening=network.listening", "resolver=network.resolver",
	), parseNetwork})
	registry.add(collectorDefinition{CollectorProcess, topN, func(values map[string]any) []collectorCommand {
		params := mustJSON(map[string]any{"topN": values["topN"]})
		return []collectorCommand{{Alias: "all", Key: "process.all"}, {Alias: "cpu", Key: "process.top_cpu", Parameters: params},
			{Alias: "memory", Key: "process.top_memory", Parameters: params}}
	}, parseProcess})
	registry.add(collectorDefinition{CollectorSystemd, service, func(values map[string]any) []collectorCommand {
		commands := fixedCommands("state=systemd.state", "failed=systemd.failed")(values)
		if name, ok := values["service"].(string); ok && name != "" {
			commands = append(commands, collectorCommand{Alias: "service", Key: "systemd.show", Parameters: mustJSON(map[string]any{"service": name})})
		}
		return commands
	}, parseSystemd})
	registry.add(collectorDefinition{CollectorTimeSync, noParameters, fixedCommands(
		"timedatectl=time.timedatectl", "chrony_tracking=time.chrony_tracking", "chrony_sources=time.chrony_sources", "ntpq=time.ntpq",
	), parseTimeSync})
	registry.add(collectorDefinition{CollectorKernelEvents, since, func(values map[string]any) []collectorCommand {
		params := mustJSON(map[string]any{"sinceHours": values["sinceHours"]})
		return []collectorCommand{{Alias: "dmesg", Key: "kernel.dmesg"}, {Alias: "journal", Key: "kernel.journal", Parameters: params}}
	}, parseKernelEvents})
	registry.add(collectorDefinition{CollectorSystemLogs, serviceSince, func(values map[string]any) []collectorCommand {
		params := mustJSON(map[string]any{"sinceHours": values["sinceHours"]})
		commands := []collectorCommand{{Alias: "warnings", Key: "logs.warning", Parameters: params}}
		if name, ok := values["service"].(string); ok && name != "" {
			commands = append(commands, collectorCommand{Alias: "service", Key: "logs.service", Parameters: mustJSON(map[string]any{"service": name, "sinceHours": values["sinceHours"]})})
		}
		return commands
	}, parseSystemLogs})
	return registry
}

func (r *CollectorRegistry) add(definition collectorDefinition) {
	r.definitions[definition.name] = definition
}

func (r *CollectorRegistry) get(name string, parameters json.RawMessage) (collectorDefinition, map[string]any, error) {
	definition, ok := r.definitions[strings.TrimSpace(name)]
	if !ok {
		return collectorDefinition{}, nil, ErrCollectorNotFound
	}
	values, err := validateParameters(definition.schema, parameters)
	if err != nil {
		return collectorDefinition{}, nil, err
	}
	return definition, values, nil
}

func (r *CollectorRegistry) Names() []string {
	names := make([]string, 0, len(r.definitions))
	for name := range r.definitions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func fixedCommands(items ...string) func(map[string]any) []collectorCommand {
	return func(map[string]any) []collectorCommand {
		commands := make([]collectorCommand, 0, len(items))
		for _, item := range items {
			alias, key, _ := strings.Cut(item, "=")
			commands = append(commands, collectorCommand{Alias: alias, Key: key})
		}
		return commands
	}
}

func mustJSON(value any) json.RawMessage {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return payload
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func aggregateCollectorStatus(results map[string]*CommandResult) (string, []string, bool, time.Duration) {
	status := CommandStatusSuccess
	warnings := []string{}
	truncated := false
	duration := time.Duration(0)
	usable := 0
	timedOut := false
	for alias, result := range results {
		duration += time.Duration(result.DurationMS) * time.Millisecond
		truncated = truncated || result.Truncated
		if result.Status == CommandStatusSuccess || result.Status == CommandStatusPartial {
			usable++
		}
		if result.Status != CommandStatusSuccess {
			status = CommandStatusPartial
			warnings = append(warnings, fmt.Sprintf("%s: %s", alias, result.Status))
		}
		if result.Status == CommandStatusTimeout {
			timedOut = true
		}
		for _, warning := range result.Warnings {
			warnings = append(warnings, alias+": "+warning)
		}
	}
	if timedOut {
		status = CommandStatusTimeout
	} else if usable == 0 {
		status = CommandStatusUnsupported
	}
	return status, warnings, truncated, duration
}
