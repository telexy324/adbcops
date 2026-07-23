package skillframework

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	k8ssvc "aiops-platform/backend/internal/k8s"
	metricssvc "aiops-platform/backend/internal/metrics"
	"aiops-platform/backend/internal/model"
)

func TestGetPodContextSkillOutputMatchesSchema(t *testing.T) {
	skill := GetPodContextSkill{k8s: fakeK8sService{podResult: &k8ssvc.PodDiagnosisResult{
		DataSourceID: 1,
		Namespace:    "prod",
		Pod:          k8ssvc.PodSummary{Name: "api-0", Namespace: "prod", Phase: "Running"},
		Rules:        []k8ssvc.RuleFinding{},
	}}}

	output, err := skill.Execute(ContextWithActor(context.Background(), adminActor()), json.RawMessage(`{
		"dataSourceId": 1,
		"namespace": "prod",
		"podName": "api-0"
	}`))
	if err != nil {
		t.Fatalf("execute get_pod_context: %v", err)
	}
	if err := ValidateJSONSchema(skill.Definition().OutputSchema, output); err != nil {
		t.Fatalf("output schema mismatch: %v output=%s", err, output)
	}
	assertNotPartial(t, output)
}

func TestRunK8sDiagnosticRulesReturnsPartialErrorOnToolFailure(t *testing.T) {
	skill := RunK8sDiagnosticRulesSkill{k8s: fakeK8sService{err: errors.New("api server unavailable")}}

	output, err := skill.Execute(ContextWithActor(context.Background(), adminActor()), json.RawMessage(`{
		"dataSourceId": 1,
		"namespace": "prod",
		"podName": "api-0"
	}`))
	if err != nil {
		t.Fatalf("execute should return structured partial output, got error %v", err)
	}
	assertPartialError(t, output, "kubernetes")
}

func TestQueryMetricsSkillOutputMatchesSchema(t *testing.T) {
	skill := QueryMetricsSkill{metrics: &fakeMetricsService{results: []*metricssvc.QueryResult{metricResult(10, 20)}}}

	output, err := skill.Execute(ContextWithActor(context.Background(), adminActor()), json.RawMessage(`{
		"dataSourceId": 1,
		"query": "up",
		"range": false
	}`))
	if err != nil {
		t.Fatalf("execute query_metrics: %v", err)
	}
	if err := ValidateJSONSchema(skill.Definition().OutputSchema, output); err != nil {
		t.Fatalf("output schema mismatch: %v output=%s", err, output)
	}
	assertNotPartial(t, output)
}

func TestCompareMetricBaselineComputesSummary(t *testing.T) {
	skill := CompareMetricBaselineSkill{metrics: &fakeMetricsService{results: []*metricssvc.QueryResult{
		metricResult(20, 40),
		metricResult(10, 10),
	}}}

	output, err := skill.Execute(ContextWithActor(context.Background(), adminActor()), json.RawMessage(`{
		"dataSourceId": 1,
		"query": "rate(http_requests_total[5m])",
		"currentStart": "2026-07-12T10:00:00Z",
		"currentEnd": "2026-07-12T11:00:00Z",
		"baselineStart": "2026-07-11T10:00:00Z",
		"baselineEnd": "2026-07-11T11:00:00Z",
		"stepSeconds": 60
	}`))
	if err != nil {
		t.Fatalf("execute compare_metric_baseline: %v", err)
	}
	assertNotPartial(t, output)
	var decoded struct {
		Summary struct {
			CurrentAverage  float64 `json:"currentAverage"`
			BaselineAverage float64 `json:"baselineAverage"`
			Delta           float64 `json:"delta"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if decoded.Summary.CurrentAverage != 30 || decoded.Summary.BaselineAverage != 10 || decoded.Summary.Delta != 20 {
		t.Fatalf("unexpected summary: %+v", decoded.Summary)
	}
}

func TestCompareMetricBaselineReturnsPartialWhenBaselineFails(t *testing.T) {
	skill := CompareMetricBaselineSkill{metrics: &fakeMetricsService{
		results: []*metricssvc.QueryResult{metricResult(20, 40)},
		errAt:   2,
		err:     errors.New("prometheus timeout"),
	}}

	output, err := skill.Execute(ContextWithActor(context.Background(), adminActor()), json.RawMessage(`{
		"dataSourceId": 1,
		"query": "up",
		"currentStart": "2026-07-12T10:00:00Z",
		"currentEnd": "2026-07-12T11:00:00Z",
		"baselineStart": "2026-07-11T10:00:00Z",
		"baselineEnd": "2026-07-11T11:00:00Z"
	}`))
	if err != nil {
		t.Fatalf("execute should return partial output, got %v", err)
	}
	assertPartialError(t, output, "prometheus")
}

type fakeK8sService struct {
	podResult      *k8ssvc.PodDiagnosisResult
	resourceResult *k8ssvc.ResourceResult
	err            error
}

func (f fakeK8sService) DiagnosePod(context.Context, *model.AppUser, k8ssvc.PodDiagnosisInput) (*k8ssvc.PodDiagnosisResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.podResult, nil
}

func (f fakeK8sService) DiagnoseService(context.Context, *model.AppUser, k8ssvc.ServiceDiagnosisInput) (*k8ssvc.ServiceDiagnosisResult, error) {
	return &k8ssvc.ServiceDiagnosisResult{}, nil
}

func (f fakeK8sService) Resources(context.Context, *model.AppUser, k8ssvc.ResourceInput) (*k8ssvc.ResourceResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.resourceResult != nil {
		return f.resourceResult, nil
	}
	return &k8ssvc.ResourceResult{}, nil
}

type fakeMetricsService struct {
	results []*metricssvc.QueryResult
	calls   int
	errAt   int
	err     error
}

func (f *fakeMetricsService) Query(context.Context, *model.AppUser, metricssvc.QueryInput) (*metricssvc.QueryResult, error) {
	f.calls++
	if f.errAt == f.calls && f.err != nil {
		return nil, f.err
	}
	if f.calls-1 < len(f.results) {
		return f.results[f.calls-1], nil
	}
	return metricResult(1), nil
}

func metricResult(values ...float64) *metricssvc.QueryResult {
	points := make([]metricssvc.MetricPoint, 0, len(values))
	for index, value := range values {
		points = append(points, metricssvc.MetricPoint{
			Timestamp: time.Date(2026, 7, 12, 10, index, 0, 0, time.UTC),
			Value:     value,
			RawValue:  "value",
		})
	}
	return &metricssvc.QueryResult{
		DataSourceID: 1,
		Query:        "up",
		Series:       []metricssvc.MetricSeries{{Metric: map[string]string{"job": "api"}, Points: points}},
	}
}

func assertPartialError(t *testing.T, output json.RawMessage, source string) {
	t.Helper()
	var decoded struct {
		Partial bool `json:"partial"`
		Error   struct {
			Source string `json:"source"`
		} `json:"error"`
	}
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("decode partial output: %v", err)
	}
	if !decoded.Partial || decoded.Error.Source != source {
		t.Fatalf("unexpected partial output: %s", output)
	}
}

func assertNotPartial(t *testing.T, output json.RawMessage) {
	t.Helper()
	var decoded struct {
		Partial bool `json:"partial"`
	}
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if decoded.Partial {
		t.Fatalf("expected non-partial output, got %s", output)
	}
}
