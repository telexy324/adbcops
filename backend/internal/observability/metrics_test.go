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
