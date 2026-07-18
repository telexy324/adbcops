package linuxserver

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCollectorParserFixtures(t *testing.T) {
	tests := []struct {
		name     string
		parse    func(map[string]*CommandResult, time.Duration) (any, []string)
		fixture  map[string]string
		contains []string
	}{
		{
			name: "system_overview", parse: parseSystemOverview,
			fixture: map[string]string{
				"hostname": "app01\n", "os": "ID=ubuntu\nVERSION_ID=22.04\nPRETTY_NAME=Ubuntu 22.04 LTS\n",
				"uname": "Linux app01 5.15.0 #1 x86_64 GNU/Linux", "uptime": "10:00 up 2 days,  3:04, 1 user",
				"boot": "system boot  2026-07-16 06:56", "cpu": "4", "mem": "MemTotal: 8192 kB",
				"time": "Timezone=Asia/Shanghai", "lscpu": "Virtualization: VT-x",
			}, contains: []string{`"hostname":"app01"`, `"os_version":"22.04"`, `"cpu_count":4`, `"memory_total":8388608`},
		},
		{
			name: "cpu", parse: parseCPU,
			fixture: map[string]string{
				"count": "4", "load": "1.00 2.00 3.00 1/100 1", "stat1": "cpu  100 0 100 800 0 0 0 0",
				"stat2": "cpu  200 0 200 900 0 0 0 0", "top": psFixture("42 1 ops R 80.0 1.0 100 200 Mon Jul 18 10:00:00 2026 00:10 app"),
			}, contains: []string{`"cpu_count":4`, `"cpu_usage_percent":66.67`, `"load_per_cpu":0.5`, `"pid":42`},
		},
		{
			name: "memory", parse: parseMemory,
			fixture: map[string]string{
				"meminfo": "MemTotal: 1000 kB\nMemAvailable: 250 kB\nSwapTotal: 200 kB\nSwapFree: 50 kB\nCached: 10 kB\nBuffers: 5 kB\nDirty: 1 kB\nSlab: 2 kB",
				"top":     psFixture("7 1 ops S 1.0 40.0 100 200 Mon Jul 18 10:00:00 2026 1-00:00 app"),
			}, contains: []string{`"mem_used_percent":75`, `"swap_used":153600`, `"pid":7`},
		},
		{
			name: "filesystem", parse: parseFilesystem,
			fixture: map[string]string{
				"bytes":  "Filesystem 1-blocks Used Available Capacity Mounted on\n/dev/sda1 1000 750 250 75% /",
				"inodes": "Filesystem Inodes IUsed IFree IUse% Mounted on\n/dev/sda1 100 20 80 20% /",
				"mounts": `{"filesystems":[{"target":"/","source":"/dev/sda1","fstype":"ext4","options":"ro,relatime"}]}`,
			}, contains: []string{`"mountpoint":"/"`, `"used_bytes":750`, `"inode_used_percent":20`, `"filesystem_type":"ext4"`, `"read_only":true`},
		},
		{
			name: "disk_io", parse: parseDiskIO,
			fixture: map[string]string{
				"disk1": "8 0 sda 10 0 20 0 30 0 40 0 0 100 0", "disk2": "8 0 sda 12 0 24 0 33 0 46 0 1 120 0",
			}, contains: []string{`"device":"sda"`, `"reads_per_second":2`, `"write_bytes_per_second":3072`, `"capability":"proc_diskstats"`},
		},
		{
			name: "network", parse: parseNetwork,
			fixture: map[string]string{
				"addresses": `[{"ifname":"eth0","address":"00:11","operstate":"UP","addr_info":[{"family":"inet","local":"10.0.0.2","prefixlen":24}],"stats64":{"rx":{"errors":1,"dropped":2},"tx":{"errors":3,"dropped":4}}}]`,
				"routes":    `[{"dst":"default","gateway":"10.0.0.1","dev":"eth0"}]`, "resolver": "nameserver 10.0.0.53",
				"summary": "Total: 10\nTCP: 5", "listening": "Netid State Recv-Q Send-Q Local Address:Port\ntcp LISTEN 0 128 0.0.0.0:22",
			}, contains: []string{`"name":"eth0"`, `"address":"10.0.0.2"`, `"10.0.0.53"`, `"eth0":4`},
		},
		{
			name: "process", parse: parseProcess,
			fixture: map[string]string{
				"all": "PID STAT ELAPSED COMMAND\n1 S 2-00:00 init\n2 Z 00:10 child\n3 D 00:20 io", "cpu": psFixture("2 1 ops Z 10 1 1 2 Mon Jul 18 10:00:00 2026 00:10 child"),
				"memory": psFixture("1 0 root S 1 2 3 4 Mon Jul 18 10:00:00 2026 2-00:00 init"),
			}, contains: []string{`"process_count":3`, `"zombie":1`, `"uninterruptible":1`, `"command":"init"`},
		},
		{
			name: "systemd", parse: parseSystemd,
			fixture:  map[string]string{"state": "degraded", "failed": "nginx.service loaded failed failed Nginx", "service": "ActiveState=failed\nSubState=failed"},
			contains: []string{`"system_state":"degraded"`, `"nginx.service`, `"ActiveState":"failed"`},
		},
		{
			name: "time_sync", parse: parseTimeSync,
			fixture:  map[string]string{"timedatectl": "NTPSynchronized=yes\nTimezone=UTC", "chrony_tracking": "Leap status : Normal", "chrony_sources": "^* ntp1", "ntpq": "*10.0.0.1"},
			contains: []string{`"NTPSynchronized":"yes"`, `"Leap status":"Normal"`, `"*10.0.0.1"`},
		},
		{
			name: "kernel_events", parse: parseKernelEvents,
			fixture:  map[string]string{"dmesg": "kernel: warning disk reset", "journal": "kernel: error I/O failed"},
			contains: []string{`"total":2`, `"error":1`, `"warning":1`},
		},
		{
			name: "system_logs", parse: parseSystemLogs,
			fixture:  map[string]string{"warnings": "app warning retry\napp error failed", "service": "nginx fatal worker exited"},
			contains: []string{`"warnings"`, `"total":2`, `"service"`, `"critical":1`},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			results := map[string]*CommandResult{}
			for alias, output := range test.fixture {
				results[alias] = successfulCommand(output)
			}
			value, _ := test.parse(results, time.Second)
			payload, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			for _, expected := range test.contains {
				if !strings.Contains(string(payload), expected) {
					t.Errorf("fixture output %s does not contain %s", payload, expected)
				}
			}
		})
	}
}

func TestSystemOverviewFixturesSupportRHELCentOSAndUbuntu(t *testing.T) {
	for _, fixture := range []struct{ id, version, name string }{
		{"rhel", "9.4", "Red Hat Enterprise Linux 9.4"},
		{"centos", "7", "CentOS Linux 7"},
		{"ubuntu", "24.04", "Ubuntu 24.04 LTS"},
	} {
		results := map[string]*CommandResult{
			"os": successfulCommand("ID=" + fixture.id + "\nVERSION_ID=" + fixture.version + "\nPRETTY_NAME=\"" + fixture.name + "\""),
		}
		value, _ := parseSystemOverview(results, 0)
		payload, _ := json.Marshal(value)
		if !strings.Contains(string(payload), `"os_version":"`+fixture.version+`"`) || !strings.Contains(string(payload), fixture.name) {
			t.Fatalf("fixture %s parsed as %s", fixture.id, payload)
		}
	}
}

func TestCollectorParametersEnforceTopNLogWindowAndServiceName(t *testing.T) {
	registry := NewCollectorRegistry()
	for _, test := range []struct {
		collector string
		params    string
	}{
		{CollectorProcess, `{"topN":101}`},
		{CollectorCPU, `{"topN":0}`},
		{CollectorKernelEvents, `{"sinceHours":169}`},
		{CollectorSystemLogs, `{"sinceHours":0}`},
		{CollectorSystemd, `{"service":"nginx.service;restart"}`},
		{CollectorSystemLogs, `{"service":"nginx.service\nwhoami"}`},
	} {
		if _, _, err := registry.get(test.collector, json.RawMessage(test.params)); err != ErrInvalidParameters {
			t.Errorf("get(%s, %s) error = %v", test.collector, test.params, err)
		}
	}
	definition, values, err := registry.get(CollectorProcess, nil)
	if err != nil {
		t.Fatal(err)
	}
	commands := definition.commands(values)
	for _, command := range commands {
		if strings.HasPrefix(command.Key, "process.top_") && !strings.Contains(string(command.Parameters), `"topN":20`) {
			t.Errorf("default TopN missing from %+v", command)
		}
	}
}

func TestCollectorCommandPlansStayInsideReadOnlyCatalog(t *testing.T) {
	catalog := NewBuiltinCatalog()
	registry := NewCollectorRegistry()
	for _, name := range registry.Names() {
		definition, values, err := registry.get(name, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, command := range definition.commands(values) {
			plan, err := catalog.Plan(command.Key, command.Parameters)
			if err != nil {
				t.Fatalf("%s command %+v: %v", name, command, err)
			}
			if plan.Executable == "sh" || plan.Executable == "bash" || strings.Contains(strings.Join(plan.Args, " "), "restart") {
				t.Fatalf("unsafe collector plan: %+v", plan)
			}
		}
	}
}

func TestCollectorPropertiesRedactSensitiveValues(t *testing.T) {
	value, _ := parseSystemd(map[string]*CommandResult{
		"state":   successfulCommand("running"),
		"failed":  successfulCommand(""),
		"service": successfulCommand("Environment=PASSWORD=super-secret\nAccessToken=abc123"),
	}, 0)
	payload, _ := json.Marshal(value)
	if strings.Contains(string(payload), "super-secret") || strings.Contains(string(payload), "abc123") || !strings.Contains(string(payload), "[REDACTED]") {
		t.Fatalf("sensitive systemd properties leaked: %s", payload)
	}
}

func successfulCommand(output string) *CommandResult {
	return &CommandResult{Status: CommandStatusSuccess, Output: output, CommandVersion: "1.0.0"}
}

func psFixture(row string) string {
	return "PID PPID USER STAT %CPU %MEM RSS VSZ STARTED ELAPSED COMMAND\n" + row
}
