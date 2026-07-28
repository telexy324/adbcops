package rag

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"

	"aiops-platform/backend/internal/model"
)

const (
	maxRCAQueryItems = 12
	maxRCAItemRunes  = 512
)

var rcaDynamicPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:trace[_-]?id|request[_-]?id|span[_-]?id|session[_-]?id|uuid|id)\s*[:=]\s*["']?[a-z0-9._:/-]+["']?`),
	regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\b`),
	regexp.MustCompile(`(?i)\b(?:[0-9a-f]{16,}|0x[0-9a-f]{8,})\b`),
	regexp.MustCompile(`\b(?:25[0-5]|2[0-4]\d|1?\d?\d)(?:\.(?:25[0-5]|2[0-4]\d|1?\d?\d)){3}(?::\d{1,5})?\b`),
	regexp.MustCompile(`\b\d{4}[-/]\d{1,2}[-/]\d{1,2}(?:[T\s]\d{1,2}:\d{2}(?::\d{2}(?:\.\d+)?)?(?:Z|[+-]\d{2}:?\d{2})?)?\b`),
	regexp.MustCompile(`\b\d{1,2}:\d{2}:\d{2}(?:\.\d+)?\b`),
	regexp.MustCompile(`\b\d{6,}\b`),
}

var rcaWhitespace = regexp.MustCompile(`\s+`)

type RCAKnowledgeSearchInput struct {
	OriginalQuestion       string   `json:"originalQuestion"`
	ConfirmedEntities      []string `json:"confirmedEntities,omitempty"`
	LogTemplates           []string `json:"logTemplates,omitempty"`
	MetricAnomalySummaries []string `json:"metricAnomalySummaries,omitempty"`
	Limit                  int      `json:"limit,omitempty"`
}

type RCARetrievalInput struct {
	OriginalQuestion       string   `json:"originalQuestion"`
	ConfirmedEntities      []string `json:"confirmedEntities"`
	SanitizedLogTemplates  []string `json:"sanitizedLogTemplates"`
	MetricAnomalySummaries []string `json:"metricAnomalySummaries"`
	ComposedQuery          string   `json:"composedQuery"`
}

type RCAKnowledgeSearchResult struct {
	Input            RCARetrievalInput `json:"retrievalInput"`
	RewrittenQuery   string            `json:"rewrittenQuery"`
	Citations        []Citation        `json:"citations"`
	Context          []ContextBlock    `json:"context"`
	RecallCount      int               `json:"recallCount"`
	RetrievalTrace   RetrievalTrace    `json:"retrievalTrace"`
	Degraded         bool              `json:"degraded"`
	DegradedChannels []string          `json:"degradedChannels"`
}

// SearchForRCA performs retrieval only. It deliberately does not create a
// conversation, message, or QA record; the Skill execution record is the audit
// boundary for RCA calls.
func (s *Service) SearchForRCA(ctx context.Context, actor *model.AppUser, input RCAKnowledgeSearchInput) (*RCAKnowledgeSearchResult, error) {
	retrievalInput, err := buildRCARetrievalInput(input)
	if err != nil {
		return nil, err
	}
	search, err := s.searchPublishedKnowledge(ctx, actor, retrievalInput.ComposedQuery, normalizeLimit(input.Limit))
	if err != nil {
		return nil, err
	}
	degradedChannels := collectDegradedChannels(search.Retrieval)
	return &RCAKnowledgeSearchResult{
		Input: retrievalInput, RewrittenQuery: search.Rewritten,
		Citations: search.Citations, Context: search.ContextBlocks, RecallCount: len(search.ContextBlocks),
		RetrievalTrace: search.Retrieval, Degraded: len(degradedChannels) > 0, DegradedChannels: degradedChannels,
	}, nil
}

func buildRCARetrievalInput(input RCAKnowledgeSearchInput) (RCARetrievalInput, error) {
	question, err := normalizeQuestion(input.OriginalQuestion)
	if err != nil {
		return RCARetrievalInput{}, err
	}
	entities := normalizeRCAItems(input.ConfirmedEntities, false)
	logTemplates := normalizeRCAItems(input.LogTemplates, true)
	metricSummaries := normalizeRCAItems(input.MetricAnomalySummaries, false)
	parts := []string{question}
	if len(entities) > 0 {
		parts = append(parts, "已确认实体 "+strings.Join(entities, " "))
	}
	if len(logTemplates) > 0 {
		parts = append(parts, "日志模式 "+strings.Join(logTemplates, " "))
	}
	if len(metricSummaries) > 0 {
		parts = append(parts, "指标异常 "+strings.Join(metricSummaries, " "))
	}
	composed := truncateUTF8Bytes(strings.Join(parts, "\n"), maxQuestionBytes)
	return RCARetrievalInput{
		OriginalQuestion: question, ConfirmedEntities: entities, SanitizedLogTemplates: logTemplates,
		MetricAnomalySummaries: metricSummaries, ComposedQuery: composed,
	}, nil
}

func normalizeRCAItems(values []string, sanitizeDynamic bool) []string {
	result := make([]string, 0, minInt(len(values), maxRCAQueryItems))
	seen := map[string]struct{}{}
	for _, value := range values {
		if len(result) >= maxRCAQueryItems {
			break
		}
		if sanitizeDynamic {
			value = sanitizeRCALogTemplate(value)
		} else {
			value = strings.TrimSpace(rcaWhitespace.ReplaceAllString(value, " "))
		}
		value = truncateRunes(value, maxRCAItemRunes)
		key := strings.ToLower(value)
		if value == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func sanitizeRCALogTemplate(value string) string {
	for _, pattern := range rcaDynamicPatterns {
		value = pattern.ReplaceAllString(value, " ")
	}
	value = strings.NewReplacer("[", " ", "]", " ", "{", " ", "}", " ", `"`, " ").Replace(value)
	return strings.Trim(strings.TrimSpace(rcaWhitespace.ReplaceAllString(value, " ")), ",;|")
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if limit <= 0 || len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func truncateUTF8Bytes(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return strings.TrimSpace(value[:limit])
}

func collectDegradedChannels(trace RetrievalTrace) []string {
	result := []string{}
	seen := map[string]struct{}{}
	add := func(name string) {
		if _, exists := seen[name]; exists {
			return
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	for _, channel := range trace.Channels {
		if channel.Degraded {
			add(channel.Channel)
		}
	}
	if trace.Rerank.Degraded {
		add("rerank")
	}
	if trace.Context.Degraded {
		add("context_builder")
	}
	return result
}
