package rca

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

const TopologyInvestigationVersion = "rca-topology-investigation-v1"

type TopologyInvestigationCandidate struct {
	NodeKey             string  `json:"nodeKey"`
	Name                string  `json:"name"`
	Kind                string  `json:"kind"`
	ComponentType       string  `json:"componentType"`
	SourceType          string  `json:"sourceType"`
	Namespace           string  `json:"namespace,omitempty"`
	HostID              int64   `json:"hostId,omitempty"`
	Hops                int     `json:"hops"`
	EdgeType            string  `json:"edgeType,omitempty"`
	Direction           string  `json:"direction"`
	Confidence          float64 `json:"confidence"`
	Score               float64 `json:"score"`
	Freshness           string  `json:"freshness"`
	AliasMatched        bool    `json:"aliasMatched"`
	Conflict            bool    `json:"conflict"`
	Selected            bool    `json:"selected"`
	DataSourceID        *int64  `json:"dataSourceId,omitempty"`
	BindingConfidence   float64 `json:"bindingConfidence,omitempty"`
	BindingStatus       string  `json:"bindingStatus,omitempty"`
	TopologyEvidenceIDs []int64 `json:"topologyEvidenceIds"`
}

type TopologyInvestigationResult struct {
	Version         string                           `json:"version"`
	RootNodeKey     string                           `json:"rootNodeKey,omitempty"`
	ObservedAliases []string                         `json:"observedAliases"`
	Candidates      []TopologyInvestigationCandidate `json:"candidates"`
	Selected        []TopologyInvestigationCandidate `json:"selected"`
	MissingEvidence []string                         `json:"missingEvidence"`
	Conflicts       []string                         `json:"conflicts"`
	FallbackUsed    bool                             `json:"fallbackUsed"`
}

type guidedActionResult struct {
	action     PlannerAction
	actionID   int64
	evidenceID int64
	output     json.RawMessage
	status     string
	errorCode  string
}

type topologyDependencyOutput struct {
	RootKey      string `json:"rootKey"`
	Dependencies []struct {
		Node struct {
			NodeKey    string `json:"nodeKey"`
			Name       string `json:"name"`
			Kind       string `json:"kind"`
			SourceType string `json:"sourceType"`
			Namespace  string `json:"namespace"`
			HostID     int64  `json:"hostId"`
			UpdatedAt  string `json:"updatedAt"`
		} `json:"node"`
		DependencyType string `json:"dependencyType"`
		Hops           int    `json:"hops"`
	} `json:"dependencies"`
	Edges []struct {
		FromNodeKey  string  `json:"fromNodeKey"`
		ToNodeKey    string  `json:"toNodeKey"`
		EdgeType     string  `json:"edgeType"`
		Confidence   float64 `json:"confidence"`
		Status       string  `json:"status"`
		LastObserved string  `json:"lastObservedAt"`
		StaleAt      string  `json:"staleAt"`
		UpdatedAt    string  `json:"updatedAt"`
	} `json:"edges"`
	MissingEvidence []string `json:"missingEvidence"`
}

func (s *Service) executeTopologyGuidedRound(
	ctx context.Context,
	actor *model.AppUser,
	runID int64,
	plan *PlannerResult,
	maxCalls int,
) (*OrchestratorRoundResult, error) {
	hypotheses := make([]Hypothesis, 0, len(plan.Hypotheses))
	for _, item := range plan.Hypotheses {
		hypotheses = append(hypotheses, Hypothesis{
			ID: item.ID, Summary: item.Summary, Confidence: item.Confidence,
			EvidenceIDs: item.SupportingEvidenceIDs,
		})
	}
	round, err := s.StartRound(ctx, actor, runID, StartRoundInput{InputHypotheses: hypotheses})
	if err != nil {
		return nil, err
	}
	detail, err := s.GetDetail(ctx, actor, runID)
	if err != nil {
		return nil, err
	}
	scope := decodePlannerScope(detail.Run.Scope)
	investigation := &TopologyInvestigationResult{
		Version: TopologyInvestigationVersion, ObservedAliases: observedDependencyAliases(scope, detail.Evidence),
		Candidates: []TopologyInvestigationCandidate{}, Selected: []TopologyInvestigationCandidate{},
		MissingEvidence: []string{}, Conflicts: []string{},
	}
	result := &OrchestratorRoundResult{
		RoundNumber: round.RoundNumber, Status: model.RCARoundStatusSuccess, Plan: plan,
		Topology: investigation,
	}
	remaining := maxCalls
	if remaining <= 0 {
		return s.completeTopologyInvestigationRound(ctx, actor, runID, round, result)
	}
	rootNodeKey := scope.string("topologyNodeKey", "nodeKey")
	if rootNodeKey == "" {
		serviceName := scope.string("serviceName", "service", "component")
		if serviceName != "" {
			action := plannerAction("find_topology_node", map[string]any{
				"query": serviceName, "environment": scope.string("environment"), "limit": 20,
			}, "通过服务名或 Alias 解析拓扑根节点", serviceName)
			executed := s.executeGuidedAction(ctx, actor, runID, round, action)
			remaining--
			mergeGuidedAction(result, executed)
			rootNodeKey = topologyRootNodeKey(executed.output)
		}
	}
	investigation.RootNodeKey = rootNodeKey
	resolvedAliasNodeKeys := map[string]bool{}
	for _, alias := range investigation.ObservedAliases {
		if remaining <= 0 {
			break
		}
		if !specificTopologyAlias(alias) {
			continue
		}
		action := plannerAction("find_topology_node", map[string]any{
			"query": alias, "environment": scope.string("environment"), "limit": 20,
		}, "将首轮日志中的依赖别名对齐到拓扑节点", alias)
		action.EvidenceIDs = allEvidenceIDs(detail.Evidence)
		executed := s.executeGuidedAction(ctx, actor, runID, round, action)
		remaining--
		mergeGuidedAction(result, executed)
		if nodeKey := topologyRootNodeKey(executed.output); nodeKey != "" {
			resolvedAliasNodeKeys[nodeKey] = true
		}
	}
	var dependencyResult guidedActionResult
	if rootNodeKey != "" && remaining > 0 {
		action := plannerAction("find_dependencies", map[string]any{
			"nodeKey": rootNodeKey, "direction": "both", "depth": 2,
			"maxNodes": 50, "maxEdges": 100, "environment": scope.string("environment"),
		}, "根据首轮异常线索查询有界下游依赖", rootNodeKey)
		action.EvidenceIDs = allEvidenceIDs(detail.Evidence)
		dependencyResult = s.executeGuidedAction(ctx, actor, runID, round, action)
		remaining--
		mergeGuidedAction(result, dependencyResult)
		if dependencyResult.evidenceID > 0 {
			investigation.Candidates = rankTopologyCandidates(
				dependencyResult.output, detail.Evidence, investigation.ObservedAliases, resolvedAliasNodeKeys,
				plan.Hypotheses, dependencyResult.evidenceID, s.now(),
			)
			investigation.Selected, investigation.Conflicts = selectTopologyCandidates(investigation.Candidates)
			for _, candidate := range investigation.Candidates {
				if candidate.Freshness == "stale" || candidate.Freshness == "expired" {
					investigation.MissingEvidence = append(investigation.MissingEvidence,
						candidate.Freshness+" topology relationship for "+candidate.NodeKey)
				}
			}
		}
	} else {
		investigation.MissingEvidence = append(investigation.MissingEvidence, "topology root node could not be resolved")
	}
	if len(investigation.Selected) == 0 {
		investigation.MissingEvidence = append(investigation.MissingEvidence, "no relevant reliable topology dependency was selected")
	}

	for index := range investigation.Selected {
		if remaining <= 0 {
			break
		}
		candidate := &investigation.Selected[index]
		if candidate.ComponentType == "linux" && candidate.HostID > 0 {
			candidate.BindingStatus = "topology_host"
			continue
		}
		explicitIDs := explicitDataSourceIDsForComponent(scope, candidate.ComponentType)
		action := plannerAction("get_topology_data_source_bindings", map[string]any{
			"nodeKey": candidate.NodeKey, "environment": scope.string("environment"),
			"explicitDataSourceIds": explicitIDs, "minimumConfidence": .8,
		}, "将相关拓扑依赖解析到当前用户可访问的数据源", candidate.NodeKey)
		action.EvidenceIDs = uniqueIDs(append(allEvidenceIDs(detail.Evidence), candidate.TopologyEvidenceIDs...))
		executed := s.executeGuidedAction(ctx, actor, runID, round, action)
		remaining--
		mergeGuidedAction(result, executed)
		id, confidence, status := topologyBindingForComponent(executed.output, candidate.ComponentType)
		candidate.DataSourceID, candidate.BindingConfidence, candidate.BindingStatus = id, confidence, status
		switch status {
		case "conflict":
			investigation.Conflicts = append(investigation.Conflicts, "conflicting data source bindings for "+candidate.NodeKey)
		case "missing":
			investigation.MissingEvidence = append(investigation.MissingEvidence, "no accepted data source binding for "+candidate.NodeKey)
		}
		if executed.evidenceID > 0 {
			candidate.TopologyEvidenceIDs = uniqueIDs(append(candidate.TopologyEvidenceIDs, executed.evidenceID))
		}
	}

	if len(investigation.Selected) == 0 {
		for _, candidate := range explicitFallbackCandidates(scope, plan.Hypotheses) {
			investigation.Selected = append(investigation.Selected, candidate)
			investigation.FallbackUsed = true
		}
	}
	componentActions := []PlannerAction{}
	for index := range investigation.Selected {
		candidate := &investigation.Selected[index]
		action, ok := componentInvestigationAction(scope, *candidate, detail.Evidence)
		if !ok {
			investigation.MissingEvidence = append(investigation.MissingEvidence,
				"no reliable component binding or required component scope for "+candidate.NodeKey)
			continue
		}
		componentActions = append(componentActions, action)
	}
	componentActions = deduplicatePlannerActions(componentActions)
	for _, action := range componentActions {
		if remaining <= 0 {
			break
		}
		executed := s.executeGuidedAction(ctx, actor, runID, round, action)
		remaining--
		mergeGuidedAction(result, executed)
	}
	investigation.Candidates = mergeSelectedTopologyCandidates(investigation.Candidates, investigation.Selected)
	investigation.MissingEvidence = uniqueStrings(investigation.MissingEvidence)
	result.Errors = uniqueStrings(result.Errors)
	return s.completeTopologyInvestigationRound(ctx, actor, runID, round, result)
}

func (s *Service) executeGuidedAction(ctx context.Context, actor *model.AppUser, runID int64, round *model.RCARound, action PlannerAction) guidedActionResult {
	result := guidedActionResult{action: action, status: model.RCAActionStatusFailed}
	detail, err := s.GetDetail(ctx, actor, runID)
	if err != nil {
		result.errorCode = "load_rca_state_failed"
		return result
	}
	input := buildPlannerInput(detail, PlanRequest{Budget: PlannerBudget{RemainingRounds: 1, RemainingSkillCalls: 1, RemainingWallTimeSeconds: 60}})
	sources, err := s.accessiblePlannerDataSources(ctx, actor)
	if err != nil {
		result.errorCode = "data_source_permission_failed"
		return result
	}
	validated := s.validatePlannerResult(actor, input, PlannerResult{NextActions: []PlannerAction{action}}, sources)
	if len(validated.NextActions) != 1 {
		result.errorCode = "unsafe_or_duplicate_action"
		return result
	}
	action = validated.NextActions[0]
	definition, err := s.skillCatalog.Get(action.SkillName)
	if err != nil {
		result.errorCode = "skill_not_found"
		return result
	}
	stored, err := s.CreateAction(ctx, actor, runID, CreateActionInput{
		RoundID: round.ID, ActionKey: action.ActionKey, SkillName: action.SkillName, Input: action.Input,
		SensitiveRead: definition.RiskLevel == model.SkillRiskSensitiveRead,
	})
	if err != nil {
		result.errorCode = "create_action_failed"
		return result
	}
	result.actionID = stored.ID
	if stored.Status == model.RCAActionStatusSuccess || stored.Status == model.RCAActionStatusSkipped {
		var ids []int64
		_ = json.Unmarshal(stored.EvidenceIDs, &ids)
		if len(ids) > 0 {
			result.evidenceID = ids[0]
		}
		result.output, result.status = stored.Output, stored.Status
		return result
	}
	if stored.Status == model.RCAActionStatusRunning {
		attempt := stored.Attempt + 1
		stored, err = s.repository.UpdateRCAAction(ctx, stored.ID, repository.RCAActionUpdates{
			Status: model.RCAActionStatusPending, Attempt: &attempt, ClearError: true, ClearFinishedAt: true,
		})
		if err != nil {
			result.errorCode = "recover_action_failed"
			return result
		}
	}
	stored, err = s.StartAction(ctx, actor, runID, stored.ID)
	if err != nil {
		result.errorCode = "start_action_failed"
		return result
	}
	executed, executeErr := s.executeRCASkill(ctx, actor, action.SkillName, action.Input, nil)
	output := json.RawMessage(`{}`)
	if executed != nil {
		output = executed.Output
	}
	status, errorCode := model.RCAActionStatusSuccess, ""
	var evidenceIDs []int64
	if executeErr != nil || executed == nil {
		if executeErr == nil {
			executeErr = errors.New("skill returned no result")
		}
		status, errorCode = model.RCAActionStatusFailed, classifyRoundOneError(executeErr)
	} else {
		execution := &plannedExecution{plan: action, action: stored, result: executed, partial: outputIsPartial(output)}
		record, evidenceErr := s.addPlannerActionEvidence(ctx, actor, runID, round, execution)
		if evidenceErr != nil {
			status, errorCode = model.RCAActionStatusFailed, "evidence_persist_failed"
		} else if record != nil {
			result.evidenceID = record.ID
			evidenceIDs = []int64{record.ID}
		}
		if execution.partial {
			status, errorCode = model.RCAActionStatusPartialSuccess, "upstream_unavailable"
		}
	}
	completed, completeErr := s.CompleteAction(ctx, actor, runID, stored.ID, CompleteActionInput{
		Status: status, Output: output, EvidenceIDs: evidenceIDs, ErrorCode: errorCode,
	})
	if completeErr != nil {
		result.errorCode = "complete_action_failed"
		return result
	}
	result.output, result.status, result.errorCode = output, completed.Status, errorCode
	return result
}

func mergeGuidedAction(result *OrchestratorRoundResult, action guidedActionResult) {
	if action.actionID > 0 {
		result.ActionIDs = append(result.ActionIDs, action.actionID)
	}
	if action.evidenceID > 0 {
		result.EvidenceIDs = append(result.EvidenceIDs, action.evidenceID)
	}
	if action.errorCode != "" {
		result.Errors = append(result.Errors, action.action.SkillName+":"+action.errorCode)
	}
}

func (s *Service) completeTopologyInvestigationRound(
	ctx context.Context,
	actor *model.AppUser,
	runID int64,
	round *model.RCARound,
	result *OrchestratorRoundResult,
) (*OrchestratorRoundResult, error) {
	result.ActionIDs = uniqueIDs(result.ActionIDs)
	result.EvidenceIDs = uniqueIDs(result.EvidenceIDs)
	result.Status = model.RCARoundStatusSuccess
	if len(result.EvidenceIDs) == 0 {
		result.Status = model.RCARoundStatusFailed
	} else if len(result.Errors) > 0 || len(result.Topology.MissingEvidence) > 0 || len(result.Topology.Conflicts) > 0 {
		result.Status = model.RCARoundStatusPartialSuccess
	}
	errorCode := ""
	if result.Status != model.RCARoundStatusSuccess {
		errorCode = "topology_investigation_partial"
	}
	_, err := s.CompleteRound(ctx, actor, runID, round.ID, CompleteRoundInput{
		Status: result.Status, NewEvidenceIDs: result.EvidenceIDs, ErrorCode: errorCode,
	})
	if err != nil {
		return result, err
	}
	if result.Status != model.RCARoundStatusSuccess {
		return result, ErrRoundPartial
	}
	return result, nil
}

func topologyRootNodeKey(raw json.RawMessage) string {
	var body struct {
		Node *struct {
			NodeKey string `json:"nodeKey"`
		} `json:"node"`
	}
	if json.Unmarshal(raw, &body) == nil && body.Node != nil {
		return strings.TrimSpace(body.Node.NodeKey)
	}
	return ""
}

func observedDependencyAliases(scope plannerScope, evidence []model.EvidenceRecord) []string {
	values := []string{}
	if raw, ok := scope["dependencyNames"].([]any); ok {
		for _, item := range raw {
			if value, ok := item.(string); ok {
				values = append(values, value)
			}
		}
	}
	for _, item := range evidence {
		text := strings.ToLower(item.Summary + " " + string(item.Content))
		for _, marker := range []string{"redis", "nacos", "nginx", "tidb", "mysql", "database", "order-db"} {
			if strings.Contains(text, marker) {
				values = append(values, marker)
			}
		}
	}
	return uniqueStrings(values)
}

func rankTopologyCandidates(
	raw json.RawMessage,
	evidence []model.EvidenceRecord,
	aliases []string,
	resolvedAliasNodeKeys map[string]bool,
	hypotheses []PlannerHypothesis,
	topologyEvidenceID int64,
	now time.Time,
) []TopologyInvestigationCandidate {
	var output topologyDependencyOutput
	if json.Unmarshal(raw, &output) != nil {
		return nil
	}
	evidenceText := strings.ToLower(strings.Join(aliases, " "))
	for _, item := range evidence {
		evidenceText += " " + strings.ToLower(item.Summary+" "+string(item.Content))
	}
	relevant := relevantComponentTypes(evidenceText, hypotheses)
	edges := topologyEdgesByNode(output)
	result := []TopologyInvestigationCandidate{}
	resultByNode := map[string]int{}
	for _, dependency := range output.Dependencies {
		componentType := topologyComponentType(dependency.Node.Kind, dependency.Node.Name, dependency.Node.SourceType)
		aliasMatched := resolvedAliasNodeKeys[dependency.Node.NodeKey] ||
			topologyIdentityObserved(evidenceText, nil, dependency.Node.NodeKey, dependency.Node.Name)
		if !relevant[componentType] && !aliasMatched {
			continue
		}
		edge := edges[dependency.Node.NodeKey]
		freshness := topologyFreshness(edge.Status, edge.StaleAt, edge.LastObserved, dependency.Node.UpdatedAt, now)
		score := .25 + edge.Confidence*.4 + 0.2/float64(maxIntRCA(1, dependency.Hops))
		if aliasMatched {
			score += .3
		}
		switch edge.EdgeType {
		case model.TopologyEdgeTypeCalls, model.TopologyEdgeTypeDependsOn, model.TopologyEdgeTypeStoresIn:
			score += .12
		case model.TopologyEdgeTypeRoutesTo, model.TopologyEdgeTypeConnectsTo:
			score += .08
		}
		if edge.Direction == "downstream" {
			score += .04
		} else if edge.Direction == "upstream" {
			score += .02
		}
		if freshness == "stale" {
			score -= .2
		}
		if freshness == "expired" {
			score -= .5
		}
		if freshness != "fresh" {
			score -= .05
		}
		candidate := TopologyInvestigationCandidate{
			NodeKey: dependency.Node.NodeKey, Name: dependency.Node.Name, Kind: dependency.Node.Kind,
			ComponentType: componentType, SourceType: dependency.Node.SourceType, Namespace: dependency.Node.Namespace,
			HostID: dependency.Node.HostID, Hops: dependency.Hops, EdgeType: edge.EdgeType, Direction: edge.Direction,
			Confidence: edge.Confidence, Score: score, Freshness: freshness, AliasMatched: aliasMatched,
			TopologyEvidenceIDs: []int64{topologyEvidenceID},
		}
		if existing, exists := resultByNode[candidate.NodeKey]; exists {
			if candidate.Score > result[existing].Score {
				result[existing] = candidate
			}
			continue
		}
		resultByNode[candidate.NodeKey] = len(result)
		result = append(result, candidate)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Score == result[j].Score {
			return result[i].NodeKey < result[j].NodeKey
		}
		return result[i].Score > result[j].Score
	})
	return result
}

type topologyDependencyOutputEdge struct {
	EdgeType     string
	Direction    string
	Confidence   float64
	Status       string
	LastObserved string
	StaleAt      string
	UpdatedAt    string
}

func topologyEdgesByNode(output topologyDependencyOutput) map[string]topologyDependencyOutputEdge {
	directions := map[string]string{output.RootKey: ""}
	for pass := 0; pass < 5; pass++ {
		changed := false
		for _, edge := range output.Edges {
			if direction, exists := directions[edge.FromNodeKey]; exists {
				next := direction
				if next == "" {
					next = "downstream"
				}
				if _, known := directions[edge.ToNodeKey]; !known {
					directions[edge.ToNodeKey], changed = next, true
				}
			}
			if direction, exists := directions[edge.ToNodeKey]; exists {
				next := direction
				if next == "" {
					next = "upstream"
				}
				if _, known := directions[edge.FromNodeKey]; !known {
					directions[edge.FromNodeKey], changed = next, true
				}
			}
		}
		if !changed {
			break
		}
	}
	result := map[string]topologyDependencyOutputEdge{}
	for _, edge := range output.Edges {
		nodeKey := edge.ToNodeKey
		if edge.ToNodeKey == output.RootKey || directions[edge.FromNodeKey] == "upstream" {
			nodeKey = edge.FromNodeKey
		}
		current := result[nodeKey]
		if edge.Confidence > current.Confidence {
			result[nodeKey] = topologyDependencyOutputEdge{
				EdgeType: edge.EdgeType, Direction: directions[nodeKey], Confidence: edge.Confidence,
				Status: edge.Status, LastObserved: edge.LastObserved, StaleAt: edge.StaleAt, UpdatedAt: edge.UpdatedAt,
			}
		}
	}
	return result
}

func selectTopologyCandidates(candidates []TopologyInvestigationCandidate) ([]TopologyInvestigationCandidate, []string) {
	byType := map[string][]int{}
	for index := range candidates {
		if candidates[index].Freshness == "expired" || candidates[index].Score < .35 {
			continue
		}
		byType[candidates[index].ComponentType] = append(byType[candidates[index].ComponentType], index)
	}
	conflicts := []string{}
	selected := []TopologyInvestigationCandidate{}
	for componentType, indexes := range byType {
		top := indexes[0]
		if len(indexes) > 1 && candidates[top].Score-candidates[indexes[1]].Score < .05 &&
			!candidates[top].AliasMatched && !candidates[indexes[1]].AliasMatched {
			candidates[top].Conflict, candidates[indexes[1]].Conflict = true, true
			conflicts = append(conflicts, "conflicting "+componentType+" topology dependencies")
			continue
		}
		candidates[top].Selected = true
		selected = append(selected, candidates[top])
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Score > selected[j].Score })
	return selected, conflicts
}

func topologyBindingForComponent(raw json.RawMessage, componentType string) (*int64, float64, string) {
	var body struct {
		Bindings []struct {
			DataSourceID int64   `json:"dataSourceId"`
			SourceType   string  `json:"sourceType"`
			Confidence   float64 `json:"confidence"`
			Status       string  `json:"status"`
			Accepted     bool    `json:"accepted"`
		} `json:"bindings"`
		Conflicts []string `json:"conflicts"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return nil, 0, "missing"
	}
	for _, binding := range body.Bindings {
		if binding.Accepted && componentMatchesDataSource(componentType, binding.SourceType) {
			id := binding.DataSourceID
			return &id, binding.Confidence, binding.Status
		}
	}
	if len(body.Conflicts) > 0 {
		return nil, 0, "conflict"
	}
	return nil, 0, "missing"
}

func componentInvestigationAction(scope plannerScope, candidate TopologyInvestigationCandidate, evidence []model.EvidenceRecord) (PlannerAction, bool) {
	inputEvidenceIDs := uniqueIDs(append(allEvidenceIDs(evidence), candidate.TopologyEvidenceIDs...))
	dataSourceID := candidate.DataSourceID
	if dataSourceID == nil {
		ids := explicitDataSourceIDsForComponent(scope, candidate.ComponentType)
		if len(ids) == 1 {
			dataSourceID = &ids[0]
		}
	}
	var action PlannerAction
	switch candidate.ComponentType {
	case "database":
		if dataSourceID == nil {
			return action, false
		}
		action = plannerAction("query_tidb_slow_queries", map[string]any{"dataSourceId": *dataSourceID, "minutes": 30, "limit": 20}, "验证拓扑相关数据库的慢 SQL", candidate.NodeKey)
	case "redis":
		if dataSourceID == nil {
			return action, false
		}
		action = plannerAction("diagnose_redis_connection_pool", map[string]any{"dataSourceId": *dataSourceID}, "验证拓扑相关 Redis 延迟与连接池压力", candidate.NodeKey)
	case "nacos":
		if dataSourceID == nil {
			return action, false
		}
		action = plannerAction("diagnose_nacos_registration", map[string]any{
			"dataSourceId": *dataSourceID, "namespace": scope.string("namespace"),
			"serviceName": scope.string("serviceName"),
		}, "验证拓扑相关 Nacos 注册状态", candidate.NodeKey)
	case "nginx":
		if dataSourceID == nil {
			return action, false
		}
		action = plannerAction("diagnose_nginx_upstream", map[string]any{"dataSourceId": *dataSourceID, "limit": 100}, "验证拓扑相关 Nginx upstream 状态", candidate.NodeKey)
	case "kubernetes":
		if dataSourceID == nil {
			return action, false
		}
		namespace := firstNonEmptyRCA(candidate.Namespace, scope.string("namespace"))
		podName := scope.string("podName", "pod")
		if podName == "" && strings.Contains(strings.ToLower(candidate.Kind), "pod") {
			podName = candidate.Name
		}
		if namespace == "" || podName == "" {
			return action, false
		}
		action = plannerAction("run_k8s_diagnostic_rules", map[string]any{
			"dataSourceId": *dataSourceID, "namespace": namespace, "podName": podName, "logTailLines": 200,
		}, "验证拓扑相关 Kubernetes 工作负载状态", candidate.NodeKey)
	case "linux":
		hostID := candidate.HostID
		if hostID == 0 {
			hostID = scope.int64("linuxHostId", "hostId")
		}
		if hostID == 0 {
			return action, false
		}
		action = plannerAction("diagnose_linux_host_health", map[string]any{"hostId": hostID, "topN": 10}, "验证拓扑相关 Linux 主机状态", candidate.NodeKey)
	default:
		return action, false
	}
	action.EvidenceIDs = inputEvidenceIDs
	return action, true
}

func explicitFallbackCandidates(scope plannerScope, hypotheses []PlannerHypothesis) []TopologyInvestigationCandidate {
	result := []TopologyInvestigationCandidate{}
	for componentType := range relevantComponentTypes("", hypotheses) {
		ids := explicitDataSourceIDsForComponent(scope, componentType)
		if len(ids) != 1 && componentType != "linux" {
			continue
		}
		candidate := TopologyInvestigationCandidate{
			NodeKey: "explicit-scope:" + componentType, Name: componentType, ComponentType: componentType,
			Direction: "explicit", Confidence: 1, Score: 1, Freshness: "explicit", Selected: true,
			BindingStatus: "explicit_scope",
		}
		if len(ids) == 1 {
			candidate.DataSourceID = &ids[0]
		}
		result = append(result, candidate)
	}
	return result
}

func explicitDataSourceIDsForComponent(scope plannerScope, componentType string) []int64 {
	keys := map[string][]string{
		"database":   {"tidbDataSourceId", "databaseDataSourceId"},
		"redis":      {"redisDataSourceId"},
		"nacos":      {"nacosDataSourceId"},
		"nginx":      {"nginxDataSourceId"},
		"kubernetes": {"kubernetesDataSourceId", "k8sDataSourceId"},
	}
	result := []int64{}
	for _, key := range keys[componentType] {
		if id := scope.int64(key); id > 0 {
			result = append(result, id)
		}
	}
	if len(result) == 0 {
		if generic := scope.int64("dataSourceId"); generic > 0 {
			result = append(result, generic)
		}
	}
	return uniqueIDs(result)
}

func relevantComponentTypes(evidenceText string, hypotheses []PlannerHypothesis) map[string]bool {
	result := map[string]bool{}
	text := strings.ToLower(evidenceText)
	if containsAny(text, []string{"database", "数据库", "db call", "slow sql", "慢sql", "tidb", "mysql", "order-db"}) {
		result["database"] = true
	}
	if containsAny(text, []string{"redis", "cache", "缓存", "连接池"}) {
		result["redis"] = true
	}
	if containsAny(text, []string{"nacos", "注册中心", "配置中心"}) {
		result["nacos"] = true
	}
	if containsAny(text, []string{"nginx", "upstream", "504", "502"}) {
		result["nginx"] = true
	}
	if containsAny(text, []string{"kubernetes", "k8s", "pod", "container", "容器"}) {
		result["kubernetes"] = true
	}
	if containsAny(text, []string{"linux", "cpu", "memory", "disk io", "network", "主机"}) {
		result["linux"] = true
	}
	for _, hypothesis := range hypotheses {
		switch {
		case strings.Contains(hypothesis.ID, "database"):
			result["database"] = true
		case strings.Contains(hypothesis.ID, "redis"):
			result["redis"] = true
		case strings.Contains(hypothesis.ID, "nginx"):
			result["nginx"] = true
		case strings.Contains(hypothesis.ID, "k8s"):
			result["kubernetes"] = true
		case strings.Contains(hypothesis.ID, "linux"):
			result["linux"] = true
		}
	}
	return result
}

func topologyComponentType(kind, name, sourceType string) string {
	text := strings.ToLower(strings.Join([]string{kind, name, sourceType}, " "))
	switch {
	case containsAny(text, []string{"tidb", "tikv", "mysql", "postgres", "database", "order-db", " db"}):
		return "database"
	case strings.Contains(text, "redis"):
		return "redis"
	case strings.Contains(text, "nacos"):
		return "nacos"
	case strings.Contains(text, "nginx"):
		return "nginx"
	case containsAny(text, []string{"kubernetes", "k8s", "pod", "deployment", "statefulset", "daemonset"}):
		return "kubernetes"
	case containsAny(text, []string{"linux", "host", "node", "process"}):
		return "linux"
	default:
		return "downstream_service"
	}
}

func specificTopologyAlias(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	if len(value) < 4 || containsAny(value, []string{"database", "mysql", "tidb", "redis", "nacos", "nginx"}) {
		return false
	}
	return strings.ContainsAny(value, "-._:/")
}

func topologyIdentityObserved(evidenceText string, aliases []string, nodeKey, name string) bool {
	normalizedEvidence := normalizeTopologyIdentity(evidenceText)
	for _, value := range append(append([]string{}, aliases...), nodeKey, name) {
		normalized := normalizeTopologyIdentity(value)
		if len(normalized) >= 4 && strings.Contains(normalizedEvidence, normalized) {
			return true
		}
	}
	return false
}

func normalizeTopologyIdentity(value string) string {
	value = strings.ToLower(value)
	return strings.Map(func(character rune) rune {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character > 127 {
			return character
		}
		return -1
	}, value)
}

func topologyFreshness(status, staleAt, observedAt, nodeUpdatedAt string, now time.Time) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "expired":
		return "expired"
	case "stale":
		return "stale"
	}
	if parsed, err := time.Parse(time.RFC3339, staleAt); err == nil && !parsed.After(now) {
		return "stale"
	}
	for _, value := range []string{observedAt, nodeUpdatedAt} {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil && now.Sub(parsed) > 24*time.Hour {
			return "stale"
		}
	}
	return "fresh"
}

func componentMatchesDataSource(componentType, sourceType string) bool {
	switch componentType {
	case "database":
		return sourceType == model.DataSourceTypeTiDB
	case "kubernetes":
		return sourceType == model.DataSourceTypeKubernetes
	case "linux":
		return sourceType == model.DataSourceTypeLinuxServer || sourceType == model.DataSourceTypeLinuxGroup
	default:
		return sourceType == componentType
	}
}

func deduplicatePlannerActions(actions []PlannerAction) []PlannerAction {
	seen := map[string]struct{}{}
	result := []PlannerAction{}
	for _, action := range actions {
		signature := actionSignature(action.SkillName, action.Input)
		if _, exists := seen[signature]; exists {
			continue
		}
		seen[signature] = struct{}{}
		result = append(result, action)
	}
	return result
}

func allEvidenceIDs(values []model.EvidenceRecord) []int64 {
	result := make([]int64, 0, len(values))
	for _, value := range values {
		result = append(result, value.ID)
	}
	return uniqueIDs(result)
}

func mergeSelectedTopologyCandidates(all, selected []TopologyInvestigationCandidate) []TopologyInvestigationCandidate {
	selectedByKey := map[string]TopologyInvestigationCandidate{}
	for _, value := range selected {
		selectedByKey[value.NodeKey] = value
	}
	for index := range all {
		if selectedValue, ok := selectedByKey[all[index].NodeKey]; ok {
			all[index] = selectedValue
		}
	}
	return all
}
