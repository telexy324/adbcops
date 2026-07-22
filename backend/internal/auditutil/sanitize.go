package auditutil

import (
	"encoding/json"
	"regexp"
	"strings"
)

const RedactedValue = "[REDACTED]"

var sensitiveKeys = []string{
	"api_key",
	"apikey",
	"authorization",
	"cookie",
	"credential",
	"jwt",
	"key",
	"argv",
	"commandline",
	"executable",
	"password",
	"private_key",
	"raw_output",
	"rawcommandoutput",
	"secret",
	"stderr",
	"stdout",
	"token",
}

var sensitiveAssignment = regexp.MustCompile(`(?i)(password|passwd|pwd|token|secret|authorization|cookie|jwt|api[_-]?key)(\s*[:=]\s*)(bearer\s+)?[^\s,;]+`)

func SanitizeJSON(raw []byte, maxBytes int) []byte {
	if len(raw) == 0 || !json.Valid(raw) {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	value = sanitize(value)
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	if maxBytes <= 0 || len(encoded) <= maxBytes {
		return encoded
	}
	envelope, _ := json.Marshal(map[string]any{
		"truncated": true,
		"bytes":     len(encoded),
		"preview":   string(encoded[:maxBytes]),
	})
	return envelope
}

func ContainsSensitiveToken(value string) bool {
	normalized := strings.ToLower(value)
	for _, key := range sensitiveKeys {
		if strings.Contains(normalized, key) {
			return true
		}
	}
	return false
}

func SanitizeText(value string) string {
	return sensitiveAssignment.ReplaceAllString(value, "$1$2"+RedactedValue)
}

func IsSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	for _, sensitive := range sensitiveKeys {
		if normalized == sensitive || (sensitive != "key" && strings.Contains(normalized, sensitive)) {
			return true
		}
	}
	return false
}

func sanitize(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, nested := range typed {
			if IsSensitiveKey(key) {
				result[key] = RedactedValue
				continue
			}
			result[key] = sanitize(nested)
		}
		return result
	case []any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, sanitize(item))
		}
		return result
	case string:
		return SanitizeText(typed)
	default:
		return typed
	}
}
