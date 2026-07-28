package agentruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	datasourcesvc "aiops-platform/backend/internal/datasource"
	"aiops-platform/backend/internal/model"
	topologysvc "aiops-platform/backend/internal/topology"
)

func TestNaturalLanguagePerformanceQueriesSelectGeneralRCA(t *testing.T) {
	cases := []string{
		"订单服务变慢，请查询可能原因",
		"订单服务响应慢，请排查",
		"订单服务耗时增加",
		"订单服务延迟升高",
		"订单服务卡顿",
		"order-service is slow, find the possible cause",
		"生产 order-service 最近半小时延迟升高",
	}
	for _, query := range cases {
		t.Run(query, func(t *testing.T) {
			plan := BuildCoordinatorPlan(AgentContext{Query: query})
			if plan.Intent != IntentGeneralRCA || plan.Workflow != WorkflowGeneralRCA {
				t.Fatalf("expected general RCA, got %+v", plan)
			}
			if plan.Scope["symptom"] != "performance_degradation" {
				t.Fatalf("symptom was not normalized: %+v", plan.Scope)
			}
		})
	}
}

func TestScopeResolverHonorsExplicitScopeAndRecordsDefaultWindow(t *testing.T) {
	resolver := NewNaturalLanguageScopeResolver(nil, nil, 45*time.Minute)
	resolver.now = func() time.Time { return time.Date(2026, 7, 28, 5, 0, 0, 0, time.UTC) }
	result := resolver.Resolve(context.Background(), nil, AgentContext{
		Query:     "生产 order-service 变慢",
		Variables: map[string]any{"environment": "test", "dataSourceId": float64(8)},
		Scope:     map[string]any{"environment": "prod", "dataSourceId": float64(9)},
	})
	if result.Scope["environment"] != "prod" || result.Scope["dataSourceId"] != float64(9) {
		t.Fatalf("explicit scope must win over variables and inference: %+v", result.Scope)
	}
	if result.Scope["from"] != "2026-07-28T04:15:00Z" || result.Scope["to"] != "2026-07-28T05:00:00Z" {
		t.Fatalf("unexpected default range: %+v", result.Scope)
	}
	if result.Scope["timeRangeSource"] != "default" || result.DefaultTimeWindowMinutes != 45 {
		t.Fatalf("default window audit metadata missing: %+v", result)
	}
}

func TestScopeResolverUsesTopologyAliasAndAccessibleDataSource(t *testing.T) {
	prod := "prod"
	resolver := NewNaturalLanguageScopeResolver(
		fakeScopeTopology{result: &topologysvc.FindNodeResult{Candidates: []model.TopologyNode{{NodeKey: "svc/order", Name: "order-service", Environment: &prod}}}},
		fakeScopeDataSources{views: []datasourcesvc.DataSourceView{{ID: 12, Name: "prod-logs", ComponentName: stringPointer("order-service"), Environment: &prod, Enabled: true, ReadOnly: true}}},
		30*time.Minute,
	)
	result := resolver.Resolve(context.Background(), &model.AppUser{ID: 7, Role: model.RoleUser}, AgentContext{Query: "订单服务变慢，请查询可能原因"})
	if result.Scope["topologyNodeKey"] != "svc/order" || result.Scope["dataSourceId"] != int64(12) {
		t.Fatalf("expected topology alias and data source resolution: %+v", result)
	}
	if result.Scope["environment"] != "prod" || len(result.MissingParameters) != 0 {
		t.Fatalf("unexpected resolved scope: %+v", result)
	}
}

func TestScopeResolverReturnsMissingEnvironmentForAmbiguousCandidates(t *testing.T) {
	prod, testEnv := "prod", "test"
	resolver := NewNaturalLanguageScopeResolver(
		fakeScopeTopology{result: &topologysvc.FindNodeResult{Candidates: []model.TopologyNode{
			{NodeKey: "prod/order", Name: "order-service", Environment: &prod},
			{NodeKey: "test/order", Name: "order-service", Environment: &testEnv},
		}}},
		fakeScopeDataSources{views: []datasourcesvc.DataSourceView{
			{ID: 1, Name: "prod-order", ComponentName: stringPointer("order-service"), Environment: &prod, Enabled: true, ReadOnly: true},
			{ID: 2, Name: "test-order", ComponentName: stringPointer("order-service"), Environment: &testEnv, Enabled: true, ReadOnly: true},
		}}, 30*time.Minute,
	)
	result := resolver.Resolve(context.Background(), &model.AppUser{ID: 7, Role: model.RoleUser}, AgentContext{Query: "order-service is slow"})
	if !result.Ambiguous || !containsString(result.MissingParameters, "environment") {
		t.Fatalf("expected structured environment ambiguity: %+v", result)
	}
	if _, exists := result.Scope["dataSourceId"]; exists {
		t.Fatalf("ambiguous target must not be silently selected: %+v", result.Scope)
	}
}

func TestScopeResolverExcludesUnauthorizedTopologyAndDataSource(t *testing.T) {
	prod, testEnv := "prod", "test"
	resolver := NewNaturalLanguageScopeResolver(
		fakeScopeTopology{result: &topologysvc.FindNodeResult{Candidates: []model.TopologyNode{
			{NodeKey: "prod/order", Name: "order-service", Environment: &prod},
			{NodeKey: "test/order", Name: "order-service", Environment: &testEnv},
		}}},
		fakeScopeDataSources{views: []datasourcesvc.DataSourceView{
			{ID: 2, Name: "test-order", ComponentName: stringPointer("order-service"), Environment: &testEnv, Enabled: true, ReadOnly: true},
		}}, 30*time.Minute,
	)
	result := resolver.Resolve(context.Background(), &model.AppUser{ID: 7, Role: model.RoleUser}, AgentContext{
		Query: "order-service is slow", Scope: map[string]any{"dataSourceId": float64(99)},
	})
	if result.Scope["topologyNodeKey"] != "test/order" || result.Scope["dataSourceId"] != int64(2) {
		t.Fatalf("only accessible candidates should be selected: %+v", result)
	}
}

func TestScopeResolverRejectsUnauthorizedExplicitDataSourceWithoutServiceTerm(t *testing.T) {
	resolver := NewNaturalLanguageScopeResolver(nil, fakeScopeDataSources{views: []datasourcesvc.DataSourceView{
		{ID: 2, Name: "accessible-redis", Enabled: true, ReadOnly: true},
	}}, 30*time.Minute)
	result := resolver.Resolve(context.Background(), &model.AppUser{ID: 7, Role: model.RoleUser}, AgentContext{
		Query: "检查 Redis 状态", Scope: map[string]any{"dataSourceId": float64(99)},
	})
	if _, exists := result.Scope["dataSourceId"]; exists || !containsString(result.MissingParameters, "dataSourceId") {
		t.Fatalf("unauthorized explicit data source must be rejected: %+v", result)
	}
}

func TestScopeResolverDegradesWithoutDroppingExplicitScope(t *testing.T) {
	resolver := NewNaturalLanguageScopeResolver(fakeScopeTopology{err: errors.New("topology unavailable")}, fakeScopeDataSources{err: errors.New("data sources unavailable")}, 30*time.Minute)
	result := resolver.Resolve(context.Background(), &model.AppUser{ID: 7, Role: model.RoleUser}, AgentContext{
		Query: "order-service is slow", Scope: map[string]any{"environment": "prod", "dataSourceId": float64(9)},
	})
	if !result.Degraded || result.Scope["environment"] != "prod" || result.Scope["dataSourceId"] != float64(9) {
		t.Fatalf("degradation must preserve explicit scope: %+v", result)
	}
}

type fakeScopeTopology struct {
	result *topologysvc.FindNodeResult
	err    error
}

func (f fakeScopeTopology) FindNode(context.Context, topologysvc.FindNodeInput) (*topologysvc.FindNodeResult, error) {
	return f.result, f.err
}

type fakeScopeDataSources struct {
	views []datasourcesvc.DataSourceView
	err   error
}

func (f fakeScopeDataSources) List(context.Context, *model.AppUser) ([]datasourcesvc.DataSourceView, error) {
	return f.views, f.err
}

func stringPointer(value string) *string { return &value }

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
