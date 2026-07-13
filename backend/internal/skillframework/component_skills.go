package skillframework

import (
	"context"
	"encoding/json"
	"strings"

	"aiops-platform/backend/internal/model"
	nacossvc "aiops-platform/backend/internal/nacos"
	nginxsvc "aiops-platform/backend/internal/nginx"
	redissvc "aiops-platform/backend/internal/redis"
	tidbsvc "aiops-platform/backend/internal/tidb"
)

func ComponentDiagnosisSkills() []Skill {
	return ComponentDiagnosisSkillsWithServices(nil, nil, nil, nil)
}

func ComponentDiagnosisSkillsWithNacos(nacos NacosQuerier) []Skill {
	return ComponentDiagnosisSkillsWithServices(nacos, nil, nil, nil)
}

func ComponentDiagnosisSkillsWithServices(nacos NacosQuerier, redis RedisQuerier, tidb TiDBQuerier, nginx NginxQuerier) []Skill {
	skills := NacosSkills(nacos)
	skills = append(skills, RedisSkills(redis)...)
	skills = append(skills, TiDBSkills(tidb)...)
	skills = append(skills, NginxSkills(nginx)...)
	names := []struct {
		name        string
		description string
		tool        string
	}{}
	for _, item := range names {
		skills = append(skills, ComponentSkill{name: item.name, description: item.description, requiredTool: item.tool})
	}
	return skills
}

type NacosQuerier interface {
	ListServices(ctx context.Context, actor *model.AppUser, input nacossvc.ListServicesInput) (*nacossvc.ServiceListResult, error)
	ListInstances(ctx context.Context, actor *model.AppUser, input nacossvc.ListInstancesInput) (*nacossvc.InstanceListResult, error)
	GetConfigMetadata(ctx context.Context, actor *model.AppUser, input nacossvc.ConfigMetadataInput) (*nacossvc.ConfigMetadataResult, error)
	ListConfigChanges(ctx context.Context, actor *model.AppUser, input nacossvc.ConfigHistoryInput) (*nacossvc.ConfigHistoryResult, error)
	ListClientConnections(ctx context.Context, actor *model.AppUser, input nacossvc.ClientConnectionsInput) (*nacossvc.ClientConnectionsResult, error)
	ListListeners(ctx context.Context, actor *model.AppUser, input nacossvc.ListenersInput) (*nacossvc.ListenersResult, error)
}

func NacosSkills(nacos NacosQuerier) []Skill {
	return []Skill{
		NacosSkill{name: "query_nacos_services", description: "Query Nacos service metadata in allowed namespaces and groups.", nacos: nacos},
		NacosSkill{name: "get_nacos_service_instances", description: "Query Nacos service instances and health state.", nacos: nacos},
		NacosSkill{name: "query_nacos_config_metadata", description: "Query Nacos configuration metadata without sensitive content.", nacos: nacos},
		NacosSkill{name: "query_nacos_config_changes", description: "Query Nacos configuration change history.", nacos: nacos},
		NacosSkill{name: "query_nacos_client_connections", description: "Query Nacos client connection and listener summaries.", nacos: nacos},
		NacosSkill{name: "diagnose_nacos_registration", description: "Diagnose Nacos service registration and instance health problems.", nacos: nacos},
		NacosSkill{name: "diagnose_nacos_config_delivery", description: "Diagnose Nacos configuration delivery and listener problems.", nacos: nacos},
	}
}

type NacosSkill struct {
	name        string
	description string
	nacos       NacosQuerier
}

type nacosSkillRequest struct {
	DataSourceID int64  `json:"dataSourceId"`
	Namespace    string `json:"namespace"`
	Group        string `json:"group"`
	ServiceName  string `json:"serviceName"`
	DataID       string `json:"dataId"`
	PageNo       int    `json:"pageNo"`
	PageSize     int    `json:"pageSize"`
	Limit        int    `json:"limit"`
}

func (s NacosSkill) Definition() SkillDefinition {
	return SkillDefinition{
		Name:          s.name,
		Version:       "v1",
		Description:   s.description,
		InputSchema:   json.RawMessage(`{"type":"object","required":["dataSourceId"],"properties":{"dataSourceId":{"type":"integer"},"namespace":{"type":"string"},"group":{"type":"string"},"serviceName":{"type":"string"},"dataId":{"type":"string"},"pageNo":{"type":"integer"},"pageSize":{"type":"integer"},"limit":{"type":"integer"}}}`),
		OutputSchema:  partialOutputSchema(),
		RiskLevel:     model.SkillRiskSensitiveRead,
		ReadOnly:      true,
		TimeoutSecond: 30,
		RequiredTools: []string{"nacos"},
	}
}

func (s NacosSkill) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var request nacosSkillRequest
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, ErrInvalidInput
	}
	if s.nacos == nil {
		return partialError("nacos", "nacos service is not configured"), nil
	}
	actor := ActorFromContext(ctx)
	scope := nacossvc.QueryScope{Namespace: request.Namespace, Group: request.Group}
	switch s.name {
	case "query_nacos_services":
		result, err := s.nacos.ListServices(ctx, actor, nacossvc.ListServicesInput{DataSourceID: request.DataSourceID, QueryScope: scope, PageNo: request.PageNo, PageSize: request.PageSize})
		if err != nil {
			return partialError("nacos", err.Error()), nil
		}
		return nacosOutput("query_nacos_services", "FACT", "nacos service list queried inside allowed namespace/group", result, nil)
	case "get_nacos_service_instances":
		result, err := s.nacos.ListInstances(ctx, actor, nacossvc.ListInstancesInput{DataSourceID: request.DataSourceID, QueryScope: scope, ServiceName: request.ServiceName})
		if err != nil {
			return partialError("nacos", err.Error()), nil
		}
		return nacosOutput("get_nacos_service_instances", "FACT", "nacos service instances queried with health state", result, nil)
	case "query_nacos_config_metadata":
		result, err := s.nacos.GetConfigMetadata(ctx, actor, nacossvc.ConfigMetadataInput{DataSourceID: request.DataSourceID, QueryScope: scope, DataID: request.DataID})
		if err != nil {
			return partialError("nacos", err.Error()), nil
		}
		return nacosOutput("query_nacos_config_metadata", "FACT", "nacos config metadata queried without config content", result, []string{"config content is intentionally not returned"})
	case "query_nacos_config_changes":
		result, err := s.nacos.ListConfigChanges(ctx, actor, nacossvc.ConfigHistoryInput{DataSourceID: request.DataSourceID, QueryScope: scope, DataID: request.DataID, Limit: request.Limit})
		if err != nil {
			return partialError("nacos", err.Error()), nil
		}
		return nacosOutput("query_nacos_config_changes", "FACT", "nacos config change history queried for timeline evidence", result, nil)
	case "query_nacos_client_connections":
		connections, err := s.nacos.ListClientConnections(ctx, actor, nacossvc.ClientConnectionsInput{DataSourceID: request.DataSourceID, QueryScope: scope, ServiceName: request.ServiceName, PageNo: request.PageNo, PageSize: request.PageSize})
		if err != nil {
			return partialError("nacos", err.Error()), nil
		}
		listeners, err := s.nacos.ListListeners(ctx, actor, nacossvc.ListenersInput{DataSourceID: request.DataSourceID, QueryScope: scope, DataID: request.DataID, Limit: request.Limit})
		if err != nil {
			return partialError("nacos", err.Error()), nil
		}
		return nacosOutput("query_nacos_client_connections", "FACT", "nacos client connections and listeners queried", map[string]any{"connections": connections, "listeners": listeners}, nil)
	case "diagnose_nacos_registration":
		return s.diagnoseRegistration(ctx, actor, request, scope)
	case "diagnose_nacos_config_delivery":
		return s.diagnoseConfigDelivery(ctx, actor, request, scope)
	default:
		return partialError("nacos", "unsupported nacos skill"), nil
	}
}

func (s NacosSkill) diagnoseRegistration(ctx context.Context, actor *model.AppUser, request nacosSkillRequest, scope nacossvc.QueryScope) (json.RawMessage, error) {
	services, err := s.nacos.ListServices(ctx, actor, nacossvc.ListServicesInput{DataSourceID: request.DataSourceID, QueryScope: scope, PageNo: request.PageNo, PageSize: request.PageSize})
	if err != nil {
		return partialError("nacos", err.Error()), nil
	}
	var instances *nacossvc.InstanceListResult
	missing := []string{}
	if strings.TrimSpace(request.ServiceName) == "" {
		missing = append(missing, "serviceName is required to inspect instance health")
	} else {
		instances, err = s.nacos.ListInstances(ctx, actor, nacossvc.ListInstancesInput{DataSourceID: request.DataSourceID, QueryScope: scope, ServiceName: request.ServiceName})
		if err != nil {
			return partialError("nacos", err.Error()), nil
		}
	}
	facts := []map[string]any{nacosFact("FACT", "nacos services were visible in the requested namespace/group", "query_nacos_services", services)}
	if instances != nil {
		unhealthy := 0
		for _, item := range instances.Instances {
			if !item.Healthy || !item.Enabled {
				unhealthy++
			}
		}
		severity := "info"
		summary := "all returned nacos instances are healthy and enabled"
		if len(instances.Instances) == 0 {
			severity = "warning"
			summary = "no nacos instances returned for service"
		} else if unhealthy > 0 {
			severity = "warning"
			summary = "nacos returned unhealthy or disabled service instances"
		}
		facts = append(facts, nacosFact("RULE", summary, "get_nacos_service_instances", map[string]any{"severity": severity, "unhealthy": unhealthy, "instances": instances}))
	}
	return json.Marshal(map[string]any{"partial": false, "skill": "diagnose_nacos_registration", "facts": facts, "missingEvidence": missing})
}

func (s NacosSkill) diagnoseConfigDelivery(ctx context.Context, actor *model.AppUser, request nacosSkillRequest, scope nacossvc.QueryScope) (json.RawMessage, error) {
	if strings.TrimSpace(request.DataID) == "" {
		return json.Marshal(map[string]any{
			"partial":         false,
			"skill":           "diagnose_nacos_config_delivery",
			"facts":           []map[string]any{nacosFact("RULE", "dataId is required to diagnose config delivery", "skill.input", nil)},
			"missingEvidence": []string{"dataId", "config metadata", "config change history", "listener list"},
		})
	}
	metadata, err := s.nacos.GetConfigMetadata(ctx, actor, nacossvc.ConfigMetadataInput{DataSourceID: request.DataSourceID, QueryScope: scope, DataID: request.DataID})
	if err != nil {
		return partialError("nacos", err.Error()), nil
	}
	changes, err := s.nacos.ListConfigChanges(ctx, actor, nacossvc.ConfigHistoryInput{DataSourceID: request.DataSourceID, QueryScope: scope, DataID: request.DataID, Limit: request.Limit})
	if err != nil {
		return partialError("nacos", err.Error()), nil
	}
	listeners, err := s.nacos.ListListeners(ctx, actor, nacossvc.ListenersInput{DataSourceID: request.DataSourceID, QueryScope: scope, DataID: request.DataID, Limit: request.Limit})
	if err != nil {
		return partialError("nacos", err.Error()), nil
	}
	missing := []string{}
	if len(listeners.Listeners) == 0 {
		missing = append(missing, "no config listener was returned for dataId")
	}
	facts := []map[string]any{
		nacosFact("FACT", "nacos config metadata was read without config content", "query_nacos_config_metadata", metadata),
		nacosFact("FACT", "nacos config changes can be used as timeline evidence", "query_nacos_config_changes", changes),
		nacosFact("RULE", "config delivery depends on listener presence for the same namespace/group/dataId", "query_nacos_client_connections", listeners),
	}
	return json.Marshal(map[string]any{"partial": false, "skill": "diagnose_nacos_config_delivery", "facts": facts, "missingEvidence": missing})
}

func nacosOutput(skill string, factType string, summary string, evidence any, missing []string) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"partial":         false,
		"skill":           skill,
		"facts":           []map[string]any{nacosFact(factType, summary, skill, evidence)},
		"missingEvidence": missing,
	})
}

func nacosFact(factType string, summary string, evidenceRef string, evidence any) map[string]any {
	return map[string]any{
		"type":        factType,
		"summary":     summary,
		"evidenceRef": evidenceRef,
		"evidence":    evidence,
	}
}

type RedisQuerier interface {
	Info(ctx context.Context, actor *model.AppUser, input redissvc.InfoInput) (*redissvc.InfoResult, error)
	MemoryStats(ctx context.Context, actor *model.AppUser, input redissvc.QueryInput) (*redissvc.MemoryStatsResult, error)
	ClientListSummary(ctx context.Context, actor *model.AppUser, input redissvc.QueryInput) (*redissvc.ClientSummaryResult, error)
	SlowLog(ctx context.Context, actor *model.AppUser, input redissvc.SlowLogInput) (*redissvc.SlowLogResult, error)
	Replication(ctx context.Context, actor *model.AppUser, input redissvc.QueryInput) (*redissvc.ReplicationResult, error)
	ClusterState(ctx context.Context, actor *model.AppUser, input redissvc.QueryInput) (*redissvc.ClusterResult, error)
	SentinelState(ctx context.Context, actor *model.AppUser, input redissvc.QueryInput) (*redissvc.SentinelResult, error)
	LatencyLatest(ctx context.Context, actor *model.AppUser, input redissvc.QueryInput) (*redissvc.LatencyResult, error)
	ScanSummary(ctx context.Context, actor *model.AppUser, input redissvc.ScanInput) (*redissvc.ScanSummaryResult, error)
}

func RedisSkills(redis RedisQuerier) []Skill {
	return []Skill{
		RedisSkill{name: "query_redis_info", description: "Query Redis INFO summary.", redis: redis},
		RedisSkill{name: "query_redis_memory", description: "Query Redis memory statistics.", redis: redis},
		RedisSkill{name: "query_redis_clients", description: "Query Redis client summary with sensitive fields masked.", redis: redis},
		RedisSkill{name: "query_redis_slowlog", description: "Query Redis slowlog within configured limits.", redis: redis},
		RedisSkill{name: "query_redis_keyspace", description: "Query Redis keyspace summary without reading values.", redis: redis},
		RedisSkill{name: "query_redis_replication", description: "Query Redis replication state.", redis: redis},
		RedisSkill{name: "query_redis_cluster", description: "Query Redis Cluster or Sentinel state.", redis: redis},
		RedisSkill{name: "query_redis_latency", description: "Query Redis latency latest summary.", redis: redis},
		RedisSkill{name: "diagnose_redis_health", description: "Diagnose Redis health from clients, memory, slowlog, replication and cluster evidence.", redis: redis},
		RedisSkill{name: "diagnose_redis_memory", description: "Diagnose Redis memory pressure and fragmentation.", redis: redis},
		RedisSkill{name: "diagnose_redis_connection_pool", description: "Diagnose Redis connection pool pressure.", redis: redis},
		RedisSkill{name: "diagnose_redis_replication", description: "Diagnose Redis replication problems.", redis: redis},
		RedisSkill{name: "diagnose_redis_cluster", description: "Diagnose Redis Cluster slot and node problems.", redis: redis},
	}
}

type RedisSkill struct {
	name        string
	description string
	redis       RedisQuerier
}

type redisSkillRequest struct {
	DataSourceID  int64    `json:"dataSourceId"`
	Sections      []string `json:"sections"`
	Limit         int      `json:"limit"`
	Match         string   `json:"match"`
	Count         int      `json:"count"`
	MaxIterations int      `json:"maxIterations"`
	MaxKeys       int      `json:"maxKeys"`
}

func (s RedisSkill) Definition() SkillDefinition {
	return SkillDefinition{
		Name:          s.name,
		Version:       "v1",
		Description:   s.description,
		InputSchema:   json.RawMessage(`{"type":"object","required":["dataSourceId"],"properties":{"dataSourceId":{"type":"integer"},"sections":{"type":"array","items":{"type":"string"}},"limit":{"type":"integer"},"match":{"type":"string"},"count":{"type":"integer"},"maxIterations":{"type":"integer"},"maxKeys":{"type":"integer"}}}`),
		OutputSchema:  partialOutputSchema(),
		RiskLevel:     model.SkillRiskSensitiveRead,
		ReadOnly:      true,
		TimeoutSecond: 30,
		RequiredTools: []string{"redis"},
	}
}

func (s RedisSkill) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var request redisSkillRequest
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, ErrInvalidInput
	}
	if s.redis == nil {
		return partialError("redis", "redis service is not configured"), nil
	}
	actor := ActorFromContext(ctx)
	switch s.name {
	case "query_redis_info":
		result, err := s.redis.Info(ctx, actor, redissvc.InfoInput{DataSourceID: request.DataSourceID, Sections: request.Sections})
		return redisQueryOutput("query_redis_info", "Redis INFO summary queried", result, err)
	case "query_redis_memory":
		result, err := s.redis.MemoryStats(ctx, actor, redissvc.QueryInput{DataSourceID: request.DataSourceID})
		return redisQueryOutput("query_redis_memory", "Redis MEMORY STATS queried", result, err)
	case "query_redis_clients":
		result, err := s.redis.ClientListSummary(ctx, actor, redissvc.QueryInput{DataSourceID: request.DataSourceID})
		return redisQueryOutput("query_redis_clients", "Redis CLIENT LIST summary queried with sensitive fields masked", result, err)
	case "query_redis_slowlog":
		result, err := s.redis.SlowLog(ctx, actor, redissvc.SlowLogInput{DataSourceID: request.DataSourceID, Limit: request.Limit})
		return redisQueryOutput("query_redis_slowlog", "Redis SLOWLOG queried with command args redacted", result, err)
	case "query_redis_keyspace":
		result, err := s.redis.ScanSummary(ctx, actor, redissvc.ScanInput{DataSourceID: request.DataSourceID, Match: request.Match, Count: request.Count, MaxIterations: request.MaxIterations, MaxKeys: request.MaxKeys})
		return redisQueryOutput("query_redis_keyspace", "Redis keyspace summarized with SCAN without reading values", result, err)
	case "query_redis_replication":
		result, err := s.redis.Replication(ctx, actor, redissvc.QueryInput{DataSourceID: request.DataSourceID})
		return redisQueryOutput("query_redis_replication", "Redis ROLE replication state queried", result, err)
	case "query_redis_cluster":
		cluster, clusterErr := s.redis.ClusterState(ctx, actor, redissvc.QueryInput{DataSourceID: request.DataSourceID})
		sentinel, sentinelErr := s.redis.SentinelState(ctx, actor, redissvc.QueryInput{DataSourceID: request.DataSourceID})
		if clusterErr != nil && sentinelErr != nil {
			return partialError("redis", clusterErr.Error()+"; "+sentinelErr.Error()), nil
		}
		return redisOutput("query_redis_cluster", "FACT", "Redis Cluster/Sentinel state queried with node source labels", map[string]any{"cluster": cluster, "sentinel": sentinel}, []string{})
	case "query_redis_latency":
		result, err := s.redis.LatencyLatest(ctx, actor, redissvc.QueryInput{DataSourceID: request.DataSourceID})
		return redisQueryOutput("query_redis_latency", "Redis LATENCY LATEST queried", result, err)
	case "diagnose_redis_health":
		return s.diagnoseHealth(ctx, actor, request)
	case "diagnose_redis_memory":
		result, err := s.redis.MemoryStats(ctx, actor, redissvc.QueryInput{DataSourceID: request.DataSourceID})
		return redisDiagnosisOutput("diagnose_redis_memory", "Redis memory pressure diagnosis requires used_memory, maxmemory and fragmentation evidence", "query_redis_memory", result, err)
	case "diagnose_redis_connection_pool":
		result, err := s.redis.ClientListSummary(ctx, actor, redissvc.QueryInput{DataSourceID: request.DataSourceID})
		return redisDiagnosisOutput("diagnose_redis_connection_pool", "Redis connection pool pressure diagnosis uses CLIENT LIST counts and commands", "query_redis_clients", result, err)
	case "diagnose_redis_replication":
		result, err := s.redis.Replication(ctx, actor, redissvc.QueryInput{DataSourceID: request.DataSourceID})
		return redisDiagnosisOutput("diagnose_redis_replication", "Redis replication diagnosis uses ROLE output by source node", "query_redis_replication", result, err)
	case "diagnose_redis_cluster":
		result, err := s.redis.ClusterState(ctx, actor, redissvc.QueryInput{DataSourceID: request.DataSourceID})
		return redisDiagnosisOutput("diagnose_redis_cluster", "Redis cluster diagnosis uses CLUSTER INFO/NODES and tolerates partial node failure", "query_redis_cluster", result, err)
	default:
		return partialError("redis", "unsupported redis skill"), nil
	}
}

func (s RedisSkill) diagnoseHealth(ctx context.Context, actor *model.AppUser, request redisSkillRequest) (json.RawMessage, error) {
	info, infoErr := s.redis.Info(ctx, actor, redissvc.InfoInput{DataSourceID: request.DataSourceID})
	clients, clientErr := s.redis.ClientListSummary(ctx, actor, redissvc.QueryInput{DataSourceID: request.DataSourceID})
	slowlog, slowErr := s.redis.SlowLog(ctx, actor, redissvc.SlowLogInput{DataSourceID: request.DataSourceID, Limit: request.Limit})
	if infoErr != nil && clientErr != nil && slowErr != nil {
		return partialError("redis", infoErr.Error()+"; "+clientErr.Error()+"; "+slowErr.Error()), nil
	}
	facts := []map[string]any{
		redisFact("FACT", "Redis INFO evidence collected by source node", "query_redis_info", info),
		redisFact("FACT", "Redis client evidence collected with sensitive fields masked", "query_redis_clients", clients),
		redisFact("RULE", "Redis health diagnosis should correlate INFO, CLIENT LIST and SLOWLOG before suggesting action", "query_redis_slowlog", slowlog),
		redisFact("RULE", "delete/cleanup/resize actions are high-risk recommendations and are not auto-executed", "skill.safety", map[string]string{"risk": "high"}),
	}
	missing := []string{}
	if infoErr != nil {
		missing = append(missing, "redis info: "+infoErr.Error())
	}
	if clientErr != nil {
		missing = append(missing, "redis clients: "+clientErr.Error())
	}
	if slowErr != nil {
		missing = append(missing, "redis slowlog: "+slowErr.Error())
	}
	return json.Marshal(map[string]any{"partial": len(missing) > 0, "skill": "diagnose_redis_health", "facts": facts, "missingEvidence": missing})
}

func redisQueryOutput(skill string, summary string, evidence any, err error) (json.RawMessage, error) {
	if err != nil {
		return partialError("redis", err.Error()), nil
	}
	return redisOutput(skill, "FACT", summary, evidence, []string{})
}

func redisDiagnosisOutput(skill string, summary string, evidenceRef string, evidence any, err error) (json.RawMessage, error) {
	if err != nil {
		return partialError("redis", err.Error()), nil
	}
	return redisOutput(skill, "RULE", summary, evidence, []string{"business values are intentionally not read"})
}

func redisOutput(skill string, factType string, summary string, evidence any, missing []string) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"partial":         false,
		"skill":           skill,
		"facts":           []map[string]any{redisFact(factType, summary, skill, evidence)},
		"missingEvidence": missing,
	})
}

func redisFact(factType string, summary string, evidenceRef string, evidence any) map[string]any {
	return map[string]any{
		"type":        factType,
		"summary":     summary,
		"evidenceRef": evidenceRef,
		"evidence":    evidence,
	}
}

type TiDBQuerier interface {
	QueryClusterStatus(ctx context.Context, actor *model.AppUser, input tidbsvc.QueryInput) (*tidbsvc.QueryResult, error)
	QuerySlowQueries(ctx context.Context, actor *model.AppUser, input tidbsvc.SlowQueryInput) (*tidbsvc.QueryResult, error)
	QueryProcessList(ctx context.Context, actor *model.AppUser, input tidbsvc.QueryInput) (*tidbsvc.QueryResult, error)
	QueryLockWaits(ctx context.Context, actor *model.AppUser, input tidbsvc.QueryInput) (*tidbsvc.QueryResult, error)
	QueryHotRegions(ctx context.Context, actor *model.AppUser, input tidbsvc.QueryInput) (*tidbsvc.QueryResult, error)
	QueryStatisticsHealth(ctx context.Context, actor *model.AppUser, input tidbsvc.QueryInput) (*tidbsvc.QueryResult, error)
	Explain(ctx context.Context, actor *model.AppUser, input tidbsvc.ExplainInput) (*tidbsvc.ExplainResult, error)
}

func TiDBSkills(tidb TiDBQuerier) []Skill {
	return []Skill{
		TiDBSkill{name: "query_tidb_cluster_status", description: "Query TiDB cluster status.", tidb: tidb},
		TiDBSkill{name: "query_tidb_metrics", description: "Query TiDB related metrics.", tidb: tidb},
		TiDBSkill{name: "query_tidb_slow_queries", description: "Query TiDB slow SQL summaries.", tidb: tidb},
		TiDBSkill{name: "query_tidb_processlist", description: "Query TiDB processlist summary.", tidb: tidb},
		TiDBSkill{name: "query_tidb_lock_waits", description: "Query TiDB lock waits.", tidb: tidb},
		TiDBSkill{name: "query_tidb_hot_regions", description: "Query TiDB hot region summary.", tidb: tidb},
		TiDBSkill{name: "query_tidb_statistics_health", description: "Query TiDB statistics health.", tidb: tidb},
		TiDBSkill{name: "explain_tidb_sql", description: "Run controlled TiDB EXPLAIN after read-only SQL validation.", tidb: tidb},
		TiDBSkill{name: "diagnose_tidb_performance", description: "Diagnose TiDB performance degradation.", tidb: tidb},
		TiDBSkill{name: "diagnose_tidb_connection_pressure", description: "Diagnose TiDB connection pressure.", tidb: tidb},
		TiDBSkill{name: "diagnose_tidb_lock_contention", description: "Diagnose TiDB lock contention.", tidb: tidb},
		TiDBSkill{name: "diagnose_tidb_plan_regression", description: "Diagnose TiDB execution plan regression.", tidb: tidb},
	}
}

type TiDBSkill struct {
	name        string
	description string
	tidb        TiDBQuerier
}

type tidbSkillRequest struct {
	DataSourceID int64  `json:"dataSourceId"`
	Limit        int    `json:"limit"`
	Minutes      int    `json:"minutes"`
	SQL          string `json:"sql"`
	Analyze      bool   `json:"analyze"`
}

func (s TiDBSkill) Definition() SkillDefinition {
	return SkillDefinition{
		Name:          s.name,
		Version:       "v1",
		Description:   s.description,
		InputSchema:   json.RawMessage(`{"type":"object","required":["dataSourceId"],"properties":{"dataSourceId":{"type":"integer"},"limit":{"type":"integer"},"minutes":{"type":"integer"},"sql":{"type":"string"},"analyze":{"type":"boolean"}}}`),
		OutputSchema:  partialOutputSchema(),
		RiskLevel:     model.SkillRiskSensitiveRead,
		ReadOnly:      true,
		TimeoutSecond: 30,
		RequiredTools: []string{"tidb"},
	}
}

func (s TiDBSkill) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var request tidbSkillRequest
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, ErrInvalidInput
	}
	if s.tidb == nil {
		return partialError("tidb", "tidb service is not configured"), nil
	}
	actor := ActorFromContext(ctx)
	queryInput := tidbsvc.QueryInput{DataSourceID: request.DataSourceID, Limit: request.Limit}
	switch s.name {
	case "query_tidb_cluster_status", "query_tidb_metrics":
		result, err := s.tidb.QueryClusterStatus(ctx, actor, queryInput)
		return tidbQueryOutput(s.name, "TiDB cluster status queried", result, err)
	case "query_tidb_slow_queries":
		result, err := s.tidb.QuerySlowQueries(ctx, actor, tidbsvc.SlowQueryInput{DataSourceID: request.DataSourceID, Minutes: request.Minutes, Limit: request.Limit})
		return tidbQueryOutput("query_tidb_slow_queries", "TiDB slow SQL queried with sanitized SQL text", result, err)
	case "query_tidb_processlist":
		result, err := s.tidb.QueryProcessList(ctx, actor, queryInput)
		return tidbQueryOutput("query_tidb_processlist", "TiDB processlist queried with sensitive columns redacted", result, err)
	case "query_tidb_lock_waits":
		result, err := s.tidb.QueryLockWaits(ctx, actor, queryInput)
		return tidbQueryOutput("query_tidb_lock_waits", "TiDB lock waits queried", result, err)
	case "query_tidb_hot_regions":
		result, err := s.tidb.QueryHotRegions(ctx, actor, queryInput)
		return tidbQueryOutput("query_tidb_hot_regions", "TiDB hot regions queried", result, err)
	case "query_tidb_statistics_health":
		result, err := s.tidb.QueryStatisticsHealth(ctx, actor, queryInput)
		return tidbQueryOutput("query_tidb_statistics_health", "TiDB statistics health queried", result, err)
	case "explain_tidb_sql":
		if !isReadOnlyTiDBSQL(request.SQL) {
			return partialError("tidb", "sql is not a single read-only SELECT/SHOW statement"), nil
		}
		result, err := s.tidb.Explain(ctx, actor, tidbsvc.ExplainInput{DataSourceID: request.DataSourceID, SQL: request.SQL, Analyze: request.Analyze})
		return tidbQueryOutput("explain_tidb_sql", "TiDB controlled EXPLAIN executed after read-only SQL validation", result, err)
	case "diagnose_tidb_performance":
		return s.diagnosePerformance(ctx, actor, request)
	case "diagnose_tidb_connection_pressure":
		result, err := s.tidb.QueryProcessList(ctx, actor, queryInput)
		return tidbDiagnosisOutput("diagnose_tidb_connection_pressure", "TiDB connection pressure diagnosis cites processlist evidence", "query_tidb_processlist", result, err)
	case "diagnose_tidb_lock_contention":
		result, err := s.tidb.QueryLockWaits(ctx, actor, queryInput)
		return tidbDiagnosisOutput("diagnose_tidb_lock_contention", "TiDB lock contention diagnosis cites lock wait evidence", "query_tidb_lock_waits", result, err)
	case "diagnose_tidb_plan_regression":
		if strings.TrimSpace(request.SQL) == "" {
			return json.Marshal(map[string]any{
				"partial":         false,
				"skill":           "diagnose_tidb_plan_regression",
				"facts":           []map[string]any{tidbFact("RULE", "execution plan regression cannot be asserted without EXPLAIN evidence", "skill.input", nil)},
				"missingEvidence": []string{"read-only sql for controlled EXPLAIN", "historical or baseline plan evidence"},
			})
		}
		if !isReadOnlyTiDBSQL(request.SQL) {
			return partialError("tidb", "sql is not a single read-only SELECT/SHOW statement"), nil
		}
		result, err := s.tidb.Explain(ctx, actor, tidbsvc.ExplainInput{DataSourceID: request.DataSourceID, SQL: request.SQL, Analyze: request.Analyze})
		return tidbDiagnosisOutput("diagnose_tidb_plan_regression", "TiDB plan regression diagnosis cites controlled EXPLAIN evidence and does not claim index failure without plan evidence", "explain_tidb_sql", result, err)
	default:
		return partialError("tidb", "unsupported tidb skill"), nil
	}
}

func (s TiDBSkill) diagnosePerformance(ctx context.Context, actor *model.AppUser, request tidbSkillRequest) (json.RawMessage, error) {
	slow, slowErr := s.tidb.QuerySlowQueries(ctx, actor, tidbsvc.SlowQueryInput{DataSourceID: request.DataSourceID, Minutes: request.Minutes, Limit: request.Limit})
	processes, processErr := s.tidb.QueryProcessList(ctx, actor, tidbsvc.QueryInput{DataSourceID: request.DataSourceID, Limit: request.Limit})
	locks, lockErr := s.tidb.QueryLockWaits(ctx, actor, tidbsvc.QueryInput{DataSourceID: request.DataSourceID, Limit: request.Limit})
	if slowErr != nil && processErr != nil && lockErr != nil {
		return partialError("tidb", slowErr.Error()+"; "+processErr.Error()+"; "+lockErr.Error()), nil
	}
	missing := []string{}
	if slowErr != nil {
		missing = append(missing, "slow queries: "+slowErr.Error())
	}
	if processErr != nil {
		missing = append(missing, "processlist: "+processErr.Error())
	}
	if lockErr != nil {
		missing = append(missing, "lock waits: "+lockErr.Error())
	}
	facts := []map[string]any{
		tidbFact("FACT", "TiDB slow SQL evidence collected and sanitized", "query_tidb_slow_queries", slow),
		tidbFact("FACT", "TiDB processlist evidence collected and sanitized", "query_tidb_processlist", processes),
		tidbFact("RULE", "TiDB performance diagnosis must cite slow SQL, process pressure, lock waits or plan evidence before root-cause claims", "query_tidb_lock_waits", locks),
	}
	return json.Marshal(map[string]any{"partial": len(missing) > 0, "skill": "diagnose_tidb_performance", "facts": facts, "missingEvidence": missing})
}

func tidbQueryOutput(skill string, summary string, evidence any, err error) (json.RawMessage, error) {
	if err != nil {
		return partialError("tidb", err.Error()), nil
	}
	return json.Marshal(map[string]any{"partial": false, "skill": skill, "facts": []map[string]any{tidbFact("FACT", summary, skill, evidence)}, "missingEvidence": []string{}})
}

func tidbDiagnosisOutput(skill string, summary string, evidenceRef string, evidence any, err error) (json.RawMessage, error) {
	if err != nil {
		return partialError("tidb", err.Error()), nil
	}
	return json.Marshal(map[string]any{"partial": false, "skill": skill, "facts": []map[string]any{tidbFact("RULE", summary, evidenceRef, evidence)}, "missingEvidence": []string{}})
}

func tidbFact(factType string, summary string, evidenceRef string, evidence any) map[string]any {
	return map[string]any{"type": factType, "summary": summary, "evidenceRef": evidenceRef, "evidence": evidence}
}

type NginxQuerier interface {
	QueryAccessLogs(ctx context.Context, actor *model.AppUser, input nginxsvc.QueryInput) (*nginxsvc.AccessLogResult, error)
	QueryErrorLogs(ctx context.Context, actor *model.AppUser, input nginxsvc.QueryInput) (*nginxsvc.ErrorLogResult, error)
	QueryMetrics(ctx context.Context, actor *model.AppUser, input nginxsvc.QueryInput) (*nginxsvc.MetricsResult, error)
	QueryUpstreamStatus(ctx context.Context, actor *model.AppUser, input nginxsvc.QueryInput) (*nginxsvc.UpstreamStatusResult, error)
	QueryConfigMetadata(ctx context.Context, actor *model.AppUser, input nginxsvc.QueryInput) (*nginxsvc.ConfigMetadataResult, error)
}

func NginxSkills(nginx NginxQuerier) []Skill {
	names := []struct {
		name        string
		description string
	}{
		{"query_nginx_access_logs", "Query Nginx access logs with sensitive fields masked."},
		{"query_nginx_error_logs", "Query Nginx error logs."},
		{"query_nginx_metrics", "Query Nginx metrics."},
		{"query_nginx_upstreams", "Query Nginx upstream status."},
		{"query_nginx_config_metadata", "Query safe Nginx config metadata."},
		{"analyze_nginx_status_codes", "Analyze Nginx status code distribution."},
		{"analyze_nginx_latency", "Analyze Nginx latency and upstream timing."},
		{"diagnose_nginx_499", "Diagnose Nginx 499 client-aborted requests."},
		{"diagnose_nginx_502", "Diagnose Nginx 502 upstream failures."},
		{"diagnose_nginx_503", "Diagnose Nginx 503 upstream unavailability or overload."},
		{"diagnose_nginx_504", "Diagnose Nginx 504 upstream timeouts."},
		{"diagnose_nginx_upstream", "Diagnose Nginx upstream health problems."},
	}
	skills := make([]Skill, 0, len(names))
	for _, item := range names {
		skills = append(skills, NginxSkill{name: item.name, description: item.description, nginx: nginx})
	}
	return skills
}

type NginxSkill struct {
	name        string
	description string
	nginx       NginxQuerier
}

type nginxSkillRequest struct {
	DataSourceID int64 `json:"dataSourceId"`
	Limit        int   `json:"limit"`
}

func (s NginxSkill) Definition() SkillDefinition {
	return SkillDefinition{
		Name:          s.name,
		Version:       "v1",
		Description:   s.description,
		InputSchema:   json.RawMessage(`{"type":"object","required":["dataSourceId"],"properties":{"dataSourceId":{"type":"integer"},"limit":{"type":"integer"}}}`),
		OutputSchema:  partialOutputSchema(),
		RiskLevel:     model.SkillRiskSensitiveRead,
		ReadOnly:      true,
		TimeoutSecond: 30,
		RequiredTools: []string{"nginx"},
	}
}

func (s NginxSkill) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var request nginxSkillRequest
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, ErrInvalidInput
	}
	if s.nginx == nil {
		return partialError("nginx", "nginx service is not configured"), nil
	}
	actor := ActorFromContext(ctx)
	queryInput := nginxsvc.QueryInput{DataSourceID: request.DataSourceID, Limit: request.Limit}
	switch s.name {
	case "query_nginx_access_logs":
		result, err := s.nginx.QueryAccessLogs(ctx, actor, queryInput)
		return nginxQueryOutput(s.name, "Nginx access logs queried with sensitive fields masked", result, err)
	case "query_nginx_error_logs":
		result, err := s.nginx.QueryErrorLogs(ctx, actor, queryInput)
		return nginxQueryOutput(s.name, "Nginx error logs queried", result, err)
	case "query_nginx_metrics":
		result, err := s.nginx.QueryMetrics(ctx, actor, queryInput)
		return nginxQueryOutput(s.name, "Nginx metrics queried", result, err)
	case "query_nginx_upstreams":
		result, err := s.nginx.QueryUpstreamStatus(ctx, actor, queryInput)
		return nginxQueryOutput(s.name, "Nginx upstream status queried", result, err)
	case "query_nginx_config_metadata":
		result, err := s.nginx.QueryConfigMetadata(ctx, actor, queryInput)
		return nginxQueryOutput(s.name, "Nginx config metadata queried without private keys", result, err)
	case "analyze_nginx_status_codes":
		return s.analyzeStatusCodes(ctx, actor, queryInput)
	case "analyze_nginx_latency":
		return s.diagnose(ctx, actor, queryInput, "analyze_nginx_latency", "RULE", "Nginx latency analysis requires access timing, upstream timing and upstream status evidence")
	case "diagnose_nginx_499":
		return s.diagnose(ctx, actor, queryInput, "diagnose_nginx_499", "RULE", "499 usually indicates client-aborted requests; cite access log status and request context before conclusion")
	case "diagnose_nginx_502":
		return s.diagnose(ctx, actor, queryInput, "diagnose_nginx_502", "RULE", "502 diagnosis should distinguish upstream connection failure, invalid response and gateway errors using access/error/upstream evidence")
	case "diagnose_nginx_503":
		return s.diagnose(ctx, actor, queryInput, "diagnose_nginx_503", "RULE", "503 diagnosis should check upstream availability, capacity and overload evidence")
	case "diagnose_nginx_504":
		return s.diagnose(ctx, actor, queryInput, "diagnose_nginx_504", "RULE", "504 diagnosis should cite upstream timeout evidence from access logs, error logs and upstream status")
	case "diagnose_nginx_upstream":
		return s.diagnose(ctx, actor, queryInput, "diagnose_nginx_upstream", "RULE", "Nginx upstream diagnosis combines upstream status, 5xx access logs and error log evidence")
	default:
		return partialError("nginx", "unsupported nginx skill"), nil
	}
}

func (s NginxSkill) analyzeStatusCodes(ctx context.Context, actor *model.AppUser, input nginxsvc.QueryInput) (json.RawMessage, error) {
	access, err := s.nginx.QueryAccessLogs(ctx, actor, input)
	if err != nil {
		return partialError("nginx", err.Error()), nil
	}
	counts := map[int]int{}
	for _, item := range access.Items {
		counts[item.Status]++
	}
	return json.Marshal(map[string]any{
		"partial": false,
		"skill":   "analyze_nginx_status_codes",
		"facts": []map[string]any{
			nginxFact("FACT", "Nginx status code distribution derived from sanitized access logs", "query_nginx_access_logs", map[string]any{"counts": counts, "accessLogs": access}),
		},
		"missingEvidence": []string{},
	})
}

func (s NginxSkill) diagnose(ctx context.Context, actor *model.AppUser, input nginxsvc.QueryInput, skill string, factType string, summary string) (json.RawMessage, error) {
	access, accessErr := s.nginx.QueryAccessLogs(ctx, actor, input)
	errorsResult, errorErr := s.nginx.QueryErrorLogs(ctx, actor, input)
	upstreams, upstreamErr := s.nginx.QueryUpstreamStatus(ctx, actor, input)
	metrics, metricsErr := s.nginx.QueryMetrics(ctx, actor, input)
	if accessErr != nil && errorErr != nil && upstreamErr != nil && metricsErr != nil {
		return partialError("nginx", accessErr.Error()+"; "+errorErr.Error()+"; "+upstreamErr.Error()+"; "+metricsErr.Error()), nil
	}
	missing := []string{}
	if accessErr != nil {
		missing = append(missing, "access logs: "+accessErr.Error())
	}
	if errorErr != nil {
		missing = append(missing, "error logs: "+errorErr.Error())
	}
	if upstreamErr != nil {
		missing = append(missing, "upstream status: "+upstreamErr.Error())
	}
	if metricsErr != nil {
		missing = append(missing, "metrics: "+metricsErr.Error())
	}
	facts := []map[string]any{
		nginxFact("FACT", "sanitized Nginx access log evidence collected", "query_nginx_access_logs", access),
		nginxFact("FACT", "Nginx error log evidence collected", "query_nginx_error_logs", errorsResult),
		nginxFact(factType, summary, "query_nginx_upstreams", map[string]any{"upstreams": upstreams, "metrics": metrics}),
		nginxFact("RULE", "configuration changes and reload/restart are high-risk suggestions requiring approval", "skill.safety", map[string]string{"risk": "high"}),
	}
	return json.Marshal(map[string]any{"partial": len(missing) > 0, "skill": skill, "facts": facts, "missingEvidence": missing})
}

func nginxQueryOutput(skill string, summary string, evidence any, err error) (json.RawMessage, error) {
	if err != nil {
		return partialError("nginx", err.Error()), nil
	}
	return json.Marshal(map[string]any{"partial": false, "skill": skill, "facts": []map[string]any{nginxFact("FACT", summary, skill, evidence)}, "missingEvidence": []string{}})
}

func nginxFact(factType string, summary string, evidenceRef string, evidence any) map[string]any {
	return map[string]any{"type": factType, "summary": summary, "evidenceRef": evidenceRef, "evidence": evidence}
}

type ComponentSkill struct {
	name         string
	description  string
	requiredTool string
}

func (s ComponentSkill) Definition() SkillDefinition {
	return SkillDefinition{
		Name:          s.name,
		Version:       "v1",
		Description:   s.description,
		InputSchema:   json.RawMessage(`{"type":"object","properties":{"dataSourceId":{"type":"integer"},"from":{"type":"string"},"to":{"type":"string"},"namespace":{"type":"string"},"group":{"type":"string"},"serviceName":{"type":"string"},"sql":{"type":"string"},"limit":{"type":"integer"}}}`),
		OutputSchema:  partialOutputSchema(),
		RiskLevel:     model.SkillRiskSensitiveRead,
		ReadOnly:      true,
		TimeoutSecond: 30,
		RequiredTools: []string{s.requiredTool},
	}
}

func (s ComponentSkill) Execute(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var request map[string]any
	if len(input) > 0 {
		if err := json.Unmarshal(input, &request); err != nil {
			return nil, ErrInvalidInput
		}
	}
	if strings.HasPrefix(s.name, "explain_tidb") {
		if sql, _ := request["sql"].(string); !isReadOnlyTiDBSQL(sql) {
			return partialError(s.name, "sql is not a single read-only SELECT/SHOW/EXPLAIN statement"), nil
		}
	}
	return json.Marshal(map[string]any{
		"partial": false,
		"skill":   s.name,
		"facts": []map[string]any{{
			"type":     "RULE",
			"summary":  "skill is registered and constrained to read-only tool access",
			"tool":     s.requiredTool,
			"evidence": "skill.definition",
		}},
		"missingEvidence": []string{"external tool adapter implementation or live data source response"},
	})
}

func isReadOnlyTiDBSQL(sql string) bool {
	normalized := strings.TrimSpace(strings.TrimRight(sql, ";"))
	if normalized == "" || strings.Contains(normalized, ";") {
		return false
	}
	lower := strings.ToLower(normalized)
	if strings.Contains(lower, " into outfile") ||
		strings.Contains(lower, " load data") ||
		strings.Contains(lower, " set global") ||
		strings.Contains(lower, " analyze ") ||
		strings.Contains(lower, " admin ") {
		return false
	}
	return strings.HasPrefix(lower, "select ") ||
		strings.HasPrefix(lower, "show ")
}
