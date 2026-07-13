package skillframework

import (
	"context"
	"encoding/json"
	"strings"

	"aiops-platform/backend/internal/model"
)

func ComponentDiagnosisSkills() []Skill {
	names := []struct {
		name        string
		description string
		tool        string
	}{
		{"query_nacos_services", "Query Nacos service metadata in allowed namespaces and groups.", "nacos"},
		{"get_nacos_service_instances", "Query Nacos service instances and health state.", "nacos"},
		{"query_nacos_config_metadata", "Query Nacos configuration metadata without sensitive content.", "nacos"},
		{"query_nacos_config_changes", "Query Nacos configuration change history.", "nacos"},
		{"query_nacos_client_connections", "Query Nacos client connection and listener summaries.", "nacos"},
		{"diagnose_nacos_registration", "Diagnose Nacos service registration and instance health problems.", "nacos"},
		{"diagnose_nacos_config_delivery", "Diagnose Nacos configuration delivery and listener problems.", "nacos"},
		{"query_redis_info", "Query Redis INFO summary.", "redis"},
		{"query_redis_memory", "Query Redis memory statistics.", "redis"},
		{"query_redis_clients", "Query Redis client summary with sensitive fields masked.", "redis"},
		{"query_redis_slowlog", "Query Redis slowlog within configured limits.", "redis"},
		{"query_redis_keyspace", "Query Redis keyspace summary without reading values.", "redis"},
		{"query_redis_replication", "Query Redis replication state.", "redis"},
		{"query_redis_cluster", "Query Redis Cluster or Sentinel state.", "redis"},
		{"query_redis_latency", "Query Redis latency latest summary.", "redis"},
		{"diagnose_redis_health", "Diagnose Redis health from clients, memory, slowlog, replication and cluster evidence.", "redis"},
		{"diagnose_redis_memory", "Diagnose Redis memory pressure and fragmentation.", "redis"},
		{"diagnose_redis_connection_pool", "Diagnose Redis connection pool pressure.", "redis"},
		{"diagnose_redis_replication", "Diagnose Redis replication problems.", "redis"},
		{"diagnose_redis_cluster", "Diagnose Redis Cluster slot and node problems.", "redis"},
		{"query_tidb_cluster_status", "Query TiDB cluster status.", "tidb"},
		{"query_tidb_metrics", "Query TiDB related metrics.", "tidb"},
		{"query_tidb_slow_queries", "Query TiDB slow SQL summaries.", "tidb"},
		{"query_tidb_processlist", "Query TiDB processlist summary.", "tidb"},
		{"query_tidb_lock_waits", "Query TiDB lock waits.", "tidb"},
		{"query_tidb_hot_regions", "Query TiDB hot region summary.", "tidb"},
		{"query_tidb_statistics_health", "Query TiDB statistics health.", "tidb"},
		{"explain_tidb_sql", "Run controlled TiDB EXPLAIN after read-only SQL validation.", "tidb"},
		{"diagnose_tidb_performance", "Diagnose TiDB performance degradation.", "tidb"},
		{"diagnose_tidb_connection_pressure", "Diagnose TiDB connection pressure.", "tidb"},
		{"diagnose_tidb_lock_contention", "Diagnose TiDB lock contention.", "tidb"},
		{"diagnose_tidb_plan_regression", "Diagnose TiDB execution plan regression.", "tidb"},
		{"query_nginx_access_logs", "Query Nginx access logs with sensitive fields masked.", "nginx"},
		{"query_nginx_error_logs", "Query Nginx error logs.", "nginx"},
		{"query_nginx_metrics", "Query Nginx metrics.", "nginx"},
		{"query_nginx_upstreams", "Query Nginx upstream status.", "nginx"},
		{"query_nginx_config_metadata", "Query safe Nginx config metadata.", "nginx"},
		{"analyze_nginx_status_codes", "Analyze Nginx status code distribution.", "nginx"},
		{"analyze_nginx_latency", "Analyze Nginx latency and upstream timing.", "nginx"},
		{"diagnose_nginx_499", "Diagnose Nginx 499 client-aborted requests.", "nginx"},
		{"diagnose_nginx_502", "Diagnose Nginx 502 upstream failures.", "nginx"},
		{"diagnose_nginx_503", "Diagnose Nginx 503 upstream unavailability or overload.", "nginx"},
		{"diagnose_nginx_504", "Diagnose Nginx 504 upstream timeouts.", "nginx"},
		{"diagnose_nginx_upstream", "Diagnose Nginx upstream health problems.", "nginx"},
	}
	skills := make([]Skill, 0, len(names))
	for _, item := range names {
		skills = append(skills, ComponentSkill{name: item.name, description: item.description, requiredTool: item.tool})
	}
	return skills
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
		strings.HasPrefix(lower, "show ") ||
		strings.HasPrefix(lower, "explain ")
}
