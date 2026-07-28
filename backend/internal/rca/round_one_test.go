package rca

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	datasourcesvc "aiops-platform/backend/internal/datasource"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/skillframework"
)

func TestCollectRoundOneRunsSourcesInParallelWithControlledQueries(t *testing.T) {
	repo := newMemoryRCARepository()
	executor := newRoundOneFakeExecutor()
	service := NewService(repo, &memoryEvidenceCreator{repository: repo}, fakeRCADataSources{
		views: roundOneViews(),
	}).WithSkillExecutor(executor)
	service.now = fixedRCATime
	actor := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	run, err := service.CreateRun(context.Background(), actor, CreateRunInput{
		Query: "订单服务变慢，请查询可能原因",
		Scope: json.RawMessage(`{
			"serviceName":"order-service",
			"environment":"prod",
			"namespace":"orders",
			"from":"2026-07-28T05:30:00Z",
			"to":"2026-07-28T06:00:00Z",
			"logDataSourceId":1,
			"metricsDataSourceId":2,
			"dependencyNames":["order-db"],
			"traceId":"4bf92f3577b34da6",
			"logTemplates":["2026-07-28T05:50:00Z db 10.2.3.4 call 987654 timeout"]
		}`),
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	result, err := service.CollectRoundOne(context.Background(), actor, run.ID, RoundOneCollectionInput{})
	if err != nil {
		t.Fatalf("collect round one: %v", err)
	}
	if result.Status != model.RCARoundStatusSuccess || len(result.Actions) != 10 || len(result.Evidence) != 10 {
		t.Fatalf("unexpected result: status=%s actions=%d evidence=%d missing=%v", result.Status, len(result.Actions), len(result.Evidence), result.Missing)
	}
	if executor.maxActive < 3 {
		t.Fatalf("sources were not queried concurrently, max active=%d", executor.maxActive)
	}
	if result.Windows.CurrentFrom.Format(time.RFC3339) != "2026-07-28T05:30:00Z" ||
		result.Windows.BaselineFrom.Format(time.RFC3339) != "2026-07-28T05:00:00Z" ||
		result.Windows.BaselineTo.Format(time.RFC3339) != "2026-07-28T05:30:00Z" {
		t.Fatalf("unexpected windows: %+v", result.Windows)
	}

	calls := executor.snapshot()
	metricKeys := map[string]bool{}
	for _, call := range calls {
		var payload map[string]any
		if json.Unmarshal(call.Payload, &payload) != nil {
			t.Fatalf("invalid payload for %s: %s", call.Name, call.Payload)
		}
		switch call.Name {
		case "compare_metric_baseline":
			query, _ := payload["query"].(string)
			for _, key := range []string{"latency_p99", "error_rate", "qps", "resource_saturation"} {
				if strings.Contains(call.ActionKey, key) {
					metricKeys[key] = true
				}
			}
			if !strings.Contains(query, `service="order-service"`) ||
				!strings.Contains(query, `environment="prod"`) ||
				!strings.Contains(query, `namespace="orders"`) {
				t.Fatalf("PromQL does not use validated scope labels: %s", query)
			}
			if payload["maxSeries"] != float64(roundOneMaxSeries) || payload["maxPoints"] != float64(roundOneMaxPoints) {
				t.Fatalf("metric limits missing: %+v", payload)
			}
		case "query_logs":
			query, _ := payload["queryString"].(string)
			if query == run.Query || (!strings.Contains(query, "error") && !strings.Contains(query, "timeout") &&
				!strings.Contains(query, "order-db") && !strings.Contains(query, "4bf92f") && !strings.Contains(query, "db")) {
				t.Fatalf("log query is not a controlled diagnostic query: %q", query)
			}
			for _, dynamic := range []string{"2026-07-28", "10.2.3.4", "987654"} {
				if strings.Contains(query, dynamic) {
					t.Fatalf("dynamic log value %q leaked into cluster query %q", dynamic, query)
				}
			}
			if payload["size"] != float64(roundOneLogSize) {
				t.Fatalf("log result limit missing: %+v", payload)
			}
		}
	}
	for _, key := range []string{"latency_p99", "error_rate", "qps", "resource_saturation"} {
		if !metricKeys[key] {
			t.Fatalf("metric %s was not attempted: %+v", key, metricKeys)
		}
	}
	for _, attempt := range result.Attempts {
		if attempt.SkillRunID == 0 || attempt.Status != model.RCAActionStatusSuccess {
			t.Fatalf("attempt is not traceable to a successful Skill Run: %+v", attempt)
		}
	}
	for _, evidence := range result.Evidence {
		if evidence.SourceSkill == nil || evidence.RCARoundID == nil || evidence.RCAActionID == nil {
			t.Fatalf("evidence is missing RCA/Skill trace: %+v", evidence)
		}
		var sourceRef map[string]any
		if json.Unmarshal(evidence.SourceRef, &sourceRef) != nil || sourceRef["skillRunId"] == nil {
			t.Fatalf("evidence source ref is not auditable: %s", evidence.SourceRef)
		}
		if evidence.SourceType == "log" && !strings.Contains(string(evidence.Content), `"templateClusters"`) {
			t.Fatalf("log evidence is missing normalized clusters: %s", evidence.Content)
		}
	}
}

func TestCollectRoundOnePreservesOtherEvidenceWhenLogSourceFails(t *testing.T) {
	repo := newMemoryRCARepository()
	executor := newRoundOneFakeExecutor()
	executor.failSkills["query_logs"] = errors.New("elasticsearch unavailable")
	service := NewService(repo, &memoryEvidenceCreator{repository: repo}, fakeRCADataSources{
		views: roundOneViews(),
	}).WithSkillExecutor(executor)
	service.now = fixedRCATime
	actor := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	run, _ := service.CreateRun(context.Background(), actor, CreateRunInput{
		Query: "订单服务变慢", Scope: json.RawMessage(`{
			"serviceName":"order-service","environment":"prod",
			"from":"2026-07-28T05:30:00Z","to":"2026-07-28T06:00:00Z",
			"logDataSourceId":1,"metricsDataSourceId":2
		}`),
	})

	result, err := service.CollectRoundOne(context.Background(), actor, run.ID, RoundOneCollectionInput{})
	if err != nil {
		t.Fatalf("collect partial round: %v", err)
	}
	if result.Status != model.RCARoundStatusPartialSuccess || len(result.Evidence) != 5 {
		t.Fatalf("partial result lost successful evidence: status=%s evidence=%d", result.Status, len(result.Evidence))
	}
	failedLogs, successfulOthers := 0, 0
	for _, attempt := range result.Attempts {
		if attempt.SkillName == "query_logs" && attempt.Status == model.RCAActionStatusFailed {
			failedLogs++
		}
		if attempt.SkillName != "query_logs" && attempt.Status == model.RCAActionStatusSuccess {
			successfulOthers++
		}
	}
	if failedLogs != 2 || successfulOthers != 5 {
		t.Fatalf("unexpected attempts: %+v", result.Attempts)
	}
	stored, _ := service.GetRun(context.Background(), actor, run.ID)
	if stored.Status != model.RCARunStatusPartialSuccess {
		t.Fatalf("run did not retain partial_success: %+v", stored)
	}
}

func TestRoundOneRejectsUnsafePromLabelAndOversizedWindow(t *testing.T) {
	scope := roundOneScope{
		ServiceName:    `order"} or vector(1)`,
		MetricSourceID: 2,
		Windows: RoundOneWindows{
			CurrentFrom: fixedRCATime().Add(-time.Hour), CurrentTo: fixedRCATime(),
			BaselineFrom: fixedRCATime().Add(-2 * time.Hour), BaselineTo: fixedRCATime().Add(-time.Hour),
		},
	}
	if _, _, err := buildRoundOnePlans("slow", scope); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unsafe label error=%v, want ErrInvalidInput", err)
	}
	if _, err := normalizeRoundOneWindows(
		"2026-07-26T00:00:00Z", "2026-07-28T06:00:00Z", "", "", fixedRCATime(),
	); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("oversized window error=%v, want ErrInvalidInput", err)
	}
}

func TestRoundOneRejectsUnauthorizedExplicitDataSource(t *testing.T) {
	repo := newMemoryRCARepository()
	service := NewService(repo, &memoryEvidenceCreator{repository: repo}, fakeRCADataSources{
		views: roundOneViews(),
	}).WithSkillExecutor(newRoundOneFakeExecutor())
	service.now = fixedRCATime
	actor := &model.AppUser{ID: 7, Role: model.RoleUser}
	_, err := service.CreateRun(context.Background(), actor, CreateRunInput{
		Query: "订单服务变慢",
		Scope: json.RawMessage(`{"serviceName":"order-service","logDataSourceId":99,"from":"2026-07-28T05:30:00Z","to":"2026-07-28T06:00:00Z"}`),
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("unauthorized data source error=%v, want ErrForbidden", err)
	}
}

type roundOneSkillCall struct {
	Name      string
	ActionKey string
	Payload   json.RawMessage
}

type roundOneFakeExecutor struct {
	mu         sync.Mutex
	nextRunID  int64
	active     int
	maxActive  int
	calls      []roundOneSkillCall
	failSkills map[string]error
}

func newRoundOneFakeExecutor() *roundOneFakeExecutor {
	return &roundOneFakeExecutor{nextRunID: 1, failSkills: map[string]error{}}
}

func (f *roundOneFakeExecutor) Execute(_ context.Context, input skillframework.ExecuteInput) (*skillframework.ExecuteResult, error) {
	var payload map[string]any
	_ = json.Unmarshal(input.Payload, &payload)
	actionKey := ""
	switch input.Name {
	case "compare_metric_baseline":
		query, _ := payload["query"].(string)
		for _, key := range []string{"latency_p99", "error_rate", "qps", "resource_saturation"} {
			if queryContainsMetricKey(query, key) {
				actionKey = "round1:metrics:" + key
			}
		}
	case "query_logs":
		query, _ := payload["queryString"].(string)
		actionKey = "round1:logs:" + classifyFakeLogQuery(query)
	default:
		actionKey = "round1:knowledge"
	}
	f.mu.Lock()
	runID := f.nextRunID
	f.nextRunID++
	f.active++
	if f.active > f.maxActive {
		f.maxActive = f.active
	}
	f.calls = append(f.calls, roundOneSkillCall{Name: input.Name, ActionKey: actionKey, Payload: append(json.RawMessage(nil), input.Payload...)})
	f.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	f.mu.Lock()
	f.active--
	failure := f.failSkills[input.Name]
	f.mu.Unlock()
	if failure != nil {
		return nil, failure
	}
	var output json.RawMessage
	switch input.Name {
	case "query_logs":
		output = json.RawMessage(`{"items":[{"level":"ERROR","message":"database call timeout"}],"total":1,"timedOut":false}`)
	case "compare_metric_baseline":
		output = json.RawMessage(`{"partial":false,"current":{"series":[{"points":[{"value":2.4}]}]},"baseline":{"series":[{"points":[{"value":0.08}]}]},"summary":{"deltaPercent":2900}}`)
	default:
		output = json.RawMessage(`{"recallCount":1,"citations":[{"citationId":"KC-9-11","documentId":3,"documentVersionId":9,"chunkId":11}],"context":[{"citationId":"KC-9-11"}],"retrievalTrace":{"channels":[]}}`)
	}
	return &skillframework.ExecuteResult{SkillName: input.Name, RunID: runID, Output: output}, nil
}

func (f *roundOneFakeExecutor) snapshot() []roundOneSkillCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]roundOneSkillCall, len(f.calls))
	copy(result, f.calls)
	return result
}

func roundOneViews() []datasourcesvc.DataSourceView {
	prod := "prod"
	component := "order-service"
	return []datasourcesvc.DataSourceView{
		{ID: 1, Name: "order-logs", SourceType: model.DataSourceTypeElasticsearch, Environment: &prod, ComponentName: &component, Enabled: true, ReadOnly: true},
		{ID: 2, Name: "order-metrics", SourceType: model.DataSourceTypePrometheus, Environment: &prod, ComponentName: &component, Enabled: true, ReadOnly: true},
	}
}

func queryContainsMetricKey(query, key string) bool {
	switch key {
	case "latency_p99":
		return strings.Contains(query, "histogram_quantile")
	case "error_rate":
		return strings.Contains(query, `status=~"5.."`)
	case "qps":
		return strings.HasPrefix(query, "sum(rate") && !strings.Contains(query, `status=~`)
	case "resource_saturation":
		return strings.Contains(query, "process_cpu_seconds_total")
	default:
		return false
	}
}

func classifyFakeLogQuery(query string) string {
	switch {
	case strings.Contains(query, "error OR"):
		return "errors"
	case strings.Contains(query, "timeout OR"):
		return "timeout_slow"
	case strings.Contains(query, "4bf92f"):
		return "trace"
	case strings.Contains(query, "order-db"):
		return "dependency_1"
	default:
		return "other"
	}
}
