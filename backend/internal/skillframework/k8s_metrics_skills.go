package skillframework

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	k8ssvc "aiops-platform/backend/internal/k8s"
	metricssvc "aiops-platform/backend/internal/metrics"
	"aiops-platform/backend/internal/model"
)

type K8sDiagnoser interface {
	DiagnosePod(ctx context.Context, actor *model.AppUser, input k8ssvc.PodDiagnosisInput) (*k8ssvc.PodDiagnosisResult, error)
	DiagnoseService(ctx context.Context, actor *model.AppUser, input k8ssvc.ServiceDiagnosisInput) (*k8ssvc.ServiceDiagnosisResult, error)
	Resources(ctx context.Context, actor *model.AppUser, input k8ssvc.ResourceInput) (*k8ssvc.ResourceResult, error)
}

type MetricsQuerier interface {
	Query(ctx context.Context, actor *model.AppUser, input metricssvc.QueryInput) (*metricssvc.QueryResult, error)
}

func K8sAndMetricsSkills(k8s K8sDiagnoser, metrics MetricsQuerier) []Skill {
	return []Skill{
		GetPodContextSkill{k8s: k8s},
		GetServiceContextSkill{k8s: k8s},
		GetIngressContextSkill{k8s: k8s},
		RunK8sDiagnosticRulesSkill{k8s: k8s},
		QueryMetricsSkill{metrics: metrics},
		CompareMetricBaselineSkill{metrics: metrics},
	}
}

type GetServiceContextSkill struct {
	k8s K8sDiagnoser
}

func (s GetServiceContextSkill) Definition() SkillDefinition {
	return SkillDefinition{
		Name:          "get_service_context",
		Version:       "v1",
		Description:   "Collect Kubernetes service context including selected pods, Endpoints, EndpointSlices, ingress backends and diagnostic rules.",
		InputSchema:   json.RawMessage(`{"type":"object","required":["dataSourceId","namespace","serviceName"],"properties":{"dataSourceId":{"type":"integer"},"namespace":{"type":"string"},"serviceName":{"type":"string"}}}`),
		OutputSchema:  partialOutputSchema(),
		RiskLevel:     model.SkillRiskSensitiveRead,
		ReadOnly:      true,
		TimeoutSecond: 30,
	}
}

func (s GetServiceContextSkill) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var request struct {
		DataSourceID int64  `json:"dataSourceId"`
		Namespace    string `json:"namespace"`
		ServiceName  string `json:"serviceName"`
	}
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, ErrInvalidInput
	}
	if s.k8s == nil {
		return partialError("kubernetes", "k8s service is not configured"), nil
	}
	result, err := s.k8s.DiagnoseService(ctx, ActorFromContext(ctx), k8ssvc.ServiceDiagnosisInput{
		DataSourceID: request.DataSourceID,
		Namespace:    request.Namespace,
		ServiceName:  request.ServiceName,
	})
	if err != nil {
		return partialError("kubernetes", err.Error()), nil
	}
	return json.Marshal(map[string]any{"partial": false, "serviceContext": result})
}

type GetPodContextSkill struct {
	k8s K8sDiagnoser
}

func (s GetPodContextSkill) Definition() SkillDefinition {
	return SkillDefinition{
		Name:          "get_pod_context",
		Version:       "v1",
		Description:   "Collect Kubernetes pod context including events, logs, owner, service and endpoint.",
		InputSchema:   json.RawMessage(`{"type":"object","required":["dataSourceId","namespace","podName"],"properties":{"dataSourceId":{"type":"integer"},"namespace":{"type":"string"},"podName":{"type":"string"},"includeNode":{"type":"boolean"},"includePreviousLogs":{"type":"boolean"},"logTailLines":{"type":"integer"},"logMaxBytes":{"type":"integer"}}}`),
		OutputSchema:  partialOutputSchema(),
		RiskLevel:     model.SkillRiskSensitiveRead,
		ReadOnly:      true,
		TimeoutSecond: 20,
		RequiredTools: []string{"kubernetes"},
	}
}

func (s GetPodContextSkill) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var request struct {
		DataSourceID        int64  `json:"dataSourceId"`
		Namespace           string `json:"namespace"`
		PodName             string `json:"podName"`
		IncludeNode         bool   `json:"includeNode"`
		IncludePreviousLogs bool   `json:"includePreviousLogs"`
		LogTailLines        int    `json:"logTailLines"`
		LogMaxBytes         int    `json:"logMaxBytes"`
	}
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, ErrInvalidInput
	}
	if s.k8s == nil {
		return partialError("kubernetes", "k8s service is not configured"), nil
	}
	result, err := s.k8s.DiagnosePod(ctx, ActorFromContext(ctx), k8ssvc.PodDiagnosisInput{
		DataSourceID:        request.DataSourceID,
		Namespace:           request.Namespace,
		PodName:             request.PodName,
		IncludeNode:         request.IncludeNode,
		IncludePreviousLogs: request.IncludePreviousLogs,
		LogTailLines:        request.LogTailLines,
		LogMaxBytes:         request.LogMaxBytes,
	})
	if err != nil {
		return partialError("kubernetes", err.Error()), nil
	}
	return json.Marshal(map[string]any{"partial": false, "podContext": result})
}

type GetIngressContextSkill struct {
	k8s K8sDiagnoser
}

func (s GetIngressContextSkill) Definition() SkillDefinition {
	return SkillDefinition{
		Name:          "get_ingress_context",
		Version:       "v1",
		Description:   "Read Kubernetes ingress resources in an allowed namespace.",
		InputSchema:   json.RawMessage(`{"type":"object","required":["dataSourceId","namespace"],"properties":{"dataSourceId":{"type":"integer"},"namespace":{"type":"string"},"name":{"type":"string"},"limit":{"type":"integer"}}}`),
		OutputSchema:  partialOutputSchema(),
		RiskLevel:     model.SkillRiskSensitiveRead,
		ReadOnly:      true,
		TimeoutSecond: 15,
		RequiredTools: []string{"kubernetes"},
	}
}

func (s GetIngressContextSkill) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var request struct {
		DataSourceID int64  `json:"dataSourceId"`
		Namespace    string `json:"namespace"`
		Name         string `json:"name"`
		Limit        int    `json:"limit"`
	}
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, ErrInvalidInput
	}
	if s.k8s == nil {
		return partialError("kubernetes", "k8s service is not configured"), nil
	}
	result, err := s.k8s.Resources(ctx, ActorFromContext(ctx), k8ssvc.ResourceInput{
		DataSourceID: request.DataSourceID,
		Namespace:    request.Namespace,
		Resource:     "ingresses",
		Name:         request.Name,
		Limit:        request.Limit,
	})
	if err != nil {
		return partialError("kubernetes", err.Error()), nil
	}
	return json.Marshal(map[string]any{"partial": false, "ingresses": result.Items})
}

type RunK8sDiagnosticRulesSkill struct {
	k8s K8sDiagnoser
}

func (s RunK8sDiagnosticRulesSkill) Definition() SkillDefinition {
	return SkillDefinition{
		Name:          "run_k8s_diagnostic_rules",
		Version:       "v1",
		Description:   "Run deterministic Kubernetes diagnostic rules from collected pod context.",
		InputSchema:   json.RawMessage(`{"type":"object","required":["dataSourceId","namespace","podName"],"properties":{"dataSourceId":{"type":"integer"},"namespace":{"type":"string"},"podName":{"type":"string"},"includePreviousLogs":{"type":"boolean"},"logTailLines":{"type":"integer"},"logMaxBytes":{"type":"integer"}}}`),
		OutputSchema:  partialOutputSchema(),
		RiskLevel:     model.SkillRiskSensitiveRead,
		ReadOnly:      true,
		TimeoutSecond: 20,
		RequiredTools: []string{"kubernetes"},
	}
}

func (s RunK8sDiagnosticRulesSkill) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var request struct {
		DataSourceID        int64  `json:"dataSourceId"`
		Namespace           string `json:"namespace"`
		PodName             string `json:"podName"`
		IncludePreviousLogs bool   `json:"includePreviousLogs"`
		LogTailLines        int    `json:"logTailLines"`
		LogMaxBytes         int    `json:"logMaxBytes"`
	}
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, ErrInvalidInput
	}
	if s.k8s == nil {
		return partialError("kubernetes", "k8s service is not configured"), nil
	}
	result, err := s.k8s.DiagnosePod(ctx, ActorFromContext(ctx), k8ssvc.PodDiagnosisInput{
		DataSourceID:        request.DataSourceID,
		Namespace:           request.Namespace,
		PodName:             request.PodName,
		IncludePreviousLogs: request.IncludePreviousLogs,
		LogTailLines:        request.LogTailLines,
		LogMaxBytes:         request.LogMaxBytes,
	})
	if err != nil {
		return partialError("kubernetes", err.Error()), nil
	}
	rules := k8ssvc.EvaluatePodRules(result)
	return json.Marshal(map[string]any{"partial": false, "rules": rules, "pod": result.Pod})
}

type QueryMetricsSkill struct {
	metrics MetricsQuerier
}

func (s QueryMetricsSkill) Definition() SkillDefinition {
	return SkillDefinition{
		Name:          "query_metrics",
		Version:       "v1",
		Description:   "Query Prometheus metrics through instant or range query.",
		InputSchema:   metricQueryInputSchema(),
		OutputSchema:  partialOutputSchema(),
		RiskLevel:     model.SkillRiskSensitiveRead,
		ReadOnly:      true,
		TimeoutSecond: 20,
		RequiredTools: []string{"prometheus"},
	}
}

func (s QueryMetricsSkill) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	request, err := parseMetricQueryRequest(input)
	if err != nil {
		return nil, err
	}
	if s.metrics == nil {
		return partialError("prometheus", "metrics service is not configured"), nil
	}
	result, err := s.metrics.Query(ctx, ActorFromContext(ctx), request)
	if err != nil {
		return partialError("prometheus", err.Error()), nil
	}
	return json.Marshal(map[string]any{"partial": false, "metrics": result})
}

type CompareMetricBaselineSkill struct {
	metrics MetricsQuerier
}

func (s CompareMetricBaselineSkill) Definition() SkillDefinition {
	return SkillDefinition{
		Name:          "compare_metric_baseline",
		Version:       "v1",
		Description:   "Compare current Prometheus query series with a baseline time window.",
		InputSchema:   json.RawMessage(`{"type":"object","required":["dataSourceId","query","currentStart","currentEnd","baselineStart","baselineEnd"],"properties":{"dataSourceId":{"type":"integer"},"query":{"type":"string"},"currentStart":{"type":"string"},"currentEnd":{"type":"string"},"baselineStart":{"type":"string"},"baselineEnd":{"type":"string"},"stepSeconds":{"type":"integer"},"maxSeries":{"type":"integer"},"maxPoints":{"type":"integer"}}}`),
		OutputSchema:  partialOutputSchema(),
		RiskLevel:     model.SkillRiskSensitiveRead,
		ReadOnly:      true,
		TimeoutSecond: 30,
		RequiredTools: []string{"prometheus"},
	}
}

func (s CompareMetricBaselineSkill) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var request struct {
		DataSourceID  int64  `json:"dataSourceId"`
		Query         string `json:"query"`
		CurrentStart  string `json:"currentStart"`
		CurrentEnd    string `json:"currentEnd"`
		BaselineStart string `json:"baselineStart"`
		BaselineEnd   string `json:"baselineEnd"`
		StepSeconds   int    `json:"stepSeconds"`
		MaxSeries     int    `json:"maxSeries"`
		MaxPoints     int    `json:"maxPoints"`
	}
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, ErrInvalidInput
	}
	currentStart, err := parseSkillTime(request.CurrentStart)
	if err != nil {
		return nil, ErrInvalidInput
	}
	currentEnd, err := parseSkillTime(request.CurrentEnd)
	if err != nil {
		return nil, ErrInvalidInput
	}
	baselineStart, err := parseSkillTime(request.BaselineStart)
	if err != nil {
		return nil, ErrInvalidInput
	}
	baselineEnd, err := parseSkillTime(request.BaselineEnd)
	if err != nil {
		return nil, ErrInvalidInput
	}
	if s.metrics == nil {
		return partialError("prometheus", "metrics service is not configured"), nil
	}
	base := metricssvc.QueryInput{
		DataSourceID: request.DataSourceID,
		Query:        strings.TrimSpace(request.Query),
		Range:        true,
		Step:         time.Duration(request.StepSeconds) * time.Second,
		MaxSeries:    request.MaxSeries,
		MaxPoints:    request.MaxPoints,
	}
	currentInput := base
	currentInput.Start = currentStart
	currentInput.End = currentEnd
	baselineInput := base
	baselineInput.Start = baselineStart
	baselineInput.End = baselineEnd
	current, err := s.metrics.Query(ctx, ActorFromContext(ctx), currentInput)
	if err != nil {
		return partialError("prometheus", err.Error()), nil
	}
	baseline, err := s.metrics.Query(ctx, ActorFromContext(ctx), baselineInput)
	if err != nil {
		return json.Marshal(map[string]any{
			"partial": true,
			"error":   map[string]string{"source": "prometheus", "message": err.Error(), "stage": "baseline"},
			"current": current,
		})
	}
	return json.Marshal(map[string]any{
		"partial":  false,
		"current":  current,
		"baseline": baseline,
		"summary":  compareMetricSeries(current.Series, baseline.Series),
	})
}

func parseMetricQueryRequest(input json.RawMessage) (metricssvc.QueryInput, error) {
	var request struct {
		DataSourceID int64  `json:"dataSourceId"`
		Query        string `json:"query"`
		Range        bool   `json:"range"`
		Start        string `json:"start"`
		End          string `json:"end"`
		StepSeconds  int    `json:"stepSeconds"`
		MaxSeries    int    `json:"maxSeries"`
		MaxPoints    int    `json:"maxPoints"`
	}
	if err := json.Unmarshal(input, &request); err != nil {
		return metricssvc.QueryInput{}, ErrInvalidInput
	}
	result := metricssvc.QueryInput{
		DataSourceID: request.DataSourceID,
		Query:        request.Query,
		Range:        request.Range,
		Step:         time.Duration(request.StepSeconds) * time.Second,
		MaxSeries:    request.MaxSeries,
		MaxPoints:    request.MaxPoints,
	}
	if request.Start != "" {
		start, err := parseSkillTime(request.Start)
		if err != nil {
			return metricssvc.QueryInput{}, ErrInvalidInput
		}
		result.Start = start
	}
	if request.End != "" {
		end, err := parseSkillTime(request.End)
		if err != nil {
			return metricssvc.QueryInput{}, ErrInvalidInput
		}
		result.End = end
	}
	return result, nil
}

func compareMetricSeries(current []metricssvc.MetricSeries, baseline []metricssvc.MetricSeries) map[string]any {
	currentAvg := averageSeries(current)
	baselineAvg := averageSeries(baseline)
	delta := currentAvg - baselineAvg
	percent := 0.0
	if baselineAvg != 0 {
		percent = delta / baselineAvg * 100
	}
	return map[string]any{
		"currentAverage":  currentAvg,
		"baselineAverage": baselineAvg,
		"delta":           delta,
		"deltaPercent":    percent,
	}
}

func averageSeries(series []metricssvc.MetricSeries) float64 {
	total := 0.0
	count := 0
	for _, item := range series {
		for _, point := range item.Points {
			total += point.Value
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func partialOutputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","required":["partial"],"properties":{"partial":{"type":"boolean"},"error":{"type":"object"}}}`)
}

func metricQueryInputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","required":["dataSourceId","query"],"properties":{"dataSourceId":{"type":"integer"},"query":{"type":"string"},"range":{"type":"boolean"},"start":{"type":"string"},"end":{"type":"string"},"stepSeconds":{"type":"integer"},"maxSeries":{"type":"integer"},"maxPoints":{"type":"integer"}}}`)
}

func partialError(source string, message string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{
		"partial": true,
		"error": map[string]string{
			"source":  source,
			"message": message,
		},
	})
	return raw
}
