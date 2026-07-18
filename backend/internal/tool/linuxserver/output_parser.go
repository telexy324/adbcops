package linuxserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

var (
	sensitiveKeyPattern  = regexp.MustCompile(`(?i)(password|passwd|passphrase|secret|token|authorization|api[_-]?key|private[_-]?key)`)
	sensitiveTextPattern = regexp.MustCompile(`(?i)(password|passwd|passphrase|secret|token|authorization|api[_-]?key)\s*[:=]\s*([^,;\n\r]+)`)
)

type OutputParser interface {
	Parse(commandKey, output string, truncated bool) (json.RawMessage, []string, error)
}

type SafeOutputParser struct{}

func (SafeOutputParser) Parse(commandKey, output string, truncated bool) (json.RawMessage, []string, error) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return json.RawMessage(`{"format":"empty","empty":true}`), nil, nil
	}
	var value any
	if json.Unmarshal([]byte(trimmed), &value) == nil {
		payload, err := json.Marshal(map[string]any{"format": "json", "value": redactJSON(value)})
		return payload, nil, err
	}
	if values, ok := parseProperties(trimmed); ok {
		payload, err := json.Marshal(map[string]any{"format": "properties", "values": values})
		return payload, nil, err
	}
	redacted := redactText(trimmed)
	lines := strings.Count(redacted, "\n") + 1
	digest := sha256.Sum256([]byte(redacted))
	summary := redacted
	if len(summary) > 512 {
		summary = summary[:512]
	}
	payload, err := json.Marshal(map[string]any{
		"format": "text_summary", "lineCount": lines, "sha256": hex.EncodeToString(digest[:]), "summary": summary,
	})
	warnings := []string{"output did not match a structured format; a redacted bounded summary was returned"}
	if truncated {
		warnings = append(warnings, "summary is based on truncated command output")
	}
	return payload, warnings, err
}

func parseProperties(output string) (map[string]string, bool) {
	values := map[string]string{}
	parsed := 0
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		separator := strings.Index(line, "=")
		if separator <= 0 {
			separator = strings.Index(line, ":")
		}
		if separator <= 0 {
			return nil, false
		}
		key := strings.TrimSpace(line[:separator])
		value := strings.TrimSpace(line[separator+1:])
		if key == "" {
			return nil, false
		}
		if sensitiveKeyPattern.MatchString(key) {
			value = "[REDACTED]"
		} else {
			value = redactText(value)
		}
		values[key] = value
		parsed++
	}
	return values, parsed > 0
}

func redactJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			if sensitiveKeyPattern.MatchString(key) {
				result[key] = "[REDACTED]"
			} else {
				result[key] = redactJSON(item)
			}
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = redactJSON(item)
		}
		return result
	case string:
		return redactText(typed)
	default:
		return value
	}
}

func redactText(value string) string {
	value = sensitiveTextPattern.ReplaceAllString(value, "$1=[REDACTED]")
	if strings.Contains(value, "-----BEGIN") && strings.Contains(value, "PRIVATE KEY-----") {
		return "[REDACTED PRIVATE KEY]"
	}
	return value
}

func validateParser(parser OutputParser) error {
	if parser == nil {
		return errors.New("output parser is required")
	}
	return nil
}
