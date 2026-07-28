package rca

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	datasourcesvc "aiops-platform/backend/internal/datasource"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/skillframework"
)

func TestTiDBDatabaseDiagnosisProviderBuildsBoundedDeepPlan(t *testing.T) {
	provider := NewTiDBDatabaseDiagnosisProvider()
	plan, err := provider.BuildPlan(DatabaseDiagnosisRequest{
		DataSourceID: 7,
		ServiceName:  "order-service",
		Environment:  "prod",
		Minutes:      30,
		Limit:        25,
		ReadonlySQL:  "SELECT * FROM orders WHERE customer_id=42 AND email='alice@example.com'",
		EvidenceIDs:  []int64{12, 12, 13},
		CorrelationDimensions: []string{
			"service", "time_window", "trace", "call_volume", "baseline",
		},
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if plan.Provider != "tidb" || plan.SourceType != model.DataSourceTypeTiDB || len(plan.Actions) != 6 {
		t.Fatalf("unexpected TiDB deep plan: %+v", plan)
	}
	expected := []string{
		"query_tidb_slow_queries",
		"query_tidb_processlist",
		"query_tidb_lock_waits",
		"query_tidb_hot_regions",
		"query_tidb_statistics_health",
		"explain_tidb_sql",
	}
	for index, skill := range expected {
		if plan.Actions[index].SkillName != skill {
			t.Fatalf("action %d = %s, want %s", index, plan.Actions[index].SkillName, skill)
		}
		if !strings.Contains(string(plan.Actions[index].Input), `"dataSourceId":7`) {
			t.Fatalf("action did not retain resolved data source: %+v", plan.Actions[index])
		}
	}
	if strings.Contains(plan.SanitizedSQL, "42") || strings.Contains(plan.SanitizedSQL, "alice@example.com") || plan.SQLFingerprint == "" {
		t.Fatalf("SQL metadata was not fingerprinted and sanitized: %+v", plan)
	}
	if plan.WindowMinutes != 30 || len(plan.MissingEvidence) != 0 {
		t.Fatalf("correlation window and dimensions were not retained: %+v", plan)
	}
	if strings.Contains(string(plan.Actions[5].Input), `"analyze":true`) {
		t.Fatalf("production deep plan must never schedule EXPLAIN ANALYZE: %+v", plan.Actions[5])
	}
}

func TestTiDBDatabaseDiagnosisProviderDoesNotExplainUnsafeOrMissingSQL(t *testing.T) {
	provider := NewTiDBDatabaseDiagnosisProvider()
	for _, sqlText := range []string{"", "UPDATE orders SET status='paid'", "CALL rebuild_stats()"} {
		plan, err := provider.BuildPlan(DatabaseDiagnosisRequest{DataSourceID: 7, ReadonlySQL: sqlText})
		if err != nil {
			t.Fatalf("BuildPlan(%q) error = %v", sqlText, err)
		}
		if len(plan.Actions) != 5 || len(plan.MissingEvidence) == 0 {
			t.Fatalf("unsafe or missing SQL should omit EXPLAIN and record missing evidence: %+v", plan)
		}
		for _, action := range plan.Actions {
			if action.SkillName == "explain_tidb_sql" {
				t.Fatalf("unsafe SQL scheduled EXPLAIN: %+v", action)
			}
		}
	}
}

func TestDatabaseDiagnosisDoesNotPretendToSupportOtherEngines(t *testing.T) {
	service := NewService(newMemoryRCARepository(), nil, fakeRCADataSources{})
	if service.databaseProvider(model.DataSourceTypeTiDB) == nil {
		t.Fatal("TiDB provider was not registered")
	}
	for _, sourceType := range []string{"mysql", "postgresql"} {
		if service.databaseProvider(sourceType) != nil {
			t.Fatalf("%s must not be presented as implemented", sourceType)
		}
	}
}

func TestBuildDatabaseDeepDiagnosisPlanUsesConfirmedEvidenceAndAccessibleScope(t *testing.T) {
	repo := newMemoryRCARepository()
	views := []datasourcesvc.DataSourceView{{
		ID: 7, Name: "tidb-prod", SourceType: model.DataSourceTypeTiDB,
		Enabled: true, ReadOnly: true,
	}}
	service := NewService(repo, &memoryEvidenceCreator{repository: repo}, fakeRCADataSources{views: views}).
		WithPlanner(databaseDiagnosisTestCatalog(), nil)
	actor := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	run := &model.RCARun{
		ID: 1, UserID: actor.ID, Query: "订单服务慢 SQL",
		Scope: json.RawMessage(`{"serviceName":"order-service","environment":"prod","tidbDataSourceId":7,"readonlySQL":"select * from orders where id=42","from":"2026-07-28T05:30:00Z","to":"2026-07-28T06:00:00Z"}`),
	}
	sourceSkill := "query_tidb_slow_queries"
	dataSourceID := int64(7)
	detail := &Detail{
		Run: run,
		Evidence: []model.EvidenceRecord{{
			ID: 31, SourceType: model.DataSourceTypeTiDB, SourceSkill: &sourceSkill,
			DataSourceID: &dataSourceID, Summary: "slow SQL fingerprint has highest accumulated impact",
		}, {
			ID: 32, SourceType: model.DataSourceTypePrometheus,
			Summary: "trace call volume and baseline show a correlated regression",
		}},
	}
	planner := &PlannerResult{Hypotheses: []PlannerHypothesis{{
		ID: "database-latency", Summary: "数据库存在 slow SQL", Confidence: .82,
		SupportingEvidenceIDs: []int64{31, 32},
	}}}

	plan, actions := service.buildDatabaseDeepDiagnosisPlan(context.Background(), actor, detail, planner, 6)
	if plan == nil || len(actions) != 6 {
		t.Fatalf("confirmed slow SQL did not produce the bounded third-round plan: plan=%+v actions=%+v", plan, actions)
	}
	if plan.Actions[0].EvidenceIDs[0] != 31 || !strings.Contains(string(plan.Actions[0].Input), `"minutes":30`) {
		t.Fatalf("plan was not derived from second-round evidence and time scope: %+v", plan)
	}
	if len(plan.SupportingIDs) != 2 || len(plan.MissingEvidence) != 0 ||
		!containsString(plan.CorrelationDimensions, "trace") ||
		!containsString(plan.CorrelationDimensions, "call_volume") ||
		!containsString(plan.CorrelationDimensions, "baseline") {
		t.Fatalf("plan did not preserve cross-source correlation evidence: %+v", plan)
	}
}

func TestDatabaseDiagnosisAssessmentRequiresSlowSQLAndSupplementalEvidence(t *testing.T) {
	service, actor, runID, roundID := plannerTestRun(t, json.RawMessage(`{"serviceName":"order-service"}`), nil)
	slowActionID, slowEvidenceID := completeDatabaseDiagnosisAction(
		t, service, actor, runID, roundID,
		"database-deep-slow-sql", "query_tidb_slow_queries",
		json.RawMessage(`{"facts":[{"evidence":{"rows":[{"digest":"digest-high-impact","total_query_time":"84.5"}]}}]}`),
	)
	lockActionID, lockEvidenceID := completeDatabaseDiagnosisAction(
		t, service, actor, runID, roundID,
		"database-deep-lock-waits", "query_tidb_lock_waits",
		json.RawMessage(`{"facts":[{"evidence":{"rows":[{"waiting_trx_id":"1"}]}}]}`),
	)

	assessment := service.assessDatabaseDiagnosisRound(
		context.Background(), actor, runID, []int64{slowActionID, lockActionID},
	)
	if !assessment.RootCauseEligible || assessment.Confidence != "medium" {
		t.Fatalf("slow SQL plus supplemental evidence should be eligible as a contributing factor: %+v", assessment)
	}
	if assessment.HighestImpactFingerprint != "digest-high-impact" {
		t.Fatalf("highest-impact fingerprint was not retained: %+v", assessment)
	}
	if !containsInt64(assessment.EvidenceIDs, slowEvidenceID) || !containsInt64(assessment.EvidenceIDs, lockEvidenceID) {
		t.Fatalf("assessment did not cite both evidence records: %+v", assessment)
	}
	if assessment.Status != model.RCARoundStatusPartialSuccess ||
		!strings.Contains(strings.Join(assessment.MissingEvidence, " "), "index failure is not asserted") {
		t.Fatalf("missing execution plan must remain explicit and prevent full success: %+v", assessment)
	}
	if got := databaseCategoriesForSkill("query_tidb_processlist"); len(got) != 2 ||
		got[0] != "connection_pressure" || got[1] != "resource_pressure" {
		t.Fatalf("connection and resource pressure must remain distinguishable: %v", got)
	}
}

func completeDatabaseDiagnosisAction(
	t *testing.T,
	service *Service,
	actor *model.AppUser,
	runID int64,
	roundID int64,
	actionKey string,
	skillName string,
	output json.RawMessage,
) (int64, int64) {
	t.Helper()
	action, err := service.CreateAction(context.Background(), actor, runID, CreateActionInput{
		RoundID: roundID, ActionKey: actionKey, SkillName: skillName,
		Input: json.RawMessage(`{"dataSourceId":7}`), SensitiveRead: true,
	})
	if err != nil {
		t.Fatalf("create action: %v", err)
	}
	action, err = service.StartAction(context.Background(), actor, runID, action.ID)
	if err != nil {
		t.Fatalf("start action: %v", err)
	}
	dataSourceID := int64(7)
	evidence, err := service.AddEvidence(context.Background(), actor, runID, CreateEvidenceInput{
		RoundID: roundID, ActionID: &action.ID, SourceType: model.DataSourceTypeTiDB,
		Summary: skillName + " evidence", Content: output, EvidenceKind: model.EvidenceKindFact,
		SourceSkill: skillName, DataSourceID: &dataSourceID,
	})
	if err != nil {
		t.Fatalf("add action evidence: %v", err)
	}
	if _, err = service.CompleteAction(context.Background(), actor, runID, action.ID, CompleteActionInput{
		Status: model.RCAActionStatusSuccess, Output: output, EvidenceIDs: []int64{evidence.ID},
	}); err != nil {
		t.Fatalf("complete action: %v", err)
	}
	return action.ID, evidence.ID
}

func databaseDiagnosisTestCatalog() fakePlannerCatalog {
	catalog := plannerTestCatalog()
	schema := json.RawMessage(`{"type":"object","required":["dataSourceId"],"properties":{"dataSourceId":{"type":"integer"},"minutes":{"type":"integer"},"limit":{"type":"integer"},"sql":{"type":"string"},"analyze":{"type":"boolean"}}}`)
	for _, name := range []string{
		"query_tidb_lock_waits",
		"query_tidb_hot_regions",
		"query_tidb_statistics_health",
		"explain_tidb_sql",
	} {
		catalog.definitions[name] = skillDefinition(name, schema)
	}
	return catalog
}

func skillDefinition(name string, schema json.RawMessage) skillframework.SkillDefinition {
	return skillframework.SkillDefinition{
		Name: name, Enabled: true, ReadOnly: true,
		RiskLevel: model.SkillRiskSensitiveRead, InputSchema: schema,
	}
}
