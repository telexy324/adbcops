package observability

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var Default = NewRegistry()

type Registry struct {
	mu         sync.Mutex
	counters   map[string]float64
	gauges     map[string]float64
	histograms map[string]histogramValue
}

type histogramValue struct {
	Count int64
	Sum   float64
}

func NewRegistry() *Registry {
	return &Registry{
		counters:   map[string]float64{},
		gauges:     map[string]float64{},
		histograms: map[string]histogramValue{},
	}
}

func (r *Registry) Inc(name string, labels map[string]string) {
	r.Add(name, labels, 1)
}

func (r *Registry) Add(name string, labels map[string]string, value float64) {
	if r == nil || name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters[seriesKey(name, labels)] += value
}

func (r *Registry) Set(name string, labels map[string]string, value float64) {
	if r == nil || name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gauges[seriesKey(name, labels)] = value
}

func (r *Registry) Observe(name string, labels map[string]string, value float64) {
	if r == nil || name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := seriesKey(name, labels)
	current := r.histograms[key]
	current.Count++
	current.Sum += value
	r.histograms[key] = current
}

func (r *Registry) WritePrometheus() []byte {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	var buffer bytes.Buffer
	writeMap(&buffer, r.counters)
	writeMap(&buffer, r.gauges)
	writeHistograms(&buffer, r.histograms)
	return buffer.Bytes()
}

func ObserveHTTPRequest(method, route string, status int, duration time.Duration) {
	if route == "" {
		route = "unknown"
	}
	statusClass := fmt.Sprintf("%dxx", status/100)
	labels := map[string]string{"method": method, "route": route, "status": strconv.Itoa(status), "status_class": statusClass}
	Default.Inc("aiops_http_requests_total", labels)
	Default.Observe("aiops_http_request_duration_seconds", labels, duration.Seconds())
}

func ObserveWorkflow(status string, duration time.Duration) {
	Default.Inc("aiops_workflow_runs_total", map[string]string{"status": status})
	Default.Observe("aiops_workflow_duration_seconds", map[string]string{"status": status}, duration.Seconds())
}

func ObserveAgent(agent, status string, duration time.Duration) {
	Default.Inc("aiops_agent_runs_total", map[string]string{"agent": agent, "status": status})
	Default.Observe("aiops_agent_latency_seconds", map[string]string{"agent": agent, "status": status}, duration.Seconds())
}

func ObserveSkill(skill, status string, duration time.Duration) {
	Default.Inc("aiops_skill_runs_total", map[string]string{"skill": skill, "status": status})
	if status != "success" {
		Default.Inc("aiops_skill_errors_total", map[string]string{"skill": skill})
	}
	Default.Observe("aiops_skill_latency_seconds", map[string]string{"skill": skill, "status": status}, duration.Seconds())
}

func ObserveTool(tool, operation string, err error) {
	status := "success"
	if err != nil {
		status = "error"
		Default.Inc("aiops_tool_errors_total", map[string]string{"tool": tool, "operation": operation})
	}
	Default.Inc("aiops_tool_operations_total", map[string]string{"tool": tool, "operation": operation, "status": status})
}

func ObserveLLM(model string, usagePrompt, usageCompletion, usageTotal int, err error, duration time.Duration) {
	status := "success"
	if err != nil {
		status = "error"
	}
	if model == "" {
		model = "unknown"
	}
	labels := map[string]string{"model": model, "status": status}
	Default.Inc("aiops_llm_requests_total", labels)
	Default.Add("aiops_llm_tokens_total", map[string]string{"model": model, "type": "prompt"}, float64(usagePrompt))
	Default.Add("aiops_llm_tokens_total", map[string]string{"model": model, "type": "completion"}, float64(usageCompletion))
	Default.Add("aiops_llm_tokens_total", map[string]string{"model": model, "type": "total"}, float64(usageTotal))
	Default.Observe("aiops_llm_latency_seconds", labels, duration.Seconds())
}

func SetDatasourceHealth(sourceType string, id int64, healthy bool) {
	value := 0.0
	if healthy {
		value = 1
	}
	Default.Set("aiops_datasource_health", map[string]string{"source_type": sourceType, "id": strconv.FormatInt(id, 10)}, value)
}

func writeMap(buffer *bytes.Buffer, values map[string]float64) {
	keys := sortedKeys(values)
	for _, key := range keys {
		name, labels := splitSeriesKey(key)
		buffer.WriteString(name)
		buffer.WriteString(labels)
		buffer.WriteByte(' ')
		buffer.WriteString(strconv.FormatFloat(values[key], 'f', -1, 64))
		buffer.WriteByte('\n')
	}
}

func writeHistograms(buffer *bytes.Buffer, values map[string]histogramValue) {
	keys := sortedKeys(values)
	for _, key := range keys {
		name, labels := splitSeriesKey(key)
		value := values[key]
		buffer.WriteString(name)
		buffer.WriteString("_count")
		buffer.WriteString(labels)
		buffer.WriteByte(' ')
		buffer.WriteString(strconv.FormatInt(value.Count, 10))
		buffer.WriteByte('\n')
		buffer.WriteString(name)
		buffer.WriteString("_sum")
		buffer.WriteString(labels)
		buffer.WriteByte(' ')
		buffer.WriteString(strconv.FormatFloat(value.Sum, 'f', -1, 64))
		buffer.WriteByte('\n')
	}
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func seriesKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+escapeLabel(labels[key]))
	}
	return name + "{" + strings.Join(parts, ",") + "}"
}

func splitSeriesKey(key string) (string, string) {
	index := strings.IndexByte(key, '{')
	if index < 0 {
		return key, ""
	}
	return key[:index], key[index:]
}

func escapeLabel(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}
