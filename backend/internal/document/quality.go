package document

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode"

	"aiops-platform/backend/internal/model"
)

const (
	QualityPrompt           = `You are an operations knowledge quality reviewer. Score the document only against the supplied scoring standards. Return strict JSON only, with no markdown fences or extra text.`
	maxQualityPromptRunes   = 20000
	defaultQualityPassScore = 70
)

var ErrInvalidQualityJSON = errors.New("invalid quality result JSON")

type QualityResult struct {
	Score          int             `json:"score"`
	Summary        string          `json:"summary"`
	Findings       []string        `json:"findings"`
	Suggestions    []string        `json:"suggestions"`
	CriteriaScores []CriteriaScore `json:"criteriaScores,omitempty"`
	Standards      []string        `json:"standards,omitempty"`
	Source         string          `json:"source,omitempty"`
}

type CriteriaScore struct {
	Name     string   `json:"name"`
	Score    int      `json:"score"`
	Matched  []string `json:"matched,omitempty"`
	Missing  []string `json:"missing,omitempty"`
	Standard string   `json:"standard"`
}

type QualityCriterion struct {
	Name     string
	Standard string
	Keywords []string
	Weight   int
}

func ParseQualityResult(raw json.RawMessage) (QualityResult, []byte, error) {
	if len(raw) == 0 {
		return QualityResult{}, nil, fmt.Errorf("%w: result is required", ErrInvalidQualityJSON)
	}
	var result QualityResult
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return QualityResult{}, nil, fmt.Errorf("%w: %v", ErrInvalidQualityJSON, err)
	}
	if result.Score < 0 || result.Score > 100 {
		return QualityResult{}, nil, fmt.Errorf("%w: score must be from 0 to 100", ErrInvalidQualityJSON)
	}
	if strings.TrimSpace(result.Summary) == "" {
		return QualityResult{}, nil, fmt.Errorf("%w: summary is required", ErrInvalidQualityJSON)
	}
	if len(result.Findings) == 0 {
		return QualityResult{}, nil, fmt.Errorf("%w: findings must not be empty", ErrInvalidQualityJSON)
	}
	if len(result.Suggestions) == 0 {
		return QualityResult{}, nil, fmt.Errorf("%w: suggestions must not be empty", ErrInvalidQualityJSON)
	}
	normalized, err := json.Marshal(result)
	if err != nil {
		return QualityResult{}, nil, fmt.Errorf("normalize quality result: %w", err)
	}
	return result, normalized, nil
}

func StatusAfterQualityScore(score int) string {
	return StatusAfterQualityScoreAt(score, defaultQualityPassScore)
}

func StatusAfterQualityScoreAt(score, passScore int) string {
	if score >= passScore {
		return model.DocumentStatusReviewing
	}
	return model.DocumentStatusRejected
}

func CanPublish(document *model.KBDocument) bool {
	return CanPublishAt(document, defaultQualityPassScore)
}

func CanPublishAt(document *model.KBDocument, passScore int) bool {
	return document != nil && document.QualityScore >= passScore && document.Status == model.DocumentStatusReviewing
}

func DefaultQualityCriteria() []QualityCriterion {
	return []QualityCriterion{
		{Name: "范围与对象清晰", Standard: "default", Keywords: []string{"系统", "组件", "环境", "版本", "范围"}, Weight: 20},
		{Name: "排障步骤可执行", Standard: "default", Keywords: []string{"步骤", "检查", "查看", "执行", "命令", "确认"}, Weight: 25},
		{Name: "包含观测指标和证据", Standard: "default", Keywords: []string{"指标", "日志", "告警", "错误", "延迟", "连接", "证据"}, Weight: 20},
		{Name: "包含风险、回滚或恢复说明", Standard: "default", Keywords: []string{"风险", "回滚", "恢复", "影响", "应急", "降级"}, Weight: 20},
		{Name: "内容可维护", Standard: "default", Keywords: []string{"负责人", "更新时间", "维护", "链接", "参考", "变更"}, Weight: 15},
	}
}

func BuildQualityResult(document *model.KBDocument, content string, customStandards []model.KBQualityStandard, useDefault bool) QualityResult {
	return BuildQualityResultAt(document, content, customStandards, useDefault, defaultQualityPassScore)
}

func BuildQualityResultAt(document *model.KBDocument, content string, customStandards []model.KBQualityStandard, useDefault bool, passScore int) QualityResult {
	criteria := make([]QualityCriterion, 0, len(customStandards)+len(DefaultQualityCriteria()))
	standards := make([]string, 0, len(customStandards)+1)
	if useDefault {
		criteria = append(criteria, DefaultQualityCriteria()...)
		standards = append(standards, "default")
	}
	for _, standard := range customStandards {
		customCriteria := criteriaFromStandard(standard)
		criteria = append(criteria, customCriteria...)
		standards = append(standards, standard.Title)
	}
	if len(criteria) == 0 {
		criteria = append(criteria, DefaultQualityCriteria()...)
		standards = append(standards, "default")
	}
	normalizedContent := normalizeQualityText(content + " " + document.Title + " " + optional(document.SystemName) + " " + optional(document.ComponentName) + " " + optional(document.DocType))
	var weightedScore float64
	var totalWeight int
	var criteriaScores []CriteriaScore
	var findings []string
	var suggestions []string
	for _, criterion := range criteria {
		score, matched, missing := scoreCriterion(normalizedContent, criterion)
		criteriaScores = append(criteriaScores, CriteriaScore{
			Name:     criterion.Name,
			Score:    score,
			Matched:  matched,
			Missing:  missing,
			Standard: criterion.Standard,
		})
		if score >= passScore {
			findings = append(findings, fmt.Sprintf("%s 达标，匹配：%s", criterion.Name, strings.Join(matched, "、")))
		} else {
			suggestions = append(suggestions, fmt.Sprintf("补充「%s」相关内容：%s", criterion.Name, strings.Join(missing, "、")))
		}
		weightedScore += float64(score * criterion.Weight)
		totalWeight += criterion.Weight
	}
	finalScore := 0
	if totalWeight > 0 {
		finalScore = int(math.Round(weightedScore / float64(totalWeight)))
	}
	if len(findings) == 0 {
		findings = append(findings, "未发现达到当前评分标准的充分证据。")
	}
	if len(suggestions) == 0 {
		suggestions = append(suggestions, "当前文档基本满足所选评分标准，建议保持周期性更新。")
	}
	summary := fmt.Sprintf("根据 %s 评分，综合得分 %d。", strings.Join(standards, " + "), finalScore)
	return QualityResult{
		Score:          finalScore,
		Summary:        summary,
		Findings:       findings,
		Suggestions:    suggestions,
		CriteriaScores: criteriaScores,
		Standards:      standards,
		Source:         "rule-based",
	}
}

func BuildQualityLLMPrompt(document *model.KBDocument, content string, customStandards []model.KBQualityStandard, useDefault bool) string {
	return BuildQualityLLMPromptAt(document, content, customStandards, useDefault, defaultQualityPassScore)
}

func BuildQualityLLMPromptAt(document *model.KBDocument, content string, customStandards []model.KBQualityStandard, useDefault bool, passScore int) string {
	var builder strings.Builder
	builder.WriteString("请根据评分标准对知识手册进行质量评分。必须返回严格 JSON：\n")
	builder.WriteString(`{"score":0,"summary":"string","findings":["string"],"suggestions":["string"],"criteriaScores":[{"name":"string","score":0,"matched":["string"],"missing":["string"],"standard":"string"}],"standards":["string"],"source":"llm"}`)
	builder.WriteString("\n\n评分要求：\n")
	builder.WriteString(fmt.Sprintf("- score 必须是 0 到 100 的整数；%d 分及以上表示可进入发布审核，低于 %d 分表示不达标。\n", passScore, passScore))
	builder.WriteString("- criteriaScores 必须覆盖所选默认标准和自定义标准中的关键条目。\n")
	builder.WriteString("- findings 写已经满足的证据，suggestions 写需要补充或修正的内容。\n")
	builder.WriteString("- source 固定返回 llm。\n\n")
	builder.WriteString("文档元数据：\n")
	if document != nil {
		builder.WriteString(fmt.Sprintf("- 标题：%s\n", document.Title))
		builder.WriteString(fmt.Sprintf("- 文件名：%s\n", document.FileName))
		builder.WriteString(fmt.Sprintf("- 系统：%s\n", optional(document.SystemName)))
		builder.WriteString(fmt.Sprintf("- 组件：%s\n", optional(document.ComponentName)))
		builder.WriteString(fmt.Sprintf("- 环境：%s\n", optional(document.Environment)))
		builder.WriteString(fmt.Sprintf("- 类型：%s\n", optional(document.DocType)))
		builder.WriteString(fmt.Sprintf("- 版本：%s\n", document.Version))
	}
	builder.WriteString("\n评分标准：\n")
	if useDefault || len(customStandards) == 0 {
		builder.WriteString("【default】\n")
		for _, criterion := range DefaultQualityCriteria() {
			builder.WriteString(fmt.Sprintf("- %s：需关注 %s\n", criterion.Name, strings.Join(criterion.Keywords, "、")))
		}
	}
	for _, standard := range customStandards {
		builder.WriteString(fmt.Sprintf("【%s】\n%s\n", standard.Title, trimPromptText(standard.Content, 6000)))
	}
	builder.WriteString("\n待评分手册正文：\n")
	builder.WriteString(trimPromptText(content, maxQualityPromptRunes))
	return builder.String()
}

func selectedStandardNames(customStandards []model.KBQualityStandard, useDefault bool) []string {
	standards := make([]string, 0, len(customStandards)+1)
	if useDefault || len(customStandards) == 0 {
		standards = append(standards, "default")
	}
	for _, standard := range customStandards {
		standards = append(standards, standard.Title)
	}
	return standards
}

func extractJSONContent(value string) string {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```json")
		trimmed = strings.TrimPrefix(trimmed, "```JSON")
		trimmed = strings.TrimPrefix(trimmed, "```")
		trimmed = strings.TrimSuffix(trimmed, "```")
		trimmed = strings.TrimSpace(trimmed)
	}
	if start := strings.Index(trimmed, "{"); start >= 0 {
		if end := strings.LastIndex(trimmed, "}"); end > start {
			return trimmed[start : end+1]
		}
	}
	return trimmed
}

func trimPromptText(value string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(value))
	if maxRunes <= 0 || len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes]) + "\n[内容已截断]"
}

func criteriaFromStandard(standard model.KBQualityStandard) []QualityCriterion {
	lines := extractCriterionLines(standard.Content)
	criteria := make([]QualityCriterion, 0, len(lines))
	for _, line := range lines {
		keywords := keywordsFromCriterion(line)
		if len(keywords) == 0 {
			continue
		}
		criteria = append(criteria, QualityCriterion{Name: line, Standard: standard.Title, Keywords: keywords, Weight: 20})
	}
	if len(criteria) == 0 {
		keywords := keywordsFromCriterion(standard.Content)
		if len(keywords) > 0 {
			criteria = append(criteria, QualityCriterion{Name: standard.Title, Standard: standard.Title, Keywords: keywords, Weight: 20})
		}
	}
	return criteria
}

func extractCriterionLines(content string) []string {
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(strings.Trim(line, "-*•0123456789.、 \t"))
		if len([]rune(trimmed)) < 4 {
			continue
		}
		lines = append(lines, trimmed)
		if len(lines) >= 20 {
			break
		}
	}
	return lines
}

func scoreCriterion(content string, criterion QualityCriterion) (int, []string, []string) {
	var matched []string
	var missing []string
	for _, keyword := range criterion.Keywords {
		normalized := normalizeQualityText(keyword)
		if normalized == "" {
			continue
		}
		if strings.Contains(content, normalized) {
			matched = append(matched, keyword)
		} else {
			missing = append(missing, keyword)
		}
	}
	if len(criterion.Keywords) == 0 {
		return 0, matched, missing
	}
	score := int(math.Round(float64(len(matched)) / float64(len(criterion.Keywords)) * 100))
	return score, matched, missing
}

func keywordsFromCriterion(value string) []string {
	seen := map[string]struct{}{}
	var keywords []string
	for _, marker := range []string{"必须包含", "应包含", "需包含", "包含", "要求", "必须", "需要"} {
		if index := strings.Index(value, marker); index >= 0 {
			tail := strings.TrimSpace(value[index+len(marker):])
			if len([]rune(tail)) >= 2 && len([]rune(tail)) <= 12 {
				keywords = append(keywords, strings.Trim(tail, "：:，。；;、 "))
				return keywords
			}
		}
	}
	for _, token := range strings.FieldsFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || strings.ContainsRune("，。；、：:（）()【】[]{}<>《》/\\|", r)
	}) {
		token = strings.TrimSpace(token)
		runeCount := len([]rune(token))
		if runeCount < 2 || runeCount > 12 {
			continue
		}
		normalized := normalizeQualityText(token)
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		keywords = append(keywords, token)
		if len(keywords) >= 8 {
			break
		}
	}
	return keywords
}

func normalizeQualityText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), ""))
}

func optional(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
