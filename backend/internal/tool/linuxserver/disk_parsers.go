package linuxserver

import (
	"sort"
	"strings"
	"time"
)

type diskCounters struct {
	reads, sectorsRead, writes, sectorsWritten, ioMillis, inFlight uint64
}

func diskstatsDelta(first, second string, interval time.Duration) []map[string]any {
	a, b := parseDiskstats(first), parseDiskstats(second)
	seconds := interval.Seconds()
	if seconds <= 0 {
		seconds = 1
	}
	result := []map[string]any{}
	for device, current := range b {
		previous, ok := a[device]
		if !ok || current.reads < previous.reads || current.writes < previous.writes || current.ioMillis < previous.ioMillis ||
			current.sectorsRead < previous.sectorsRead || current.sectorsWritten < previous.sectorsWritten {
			continue
		}
		result = append(result, map[string]any{
			"device": device, "reads_per_second": float64(current.reads-previous.reads) / seconds,
			"writes_per_second":      float64(current.writes-previous.writes) / seconds,
			"read_bytes_per_second":  float64(current.sectorsRead-previous.sectorsRead) * 512 / seconds,
			"write_bytes_per_second": float64(current.sectorsWritten-previous.sectorsWritten) * 512 / seconds,
			"await_ms":               nil, "util_percent": float64(current.ioMillis-previous.ioMillis) / (seconds * 10),
			"queue_depth": current.inFlight,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i]["device"].(string) < result[j]["device"].(string) })
	return result
}

func parseDiskstats(output string) map[string]diskCounters {
	result := map[string]diskCounters{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 14 {
			continue
		}
		result[fields[2]] = diskCounters{
			reads: parseUint(fields[3]), sectorsRead: parseUint(fields[5]), writes: parseUint(fields[7]),
			sectorsWritten: parseUint(fields[9]), inFlight: parseUint(fields[11]), ioMillis: parseUint(fields[12]),
		}
	}
	return result
}

func parseIostat(output string) []map[string]any {
	lines := strings.Split(output, "\n")
	header := map[string]int{}
	result := []map[string]any{}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "Device" || fields[0] == "Device:" {
			header = map[string]int{}
			for index, field := range fields {
				header[strings.TrimSuffix(field, ":")] = index
			}
			result = nil // iostat prints two reports; retain the last one.
			continue
		}
		if len(header) == 0 || len(fields) < 3 || strings.HasPrefix(fields[0], "avg-cpu") {
			continue
		}
		value := func(names ...string) float64 {
			for _, name := range names {
				if index, ok := header[name]; ok && index < len(fields) {
					return parseFloat(fields[index])
				}
			}
			return 0
		}
		result = append(result, map[string]any{
			"device": fields[0], "reads_per_second": value("r/s"), "writes_per_second": value("w/s"),
			"read_bytes_per_second":  value("rMB/s")*1024*1024 + value("rkB/s")*1024,
			"write_bytes_per_second": value("wMB/s")*1024*1024 + value("wkB/s")*1024,
			"await_ms":               value("await"), "util_percent": value("%util"), "queue_depth": value("aqu-sz", "avgqu-sz"),
		})
	}
	return result
}
