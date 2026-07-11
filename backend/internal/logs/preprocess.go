package logs

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"

	"aiops-platform/backend/internal/model"
)

const (
	defaultStackMaxLines = 40
	timeBucketDuration   = time.Minute
)

var (
	idCardPattern   = regexp.MustCompile(`\b\d{17}[\dXx]\b`)
	mobilePattern   = regexp.MustCompile(`\b1[3-9]\d{9}\b`)
	cardPattern     = regexp.MustCompile(`\b(?:\d[ -]?){13,19}\b`)
	tokenPattern    = regexp.MustCompile(`(?i)["']?\b(token|access_token|refresh_token|authorization|bearer)\b["']?\s*[:=]\s*["']?[^"',\s}]+`)
	passwordPattern = regexp.MustCompile(`(?i)["']?\b(password|passwd|pwd)\b["']?\s*[:=]\s*["']?[^"',\s}]+`)
	uuidPattern     = regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)
	ipPattern       = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)
	numberPattern   = regexp.MustCompile(`\b\d+\b`)
	quotedPattern   = regexp.MustCompile(`"[^"]*"|'[^']*'`)
	spacePattern    = regexp.MustCompile(`\s+`)
)

type PreprocessInput struct {
	Items         []model.LogItem
	StackMaxLines int
}

type ProcessedLogItem struct {
	model.LogItem
	Template       string `json:"template"`
	DuplicateCount int    `json:"duplicateCount"`
}

type TemplateCluster struct {
	Template string `json:"template"`
	Count    int    `json:"count"`
	Level    string `json:"level,omitempty"`
	Example  string `json:"example"`
}

type TimeBucket struct {
	Start      time.Time `json:"start"`
	Count      int       `json:"count"`
	ErrorCount int       `json:"errorCount"`
}

type PreprocessResult struct {
	Items          []ProcessedLogItem `json:"items"`
	Clusters       []TemplateCluster  `json:"clusters"`
	TimeStats      []TimeBucket       `json:"timeStats"`
	TotalInput     int                `json:"totalInput"`
	TotalOutput    int                `json:"totalOutput"`
	ErrorCount     int                `json:"errorCount"`
	RedactionCount int                `json:"redactionCount"`
}

func Preprocess(input PreprocessInput) PreprocessResult {
	stackMaxLines := input.StackMaxLines
	if stackMaxLines <= 0 {
		stackMaxLines = defaultStackMaxLines
	}
	result := PreprocessResult{TotalInput: len(input.Items)}
	seen := make(map[string]int)
	clusters := make(map[string]*TemplateCluster)
	buckets := make(map[time.Time]*TimeBucket)
	for _, item := range input.Items {
		normalized, redactions := normalizeLogItem(item, stackMaxLines)
		result.RedactionCount += redactions
		template := buildTemplate(normalized.Message)
		dedupKey := dedupKey(normalized, template)
		if index, ok := seen[dedupKey]; ok {
			result.Items[index].DuplicateCount++
		} else {
			seen[dedupKey] = len(result.Items)
			result.Items = append(result.Items, ProcessedLogItem{
				LogItem:        normalized,
				Template:       template,
				DuplicateCount: 1,
			})
		}
		cluster := clusters[template]
		if cluster == nil {
			cluster = &TemplateCluster{Template: template, Level: normalized.Level, Example: normalized.Message}
			clusters[template] = cluster
		}
		cluster.Count++
		if isErrorLevel(normalized.Level) {
			result.ErrorCount++
		}
		if !normalized.Timestamp.IsZero() {
			start := normalized.Timestamp.UTC().Truncate(timeBucketDuration)
			bucket := buckets[start]
			if bucket == nil {
				bucket = &TimeBucket{Start: start}
				buckets[start] = bucket
			}
			bucket.Count++
			if isErrorLevel(normalized.Level) {
				bucket.ErrorCount++
			}
		}
	}
	result.TotalOutput = len(result.Items)
	result.Clusters = sortedClusters(clusters)
	result.TimeStats = sortedBuckets(buckets)
	return result
}

func normalizeLogItem(item model.LogItem, stackMaxLines int) (model.LogItem, int) {
	item.Level = normalizeLevel(item.Level)
	item.Message = strings.TrimSpace(item.Message)
	item.Source = strings.TrimSpace(item.Source)
	item.SystemName = strings.TrimSpace(item.SystemName)
	item.Component = strings.TrimSpace(item.Component)
	item.Environment = strings.TrimSpace(item.Environment)
	item.Host = strings.TrimSpace(item.Host)
	item.Cluster = strings.TrimSpace(item.Cluster)
	item.Namespace = strings.TrimSpace(item.Namespace)
	item.Pod = strings.TrimSpace(item.Pod)
	item.Container = strings.TrimSpace(item.Container)
	item.TraceID = strings.TrimSpace(item.TraceID)
	item.RequestID = strings.TrimSpace(item.RequestID)
	item.ErrorCode = strings.TrimSpace(item.ErrorCode)
	item.Timestamp = item.Timestamp.UTC()
	item.Message = truncateStack(item.Message, stackMaxLines)
	item.Raw = truncateStack(strings.TrimSpace(item.Raw), stackMaxLines)
	message, count := redactSensitive(item.Message)
	raw, rawCount := redactSensitive(item.Raw)
	item.Message = message
	item.Raw = raw
	return item, count + rawCount
}

func normalizeLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "err", "error", "fatal", "panic":
		return "ERROR"
	case "warn", "warning":
		return "WARN"
	case "info", "information":
		return "INFO"
	case "debug":
		return "DEBUG"
	case "trace":
		return "TRACE"
	default:
		return strings.ToUpper(strings.TrimSpace(level))
	}
}

func redactSensitive(value string) (string, int) {
	total := 0
	value, total = replaceAll(idCardPattern, value, "[REDACTED_ID_CARD]", total)
	value, total = replaceAll(mobilePattern, value, "[REDACTED_PHONE]", total)
	value, total = replaceAll(cardPattern, value, "[REDACTED_CARD]", total)
	value, total = replaceAll(tokenPattern, value, "$1=[REDACTED_TOKEN]", total)
	value, total = replaceAll(passwordPattern, value, "$1=[REDACTED_PASSWORD]", total)
	return value, total
}

func replaceAll(pattern *regexp.Regexp, value, replacement string, total int) (string, int) {
	matches := pattern.FindAllStringIndex(value, -1)
	if len(matches) == 0 {
		return value, total
	}
	return pattern.ReplaceAllString(value, replacement), total + len(matches)
}

func truncateStack(value string, maxLines int) string {
	if value == "" || maxLines <= 0 {
		return value
	}
	lines := strings.Split(value, "\n")
	if len(lines) <= maxLines {
		return value
	}
	return strings.Join(lines[:maxLines], "\n") + "\n... [stack truncated]"
}

func buildTemplate(message string) string {
	template := strings.ToLower(strings.TrimSpace(message))
	template = quotedPattern.ReplaceAllString(template, "<str>")
	template = uuidPattern.ReplaceAllString(template, "<uuid>")
	template = ipPattern.ReplaceAllString(template, "<ip>")
	template = numberPattern.ReplaceAllString(template, "<num>")
	template = spacePattern.ReplaceAllString(template, " ")
	return strings.TrimSpace(template)
}

func dedupKey(item model.LogItem, template string) string {
	payload, _ := json.Marshal([]string{
		item.Timestamp.UTC().Format(time.RFC3339Nano),
		item.Level,
		item.Message,
		template,
		item.Source,
		item.Host,
		item.Pod,
		item.Container,
	})
	sum := sha1.Sum(payload)
	return hex.EncodeToString(sum[:])
}

func sortedClusters(clusters map[string]*TemplateCluster) []TemplateCluster {
	result := make([]TemplateCluster, 0, len(clusters))
	for _, cluster := range clusters {
		result = append(result, *cluster)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return result[i].Template < result[j].Template
	})
	return result
}

func sortedBuckets(buckets map[time.Time]*TimeBucket) []TimeBucket {
	result := make([]TimeBucket, 0, len(buckets))
	for _, bucket := range buckets {
		result = append(result, *bucket)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Start.Before(result[j].Start)
	})
	return result
}

func isErrorLevel(level string) bool {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "ERROR", "FATAL", "PANIC":
		return true
	default:
		return false
	}
}
