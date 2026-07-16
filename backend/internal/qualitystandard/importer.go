package qualitystandard

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"aiops-platform/backend/internal/document"
	"aiops-platform/backend/internal/model"
)

var (
	ErrImporterNotConfigured = errors.New("quality standard importer is not configured")
	ErrUnsupportedImportFile = errors.New("only .docx and .xlsx quality standards are supported")
	ErrImportFileTooLarge    = errors.New("quality standard import file is too large")
)

type ImportOptions struct {
	Name        string
	Version     string
	ProfileKey  string
	ProfileName string
}

type ImportWarning struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Location string `json:"location,omitempty"`
}

type ImportResult struct {
	Import     model.KBQualityStandardImport      `json:"import"`
	Draft      *model.KBStructuredQualityStandard `json:"draft,omitempty"`
	Validation ValidationResult                   `json:"validation"`
	Warnings   []ImportWarning                    `json:"warnings"`
}

type ImportValidationError struct{ Result *ImportResult }

func (e *ImportValidationError) Error() string { return "imported quality standard failed validation" }

type Importer struct {
	baseDir        string
	maxUploadBytes int64
	registry       *document.ParserRegistry
}

func NewImporter(localFileDir string, maxUploadBytes int64) (*Importer, error) {
	if strings.TrimSpace(localFileDir) == "" || maxUploadBytes <= 0 {
		return nil, fmt.Errorf("invalid quality standard importer configuration")
	}
	base, err := filepath.Abs(filepath.Join(localFileDir, "quality-standard-imports"))
	if err != nil {
		return nil, fmt.Errorf("resolve quality standard import dir: %w", err)
	}
	registry, err := document.NewDefaultParserRegistry(document.ParseLimits{MaxBytes: maxUploadBytes})
	if err != nil {
		return nil, fmt.Errorf("create quality standard parser: %w", err)
	}
	return &Importer{baseDir: base, maxUploadBytes: maxUploadBytes, registry: registry}, nil
}

func (s *Service) Import(ctx context.Context, actorID int64, header *multipart.FileHeader, options ImportOptions) (*ImportResult, error) {
	if s.importer == nil {
		return nil, ErrImporterNotConfigured
	}
	if header == nil {
		return nil, ErrInvalidStandard
	}
	record, ast, parseErr := s.importer.saveAndParse(ctx, actorID, header)
	if record == nil {
		return nil, parseErr
	}
	if err := s.repository.CreateQualityStandardImport(ctx, record); err != nil {
		return nil, err
	}
	result := &ImportResult{Import: *record, Warnings: []ImportWarning{}, Validation: ValidationResult{}}
	if parseErr != nil {
		record.Status = "validation_failed"
		record.ValidationErrors = mustJSON([]string{parseErr.Error()})
		_ = s.repository.UpdateQualityStandardImport(ctx, record)
		result.Import = *record
		result.Validation = ValidationResult{Errors: []string{parseErr.Error()}}
		return result, &ImportValidationError{Result: result}
	}
	record.ParserName, record.ParserVersion = stringPointer(ast.ParserName), stringPointer(ast.ParserVersion)
	standard, warnings := buildStructuredDraft(ast, options)
	result.Draft, result.Warnings = standard, warnings
	validation := Validate(standard)
	result.Validation = validation
	record.Warnings = mustJSON(warnings)
	record.Preview = mustJSON(standard)
	if !validation.Valid {
		record.Status = "validation_failed"
		record.ValidationErrors = mustJSON(validation.Errors)
		if err := s.repository.UpdateQualityStandardImport(ctx, record); err != nil {
			return nil, err
		}
		result.Import = *record
		return result, &ImportValidationError{Result: result}
	}
	created, err := s.Create(ctx, actorID, standard)
	if err != nil {
		record.Status = "validation_failed"
		record.ValidationErrors = mustJSON([]string{err.Error()})
		_ = s.repository.UpdateQualityStandardImport(ctx, record)
		result.Import = *record
		result.Validation = ValidationResult{Errors: []string{err.Error()}}
		return result, &ImportValidationError{Result: result}
	}
	record.StandardID = &created.ID
	record.Status = "awaiting_confirmation"
	record.ValidationErrors = mustJSON([]string{})
	record.Preview = mustJSON(created)
	if err := s.repository.UpdateQualityStandardImport(ctx, record); err != nil {
		return nil, err
	}
	result.Import, result.Draft = *record, created
	return result, nil
}

func (i *Importer) saveAndParse(ctx context.Context, actorID int64, header *multipart.FileHeader) (*model.KBQualityStandardImport, *document.DocumentAST, error) {
	name := strings.TrimSpace(header.Filename)
	if name == "" || name != filepath.Base(name) || strings.Contains(name, "\\") || len(name) > 255 {
		return nil, nil, ErrInvalidStandard
	}
	ext := strings.ToLower(filepath.Ext(name))
	fileType := strings.TrimPrefix(ext, ".")
	if fileType != "docx" && fileType != "xlsx" {
		return nil, nil, ErrUnsupportedImportFile
	}
	if err := os.MkdirAll(i.baseDir, 0o750); err != nil {
		return nil, nil, fmt.Errorf("create quality standard import dir: %w", err)
	}
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return nil, nil, fmt.Errorf("generate import file name: %w", err)
	}
	path := filepath.Join(i.baseDir, hex.EncodeToString(token)+ext)
	source, err := header.Open()
	if err != nil {
		return nil, nil, fmt.Errorf("open quality standard upload: %w", err)
	}
	defer source.Close()
	target, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return nil, nil, fmt.Errorf("create quality standard upload: %w", err)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(target, hash), io.LimitReader(source, i.maxUploadBytes+1))
	closeErr := target.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(path)
		if copyErr != nil {
			return nil, nil, fmt.Errorf("save quality standard upload: %w", copyErr)
		}
		return nil, nil, fmt.Errorf("close quality standard upload: %w", closeErr)
	}
	if written <= 0 {
		_ = os.Remove(path)
		return nil, nil, ErrInvalidStandard
	}
	if written > i.maxUploadBytes {
		_ = os.Remove(path)
		return nil, nil, ErrImportFileTooLarge
	}
	record := &model.KBQualityStandardImport{OriginalFileName: name, StoredFilePath: path, FileType: fileType, FileSize: written, FileHash: hex.EncodeToString(hash.Sum(nil)), Status: "uploaded", Warnings: mustJSON([]ImportWarning{}), ValidationErrors: mustJSON([]string{}), CreatedBy: &actorID}
	ast, err := i.registry.Parse(ctx, document.ParseRequest{Path: path, FileName: name, FileType: fileType, Title: strings.TrimSuffix(name, ext)})
	return record, ast, err
}

type importRow struct {
	values   map[string]string
	location string
}

func buildStructuredDraft(ast *document.DocumentAST, options ImportOptions) (*model.KBStructuredQualityStandard, []ImportWarning) {
	warnings := make([]ImportWarning, 0)
	rows := extractImportRows(ast, &warnings)
	name := strings.TrimSpace(options.Name)
	if name == "" {
		name = strings.TrimSpace(ast.Title)
	}
	if name == "" {
		name = "imported-quality-standard"
		warnings = append(warnings, ImportWarning{Code: "inferred_standard_name", Message: "Standard name was inferred from the file name."})
	}
	version := strings.TrimSpace(options.Version)
	if version == "" {
		version = "v1.0"
		warnings = append(warnings, ImportWarning{Code: "default_version", Message: "Version was not supplied; v1.0 was used."})
	}
	standard := &model.KBStructuredQualityStandard{Name: name, Version: version, Status: model.QualityStandardDraft}
	type criterionBuilder struct {
		criterion model.KBQualityCriterion
	}
	type profileBuilder struct {
		profile  model.KBQualityProfile
		criteria map[string]*criterionBuilder
		order    []string
		docTypes map[string]struct{}
	}
	profiles, profileOrder := map[string]*profileBuilder{}, []string{}
	for rowIndex, row := range rows {
		profileKey := cleanKey(row.values["profile"])
		if profileKey == "" {
			profileKey = cleanKey(options.ProfileKey)
		}
		if profileKey == "" {
			if ast.ParserName == "excelize" {
				warnings = appendUniqueWarning(warnings, ImportWarning{Code: "missing_profile", Message: "Excel rows must identify a profile.", Location: row.location})
			} else {
				profileKey = "default"
				warnings = appendUniqueWarning(warnings, ImportWarning{Code: "missing_profile", Message: "Profile was missing; default was inferred for the Word draft.", Location: row.location})
			}
		}
		pb := profiles[profileKey]
		if pb == nil {
			profileName := strings.TrimSpace(options.ProfileName)
			if profileName == "" {
				profileName = strings.TrimSpace(row.values["profile"])
				if profileName == "" {
					profileName = profileKey
				}
			}
			pb = &profileBuilder{profile: model.KBQualityProfile{ProfileKey: profileKey, Name: profileName, Status: model.QualityStandardDraft, GatePolicy: json.RawMessage(`{"violationResult":"blocked"}`)}, criteria: map[string]*criterionBuilder{}, docTypes: map[string]struct{}{}}
			profiles[profileKey] = pb
			profileOrder = append(profileOrder, profileKey)
		}
		for _, dt := range splitList(row.values["doc_type"]) {
			pb.docTypes[dt] = struct{}{}
		}
		criterionName := strings.TrimSpace(row.values["criterion_name"])
		criterionKey := cleanKey(row.values["criterion_key"])
		if criterionKey == "" && criterionName != "" {
			criterionKey = inferredKey(criterionName, "criterion", len(pb.order)+1)
			warnings = append(warnings, ImportWarning{Code: "inferred_criterion_key", Message: "Criterion key was inferred from its title.", Location: row.location})
		}
		if criterionName == "" {
			criterionName = criterionKey
		}
		cb := pb.criteria[criterionKey]
		if cb == nil {
			weight, _ := parseNumber(row.values["criterion_weight"])
			if weight > 1 && weight <= 100 {
				weight /= 100
				warnings = append(warnings, ImportWarning{Code: "normalized_weight", Message: "Criterion weight was interpreted as a percentage.", Location: row.location})
			}
			cb = &criterionBuilder{criterion: model.KBQualityCriterion{CriterionKey: criterionKey, Name: criterionName, Weight: weight, ScoringMethod: "hybrid", OrderNo: len(pb.order) + 1}}
			pb.criteria[criterionKey] = cb
			pb.order = append(pb.order, criterionKey)
		} else if value, ok := parseNumber(row.values["criterion_weight"]); ok && value > 0 {
			if value > 1 && value <= 100 {
				value /= 100
			}
			if cb.criterion.Weight != 0 && abs(cb.criterion.Weight-value) > 0.0001 {
				warnings = append(warnings, ImportWarning{Code: "conflicting_criterion_weight", Message: "Conflicting criterion weights were found; the first value was kept.", Location: row.location})
			}
		}
		ruleName := strings.TrimSpace(row.values["rule_name"])
		ruleKey := cleanKey(row.values["rule_key"])
		if ruleKey == "" && ruleName != "" {
			ruleKey = inferredKey(ruleName, "rule", rowIndex+1)
			warnings = append(warnings, ImportWarning{Code: "inferred_rule_key", Message: "Rule key was inferred from its name.", Location: row.location})
		}
		if ruleName == "" {
			ruleName = ruleKey
		}
		description := optionalText(row.values["rule_description"])
		maxScore, _ := parseNumber(row.values["max_score"])
		deduction, hasDeduction := parseNumber(row.values["deduction"])
		ruleType := strings.ToLower(strings.TrimSpace(row.values["rule_type"]))
		if ruleType == "" {
			ruleType = "manual"
			warnings = append(warnings, ImportWarning{Code: "default_rule_type", Message: "Rule type was missing; manual was used.", Location: row.location})
		}
		severity := strings.ToLower(strings.TrimSpace(row.values["severity"]))
		if severity == "" {
			severity = "medium"
		}
		evidence := parseJSONObject(row.values["evidence_requirement"])
		detector := parseJSONObject(row.values["detector_config"])
		examples := map[string]string{}
		if value := strings.TrimSpace(row.values["positive_example"]); value != "" {
			examples["positive"] = value
		}
		if value := strings.TrimSpace(row.values["negative_example"]); value != "" {
			examples["negative"] = value
		}
		rule := model.KBQualityRule{RuleKey: ruleKey, Name: ruleName, Description: description, RuleType: ruleType, Severity: severity, MaxScore: maxScore, Required: parseBool(row.values["required"]), HardGate: parseBool(row.values["hard_gate"]), EvidenceRequirement: evidence, DetectorConfig: detector, LLMInstruction: optionalText(row.values["llm_instruction"]), OrderNo: len(cb.criterion.Rules) + 1}
		if hasDeduction {
			rule.Deduction = &deduction
		}
		if len(examples) > 0 {
			rule.Examples = mustJSON(examples)
		}
		cb.criterion.Rules = append(cb.criterion.Rules, rule)
		cb.criterion.MaxScore += maxScore
	}
	for _, key := range profileOrder {
		pb := profiles[key]
		docTypes := sortedKeys(pb.docTypes)
		if len(docTypes) == 0 {
			docTypes = []string{"all"}
			warnings = appendUniqueWarning(warnings, ImportWarning{Code: "default_doc_type", Message: "Applicable document type was missing; all was used."})
		}
		pb.profile.ApplicableDocTypes = mustJSON(docTypes)
		for _, criterionKey := range pb.order {
			pb.profile.Criteria = append(pb.profile.Criteria, pb.criteria[criterionKey].criterion)
			pb.profile.TotalScore += pb.criteria[criterionKey].criterion.MaxScore
		}
		pb.profile.PassScore = round2(pb.profile.TotalScore * 0.8)
		pb.profile.WarningScore = round2(pb.profile.TotalScore * 0.7)
		standard.Profiles = append(standard.Profiles, pb.profile)
	}
	return standard, warnings
}

func extractImportRows(ast *document.DocumentAST, warnings *[]ImportWarning) []importRow {
	rows := []importRow{}
	for _, block := range ast.Blocks {
		if block.Type != document.BlockTypeTable || len(block.Children) < 2 {
			continue
		}
		headings := make([]string, len(block.Children[0].Children))
		for i, cell := range block.Children[0].Children {
			headings[i] = canonicalHeader(cell.Text, warnings, fmt.Sprintf("block:%s header:%d", block.ID, i+1))
		}
		criterionFromHeading := ""
		if len(block.SectionPath) > 0 {
			criterionFromHeading = block.SectionPath[len(block.SectionPath)-1]
		}
		for ri, row := range block.Children[1:] {
			values := map[string]string{}
			for ci, cell := range row.Children {
				if ci < len(headings) && headings[ci] != "" {
					values[headings[ci]] = strings.TrimSpace(cell.Text)
				}
			}
			if values["criterion_name"] == "" && criterionFromHeading != "" {
				values["criterion_name"] = criterionFromHeading
			}
			rows = append(rows, importRow{values: values, location: fmt.Sprintf("block:%s row:%d", block.ID, ri+2)})
		}
	}
	return rows
}

var exactHeaders = map[string]string{"profile": "profile", "doc_type": "doc_type", "criterion_key": "criterion_key", "criterion_name": "criterion_name", "criterion_weight": "criterion_weight", "rule_key": "rule_key", "rule_name": "rule_name", "rule_description": "rule_description", "rule_type": "rule_type", "max_score": "max_score", "deduction": "deduction", "required": "required", "hard_gate": "hard_gate", "severity": "severity", "evidence_requirement": "evidence_requirement", "detector_config": "detector_config", "llm_instruction": "llm_instruction", "positive_example": "positive_example", "negative_example": "negative_example"}
var aliasHeaders = map[string]string{"评分项": "rule_name", "规则": "rule_name", "规则名称": "rule_name", "说明": "rule_description", "描述": "rule_description", "分值": "max_score", "得分": "max_score", "权重": "criterion_weight", "必填": "required", "一票否决": "hard_gate", "硬门禁": "hard_gate", "严重度": "severity", "维度": "criterion_name", "评分维度": "criterion_name", "criterion": "criterion_name", "rule": "rule_name", "score": "max_score"}

func canonicalHeader(value string, warnings *[]ImportWarning, location string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.NewReplacer(" ", "_", "-", "_").Replace(normalized)
	if key := exactHeaders[normalized]; key != "" {
		return key
	}
	if key := aliasHeaders[normalized]; key != "" {
		*warnings = append(*warnings, ImportWarning{Code: "ambiguous_header_mapping", Message: fmt.Sprintf("Header %q was mapped to %s.", value, key), Location: location})
		return key
	}
	if normalized != "" {
		*warnings = append(*warnings, ImportWarning{Code: "unknown_header", Message: fmt.Sprintf("Header %q was ignored.", value), Location: location})
	}
	return ""
}

var nonKey = regexp.MustCompile(`[^a-z0-9]+`)

func cleanKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.Trim(nonKey.ReplaceAllString(value, "_"), "_")
}
func inferredKey(value, prefix string, index int) string {
	if key := cleanKey(value); key != "" {
		return key
	}
	return fmt.Sprintf("%s_%d", prefix, index)
}
func parseNumber(value string) (float64, bool) {
	value = strings.TrimSpace(strings.TrimSuffix(value, "%"))
	if value == "" {
		return 0, false
	}
	number, err := strconv.ParseFloat(value, 64)
	return number, err == nil
}
func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "y", "是", "必填", "一票否决":
		return true
	}
	return false
}
func splitList(value string) []string {
	return strings.FieldsFunc(strings.TrimSpace(value), func(r rune) bool { return r == ',' || r == ';' || r == '，' || r == '；' || unicode.IsSpace(r) })
}
func optionalText(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
func parseJSONObject(value string) json.RawMessage {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var object map[string]any
	if json.Unmarshal([]byte(value), &object) == nil {
		return json.RawMessage(value)
	}
	return mustJSON(map[string]string{"text": value})
}
func appendUniqueWarning(values []ImportWarning, value ImportWarning) []ImportWarning {
	for _, current := range values {
		if current.Code == value.Code && current.Location == value.Location {
			return values
		}
	}
	return append(values, value)
}
func sortedKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
func mustJSON(value any) json.RawMessage { data, _ := json.Marshal(value); return data }
func stringPointer(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}
func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
func round2(value float64) float64 {
	rounded, _ := strconv.ParseFloat(fmt.Sprintf("%.2f", value), 64)
	return rounded
}
