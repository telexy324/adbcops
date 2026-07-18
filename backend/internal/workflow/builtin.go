package workflow

import (
	"context"
	"encoding/json"
	"fmt"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

const builtinWorkflowVersion = "v1"

type BootstrapRepository interface {
	ListWorkflowDefinitions(ctx context.Context, limit int) ([]model.WorkflowDefinition, error)
	CreateWorkflowDefinition(ctx context.Context, definition *model.WorkflowDefinition) error
	UpdateWorkflowDefinition(ctx context.Context, id int64, updates repository.WorkflowDefinitionUpdates) (*model.WorkflowDefinition, error)
}

func BuiltinDefinitions() []Definition {
	return []Definition{
		knowledgeQAWorkflow(),
		logAnalysisWorkflow(),
		podDiagnosisWorkflow(),
		ingressDiagnosisWorkflow(),
		alertDiagnosisWorkflow(),
		nacosDiagnosisWorkflow(),
		nacosRegistrationWorkflow(),
		nacosConfigDeliveryWorkflow(),
		redisDiagnosisWorkflow(),
		redisMemoryWorkflow(),
		redisConnectionPoolWorkflow(),
		redisReplicationWorkflow(),
		redisClusterWorkflow(),
		tidbDiagnosisWorkflow(),
		tidbPerformanceWorkflow(),
		tidbConnectionPressureWorkflow(),
		tidbLockContentionWorkflow(),
		tidbPlanRegressionWorkflow(),
		nginxDiagnosisWorkflow(),
		nginxStatusDiagnosisWorkflow("nginx_499_diagnosis_workflow", "Nginx 499 Diagnosis", "Diagnose client aborted Nginx 499 requests.", "diagnose_nginx_499"),
		nginxStatusDiagnosisWorkflow("nginx_502_diagnosis_workflow", "Nginx 502 Diagnosis", "Diagnose upstream failure Nginx 502 responses.", "diagnose_nginx_502"),
		nginxStatusDiagnosisWorkflow("nginx_503_diagnosis_workflow", "Nginx 503 Diagnosis", "Diagnose unavailable or overloaded upstream Nginx 503 responses.", "diagnose_nginx_503"),
		nginxStatusDiagnosisWorkflow("nginx_504_diagnosis_workflow", "Nginx 504 Diagnosis", "Diagnose upstream timeout Nginx 504 responses.", "diagnose_nginx_504"),
		linuxBasicHostDiagnosisWorkflow(),
		linuxCPUWorkflow(),
		linuxMemoryWorkflow(),
		linuxDiskWorkflow(),
		linuxNetworkWorkflow(),
		linuxBatchHealthWorkflow(),
	}
}

func BootstrapBuiltinDefinitions(ctx context.Context, repo BootstrapRepository, createdBy *int64) error {
	existing, err := repo.ListWorkflowDefinitions(ctx, 200)
	if err != nil {
		return err
	}
	byKey := map[string]model.WorkflowDefinition{}
	for _, item := range existing {
		byKey[workflowKey(item.Name, item.Version)] = item
	}
	for _, definition := range BuiltinDefinitions() {
		raw, err := json.Marshal(definition)
		if err != nil {
			return err
		}
		description := definition.Description
		if record, ok := byKey[workflowKey(definition.Name, definition.Version)]; ok {
			if _, err := repo.UpdateWorkflowDefinition(ctx, record.ID, repository.WorkflowDefinitionUpdates{
				Name:           definition.Name,
				Version:        definition.Version,
				Description:    &description,
				DescriptionSet: true,
				Definition:     raw,
				Enabled:        true,
				EnabledSet:     true,
			}); err != nil {
				return err
			}
			continue
		}
		if err := repo.CreateWorkflowDefinition(ctx, &model.WorkflowDefinition{
			Name:        definition.Name,
			Version:     definition.Version,
			Description: &description,
			Definition:  raw,
			Enabled:     true,
			CreatedBy:   createdBy,
		}); err != nil {
			return err
		}
	}
	return nil
}

func knowledgeQAWorkflow() Definition {
	return linearWorkflow(
		"knowledge_qa_workflow",
		"Knowledge QA",
		"Search published knowledge and produce citation-backed answer evidence.",
		[]Node{
			controlNode("normalize_question", "Normalize question"),
			controlNode("rewrite_query", "Rewrite query"),
			{ID: "search_knowledge", Type: NodeTypeSkill, Name: "Search knowledge", SkillName: "search_knowledge", Config: rawConfig(map[string]any{"input": map[string]any{"query": "workflow knowledge query", "limit": 5}})},
			controlNode("rerank", "Rerank citations"),
			{ID: "knowledge_agent", Type: NodeTypeAgent, Name: "Knowledge Agent", AgentName: "knowledge_agent", Config: rawConfig(map[string]any{"context": map[string]any{"query": "workflow knowledge query"}})},
			controlNode("validate_citations", "Validate citations"),
		},
	)
}

func logAnalysisWorkflow() Definition {
	return linearWorkflow(
		"log_analysis_workflow",
		"Log Analysis",
		"Query logs, aggregate templates, extract entities, search knowledge and summarize with Log Agent.",
		[]Node{
			{ID: "query_logs", Type: NodeTypeSkill, Name: "Query logs", SkillName: "query_logs", Config: rawConfig(map[string]any{"input": map[string]any{"dataSourceId": 1, "from": "2026-01-01T00:00:00Z", "to": "2026-01-01T00:05:00Z", "size": 20}})},
			controlNode("sanitize", "Sanitize logs"),
			{ID: "aggregate_templates", Type: NodeTypeSkill, Name: "Aggregate log templates", SkillName: "aggregate_log_templates", Config: rawConfig(map[string]any{"input": map[string]any{"items": []map[string]any{{"message": "error sample"}}}})},
			{ID: "extract_entities", Type: NodeTypeSkill, Name: "Extract log entities", SkillName: "extract_log_entities", Config: rawConfig(map[string]any{"input": map[string]any{"items": []map[string]any{{"message": "error sample"}}}})},
			{ID: "search_knowledge", Type: NodeTypeSkill, Name: "Search related knowledge", SkillName: "search_knowledge", Config: rawConfig(map[string]any{"input": map[string]any{"query": "log error analysis", "limit": 5}})},
			controlNode("build_log_timeline", "Build log timeline"),
			{ID: "log_agent", Type: NodeTypeAgent, Name: "Log Agent", AgentName: "log_agent", Config: rawConfig(map[string]any{"context": map[string]any{"query": "log analysis", "variables": map[string]any{"dataSourceId": 1, "from": "2026-01-01T00:00:00Z", "to": "2026-01-01T00:05:00Z"}}})},
			controlNode("incident_summary", "Incident summary"),
		},
	)
}

func podDiagnosisWorkflow() Definition {
	return linearWorkflow(
		"pod_diagnosis_workflow",
		"K8s Pod Diagnosis",
		"Collect pod context, metrics, rules and knowledge for Kubernetes pod diagnosis.",
		[]Node{
			{ID: "get_pod_context", Type: NodeTypeSkill, Name: "Get pod context", SkillName: "get_pod_context", Config: k8sPodConfig()},
			controlNode("get_events", "Get pod events"),
			controlNode("get_current_logs", "Get current logs"),
			controlNode("get_previous_logs", "Get previous logs"),
			controlNode("get_owner_workload", "Get owner workload"),
			controlNode("get_service_endpoints", "Get service endpoints"),
			{ID: "query_metrics", Type: NodeTypeSkill, Name: "Query pod metrics", SkillName: "query_metrics", Config: rawConfig(map[string]any{"input": map[string]any{"dataSourceId": 1, "query": "up", "range": false}})},
			controlNode("query_recent_changes", "Query recent changes"),
			{ID: "run_rules", Type: NodeTypeSkill, Name: "Run K8s rules", SkillName: "run_k8s_diagnostic_rules", Config: k8sPodConfig()},
			{ID: "search_knowledge", Type: NodeTypeSkill, Name: "Search K8s knowledge", SkillName: "search_knowledge", Config: rawConfig(map[string]any{"input": map[string]any{"query": "kubernetes pod diagnosis", "limit": 5}})},
			controlNode("correlate", "Correlate evidence"),
			{ID: "kubernetes_agent", Type: NodeTypeAgent, Name: "Kubernetes Agent", AgentName: "kubernetes_agent", Config: rawConfig(map[string]any{"context": map[string]any{"query": "pod diagnosis", "variables": map[string]any{"dataSourceId": 1, "namespace": "default", "podName": "sample-pod"}}})},
		},
	)
}

func ingressDiagnosisWorkflow() Definition {
	return linearWorkflow(
		"ingress_diagnosis_workflow",
		"Ingress Diagnosis",
		"Collect ingress, backend and metric context for ingress diagnosis.",
		[]Node{
			{ID: "get_ingress", Type: NodeTypeSkill, Name: "Get ingress", SkillName: "get_ingress_context", Config: rawConfig(map[string]any{"input": map[string]any{"dataSourceId": 1, "namespace": "default", "limit": 20}})},
			controlNode("get_backend_service", "Get backend service"),
			controlNode("get_endpoints", "Get endpoints"),
			controlNode("get_backend_pods", "Get backend pods"),
			controlNode("query_ingress_logs", "Query ingress logs"),
			{ID: "query_4xx_5xx_latency", Type: NodeTypeSkill, Name: "Query 4xx/5xx latency", SkillName: "query_metrics", Config: rawConfig(map[string]any{"input": map[string]any{"dataSourceId": 1, "query": "up", "range": false}})},
			controlNode("query_recent_changes", "Query recent changes"),
			controlNode("correlate", "Correlate evidence"),
			{ID: "kubernetes_agent", Type: NodeTypeAgent, Name: "Kubernetes Agent", AgentName: "kubernetes_agent", Config: rawConfig(map[string]any{"context": map[string]any{"query": "ingress diagnosis", "variables": map[string]any{"dataSourceId": 1, "namespace": "default", "podName": "sample-pod"}}})},
		},
	)
}

func alertDiagnosisWorkflow() Definition {
	return linearWorkflow(
		"alert_diagnosis_workflow",
		"Alert Diagnosis",
		"Normalize alert, select workflow, collect context and summarize alert diagnosis plan.",
		[]Node{
			controlNode("parse_alert", "Parse alert"),
			controlNode("normalize_event", "Normalize event"),
			{ID: "select_workflow", Type: NodeTypeAgent, Name: "Coordinator Agent", AgentName: "coordinator_agent", Config: rawConfig(map[string]any{"context": map[string]any{"query": "alert diagnosis"}})},
			controlNode("collect_context", "Collect context"),
			controlNode("build_timeline", "Build timeline"),
			controlNode("correlate", "Correlate evidence"),
			controlNode("create_incident", "Create incident draft"),
		},
	)
}

func nacosDiagnosisWorkflow() Definition {
	return linearWorkflow(
		"nacos_diagnosis_workflow",
		"Nacos Diagnosis",
		"Diagnose Nacos service registration and configuration delivery issues.",
		[]Node{
			componentSkillNode("query_services", "Query services", "query_nacos_services"),
			componentSkillNode("query_instances", "Query instances", "get_nacos_service_instances"),
			componentSkillNode("query_config_metadata", "Query config metadata", "query_nacos_config_metadata"),
			componentSkillNode("query_recent_config_changes", "Query config changes", "query_nacos_config_changes"),
			componentSkillNode("query_client_connections", "Query client connections", "query_nacos_client_connections"),
			controlNode("query_application_logs", "Query application logs"),
			controlNode("query_recent_releases", "Query recent releases"),
			componentSkillNode("diagnose_registration", "Diagnose registration", "diagnose_nacos_registration"),
			componentSkillNode("diagnose_config_delivery", "Diagnose config delivery", "diagnose_nacos_config_delivery"),
			controlNode("build_timeline", "Build timeline"),
			controlNode("correlate", "Correlate"),
			controlNode("report", "Report"),
		},
	)
}

func nacosRegistrationWorkflow() Definition {
	return linearWorkflow(
		"nacos_registration_diagnosis_workflow",
		"Nacos Registration Diagnosis",
		"Diagnose Nacos service registration and instance health with namespace/group evidence.",
		[]Node{
			componentSkillNode("query_services", "Query services", "query_nacos_services"),
			componentSkillNode("query_instances", "Query instances", "get_nacos_service_instances"),
			componentSkillNode("query_client_connections", "Query client connections", "query_nacos_client_connections"),
			componentSkillNode("diagnose_registration", "Diagnose registration", "diagnose_nacos_registration"),
			controlNode("report", "Report"),
		},
	)
}

func nacosConfigDeliveryWorkflow() Definition {
	return linearWorkflow(
		"nacos_config_delivery_diagnosis_workflow",
		"Nacos Config Delivery Diagnosis",
		"Diagnose Nacos config metadata, change history and listener delivery evidence.",
		[]Node{
			componentSkillNode("query_config_metadata", "Query config metadata", "query_nacos_config_metadata"),
			componentSkillNode("query_config_changes", "Query config changes", "query_nacos_config_changes"),
			componentSkillNode("query_client_connections", "Query client connections and listeners", "query_nacos_client_connections"),
			componentSkillNode("diagnose_config_delivery", "Diagnose config delivery", "diagnose_nacos_config_delivery"),
			controlNode("report", "Report"),
		},
	)
}

func redisDiagnosisWorkflow() Definition {
	return linearWorkflow(
		"redis_diagnosis_workflow",
		"Redis Diagnosis",
		"Diagnose Redis memory, connection pool, replication and cluster problems.",
		[]Node{
			componentSkillNode("query_redis_info", "Query Redis info", "query_redis_info"),
			componentSkillNode("query_memory", "Query memory", "query_redis_memory"),
			componentSkillNode("query_clients", "Query clients", "query_redis_clients"),
			componentSkillNode("query_slowlog", "Query slowlog", "query_redis_slowlog"),
			componentSkillNode("query_replication_or_cluster", "Query replication or cluster", "query_redis_cluster"),
			controlNode("query_prometheus_metrics", "Query Prometheus metrics"),
			controlNode("query_application_logs", "Query application logs"),
			{ID: "search_knowledge", Type: NodeTypeSkill, Name: "Search Redis knowledge", SkillName: "search_knowledge", Config: rawConfig(map[string]any{"input": map[string]any{"query": "redis diagnosis", "limit": 5}})},
			componentSkillNode("diagnose_health", "Diagnose health", "diagnose_redis_health"),
			componentSkillNode("diagnose_memory", "Diagnose memory", "diagnose_redis_memory"),
			componentSkillNode("diagnose_connection_pool", "Diagnose connection pool", "diagnose_redis_connection_pool"),
			componentSkillNode("diagnose_replication_or_cluster", "Diagnose replication or cluster", "diagnose_redis_cluster"),
			controlNode("correlate", "Correlate"),
			controlNode("report", "Report"),
		},
	)
}

func redisMemoryWorkflow() Definition {
	return linearWorkflow(
		"redis_memory_diagnosis_workflow",
		"Redis Memory Diagnosis",
		"Diagnose Redis memory pressure using INFO, MEMORY STATS, keyspace summary and slowlog evidence.",
		[]Node{
			componentSkillNode("query_info", "Query Redis info", "query_redis_info"),
			componentSkillNode("query_memory", "Query memory", "query_redis_memory"),
			componentSkillNode("query_keyspace", "Query keyspace summary", "query_redis_keyspace"),
			componentSkillNode("query_slowlog", "Query slowlog", "query_redis_slowlog"),
			componentSkillNode("diagnose_memory", "Diagnose memory", "diagnose_redis_memory"),
			controlNode("report", "Report"),
		},
	)
}

func redisConnectionPoolWorkflow() Definition {
	return linearWorkflow(
		"redis_connection_pool_diagnosis_workflow",
		"Redis Connection Pool Diagnosis",
		"Diagnose Redis connection pressure using client summaries, latency and slowlog evidence.",
		[]Node{
			componentSkillNode("query_clients", "Query clients", "query_redis_clients"),
			componentSkillNode("query_latency", "Query latency", "query_redis_latency"),
			componentSkillNode("query_slowlog", "Query slowlog", "query_redis_slowlog"),
			componentSkillNode("diagnose_connection_pool", "Diagnose connection pool", "diagnose_redis_connection_pool"),
			controlNode("report", "Report"),
		},
	)
}

func redisReplicationWorkflow() Definition {
	return linearWorkflow(
		"redis_replication_diagnosis_workflow",
		"Redis Replication Diagnosis",
		"Diagnose Redis standalone or Sentinel replication problems.",
		[]Node{
			componentSkillNode("query_info", "Query Redis info", "query_redis_info"),
			componentSkillNode("query_replication", "Query replication", "query_redis_replication"),
			componentSkillNode("query_cluster_or_sentinel", "Query Cluster or Sentinel", "query_redis_cluster"),
			componentSkillNode("diagnose_replication", "Diagnose replication", "diagnose_redis_replication"),
			controlNode("report", "Report"),
		},
	)
}

func redisClusterWorkflow() Definition {
	return linearWorkflow(
		"redis_cluster_diagnosis_workflow",
		"Redis Cluster Diagnosis",
		"Diagnose Redis Cluster slots, nodes and partial node failures.",
		[]Node{
			componentSkillNode("query_info", "Query Redis info", "query_redis_info"),
			componentSkillNode("query_cluster", "Query Redis cluster", "query_redis_cluster"),
			componentSkillNode("query_latency", "Query latency", "query_redis_latency"),
			componentSkillNode("diagnose_cluster", "Diagnose cluster", "diagnose_redis_cluster"),
			controlNode("report", "Report"),
		},
	)
}

func tidbDiagnosisWorkflow() Definition {
	return linearWorkflow(
		"tidb_diagnosis_workflow",
		"TiDB Diagnosis",
		"Diagnose TiDB performance, connection pressure, lock contention and plan regression.",
		[]Node{
			componentSkillNode("query_cluster_status", "Query cluster status", "query_tidb_cluster_status"),
			componentSkillNode("query_tidb_metrics", "Query TiDB metrics", "query_tidb_metrics"),
			componentSkillNode("query_slow_queries", "Query slow queries", "query_tidb_slow_queries"),
			componentSkillNode("query_processlist", "Query processlist", "query_tidb_processlist"),
			componentSkillNode("query_lock_waits", "Query lock waits", "query_tidb_lock_waits"),
			componentSkillNode("query_hot_regions", "Query hot regions", "query_tidb_hot_regions"),
			componentSkillNode("query_statistics_health", "Query statistics health", "query_tidb_statistics_health"),
			componentSkillNode("optional_explain", "Optional explain", "explain_tidb_sql"),
			controlNode("query_recent_changes", "Query recent changes"),
			{ID: "search_knowledge", Type: NodeTypeSkill, Name: "Search TiDB knowledge", SkillName: "search_knowledge", Config: rawConfig(map[string]any{"input": map[string]any{"query": "tidb diagnosis", "limit": 5}})},
			componentSkillNode("diagnose_performance", "Diagnose performance", "diagnose_tidb_performance"),
			componentSkillNode("diagnose_connection_pressure", "Diagnose connection pressure", "diagnose_tidb_connection_pressure"),
			componentSkillNode("diagnose_lock_contention", "Diagnose lock contention", "diagnose_tidb_lock_contention"),
			componentSkillNode("diagnose_plan_regression", "Diagnose plan regression", "diagnose_tidb_plan_regression"),
			controlNode("correlate", "Correlate"),
			controlNode("report", "Report"),
		},
	)
}

func tidbPerformanceWorkflow() Definition {
	return linearWorkflow(
		"tidb_performance_diagnosis_workflow",
		"TiDB Performance Diagnosis",
		"Diagnose TiDB performance using slow SQL, processlist, hot regions, stats health and metrics evidence.",
		[]Node{
			componentSkillNode("query_cluster_status", "Query cluster status", "query_tidb_cluster_status"),
			componentSkillNode("query_slow_queries", "Query slow queries", "query_tidb_slow_queries"),
			componentSkillNode("query_processlist", "Query processlist", "query_tidb_processlist"),
			componentSkillNode("query_hot_regions", "Query hot regions", "query_tidb_hot_regions"),
			componentSkillNode("query_statistics_health", "Query statistics health", "query_tidb_statistics_health"),
			componentSkillNode("diagnose_performance", "Diagnose performance", "diagnose_tidb_performance"),
			controlNode("report", "Report"),
		},
	)
}

func tidbConnectionPressureWorkflow() Definition {
	return linearWorkflow(
		"tidb_connection_pressure_diagnosis_workflow",
		"TiDB Connection Pressure Diagnosis",
		"Diagnose TiDB connection pressure using processlist and cluster status evidence.",
		[]Node{
			componentSkillNode("query_cluster_status", "Query cluster status", "query_tidb_cluster_status"),
			componentSkillNode("query_processlist", "Query processlist", "query_tidb_processlist"),
			componentSkillNode("diagnose_connection_pressure", "Diagnose connection pressure", "diagnose_tidb_connection_pressure"),
			controlNode("report", "Report"),
		},
	)
}

func tidbLockContentionWorkflow() Definition {
	return linearWorkflow(
		"tidb_lock_contention_diagnosis_workflow",
		"TiDB Lock Contention Diagnosis",
		"Diagnose TiDB lock contention using lock wait, processlist and slow SQL evidence.",
		[]Node{
			componentSkillNode("query_lock_waits", "Query lock waits", "query_tidb_lock_waits"),
			componentSkillNode("query_processlist", "Query processlist", "query_tidb_processlist"),
			componentSkillNode("query_slow_queries", "Query slow queries", "query_tidb_slow_queries"),
			componentSkillNode("diagnose_lock_contention", "Diagnose lock contention", "diagnose_tidb_lock_contention"),
			controlNode("report", "Report"),
		},
	)
}

func tidbPlanRegressionWorkflow() Definition {
	return linearWorkflow(
		"tidb_plan_regression_diagnosis_workflow",
		"TiDB Plan Regression Diagnosis",
		"Diagnose TiDB execution plan regression using controlled EXPLAIN and slow SQL evidence.",
		[]Node{
			componentSkillNode("query_slow_queries", "Query slow queries", "query_tidb_slow_queries"),
			componentSkillNode("optional_explain", "Optional controlled explain", "explain_tidb_sql"),
			componentSkillNode("query_statistics_health", "Query statistics health", "query_tidb_statistics_health"),
			componentSkillNode("diagnose_plan_regression", "Diagnose plan regression", "diagnose_tidb_plan_regression"),
			controlNode("report", "Report"),
		},
	)
}

func nginxDiagnosisWorkflow() Definition {
	return linearWorkflow(
		"nginx_diagnosis_workflow",
		"Nginx Diagnosis",
		"Diagnose Nginx 499, 502, 503 and 504 problems with logs, metrics, upstreams and topology.",
		[]Node{
			componentSkillNode("query_access_logs", "Query access logs", "query_nginx_access_logs"),
			componentSkillNode("query_error_logs", "Query error logs", "query_nginx_error_logs"),
			componentSkillNode("query_nginx_metrics", "Query Nginx metrics", "query_nginx_metrics"),
			componentSkillNode("get_upstream_status", "Get upstream status", "query_nginx_upstreams"),
			controlNode("get_topology", "Get topology"),
			controlNode("query_backend_k8s_context", "Query backend K8s context"),
			controlNode("query_recent_changes", "Query recent changes"),
			componentSkillNode("analyze_status_codes", "Analyze status codes", "analyze_nginx_status_codes"),
			componentSkillNode("diagnose_499", "Diagnose 499", "diagnose_nginx_499"),
			componentSkillNode("diagnose_502", "Diagnose 502", "diagnose_nginx_502"),
			componentSkillNode("diagnose_503", "Diagnose 503", "diagnose_nginx_503"),
			componentSkillNode("diagnose_504", "Diagnose 504", "diagnose_nginx_504"),
			controlNode("correlate", "Correlate"),
			controlNode("report", "Report"),
		},
	)
}

func nginxStatusDiagnosisWorkflow(name, title, description, diagnosisSkill string) Definition {
	return linearWorkflow(
		name,
		title,
		description,
		[]Node{
			componentSkillNode("query_access_logs", "Query access logs", "query_nginx_access_logs"),
			componentSkillNode("query_error_logs", "Query error logs", "query_nginx_error_logs"),
			componentSkillNode("query_nginx_metrics", "Query Nginx metrics", "query_nginx_metrics"),
			componentSkillNode("query_upstreams", "Query upstreams", "query_nginx_upstreams"),
			componentSkillNode("analyze_status_codes", "Analyze status codes", "analyze_nginx_status_codes"),
			componentSkillNode("diagnose_status", "Diagnose status", diagnosisSkill),
			controlNode("report", "Report"),
		},
	)
}

func linearWorkflow(name, title, description string, body []Node) Definition {
	nodes := []Node{{ID: "start", Type: NodeTypeStart, Name: "Start"}}
	nodes = append(nodes, body...)
	nodes = append(nodes, Node{ID: "end", Type: NodeTypeEnd, Name: "End"})
	edges := make([]Edge, 0, len(nodes)-1)
	for index := 0; index < len(nodes)-1; index++ {
		edges = append(edges, Edge{From: nodes[index].ID, To: nodes[index+1].ID})
	}
	return Definition{Name: name, Version: builtinWorkflowVersion, Description: fmt.Sprintf("%s: %s", title, description), Nodes: nodes, Edges: edges}
}

func controlNode(id, name string) Node {
	return Node{ID: id, Type: NodeTypeCondition, Name: name}
}

func componentSkillNode(id, name, skillName string) Node {
	return Node{ID: id, Type: NodeTypeSkill, Name: name, SkillName: skillName, Config: rawConfig(map[string]any{"input": map[string]any{"dataSourceId": 1, "limit": 20}})}
}

func k8sPodConfig() json.RawMessage {
	return rawConfig(map[string]any{"input": map[string]any{"dataSourceId": 1, "namespace": "default", "podName": "sample-pod", "logTailLines": 100}})
}

func rawConfig(value map[string]any) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}

func workflowKey(name, version string) string {
	return name + ":" + version
}
