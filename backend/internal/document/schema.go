package document

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"aiops-platform/backend/internal/model"
)

const DocumentSchemaVersion = "ops-schema-v1"

var supportedDocumentSchemas = map[string][]string{
	"runbook":        {"title", "version", "applicable_system", "applicable_environment", "applicable_component", "scenario", "prerequisites", "risk_level", "steps", "verification", "rollback", "escalation", "owner", "reviewer", "last_reviewed_at"},
	"alert_handbook": {"alert_name", "alert_meaning", "trigger_condition", "impact", "common_causes", "evidence_to_collect", "diagnostic_steps", "recovery_criteria", "escalation", "risk_warning", "owner"},
	"emergency_plan": {"incident_level", "trigger_condition", "roles", "communication", "containment", "recovery", "fallback", "data_consistency_check", "verification", "exit_criteria", "post_incident"},
	"change_plan":    {}, "rollback_plan": {}, "architecture": {}, "dependency": {}, "capacity": {},
	"database_manual": {}, "middleware_manual": {}, "k8s_manual": {}, "incident_postmortem": {},
	"faq": {}, "policy": {}, "scoring_standard": {},
}

var sectionAliases = map[string]map[string][]string{
	"runbook": {
		"scenario": {"scenario", "场景", "适用场景"}, "prerequisites": {"prerequisite", "前置", "准备"},
		"risk_level": {"risk", "warning", "风险", "警告", "注意"}, "steps": {"step", "procedure", "步骤", "操作", "处置"},
		"verification": {"verification", "verify", "validation", "验证", "确认", "检查结果"},
		"rollback":     {"rollback", "fallback", "回滚", "回退", "降级"}, "escalation": {"escalation", "升级", "上报"},
		"owner": {"owner", "负责人"}, "reviewer": {"reviewer", "审核人"}, "last_reviewed_at": {"last reviewed", "review date", "复审日期", "审核日期"},
	},
	"alert_handbook": {
		"alert_meaning": {"meaning", "description", "告警含义", "告警说明"}, "trigger_condition": {"trigger", "condition", "触发条件", "阈值"},
		"impact": {"impact", "影响"}, "common_causes": {"cause", "原因", "根因"},
		"evidence_to_collect": {"evidence", "collect", "证据", "采集"}, "diagnostic_steps": {"diagnostic", "troubleshoot", "诊断", "排查"},
		"recovery_criteria": {"recovery criteria", "恢复标准", "恢复条件"}, "escalation": {"escalation", "升级", "上报"},
		"risk_warning": {"risk", "warning", "风险", "警告"}, "owner": {"owner", "负责人"},
	},
	"emergency_plan": {
		"incident_level": {"incident level", "事件等级", "故障等级"}, "trigger_condition": {"trigger", "启动条件", "触发条件"},
		"roles": {"role", "职责", "角色"}, "communication": {"communication", "沟通", "通报"},
		"containment": {"containment", "止损", "遏制"}, "recovery": {"recovery", "恢复"}, "fallback": {"fallback", "rollback", "降级", "回退"},
		"data_consistency_check": {"data consistency", "数据一致性"}, "verification": {"verification", "验证"},
		"exit_criteria": {"exit criteria", "退出条件", "结束条件"}, "post_incident": {"post incident", "复盘", "事后"},
	},
}

var (
	commandPattern          = regexp.MustCompile(`(?i)(?:^|\s)(kubectl|systemctl|journalctl|curl|wget|ssh|psql|mysql|redis-cli|helm|docker|grep|awk|sed|rm|mv|cp)\b[^\n]*`)
	dangerousCommandPattern = regexp.MustCompile(`(?i)(rm\s+-rf|kubectl\s+delete|drop\s+(table|database)|truncate\s+table|flushall|shutdown|reboot)`)
	errorCodePattern        = regexp.MustCompile(`\b(?:SQLSTATE\s+[A-Z0-9]+|ORA-\d+|[45]\d{2}|[A-Z][A-Z0-9]*[_-][A-Z0-9_-]+|[A-Z]+\d+[A-Z0-9]*)\b`)
	environmentPattern      = regexp.MustCompile(`(?i)\b(prod(?:uction)?|staging|stage|test|dev(?:elopment)?)\b|生产环境|测试环境|开发环境|预发环境`)
)

type DocumentSchemaExtraction struct {
	SchemaVersion string                    `json:"schemaVersion"`
	DocumentType  string                    `json:"documentType"`
	TypeInferred  bool                      `json:"typeInferred"`
	Confidence    float64                   `json:"confidence"`
	Fields        map[string]ExtractedField `json:"fields"`
	MissingFields []string                  `json:"missingFields"`
	Entities      []DocumentEntity          `json:"entities"`
	Sections      []SectionClassification   `json:"sections"`
	Diagnostics   []SchemaDiagnostic        `json:"diagnostics"`
}

type ExtractedField struct {
	Name       string           `json:"name"`
	Values     []string         `json:"values"`
	Evidence   []SchemaEvidence `json:"evidence"`
	Inferred   bool             `json:"inferred"`
	Confidence float64          `json:"confidence"`
}

type SchemaEvidence struct {
	Source   string `json:"source"`
	BlockKey string `json:"blockKey,omitempty"`
	Page     *int   `json:"page,omitempty"`
	Text     string `json:"text"`
}

type DocumentEntity struct {
	Type       string         `json:"type"`
	Value      string         `json:"value"`
	BlockKey   string         `json:"blockKey,omitempty"`
	Page       *int           `json:"page,omitempty"`
	Inferred   bool           `json:"inferred"`
	Confidence float64        `json:"confidence"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

type SectionClassification struct {
	BlockKey   string  `json:"blockKey"`
	Section    string  `json:"section"`
	Text       string  `json:"text"`
	Inferred   bool    `json:"inferred"`
	Confidence float64 `json:"confidence"`
}

type SchemaDiagnostic struct {
	Code       string `json:"code"`
	Severity   string `json:"severity"`
	BlockKey   string `json:"blockKey,omitempty"`
	Message    string `json:"message"`
	SourceText string `json:"sourceText,omitempty"`
}

func ExtractDocumentSchema(document *model.KBDocument, ast *DocumentAST) DocumentSchemaExtraction {
	if ast == nil {
		ast = &DocumentAST{}
	}
	documentType, inferred, typeConfidence := resolveDocumentType(document, ast)
	extraction := DocumentSchemaExtraction{
		SchemaVersion: DocumentSchemaVersion, DocumentType: documentType, TypeInferred: inferred,
		Confidence: typeConfidence, Fields: map[string]ExtractedField{}, Entities: []DocumentEntity{},
		Sections: []SectionClassification{}, Diagnostics: []SchemaDiagnostic{}, MissingFields: []string{},
	}
	for _, name := range supportedDocumentSchemas[documentType] {
		extraction.Fields[name] = ExtractedField{Name: name, Values: []string{}, Evidence: []SchemaEvidence{}}
	}
	addDocumentMetadataFields(&extraction, document)
	flat := flattenDocumentBlocks(ast.Blocks)
	if len(flat) > 0 {
		addTitleField(&extraction, ast, flat)
	}
	for _, block := range flat {
		classifySchemaBlock(&extraction, block)
		extractBlockEntities(&extraction, block)
	}
	for name, field := range extraction.Fields {
		if len(field.Values) == 0 {
			extraction.MissingFields = append(extraction.MissingFields, name)
		}
	}
	sort.Strings(extraction.MissingFields)
	extraction.Confidence = extractionConfidence(extraction)
	return extraction
}

func (extraction DocumentSchemaExtraction) JSON() []byte {
	value, _ := json.Marshal(extraction)
	return value
}

func resolveDocumentType(document *model.KBDocument, ast *DocumentAST) (string, bool, float64) {
	if document != nil && document.DocType != nil {
		value := strings.ToLower(strings.TrimSpace(*document.DocType))
		if _, ok := supportedDocumentSchemas[value]; ok {
			return value, false, 1
		}
	}
	if ast == nil {
		ast = &DocumentAST{}
	}
	text := strings.ToLower(ast.Title + " " + documentBlockText(ast.Blocks))
	typeSignals := []struct {
		name    string
		signals []string
	}{
		{"alert_handbook", []string{"alert", "告警", "trigger condition", "recovery criteria"}},
		{"emergency_plan", []string{"emergency", "应急预案", "incident level", "启动条件"}},
		{"runbook", []string{"runbook", "操作手册", "排障手册", "rollback", "回滚", "verification", "验证"}},
	}
	bestType, bestScore := "runbook", 0
	for _, candidate := range typeSignals {
		score := 0
		for _, signal := range candidate.signals {
			if strings.Contains(text, signal) {
				score++
			}
		}
		if score > bestScore {
			bestType, bestScore = candidate.name, score
		}
	}
	confidence := 0.45 + float64(bestScore)*0.12
	if confidence > 0.85 {
		confidence = 0.85
	}
	return bestType, true, confidence
}

func addDocumentMetadataFields(extraction *DocumentSchemaExtraction, document *model.KBDocument) {
	if document == nil {
		return
	}
	metadata := map[string]*string{"applicable_system": document.SystemName, "applicable_environment": document.Environment, "applicable_component": document.ComponentName}
	for name, value := range metadata {
		if value != nil {
			addFieldValue(extraction, name, *value, SchemaEvidence{Source: "document_metadata", Text: *value}, false, 1)
		}
	}
	if document.Version != "" {
		addFieldValue(extraction, "version", document.Version, SchemaEvidence{Source: "document_metadata", Text: document.Version}, false, 1)
	}
	if document.Title != "" && extraction.DocumentType == "runbook" {
		addFieldValue(extraction, "title", document.Title, SchemaEvidence{Source: "document_metadata", Text: document.Title}, false, 1)
	}
}

func addTitleField(extraction *DocumentSchemaExtraction, ast *DocumentAST, blocks []DocumentBlock) {
	fieldName := "title"
	if extraction.DocumentType == "alert_handbook" {
		fieldName = "alert_name"
	}
	if _, ok := extraction.Fields[fieldName]; !ok {
		return
	}
	for _, block := range blocks {
		if block.Type == BlockTypeHeading {
			addFieldValue(extraction, fieldName, block.Text, blockEvidence(block), true, 0.95)
			return
		}
	}
	if ast.Title != "" {
		addFieldValue(extraction, fieldName, ast.Title, SchemaEvidence{Source: "parser_title", Text: ast.Title}, true, 0.75)
	}
}

func classifySchemaBlock(extraction *DocumentSchemaExtraction, block DocumentBlock) {
	fieldName, confidence := matchSectionField(extraction.DocumentType, block)
	if fieldName == "" {
		return
	}
	extraction.Sections = append(extraction.Sections, SectionClassification{BlockKey: block.ID, Section: fieldName, Text: block.Text, Inferred: true, Confidence: confidence})
	if block.Type != BlockTypeHeading && block.Text != "" {
		addFieldValue(extraction, fieldName, block.Text, blockEvidence(block), true, confidence)
	}
	if block.Type == BlockTypeTable {
		for _, row := range block.Children {
			for _, cell := range row.Children {
				if strings.TrimSpace(cell.Text) != "" {
					addFieldValue(extraction, fieldName, cell.Text, blockEvidence(cell), true, confidence)
				}
			}
		}
	}
}

func matchSectionField(documentType string, block DocumentBlock) (string, float64) {
	candidates := []string{block.Text}
	if len(block.SectionPath) > 0 {
		candidates = append(candidates, block.SectionPath[len(block.SectionPath)-1])
	}
	aliases := sectionAliases[documentType]
	for field, values := range aliases {
		for _, candidate := range candidates {
			normalized := strings.ToLower(strings.TrimSpace(candidate))
			for _, alias := range values {
				if strings.Contains(normalized, strings.ToLower(alias)) {
					return field, 0.9
				}
			}
		}
	}
	return "", 0
}

func extractBlockEntities(extraction *DocumentSchemaExtraction, block DocumentBlock) {
	text := block.Text
	for _, match := range environmentPattern.FindAllString(text, -1) {
		addEntity(extraction, "environment", match, block, 0.9, nil)
	}
	for _, match := range errorCodePattern.FindAllString(text, -1) {
		addEntity(extraction, "error_code", match, block, 0.8, nil)
	}
	commands := commandPattern.FindAllString(text, -1)
	if block.Type == BlockTypeCode || block.Type == BlockTypeCommand {
		commands = append(commands, text)
	}
	seen := map[string]bool{}
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command == "" || seen[command] {
			continue
		}
		seen[command] = true
		risk := "normal"
		if dangerousCommandPattern.MatchString(command) {
			risk = "high"
			extraction.Diagnostics = append(extraction.Diagnostics, SchemaDiagnostic{Code: "command_risk", Severity: "high", BlockKey: block.ID, Message: "potentially destructive command requires an explicit risk warning", SourceText: command})
		}
		addEntity(extraction, "command", command, block, 0.95, map[string]any{"risk": risk})
	}
	if block.Type == BlockTypeTable {
		for _, row := range block.Children {
			for _, cell := range row.Children {
				if strings.TrimSpace(cell.Text) == "" {
					extraction.Diagnostics = append(extraction.Diagnostics, SchemaDiagnostic{Code: "empty_table_cell", Severity: "warning", BlockKey: block.ID, Message: "table contains an empty cell"})
					return
				}
			}
		}
	}
}

func addEntity(extraction *DocumentSchemaExtraction, entityType, value string, block DocumentBlock, confidence float64, attributes map[string]any) {
	for _, entity := range extraction.Entities {
		if entity.Type == entityType && entity.Value == value && entity.BlockKey == block.ID {
			return
		}
	}
	extraction.Entities = append(extraction.Entities, DocumentEntity{Type: entityType, Value: value, BlockKey: block.ID, Page: block.Page, Inferred: true, Confidence: confidence, Attributes: attributes})
}

func addFieldValue(extraction *DocumentSchemaExtraction, name, value string, evidence SchemaEvidence, inferred bool, confidence float64) {
	field, ok := extraction.Fields[name]
	if !ok || strings.TrimSpace(value) == "" {
		return
	}
	for _, existing := range field.Values {
		if existing == value {
			return
		}
	}
	wasEmpty := len(field.Values) == 0
	field.Values = append(field.Values, value)
	field.Evidence = append(field.Evidence, evidence)
	if wasEmpty {
		field.Inferred = inferred
	} else {
		field.Inferred = field.Inferred && inferred
	}
	if confidence > field.Confidence {
		field.Confidence = confidence
	}
	extraction.Fields[name] = field
}

func blockEvidence(block DocumentBlock) SchemaEvidence {
	return SchemaEvidence{Source: "document_block", BlockKey: block.ID, Page: block.Page, Text: block.Text}
}

func flattenDocumentBlocks(blocks []DocumentBlock) []DocumentBlock {
	result := make([]DocumentBlock, 0, countBlocks(blocks))
	for _, block := range blocks {
		result = append(result, block)
		result = append(result, flattenDocumentBlocks(block.Children)...)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Order < result[j].Order })
	return result
}

func documentBlockText(blocks []DocumentBlock) string {
	var builder strings.Builder
	for _, block := range blocks {
		builder.WriteString(block.Text)
		builder.WriteByte('\n')
		builder.WriteString(documentBlockText(block.Children))
	}
	return builder.String()
}

func extractionConfidence(extraction DocumentSchemaExtraction) float64 {
	if len(extraction.Fields) == 0 {
		return extraction.Confidence
	}
	present := len(extraction.Fields) - len(extraction.MissingFields)
	coverage := float64(present) / float64(len(extraction.Fields))
	return extraction.Confidence*0.6 + coverage*0.4
}
