package linuxserver

import (
	"bufio"
	"sort"
	"strconv"
	"strings"
)

const (
	CapabilitySupported   = "supported"
	CapabilityPartial     = "partial"
	CapabilityUnsupported = "unsupported"
)

type LinuxPlatformInfo struct {
	ID                string          `json:"id"`
	VersionID         string          `json:"versionId"`
	Name              string          `json:"name"`
	Kernel            string          `json:"kernel"`
	Status            string          `json:"status"`
	AvailableCommands map[string]bool `json:"availableCommands"`
	Warnings          []string        `json:"warnings,omitempty"`
}

type CommandCapability struct {
	Status          string   `json:"status"`
	Runnable        bool     `json:"runnable"`
	MissingCommands []string `json:"missingCommands,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
}

func DetectPlatform(osRelease, kernel string, availableCommands []string) LinuxPlatformInfo {
	fields := parseOSRelease(osRelease)
	id := strings.ToLower(fields["ID"])
	versionID := fields["VERSION_ID"]
	name := fields["PRETTY_NAME"]
	if name == "" {
		name = fields["NAME"]
	}
	commands := make(map[string]bool, len(availableCommands))
	for _, command := range availableCommands {
		command = strings.TrimSpace(command)
		if command != "" {
			commands[command] = true
		}
	}
	status := supportedPlatformStatus(id, versionID)
	if status == CapabilityUnsupported && id != "" && strings.Contains(strings.ToLower(kernel), "linux") {
		status = CapabilityPartial
	}
	warnings := []string{}
	if status == CapabilityPartial {
		warnings = append(warnings, "operating system version is outside the initial support matrix; capability checks will be used")
	}
	if status == CapabilityUnsupported {
		warnings = append(warnings, "operating system is unsupported")
	}
	return LinuxPlatformInfo{
		ID: id, VersionID: versionID, Name: name, Kernel: strings.TrimSpace(kernel),
		Status: status, AvailableCommands: commands, Warnings: warnings,
	}
}

func EvaluateCommandCapability(platform LinuxPlatformInfo, definition LinuxCommandDefinition) CommandCapability {
	missing := make([]string, 0)
	for _, command := range definition.RequiredCommands {
		if !platform.AvailableCommands[command] {
			missing = append(missing, command)
		}
	}
	sort.Strings(missing)
	if platform.Status == CapabilityUnsupported {
		return CommandCapability{Status: CapabilityUnsupported, Runnable: false, MissingCommands: missing, Warnings: []string{"platform is unsupported"}}
	}
	if len(missing) > 0 {
		return CommandCapability{Status: CapabilityPartial, Runnable: false, MissingCommands: missing, Warnings: []string{"required command is unavailable"}}
	}
	if len(definition.SupportedOS) > 0 && !containsString(definition.SupportedOS, platform.ID) {
		return CommandCapability{Status: CapabilityPartial, Runnable: true, Warnings: []string{"command is not certified for this operating system"}}
	}
	if platform.Status == CapabilityPartial {
		return CommandCapability{Status: CapabilityPartial, Runnable: true, Warnings: append([]string(nil), platform.Warnings...)}
	}
	return CommandCapability{Status: CapabilitySupported, Runnable: true}
}

func parseOSRelease(content string) map[string]string {
	result := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" {
			result[key] = value
		}
	}
	return result
}

func supportedPlatformStatus(id, versionID string) string {
	major, minor := platformVersion(versionID)
	switch id {
	case "rhel":
		if major >= 7 && major <= 9 {
			return CapabilitySupported
		}
	case "centos":
		if major == 7 {
			return CapabilitySupported
		}
	case "rocky", "almalinux":
		if major == 8 || major == 9 {
			return CapabilitySupported
		}
	case "ubuntu":
		if (major == 20 && minor == 4) || (major == 22 && minor == 4) || (major == 24 && minor == 4) {
			return CapabilitySupported
		}
	case "debian":
		if major == 11 || major == 12 {
			return CapabilitySupported
		}
	default:
		return CapabilityUnsupported
	}
	if id == "" {
		return CapabilityUnsupported
	}
	return CapabilityPartial
}

func platformVersion(value string) (int, int) {
	parts := strings.Split(strings.TrimSpace(value), ".")
	if len(parts) == 0 {
		return 0, 0
	}
	major, _ := strconv.Atoi(parts[0])
	minor := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	return major, minor
}
