package rca

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	datasourcesvc "aiops-platform/backend/internal/datasource"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/skillframework"
)

func TestPlannerDatabaseLatencyPlansTopologyAndSlowSQL(t *testing.T) {
	service, actor, runID, roundID := plannerTestRun(t, json.RawMessage(`{
		"environment":"prod","serviceName":"order-service","topologyNodeKey":"svc:order","tidbDataSourceId":7
	}`), []datasourcesvc.DataSourceView{plannerDataSource(7, model.DataSourceTypeTiDB)})
	record := addPlannerEvidence(t, service, actor, runID, roundID, "log", "database call took 2400ms", json.RawMessage(`{"durationMs":2400}`))

	result, err := service.PlanNext(context.Background(), actor, runID, PlanRequest{UseLLM: boolPointer(false)})
	if err != nil {
		t.Fatalf("plan next: %v", err)
	}
	if !hasPlannerSkill(result.NextActions, "find_dependencies") || !hasPlannerSkill(result.NextActions, "query_tidb_slow_queries") {
		t.Fatalf("database rule did not plan topology and slow SQL: %+v", result.NextActions)
	}
	hypothesis := plannerHypothesisByID(result.Hypotheses, "database-latency")
	if hypothesis == nil || !containsInt64(hypothesis.SupportingEvidenceIDs, record.ID) {
		t.Fatalf("database evidence was not cited: %+v", result.Hypotheses)
	}
	if len(service.repository.(*memoryRCARepository).candidates) == 0 {
		t.Fatal("planner hypotheses were not persisted as RCA candidates")
	}
}

func TestPlannerV1RegressionFixtureIsVersionedAndValid(t *testing.T) {
	raw, err := os.ReadFile("testdata/planner_v1/database_slow.json")
	if err != nil {
		t.Fatalf("read regression fixture: %v", err)
	}
	var fixture struct {
		FixtureVersion string   `json:"fixtureVersion"`
		ExpectedSkills []string `json:"expectedSkills"`
	}
	if json.Unmarshal(raw, &fixture) != nil || fixture.FixtureVersion != "rca-planner-fixture-v1" || len(fixture.ExpectedSkills) < 2 {
		t.Fatalf("invalid planner regression fixture: %s", raw)
	}
	if PlannerVersion != "rca-planner-v1" || PlannerPromptVersion != "rca-planner-prompt-v1" || PlannerSchemaVersion != "rca-planner-schema-v1" {
		t.Fatal("planner protocol assets lost explicit versioning")
	}
}

func TestPlannerControlledPerformanceRules(t *testing.T) {
	tests := []struct {
		name          string
		summary       string
		scope         json.RawMessage
		hypothesisID  string
		expectedSkill string
	}{
		{"downstream", "downstream RPC 调用耗时 1800ms", json.RawMessage(`{"serviceName":"order","topologyNodeKey":"svc:order"}`), "downstream-latency", "find_dependencies"},
		{"redis", "Redis connection pool 连接池等待", json.RawMessage(`{"redisDataSourceId":2}`), "redis-latency", "diagnose_redis_connection_pool"},
		{"nginx", "Nginx upstream 504 timeout", json.RawMessage(`{"nginxDataSourceId":3}`), "nginx-upstream", "diagnose_nginx_504"},
		{"kubernetes", "Kubernetes pod OOM 重启", json.RawMessage(`{"kubernetesDataSourceId":4,"namespace":"prod","podName":"order-1"}`), "k8s-pressure", "run_k8s_diagnostic_rules"},
		{"linux-memory", "memory swap 内存压力", json.RawMessage(`{"linuxHostId":5}`), "linux-memory", "diagnose_linux_memory_pressure"},
		{"linux-disk", "disk io iowait 磁盘异常", json.RawMessage(`{"linuxHostId":5}`), "linux-disk-io", "diagnose_linux_disk_io"},
		{"linux-network", "network packet loss 网络丢包", json.RawMessage(`{"linuxHostId":5}`), "linux-network", "diagnose_linux_network"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := deterministicPlan(PlannerInput{
				Round: 1, Scope: test.scope, Budget: PlannerBudget{RemainingRounds: 2, RemainingSkillCalls: 8, RemainingWallTimeSeconds: 120},
				Evidence: []PlannerEvidence{{ID: 1, Summary: test.summary}},
			}, nil)
			if plannerHypothesisByID(result.Hypotheses, test.hypothesisID) == nil || !hasPlannerSkill(result.NextActions, test.expectedSkill) {
				t.Fatalf("controlled rule did not activate: %+v", result)
			}
		})
	}
}

func TestPlannerNormalMetricLowersExistingConfidenceWithCounterevidence(t *testing.T) {
	service, actor, runID, roundID := plannerTestRun(t, json.RawMessage(`{"linuxHostId":9}`), nil)
	previous := addPlannerEvidence(t, service, actor, runID, roundID, "metric", "CPU load high", json.RawMessage(`{"deltaPercent":80}`))
	record := addPlannerEvidence(t, service, actor, runID, roundID, "metric", "CPU 正常，within baseline", json.RawMessage(`{"deltaPercent":2}`))
	result, err := service.PlanNext(context.Background(), actor, runID, PlanRequest{
		UseLLM: boolPointer(false),
		ExistingHypotheses: []PlannerHypothesis{{
			ID: "linux-cpu", Summary: "Linux 主机 CPU 压力导致服务处理变慢",
			Confidence: .7, SupportingEvidenceIDs: []int64{previous.ID},
		}},
	})
	if err != nil {
		t.Fatalf("plan next: %v", err)
	}
	hypothesis := plannerHypothesisByID(result.Hypotheses, "linux-cpu")
	if hypothesis == nil || hypothesis.Confidence >= .7 || !containsInt64(hypothesis.ContradictingEvidenceIDs, record.ID) {
		t.Fatalf("normal CPU evidence did not lower confidence: %+v", hypothesis)
	}
}

func TestPlannerFiltersUnknownUnauthorizedInvalidAndDuplicateActions(t *testing.T) {
	service, actor, runID, roundID := plannerTestRun(t, json.RawMessage(`{
		"serviceName":"order-service","topologyNodeKey":"svc:order","tidbDataSourceId":7
	}`), []datasourcesvc.DataSourceView{
		plannerDataSource(7, model.DataSourceTypeTiDB),
		plannerDataSource(8, model.DataSourceTypeTiDB),
	})
	addPlannerEvidence(t, service, actor, runID, roundID, "log", "database call took 2400ms", json.RawMessage(`{"durationMs":2400}`))
	if _, err := service.CreateAction(context.Background(), actor, runID, CreateActionInput{
		RoundID: roundID, ActionKey: "existing", SkillName: "find_dependencies",
		Input: json.RawMessage(`{"depth":2,"direction":"downstream","maxEdges":100,"maxNodes":50,"nodeKey":"svc:order"}`),
	}); err != nil {
		t.Fatalf("create existing action: %v", err)
	}
	service.plannerModel = plannerModelFunc(func(context.Context, PlannerModelRequest) (json.RawMessage, error) {
		return json.RawMessage(`{
			"hypotheses":[{"id":"database-latency","summary":"db slow","confidence":0.99,"supportingEvidenceIds":[1],"contradictingEvidenceIds":[]}],
			"missingEvidence":[],
			"nextActions":[
				{"actionKey":"bad-unknown","skillName":"delete_database","input":{},"reason":"bad"},
				{"actionKey":"bad-source","skillName":"query_tidb_slow_queries","input":{"dataSourceId":999,"minutes":30,"limit":20},"reason":"bad"},
				{"actionKey":"bad-type","skillName":"query_tidb_slow_queries","input":{"dataSourceId":"7"},"reason":"bad"},
				{"actionKey":"valid","skillName":"query_tidb_slow_queries","input":{"dataSourceId":7,"minutes":30,"limit":20},"reason":"valid"}
			],
			"shouldStop":false,"stopReason":""
		}`), nil
	})
	result, err := service.PlanNext(context.Background(), actor, runID, PlanRequest{})
	if err != nil {
		t.Fatalf("plan next: %v", err)
	}
	for _, action := range result.NextActions {
		if action.SkillName == "delete_database" || string(action.Input) == `{"dataSourceId":999,"minutes":30,"limit":20}` {
			t.Fatalf("unsafe planner action survived validation: %+v", action)
		}
	}
	if countPlannerSkill(result.NextActions, "find_dependencies") != 0 {
		t.Fatalf("previously executed action was not suppressed: %+v", result.NextActions)
	}
	if countPlannerSkill(result.NextActions, "query_tidb_slow_queries") != 1 {
		t.Fatalf("valid duplicate slow SQL actions were not canonicalized: %+v", result.NextActions)
	}
}

func TestPlannerFallsBackWhenLLMReturnsInvalidJSON(t *testing.T) {
	service, actor, runID, roundID := plannerTestRun(t, json.RawMessage(`{
		"serviceName":"order-service","topologyNodeKey":"svc:order","tidbDataSourceId":7
	}`), []datasourcesvc.DataSourceView{plannerDataSource(7, model.DataSourceTypeTiDB)})
	addPlannerEvidence(t, service, actor, runID, roundID, "log", "database call took 2400ms", json.RawMessage(`{"durationMs":2400}`))
	service.plannerModel = plannerModelFunc(func(context.Context, PlannerModelRequest) (json.RawMessage, error) {
		return json.RawMessage(`not-json`), nil
	})
	result, err := service.PlanNext(context.Background(), actor, runID, PlanRequest{})
	if err != nil {
		t.Fatalf("plan next: %v", err)
	}
	if !result.PlannerDegraded || !hasPlannerSkill(result.NextActions, "query_tidb_slow_queries") {
		t.Fatalf("invalid LLM output did not fall back safely: %+v", result)
	}
}

func TestPlannerPreservesConfidenceWithoutNewEvidence(t *testing.T) {
	input := PlannerInput{
		ExistingHypotheses: []PlannerHypothesis{{ID: "h1", Summary: "same", Confidence: .4, SupportingEvidenceIDs: []int64{1}}},
		Evidence:           []PlannerEvidence{{ID: 1}},
	}
	got := validatePlannerHypotheses(input, []PlannerHypothesis{{ID: "h1", Summary: "same", Confidence: .9, SupportingEvidenceIDs: []int64{1}}})
	if len(got) != 1 || got[0].Confidence != .4 {
		t.Fatalf("confidence changed without new evidence: %+v", got)
	}
}

func TestPlannerModelErrorUsesDeterministicFallback(t *testing.T) {
	service, actor, runID, roundID := plannerTestRun(t, json.RawMessage(`{
		"serviceName":"order-service","topologyNodeKey":"svc:order","tidbDataSourceId":7
	}`), []datasourcesvc.DataSourceView{plannerDataSource(7, model.DataSourceTypeTiDB)})
	addPlannerEvidence(t, service, actor, runID, roundID, "log", "database call took 2400ms", nil)
	service.plannerModel = plannerModelFunc(func(context.Context, PlannerModelRequest) (json.RawMessage, error) {
		return nil, errors.New("timeout")
	})
	result, err := service.PlanNext(context.Background(), actor, runID, PlanRequest{})
	if err != nil || !result.PlannerDegraded || !hasPlannerSkill(result.NextActions, "query_tidb_slow_queries") {
		t.Fatalf("model failure fallback failed: result=%+v err=%v", result, err)
	}
}

type plannerModelFunc func(context.Context, PlannerModelRequest) (json.RawMessage, error)

func (f plannerModelFunc) Plan(ctx context.Context, request PlannerModelRequest) (json.RawMessage, error) {
	return f(ctx, request)
}

type fakePlannerCatalog struct {
	definitions map[string]skillframework.SkillDefinition
}

func (f fakePlannerCatalog) Get(name string) (skillframework.SkillDefinition, error) {
	value, ok := f.definitions[name]
	if !ok {
		return skillframework.SkillDefinition{}, skillframework.ErrSkillNotFound
	}
	return value, nil
}

func (f fakePlannerCatalog) List() []skillframework.SkillDefinition {
	result := make([]skillframework.SkillDefinition, 0, len(f.definitions))
	for _, value := range f.definitions {
		result = append(result, value)
	}
	return result
}

func plannerTestRun(t *testing.T, scope json.RawMessage, views []datasourcesvc.DataSourceView) (*Service, *model.AppUser, int64, int64) {
	t.Helper()
	repo := newMemoryRCARepository()
	catalog := plannerTestCatalog()
	service := NewService(repo, &memoryEvidenceCreator{repository: repo}, fakeRCADataSources{views: views}).WithPlanner(catalog, nil)
	actor := &model.AppUser{ID: 1, Username: "admin", Role: model.RoleAdmin, Enabled: true}
	run, err := service.CreateRun(context.Background(), actor, CreateRunInput{Query: "订单服务变慢，请查询可能原因", Scope: scope})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	round, err := service.StartRound(context.Background(), actor, run.ID, StartRoundInput{})
	if err != nil {
		t.Fatalf("start round: %v", err)
	}
	return service, actor, run.ID, round.ID
}

func plannerTestCatalog() fakePlannerCatalog {
	definitions := map[string]skillframework.SkillDefinition{}
	add := func(name, risk, schema string) {
		definitions[name] = skillframework.SkillDefinition{Name: name, Enabled: true, ReadOnly: true, RiskLevel: risk, InputSchema: json.RawMessage(schema)}
	}
	add("find_topology_node", model.SkillRiskSafeRead, `{"type":"object","required":["query"],"properties":{"query":{"type":"string"},"environment":{"type":"string"},"limit":{"type":"integer"}}}`)
	add("find_dependencies", model.SkillRiskSafeRead, `{"type":"object","required":["nodeKey"],"properties":{"nodeKey":{"type":"string"},"direction":{"type":"string"},"depth":{"type":"integer"},"maxNodes":{"type":"integer"},"maxEdges":{"type":"integer"},"environment":{"type":"string"}}}`)
	add("get_topology_data_source_bindings", model.SkillRiskSafeRead, `{"type":"object","required":["nodeKey"],"properties":{"nodeKey":{"type":"string"},"environment":{"type":"string"},"explicitDataSourceIds":{"type":"array","items":{"type":"integer"}},"minimumConfidence":{"type":"number"}}}`)
	add("query_logs", model.SkillRiskSensitiveRead, `{"type":"object"}`)
	add("compare_metric_baseline", model.SkillRiskSensitiveRead, `{"type":"object"}`)
	add("hybrid_search_knowledge", model.SkillRiskSafeRead, `{"type":"object"}`)
	add("query_tidb_slow_queries", model.SkillRiskSensitiveRead, `{"type":"object","required":["dataSourceId"],"properties":{"dataSourceId":{"type":"integer"},"minutes":{"type":"integer"},"limit":{"type":"integer"}}}`)
	add("query_tidb_processlist", model.SkillRiskSensitiveRead, `{"type":"object","required":["dataSourceId"],"properties":{"dataSourceId":{"type":"integer"},"limit":{"type":"integer"}}}`)
	add("query_redis_latency", model.SkillRiskSensitiveRead, `{"type":"object","required":["dataSourceId"],"properties":{"dataSourceId":{"type":"integer"}}}`)
	add("diagnose_redis_connection_pool", model.SkillRiskSensitiveRead, `{"type":"object","required":["dataSourceId"],"properties":{"dataSourceId":{"type":"integer"}}}`)
	add("diagnose_nginx_504", model.SkillRiskSensitiveRead, `{"type":"object","required":["dataSourceId"],"properties":{"dataSourceId":{"type":"integer"},"limit":{"type":"integer"}}}`)
	add("diagnose_nginx_upstream", model.SkillRiskSensitiveRead, `{"type":"object","required":["dataSourceId"],"properties":{"dataSourceId":{"type":"integer"},"limit":{"type":"integer"}}}`)
	add("diagnose_nacos_registration", model.SkillRiskSensitiveRead, `{"type":"object","required":["dataSourceId"],"properties":{"dataSourceId":{"type":"integer"},"namespace":{"type":"string"},"serviceName":{"type":"string"}}}`)
	add("run_k8s_diagnostic_rules", model.SkillRiskSensitiveRead, `{"type":"object","required":["dataSourceId","namespace","podName"],"properties":{"dataSourceId":{"type":"integer"},"namespace":{"type":"string"},"podName":{"type":"string"},"logTailLines":{"type":"integer"}}}`)
	add("diagnose_linux_host_health", model.SkillRiskSafeRead, `{"type":"object","required":["hostId"],"properties":{"hostId":{"type":"integer"},"topN":{"type":"integer"}}}`)
	for _, name := range []string{"diagnose_linux_cpu_pressure", "diagnose_linux_memory_pressure", "diagnose_linux_disk_io", "diagnose_linux_network"} {
		add(name, model.SkillRiskSafeRead, `{"type":"object","required":["hostId"],"properties":{"hostId":{"type":"integer"},"topN":{"type":"integer"}}}`)
	}
	return fakePlannerCatalog{definitions: definitions}
}

func plannerDataSource(id int64, sourceType string) datasourcesvc.DataSourceView {
	return datasourcesvc.DataSourceView{ID: id, Name: sourceType, SourceType: sourceType, Enabled: true, ReadOnly: true}
}

func addPlannerEvidence(t *testing.T, service *Service, actor *model.AppUser, runID, roundID int64, sourceType, summary string, content json.RawMessage) *model.EvidenceRecord {
	t.Helper()
	record, err := service.AddEvidence(context.Background(), actor, runID, CreateEvidenceInput{
		RoundID: roundID, SourceType: sourceType, Summary: summary, Content: content,
		EvidenceKind: model.EvidenceKindFact, SourceSkill: "test",
	})
	if err != nil {
		t.Fatalf("add evidence: %v", err)
	}
	return record
}

func hasPlannerSkill(actions []PlannerAction, name string) bool {
	return countPlannerSkill(actions, name) > 0
}

func countPlannerSkill(actions []PlannerAction, name string) int {
	count := 0
	for _, action := range actions {
		if action.SkillName == name {
			count++
		}
	}
	return count
}

func plannerHypothesisByID(values []PlannerHypothesis, id string) *PlannerHypothesis {
	for index := range values {
		if values[index].ID == id {
			return &values[index]
		}
	}
	return nil
}

func containsInt64(values []int64, expected int64) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func boolPointer(value bool) *bool { return &value }
