package document

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	"aiops-platform/backend/internal/model"
)

const (
	maxSummaryRunes  = 120
	maxKeywordCount  = 12
	maxQuestionCount = 4
)

func EnhanceChunk(chunk *model.KBChunk) {
	if chunk == nil {
		return
	}
	summary := summarize(chunk.Content)
	keywords := extractKeywords(chunk)
	questions := possibleQuestions(keywords, chunk.SourceSection)
	searchText := buildSearchText(chunk, summary, keywords, questions)

	chunk.Summary = &summary
	chunk.SearchText = &searchText
	chunk.Keywords = mustJSON(keywords)
	chunk.PossibleQuestions = mustJSON(questions)
}

func summarize(content string) string {
	normalized := normalizeWhitespace(content)
	if normalized == "" {
		return ""
	}
	for _, separator := range []string{"。", "！", "？", ".", "!", "?"} {
		if index := strings.Index(normalized, separator); index >= 0 {
			candidate := strings.TrimSpace(normalized[:index+len(separator)])
			if candidate != "" && utf8.RuneCountInString(candidate) <= maxSummaryRunes {
				return candidate
			}
		}
	}
	return trimRunes(normalized, maxSummaryRunes)
}

func extractKeywords(chunk *model.KBChunk) []string {
	seen := make(map[string]struct{})
	keywords := make([]string, 0, maxKeywordCount)
	add := func(value string) {
		value = strings.Trim(strings.TrimSpace(value), "#*`[]()（）:：,，.。;；")
		if utf8.RuneCountInString(value) < 2 || utf8.RuneCountInString(value) > 32 {
			return
		}
		if _, exists := seen[value]; exists {
			return
		}
		seen[value] = struct{}{}
		keywords = append(keywords, value)
	}
	if chunk.SourceTitle != nil {
		add(*chunk.SourceTitle)
	}
	if chunk.SourceSection != nil {
		add(*chunk.SourceSection)
	}
	for _, token := range splitKeywordCandidates(chunk.Content) {
		add(token)
		if len(keywords) >= maxKeywordCount {
			break
		}
	}
	return keywords
}

func splitKeywordCandidates(content string) []string {
	fields := strings.FieldsFunc(content, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("#*_`[]()（）,，.。;；:：!?！？/\\|", r)
	})
	candidates := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		runes := []rune(field)
		if len(runes) <= 8 {
			candidates = append(candidates, field)
			continue
		}
		for start := 0; start < len(runes); start += 4 {
			end := start + 8
			if end > len(runes) {
				end = len(runes)
			}
			if end-start >= 2 {
				candidates = append(candidates, string(runes[start:end]))
			}
		}
	}
	return candidates
}

func possibleQuestions(keywords []string, section *string) []string {
	questions := make([]string, 0, maxQuestionCount)
	if section != nil && strings.TrimSpace(*section) != "" {
		questions = append(questions, "如何排查"+strings.TrimSpace(*section)+"？")
	}
	for _, keyword := range keywords {
		if len(questions) >= maxQuestionCount {
			break
		}
		questions = append(questions, "如何处理"+keyword+"？")
	}
	if len(questions) == 0 {
		questions = append(questions, "这段知识适用于什么场景？")
	}
	return questions
}

func buildSearchText(chunk *model.KBChunk, summary string, keywords, questions []string) string {
	parts := make([]string, 0, 6)
	if chunk.SourceTitle != nil {
		parts = append(parts, *chunk.SourceTitle)
	}
	if chunk.SourceSection != nil {
		parts = append(parts, *chunk.SourceSection)
	}
	parts = append(parts, chunk.Content, summary)
	parts = append(parts, keywords...)
	parts = append(parts, questions...)
	return normalizeWhitespace(strings.Join(parts, "\n"))
}

func normalizeWhitespace(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func trimRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return strings.TrimSpace(string(runes[:max]))
}

func mustJSON(values []string) []byte {
	payload, err := json.Marshal(values)
	if err != nil {
		return []byte("[]")
	}
	return payload
}
