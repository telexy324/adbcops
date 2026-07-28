package rca

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	datasourcesvc "aiops-platform/backend/internal/datasource"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/skillframework"
)

func TestRankTopologyCandidatesUsesAliasEvidenceAndExcludesUnrelatedComponents(t *testing.T) {
	raw := topologyDependencyFixture(
		`{"nodeKey":"db:orders","name":"orders-primary","kind":"database","sourceType":"tidb","updatedAt":"2026-07-28T05:55:00Z"}`,
		`{"nodeKey":"redis:sessions","name":"session-cache","kind":"redis","sourceType":"redis","updatedAt":"2026-07-28T05:55:00Z"}`,
	)
	candidates := rankTopologyCandidates(
		raw,
		[]model.EvidenceRecord{{ID: 11, Summary: "order-db call took 2400ms; database timeout"}},
		[]string{"order-db"},
		map[string]bool{"db:orders": true},
		[]PlannerHypothesis{{ID: "database-latency", Summary: "database latency"}},
		99,
		time.Date(2026, 7, 28, 6, 0, 0, 0, time.UTC),
	)
	if len(candidates) != 1 || candidates[0].NodeKey != "db:orders" || !candidates[0].AliasMatched {
		t.Fatalf("alias did not select only the evidence-related database: %+v", candidates)
	}
	selected, conflicts := selectTopologyCandidates(candidates)
	if len(selected) != 1 || len(conflicts) != 0 {
		t.Fatalf("expected one reliable database selection: selected=%+v conflicts=%+v", selected, conflicts)
	}
}

func TestSelectTopologyCandidatesFlagsStaleAndConflictingRelationships(t *testing.T) {
	raw := topologyDependencyFixture(
		`{"nodeKey":"db:a","name":"orders-a","kind":"database","sourceType":"tidb","updatedAt":"2026-07-20T05:55:00Z"}`,
		`{"nodeKey":"db:b","name":"orders-b","kind":"database","sourceType":"tidb","updatedAt":"2026-07-20T05:55:00Z"}`,
	)
	candidates := rankTopologyCandidates(
		raw,
		[]model.EvidenceRecord{{ID: 1, Summary: "database timeout"}},
		nil,
		nil,
		[]PlannerHypothesis{{ID: "database-latency"}},
		9,
		time.Date(2026, 7, 28, 6, 0, 0, 0, time.UTC),
	)
	selected, conflicts := selectTopologyCandidates(candidates)
	if len(selected) != 0 || len(conflicts) != 1 {
		t.Fatalf("equally ranked database dependencies were not treated as conflict: selected=%+v conflicts=%+v", selected, conflicts)
	}
	for _, candidate := range candidates {
		if candidate.Freshness != "stale" {
			t.Fatalf("stale topology was not marked: %+v", candidates)
		}
	}
}

func TestTopologyCandidateDirectionIsDerivedFromRootEdges(t *testing.T) {
	raw := json.RawMessage(`{
		"rootKey":"svc:order",
		"dependencies":[{"node":{"nodeKey":"nginx:edge","name":"orders-nginx","kind":"nginx","sourceType":"nginx","updatedAt":"2026-07-28T05:55:00Z"},"hops":1}],
		"edges":[{"fromNodeKey":"nginx:edge","toNodeKey":"svc:order","edgeType":"routes_to","confidence":0.9,"status":"active","lastObservedAt":"2026-07-28T05:58:00Z"}]
	}`)
	candidates := rankTopologyCandidates(
		raw,
		[]model.EvidenceRecord{{Summary: "Nginx upstream 504"}},
		nil,
		nil,
		[]PlannerHypothesis{{ID: "nginx-upstream"}},
		1,
		time.Date(2026, 7, 28, 6, 0, 0, 0, time.UTC),
	)
	if len(candidates) != 1 || candidates[0].Direction != "upstream" {
		t.Fatalf("upstream topology direction was not preserved: %+v", candidates)
	}
}

func TestTopologyGuidedRoundRunsDatabaseSkillAndCitesOriginalEvidence(t *testing.T) {
	executor := &topologyInvestigationExecutor{}
	service, actor, runID, evidenceID := topologyInvestigationRun(t, executor, json.RawMessage(`{
		"serviceName":"order-service","environment":"prod","topologyNodeKey":"svc:order"
	}`), []datasourcesvc.DataSourceView{plannerDataSource(77, model.DataSourceTypeTiDB)},
		"order-db database call took 2400ms")
	plan := &PlannerResult{Hypotheses: []PlannerHypothesis{{
		ID: "database-latency", Summary: "database call latency", Confidence: .76,
		SupportingEvidenceIDs: []int64{evidenceID},
	}}}
	result, err := service.executeTopologyGuidedRound(context.Background(), actor, runID, plan, 8)
	if err != nil {
		t.Fatalf("execute topology-guided round: %v", err)
	}
	if result.Topology == nil || len(result.Topology.Selected) != 1 || result.Topology.Selected[0].NodeKey != "db:orders" {
		t.Fatalf("database dependency was not selected: %+v", result.Topology)
	}
	calls := executor.snapshot()
	if !containsSkillCall(calls, "query_tidb_slow_queries") || containsSkillCall(calls, "diagnose_redis_connection_pool") {
		t.Fatalf("component investigation was not bounded to database evidence: %+v", calls)
	}
	detail, detailErr := service.GetDetail(context.Background(), actor, runID)
	if detailErr != nil {
		t.Fatalf("load detail: %v", detailErr)
	}
	foundOriginalCitation := false
	for _, record := range detail.Evidence {
		if record.SourceSkill == nil || *record.SourceSkill != "query_tidb_slow_queries" {
			continue
		}
		var sourceRef map[string]any
		_ = json.Unmarshal(record.SourceRef, &sourceRef)
		for _, value := range sourceRef["inputEvidenceIds"].([]any) {
			if int64(value.(float64)) == evidenceID {
				foundOriginalCitation = true
			}
		}
	}
	if !foundOriginalCitation {
		t.Fatalf("component evidence did not cite original evidence %d: %+v", evidenceID, detail.Evidence)
	}
}

func TestTopologyGuidedRoundRunsRedisSkill(t *testing.T) {
	executor := &topologyInvestigationExecutor{}
	service, actor, runID, evidenceID := topologyInvestigationRun(t, executor, json.RawMessage(`{
		"serviceName":"order-service","environment":"prod","topologyNodeKey":"svc:order"
	}`), []datasourcesvc.DataSourceView{plannerDataSource(88, model.DataSourceTypeRedis)},
		"Redis connection pool wait increased")
	plan := &PlannerResult{Hypotheses: []PlannerHypothesis{{
		ID: "redis-latency", Summary: "redis latency", Confidence: .72, SupportingEvidenceIDs: []int64{evidenceID},
	}}}
	result, err := service.executeTopologyGuidedRound(context.Background(), actor, runID, plan, 7)
	if err != nil {
		t.Fatalf("execute topology-guided round: %v", err)
	}
	calls := executor.snapshot()
	if !containsSkillCall(calls, "diagnose_redis_connection_pool") || containsSkillCall(calls, "query_tidb_slow_queries") {
		t.Fatalf("Redis evidence did not trigger only Redis component investigation: %+v result=%+v", calls, result)
	}
}

func TestTopologyGuidedRoundFallsBackToExplicitDataSourceWhenTopologyMissing(t *testing.T) {
	executor := &topologyInvestigationExecutor{missingTopology: true}
	service, actor, runID, evidenceID := topologyInvestigationRun(t, executor, json.RawMessage(`{
		"serviceName":"order-service","environment":"prod","topologyNodeKey":"svc:order","tidbDataSourceId":77
	}`), []datasourcesvc.DataSourceView{plannerDataSource(77, model.DataSourceTypeTiDB)},
		"database call timeout")
	plan := &PlannerResult{Hypotheses: []PlannerHypothesis{{
		ID: "database-latency", Summary: "database latency", Confidence: .7, SupportingEvidenceIDs: []int64{evidenceID},
	}}}
	result, err := service.executeTopologyGuidedRound(context.Background(), actor, runID, plan, 5)
	if err == nil || result.Topology == nil || !result.Topology.FallbackUsed {
		t.Fatalf("missing topology did not produce partial explicit fallback: result=%+v err=%v", result, err)
	}
	if !containsSkillCall(executor.snapshot(), "query_tidb_slow_queries") ||
		!strings.Contains(strings.Join(result.Topology.MissingEvidence, " "), "no relevant reliable topology") {
		t.Fatalf("explicit database fallback or missing evidence absent: %+v calls=%+v", result.Topology, executor.snapshot())
	}
}

func TestTopologyGuidedRoundRejectsUnauthorizedBoundDataSource(t *testing.T) {
	executor := &topologyInvestigationExecutor{}
	service, actor, runID, evidenceID := topologyInvestigationRun(t, executor, json.RawMessage(`{
		"serviceName":"order-service","environment":"prod","topologyNodeKey":"svc:order"
	}`), []datasourcesvc.DataSourceView{plannerDataSource(88, model.DataSourceTypeRedis)},
		"database call timeout")
	plan := &PlannerResult{Hypotheses: []PlannerHypothesis{{
		ID: "database-latency", Summary: "database latency", Confidence: .7, SupportingEvidenceIDs: []int64{evidenceID},
	}}}
	result, err := service.executeTopologyGuidedRound(context.Background(), actor, runID, plan, 7)
	if err == nil || result.Status != model.RCARoundStatusPartialSuccess {
		t.Fatalf("unauthorized binding should degrade the round: result=%+v err=%v", result, err)
	}
	if containsSkillCall(executor.snapshot(), "query_tidb_slow_queries") {
		t.Fatalf("unauthorized TiDB binding was executed: %+v", executor.snapshot())
	}
}

func TestTopologyCandidateAndActionDeduplication(t *testing.T) {
	node := `{"nodeKey":"db:orders","name":"order-db","kind":"database","sourceType":"tidb","updatedAt":"2026-07-28T05:55:00Z"}`
	candidates := rankTopologyCandidates(
		topologyDependencyFixture(node, node),
		[]model.EvidenceRecord{{Summary: "order-db database timeout"}},
		nil,
		nil,
		[]PlannerHypothesis{{ID: "database-latency"}},
		1,
		time.Date(2026, 7, 28, 6, 0, 0, 0, time.UTC),
	)
	if len(candidates) != 1 {
		t.Fatalf("same dependency was not deduplicated: %+v", candidates)
	}
	action := plannerAction("query_tidb_slow_queries", map[string]any{"dataSourceId": 77}, "test", "db:orders")
	if got := deduplicatePlannerActions([]PlannerAction{action, action}); len(got) != 1 {
		t.Fatalf("same component action was not deduplicated: %+v", got)
	}
}

func TestLinuxTopologyCandidateUsesResolvedHostID(t *testing.T) {
	candidate := TopologyInvestigationCandidate{
		NodeKey: "host:9", Name: "orders-host", ComponentType: "linux", HostID: 9,
		TopologyEvidenceIDs: []int64{3},
	}
	action, ok := componentInvestigationAction(nil, candidate, []model.EvidenceRecord{{ID: 1}})
	if !ok || action.SkillName != "diagnose_linux_host_health" ||
		!strings.Contains(string(action.Input), `"hostId":9`) {
		t.Fatalf("Linux topology host was not converted to a bounded health action: %+v ok=%v", action, ok)
	}
}

type topologyInvestigationExecutor struct {
	mu              sync.Mutex
	calls           []roundOneSkillCall
	missingTopology bool
}

func (f *topologyInvestigationExecutor) Execute(_ context.Context, input skillframework.ExecuteInput) (*skillframework.ExecuteResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, roundOneSkillCall{Name: input.Name, Payload: append(json.RawMessage(nil), input.Payload...)})
	f.mu.Unlock()
	output := json.RawMessage(`{"summary":"component evidence"}`)
	switch input.Name {
	case "find_topology_node":
		var payload map[string]any
		_ = json.Unmarshal(input.Payload, &payload)
		if strings.Contains(payload["query"].(string), "order-db") {
			output = json.RawMessage(`{"node":{"nodeKey":"db:orders","name":"orders-primary"}}`)
		} else {
			output = json.RawMessage(`{"node":{"nodeKey":"svc:order","name":"order-service"}}`)
		}
	case "find_dependencies":
		if f.missingTopology {
			output = json.RawMessage(`{"rootKey":"svc:order","dependencies":[],"edges":[],"missingEvidence":["no dependency edge"]}`)
		} else {
			output = topologyDependencyFixture(
				`{"nodeKey":"db:orders","name":"orders-primary","kind":"database","sourceType":"tidb","updatedAt":"2026-07-28T05:55:00Z"}`,
				`{"nodeKey":"redis:sessions","name":"session-cache","kind":"redis","sourceType":"redis","updatedAt":"2026-07-28T05:55:00Z"}`,
			)
		}
	case "get_topology_data_source_bindings":
		if strings.Contains(string(input.Payload), "redis:sessions") {
			output = json.RawMessage(`{"bindings":[{"dataSourceId":88,"sourceType":"redis","confidence":0.96,"status":"accepted","accepted":true}],"conflicts":[]}`)
		} else {
			output = json.RawMessage(`{"bindings":[{"dataSourceId":77,"sourceType":"tidb","confidence":0.97,"status":"accepted","accepted":true}],"conflicts":[]}`)
		}
	case "query_tidb_slow_queries":
		output = json.RawMessage(`{"summary":"slow SQL found","items":[{"digest":"select-order","latencyMs":2300}]}`)
	case "diagnose_redis_connection_pool":
		output = json.RawMessage(`{"summary":"Redis pool wait high","pool":{"waitMs":800}}`)
	}
	return &skillframework.ExecuteResult{SkillName: input.Name, RunID: int64(len(f.snapshot())), Output: output}, nil
}

func (f *topologyInvestigationExecutor) snapshot() []roundOneSkillCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]roundOneSkillCall, len(f.calls))
	copy(result, f.calls)
	return result
}

func topologyInvestigationRun(
	t *testing.T,
	executor RoundOneSkillExecutor,
	scope json.RawMessage,
	views []datasourcesvc.DataSourceView,
	summary string,
) (*Service, *model.AppUser, int64, int64) {
	t.Helper()
	repo := newMemoryRCARepository()
	service := NewService(repo, &memoryEvidenceCreator{repository: repo}, fakeRCADataSources{views: views}).
		WithSkillExecutor(executor).
		WithPlanner(plannerTestCatalog(), nil)
	service.now = fixedRCATime
	actor := &model.AppUser{ID: 1, Username: "admin", Role: model.RoleAdmin, Enabled: true}
	run, err := service.CreateRun(context.Background(), actor, CreateRunInput{Query: "订单服务变慢", Scope: scope, MaxRounds: 3})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	round, err := service.StartRound(context.Background(), actor, run.ID, StartRoundInput{})
	if err != nil {
		t.Fatalf("start first round: %v", err)
	}
	record := addPlannerEvidence(t, service, actor, run.ID, round.ID, "log", summary, json.RawMessage(`{"durationMs":2400}`))
	if _, err = service.CompleteRound(context.Background(), actor, run.ID, round.ID, CompleteRoundInput{
		Status: model.RCARoundStatusSuccess, NewEvidenceIDs: []int64{record.ID},
	}); err != nil {
		t.Fatalf("complete first round: %v", err)
	}
	return service, actor, run.ID, record.ID
}

func topologyDependencyFixture(nodes ...string) json.RawMessage {
	edges := make([]string, 0, len(nodes))
	for _, node := range nodes {
		var value struct {
			NodeKey string `json:"nodeKey"`
		}
		_ = json.Unmarshal([]byte(node), &value)
		edges = append(edges, `{"fromNodeKey":"svc:order","toNodeKey":"`+value.NodeKey+`","edgeType":"depends_on","confidence":0.95,"status":"active","lastObservedAt":"2026-07-28T05:58:00Z"}`)
	}
	return json.RawMessage(`{"rootKey":"svc:order","dependencies":[` + strings.Join(func() []string {
		result := make([]string, 0, len(nodes))
		for _, node := range nodes {
			result = append(result, `{"node":`+node+`,"dependencyType":"downstream","hops":1}`)
		}
		return result
	}(), ",") + `],"edges":[` + strings.Join(edges, ",") + `],"missingEvidence":[]}`)
}

func containsSkillCall(calls []roundOneSkillCall, name string) bool {
	for _, call := range calls {
		if call.Name == name {
			return true
		}
	}
	return false
}
