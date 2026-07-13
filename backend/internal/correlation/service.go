package correlation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

var ErrInvalidInput = errors.New("invalid input")

type EventRepository interface {
	FindOpsEventByID(ctx context.Context, id int64) (*model.OpsEvent, error)
	ListOpsEvents(ctx context.Context, filters repository.EventFilters) ([]model.OpsEvent, error)
}

type TopologyRepository interface {
	ListNodes(ctx context.Context, filters repository.TopologyFilters) ([]model.TopologyNode, error)
	ListEdges(ctx context.Context, filters repository.TopologyFilters) ([]model.TopologyEdge, error)
}

type Service struct {
	events   EventRepository
	topology TopologyRepository
}

type Query struct {
	TargetEventID   int64      `json:"targetEventId"`
	From            *time.Time `json:"from"`
	To              *time.Time `json:"to"`
	BeforeMinutes   int        `json:"beforeMinutes"`
	AfterMinutes    int        `json:"afterMinutes"`
	Environment     string     `json:"environment"`
	SystemName      string     `json:"systemName"`
	ComponentName   string     `json:"componentName"`
	Namespace       string     `json:"namespace"`
	ResourceName    string     `json:"resourceName"`
	Limit           int        `json:"limit"`
	IncludeTopology bool       `json:"includeTopology"`
}

type Result struct {
	Target       EventSummary `json:"target"`
	From         time.Time    `json:"from"`
	To           time.Time    `json:"to"`
	Timezone     string       `json:"timezone"`
	Candidates   []Candidate  `json:"candidates"`
	ScorePolicy  string       `json:"scorePolicy"`
	TopologyUsed bool         `json:"topologyUsed"`
	EvidenceGate string       `json:"evidenceGate"`
}

type Candidate struct {
	Event             EventSummary          `json:"event"`
	Score             float64               `json:"score"`
	Confidence        string                `json:"confidence"`
	ScoreDetails      []ScoreDetail         `json:"scoreDetails"`
	TopologyPaths     []TopologyPathSummary `json:"topologyPaths,omitempty"`
	EvidenceKeys      []string              `json:"evidenceKeys,omitempty"`
	EvidenceAvailable bool                  `json:"evidenceAvailable"`
	Reason            string                `json:"reason"`
}

type EventSummary struct {
	ID            int64     `json:"id"`
	Time          time.Time `json:"time"`
	SourceType    string    `json:"sourceType"`
	EventType     string    `json:"eventType"`
	Summary       string    `json:"summary"`
	Environment   string    `json:"environment,omitempty"`
	SystemName    string    `json:"systemName,omitempty"`
	ComponentName string    `json:"componentName,omitempty"`
	Namespace     string    `json:"namespace,omitempty"`
	ResourceKind  string    `json:"resourceKind,omitempty"`
	ResourceName  string    `json:"resourceName,omitempty"`
	TraceID       string    `json:"traceId,omitempty"`
}

type ScoreDetail struct {
	Name        string  `json:"name"`
	Score       float64 `json:"score"`
	Weight      float64 `json:"weight"`
	Weighted    float64 `json:"weighted"`
	Explanation string  `json:"explanation"`
}

type TopologyPathSummary struct {
	NodeKeys   []string `json:"nodeKeys"`
	EdgeTypes  []string `json:"edgeTypes"`
	Semantics  []string `json:"semantics"`
	Hops       int      `json:"hops"`
	Confidence float64  `json:"confidence"`
	ImpactType string   `json:"impactType"`
}

type topologyGraph struct {
	nodes []model.TopologyNode
	edges []model.TopologyEdge
}

type topologySignal struct {
	score       float64
	explanation string
	paths       []topologyPath
}

type topologyPath struct {
	nodeKeys   []string
	edgeTypes  []string
	confidence float64
	semantics  []string
}

func NewService(events EventRepository, topology TopologyRepository) *Service {
	return &Service{events: events, topology: topology}
}

func (s *Service) Analyze(ctx context.Context, query Query) (*Result, error) {
	if query.TargetEventID <= 0 {
		return nil, ErrInvalidInput
	}
	target, err := s.events.FindOpsEventByID(ctx, query.TargetEventID)
	if err != nil {
		return nil, err
	}
	from, to, err := resolveWindow(*target, query)
	if err != nil {
		return nil, err
	}
	limit := query.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	events, err := s.events.ListOpsEvents(ctx, repository.EventFilters{
		Limit:         limit,
		Environment:   firstNonEmpty(query.Environment, deref(target.Environment)),
		SystemName:    firstNonEmpty(query.SystemName, deref(target.SystemName)),
		ComponentName: strings.TrimSpace(query.ComponentName),
		Namespace:     strings.TrimSpace(query.Namespace),
		ResourceName:  strings.TrimSpace(query.ResourceName),
		From:          &from,
		To:            &to,
	})
	if err != nil {
		return nil, err
	}
	graph := topologyGraph{}
	topologyUsed := false
	if query.IncludeTopology && s.topology != nil {
		graph, topologyUsed = s.loadTopology(ctx, target, query)
	}
	candidates := make([]Candidate, 0, len(events))
	targetEvidence := evidenceKeysFromPayload(target.Payload)
	for _, event := range events {
		if event.ID == target.ID {
			continue
		}
		candidate := scoreCandidate(*target, event, graph, topologyUsed, targetEvidence)
		candidates = append(candidates, candidate)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			if !candidates[i].Event.Time.Equal(candidates[j].Event.Time) {
				return candidates[i].Event.Time.Before(candidates[j].Event.Time)
			}
			return candidates[i].Event.ID < candidates[j].Event.ID
		}
		return candidates[i].Score > candidates[j].Score
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return &Result{
		Target:       summarizeEvent(*target),
		From:         from,
		To:           to,
		Timezone:     "UTC",
		Candidates:   candidates,
		ScorePolicy:  "identifier 35%, temporal 25%, topology 20%, semantic 15%, evidence 5%",
		TopologyUsed: topologyUsed,
		EvidenceGate: "candidate without evidence reference is capped below high confidence",
	}, nil
}

func resolveWindow(target model.OpsEvent, query Query) (time.Time, time.Time, error) {
	if query.From != nil && query.To != nil {
		from := query.From.UTC()
		to := query.To.UTC()
		if to.Before(from) {
			return time.Time{}, time.Time{}, ErrInvalidInput
		}
		return from, to, nil
	}
	before := query.BeforeMinutes
	after := query.AfterMinutes
	if before <= 0 {
		before = 60
	}
	if after <= 0 {
		after = 30
	}
	if before > 24*60 || after > 24*60 {
		return time.Time{}, time.Time{}, ErrInvalidInput
	}
	anchor := target.EventTime.UTC()
	return anchor.Add(-time.Duration(before) * time.Minute), anchor.Add(time.Duration(after) * time.Minute), nil
}

func (s *Service) loadTopology(ctx context.Context, target *model.OpsEvent, query Query) (topologyGraph, bool) {
	nodes, err := s.topology.ListNodes(ctx, repository.TopologyFilters{
		Environment: firstNonEmpty(query.Environment, deref(target.Environment)),
		Cluster:     deref(target.Cluster),
		Namespace:   firstNonEmpty(query.Namespace, deref(target.Namespace)),
		Limit:       1000,
	})
	if err != nil {
		return topologyGraph{}, false
	}
	edges, err := s.topology.ListEdges(ctx, repository.TopologyFilters{
		Environment: firstNonEmpty(query.Environment, deref(target.Environment)),
		Cluster:     deref(target.Cluster),
		Namespace:   firstNonEmpty(query.Namespace, deref(target.Namespace)),
		Limit:       3000,
	})
	if err != nil {
		return topologyGraph{}, false
	}
	return topologyGraph{nodes: nodes, edges: edges}, true
}

func scoreCandidate(target, candidate model.OpsEvent, graph topologyGraph, topologyUsed bool, targetEvidence []string) Candidate {
	candidateEvidence := evidenceKeysFromPayload(candidate.Payload)
	topologyDetail, topologyPaths := topologyScoreAndPaths(target, candidate, graph, topologyUsed)
	details := []ScoreDetail{
		identifierScore(target, candidate),
		temporalScore(target, candidate),
		topologyDetail,
		semanticScore(target, candidate),
		evidenceScore(targetEvidence, candidateEvidence),
	}
	total := 0.0
	for _, detail := range details {
		total += detail.Weighted
	}
	evidenceAvailable := len(candidateEvidence) > 0
	if !evidenceAvailable && total >= 0.7 {
		total = 0.69
	}
	total = roundScore(total)
	return Candidate{
		Event:             summarizeEvent(candidate),
		Score:             total,
		Confidence:        confidence(total),
		ScoreDetails:      details,
		TopologyPaths:     topologyPaths,
		EvidenceKeys:      candidateEvidence,
		EvidenceAvailable: evidenceAvailable,
		Reason:            reasonFromDetails(details, evidenceAvailable),
	}
}

func identifierScore(target, candidate model.OpsEvent) ScoreDetail {
	matches := 0
	total := 0
	check := func(name string, left, right string) {
		if left == "" || right == "" {
			return
		}
		total++
		if strings.EqualFold(left, right) {
			matches++
		}
	}
	check("environment", deref(target.Environment), deref(candidate.Environment))
	check("system", deref(target.SystemName), deref(candidate.SystemName))
	check("component", deref(target.ComponentName), deref(candidate.ComponentName))
	check("namespace", deref(target.Namespace), deref(candidate.Namespace))
	check("resource", deref(target.ResourceName), deref(candidate.ResourceName))
	check("trace", deref(target.TraceID), deref(candidate.TraceID))
	score := 0.0
	explanation := "no comparable identifiers"
	if total > 0 {
		score = float64(matches) / float64(total)
		explanation = strings.TrimSpace(strings.Join([]string{
			intString(matches), "of", intString(total), "comparable identifiers matched",
		}, " "))
	}
	return weightedDetail("identifier", score, 0.35, explanation)
}

func temporalScore(target, candidate model.OpsEvent) ScoreDetail {
	delta := math.Abs(candidate.EventTime.UTC().Sub(target.EventTime.UTC()).Minutes())
	score := 0.0
	switch {
	case delta <= 5:
		score = 1
	case delta <= 15:
		score = 0.8
	case delta <= 60:
		score = 0.55
	case delta <= 180:
		score = 0.25
	default:
		score = 0.05
	}
	return weightedDetail("temporal", score, 0.25, "event is "+intString(int(delta))+" minutes from target")
}

func topologyScoreAndPaths(target, candidate model.OpsEvent, graph topologyGraph, used bool) (ScoreDetail, []TopologyPathSummary) {
	if !used {
		return weightedDetail("topology", 0, 0.20, "topology scoring disabled or unavailable"), nil
	}
	targetNodes := matchingNodeKeys(target, graph.nodes)
	candidateNodes := matchingNodeKeys(candidate, graph.nodes)
	if len(targetNodes) == 0 || len(candidateNodes) == 0 {
		return weightedDetail("topology", 0, 0.20, "no matching topology node for target or candidate"), nil
	}
	if intersects(targetNodes, candidateNodes) {
		return weightedDetail("topology", 1, 0.20, "target and candidate map to the same topology node"), nil
	}
	pathSignal := bestPathSignal(targetNodes, candidateNodes, graph.edges)
	commonSignal := commonDependencySignal(targetNodes, candidateNodes, graph.edges)
	best := pathSignal
	if commonSignal.score > best.score {
		best = commonSignal
	}
	if best.score > 0 {
		return weightedDetail("topology", best.score, 0.20, best.explanation), topologyPathSummaries(best.paths)
	}
	return weightedDetail("topology", 0.05, 0.20, "topology nodes found but no close path discovered"), nil
}

func bestPathSignal(targetNodes, candidateNodes map[string]struct{}, edges []model.TopologyEdge) topologySignal {
	path, ok := bestTopologyPath(targetNodes, candidateNodes, edges, 3)
	if !ok {
		return topologySignal{}
	}
	score := pathScore(path)
	return topologySignal{
		score: score,
		explanation: strings.Join([]string{
			"topology path",
			strings.Join(path.nodeKeys, " -> "),
			"hops=" + intString(len(path.edgeTypes)),
			"edges=" + strings.Join(path.edgeTypes, ","),
			"semantics=" + strings.Join(path.semantics, ","),
			"confidence=" + scoreString(path.confidence),
		}, "; "),
		paths: []topologyPath{path},
	}
}

func bestTopologyPath(from, to map[string]struct{}, edges []model.TopologyEdge, maxDepth int) (topologyPath, bool) {
	type state struct {
		node       string
		nodes      []string
		edgeTypes  []string
		semantics  []string
		confidence float64
	}
	adjacency := map[string][]model.TopologyEdge{}
	for _, edge := range edges {
		adjacency[edge.FromNodeKey] = append(adjacency[edge.FromNodeKey], edge)
		reversed := edge
		reversed.FromNodeKey = edge.ToNodeKey
		reversed.ToNodeKey = edge.FromNodeKey
		adjacency[edge.ToNodeKey] = append(adjacency[edge.ToNodeKey], reversed)
	}
	queue := []state{}
	for key := range from {
		queue = append(queue, state{node: key, nodes: []string{key}, confidence: 1})
	}
	best := topologyPath{}
	bestScore := -1.0
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if len(current.edgeTypes) > maxDepth {
			continue
		}
		if _, ok := to[current.node]; ok && len(current.edgeTypes) > 0 {
			path := topologyPath{
				nodeKeys:   append([]string{}, current.nodes...),
				edgeTypes:  append([]string{}, current.edgeTypes...),
				semantics:  append([]string{}, current.semantics...),
				confidence: current.confidence,
			}
			score := pathScore(path)
			if score > bestScore {
				best = path
				bestScore = score
			}
			continue
		}
		if len(current.edgeTypes) == maxDepth {
			continue
		}
		visited := stringSet(current.nodes)
		for _, edge := range adjacency[current.node] {
			if _, seen := visited[edge.ToNodeKey]; seen {
				continue
			}
			queue = append(queue, state{
				node:       edge.ToNodeKey,
				nodes:      append(append([]string{}, current.nodes...), edge.ToNodeKey),
				edgeTypes:  append(append([]string{}, current.edgeTypes...), edge.EdgeType),
				semantics:  append(append([]string{}, current.semantics...), relationSemantics(edge.EdgeType)),
				confidence: current.confidence * edgeConfidence(edge),
			})
		}
	}
	return best, bestScore >= 0
}

func pathScore(path topologyPath) float64 {
	if len(path.edgeTypes) == 0 {
		return 0
	}
	base := 0.0
	observationOnly := true
	for _, edgeType := range path.edgeTypes {
		edgeBase := relationCorrelationBase(edgeType)
		if edgeBase > base {
			base = edgeBase
		}
		if relationSemantics(edgeType) != model.TopologyRelationSemanticsObservation {
			observationOnly = false
		}
	}
	hops := len(path.edgeTypes)
	hopFactor := 1.0
	if hops == 2 {
		hopFactor = 0.72
	} else if hops >= 3 {
		hopFactor = 0.55
	}
	score := base * path.confidence * hopFactor
	if observationOnly && score > 0.2 {
		score = 0.2
	}
	return roundScore(score)
}

func commonDependencySignal(left, right map[string]struct{}, edges []model.TopologyEdge) topologySignal {
	leftDeps := dependencyTargets(left, edges)
	rightDeps := dependencyTargets(right, edges)
	bestNode := ""
	bestConfidence := 0.0
	for node, leftConfidence := range leftDeps {
		if rightConfidence, ok := rightDeps[node]; ok {
			confidence := math.Min(leftConfidence, rightConfidence)
			if confidence > bestConfidence {
				bestNode = node
				bestConfidence = confidence
			}
		}
	}
	if bestNode == "" {
		return topologySignal{}
	}
	return topologySignal{
		score:       roundScore(0.65 * bestConfidence),
		explanation: "target and candidate share common dependency " + bestNode + "; confidence=" + scoreString(bestConfidence),
	}
}

func topologyPathSummaries(paths []topologyPath) []TopologyPathSummary {
	result := make([]TopologyPathSummary, 0, len(paths))
	for _, path := range paths {
		result = append(result, TopologyPathSummary{
			NodeKeys:   append([]string{}, path.nodeKeys...),
			EdgeTypes:  append([]string{}, path.edgeTypes...),
			Semantics:  append([]string{}, path.semantics...),
			Hops:       len(path.edgeTypes),
			Confidence: roundScore(path.confidence),
			ImpactType: impactTypeForPath(path),
		})
	}
	return result
}

func impactTypeForPath(path topologyPath) string {
	if len(path.semantics) == 0 {
		return "potentially_affected"
	}
	for _, semantics := range path.semantics {
		if semantics == model.TopologyRelationSemanticsObservation || semantics == model.TopologyRelationSemanticsAnnotation {
			return "potentially_related"
		}
	}
	return "potentially_affected"
}

func dependencyTargets(nodes map[string]struct{}, edges []model.TopologyEdge) map[string]float64 {
	result := map[string]float64{}
	for _, edge := range edges {
		if !relationPropagates(edge.EdgeType) {
			continue
		}
		if _, ok := nodes[edge.FromNodeKey]; ok {
			confidence := edgeConfidence(edge)
			if confidence > result[edge.ToNodeKey] {
				result[edge.ToNodeKey] = confidence
			}
		}
	}
	return result
}

func semanticScore(target, candidate model.OpsEvent) ScoreDetail {
	left := tokenSet(target.Summary + " " + target.EventType + " " + target.SourceType)
	right := tokenSet(candidate.Summary + " " + candidate.EventType + " " + candidate.SourceType)
	if len(left) == 0 || len(right) == 0 {
		return weightedDetail("semantic", 0, 0.15, "no semantic tokens")
	}
	common := 0
	for token := range left {
		if _, ok := right[token]; ok {
			common++
		}
	}
	union := len(left) + len(right) - common
	score := 0.0
	if union > 0 {
		score = float64(common) / float64(union)
	}
	return weightedDetail("semantic", score, 0.15, intString(common)+" shared tokens")
}

func evidenceScore(targetEvidence, candidateEvidence []string) ScoreDetail {
	if len(candidateEvidence) == 0 {
		return weightedDetail("evidence", 0, 0.05, "candidate has no evidence references")
	}
	if intersectsStrings(targetEvidence, candidateEvidence) {
		return weightedDetail("evidence", 1, 0.05, "target and candidate share evidence references")
	}
	return weightedDetail("evidence", 0.6, 0.05, "candidate has evidence references")
}

func weightedDetail(name string, score, weight float64, explanation string) ScoreDetail {
	score = roundScore(score)
	return ScoreDetail{Name: name, Score: score, Weight: weight, Weighted: roundScore(score * weight), Explanation: explanation}
}

func summarizeEvent(event model.OpsEvent) EventSummary {
	return EventSummary{
		ID:            event.ID,
		Time:          event.EventTime.UTC(),
		SourceType:    event.SourceType,
		EventType:     event.EventType,
		Summary:       event.Summary,
		Environment:   deref(event.Environment),
		SystemName:    deref(event.SystemName),
		ComponentName: deref(event.ComponentName),
		Namespace:     deref(event.Namespace),
		ResourceKind:  deref(event.ResourceKind),
		ResourceName:  deref(event.ResourceName),
		TraceID:       deref(event.TraceID),
	}
}

func evidenceKeysFromPayload(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	keys := normalizeKeysFromAny(payload["evidenceKeys"])
	keys = append(keys, normalizeKeysFromAny(payload["evidenceKey"])...)
	keys = append(keys, normalizeKeysFromAny(payload["evidence_refs"])...)
	return uniqueStrings(keys)
}

func normalizeKeysFromAny(value interface{}) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []interface{}:
		keys := []string{}
		for _, item := range typed {
			if key, ok := item.(string); ok {
				keys = append(keys, key)
			}
		}
		return keys
	default:
		return nil
	}
}

func matchingNodeKeys(event model.OpsEvent, nodes []model.TopologyNode) map[string]struct{} {
	result := map[string]struct{}{}
	resource := strings.ToLower(deref(event.ResourceName))
	component := strings.ToLower(deref(event.ComponentName))
	system := strings.ToLower(deref(event.SystemName))
	for _, node := range nodes {
		name := strings.ToLower(node.Name)
		key := strings.ToLower(node.NodeKey)
		if resource != "" && (name == resource || strings.Contains(key, resource)) {
			result[node.NodeKey] = struct{}{}
			continue
		}
		if component != "" && (name == component || strings.Contains(key, component)) {
			result[node.NodeKey] = struct{}{}
			continue
		}
		if system != "" && name == system {
			result[node.NodeKey] = struct{}{}
		}
	}
	return result
}

func relationSemantics(edgeType string) string {
	switch edgeType {
	case model.TopologyEdgeTypeObservedWith:
		return model.TopologyRelationSemanticsObservation
	case model.TopologyEdgeTypeOwns:
		return model.TopologyRelationSemanticsOwnership
	case model.TopologyEdgeTypeSelects, model.TopologyEdgeTypeMemberOf, model.TopologyEdgeTypeExposes:
		return model.TopologyRelationSemanticsAnnotation
	case model.TopologyEdgeTypeRunsOn:
		return model.TopologyRelationSemanticsContainment
	case model.TopologyEdgeTypeDependsOn, model.TopologyEdgeTypeStoresIn, model.TopologyEdgeTypeReplicatesTo, model.TopologyEdgeTypeConnectsTo:
		return model.TopologyRelationSemanticsRuntimeDep
	case model.TopologyEdgeTypeCalls, model.TopologyEdgeTypeRoutesTo, model.TopologyEdgeTypeRegisteredIn:
		return model.TopologyRelationSemanticsTraffic
	default:
		return model.TopologyRelationSemanticsRuntimeDep
	}
}

func relationCorrelationBase(edgeType string) float64 {
	switch relationSemantics(edgeType) {
	case model.TopologyRelationSemanticsObservation:
		return 0.2
	case model.TopologyRelationSemanticsHardDep:
		return 0.95
	case model.TopologyRelationSemanticsRuntimeDep, model.TopologyRelationSemanticsTraffic:
		return 0.9
	case model.TopologyRelationSemanticsOwnership, model.TopologyRelationSemanticsContainment:
		return 0.8
	case model.TopologyRelationSemanticsConfiguration:
		return 0.65
	case model.TopologyRelationSemanticsAnnotation:
		return 0.35
	default:
		return 0.5
	}
}

func relationPropagates(edgeType string) bool {
	switch relationSemantics(edgeType) {
	case model.TopologyRelationSemanticsObservation, model.TopologyRelationSemanticsAnnotation:
		return false
	default:
		return true
	}
}

func edgeConfidence(edge model.TopologyEdge) float64 {
	confidence := 1.0
	if edge.ResolvedConfidence != nil {
		confidence = *edge.ResolvedConfidence
	} else if edge.Confidence != nil {
		confidence = *edge.Confidence
	}
	if confidence < 0 {
		return 0
	}
	if confidence > 1 {
		return 1
	}
	return confidence
}

func directlyConnected(left, right map[string]struct{}, edges []model.TopologyEdge) bool {
	for _, edge := range edges {
		if _, ok := left[edge.FromNodeKey]; ok {
			if _, ok := right[edge.ToNodeKey]; ok {
				return true
			}
		}
		if _, ok := right[edge.FromNodeKey]; ok {
			if _, ok := left[edge.ToNodeKey]; ok {
				return true
			}
		}
	}
	return false
}

func twoHopConnected(left, right map[string]struct{}, edges []model.TopologyEdge) bool {
	midpoints := map[string]struct{}{}
	for _, edge := range edges {
		if _, ok := left[edge.FromNodeKey]; ok {
			midpoints[edge.ToNodeKey] = struct{}{}
		}
		if _, ok := left[edge.ToNodeKey]; ok {
			midpoints[edge.FromNodeKey] = struct{}{}
		}
	}
	for _, edge := range edges {
		if _, ok := midpoints[edge.FromNodeKey]; ok {
			if _, ok := right[edge.ToNodeKey]; ok {
				return true
			}
		}
		if _, ok := midpoints[edge.ToNodeKey]; ok {
			if _, ok := right[edge.FromNodeKey]; ok {
				return true
			}
		}
	}
	return false
}

func tokenSet(value string) map[string]struct{} {
	fields := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	result := map[string]struct{}{}
	for _, field := range fields {
		if len(field) < 3 {
			continue
		}
		result[field] = struct{}{}
	}
	return result
}

func confidence(score float64) string {
	switch {
	case score >= 0.8:
		return "high"
	case score >= 0.5:
		return "medium"
	default:
		return "low"
	}
}

func reasonFromDetails(details []ScoreDetail, evidenceAvailable bool) string {
	best := details[0]
	for _, detail := range details[1:] {
		if detail.Weighted > best.Weighted {
			best = detail
		}
	}
	if !evidenceAvailable {
		return "score led by " + best.Name + ", capped because candidate has no evidence reference"
	}
	return "score led by " + best.Name + " with evidence available"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func intersects(left, right map[string]struct{}) bool {
	for key := range left {
		if _, ok := right[key]; ok {
			return true
		}
	}
	return false
}

func intersectsStrings(left, right []string) bool {
	seen := map[string]struct{}{}
	for _, value := range left {
		seen[value] = struct{}{}
	}
	for _, value := range right {
		if _, ok := seen[value]; ok {
			return true
		}
	}
	return false
}

func stringSet(values []string) map[string]struct{} {
	result := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func roundScore(value float64) float64 {
	return math.Round(value*1000) / 1000
}

func scoreString(value float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", roundScore(value)), "0"), ".")
}

func intString(value int) string {
	if value == 0 {
		return "0"
	}
	digits := []byte{}
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
