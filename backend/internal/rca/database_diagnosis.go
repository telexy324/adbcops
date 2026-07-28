package rca

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/skillframework"
	tidbsvc "aiops-platform/backend/internal/tidb"
)

const DatabaseDiagnosisPlanVersion = "database-diagnosis-plan-v1"

var (
	ErrDatabaseProviderUnsupported = errors.New("database diagnosis provider unsupported")
	ErrDatabaseScopeInvalid        = errors.New("database diagnosis scope invalid")
)

// DatabaseDiagnosisProvider converts a confirmed database hypothesis into
// bounded, read-only Skill actions. It is deliberately database-neutral:
// providers for MySQL or PostgreSQL must be registered explicitly and may not
// claim support by falling back to the TiDB implementation.
type DatabaseDiagnosisProvider interface {
	Name() string
	SourceType() string
	BuildPlan(DatabaseDiagnosisRequest) (*DatabaseDiagnosisPlan, error)
}

type DatabaseDiagnosisRequest struct {
	DataSourceID          int64
	ServiceName           string
	Environment           string
	Minutes               int
	Limit                 int
	ReadonlySQL           string
	EvidenceIDs           []int64
	CorrelationDimensions []string
}

type DatabaseDiagnosisPlan struct {
	Version               string                       `json:"version"`
	Provider              string                       `json:"provider"`
	SourceType            string                       `json:"sourceType"`
	DataSourceID          int64                        `json:"dataSourceId"`
	ServiceName           string                       `json:"serviceName,omitempty"`
	Environment           string                       `json:"environment,omitempty"`
	WindowMinutes         int                          `json:"windowMinutes"`
	CorrelationDimensions []string                     `json:"correlationDimensions"`
	SQLFingerprint        string                       `json:"sqlFingerprint,omitempty"`
	SanitizedSQL          string                       `json:"sanitizedSql,omitempty"`
	Actions               []PlannerAction              `json:"actions"`
	MissingEvidence       []string                     `json:"missingEvidence"`
	SupportingIDs         []int64                      `json:"supportingEvidenceIds"`
	ConfidencePolicy      string                       `json:"confidencePolicy"`
	Assessment            *DatabaseDiagnosisAssessment `json:"assessment,omitempty"`
}

type DatabaseDiagnosisAssessment struct {
	Status                   string                      `json:"status"`
	HighestImpactFingerprint string                      `json:"highestImpactFingerprint,omitempty"`
	Categories               []DatabaseDiagnosisCategory `json:"categories"`
	EvidenceIDs              []int64                     `json:"evidenceIds"`
	MissingEvidence          []string                    `json:"missingEvidence"`
	RootCauseEligible        bool                        `json:"rootCauseEligible"`
	Confidence               string                      `json:"confidence"`
	Conclusion               string                      `json:"conclusion"`
}

type DatabaseDiagnosisCategory struct {
	Category    string  `json:"category"`
	SourceSkill string  `json:"sourceSkill"`
	Collected   bool    `json:"collected"`
	EvidenceIDs []int64 `json:"evidenceIds"`
}

type TiDBDatabaseDiagnosisProvider struct{}

func NewTiDBDatabaseDiagnosisProvider() DatabaseDiagnosisProvider {
	return TiDBDatabaseDiagnosisProvider{}
}

func (TiDBDatabaseDiagnosisProvider) Name() string {
	return "tidb"
}

func (TiDBDatabaseDiagnosisProvider) SourceType() string {
	return model.DataSourceTypeTiDB
}

func (TiDBDatabaseDiagnosisProvider) BuildPlan(request DatabaseDiagnosisRequest) (*DatabaseDiagnosisPlan, error) {
	if request.DataSourceID <= 0 {
		return nil, ErrDatabaseScopeInvalid
	}
	if request.Minutes <= 0 || request.Minutes > 24*60 {
		request.Minutes = 60
	}
	if request.Limit <= 0 || request.Limit > 200 {
		request.Limit = 50
	}
	request.ServiceName = strings.TrimSpace(request.ServiceName)
	request.Environment = strings.TrimSpace(request.Environment)
	request.EvidenceIDs = uniqueIDs(request.EvidenceIDs)
	request.CorrelationDimensions = uniqueStrings(request.CorrelationDimensions)

	common := map[string]any{
		"dataSourceId": request.DataSourceID,
		"limit":        request.Limit,
	}
	windowed := cloneAnyMap(common)
	windowed["minutes"] = request.Minutes
	target := request.ServiceName
	if target == "" {
		target = fmt.Sprintf("tidb:%d", request.DataSourceID)
	}
	reason := "Deepen a confirmed slow-SQL hypothesis with bounded TiDB read-only evidence."
	actions := []PlannerAction{
		databasePlannerAction("slow-sql", "query_tidb_slow_queries", windowed, reason, target, request.EvidenceIDs),
		databasePlannerAction("process-pressure", "query_tidb_processlist", common, "Check connection and running-query pressure for the same TiDB source.", target, request.EvidenceIDs),
		databasePlannerAction("lock-waits", "query_tidb_lock_waits", common, "Check whether lock contention contributes to the slow-SQL impact.", target, request.EvidenceIDs),
		databasePlannerAction("hot-regions", "query_tidb_hot_regions", common, "Check whether region hotspots contribute to the slow-SQL impact.", target, request.EvidenceIDs),
		databasePlannerAction("statistics-health", "query_tidb_statistics_health", common, "Check statistics health without asserting plan regression.", target, request.EvidenceIDs),
	}
	plan := &DatabaseDiagnosisPlan{
		Version: DatabaseDiagnosisPlanVersion, Provider: "tidb", SourceType: model.DataSourceTypeTiDB,
		DataSourceID: request.DataSourceID, ServiceName: request.ServiceName, Environment: request.Environment,
		WindowMinutes: request.Minutes, CorrelationDimensions: request.CorrelationDimensions,
		Actions: actions, SupportingIDs: request.EvidenceIDs,
		ConfidencePolicy: "A root-cause claim requires slow-SQL evidence plus one supplemental source; otherwise keep confidence low.",
	}
	for _, dimension := range []string{"trace", "call_volume", "baseline"} {
		if !containsString(plan.CorrelationDimensions, dimension) {
			plan.MissingEvidence = append(plan.MissingEvidence, dimension+" correlation")
		}
	}
	if strings.TrimSpace(request.ReadonlySQL) == "" {
		plan.MissingEvidence = append(plan.MissingEvidence, "read-only SQL text for controlled EXPLAIN")
		return plan, nil
	}
	normalized, err := tidbsvc.NormalizeReadonlySQL(request.ReadonlySQL)
	if err != nil {
		plan.MissingEvidence = append(plan.MissingEvidence, "safe single-statement SELECT or SHOW for controlled EXPLAIN")
		return plan, nil
	}
	plan.SQLFingerprint = tidbsvc.SQLFingerprint(normalized)
	plan.SanitizedSQL = tidbsvc.SanitizeSQLForEvidence(normalized)
	explain := cloneAnyMap(common)
	explain["sql"] = normalized
	explain["analyze"] = false
	plan.Actions = append(plan.Actions, databasePlannerAction(
		"explain", "explain_tidb_sql", explain,
		"Run controlled EXPLAIN only after the shared read-only SQL guard accepts the statement; never use EXPLAIN ANALYZE.",
		target, request.EvidenceIDs,
	))
	return plan, nil
}

func databasePlannerAction(suffix string, skill string, input map[string]any, reason string, target string, evidenceIDs []int64) PlannerAction {
	raw, _ := json.Marshal(input)
	return PlannerAction{
		ActionKey: "database-deep-" + suffix, SkillName: skill, Input: raw,
		Reason: reason, TargetEntity: target, EvidenceIDs: append([]int64{}, evidenceIDs...),
	}
}

func cloneAnyMap(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func (s *Service) WithDatabaseDiagnosisProviders(providers ...DatabaseDiagnosisProvider) *Service {
	filtered := make([]DatabaseDiagnosisProvider, 0, len(providers))
	seen := map[string]struct{}{}
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		sourceType := strings.ToLower(strings.TrimSpace(provider.SourceType()))
		if sourceType == "" {
			continue
		}
		if _, exists := seen[sourceType]; exists {
			continue
		}
		seen[sourceType] = struct{}{}
		filtered = append(filtered, provider)
	}
	s.databaseDiagnosisProviders = filtered
	return s
}

func (s *Service) buildDatabaseDeepDiagnosisPlan(
	ctx context.Context,
	actor *model.AppUser,
	detail *Detail,
	planner *PlannerResult,
	remainingCalls int,
) (*DatabaseDiagnosisPlan, []PlannerAction) {
	if actor == nil || detail == nil || detail.Run == nil || planner == nil || remainingCalls <= 0 {
		return nil, nil
	}
	hypothesis, evidenceIDs := confirmedSlowSQLHypothesis(planner.Hypotheses, detail.Evidence)
	if hypothesis == nil {
		return nil, nil
	}
	sourceType, dataSourceID := databaseEvidenceSource(detail.Evidence, evidenceIDs)
	if dataSourceID <= 0 {
		scope := decodePlannerScope(detail.Run.Scope)
		dataSourceID = scope.int64("tidbDataSourceId")
		sourceType = model.DataSourceTypeTiDB
	}
	if dataSourceID <= 0 || sourceType == "" || !s.databaseSourceAccessible(ctx, actor, dataSourceID, sourceType) {
		planner.MissingEvidence = uniqueStrings(append(planner.MissingEvidence, "accessible read-only database data source for slow-SQL deep diagnosis"))
		return nil, nil
	}
	provider := s.databaseProvider(sourceType)
	if provider == nil {
		planner.MissingEvidence = uniqueStrings(append(planner.MissingEvidence, "registered database diagnosis provider for "+sourceType))
		return nil, nil
	}
	scope := decodePlannerScope(detail.Run.Scope)
	serviceName := scope.string("serviceName", "component")
	plan, err := provider.BuildPlan(DatabaseDiagnosisRequest{
		DataSourceID: dataSourceID,
		ServiceName:  serviceName,
		Environment:  scope.string("environment"),
		Minutes:      diagnosisWindowMinutes(scope),
		Limit:        50,
		ReadonlySQL:  scope.string("readonlySQL", "sql", "querySQL"),
		EvidenceIDs:  evidenceIDs,
		CorrelationDimensions: databaseCorrelationDimensions(
			detail.Evidence, evidenceIDs, serviceName,
		),
	})
	if err != nil || plan == nil {
		planner.MissingEvidence = uniqueStrings(append(planner.MissingEvidence, "valid database diagnosis scope"))
		return nil, nil
	}
	actions := s.filterDatabaseDiagnosisActions(actor, detail, plan.Actions, remainingCalls)
	plan.Actions = actions
	planner.MissingEvidence = uniqueStrings(append(planner.MissingEvidence, plan.MissingEvidence...))
	if len(actions) == 0 {
		return plan, nil
	}
	planner.NextActions = actions
	planner.ShouldStop = false
	planner.StopReason = ""
	return plan, actions
}

func (s *Service) databaseProvider(sourceType string) DatabaseDiagnosisProvider {
	for _, provider := range s.databaseDiagnosisProviders {
		if strings.EqualFold(provider.SourceType(), sourceType) {
			return provider
		}
	}
	return nil
}

func (s *Service) databaseSourceAccessible(ctx context.Context, actor *model.AppUser, id int64, sourceType string) bool {
	if s.dataSources == nil {
		return false
	}
	views, err := s.dataSources.List(ctx, actor)
	if err != nil {
		return false
	}
	for _, view := range views {
		if view.ID == id && view.Enabled && view.ReadOnly && strings.EqualFold(view.SourceType, sourceType) {
			return true
		}
	}
	return false
}

func (s *Service) filterDatabaseDiagnosisActions(actor *model.AppUser, detail *Detail, actions []PlannerAction, limit int) []PlannerAction {
	if s.skillCatalog == nil {
		return nil
	}
	completed := map[string]struct{}{}
	for _, action := range detail.Actions {
		completed[actionSignature(action.SkillName, action.Input)] = struct{}{}
	}
	result := make([]PlannerAction, 0, minIntRCA(len(actions), limit))
	for _, action := range actions {
		definition, err := s.skillCatalog.Get(action.SkillName)
		if err != nil || !definition.Enabled || !definition.ReadOnly || !plannerSkillAuthorized(actor, definition) {
			continue
		}
		if skillframework.ValidateJSONSchema(definition.InputSchema, action.Input) != nil {
			continue
		}
		signature := actionSignature(action.SkillName, action.Input)
		if _, exists := completed[signature]; exists {
			continue
		}
		completed[signature] = struct{}{}
		result = append(result, action)
		if len(result) >= limit {
			break
		}
	}
	return result
}

func confirmedSlowSQLHypothesis(hypotheses []PlannerHypothesis, evidence []model.EvidenceRecord) (*PlannerHypothesis, []int64) {
	evidenceByID := map[int64]model.EvidenceRecord{}
	for _, record := range evidence {
		evidenceByID[record.ID] = record
	}
	candidates := append([]PlannerHypothesis{}, hypotheses...)
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Confidence > candidates[j].Confidence })
	for index := range candidates {
		item := &candidates[index]
		if item.Confidence < .5 || !containsSlowSQL(item.ID+" "+item.Summary+" "+item.Rationale) {
			continue
		}
		support := make([]int64, 0, len(item.SupportingEvidenceIDs))
		hasSlowEvidence := false
		for _, id := range item.SupportingEvidenceIDs {
			record, exists := evidenceByID[id]
			if !exists {
				continue
			}
			support = append(support, id)
			if strings.EqualFold(record.SourceType, model.DataSourceTypeTiDB) ||
				(record.SourceSkill != nil && strings.Contains(strings.ToLower(*record.SourceSkill), "tidb")) ||
				containsSlowSQL(record.Summary+" "+string(record.Content)) {
				hasSlowEvidence = true
			}
		}
		if hasSlowEvidence {
			return item, uniqueIDs(support)
		}
	}
	return nil, nil
}

func databaseCorrelationDimensions(evidence []model.EvidenceRecord, ids []int64, serviceName string) []string {
	dimensions := []string{"time_window"}
	if strings.TrimSpace(serviceName) != "" {
		dimensions = append(dimensions, "service")
	}
	set := map[int64]struct{}{}
	for _, id := range ids {
		set[id] = struct{}{}
	}
	for _, record := range evidence {
		if _, exists := set[record.ID]; !exists {
			continue
		}
		searchable := strings.ToLower(record.Summary + " " + string(record.Content))
		for dimension, markers := range map[string][]string{
			"trace":       {"trace", "span"},
			"call_volume": {"qps", "throughput", "request count", "call volume", "调用量", "请求量"},
			"baseline":    {"baseline", "基线", "同比", "环比"},
		} {
			for _, marker := range markers {
				if strings.Contains(searchable, marker) {
					dimensions = append(dimensions, dimension)
					break
				}
			}
		}
	}
	return uniqueStrings(dimensions)
}

func databaseEvidenceSource(evidence []model.EvidenceRecord, ids []int64) (string, int64) {
	set := map[int64]struct{}{}
	for _, id := range ids {
		set[id] = struct{}{}
	}
	for _, record := range evidence {
		if _, exists := set[record.ID]; !exists || record.DataSourceID == nil || *record.DataSourceID <= 0 {
			continue
		}
		if strings.EqualFold(record.SourceType, model.DataSourceTypeTiDB) {
			return model.DataSourceTypeTiDB, *record.DataSourceID
		}
	}
	return "", 0
}

func containsSlowSQL(value string) bool {
	lower := strings.ToLower(value)
	for _, token := range []string{"slow sql", "slow-query", "slow query", "慢sql", "慢 sql", "慢查询", "数据库调用耗时"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func diagnosisWindowMinutes(scope plannerScope) int {
	from, fromErr := time.Parse(time.RFC3339, scope.string("from", "startTime", "windowStart"))
	to, toErr := time.Parse(time.RFC3339, scope.string("to", "endTime", "windowEnd"))
	if fromErr == nil && toErr == nil && to.After(from) {
		minutes := int(to.Sub(from).Minutes())
		if minutes > 0 && minutes <= 24*60 {
			return minutes
		}
	}
	return 60
}

func (s *Service) assessDatabaseDiagnosisRound(
	ctx context.Context,
	actor *model.AppUser,
	runID int64,
	actionIDs []int64,
) *DatabaseDiagnosisAssessment {
	assessment := &DatabaseDiagnosisAssessment{
		Status: model.RCARoundStatusPartialSuccess, Categories: []DatabaseDiagnosisCategory{},
		Confidence: "low", Conclusion: "Slow SQL deep diagnosis is incomplete; no root cause is confirmed.",
	}
	if actor == nil || runID <= 0 || len(actionIDs) == 0 {
		assessment.MissingEvidence = []string{"database diagnosis actions"}
		return assessment
	}
	actions, err := s.ListActions(ctx, actor, runID)
	if err != nil {
		assessment.MissingEvidence = []string{"database diagnosis action results"}
		return assessment
	}
	idSet := map[int64]struct{}{}
	for _, id := range actionIDs {
		idSet[id] = struct{}{}
	}
	slowEvidence := []int64{}
	supplementalEvidence := []int64{}
	explainCollected := false
	for _, action := range actions {
		if _, exists := idSet[action.ID]; !exists {
			continue
		}
		categories := databaseCategoriesForSkill(action.SkillName)
		if len(categories) == 0 {
			continue
		}
		var evidenceIDs []int64
		_ = json.Unmarshal(action.EvidenceIDs, &evidenceIDs)
		collected := (action.Status == model.RCAActionStatusSuccess || action.Status == model.RCAActionStatusPartialSuccess) && len(evidenceIDs) > 0
		for _, category := range categories {
			assessment.Categories = append(assessment.Categories, DatabaseDiagnosisCategory{
				Category: category, SourceSkill: action.SkillName, Collected: collected, EvidenceIDs: evidenceIDs,
			})
			if !collected || action.Status == model.RCAActionStatusPartialSuccess {
				assessment.MissingEvidence = append(assessment.MissingEvidence, category+" evidence")
			}
		}
		if !collected {
			continue
		}
		assessment.EvidenceIDs = append(assessment.EvidenceIDs, evidenceIDs...)
		if action.SkillName == "query_tidb_slow_queries" {
			slowEvidence = append(slowEvidence, evidenceIDs...)
			assessment.HighestImpactFingerprint = firstJSONValue(action.Output, "sql_fingerprint", "digest")
		} else {
			supplementalEvidence = append(supplementalEvidence, evidenceIDs...)
		}
		if action.SkillName == "explain_tidb_sql" {
			explainCollected = true
		}
	}
	assessment.EvidenceIDs = uniqueIDs(assessment.EvidenceIDs)
	assessment.MissingEvidence = uniqueStrings(assessment.MissingEvidence)
	assessment.RootCauseEligible = len(slowEvidence) > 0 && len(supplementalEvidence) > 0
	if assessment.RootCauseEligible {
		assessment.Status = model.RCARoundStatusSuccess
		assessment.Confidence = "medium"
		assessment.Conclusion = "Slow SQL is supported as a contributing factor by the slow-query fingerprint and supplemental database evidence."
	}
	if !explainCollected {
		assessment.MissingEvidence = uniqueStrings(append(assessment.MissingEvidence, "execution plan evidence; index failure is not asserted"))
	}
	if len(assessment.MissingEvidence) > 0 {
		assessment.Status = model.RCARoundStatusPartialSuccess
	}
	return assessment
}

func databaseCategoriesForSkill(skill string) []string {
	switch skill {
	case "query_tidb_slow_queries":
		return []string{"slow_sql_impact"}
	case "query_tidb_processlist":
		return []string{"connection_pressure", "resource_pressure"}
	case "query_tidb_lock_waits":
		return []string{"lock_contention"}
	case "query_tidb_hot_regions":
		return []string{"hot_region_pressure"}
	case "query_tidb_statistics_health":
		return []string{"statistics_anomaly"}
	case "explain_tidb_sql":
		return []string{"plan_regression"}
	default:
		return nil
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func firstJSONValue(raw json.RawMessage, keys ...string) string {
	if len(raw) == 0 || !json.Valid(raw) {
		return ""
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return findJSONValue(value, mapStrings(keys))
}

func findJSONValue(value any, keys map[string]struct{}) string {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if _, exists := keys[strings.ToLower(key)]; exists {
				if text := strings.TrimSpace(fmt.Sprint(item)); text != "" && text != "<nil>" {
					return text
				}
			}
		}
		for _, item := range typed {
			if found := findJSONValue(item, keys); found != "" {
				return found
			}
		}
	case []any:
		for _, item := range typed {
			if found := findJSONValue(item, keys); found != "" {
				return found
			}
		}
	}
	return ""
}

func mapStrings(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[strings.ToLower(value)] = struct{}{}
	}
	return result
}

// Compile-time assertion documents that only TiDB is implemented in this Task.
var _ DatabaseDiagnosisProvider = TiDBDatabaseDiagnosisProvider{}
