package linuxserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

const (
	RiskSafeRead      = "safe_read"
	RiskSensitiveRead = "sensitive_read"
)

var (
	ErrCommandNotFound   = errors.New("linux command definition not found")
	ErrInvalidDefinition = errors.New("invalid linux command definition")
	ErrInvalidParameters = errors.New("invalid linux command parameters")
)

var allowedReadOnlyExecutables = map[string]struct{}{
	"cat": {}, "chronyc": {}, "df": {}, "dmesg": {}, "findmnt": {}, "free": {},
	"hostnamectl": {}, "iostat": {}, "ip": {}, "journalctl": {}, "lscpu": {}, "lsblk": {},
	"nproc": {}, "ntpq": {}, "ps": {}, "ss": {}, "swapon": {}, "systemctl": {},
	"timedatectl": {}, "uname": {}, "uptime": {}, "which": {}, "who": {},
}

type LinuxCommandDefinition struct {
	Key               string          `json:"key"`
	Version           string          `json:"version"`
	Description       string          `json:"description"`
	Executable        string          `json:"executable"`
	ArgsTemplate      []string        `json:"argsTemplate"`
	AllowedParameters json.RawMessage `json:"allowedParameters"`
	RiskLevel         string          `json:"riskLevel"`
	TimeoutSeconds    int             `json:"timeoutSeconds"`
	MaxOutputBytes    int64           `json:"maxOutputBytes"`
	MaxRows           int             `json:"maxRows"`
	RequiredCommands  []string        `json:"requiredCommands"`
	SupportedOS       []string        `json:"supportedOS"`
	EnabledByDefault  bool            `json:"enabledByDefault"`
}

type CommandPlan struct {
	Key            string
	Version        string
	Executable     string
	Args           []string
	RiskLevel      string
	TimeoutSeconds int
	MaxOutputBytes int64
	MaxRows        int
}

type Catalog struct {
	definitions map[string]LinuxCommandDefinition
}

func NewCatalog(definitions ...LinuxCommandDefinition) (*Catalog, error) {
	catalog := &Catalog{definitions: make(map[string]LinuxCommandDefinition, len(definitions))}
	for _, definition := range definitions {
		if err := validateDefinition(definition); err != nil {
			return nil, fmt.Errorf("%w: %s", err, definition.Key)
		}
		if _, exists := catalog.definitions[definition.Key]; exists {
			return nil, fmt.Errorf("%w: duplicate key %s", ErrInvalidDefinition, definition.Key)
		}
		definition.ArgsTemplate = append([]string(nil), definition.ArgsTemplate...)
		definition.RequiredCommands = append([]string(nil), definition.RequiredCommands...)
		definition.SupportedOS = append([]string(nil), definition.SupportedOS...)
		catalog.definitions[definition.Key] = definition
	}
	return catalog, nil
}

func NewBuiltinCatalog() *Catalog {
	catalog, err := NewCatalog(BuiltinCommandDefinitions()...)
	if err != nil {
		panic(err)
	}
	return catalog
}

func (c *Catalog) Get(key string) (LinuxCommandDefinition, error) {
	definition, ok := c.definitions[strings.TrimSpace(key)]
	if !ok || !definition.EnabledByDefault {
		return LinuxCommandDefinition{}, ErrCommandNotFound
	}
	definition.ArgsTemplate = append([]string(nil), definition.ArgsTemplate...)
	definition.RequiredCommands = append([]string(nil), definition.RequiredCommands...)
	definition.SupportedOS = append([]string(nil), definition.SupportedOS...)
	return definition, nil
}

func (c *Catalog) List() []LinuxCommandDefinition {
	definitions := make([]LinuxCommandDefinition, 0, len(c.definitions))
	for _, definition := range c.definitions {
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Key < definitions[j].Key })
	return definitions
}

func (c *Catalog) Plan(key string, parameters json.RawMessage) (CommandPlan, error) {
	definition, err := c.Get(key)
	if err != nil {
		return CommandPlan{}, err
	}
	values, err := validateParameters(definition.AllowedParameters, parameters)
	if err != nil {
		return CommandPlan{}, err
	}
	args := make([]string, len(definition.ArgsTemplate))
	for index, template := range definition.ArgsTemplate {
		expanded, err := expandArgument(template, values)
		if err != nil {
			return CommandPlan{}, err
		}
		args[index] = expanded
	}
	maxRows := definition.MaxRows
	if topN, ok := values["topN"].(int64); ok {
		maxRows = int(topN) + 1 // ps emits one header row before the bounded process rows.
	}
	return CommandPlan{
		Key: definition.Key, Version: definition.Version, Executable: definition.Executable,
		Args: args, RiskLevel: definition.RiskLevel, TimeoutSeconds: definition.TimeoutSeconds,
		MaxOutputBytes: definition.MaxOutputBytes, MaxRows: maxRows,
	}, nil
}

func validateDefinition(definition LinuxCommandDefinition) error {
	if definition.Key == "" || definition.Version == "" || definition.Description == "" ||
		definition.TimeoutSeconds < 1 || definition.TimeoutSeconds > 60 ||
		definition.MaxOutputBytes < 1024 || definition.MaxOutputBytes > 1024*1024 ||
		definition.MaxRows < 1 || definition.MaxRows > 10000 ||
		(definition.RiskLevel != RiskSafeRead && definition.RiskLevel != RiskSensitiveRead) ||
		!json.Valid(definition.AllowedParameters) {
		return ErrInvalidDefinition
	}
	executable := filepath.Base(strings.TrimSpace(definition.Executable))
	if executable == "" || executable != definition.Executable || executable == "sh" || executable == "bash" || executable == "eval" {
		return ErrInvalidDefinition
	}
	if _, allowed := allowedReadOnlyExecutables[executable]; !allowed {
		return ErrInvalidDefinition
	}
	for _, argument := range definition.ArgsTemplate {
		if strings.ContainsAny(argument, "\n\r") || dangerousFixedArgument(argument) {
			return ErrInvalidDefinition
		}
	}
	return nil
}

func dangerousFixedArgument(argument string) bool {
	normalized := strings.ToLower(strings.TrimSpace(argument))
	switch normalized {
	case "start", "stop", "restart", "reload", "enable", "disable", "mask", "unmask",
		"delete", "del", "add", "set", "flush", "write", "--delete", "--remove":
		return true
	default:
		return false
	}
}

func expandArgument(template string, values map[string]any) (string, error) {
	result := template
	for {
		start := strings.Index(result, "${")
		if start < 0 {
			break
		}
		endOffset := strings.Index(result[start+2:], "}")
		if endOffset < 0 {
			return "", ErrInvalidDefinition
		}
		end := start + 2 + endOffset
		name := result[start+2 : end]
		value, exists := values[name]
		if !exists {
			return "", ErrInvalidParameters
		}
		result = result[:start] + fmt.Sprint(value) + result[end+1:]
	}
	if strings.ContainsAny(result, "\n\r") {
		return "", ErrInvalidParameters
	}
	return result, nil
}

func BuiltinCommandDefinitions() []LinuxCommandDefinition {
	const version = "1.0.0"
	allLinux := []string{"rhel", "centos", "rocky", "almalinux", "ubuntu", "debian"}
	noParameters := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{}}`)
	topNParameters := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"topN":{"type":"integer","minimum":1,"maximum":100,"default":20}}}`)
	serviceParameters := json.RawMessage(`{"type":"object","additionalProperties":false,"required":["service"],"properties":{"service":{"type":"string","pattern":"^[a-zA-Z0-9_.@:-]+$","maxLength":120}}}`)
	sinceParameters := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"sinceHours":{"type":"integer","minimum":1,"maximum":168,"default":24}}}`)
	serviceSinceParameters := json.RawMessage(`{"type":"object","additionalProperties":false,"required":["service"],"properties":{"service":{"type":"string","pattern":"^[a-zA-Z0-9_.@:-]+$","maxLength":120},"sinceHours":{"type":"integer","minimum":1,"maximum":168,"default":24}}}`)
	commandParameters := json.RawMessage(`{"type":"object","additionalProperties":false,"required":["command"],"properties":{"command":{"type":"string","enum":["cat","chronyc","df","dmesg","findmnt","free","hostnamectl","iostat","ip","journalctl","lscpu","lsblk","nproc","ntpq","ps","ss","swapon","systemctl","timedatectl","uname","uptime","which","who"]}}}`)

	definition := func(key, description, executable string, args []string, parameters json.RawMessage, risk string, timeout int, maxBytes int64, maxRows int) LinuxCommandDefinition {
		return LinuxCommandDefinition{
			Key: key, Version: version, Description: description, Executable: executable,
			ArgsTemplate: args, AllowedParameters: parameters, RiskLevel: risk,
			TimeoutSeconds: timeout, MaxOutputBytes: maxBytes, MaxRows: maxRows,
			RequiredCommands: []string{executable}, SupportedOS: allLinux, EnabledByDefault: true,
		}
	}
	return []LinuxCommandDefinition{
		definition("platform.which", "Detect one catalog executable without invoking a shell.", "which", []string{"${command}"}, commandParameters, RiskSafeRead, 3, 8*1024, 20),
		definition("system.uname", "Read kernel and architecture summary.", "uname", []string{"-a"}, noParameters, RiskSafeRead, 5, 32*1024, 100),
		definition("system.hostname", "Read static hostname.", "hostnamectl", []string{"--static"}, noParameters, RiskSafeRead, 5, 8*1024, 20),
		definition("system.os_release", "Read operating system release metadata.", "cat", []string{"/etc/os-release"}, noParameters, RiskSafeRead, 5, 32*1024, 100),
		definition("system.uptime", "Read uptime and load summary.", "uptime", nil, noParameters, RiskSafeRead, 5, 8*1024, 20),
		definition("system.boot_time", "Read last system boot time.", "who", []string{"-b"}, noParameters, RiskSafeRead, 5, 8*1024, 20),
		definition("cpu.count", "Read available CPU count.", "nproc", nil, noParameters, RiskSafeRead, 5, 8*1024, 20),
		definition("cpu.lscpu", "Read CPU topology.", "lscpu", nil, noParameters, RiskSafeRead, 5, 64*1024, 500),
		definition("cpu.loadavg", "Read kernel load averages.", "cat", []string{"/proc/loadavg"}, noParameters, RiskSafeRead, 5, 8*1024, 20),
		definition("cpu.stat", "Read cumulative CPU counters.", "cat", []string{"/proc/stat"}, noParameters, RiskSafeRead, 5, 64*1024, 500),
		definition("memory.meminfo", "Read kernel memory counters.", "cat", []string{"/proc/meminfo"}, noParameters, RiskSafeRead, 5, 64*1024, 500),
		definition("memory.free", "Read memory totals in bytes.", "free", []string{"-b"}, noParameters, RiskSafeRead, 5, 16*1024, 100),
		definition("memory.swap", "Read configured swap devices.", "swapon", []string{"--show", "--bytes", "--noheadings"}, noParameters, RiskSafeRead, 5, 32*1024, 200),
		definition("filesystem.df_bytes", "Read filesystem byte usage.", "df", []string{"-P", "-B1"}, noParameters, RiskSafeRead, 10, 128*1024, 1000),
		definition("filesystem.df_inodes", "Read filesystem inode usage.", "df", []string{"-Pi"}, noParameters, RiskSafeRead, 10, 128*1024, 1000),
		definition("filesystem.findmnt", "Read mounted filesystems as JSON.", "findmnt", []string{"--json"}, noParameters, RiskSafeRead, 10, 256*1024, 2000),
		definition("filesystem.lsblk", "Read block devices as JSON.", "lsblk", []string{"--json", "--bytes"}, noParameters, RiskSafeRead, 10, 256*1024, 2000),
		definition("diskio.proc", "Read kernel disk counters.", "cat", []string{"/proc/diskstats"}, noParameters, RiskSafeRead, 5, 128*1024, 1000),
		definition("diskio.iostat", "Read extended disk IO statistics.", "iostat", []string{"-x", "-d", "1", "2"}, noParameters, RiskSafeRead, 10, 128*1024, 1000),
		definition("network.address", "Read network addresses as JSON.", "ip", []string{"-j", "address"}, noParameters, RiskSensitiveRead, 5, 256*1024, 2000),
		definition("network.route", "Read network routes as JSON.", "ip", []string{"-j", "route"}, noParameters, RiskSensitiveRead, 5, 128*1024, 1000),
		definition("network.socket_summary", "Read socket summary.", "ss", []string{"-s"}, noParameters, RiskSensitiveRead, 5, 32*1024, 200),
		definition("network.listening", "Read listening sockets and owning processes when permitted.", "ss", []string{"-lntup"}, noParameters, RiskSensitiveRead, 10, 256*1024, 2000),
		definition("network.resolver", "Read resolver configuration.", "cat", []string{"/etc/resolv.conf"}, noParameters, RiskSensitiveRead, 5, 32*1024, 200),
		definition("process.top_cpu", "Read processes sorted by CPU usage.", "ps", []string{"-eo", "pid,ppid,user,stat,pcpu,pmem,rss,vsz,lstart,etime,comm", "--sort=-pcpu"}, topNParameters, RiskSensitiveRead, 10, 256*1024, 20),
		definition("process.top_memory", "Read processes sorted by memory usage.", "ps", []string{"-eo", "pid,ppid,user,stat,pcpu,pmem,rss,vsz,lstart,etime,comm", "--sort=-pmem"}, topNParameters, RiskSensitiveRead, 10, 256*1024, 20),
		definition("process.all", "Read bounded process state and elapsed time for aggregate counts.", "ps", []string{"-eo", "pid,stat,etime,comm"}, noParameters, RiskSensitiveRead, 10, 512*1024, 10000),
		definition("systemd.state", "Read systemd overall state.", "systemctl", []string{"is-system-running"}, noParameters, RiskSafeRead, 10, 16*1024, 100),
		definition("systemd.failed", "Read failed systemd services.", "systemctl", []string{"list-units", "--type=service", "--state=failed", "--no-pager", "--no-legend"}, noParameters, RiskSafeRead, 10, 128*1024, 1000),
		definition("systemd.show", "Read properties for one validated systemd service.", "systemctl", []string{"show", "${service}", "--no-pager"}, serviceParameters, RiskSensitiveRead, 10, 128*1024, 1000),
		definition("time.timedatectl", "Read time synchronization state.", "timedatectl", []string{"show"}, noParameters, RiskSafeRead, 5, 32*1024, 200),
		definition("time.chrony_tracking", "Read chrony tracking state.", "chronyc", []string{"tracking"}, noParameters, RiskSafeRead, 5, 32*1024, 200),
		definition("time.chrony_sources", "Read chrony sources.", "chronyc", []string{"sources"}, noParameters, RiskSafeRead, 5, 64*1024, 500),
		definition("time.ntpq", "Read NTP peer state.", "ntpq", []string{"-pn"}, noParameters, RiskSafeRead, 5, 64*1024, 500),
		definition("kernel.dmesg", "Read warning and error kernel messages.", "dmesg", []string{"--level=emerg,alert,crit,err,warn", "--ctime"}, noParameters, RiskSensitiveRead, 10, 256*1024, 2000),
		definition("kernel.journal", "Read bounded kernel journal warnings.", "journalctl", []string{"-k", "-p", "warning", "--since", "-${sinceHours}h", "--no-pager"}, sinceParameters, RiskSensitiveRead, 15, 256*1024, 2000),
		definition("logs.warning", "Read bounded system warning journal.", "journalctl", []string{"-p", "warning", "--since", "-${sinceHours}h", "--no-pager"}, sinceParameters, RiskSensitiveRead, 15, 256*1024, 2000),
		definition("logs.service", "Read bounded journal for one validated service.", "journalctl", []string{"-u", "${service}", "--since", "-${sinceHours}h", "--no-pager"}, serviceSinceParameters, RiskSensitiveRead, 15, 256*1024, 2000),
	}
}
