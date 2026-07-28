package observability

import (
	"strings"
	"testing"
	"time"
)

func TestRegistryWritesPrometheusText(t *testing.T) {
	registry := NewRegistry()
	registry.Inc("aiops_test_total", map[string]string{"status": "ok"})
	registry.Set("aiops_test_health", map[string]string{"source_type": "prometheus", "id": "1"}, 1)
	registry.Observe("aiops_test_duration_seconds", map[string]string{"status": "ok"}, (250 * time.Millisecond).Seconds())

	output := string(registry.WritePrometheus())
	for _, expected := range []string{
		`aiops_test_total{status="ok"} 1`,
		`aiops_test_health{id="1",source_type="prometheus"} 1`,
		`aiops_test_duration_seconds_count{status="ok"} 1`,
		`aiops_test_duration_seconds_sum{status="ok"} 0.25`,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("metrics output missing %q in:\n%s", expected, output)
		}
	}
}

func TestRCAMetricsExposeBoundedLowCardinalitySeries(t *testing.T) {
	previous := Default
	Default = NewRegistry()
	t.Cleanup(func() { Default = previous })

	ObserveRCARunCreated()
	AddActiveRCA("global", 1)
	ObserveRCARound(2, "partial_success", 250*time.Millisecond)
	ObserveRCAPlanner("success", true, 100*time.Millisecond)
	ObserveRCAAction("query_logs", "failed", "upstream_unavailable")
	ObserveRCAEvidence("fact", "log")
	ObserveRCAOrchestration("partial_success", "skill_call_budget_exhausted", time.Second)
	ObserveRCALimit("user")
	AddActiveRCA("global", -1)

	output := string(Default.WritePrometheus())
	for _, expected := range []string{
		"aiops_rca_runs_created_total 1",
		`aiops_rca_active_orchestrations{scope="global"} 0`,
		`aiops_rca_rounds_total{round="2",status="partial_success"} 1`,
		`aiops_rca_planner_runs_total{degraded="true",status="success"} 1`,
		`aiops_rca_actions_total{error_code="upstream_unavailable",skill="query_logs",status="failed"} 1`,
		`aiops_rca_evidence_total{kind="fact",source_type="log"} 1`,
		`aiops_rca_budget_stops_total{reason="skill_call_budget_exhausted"} 1`,
		`aiops_rca_limit_rejections_total{scope="user"} 1`,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("RCA metrics output missing %q in:\n%s", expected, output)
		}
	}
	for _, forbidden := range []string{"run_id", "user_id", "query="} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("high-cardinality or sensitive RCA label leaked: %q", forbidden)
		}
	}
}
