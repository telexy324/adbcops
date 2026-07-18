package linuxserver

import (
	"bufio"
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

func parseSystemOverview(results map[string]*CommandResult, _ time.Duration) (any, []string) {
	os := parseKeyValues(commandOutput(results, "os"), "=")
	mem := parseKeyValues(commandOutput(results, "mem"), ":")
	timeState := parseKeyValues(commandOutput(results, "time"), "=")
	cpu := parseKeyValues(commandOutput(results, "lscpu"), ":")
	uname := strings.Fields(commandOutput(results, "uname"))
	kernel, architecture := "", ""
	if len(uname) > 2 {
		kernel = uname[2]
	}
	for _, value := range uname {
		if value == "x86_64" || value == "aarch64" || value == "arm64" || strings.HasPrefix(value, "ppc64") {
			architecture = value
		}
	}
	hostname := firstLine(commandOutput(results, "hostname"))
	data := map[string]any{
		"hostname": hostname, "fqdn": hostname, "os_name": firstNonEmptyMap(os, "PRETTY_NAME", "NAME"),
		"os_version": os["VERSION_ID"], "kernel": kernel, "architecture": architecture,
		"boot_time":      strings.TrimSpace(strings.TrimPrefix(commandOutput(results, "boot"), "system boot")),
		"uptime_seconds": parseUptimeSeconds(commandOutput(results, "uptime")),
		"virtualization": firstNonEmptyMap(cpu, "Virtualization", "Hypervisor vendor"),
		"cpu_count":      parseInt(firstLine(commandOutput(results, "cpu"))),
		"memory_total":   meminfoBytes(mem, "MemTotal"), "timezone": firstNonEmptyMap(timeState, "Timezone", "Time zone"),
	}
	return data, missingFieldWarnings(data)
}

func parseCPU(results map[string]*CommandResult, interval time.Duration) (any, []string) {
	count := parseInt(firstLine(commandOutput(results, "count")))
	loadFields := strings.Fields(commandOutput(results, "load"))
	load1, load5, load15 := fieldFloat(loadFields, 0), fieldFloat(loadFields, 1), fieldFloat(loadFields, 2)
	usage, ok := cpuUsage(commandOutput(results, "stat1"), commandOutput(results, "stat2"))
	data := map[string]any{
		"cpu_count": count, "cpu_usage_percent": nullableFloat(usage, ok), "load_1m": load1,
		"load_5m": load5, "load_15m": load15, "load_per_cpu": nullableFloat(load5/float64(maxInt(count, 1)), count > 0),
		"top_cpu_processes": parsePS(commandOutput(results, "top")), "sample_interval_ms": interval.Milliseconds(),
	}
	return data, missingFieldWarnings(data)
}

func parseMemory(results map[string]*CommandResult, _ time.Duration) (any, []string) {
	mem := parseKeyValues(commandOutput(results, "meminfo"), ":")
	total, available := meminfoBytes(mem, "MemTotal"), meminfoBytes(mem, "MemAvailable")
	swapTotal, swapFree := meminfoBytes(mem, "SwapTotal"), meminfoBytes(mem, "SwapFree")
	data := map[string]any{
		"mem_total": total, "mem_available": available, "mem_used_percent": percent(total-available, total),
		"swap_total": swapTotal, "swap_used": swapTotal - swapFree, "swap_used_percent": percent(swapTotal-swapFree, swapTotal),
		"cached": meminfoBytes(mem, "Cached"), "buffers": meminfoBytes(mem, "Buffers"),
		"dirty": meminfoBytes(mem, "Dirty"), "slab": meminfoBytes(mem, "Slab"),
		"top_memory_processes": parsePS(commandOutput(results, "top")),
	}
	return data, missingFieldWarnings(data)
}

func parseFilesystem(results map[string]*CommandResult, _ time.Duration) (any, []string) {
	filesystems := parseDF(commandOutput(results, "bytes"), false)
	inodes := parseDF(commandOutput(results, "inodes"), true)
	mounts := parseFindmnt(commandOutput(results, "mounts"))
	inodeByMount := map[string]map[string]any{}
	for _, item := range inodes {
		inodeByMount[item["mountpoint"].(string)] = item
	}
	for _, item := range filesystems {
		if inode := inodeByMount[item["mountpoint"].(string)]; inode != nil {
			item["inode_total"], item["inode_used"], item["inode_used_percent"] = inode["total"], inode["used"], inode["used_percent"]
		}
		mount := mounts[item["mountpoint"].(string)]
		item["read_only"] = mount.readOnly
		item["filesystem_type"] = mount.filesystemType
		item["mount_options_summary"] = mount.options
	}
	warnings := []string{}
	if len(filesystems) == 0 {
		warnings = append(warnings, "filesystem usage could not be parsed")
	}
	return map[string]any{"filesystems": filesystems}, warnings
}

func parseDiskIO(results map[string]*CommandResult, interval time.Duration) (any, []string) {
	devices := diskstatsDelta(commandOutput(results, "disk1"), commandOutput(results, "disk2"), interval)
	capability := "proc_diskstats"
	if parsed := parseIostat(commandOutput(results, "iostat")); len(parsed) > 0 {
		devices = parsed
		capability = "iostat_extended"
	}
	warnings := []string{}
	if len(devices) == 0 {
		warnings = append(warnings, "disk IO counters are unavailable")
	}
	if commandOutput(results, "iostat") == "" {
		warnings = append(warnings, "iostat unavailable; using /proc/diskstats basic counters")
	}
	return map[string]any{"devices": devices, "capability": capability, "sample_interval_ms": interval.Milliseconds()}, warnings
}

func parseNetwork(results map[string]*CommandResult, _ time.Duration) (any, []string) {
	interfaces, addresses, errorsByLink := parseIPAddresses(commandOutput(results, "addresses"))
	routes := parseIPRoutes(commandOutput(results, "routes"))
	data := map[string]any{
		"interfaces": interfaces, "addresses": addresses, "default_routes": routes,
		"dns_servers":     parseNameServers(commandOutput(results, "resolver")),
		"tcp_summary":     parseKeyValues(commandOutput(results, "summary"), ":"),
		"listening_ports": parseListening(commandOutput(results, "listening")),
		"link_errors":     errorsByLink["errors"], "link_drops": errorsByLink["drops"],
	}
	warnings := []string{}
	if commandOutput(results, "addresses") != "" && len(interfaces) == 0 {
		warnings = append(warnings, "network address output could not be parsed")
	}
	return data, warnings
}

func parseProcess(results map[string]*CommandResult, _ time.Duration) (any, []string) {
	all := parseProcessStates(commandOutput(results, "all"))
	data := map[string]any{
		"process_count": all.total, "running": all.running, "sleeping": all.sleeping,
		"zombie": all.zombie, "uninterruptible": all.uninterruptible,
		"top_cpu": parsePS(commandOutput(results, "cpu")), "top_memory": parsePS(commandOutput(results, "memory")),
		"long_running": all.longRunning,
	}
	warnings := []string{}
	if all.total == 0 {
		warnings = append(warnings, "process aggregate output is unavailable or could not be parsed")
	}
	return data, warnings
}

func parseSystemd(results map[string]*CommandResult, _ time.Duration) (any, []string) {
	data := map[string]any{
		"system_state":    firstLine(commandOutput(results, "state")),
		"failed_services": nonEmptyLines(commandOutput(results, "failed"), 100),
	}
	if service := commandOutput(results, "service"); service != "" {
		data["specific_service"] = parseKeyValues(service, "=")
	}
	return data, missingFieldWarnings(data)
}

func parseTimeSync(results map[string]*CommandResult, _ time.Duration) (any, []string) {
	data := map[string]any{
		"timedatectl":     parseKeyValues(commandOutput(results, "timedatectl"), "="),
		"chrony_tracking": parseKeyValues(commandOutput(results, "chrony_tracking"), ":"),
		"chrony_sources":  nonEmptyLines(commandOutput(results, "chrony_sources"), 100),
		"ntp_peers":       nonEmptyLines(commandOutput(results, "ntpq"), 100),
	}
	warnings := []string{}
	if len(data["timedatectl"].(map[string]string)) == 0 && len(data["chrony_tracking"].(map[string]string)) == 0 && len(data["ntp_peers"].([]string)) == 0 {
		warnings = append(warnings, "no supported time synchronization command returned data")
	}
	return data, warnings
}

func parseKernelEvents(results map[string]*CommandResult, _ time.Duration) (any, []string) {
	return parseLogSummary(commandOutput(results, "dmesg") + "\n" + commandOutput(results, "journal")), nil
}

func parseSystemLogs(results map[string]*CommandResult, _ time.Duration) (any, []string) {
	warnings := parseLogSummary(commandOutput(results, "warnings"))
	data := map[string]any{"warnings": warnings}
	if service := commandOutput(results, "service"); service != "" {
		data["service"] = parseLogSummary(service)
	}
	return data, nil
}

func commandOutput(results map[string]*CommandResult, alias string) string {
	result := results[alias]
	if result == nil || (result.Status != CommandStatusSuccess && result.Status != CommandStatusPartial) {
		return ""
	}
	return strings.TrimSpace(result.Output)
}

func parseKeyValues(output, separator string) map[string]string {
	result := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), separator)
		if ok && strings.TrimSpace(key) != "" {
			key = strings.TrimSpace(key)
			value = strings.Trim(strings.TrimSpace(value), `"'`)
			if sensitiveKeyPattern.MatchString(key) {
				value = "[REDACTED]"
			} else {
				value = redactText(value)
			}
			result[key] = value
		}
	}
	return result
}

func firstNonEmptyMap(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if values[key] != "" {
			return values[key]
		}
	}
	return ""
}

func firstLine(value string) string {
	if line, _, ok := strings.Cut(strings.TrimSpace(value), "\n"); ok {
		return strings.TrimSpace(line)
	}
	return strings.TrimSpace(value)
}

func parseInt(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}

func parseUint(value string) uint64 {
	parsed, _ := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	return parsed
}

func parseFloat(value string) float64 {
	value = strings.TrimSuffix(strings.TrimSpace(value), "%")
	parsed, _ := strconv.ParseFloat(value, 64)
	return parsed
}

func meminfoBytes(values map[string]string, key string) uint64 {
	fields := strings.Fields(values[key])
	if len(fields) == 0 {
		return 0
	}
	value := parseUint(fields[0])
	if len(fields) > 1 && strings.EqualFold(fields[1], "kb") {
		value *= 1024
	}
	return value
}

func percent(value, total uint64) any {
	if total == 0 {
		return nil
	}
	return math.Round(float64(value)*10000/float64(total)) / 100
}

func nullableFloat(value float64, valid bool) any {
	if !valid || math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	return math.Round(value*100) / 100

}

func fieldFloat(fields []string, index int) float64 {
	if index >= len(fields) {
		return 0
	}
	return parseFloat(fields[index])
}

func cpuUsage(first, second string) (float64, bool) {
	a, b := cpuCounters(first), cpuCounters(second)
	if len(a) < 5 || len(b) < 5 {
		return 0, false
	}
	var totalA, totalB uint64
	for _, value := range a {
		totalA += value
	}
	for _, value := range b {
		totalB += value
	}
	idleA, idleB := a[3], b[3]
	if len(a) > 4 {
		idleA += a[4]
		idleB += b[4]
	}
	if totalB <= totalA || idleB < idleA {
		return 0, false
	}
	delta := totalB - totalA
	return float64(delta-(idleB-idleA)) * 100 / float64(delta), true
}

func cpuCounters(output string) []uint64 {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 && fields[0] == "cpu" {
			result := make([]uint64, 0, len(fields)-1)
			for _, value := range fields[1:] {
				result = append(result, parseUint(value))
			}
			return result
		}
	}
	return nil
}

func parsePS(output string) []map[string]any {
	result := []map[string]any{}
	for index, line := range strings.Split(output, "\n") {
		if index == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 15 {
			continue
		}
		result = append(result, map[string]any{
			"pid": parseInt(fields[0]), "ppid": parseInt(fields[1]), "user": fields[2], "state": fields[3],
			"cpu_percent": parseFloat(fields[4]), "memory_percent": parseFloat(fields[5]),
			"rss_bytes": parseUint(fields[6]) * 1024, "vsz_bytes": parseUint(fields[7]) * 1024,
			"started_at": strings.Join(fields[8:13], " "), "elapsed": fields[13], "command": strings.Join(fields[14:], " "),
		})
	}
	return result
}

type processStateSummary struct {
	total, running, sleeping, zombie, uninterruptible int
	longRunning                                       []map[string]any
}

func parseProcessStates(output string) processStateSummary {
	result := processStateSummary{longRunning: []map[string]any{}}
	for index, line := range strings.Split(output, "\n") {
		if index == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		result.total++
		switch fields[1][0] {
		case 'R':
			result.running++
		case 'S', 'I':
			result.sleeping++
		case 'Z':
			result.zombie++
		case 'D':
			result.uninterruptible++
		}
		if elapsedSeconds(fields[2]) >= 86400 {
			result.longRunning = append(result.longRunning, map[string]any{"pid": parseInt(fields[0]), "elapsed": fields[2], "command": strings.Join(fields[3:], " ")})
		}
	}
	return result
}

func elapsedSeconds(value string) int64 {
	var days int64
	if before, after, ok := strings.Cut(value, "-"); ok {
		days, _ = strconv.ParseInt(before, 10, 64)
		value = after
	}
	parts := strings.Split(value, ":")
	var seconds int64
	for _, part := range parts {
		seconds = seconds*60 + int64(parseInt(part))
	}
	return days*86400 + seconds
}

func parseUptimeSeconds(output string) int64 {
	match := regexp.MustCompile(`up\s+(?:(\d+)\s+days?,\s*)?(?:(\d+):(\d+)|(?:(\d+)\s+min))`).FindStringSubmatch(output)
	if len(match) == 0 {
		return 0
	}
	days, _ := strconv.ParseInt(match[1], 10, 64)
	hours, _ := strconv.ParseInt(match[2], 10, 64)
	minutes, _ := strconv.ParseInt(firstNonEmpty(match[3], match[4]), 10, 64)
	return days*86400 + hours*3600 + minutes*60
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func nonEmptyLines(output string, max int) []string {
	result := []string{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() && len(result) < max {
		line := strings.TrimSpace(redactText(scanner.Text()))
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

func parseLogSummary(output string) map[string]any {
	lines := nonEmptyLines(output, 20)
	counts := map[string]int{"critical": 0, "error": 0, "warning": 0}
	for _, line := range strings.Split(strings.ToLower(output), "\n") {
		switch {
		case strings.Contains(line, "crit") || strings.Contains(line, "fatal") || strings.Contains(line, "emerg"):
			counts["critical"]++
		case strings.Contains(line, "error") || strings.Contains(line, "failed"):
			counts["error"]++
		case strings.Contains(line, "warn"):
			counts["warning"]++
		}
	}
	return map[string]any{"total": len(nonEmptyLines(output, 100000)), "severity_counts": counts, "samples": lines}
}

func missingFieldWarnings(data map[string]any) []string {
	warnings := []string{}
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := data[key]
		if value == "" || value == nil || value == 0 || value == int64(0) || value == uint64(0) {
			warnings = append(warnings, key+" is unknown")
		}
	}
	return warnings
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func parseDF(output string, inode bool) []map[string]any {
	result := []map[string]any{}
	for index, line := range strings.Split(output, "\n") {
		if index == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		item := map[string]any{"filesystem": fields[0], "mountpoint": strings.Join(fields[5:], " "), "total": parseUint(fields[1]), "used": parseUint(fields[2]), "available": parseUint(fields[3]), "used_percent": parseFloat(fields[4])}
		if !inode {
			item["total_bytes"], item["used_bytes"], item["available_bytes"] = item["total"], item["used"], item["available"]
			delete(item, "total")
			delete(item, "used")
			delete(item, "available")
		}
		result = append(result, item)
	}
	return result
}

type mountMetadata struct {
	filesystemType string
	options        []string
	readOnly       bool
}

type findmntEntry struct {
	Target         string         `json:"target"`
	FilesystemType string         `json:"fstype"`
	Options        string         `json:"options"`
	Children       []findmntEntry `json:"children"`
}

func parseFindmnt(output string) map[string]mountMetadata {
	var document struct {
		Filesystems []findmntEntry `json:"filesystems"`
	}
	if json.Unmarshal([]byte(output), &document) != nil {
		return map[string]mountMetadata{}
	}
	result := map[string]mountMetadata{}
	var visit func([]findmntEntry)
	visit = func(entries []findmntEntry) {
		for _, entry := range entries {
			options := splitNonEmpty(entry.Options, ",")
			readOnly := false
			for _, option := range options {
				if option == "ro" {
					readOnly = true
				}
			}
			result[entry.Target] = mountMetadata{filesystemType: entry.FilesystemType, options: options, readOnly: readOnly}
			visit(entry.Children)
		}
	}
	visit(document.Filesystems)
	return result
}

func splitNonEmpty(value, separator string) []string {
	result := []string{}
	for _, item := range strings.Split(value, separator) {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func parseIPAddresses(output string) ([]map[string]any, []map[string]any, map[string]any) {
	var raw []struct {
		Name      string `json:"ifname"`
		Address   string `json:"address"`
		OperState string `json:"operstate"`
		AddrInfo  []struct {
			Family    string `json:"family"`
			Local     string `json:"local"`
			PrefixLen int    `json:"prefixlen"`
		} `json:"addr_info"`
		Stats struct {
			RX struct{ Errors, Dropped uint64 } `json:"rx"`
			TX struct{ Errors, Dropped uint64 } `json:"tx"`
		} `json:"stats64"`
	}
	_ = json.Unmarshal([]byte(output), &raw)
	interfaces, addresses := []map[string]any{}, []map[string]any{}
	errorsByLink, dropsByLink := map[string]uint64{}, map[string]uint64{}
	for _, item := range raw {
		interfaces = append(interfaces, map[string]any{"name": item.Name, "mac": item.Address, "state": item.OperState})
		for _, address := range item.AddrInfo {
			addresses = append(addresses, map[string]any{"interface": item.Name, "family": address.Family, "address": address.Local, "prefix_length": address.PrefixLen})
		}
		errorsByLink[item.Name] = item.Stats.RX.Errors + item.Stats.TX.Errors
		dropsByLink[item.Name] = item.Stats.RX.Dropped + item.Stats.TX.Dropped
	}
	return interfaces, addresses, map[string]any{"errors": errorsByLink, "drops": dropsByLink}
}

func parseIPRoutes(output string) []map[string]any {
	var raw []map[string]any
	_ = json.Unmarshal([]byte(output), &raw)
	result := []map[string]any{}
	for _, route := range raw {
		destination, _ := route["dst"].(string)
		if destination == "default" || destination == "0.0.0.0/0" || destination == "::/0" {
			result = append(result, route)
		}
	}
	return result
}

func parseNameServers(output string) []string {
	result := []string{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "nameserver" {
			result = append(result, fields[1])
		}
	}
	return result
}

func parseListening(output string) []map[string]any {
	result := []map[string]any{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] == "Netid" {
			continue
		}
		result = append(result, map[string]any{"protocol": fields[0], "state": fields[1], "local_address": fields[4]})
	}
	return result
}
