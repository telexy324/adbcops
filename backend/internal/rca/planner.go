package rca

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	datasourcesvc "aiops-platform/backend/internal/datasource"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/observability"
	"aiops-platform/backend/internal/repository"
	"aiops-platform/backend/internal/skillframework"
)

type PlannerSkillCatalog interface {
	Get(name string) (skillframework.SkillDefinition, error)
	List() []skillframework.SkillDefinition
}

func (s *Service) WithPlanner(catalog PlannerSkillCatalog, plannerModel PlannerModel) *Service {
	s.skillCatalog = catalog
	s.plannerModel = plannerModel
	return s
}

func (s *Service) PlanNext(ctx context.Context, actor *model.AppUser, runID int64, request PlanRequest) (planned *PlannerResult, resultErr error) {
	startedAt := time.Now()
	defer func() {
		status, degraded, actionCount, hypothesisCount := "error", false, 0, 0
		if planned != nil {
			status, degraded = "success", planned.PlannerDegraded
			actionCount, hypothesisCount = len(planned.NextActions), len(planned.Hypotheses)
		}
		observability.ObserveRCAPlanner(status, degraded, time.Since(startedAt))
		slog.InfoContext(ctx, "rca planner completed",
			"rca_run_id", runID, "status", status, "degraded", degraded,
			"action_count", actionCount, "hypothesis_count", hypothesisCount,
			"duration_ms", time.Since(startedAt).Milliseconds(), "error", safeRCAErrorCode(resultErr),
		)
	}()
	if actor == nil || runID <= 0 || s.skillCatalog == nil {
		return nil, ErrInvalidInput
	}
	detail, err := s.GetDetail(ctx, actor, runID)
	if err != nil {
		return nil, err
	}
	if terminalRunStatus(detail.Run.Status) || detail.Run.FinishedAt != nil || len(detail.Rounds) == 0 {
		return nil, ErrInvalidTransition
	}
	input := buildPlannerInput(detail, request)
	allowedSources, err := s.accessiblePlannerDataSources(ctx, actor)
	if err != nil {
		return nil, err
	}
	result := deterministicPlan(input, allowedSources)
	useLLM := request.UseLLM == nil || *request.UseLLM
	if useLLM {
		if s.plannerModel == nil {
			result.PlannerDegraded = true
			result.DegradationReason = "llm planner unavailable; deterministic rules used"
		} else {
			raw, modelErr := s.plannerModel.Plan(ctx, PlannerModelRequest{
				PromptVersion: PlannerPromptVersion, SystemPrompt: PlannerSystemPrompt,
				OutputSchema: PlannerOutputSchema, Input: input,
				Skills: plannerSkillSpecs(s.skillCatalog.List(), actor), Deterministic: result,
			})
			if modelErr != nil {
				result.PlannerDegraded = true
				result.DegradationReason = "llm planner failed; deterministic rules used"
			} else if candidate, parseErr := parsePlannerResult(raw); parseErr != nil {
				result.PlannerDegraded = true
				result.DegradationReason = "llm planner returned invalid output; deterministic rules used"
			} else {
				result = mergePlannerResults(input, result, *candidate)
			}
		}
	}
	result = s.validatePlannerResult(actor, input, result, allowedSources)
	if err := s.persistPlannerResult(ctx, actor, detail, result); err != nil {
		return nil, err
	}
	return &result, nil
}

func buildPlannerInput(detail *Detail, request PlanRequest) PlannerInput {
	budget := request.Budget
	if budget.RemainingRounds == 0 && budget.RemainingSkillCalls == 0 && budget.RemainingWallTimeSeconds == 0 {
		budget.RemainingRounds = detail.Run.MaxRounds - detail.Run.CurrentRound
		if budget.RemainingRounds < 1 {
			budget.RemainingRounds = 1
		}
		budget.RemainingSkillCalls = 8
		budget.RemainingWallTimeSeconds = 120
	}
	history := make([]PlannerRoundHistory, 0, len(detail.Rounds))
	for _, round := range detail.Rounds {
		var evidenceIDs []int64
		var nextActions []NextAction
		_ = json.Unmarshal(round.NewEvidenceIDs, &evidenceIDs)
		_ = json.Unmarshal(round.NextActions, &nextActions)
		history = append(history, PlannerRoundHistory{RoundNumber: round.RoundNumber, Status: round.Status, NewEvidenceIDs: evidenceIDs, NextActions: nextActions})
	}
	evidence := make([]PlannerEvidence, 0, len(detail.Evidence))
	for _, record := range detail.Evidence {
		evidence = append(evidence, PlannerEvidence{
			ID: record.ID, Kind: plannerString(record.EvidenceKind), SourceType: record.SourceType,
			Summary: truncateText(record.Summary, 1024), Signals: compactPlannerSignals(record.Content),
			Entity: sanitizeJSON(record.Entity, 4096, nil), SourceSkill: plannerString(record.SourceSkill),
			DataSourceID: record.DataSourceID, WindowStart: record.WindowStart, WindowEnd: record.WindowEnd,
		})
	}
	existing := append([]PlannerHypothesis{}, request.ExistingHypotheses...)
	for _, candidate := range detail.Candidates {
		var ids []int64
		_ = json.Unmarshal(candidate.EvidenceIDs, &ids)
		hypothesisID := plannerHypothesisID(candidate.Summary)
		if hypothesisID == "" {
			hypothesisID = "candidate-" + strconv.FormatInt(candidate.ID, 10)
		}
		existing = append(existing, PlannerHypothesis{
			ID: hypothesisID, Summary: candidate.Summary,
			Confidence: candidate.Confidence, SupportingEvidenceIDs: ids,
		})
	}
	actionKeys := make([]string, 0, len(detail.Actions))
	for _, action := range detail.Actions {
		actionKeys = append(actionKeys, actionSignature(action.SkillName, action.Input))
	}
	return PlannerInput{
		Version: PlannerVersion, RunID: detail.Run.ID, Round: detail.Run.CurrentRound,
		Query: detail.Run.Query, Scope: detail.Run.Scope, History: history, Evidence: evidence,
		ExistingHypotheses: existing, CompletedActionKeys: actionKeys, Budget: budget,
	}
}

func plannerHypothesisID(summary string) string {
	value := strings.ToLower(summary)
	switch {
	case strings.Contains(value, "数据库") || strings.Contains(value, "slow sql"):
		return "database-latency"
	case strings.Contains(value, "redis"):
		return "redis-latency"
	case strings.Contains(value, "nginx"):
		return "nginx-upstream"
	case strings.Contains(value, "kubernetes"):
		return "k8s-pressure"
	case strings.Contains(value, "cpu"):
		return "linux-cpu"
	case strings.Contains(value, "内存") || strings.Contains(value, "memory"):
		return "linux-memory"
	case strings.Contains(value, "磁盘") || strings.Contains(value, "disk"):
		return "linux-disk-io"
	case strings.Contains(value, "网络") || strings.Contains(value, "network"):
		return "linux-network"
	case strings.Contains(value, "下游") || strings.Contains(value, "downstream"):
		return "downstream-latency"
	default:
		return ""
	}
}

func plannerString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func compactPlannerSignals(content json.RawMessage) json.RawMessage {
	if len(content) == 0 || !json.Valid(content) {
		return nil
	}
	var value any
	if json.Unmarshal(content, &value) != nil {
		return nil
	}
	redacted := compactSignalValue(value, 0)
	raw, _ := json.Marshal(redacted)
	return sanitizeJSON(raw, 4096, nil)
}

func compactSignalValue(value any, depth int) any {
	if depth > 3 {
		return nil
	}
	switch typed := value.(type) {
	case map[string]any:
		result := map[string]any{}
		for key, item := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "credential") || strings.Contains(lower, "raw") || strings.Contains(lower, "querytext") {
				continue
			}
			switch lower {
			case "summary", "durationms", "latencyms", "deltapercent", "value", "baseline", "status", "severity", "metric", "name", "count", "partial", "facts", "findings", "items", "message", "templateclusters", "template":
				result[key] = compactSignalValue(item, depth+1)
			}
		}
		return result
	case []any:
		if len(typed) > 8 {
			typed = typed[:8]
		}
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, compactSignalValue(item, depth+1))
		}
		return result
	case string:
		return truncateText(typed, 512)
	case float64, bool, nil:
		return typed
	default:
		return nil
	}
}

type plannerScope map[string]any

func decodePlannerScope(raw json.RawMessage) plannerScope {
	var scope plannerScope
	if json.Unmarshal(raw, &scope) != nil {
		return plannerScope{}
	}
	return scope
}

func (s plannerScope) string(keys ...string) string {
	for _, key := range keys {
		if value, ok := s[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (s plannerScope) int64(keys ...string) int64 {
	for _, key := range keys {
		switch value := s[key].(type) {
		case float64:
			return int64(value)
		case string:
			parsed, _ := strconv.ParseInt(value, 10, 64)
			if parsed > 0 {
				return parsed
			}
		}
	}
	return 0
}

type plannerRule struct {
	id          string
	summary     string
	keywords    []string
	normalTerms []string
	confidence  float64
	actions     func(plannerScope) ([]PlannerAction, []string)
}

func deterministicPlan(input PlannerInput, sources map[int64]datasourcesvc.DataSourceView) PlannerResult {
	scope := decodePlannerScope(input.Scope)
	existingByID := map[string]PlannerHypothesis{}
	for _, item := range input.ExistingHypotheses {
		existingByID[item.ID] = item
	}
	result := PlannerResult{
		Version: PlannerVersion, PromptVersion: PlannerPromptVersion, SchemaVersion: PlannerSchemaVersion,
		Round: input.Round + 1, Hypotheses: []PlannerHypothesis{}, MissingEvidence: []string{}, NextActions: []PlannerAction{},
	}
	if input.Budget.RemainingRounds <= 0 || input.Budget.RemainingSkillCalls <= 0 || input.Budget.RemainingWallTimeSeconds <= 0 {
		result.ShouldStop, result.StopReason = true, "budget_exhausted"
		return result
	}
	rules := []plannerRule{
		{id: "database-latency", summary: "服务依赖的数据库调用耗时异常或存在慢 SQL", keywords: []string{"database", "数据库", "slow sql", "慢sql", "慢 sql", "db call", "jdbc", "tidb", "mysql", "sql duration"}, normalTerms: []string{"database", "db", "sql"}, confidence: .78, actions: databasePlannerActions},
		{id: "redis-latency", summary: "Redis 延迟或连接池压力导致请求排队", keywords: []string{"redis", "缓存", "connection pool", "连接池"}, normalTerms: []string{"redis", "cache", "连接池"}, confidence: .72, actions: redisPlannerActions},
		{id: "nginx-upstream", summary: "Nginx 上游超时或后端健康异常", keywords: []string{"nginx", "upstream", "504", "502"}, normalTerms: []string{"nginx", "upstream"}, confidence: .73, actions: nginxPlannerActions},
		{id: "k8s-pressure", summary: "Kubernetes 工作负载存在资源压力、重启或调度异常", keywords: []string{"kubernetes", "k8s", "pod", "oom", "crashloop", "throttl", "重启", "资源压力"}, normalTerms: []string{"pod", "k8s", "cpu", "memory", "内存"}, confidence: .68, actions: k8sPlannerActions},
		{id: "linux-cpu", summary: "Linux 主机 CPU 压力导致服务处理变慢", keywords: []string{"cpu", "load average", "负载"}, normalTerms: []string{"cpu", "load", "负载"}, confidence: .66, actions: linuxAction("diagnose_linux_cpu_pressure")},
		{id: "linux-memory", summary: "Linux 主机内存或交换压力导致服务抖动", keywords: []string{"memory", "swap", "内存"}, normalTerms: []string{"memory", "swap", "内存"}, confidence: .66, actions: linuxAction("diagnose_linux_memory_pressure")},
		{id: "linux-disk-io", summary: "Linux 磁盘 IO 压力导致请求延迟", keywords: []string{"disk io", "iowait", "磁盘", "io wait"}, normalTerms: []string{"disk", "iowait", "磁盘"}, confidence: .67, actions: linuxAction("diagnose_linux_disk_io")},
		{id: "linux-network", summary: "Linux 网络异常导致依赖调用延迟", keywords: []string{"network", "tcp retrans", "packet loss", "网络", "丢包"}, normalTerms: []string{"network", "网络", "丢包"}, confidence: .65, actions: linuxAction("diagnose_linux_network")},
		{id: "downstream-latency", summary: "下游服务调用延迟正在放大订单服务响应时间", keywords: []string{"downstream", "rpc", "http client", "下游", "调用耗时", "call took"}, normalTerms: []string{"downstream", "rpc", "下游"}, confidence: .64, actions: topologyPlannerActions},
	}
	for _, rule := range rules {
		support, contradict := evidenceForRule(input.Evidence, rule)
		previous, previouslyExists := existingByID[rule.id]
		if len(support) == 0 && !(previouslyExists && len(contradict) > 0) {
			continue
		}
		confidence := rule.confidence
		if previouslyExists {
			confidence = previous.Confidence
		}
		support = uniqueIDs(append(previous.SupportingEvidenceIDs, support...))
		if len(contradict) > 0 {
			confidence -= .18
			if confidence < 0 {
				confidence = 0
			}
		}
		hypothesis := PlannerHypothesis{
			ID: rule.id, Summary: rule.summary, Confidence: confidence,
			SupportingEvidenceIDs: support, ContradictingEvidenceIDs: contradict,
			Rationale: "由结构化证据触发的受控诊断规则",
		}
		result.Hypotheses = append(result.Hypotheses, hypothesis)
		actions, missing := rule.actions(scope)
		result.NextActions = append(result.NextActions, actions...)
		result.MissingEvidence = append(result.MissingEvidence, missing...)
	}
	result.Hypotheses = mergeExistingHypotheses(input.ExistingHypotheses, result.Hypotheses)
	if len(result.Hypotheses) == 0 && len(input.Evidence) > 0 {
		ids := []int64{input.Evidence[0].ID}
		result.Hypotheses = append(result.Hypotheses, PlannerHypothesis{ID: "unclassified-anomaly", Summary: "已发现异常证据，但尚需结合拓扑定位具体依赖", Confidence: .35, SupportingEvidenceIDs: ids, Rationale: "证据未命中受控组件规则"})
		actions, missing := topologyPlannerActions(scope)
		result.NextActions = append(result.NextActions, actions...)
		result.MissingEvidence = append(result.MissingEvidence, missing...)
	}
	_ = sources
	return result
}

func evidenceForRule(evidence []PlannerEvidence, rule plannerRule) ([]int64, []int64) {
	support, contradict := []int64{}, []int64{}
	for _, item := range evidence {
		text := strings.ToLower(item.Summary + " " + string(item.Signals))
		if !containsAny(text, rule.keywords) {
			continue
		}
		if evidenceLooksNormal(text, item.Signals) && containsAny(text, rule.normalTerms) {
			contradict = append(contradict, item.ID)
		} else {
			support = append(support, item.ID)
		}
	}
	return uniqueIDs(support), uniqueIDs(contradict)
}

func evidenceLooksNormal(text string, signals json.RawMessage) bool {
	if containsAny(text, []string{"正常", "无异常", "within baseline", "normal", "healthy", "no anomaly"}) {
		return true
	}
	var value map[string]any
	if json.Unmarshal(signals, &value) == nil {
		if summary, ok := value["summary"].(map[string]any); ok {
			if delta, ok := summary["deltaPercent"].(float64); ok && delta > -15 && delta < 15 {
				return true
			}
		}
		if delta, ok := value["deltaPercent"].(float64); ok && delta > -15 && delta < 15 {
			return true
		}
	}
	return false
}

func databasePlannerActions(scope plannerScope) ([]PlannerAction, []string) {
	actions, missing := topologyPlannerActions(scope)
	id := scope.int64("tidbDataSourceId", "databaseDataSourceId", "dataSourceId")
	if id > 0 {
		actions = append(actions, plannerAction("query_tidb_slow_queries", map[string]any{"dataSourceId": id, "minutes": 30, "limit": 20}, "查询疑似数据库依赖的慢 SQL 证据", "database"))
	} else {
		missing = append(missing, "需要从拓扑绑定确认可访问的 TiDB 数据源")
	}
	return actions, missing
}

func redisPlannerActions(scope plannerScope) ([]PlannerAction, []string) {
	id := scope.int64("redisDataSourceId", "dataSourceId")
	if id == 0 {
		return nil, []string{"需要确认可访问的 Redis 数据源"}
	}
	return []PlannerAction{
		plannerAction("query_redis_latency", map[string]any{"dataSourceId": id}, "确认 Redis 延迟事件", "redis"),
		plannerAction("diagnose_redis_connection_pool", map[string]any{"dataSourceId": id}, "确认客户端连接池压力", "redis"),
	}, nil
}

func nginxPlannerActions(scope plannerScope) ([]PlannerAction, []string) {
	id := scope.int64("nginxDataSourceId", "dataSourceId")
	if id == 0 {
		return nil, []string{"需要确认可访问的 Nginx 数据源"}
	}
	return []PlannerAction{
		plannerAction("diagnose_nginx_504", map[string]any{"dataSourceId": id, "limit": 100}, "核对上游超时与 504 证据", "nginx"),
		plannerAction("diagnose_nginx_upstream", map[string]any{"dataSourceId": id, "limit": 100}, "核对上游健康状态", "nginx"),
	}, nil
}

func k8sPlannerActions(scope plannerScope) ([]PlannerAction, []string) {
	id := scope.int64("kubernetesDataSourceId", "k8sDataSourceId", "dataSourceId")
	namespace, pod := scope.string("namespace"), scope.string("podName", "pod")
	missing := []string{}
	if id == 0 {
		missing = append(missing, "需要确认可访问的 Kubernetes 数据源")
	}
	if namespace == "" || pod == "" {
		missing = append(missing, "需要从拓扑确认 namespace 与 podName")
	}
	if id == 0 || namespace == "" || pod == "" {
		return nil, missing
	}
	return []PlannerAction{plannerAction("run_k8s_diagnostic_rules", map[string]any{"dataSourceId": id, "namespace": namespace, "podName": pod, "logTailLines": 200}, "验证 Pod 资源、重启与事件异常", pod)}, missing
}

func linuxAction(skill string) func(plannerScope) ([]PlannerAction, []string) {
	return func(scope plannerScope) ([]PlannerAction, []string) {
		hostID := scope.int64("linuxHostId", "hostId")
		if hostID == 0 {
			return nil, []string{"需要从拓扑确认 Linux hostId"}
		}
		return []PlannerAction{plannerAction(skill, map[string]any{"hostId": hostID, "topN": 10}, "验证主机层异常", strconv.FormatInt(hostID, 10))}, nil
	}
}

func topologyPlannerActions(scope plannerScope) ([]PlannerAction, []string) {
	environment := scope.string("environment")
	nodeKey := scope.string("topologyNodeKey", "nodeKey")
	service := scope.string("serviceName", "service", "systemName")
	actions, missing := []PlannerAction{}, []string{}
	if service != "" {
		input := map[string]any{"query": service, "limit": 20}
		if environment != "" {
			input["environment"] = environment
		}
		actions = append(actions, plannerAction("find_topology_node", input, "定位问题服务的拓扑节点", service))
	}
	if nodeKey != "" {
		input := map[string]any{"nodeKey": nodeKey, "direction": "downstream", "depth": 2, "maxNodes": 50, "maxEdges": 100}
		if environment != "" {
			input["environment"] = environment
		}
		actions = append(actions, plannerAction("find_dependencies", input, "沿拓扑查询可能涉及的下游组件", nodeKey))
	} else {
		missing = append(missing, "需要确认唯一的 topologyNodeKey 后扩展下游依赖")
	}
	if service == "" && nodeKey == "" {
		missing = append(missing, "需要提供 serviceName 或 topologyNodeKey")
	}
	return actions, missing
}

func plannerAction(skill string, input map[string]any, reason, target string) PlannerAction {
	raw, _ := json.Marshal(input)
	hash := sha256.Sum256([]byte(skill + "\x00" + string(raw)))
	return PlannerAction{ActionKey: "planner:" + skill + ":" + hex.EncodeToString(hash[:6]), SkillName: skill, Input: raw, Reason: reason, TargetEntity: target}
}

func mergeExistingHypotheses(existing, inferred []PlannerHypothesis) []PlannerHypothesis {
	byID := map[string]PlannerHypothesis{}
	for _, item := range existing {
		item.SupportingEvidenceIDs = uniqueIDs(item.SupportingEvidenceIDs)
		item.ContradictingEvidenceIDs = uniqueIDs(item.ContradictingEvidenceIDs)
		byID[item.ID] = item
	}
	for _, item := range inferred {
		if old, ok := byID[item.ID]; ok {
			newSupport := difference(item.SupportingEvidenceIDs, append(old.SupportingEvidenceIDs, old.ContradictingEvidenceIDs...))
			newContradict := difference(item.ContradictingEvidenceIDs, append(old.SupportingEvidenceIDs, old.ContradictingEvidenceIDs...))
			if len(newSupport) == 0 && len(newContradict) == 0 {
				item.Confidence = old.Confidence
			}
			item.SupportingEvidenceIDs = uniqueIDs(append(old.SupportingEvidenceIDs, item.SupportingEvidenceIDs...))
			item.ContradictingEvidenceIDs = uniqueIDs(append(old.ContradictingEvidenceIDs, item.ContradictingEvidenceIDs...))
		}
		byID[item.ID] = item
	}
	result := make([]PlannerHypothesis, 0, len(byID))
	for _, item := range byID {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Confidence == result[j].Confidence {
			return result[i].ID < result[j].ID
		}
		return result[i].Confidence > result[j].Confidence
	})
	return result
}

func mergePlannerResults(input PlannerInput, deterministic PlannerResult, modelResult PlannerResult) PlannerResult {
	modelResult.Version, modelResult.PromptVersion, modelResult.SchemaVersion = PlannerVersion, PlannerPromptVersion, PlannerSchemaVersion
	modelResult.Round = deterministic.Round
	modelResult.Hypotheses = mergeExistingHypotheses(input.ExistingHypotheses, modelResult.Hypotheses)
	modelResult.Hypotheses = mergeExistingHypotheses(modelResult.Hypotheses, deterministic.Hypotheses)
	modelResult.NextActions = append(modelResult.NextActions, deterministic.NextActions...)
	modelResult.MissingEvidence = append(modelResult.MissingEvidence, deterministic.MissingEvidence...)
	return modelResult
}

func parsePlannerResult(raw json.RawMessage) (*PlannerResult, error) {
	content := strings.TrimSpace(string(raw))
	if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(strings.TrimSpace(content), "```")
	}
	if !json.Valid([]byte(content)) {
		return nil, ErrInvalidInput
	}
	if err := skillframework.ValidateJSONSchema(PlannerOutputSchema, json.RawMessage(content)); err != nil {
		return nil, err
	}
	var result PlannerResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Service) validatePlannerResult(actor *model.AppUser, input PlannerInput, result PlannerResult, sources map[int64]datasourcesvc.DataSourceView) PlannerResult {
	result.Version, result.PromptVersion, result.SchemaVersion = PlannerVersion, PlannerPromptVersion, PlannerSchemaVersion
	result.Hypotheses = validatePlannerHypotheses(input, result.Hypotheses)
	seen := map[string]struct{}{}
	for _, signature := range input.CompletedActionKeys {
		seen[signature] = struct{}{}
	}
	actions := make([]PlannerAction, 0, len(result.NextActions))
	for _, action := range result.NextActions {
		definition, err := s.skillCatalog.Get(action.SkillName)
		if err != nil || !definition.Enabled || !definition.ReadOnly || !plannerSkillAuthorized(actor, definition) {
			continue
		}
		action.Input = sanitizeJSON(action.Input, 16<<10, nil)
		if action.Input == nil || skillframework.ValidateJSONSchema(definition.InputSchema, action.Input) != nil {
			continue
		}
		if !plannerDataSourceAllowed(action.SkillName, action.Input, sources) {
			continue
		}
		signature := actionSignature(action.SkillName, action.Input)
		if _, exists := seen[signature]; exists {
			continue
		}
		seen[signature] = struct{}{}
		action.ActionKey = truncateText(action.ActionKey, 160)
		action.Reason = truncateText(action.Reason, 512)
		action.TargetEntity = truncateText(action.TargetEntity, 256)
		action.EvidenceIDs = filterEvidenceIDs(action.EvidenceIDs, evidenceIDSet(input.Evidence))
		actions = append(actions, action)
		if len(actions) >= input.Budget.RemainingSkillCalls {
			break
		}
	}
	result.NextActions = actions
	result.MissingEvidence = uniqueStrings(result.MissingEvidence)
	if len(actions) == 0 && !result.ShouldStop {
		result.ShouldStop = true
		result.StopReason = "no_safe_new_actions"
	}
	if result.ShouldStop && strings.TrimSpace(result.StopReason) == "" {
		result.StopReason = "planner_stopped"
	}
	return result
}

func evidenceIDSet(values []PlannerEvidence) map[int64]struct{} {
	result := map[int64]struct{}{}
	for _, value := range values {
		result[value.ID] = struct{}{}
	}
	return result
}

func validatePlannerHypotheses(input PlannerInput, candidates []PlannerHypothesis) []PlannerHypothesis {
	validEvidence := map[int64]struct{}{}
	for _, item := range input.Evidence {
		validEvidence[item.ID] = struct{}{}
	}
	old := map[string]PlannerHypothesis{}
	for _, item := range input.ExistingHypotheses {
		old[item.ID] = item
	}
	result := []PlannerHypothesis{}
	for _, item := range candidates {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Summary) == "" || item.Confidence < 0 || item.Confidence > 1 {
			continue
		}
		item.SupportingEvidenceIDs = filterEvidenceIDs(item.SupportingEvidenceIDs, validEvidence)
		item.ContradictingEvidenceIDs = filterEvidenceIDs(item.ContradictingEvidenceIDs, validEvidence)
		if len(item.SupportingEvidenceIDs) == 0 {
			continue
		}
		if previous, ok := old[item.ID]; ok && previous.Confidence != item.Confidence {
			oldIDs := append(append([]int64{}, previous.SupportingEvidenceIDs...), previous.ContradictingEvidenceIDs...)
			newIDs := append(append([]int64{}, item.SupportingEvidenceIDs...), item.ContradictingEvidenceIDs...)
			if len(difference(newIDs, oldIDs)) == 0 {
				item.Confidence = previous.Confidence
			}
		}
		item.Summary, item.Rationale = truncateText(item.Summary, 1024), truncateText(item.Rationale, 1024)
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Confidence > result[j].Confidence })
	return result
}

func plannerSkillSpecs(definitions []skillframework.SkillDefinition, actor *model.AppUser) []PlannerSkillSpec {
	result := []PlannerSkillSpec{}
	for _, definition := range definitions {
		if definition.Enabled && definition.ReadOnly && plannerSkillAuthorized(actor, definition) {
			result = append(result, PlannerSkillSpec{Name: definition.Name, Description: definition.Description, InputSchema: definition.InputSchema, RiskLevel: definition.RiskLevel})
		}
	}
	return result
}

func plannerSkillAuthorized(actor *model.AppUser, definition skillframework.SkillDefinition) bool {
	return actor != nil && (definition.RiskLevel == model.SkillRiskSafeRead || actor.Role == model.RoleAdmin && definition.RiskLevel == model.SkillRiskSensitiveRead)
}

func (s *Service) accessiblePlannerDataSources(ctx context.Context, actor *model.AppUser) (map[int64]datasourcesvc.DataSourceView, error) {
	result := map[int64]datasourcesvc.DataSourceView{}
	if s.dataSources == nil {
		return result, nil
	}
	views, err := s.dataSources.List(ctx, actor)
	if err != nil {
		return nil, err
	}
	for _, view := range views {
		if view.Enabled && view.ReadOnly {
			result[view.ID] = view
		}
	}
	return result, nil
}

func plannerDataSourceAllowed(skill string, input json.RawMessage, sources map[int64]datasourcesvc.DataSourceView) bool {
	var values map[string]any
	if json.Unmarshal(input, &values) != nil {
		return false
	}
	rawID, exists := values["dataSourceId"]
	if !exists {
		return true
	}
	id, ok := rawID.(float64)
	if !ok || id <= 0 {
		return false
	}
	source, ok := sources[int64(id)]
	if !ok {
		return false
	}
	expected := ""
	switch {
	case strings.Contains(skill, "tidb"):
		expected = model.DataSourceTypeTiDB
	case strings.Contains(skill, "redis"):
		expected = model.DataSourceTypeRedis
	case strings.Contains(skill, "nginx"):
		expected = model.DataSourceTypeNginx
	case strings.Contains(skill, "k8s") || strings.Contains(skill, "pod_"):
		expected = model.DataSourceTypeKubernetes
	}
	return expected == "" || source.SourceType == expected
}

func (s *Service) persistPlannerResult(ctx context.Context, actor *model.AppUser, detail *Detail, result PlannerResult) error {
	latest := detail.Rounds[len(detail.Rounds)-1]
	nextActions := make([]NextAction, 0, len(result.NextActions))
	for _, action := range result.NextActions {
		nextActions = append(nextActions, NextAction{ActionKey: action.ActionKey, SkillName: action.SkillName, Input: action.Input})
	}
	rawActions, _ := json.Marshal(nextActions)
	if _, err := s.repository.UpdateRCARound(ctx, latest.ID, repository.RCARoundUpdates{NextActions: rawActions}); err != nil {
		return err
	}
	existing := map[string]struct{}{}
	for _, candidate := range detail.Candidates {
		existing[strings.TrimSpace(candidate.Summary)] = struct{}{}
	}
	for _, hypothesis := range result.Hypotheses {
		if _, ok := existing[strings.TrimSpace(hypothesis.Summary)]; ok || len(hypothesis.SupportingEvidenceIDs) == 0 {
			continue
		}
		if _, err := s.AddRootCauseCandidate(ctx, actor, detail.Run.ID, CreateCandidateInput{
			RoundID: latest.ID, Summary: hypothesis.Summary, Confidence: hypothesis.Confidence,
			EvidenceIDs: hypothesis.SupportingEvidenceIDs,
		}); err != nil {
			return err
		}
		existing[strings.TrimSpace(hypothesis.Summary)] = struct{}{}
	}
	return nil
}

func actionSignature(skill string, input json.RawMessage) string {
	var value any
	if json.Unmarshal(input, &value) != nil {
		return skill + "\x00" + string(input)
	}
	canonical, _ := json.Marshal(value)
	return skill + "\x00" + string(canonical)
}

func difference(values, excluded []int64) []int64 {
	set := map[int64]struct{}{}
	for _, value := range excluded {
		set[value] = struct{}{}
	}
	result := []int64{}
	for _, value := range values {
		if _, exists := set[value]; !exists {
			result = append(result, value)
		}
	}
	return uniqueIDs(result)
}

func filterEvidenceIDs(values []int64, valid map[int64]struct{}) []int64 {
	result := []int64{}
	for _, value := range values {
		if _, ok := valid[value]; ok {
			result = append(result, value)
		}
	}
	return uniqueIDs(result)
}

func containsAny(value string, candidates []string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, strings.ToLower(candidate)) {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	seen, result := map[string]struct{}{}, []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; !exists {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}
