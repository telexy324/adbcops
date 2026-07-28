package agentruntime

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	datasourcesvc "aiops-platform/backend/internal/datasource"
	"aiops-platform/backend/internal/model"
	topologysvc "aiops-platform/backend/internal/topology"
)

type ScopeDataSourceLister interface {
	List(ctx context.Context, actor *model.AppUser) ([]datasourcesvc.DataSourceView, error)
}

type ScopeTopologyFinder interface {
	FindNode(ctx context.Context, input topologysvc.FindNodeInput) (*topologysvc.FindNodeResult, error)
}

type NaturalLanguageScopeResolver struct {
	topology    ScopeTopologyFinder
	dataSources ScopeDataSourceLister
	window      time.Duration
	now         func() time.Time
}

func NewNaturalLanguageScopeResolver(topology ScopeTopologyFinder, dataSources ScopeDataSourceLister, window time.Duration) *NaturalLanguageScopeResolver {
	if window <= 0 {
		window = 30 * time.Minute
	}
	return &NaturalLanguageScopeResolver{
		topology: topology, dataSources: dataSources, window: window,
		now: func() time.Time { return time.Now().UTC() },
	}
}

func NewCoordinatorAgent(resolver CoordinatorScopeResolver) CoordinatorAgent {
	return CoordinatorAgent{scopeResolver: resolver}
}

func (r *NaturalLanguageScopeResolver) Resolve(ctx context.Context, actor *model.AppUser, input AgentContext) ScopeResolution {
	scope := extractCoordinatorScope(input)
	result := ScopeResolution{
		Scope: scope, Source: "natural_language", DefaultTimeWindowMinutes: int(r.window / time.Minute),
	}
	query := strings.TrimSpace(input.Query)
	if isPerformanceDegradation(query) {
		scope["symptom"] = "performance_degradation"
	}
	resolveEnvironment(scope, query)
	r.resolveTimeRange(scope, query)

	terms := serviceTerms(input, scope)
	if len(terms) > 0 && !hasExplicitValue(input, "component") && !hasExplicitValue(input, "serviceName") {
		scope["serviceQuery"] = terms[0]
	}

	if actor == nil || (r.dataSources == nil && r.topology == nil) {
		return result
	}

	views, listOK := r.accessibleDataSources(ctx, actor, &result)
	accessibleEnvironments := environmentSet(views)
	explicitEnvironment := stringValue(scope["environment"])

	var nodes []model.TopologyNode
	if r.topology != nil {
		for _, term := range terms {
			found, err := r.topology.FindNode(ctx, topologysvc.FindNodeInput{Query: term, Environment: explicitEnvironment, Limit: 20})
			if err != nil {
				result.Degraded = true
				continue
			}
			if found != nil {
				nodes = append(nodes, found.Candidates...)
			}
			if len(nodes) > 0 {
				break
			}
		}
	}
	if listOK && actor.Role != model.RoleAdmin {
		nodes = filterNodesByEnvironment(nodes, accessibleEnvironments)
	}
	nodes = uniqueNodes(nodes)
	result.CandidateCount = len(nodes)
	if len(nodes) == 1 {
		applyTopologyNode(scope, &nodes[0])
		terms = appendUnique(terms, nodes[0].Name)
	} else if len(nodes) > 1 {
		result.Ambiguous = true
		if explicitEnvironment == "" && countNodeEnvironments(nodes) > 1 {
			result.MissingParameters = appendUnique(result.MissingParameters, "environment")
		} else {
			result.MissingParameters = appendUnique(result.MissingParameters, "serviceName")
		}
	}

	if listOK {
		if id, exists := numericID(scope["dataSourceId"]); exists && !containsDataSource(views, id) {
			delete(scope, "dataSourceId")
			scope["dataSourceScopeRejected"] = true
			result.MissingParameters = appendUnique(result.MissingParameters, "dataSourceId")
		}
		matches := matchingDataSources(views, terms, stringValue(scope["environment"]))
		if len(matches) == 1 {
			applyDataSource(scope, matches[0])
			result.MissingParameters = removeString(result.MissingParameters, "dataSourceId")
		} else if len(matches) > 1 && !hasExplicitValue(input, "dataSourceId") {
			result.Ambiguous = true
			result.CandidateCount += len(matches)
			if stringValue(scope["environment"]) == "" && countDataSourceEnvironments(matches) > 1 {
				result.MissingParameters = appendUnique(result.MissingParameters, "environment")
			} else {
				result.MissingParameters = appendUnique(result.MissingParameters, "dataSourceId")
			}
		}
	}
	return result
}

func (r *NaturalLanguageScopeResolver) accessibleDataSources(ctx context.Context, actor *model.AppUser, result *ScopeResolution) ([]datasourcesvc.DataSourceView, bool) {
	if r.dataSources == nil {
		return nil, false
	}
	views, err := r.dataSources.List(ctx, actor)
	if err != nil {
		result.Degraded = true
		return nil, false
	}
	filtered := views[:0]
	for _, view := range views {
		if view.Enabled && view.ReadOnly {
			filtered = append(filtered, view)
		}
	}
	return filtered, true
}

func (r *NaturalLanguageScopeResolver) resolveTimeRange(scope map[string]any, query string) {
	now := r.now().UTC()
	fromValue, hasFrom := scope["from"]
	toValue, hasTo := scope["to"]
	if hasFrom && !isZeroVariable(fromValue) && hasTo && !isZeroVariable(toValue) {
		scope["timeRangeSource"] = "explicit"
		return
	}
	duration, parsed := parseRelativeDuration(query)
	if !parsed {
		duration = r.window
	}
	if !hasTo || isZeroVariable(toValue) {
		scope["to"] = now.Format(time.RFC3339)
	}
	if !hasFrom || isZeroVariable(fromValue) {
		scope["from"] = now.Add(-duration).Format(time.RFC3339)
	}
	if parsed {
		scope["timeRangeSource"] = "natural_language"
	} else {
		scope["timeRangeSource"] = "default"
		scope["defaultTimeWindowMinutes"] = int(r.window / time.Minute)
	}
}

func isPerformanceDegradation(query string) bool {
	text := strings.ToLower(query)
	return hasAny(text, "变慢", "响应慢", "耗时增加", "耗时升高", "延迟升高", "延迟增加", "卡顿",
		"slow", "slower", "latency increased", "latency spike", "high latency", "response time increased")
}

func resolveEnvironment(scope map[string]any, query string) {
	if value, ok := scope["environment"]; ok && !isZeroVariable(value) {
		scope["environment"] = normalizeEnvironment(stringValue(value))
		return
	}
	text := strings.ToLower(query)
	for _, item := range []struct {
		canonical string
		terms     []string
	}{
		{"prod", []string{"生产环境", "生产", "production", "prod"}},
		{"staging", []string{"预发环境", "预发", "staging", "stage"}},
		{"test", []string{"测试环境", "测试", "test", "qa"}},
		{"dev", []string{"开发环境", "开发", "development", " dev "}},
	} {
		for _, term := range item.terms {
			if strings.Contains(text, term) {
				scope["environment"] = item.canonical
				return
			}
		}
	}
}

func normalizeEnvironment(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "生产", "生产环境", "production", "prd":
		return "prod"
	case "预发", "预发环境", "stage":
		return "staging"
	case "测试", "测试环境", "qa":
		return "test"
	case "开发", "开发环境", "development":
		return "dev"
	default:
		return strings.TrimSpace(value)
	}
}

var (
	chineseServicePattern = regexp.MustCompile(`([\p{Han}]{1,16}服务)(?:变慢|响应慢|耗时增加|耗时升高|延迟升高|延迟增加|卡顿)`)
	englishSlowPattern    = regexp.MustCompile(`(?i)\b([a-z0-9][a-z0-9._-]{1,80})(?:\s+service)?\s+(?:is\s+)?(?:slow|slower|degraded)`)
	environmentSvcPattern = regexp.MustCompile(`(?i)(?:生产|预发|测试|开发|prod(?:uction)?|staging|test|dev(?:elopment)?)\s+([a-z0-9][a-z0-9._-]{1,80})`)
)

func serviceTerms(input AgentContext, scope map[string]any) []string {
	terms := []string{}
	for _, key := range []string{"serviceName", "component"} {
		if value := stringValue(scope[key]); value != "" {
			terms = append(terms, value)
		}
	}
	if match := chineseServicePattern.FindStringSubmatch(input.Query); len(match) == 2 {
		value := strings.TrimPrefix(match[1], "生产")
		value = strings.TrimPrefix(value, "预发")
		value = strings.TrimPrefix(value, "测试")
		value = strings.TrimPrefix(value, "开发")
		terms = append(terms, value, strings.TrimSuffix(value, "服务"))
	}
	for _, pattern := range []*regexp.Regexp{englishSlowPattern, environmentSvcPattern} {
		if match := pattern.FindStringSubmatch(input.Query); len(match) == 2 {
			terms = append(terms, match[1])
		}
	}
	return uniqueStrings(terms)
}

func parseRelativeDuration(query string) (time.Duration, bool) {
	text := strings.ToLower(strings.TrimSpace(query))
	if hasAny(text, "最近半小时", "近半小时", "过去半小时", "last half hour") {
		return 30 * time.Minute, true
	}
	patterns := []struct {
		re   *regexp.Regexp
		unit time.Duration
	}{
		{regexp.MustCompile(`(?:最近|近|过去)\s*(\d+)\s*分钟`), time.Minute},
		{regexp.MustCompile(`(?:最近|近|过去)\s*(\d+)\s*(?:小时|时)`), time.Hour},
		{regexp.MustCompile(`(?:最近|近|过去)\s*(\d+)\s*天`), 24 * time.Hour},
		{regexp.MustCompile(`(?i)(?:last|past)\s+(\d+)\s*(?:minutes?|mins?)`), time.Minute},
		{regexp.MustCompile(`(?i)(?:last|past)\s+(\d+)\s*(?:hours?|hrs?)`), time.Hour},
	}
	for _, pattern := range patterns {
		if match := pattern.re.FindStringSubmatch(text); len(match) == 2 {
			value, _ := strconv.Atoi(match[1])
			if value > 0 {
				return time.Duration(value) * pattern.unit, true
			}
		}
	}
	if hasAny(text, "最近一小时", "近一小时", "过去一小时", "last hour") {
		return time.Hour, true
	}
	return 0, false
}

func matchingDataSources(views []datasourcesvc.DataSourceView, terms []string, environment string) []datasourcesvc.DataSourceView {
	matches := []datasourcesvc.DataSourceView{}
	for _, view := range views {
		if environment != "" && normalizeEnvironment(stringValue(view.Environment)) != normalizeEnvironment(environment) {
			continue
		}
		labels := []string{view.Name, stringValue(view.SystemName), stringValue(view.ComponentName)}
		matched := false
		for _, term := range terms {
			for _, label := range labels {
				if fuzzyLabelMatch(term, label) {
					matched = true
					break
				}
			}
		}
		if matched {
			matches = append(matches, view)
		}
	}
	return matches
}

func fuzzyLabelMatch(left, right string) bool {
	left = normalizeLabel(left)
	right = normalizeLabel(right)
	return left != "" && right != "" && (left == right || strings.Contains(left, right) || strings.Contains(right, left))
}

func normalizeLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimSuffix(value, "服务")
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, value)
}

func applyTopologyNode(scope map[string]any, node *model.TopologyNode) {
	if node == nil {
		return
	}
	if _, ok := scope["serviceName"]; !ok {
		scope["serviceName"] = node.Name
	}
	scope["topologyNodeKey"] = node.NodeKey
	if node.Environment != nil {
		if _, ok := scope["environment"]; !ok {
			scope["environment"] = normalizeEnvironment(*node.Environment)
		}
	}
}

func applyDataSource(scope map[string]any, view datasourcesvc.DataSourceView) {
	if _, ok := scope["dataSourceId"]; !ok {
		scope["dataSourceId"] = view.ID
	}
	if _, ok := scope["environment"]; !ok && view.Environment != nil {
		scope["environment"] = normalizeEnvironment(*view.Environment)
	}
	if _, ok := scope["component"]; !ok && view.ComponentName != nil {
		scope["component"] = *view.ComponentName
	}
}

func hasExplicitValue(input AgentContext, key string) bool {
	for _, values := range []map[string]any{input.Scope, input.Variables} {
		if value, ok := values[key]; ok && !isZeroVariable(value) {
			return true
		}
	}
	return false
}

func filterNodesByEnvironment(nodes []model.TopologyNode, allowed map[string]struct{}) []model.TopologyNode {
	filtered := make([]model.TopologyNode, 0, len(nodes))
	for _, node := range nodes {
		if node.Environment == nil {
			continue
		}
		if _, ok := allowed[normalizeEnvironment(*node.Environment)]; ok {
			filtered = append(filtered, node)
		}
	}
	return filtered
}

func environmentSet(views []datasourcesvc.DataSourceView) map[string]struct{} {
	result := map[string]struct{}{}
	for _, view := range views {
		if view.Environment != nil {
			result[normalizeEnvironment(*view.Environment)] = struct{}{}
		}
	}
	return result
}

func uniqueNodes(nodes []model.TopologyNode) []model.TopologyNode {
	seen := map[string]model.TopologyNode{}
	for _, node := range nodes {
		seen[node.NodeKey] = node
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]model.TopologyNode, 0, len(keys))
	for _, key := range keys {
		result = append(result, seen[key])
	}
	return result
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func removeString(values []string, target string) []string {
	result := values[:0]
	for _, value := range values {
		if value != target {
			result = append(result, value)
		}
	}
	return result
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case *string:
		if typed != nil {
			return strings.TrimSpace(*typed)
		}
	}
	return ""
}

func numericID(value any) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, typed > 0
	case int:
		return int64(typed), typed > 0
	case float64:
		return int64(typed), typed > 0 && typed == float64(int64(typed))
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed, err == nil && parsed > 0
	default:
		return 0, false
	}
}

func containsDataSource(views []datasourcesvc.DataSourceView, id int64) bool {
	for _, view := range views {
		if view.ID == id {
			return true
		}
	}
	return false
}

func countNodeEnvironments(nodes []model.TopologyNode) int {
	values := map[string]struct{}{}
	for _, node := range nodes {
		if node.Environment != nil {
			values[normalizeEnvironment(*node.Environment)] = struct{}{}
		}
	}
	return len(values)
}

func countDataSourceEnvironments(views []datasourcesvc.DataSourceView) int {
	values := map[string]struct{}{}
	for _, view := range views {
		if view.Environment != nil {
			values[normalizeEnvironment(*view.Environment)] = struct{}{}
		}
	}
	return len(values)
}
