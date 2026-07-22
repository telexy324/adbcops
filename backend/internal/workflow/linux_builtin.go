package workflow

import "fmt"

func linuxBasicHostDiagnosisWorkflow() Definition {
	collectors := []Node{
		linuxSkillNode("collect_system_overview", "Collect system overview", "get_linux_system_overview"),
		linuxSkillNode("collect_cpu", "Collect CPU", "get_linux_cpu_status"),
		linuxSkillNode("collect_memory", "Collect memory", "get_linux_memory_status"),
		linuxSkillNode("collect_filesystem", "Collect filesystems", "get_linux_filesystem_status"),
		linuxSkillNode("collect_network", "Collect network", "get_linux_network_status"),
		linuxSkillNode("collect_processes", "Collect processes", "get_linux_process_status"),
		linuxSkillNode("collect_systemd", "Collect systemd", "get_linux_service_status"),
		linuxSkillNode("collect_time_sync", "Collect time sync", "get_linux_time_sync_status"),
		linuxSkillNode("collect_kernel_events", "Collect kernel events", "get_linux_kernel_event_summary"),
	}
	nodes := []Node{
		{ID: "start", Type: NodeTypeStart, Name: "Start"},
		linuxSkillNode("test_connection", "Test connection", "test_linux_server_connection"),
		controlNode("detect_platform", "Detect platform"),
		controlNode("load_host_profile", "Load host profile"),
	}
	nodes = append(nodes, collectors...)
	nodes = append(nodes,
		Node{ID: "merge_collectors", Type: NodeTypeMerge, Name: "Merge collector results"},
		linuxSkillNode("run_linux_rules", "Run Linux diagnostic rules", "diagnose_linux_host_health"),
		linuxKnowledgeNode("search_knowledge", "Search Linux knowledge", "Linux host diagnosis"),
		controlNode("get_host_topology", "Get host topology"),
		linuxAgentNode("linux_server_agent", "Linux Server Agent"),
		Node{ID: "end", Type: NodeTypeEnd, Name: "End"},
	)
	edges := []Edge{
		{From: "start", To: "test_connection"},
		{From: "test_connection", To: "detect_platform"},
		{From: "detect_platform", To: "load_host_profile"},
	}
	for _, collector := range collectors {
		edges = append(edges, Edge{From: "load_host_profile", To: collector.ID}, Edge{From: collector.ID, To: "merge_collectors"})
	}
	edges = append(edges,
		Edge{From: "merge_collectors", To: "run_linux_rules"},
		Edge{From: "run_linux_rules", To: "search_knowledge"},
		Edge{From: "search_knowledge", To: "get_host_topology"},
		Edge{From: "get_host_topology", To: "linux_server_agent"},
		Edge{From: "linux_server_agent", To: "end"},
	)
	return linuxWorkflow("linux_basic_host_diagnosis_workflow", "Basic Host Diagnosis", "Collect Linux host context in parallel, run rules and build an evidence-backed report.", nodes, edges)
}

func linuxCPUWorkflow() Definition {
	parallel := []Node{
		linuxSkillNode("collect_cpu", "Collect CPU and top processes", "get_linux_cpu_status"),
		linuxSkillNode("collect_memory", "Collect memory", "get_linux_memory_status"),
		linuxSkillNode("collect_disk_io", "Collect disk IO", "get_linux_disk_io_status"),
		linuxSkillNode("collect_kernel_events", "Collect kernel events", "get_linux_kernel_event_summary"),
		controlNode("query_prometheus_if_available", "Query Prometheus if available"),
		controlNode("query_recent_changes", "Query recent changes"),
	}
	return linuxDiagnosisDAG("linux_cpu_diagnosis_workflow", "CPU Diagnosis", "Diagnose Linux CPU pressure from bounded process, memory, IO, kernel and change evidence.", parallel, []Node{
		linuxSkillNode("run_cpu_rules", "Run CPU rules", "diagnose_linux_cpu_pressure"),
	})
}

func linuxMemoryWorkflow() Definition {
	parallel := []Node{
		linuxSkillNode("collect_memory", "Collect memory and top processes", "get_linux_memory_status"),
		linuxSkillNode("collect_kernel_events", "Collect kernel events", "get_linux_kernel_event_summary"),
		linuxSkillNode("collect_system_logs", "Collect system logs", "get_linux_system_log_summary"),
		controlNode("query_prometheus_if_available", "Query Prometheus if available"),
		controlNode("query_recent_changes", "Query recent changes"),
	}
	return linuxDiagnosisDAG("linux_memory_diagnosis_workflow", "Memory Diagnosis", "Diagnose Linux memory pressure from memory, kernel, log, metrics and change evidence.", parallel, []Node{
		linuxSkillNode("run_memory_rules", "Run memory rules", "diagnose_linux_memory_pressure"),
	})
}

func linuxDiskWorkflow() Definition {
	parallel := []Node{
		linuxSkillNode("collect_filesystem", "Collect filesystem and inode usage", "get_linux_filesystem_status"),
		linuxSkillNode("collect_disk_io", "Collect disk IO", "get_linux_disk_io_status"),
		linuxSkillNode("collect_kernel_events", "Collect kernel events", "get_linux_kernel_event_summary"),
		controlNode("query_application_logs", "Query application logs if configured"),
	}
	return linuxDiagnosisDAG("linux_disk_diagnosis_workflow", "Disk Diagnosis", "Diagnose Linux capacity and IO pressure without broad directory scans.", parallel, []Node{
		linuxSkillNode("run_capacity_rules", "Run filesystem capacity rules", "diagnose_linux_disk_capacity"),
		linuxSkillNode("run_disk_io_rules", "Run disk IO rules", "diagnose_linux_disk_io"),
	})
}

func linuxNetworkWorkflow() Definition {
	parallel := []Node{
		linuxSkillNode("collect_network", "Collect network and listening ports", "get_linux_network_status"),
		linuxSkillNode("collect_kernel_events", "Collect kernel events", "get_linux_kernel_event_summary"),
		controlNode("get_host_topology", "Get host topology"),
		controlNode("query_related_alerts", "Query related service alerts"),
	}
	return linuxDiagnosisDAG("linux_network_diagnosis_workflow", "Network Diagnosis", "Diagnose Linux network state without active port scanning.", parallel, []Node{
		linuxSkillNode("run_network_rules", "Run network rules", "diagnose_linux_network"),
	})
}

func linuxBatchHealthWorkflow() Definition {
	nodes := []Node{
		{ID: "start", Type: NodeTypeStart, Name: "Start"},
		controlNode("resolve_host_scope", "Resolve host IDs, group and filters"),
		linuxSkillNode("batch_diagnose", "Batch diagnose Linux hosts", "batch_diagnose_linux_hosts"),
		controlNode("aggregate_host_health", "Aggregate healthy, warning, critical and unknown hosts"),
		controlNode("identify_common_findings", "Identify common findings"),
		controlNode("find_common_dependencies", "Find common runtime hosts or dependencies"),
		controlNode("generate_batch_report", "Generate batch report"),
		{ID: "end", Type: NodeTypeEnd, Name: "End"},
	}
	edges := linearEdges(nodes)
	return linuxWorkflow("linux_batch_health_workflow", "Batch Health", "Run bounded low-cost Linux host diagnosis with a maximum of 200 hosts and concurrency 10.", nodes, edges)
}

func linuxDiagnosisDAG(name, title, description string, parallel, rules []Node) Definition {
	nodes := []Node{{ID: "start", Type: NodeTypeStart, Name: "Start"}, linuxSkillNode("test_connection", "Test connection", "test_linux_server_connection")}
	nodes = append(nodes, parallel...)
	nodes = append(nodes, Node{ID: "merge_evidence", Type: NodeTypeMerge, Name: "Merge evidence"})
	nodes = append(nodes, rules...)
	nodes = append(nodes,
		Node{ID: "merge_rules", Type: NodeTypeMerge, Name: "Merge rule findings"},
		linuxKnowledgeNode("search_knowledge", "Search Linux knowledge", title),
		controlNode("correlate", "Correlate evidence"),
		linuxAgentNode("linux_server_agent", "Linux Server Agent"),
		Node{ID: "end", Type: NodeTypeEnd, Name: "End"},
	)
	edges := []Edge{{From: "start", To: "test_connection"}}
	for _, node := range parallel {
		edges = append(edges, Edge{From: "test_connection", To: node.ID}, Edge{From: node.ID, To: "merge_evidence"})
	}
	for _, node := range rules {
		edges = append(edges, Edge{From: "merge_evidence", To: node.ID}, Edge{From: node.ID, To: "merge_rules"})
	}
	edges = append(edges,
		Edge{From: "merge_rules", To: "search_knowledge"},
		Edge{From: "search_knowledge", To: "correlate"},
		Edge{From: "correlate", To: "linux_server_agent"},
		Edge{From: "linux_server_agent", To: "end"},
	)
	return linuxWorkflow(name, title, description, nodes, edges)
}

func linuxWorkflow(name, title, description string, nodes []Node, edges []Edge) Definition {
	return Definition{Name: name, Version: builtinWorkflowVersion, Description: fmt.Sprintf("%s: %s", title, description), Nodes: nodes, Edges: edges}
}

func linuxSkillNode(id, name, skill string) Node {
	return Node{ID: id, Type: NodeTypeSkill, Name: name, SkillName: skill}
}

func linuxKnowledgeNode(id, name, query string) Node {
	return Node{ID: id, Type: NodeTypeSkill, Name: name, SkillName: "search_knowledge", Config: rawConfig(map[string]any{"input": map[string]any{"query": query, "limit": 5}})}
}

func linuxAgentNode(id, name string) Node {
	return Node{ID: id, Type: NodeTypeAgent, Name: name, AgentName: "linux_server_agent"}
}

func linearEdges(nodes []Node) []Edge {
	edges := make([]Edge, 0, len(nodes)-1)
	for index := 0; index < len(nodes)-1; index++ {
		edges = append(edges, Edge{From: nodes[index].ID, To: nodes[index+1].ID})
	}
	return edges
}
