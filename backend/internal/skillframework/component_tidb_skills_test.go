package skillframework

import (
	"context"
	"encoding/json"
	"testing"

	"aiops-platform/backend/internal/model"
	tidbsvc "aiops-platform/backend/internal/tidb"
)

func TestTiDBExplainSkillRejectsUnsafeSQLWithoutExecution(t *testing.T) {
	fake := &fakeTiDBQuerier{}
	skill := tidbSkillByName(t, fake, "explain_tidb_sql")
	output, err := skill.Execute(ContextWithActor(context.Background(), adminActor()), json.RawMessage(`{"dataSourceId":1,"sql":"update t set a=1"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if fake.explainCalls != 0 {
		t.Fatalf("unsafe sql was executed")
	}
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if decoded["partial"] != true {
		t.Fatalf("expected partial safety error: %s", string(output))
	}
}

func TestTiDBPlanRegressionRequiresExplainEvidence(t *testing.T) {
	skill := tidbSkillByName(t, &fakeTiDBQuerier{}, "diagnose_tidb_plan_regression")
	output, err := skill.Execute(ContextWithActor(context.Background(), adminActor()), json.RawMessage(`{"dataSourceId":1}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !jsonContainsString(output, "cannot be asserted without EXPLAIN evidence") {
		t.Fatalf("missing cautious plan regression rule: %s", string(output))
	}
}

func TestTiDBPerformanceDiagnosisCitesEvidence(t *testing.T) {
	fake := &fakeTiDBQuerier{
		slow:      &tidbsvc.QueryResult{DataSourceID: 1, Rows: []map[string]string{{"query": "SELECT [text redacted]"}}},
		processes: &tidbsvc.QueryResult{DataSourceID: 1, Rows: []map[string]string{{"id": "1"}}},
		locks:     &tidbsvc.QueryResult{DataSourceID: 1, Rows: []map[string]string{{"wait": "1"}}},
	}
	skill := tidbSkillByName(t, fake, "diagnose_tidb_performance")
	output, err := skill.Execute(ContextWithActor(context.Background(), adminActor()), json.RawMessage(`{"dataSourceId":1,"limit":10}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !jsonContainsString(output, "query_tidb_slow_queries") || !jsonContainsString(output, "query_tidb_lock_waits") {
		t.Fatalf("diagnosis did not cite evidence refs: %s", string(output))
	}
}

func tidbSkillByName(t *testing.T, tidb TiDBQuerier, name string) Skill {
	t.Helper()
	for _, skill := range TiDBSkills(tidb) {
		if skill.Definition().Name == name {
			return skill
		}
	}
	t.Fatalf("tidb skill %s not found", name)
	return nil
}

type fakeTiDBQuerier struct {
	slow         *tidbsvc.QueryResult
	processes    *tidbsvc.QueryResult
	locks        *tidbsvc.QueryResult
	cluster      *tidbsvc.QueryResult
	hotRegions   *tidbsvc.QueryResult
	stats        *tidbsvc.QueryResult
	explain      *tidbsvc.ExplainResult
	explainCalls int
}

func (f *fakeTiDBQuerier) QueryClusterStatus(context.Context, *model.AppUser, tidbsvc.QueryInput) (*tidbsvc.QueryResult, error) {
	if f.cluster != nil {
		return f.cluster, nil
	}
	return &tidbsvc.QueryResult{}, nil
}

func (f *fakeTiDBQuerier) QuerySlowQueries(context.Context, *model.AppUser, tidbsvc.SlowQueryInput) (*tidbsvc.QueryResult, error) {
	if f.slow != nil {
		return f.slow, nil
	}
	return &tidbsvc.QueryResult{}, nil
}

func (f *fakeTiDBQuerier) QueryProcessList(context.Context, *model.AppUser, tidbsvc.QueryInput) (*tidbsvc.QueryResult, error) {
	if f.processes != nil {
		return f.processes, nil
	}
	return &tidbsvc.QueryResult{}, nil
}

func (f *fakeTiDBQuerier) QueryLockWaits(context.Context, *model.AppUser, tidbsvc.QueryInput) (*tidbsvc.QueryResult, error) {
	if f.locks != nil {
		return f.locks, nil
	}
	return &tidbsvc.QueryResult{}, nil
}

func (f *fakeTiDBQuerier) QueryHotRegions(context.Context, *model.AppUser, tidbsvc.QueryInput) (*tidbsvc.QueryResult, error) {
	if f.hotRegions != nil {
		return f.hotRegions, nil
	}
	return &tidbsvc.QueryResult{}, nil
}

func (f *fakeTiDBQuerier) QueryStatisticsHealth(context.Context, *model.AppUser, tidbsvc.QueryInput) (*tidbsvc.QueryResult, error) {
	if f.stats != nil {
		return f.stats, nil
	}
	return &tidbsvc.QueryResult{}, nil
}

func (f *fakeTiDBQuerier) Explain(context.Context, *model.AppUser, tidbsvc.ExplainInput) (*tidbsvc.ExplainResult, error) {
	f.explainCalls++
	if f.explain != nil {
		return f.explain, nil
	}
	return &tidbsvc.ExplainResult{}, nil
}
