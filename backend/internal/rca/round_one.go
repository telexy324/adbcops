package rca

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	datasourcesvc "aiops-platform/backend/internal/datasource"
	logssvc "aiops-platform/backend/internal/logs"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/skillframework"
)

const (
	roundOneMaxWindow     = 24 * time.Hour
	roundOneLogSize       = 100
	roundOneMaxSeries     = 20
	roundOneMaxPoints     = 500
	roundOneStepSeconds   = 60
	roundOneMaxLogQueries = 6
)

type RoundOneCollectionInput struct {
	LogDataSourceID     int64    `json:"logDataSourceId,omitempty"`
	MetricsDataSourceID int64    `json:"metricsDataSourceId,omitempty"`
	From                string   `json:"from,omitempty"`
	To                  string   `json:"to,omitempty"`
	BaselineFrom        string   `json:"baselineFrom,omitempty"`
	BaselineTo          string   `json:"baselineTo,omitempty"`
	LogIndex            string   `json:"logIndex,omitempty"`
	TraceID             string   `json:"traceId,omitempty"`
	DependencyNames     []string `json:"dependencyNames,omitempty"`
	LogTemplates        []string `json:"logTemplates,omitempty"`
}

type RoundOneWindows struct {
	CurrentFrom  time.Time `json:"currentFrom"`
	CurrentTo    time.Time `json:"currentTo"`
	BaselineFrom time.Time `json:"baselineFrom"`
	BaselineTo   time.Time `json:"baselineTo"`
}

type RoundOneAttempt struct {
	ActionID     int64  `json:"actionId"`
	ActionKey    string `json:"actionKey"`
	SkillName    string `json:"skillName"`
	SkillRunID   int64  `json:"skillRunId,omitempty"`
	DataSourceID *int64 `json:"dataSourceId,omitempty"`
	Status       string `json:"status"`
	ErrorCode    string `json:"errorCode,omitempty"`
}

type RoundOneCollectionResult struct {
	Status   string                 `json:"status"`
	Round    *model.RCARound        `json:"round"`
	Actions  []model.RCAAction      `json:"actions"`
	Evidence []model.EvidenceRecord `json:"evidence"`
	Attempts []RoundOneAttempt      `json:"attempts"`
	Windows  RoundOneWindows        `json:"windows"`
	Missing  []string               `json:"missingEvidence"`
}

type roundOneScope struct {
	ServiceName    string
	Environment    string
	Namespace      string
	LogSourceID    int64
	MetricSourceID int64
	Windows        RoundOneWindows
	LogIndex       string
	TraceID        string
	Dependencies   []string
	LogTemplates   []string
}

type roundOnePlan struct {
	ActionKey     string
	SkillName     string
	Payload       json.RawMessage
	SensitiveRead bool
	SourceType    string
	EvidenceKind  string
	Summary       string
	DataSourceID  *int64
	CatalogKey    string
	Execute       bool
	MissingReason string
}

type roundOneExecution struct {
	plan    roundOnePlan
	action  *model.RCAAction
	result  *skillframework.ExecuteResult
	err     error
	partial bool
}

func (s *Service) CollectRoundOne(ctx context.Context, actor *model.AppUser, runID int64, input RoundOneCollectionInput) (*RoundOneCollectionResult, error) {
	if s.skills == nil {
		return nil, ErrInvalidInput
	}
	run, err := s.GetRun(ctx, actor, runID)
	if err != nil {
		return nil, err
	}
	if run.CurrentRound != 0 || run.Status != model.RCARunStatusPending {
		return nil, ErrInvalidTransition
	}
	if s.dataSources == nil {
		return nil, ErrInvalidInput
	}
	scope, err := s.resolveRoundOneScope(ctx, actor, run, input)
	if err != nil {
		return nil, err
	}
	plans, missing, err := buildRoundOnePlans(run.Query, scope)
	if err != nil {
		return nil, err
	}
	round, err := s.StartRound(ctx, actor, runID, StartRoundInput{})
	if err != nil {
		return nil, err
	}
	executions := make([]roundOneExecution, 0, len(plans))
	for _, plan := range plans {
		action, createErr := s.CreateAction(ctx, actor, runID, CreateActionInput{
			RoundID: round.ID, ActionKey: plan.ActionKey, SkillName: plan.SkillName,
			Input: plan.Payload, SensitiveRead: plan.SensitiveRead,
		})
		if createErr != nil {
			return nil, createErr
		}
		action, startErr := s.StartAction(ctx, actor, runID, action.ID)
		if startErr != nil {
			return nil, startErr
		}
		executions = append(executions, roundOneExecution{plan: plan, action: action})
	}

	var wait sync.WaitGroup
	for index := range executions {
		if !executions[index].plan.Execute {
			executions[index].err = errors.New(executions[index].plan.MissingReason)
			continue
		}
		wait.Add(1)
		go func(execution *roundOneExecution) {
			defer wait.Done()
			execution.result, execution.err = s.skills.Execute(ctx, skillframework.ExecuteInput{
				Actor: actor, Name: execution.plan.SkillName, Payload: execution.plan.Payload,
				WorkflowRunID: run.WorkflowRunID,
			})
			if execution.err == nil && execution.result == nil {
				execution.err = errors.New("invalid skill response")
			}
			if execution.err == nil {
				execution.result.Output = normalizeRoundOneOutput(execution.plan, execution.result.Output)
				execution.partial = outputIsPartial(execution.result.Output)
			}
		}(&executions[index])
	}
	wait.Wait()

	evidenceRecords := []model.EvidenceRecord{}
	completedActions := make([]model.RCAAction, 0, len(executions))
	attempts := make([]RoundOneAttempt, 0, len(executions))
	evidenceIDs := []int64{}
	degradedCount := 0
	entity, _ := json.Marshal(map[string]string{
		"serviceName": scope.ServiceName, "environment": scope.Environment, "namespace": scope.Namespace,
	})
	for index := range executions {
		execution := &executions[index]
		attempt := RoundOneAttempt{
			ActionID: execution.action.ID, ActionKey: execution.plan.ActionKey,
			SkillName: execution.plan.SkillName, DataSourceID: execution.plan.DataSourceID,
		}
		if execution.result != nil {
			attempt.SkillRunID = execution.result.RunID
		}
		status := model.RCAActionStatusSuccess
		errorCode := ""
		var actionOutput json.RawMessage
		if execution.err != nil {
			status, errorCode = model.RCAActionStatusFailed, classifyRoundOneError(execution.err)
			degradedCount++
			actionOutput = mustJSON(map[string]any{"skillRunId": attempt.SkillRunID, "status": "failed"})
		} else {
			actionOutput = mustJSON(map[string]any{"skillRunId": attempt.SkillRunID, "result": json.RawMessage(execution.result.Output)})
			if execution.partial {
				status, errorCode = model.RCAActionStatusPartialSuccess, "upstream_unavailable"
				degradedCount++
			}
		}

		actionEvidenceIDs := []int64{}
		if execution.err == nil && (!execution.partial || usefulPartialOutput(execution.result.Output)) {
			sourceRef := mustJSON(map[string]any{
				"skillRunId": attempt.SkillRunID, "actionId": execution.action.ID,
				"dataSourceId": execution.plan.DataSourceID, "catalogVersion": roundOnePromQLCatalogVersion,
				"queryKey": execution.plan.CatalogKey,
			})
			confidence := 0.8
			observedAt := scope.Windows.CurrentTo
			record, addErr := s.AddEvidence(ctx, actor, runID, CreateEvidenceInput{
				RoundID: round.ID, ActionID: &execution.action.ID,
				EvidenceKey: fmt.Sprintf("rca_%d_round1_%s", runID, execution.plan.ActionKey),
				SourceType:  execution.plan.SourceType, SourceRef: sourceRef, ObservedAt: &observedAt,
				Summary: summarizeRoundOneOutput(execution.plan, execution.result.Output),
				Content: execution.result.Output, Confidence: &confidence,
				Sensitivity: model.EvidenceSensitivityInternal, EvidenceKind: execution.plan.EvidenceKind,
				Entity: entity, WindowStart: &scope.Windows.CurrentFrom, WindowEnd: &scope.Windows.CurrentTo,
				SourceSkill: execution.plan.SkillName, DataSourceID: execution.plan.DataSourceID,
			})
			if addErr != nil {
				status, errorCode = model.RCAActionStatusFailed, "internal_error"
				degradedCount++
			} else {
				actionEvidenceIDs = append(actionEvidenceIDs, record.ID)
				evidenceIDs = append(evidenceIDs, record.ID)
				evidenceRecords = append(evidenceRecords, *record)
			}
		}
		completed, completeErr := s.CompleteAction(ctx, actor, runID, execution.action.ID, CompleteActionInput{
			Status: status, Output: actionOutput, EvidenceIDs: actionEvidenceIDs, ErrorCode: errorCode,
		})
		if completeErr != nil {
			return nil, completeErr
		}
		completedActions = append(completedActions, *completed)
		attempt.Status, attempt.ErrorCode = status, errorCode
		attempts = append(attempts, attempt)
	}

	roundStatus := model.RCARoundStatusSuccess
	roundErrorCode := ""
	if len(evidenceIDs) == 0 {
		roundStatus, roundErrorCode = model.RCARoundStatusFailed, "upstream_unavailable"
	} else if degradedCount > 0 {
		roundStatus, roundErrorCode = model.RCARoundStatusPartialSuccess, "upstream_unavailable"
	}
	round, err = s.CompleteRound(ctx, actor, runID, round.ID, CompleteRoundInput{
		Status: roundStatus, NewEvidenceIDs: evidenceIDs, ErrorCode: roundErrorCode,
	})
	if err != nil {
		return nil, err
	}
	if roundStatus == model.RCARoundStatusFailed {
		if _, err := s.CompleteRun(ctx, actor, runID, CompleteRunInput{
			Status: model.RCARunStatusFailed, ErrorCode: roundErrorCode,
		}); err != nil {
			return nil, err
		}
	}
	result := &RoundOneCollectionResult{
		Status: roundStatus, Round: round, Actions: completedActions, Evidence: evidenceRecords,
		Attempts: attempts, Windows: scope.Windows, Missing: missing,
	}
	sortRoundOneResult(result)
	return result, nil
}

func (s *Service) resolveRoundOneScope(ctx context.Context, actor *model.AppUser, run *model.RCARun, input RoundOneCollectionInput) (roundOneScope, error) {
	var raw map[string]any
	if json.Unmarshal(run.Scope, &raw) != nil {
		return roundOneScope{}, ErrInvalidInput
	}
	scope := roundOneScope{
		ServiceName:    firstScopeString(raw, "serviceName", "component", "serviceQuery"),
		Environment:    firstScopeString(raw, "environment"),
		Namespace:      firstScopeString(raw, "namespace"),
		LogSourceID:    firstPositiveID(input.LogDataSourceID, scopeID(raw, "logDataSourceId")),
		MetricSourceID: firstPositiveID(input.MetricsDataSourceID, scopeID(raw, "metricsDataSourceId")),
		LogIndex:       firstNonEmptyRCA(input.LogIndex, firstScopeString(raw, "logIndex", "index")),
		TraceID:        firstNonEmptyRCA(input.TraceID, firstScopeString(raw, "traceId")),
		Dependencies:   normalizeOperationalValues(append(scopeStrings(raw, "dependencyNames"), input.DependencyNames...), 8),
		LogTemplates:   normalizeLogTemplates(append(scopeStrings(raw, "logTemplates"), input.LogTemplates...), 4),
	}
	if scope.TraceID != "" {
		if _, err := validatePromLabelValue(scope.TraceID); err != nil {
			return roundOneScope{}, ErrInvalidInput
		}
	}
	genericID := scopeID(raw, "dataSourceId")
	views, err := s.dataSources.List(ctx, actor)
	if err != nil {
		return roundOneScope{}, err
	}
	views = accessibleRoundOneViews(views)
	if genericID > 0 {
		for _, view := range views {
			if view.ID != genericID {
				continue
			}
			if view.SourceType == model.DataSourceTypeElasticsearch || view.SourceType == model.DataSourceTypeOpenSearch {
				scope.LogSourceID = firstPositiveID(scope.LogSourceID, genericID)
			}
			if view.SourceType == model.DataSourceTypePrometheus {
				scope.MetricSourceID = firstPositiveID(scope.MetricSourceID, genericID)
			}
		}
	}
	if scope.LogSourceID == 0 {
		scope.LogSourceID = selectRoundOneDataSource(views, scope, model.DataSourceTypeElasticsearch, model.DataSourceTypeOpenSearch)
	}
	if scope.MetricSourceID == 0 {
		scope.MetricSourceID = selectRoundOneDataSource(views, scope, model.DataSourceTypePrometheus)
	}
	if !roundOneDataSourceAllowed(views, scope.LogSourceID, model.DataSourceTypeElasticsearch, model.DataSourceTypeOpenSearch) && scope.LogSourceID > 0 {
		return roundOneScope{}, ErrForbidden
	}
	if !roundOneDataSourceAllowed(views, scope.MetricSourceID, model.DataSourceTypePrometheus) && scope.MetricSourceID > 0 {
		return roundOneScope{}, ErrForbidden
	}
	from := firstNonEmptyRCA(input.From, firstScopeString(raw, "from"))
	to := firstNonEmptyRCA(input.To, firstScopeString(raw, "to"))
	baselineFrom := firstNonEmptyRCA(input.BaselineFrom, firstScopeString(raw, "baselineFrom"))
	baselineTo := firstNonEmptyRCA(input.BaselineTo, firstScopeString(raw, "baselineTo"))
	scope.Windows, err = normalizeRoundOneWindows(from, to, baselineFrom, baselineTo, s.now())
	if err != nil {
		return roundOneScope{}, err
	}
	return scope, nil
}

func buildRoundOnePlans(question string, scope roundOneScope) ([]roundOnePlan, []string, error) {
	plans := []roundOnePlan{}
	missing := []string{}
	if scope.LogSourceID == 0 {
		missing = append(missing, "Elasticsearch/OpenSearch data source binding is missing")
		plans = append(plans, missingRoundOnePlan("round1:logs:missing", "query_logs", "log", true, missing[len(missing)-1]))
	} else {
		for _, query := range controlledLogQueries(scope) {
			payload := mustJSON(map[string]any{
				"dataSourceId": scope.LogSourceID, "index": scope.LogIndex,
				"from": scope.Windows.CurrentFrom.Format(time.RFC3339), "to": scope.Windows.CurrentTo.Format(time.RFC3339),
				"queryString": query.Query, "size": roundOneLogSize,
			})
			dataSourceID := scope.LogSourceID
			plans = append(plans, roundOnePlan{
				ActionKey: "round1:logs:" + query.Key, SkillName: "query_logs", Payload: payload,
				SensitiveRead: true, SourceType: "log", EvidenceKind: model.EvidenceKindFact,
				Summary: query.Description, DataSourceID: &dataSourceID, CatalogKey: query.Key, Execute: true,
			})
		}
	}

	if scope.MetricSourceID == 0 {
		missing = append(missing, "Prometheus data source binding is missing")
		plans = append(plans, missingRoundOnePlan("round1:metrics:missing", "compare_metric_baseline", "metric", true, missing[len(missing)-1]))
	} else if strings.TrimSpace(scope.ServiceName) == "" {
		missing = append(missing, "validated service entity for Prometheus labels is missing")
		plans = append(plans, missingRoundOnePlan("round1:metrics:missing_entity", "compare_metric_baseline", "metric", true, missing[len(missing)-1]))
	} else {
		queries, err := roundOnePromQL(promLabelScope{Service: scope.ServiceName, Environment: scope.Environment, Namespace: scope.Namespace})
		if err != nil {
			return nil, nil, err
		}
		for _, query := range queries {
			payload := mustJSON(map[string]any{
				"dataSourceId": scope.MetricSourceID, "query": query.Query,
				"currentStart": scope.Windows.CurrentFrom.Format(time.RFC3339), "currentEnd": scope.Windows.CurrentTo.Format(time.RFC3339),
				"baselineStart": scope.Windows.BaselineFrom.Format(time.RFC3339), "baselineEnd": scope.Windows.BaselineTo.Format(time.RFC3339),
				"stepSeconds": roundOneStepSeconds, "maxSeries": roundOneMaxSeries, "maxPoints": roundOneMaxPoints,
			})
			dataSourceID := scope.MetricSourceID
			plans = append(plans, roundOnePlan{
				ActionKey: "round1:metrics:" + query.Key, SkillName: "compare_metric_baseline", Payload: payload,
				SensitiveRead: true, SourceType: "metric", EvidenceKind: model.EvidenceKindFact,
				Summary: query.Description, DataSourceID: &dataSourceID, CatalogKey: query.Key, Execute: true,
			})
		}
	}
	entities := normalizeOperationalValues([]string{scope.ServiceName, scope.Environment, scope.Namespace}, 8)
	plans = append(plans, roundOnePlan{
		ActionKey: "round1:knowledge", SkillName: "hybrid_search_knowledge",
		Payload: mustJSON(map[string]any{
			"originalQuestion": question, "confirmedEntities": entities,
			"logTemplates": scope.LogTemplates, "metricAnomalySummaries": []string{"performance degradation"}, "limit": 5,
		}),
		SourceType: "knowledge", EvidenceKind: model.EvidenceKindKnowledge,
		Summary: "Related historical incidents and runbooks", CatalogKey: "hybrid", Execute: true,
	})
	return plans, missing, nil
}

type controlledLogQuery struct {
	Key         string
	Description string
	Query       string
}

func controlledLogQueries(scope roundOneScope) []controlledLogQuery {
	service := escapeSimpleQueryValue(scope.ServiceName)
	prefix := ""
	if service != "" {
		prefix = `"` + service + `" AND `
	}
	result := []controlledLogQuery{
		{Key: "errors", Description: "Error and exception logs", Query: prefix + `(error OR exception OR failed OR fatal)`},
		{Key: "timeout_slow", Description: "Timeout and slow-call logs", Query: prefix + `(timeout OR "timed out" OR slow OR latency OR duration)`},
	}
	if trace := escapeSimpleQueryValue(scope.TraceID); trace != "" {
		result = append(result, controlledLogQuery{Key: "trace", Description: "Logs for the confirmed trace ID", Query: `"` + trace + `"`})
	}
	for index, dependency := range scope.Dependencies {
		result = append(result, controlledLogQuery{
			Key: "dependency_" + strconv.Itoa(index+1), Description: "Logs mentioning dependency " + dependency,
			Query: prefix + `"` + escapeSimpleQueryValue(dependency) + `"`,
		})
	}
	for index, template := range scope.LogTemplates {
		result = append(result, controlledLogQuery{
			Key: "template_" + strconv.Itoa(index+1), Description: "Logs matching a normalized cluster template",
			Query: prefix + `"` + escapeSimpleQueryValue(template) + `"`,
		})
	}
	if len(result) > roundOneMaxLogQueries {
		result = result[:roundOneMaxLogQueries]
	}
	return result
}

func normalizeRoundOneWindows(fromText, toText, baselineFromText, baselineToText string, now time.Time) (RoundOneWindows, error) {
	to := now.UTC()
	var err error
	if strings.TrimSpace(toText) != "" {
		to, err = time.Parse(time.RFC3339, strings.TrimSpace(toText))
		if err != nil {
			return RoundOneWindows{}, ErrInvalidInput
		}
	}
	from := to.Add(-30 * time.Minute)
	if strings.TrimSpace(fromText) != "" {
		from, err = time.Parse(time.RFC3339, strings.TrimSpace(fromText))
		if err != nil {
			return RoundOneWindows{}, ErrInvalidInput
		}
	}
	if !from.Before(to) || to.Sub(from) > roundOneMaxWindow {
		return RoundOneWindows{}, ErrInvalidInput
	}
	duration := to.Sub(from)
	baselineTo := from
	baselineFrom := baselineTo.Add(-duration)
	if strings.TrimSpace(baselineToText) != "" {
		baselineTo, err = time.Parse(time.RFC3339, strings.TrimSpace(baselineToText))
		if err != nil {
			return RoundOneWindows{}, ErrInvalidInput
		}
	}
	if strings.TrimSpace(baselineFromText) != "" {
		baselineFrom, err = time.Parse(time.RFC3339, strings.TrimSpace(baselineFromText))
		if err != nil {
			return RoundOneWindows{}, ErrInvalidInput
		}
	} else {
		baselineFrom = baselineTo.Add(-duration)
	}
	if !baselineFrom.Before(baselineTo) || baselineTo.After(from) || baselineTo.Sub(baselineFrom) > roundOneMaxWindow {
		return RoundOneWindows{}, ErrInvalidInput
	}
	return RoundOneWindows{CurrentFrom: from.UTC(), CurrentTo: to.UTC(), BaselineFrom: baselineFrom.UTC(), BaselineTo: baselineTo.UTC()}, nil
}

func accessibleRoundOneViews(views []datasourcesvc.DataSourceView) []datasourcesvc.DataSourceView {
	result := make([]datasourcesvc.DataSourceView, 0, len(views))
	for _, view := range views {
		if view.Enabled && view.ReadOnly {
			result = append(result, view)
		}
	}
	return result
}

func selectRoundOneDataSource(views []datasourcesvc.DataSourceView, scope roundOneScope, sourceTypes ...string) int64 {
	matches := []datasourcesvc.DataSourceView{}
	for _, view := range views {
		if !containsValue(sourceTypes, view.SourceType) {
			continue
		}
		if scope.Environment != "" && normalizeRCAEnvironment(pointerValue(view.Environment)) != normalizeRCAEnvironment(scope.Environment) {
			continue
		}
		if scope.ServiceName != "" && !dataSourceMatchesService(view, scope.ServiceName) {
			continue
		}
		matches = append(matches, view)
	}
	if len(matches) == 1 {
		return matches[0].ID
	}
	if len(matches) == 0 && scope.Environment != "" {
		for _, view := range views {
			if containsValue(sourceTypes, view.SourceType) &&
				normalizeRCAEnvironment(pointerValue(view.Environment)) == normalizeRCAEnvironment(scope.Environment) {
				matches = append(matches, view)
			}
		}
		if len(matches) == 1 {
			return matches[0].ID
		}
	}
	if len(matches) == 0 && scope.Environment == "" {
		for _, view := range views {
			if containsValue(sourceTypes, view.SourceType) {
				matches = append(matches, view)
			}
		}
		if len(matches) == 1 {
			return matches[0].ID
		}
	}
	return 0
}

func roundOneDataSourceAllowed(views []datasourcesvc.DataSourceView, id int64, sourceTypes ...string) bool {
	if id == 0 {
		return false
	}
	for _, view := range views {
		if view.ID == id && containsValue(sourceTypes, view.SourceType) {
			return true
		}
	}
	return false
}

func dataSourceMatchesService(view datasourcesvc.DataSourceView, service string) bool {
	service = normalizeOperationalLabel(service)
	for _, candidate := range []string{view.Name, pointerValue(view.SystemName), pointerValue(view.ComponentName)} {
		candidate = normalizeOperationalLabel(candidate)
		if candidate != "" && (candidate == service || strings.Contains(candidate, service) || strings.Contains(service, candidate)) {
			return true
		}
	}
	return false
}

func outputIsPartial(raw json.RawMessage) bool {
	var value struct {
		Partial bool `json:"partial"`
	}
	return json.Unmarshal(raw, &value) == nil && value.Partial
}

func usefulPartialOutput(raw json.RawMessage) bool {
	var value map[string]json.RawMessage
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	for _, key := range []string{"current", "metrics", "items", "citations", "context"} {
		if item, exists := value[key]; exists && len(item) > 0 && string(item) != "null" && string(item) != "[]" {
			return true
		}
	}
	return false
}

func summarizeRoundOneOutput(plan roundOnePlan, raw json.RawMessage) string {
	var value map[string]json.RawMessage
	_ = json.Unmarshal(raw, &value)
	switch plan.SourceType {
	case "log":
		var total int64
		_ = json.Unmarshal(value["total"], &total)
		return fmt.Sprintf("%s returned %d matching log events", plan.Summary, total)
	case "knowledge":
		var count int
		_ = json.Unmarshal(value["recallCount"], &count)
		return fmt.Sprintf("%s returned %d citation-ready context blocks", plan.Summary, count)
	default:
		return plan.Summary + " current and baseline windows were queried"
	}
}

func normalizeRoundOneOutput(plan roundOnePlan, raw json.RawMessage) json.RawMessage {
	if plan.SourceType != "log" {
		return raw
	}
	var result struct {
		Items    []model.LogItem `json:"items"`
		Total    int64           `json:"total"`
		TimedOut bool            `json:"timedOut"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return raw
	}
	processed := logssvc.Preprocess(logssvc.PreprocessInput{Items: result.Items})
	return mustJSON(map[string]any{
		"items": processed.Items, "total": result.Total, "timedOut": result.TimedOut,
		"templateClusters": processed.Clusters, "timeStats": processed.TimeStats,
		"errorCount": processed.ErrorCount, "redactionCount": processed.RedactionCount,
	})
}

func missingRoundOnePlan(key, skill, source string, sensitive bool, reason string) roundOnePlan {
	return roundOnePlan{
		ActionKey: key, SkillName: skill, Payload: mustJSON(map[string]string{"missing": reason}),
		SensitiveRead: sensitive, SourceType: source, EvidenceKind: model.EvidenceKindFact,
		Summary: reason, Execute: false, MissingReason: reason,
	}
}

func classifyRoundOneError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "timeout") {
		return "upstream_timeout"
	}
	if errors.Is(err, skillframework.ErrPermissionDenied) || strings.Contains(strings.ToLower(err.Error()), "forbidden") {
		return "permission_denied"
	}
	if strings.Contains(strings.ToLower(err.Error()), "limit") {
		return "resource_limit"
	}
	if strings.Contains(strings.ToLower(err.Error()), "invalid") {
		return "invalid_response"
	}
	return "upstream_unavailable"
}

func normalizeOperationalValues(values []string, limit int) []string {
	result := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		if len(result) >= limit {
			break
		}
		value = strings.TrimSpace(value)
		if _, err := validatePromLabelValue(value); err != nil {
			continue
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func normalizeLogTemplates(values []string, limit int) []string {
	items := make([]model.LogItem, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len([]rune(value)) > 256 {
			continue
		}
		safe := true
		for _, character := range value {
			if unicode.IsControl(character) {
				safe = false
				break
			}
		}
		if safe {
			items = append(items, model.LogItem{Message: value})
		}
	}
	processed := logssvc.Preprocess(logssvc.PreprocessInput{Items: items, StackMaxLines: 1})
	result := make([]string, 0, minIntRCA(limit, len(processed.Clusters)))
	for _, cluster := range processed.Clusters {
		if len(result) >= limit {
			break
		}
		if template := strings.TrimSpace(cluster.Template); template != "" {
			result = append(result, template)
		}
	}
	return result
}

func escapeSimpleQueryValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ").Replace(value)
	return value
}

func firstScopeString(scope map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := scope[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func scopeStrings(scope map[string]any, key string) []string {
	raw, exists := scope[key]
	if !exists {
		return nil
	}
	switch values := raw.(type) {
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok {
				result = append(result, text)
			}
		}
		return result
	case []string:
		return append([]string(nil), values...)
	default:
		return nil
	}
}

func scopeID(scope map[string]any, key string) int64 {
	switch value := scope[key].(type) {
	case float64:
		return int64(value)
	case int64:
		return value
	case int:
		return int64(value)
	case json.Number:
		parsed, _ := value.Int64()
		return parsed
	default:
		return 0
	}
}

func firstPositiveID(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstNonEmptyRCA(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeOperationalLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimSuffix(value, "服务")
	return strings.Map(func(character rune) rune {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			return character
		}
		return -1
	}, value)
}

func normalizeRCAEnvironment(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "production", "prd", "生产", "生产环境":
		return "prod"
	case "stage", "预发", "预发环境":
		return "staging"
	case "qa", "测试", "测试环境":
		return "test"
	case "development", "开发", "开发环境":
		return "dev"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func containsValue(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func mustJSON(value any) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}

func minIntRCA(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func sortRoundOneResult(result *RoundOneCollectionResult) {
	sort.Slice(result.Actions, func(i, j int) bool { return result.Actions[i].ID < result.Actions[j].ID })
	sort.Slice(result.Evidence, func(i, j int) bool { return result.Evidence[i].ID < result.Evidence[j].ID })
	sort.Slice(result.Attempts, func(i, j int) bool { return result.Attempts[i].ActionID < result.Attempts[j].ActionID })
}
