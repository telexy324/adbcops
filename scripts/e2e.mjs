#!/usr/bin/env node

import { readFile } from "node:fs/promises";
import { basename } from "node:path";

const baseURL = env("E2E_BASE_URL", "http://127.0.0.1:8080").replace(/\/+$/, "");
const adminUsername = env("E2E_ADMIN_USERNAME", "admin");
const adminPassword = env("E2E_ADMIN_PASSWORD", "initial-admin-password");
const strictExternal = env("E2E_STRICT_EXTERNAL", "0") === "1";
const runID = new Date().toISOString().replace(/[-:.TZ]/g, "").slice(0, 14);

const state = {
  adminToken: "",
  userToken: "",
  operator: {
    username: `e2e_operator_${runID}`,
    password: `operator-${runID}-password`,
  },
  ids: {},
};

const steps = [
  ["admin 初始化", adminBootstrap],
  ["用户创建", createUser],
  ["LLM 配置", createLLMConfig],
  ["文档上传与发布", uploadAndPublishDocument],
  ["RAG", runRAG],
  ["日志数据源", createLogDataSource],
  ["K8s 数据源", createK8sDataSource],
  ["Prometheus 数据源", createPrometheusDataSource],
  ["Agent 分析", runAgentAnalysis],
  ["Workflow", runWorkflow],
  ["Alert Webhook", sendAlertWebhook],
  ["Incident", createIncident],
  ["Topology", createTopology],
  ["审计", checkAudit],
  ["权限隔离", checkRBACIsolation],
];

async function main() {
  console.log(`AIOps E2E baseURL=${baseURL} strictExternal=${strictExternal}`);
  const startedAt = Date.now();
  for (const [name, fn] of steps) {
    const stepStarted = Date.now();
    try {
      await fn();
      console.log(`PASS ${name} (${Date.now() - stepStarted}ms)`);
    } catch (error) {
      console.error(`FAIL ${name}`);
      console.error(error?.stack || error);
      process.exitCode = 1;
      return;
    }
  }
  console.log(`PASS all ${steps.length} E2E checks (${Date.now() - startedAt}ms)`);
}

async function adminBootstrap() {
  const health = await request("GET", "/api/health");
  assertEqual(health.status, 200, "health status");
  const ready = await request("GET", "/api/ready");
  assertEqual(ready.status, 200, "readiness status");

  const login = await request("POST", "/api/auth/login", {
    body: { username: adminUsername, password: adminPassword },
  });
  assertEqual(login.status, 200, "admin login status");
  state.adminToken = must(login.body, "data.accessToken");
  assertEqual(must(login.body, "data.user.role"), "admin", "admin role");
}

async function createUser() {
  const create = await request("POST", "/api/users", {
    token: state.adminToken,
    body: {
      username: state.operator.username,
      password: state.operator.password,
      displayName: "E2E Operator",
      role: "user",
    },
  });
  assertOneOf(create.status, [200, 409], "create user status");

  const login = await request("POST", "/api/auth/login", {
    body: { username: state.operator.username, password: state.operator.password },
  });
  assertEqual(login.status, 200, "operator login status");
  state.userToken = must(login.body, "data.accessToken");
  assertEqual(must(login.body, "data.user.role"), "user", "operator role");
}

async function createLLMConfig() {
  const create = await request("POST", "/api/llm-configs", {
    token: state.adminToken,
    body: {
      name: `e2e-llm-${runID}`,
      provider: "openai-compatible",
      baseUrl: "https://api.openai.example/v1",
      model: "e2e-model",
      apiKey: "e2e-api-key",
      temperature: 0.2,
      enabled: true,
      isDefault: true,
    },
  });
  assertEqual(create.status, 200, "create llm status");
  state.ids.llm = must(create.body, "data.id");
  assertEqual(must(create.body, "data.apiKeyConfigured"), true, "api key configured");
}

async function uploadAndPublishDocument() {
  const content = [
    "# E2E Runbook",
    "",
    "支付系统数据库连接池耗尽时，应先查看连接数、慢查询和最近变更。",
    "如果 Web Pod 出现 CrashLoopBackOff，应检查上一轮容器日志、探针和依赖服务。",
  ].join("\n");
  const form = new FormData();
  form.set("title", `E2E Runbook ${runID}`);
  form.set("systemName", "payments");
  form.set("componentName", "api");
  form.set("environment", "prod");
  form.set("docType", "runbook");
  form.set("version", "v1");
  form.set("tags", JSON.stringify(["e2e", "runbook"]));
  form.set("file", new Blob([content], { type: "text/markdown" }), `e2e-${runID}.md`);

  const upload = await request("POST", "/api/documents/upload", {
    token: state.adminToken,
    form,
  });
  assertEqual(upload.status, 200, "upload document status");
  state.ids.document = must(upload.body, "data.id");

  const quality = await request("POST", `/api/documents/${state.ids.document}/review`, {
    token: state.adminToken,
    body: {
      result: {
        score: 85,
        summary: "内容包含范围、症状和处理建议，适合发布。",
        findings: ["scope clear", "actionable steps"],
        suggestions: ["keep ownership updated"],
      },
    },
  });
  assertEqual(quality.status, 200, "quality review status");
  assertEqual(must(quality.body, "data.canPublish"), true, "document can publish");

  const publish = await request("POST", `/api/documents/${state.ids.document}/review`, {
    token: state.adminToken,
    body: { action: "publish", comment: "E2E publish" },
  });
  assertEqual(publish.status, 200, "publish document status");
  assertEqual(must(publish.body, "data.document.status"), "published", "document published");

  const reprocess = await request("POST", `/api/documents/${state.ids.document}/reprocess`, {
    token: state.adminToken,
  });
  assertEqual(reprocess.status, 200, "reprocess document status");
  assert(must(reprocess.body, "data.chunkCount") > 0, "document reprocess generated chunks");

  const chunks = await request("GET", `/api/documents/${state.ids.document}/chunks`, {
    token: state.adminToken,
  });
  assertEqual(chunks.status, 200, "list chunks status");
  assert(must(chunks.body, "data.chunkCount") > 0, "document chunks generated");
}

async function runRAG() {
  const rag = await request("POST", "/api/knowledge/ask", {
    token: state.adminToken,
    body: {
      question: "支付系统数据库连接池耗尽时应该先检查什么？",
      limit: 3,
    },
  });
  assertEqual(rag.status, 200, "rag status");
  assert(must(rag.body, "data.answer").length > 0, "rag answer exists");
  state.ids.conversation = must(rag.body, "data.conversation.id");
}

async function createLogDataSource() {
  state.ids.logs = await createDataSource({
    name: `e2e-logs-${runID}`,
    sourceType: "elasticsearch",
    config: { baseUrl: "https://example.com/elasticsearch", index: "logs-*" },
    credential: { username: "elastic", password: "elastic-password" },
  });
}

async function createK8sDataSource() {
  state.ids.k8s = await createDataSource({
    name: `e2e-k8s-${runID}`,
    sourceType: "kubernetes",
    config: { apiServer: "https://kubernetes.example.com", namespace: "default" },
    credential: { bearerToken: "k8s-token" },
  });
}

async function createPrometheusDataSource() {
  state.ids.prometheus = await createDataSource({
    name: `e2e-prom-${runID}`,
    sourceType: "prometheus",
    config: { baseUrl: "https://example.com/prometheus" },
    credential: { bearerToken: "prom-token" },
  });
}

async function createDataSource(input) {
  const create = await request("POST", "/api/data-sources", {
    token: state.adminToken,
    body: {
      environment: "prod",
      systemName: "payments",
      componentName: "api",
      enabled: true,
      readOnly: true,
      ...input,
    },
  });
  assertEqual(create.status, 200, `create ${input.sourceType} data source`);
  const id = must(create.body, "data.id");
  assertEqual(must(create.body, "data.credentialConfigured"), true, "credential configured");

  if (strictExternal) {
    const test = await request("POST", `/api/data-sources/${id}/test`, { token: state.adminToken });
    assertEqual(test.status, 200, `test ${input.sourceType} data source`);
    assertEqual(must(test.body, "data.ok"), true, "data source test ok");
  }
  return id;
}

async function runAgentAnalysis() {
  const agent = await request("POST", "/api/agents/knowledge_agent/test", {
    token: state.adminToken,
    body: {
      context: {
        query: "数据库连接池耗尽排查",
        variables: { query: "数据库连接池耗尽", limit: 3 },
        evidence: [],
      },
    },
  });
  assertEqual(agent.status, 200, "agent status");
  assert(must(agent.body, "data.result.summary").length > 0, "agent summary exists");
}

async function runWorkflow() {
  const list = await request("GET", "/api/workflows?limit=50", { token: state.adminToken });
  assertEqual(list.status, 200, "list workflow status");
  const workflow = must(list.body, "data").find((item) => item.name === "knowledge_qa_workflow");
  assert(workflow, "knowledge_qa_workflow exists");

  const run = await request("POST", `/api/workflows/${workflow.id}/run`, {
    token: state.adminToken,
    body: {
      input: { query: "数据库连接池耗尽排查" },
      conversationId: state.ids.conversation,
      timeoutSeconds: 30,
    },
  });
  assertEqual(run.status, 200, "run workflow status");
  state.ids.workflowRun = must(run.body, "data.id");
  assertOneOf(must(run.body, "data.status"), ["success", "failed"], "workflow terminal status");
}

async function sendAlertWebhook() {
  const alert = await request("POST", "/api/events/alertmanager", {
    body: {
      receiver: "e2e",
      status: "firing",
      groupLabels: { alertname: "E2EDatabasePoolExhausted" },
      commonLabels: {
        alertname: "E2EDatabasePoolExhausted",
        severity: "critical",
        namespace: "payments",
        pod: "payments-api-0",
      },
      commonAnnotations: {
        summary: "E2E database connection pool exhausted",
        description: "Synthetic E2E alert",
      },
      alerts: [
        {
          status: "firing",
          labels: {
            alertname: "E2EDatabasePoolExhausted",
            severity: "critical",
            namespace: "payments",
            pod: "payments-api-0",
          },
          annotations: { summary: "E2E database connection pool exhausted" },
          startsAt: new Date().toISOString(),
        },
      ],
    },
  });
  assertEqual(alert.status, 200, "alert webhook status");
  state.ids.event = must(alert.body, "data.events.0.id");
}

async function createIncident() {
  const incident = await request("POST", "/api/incidents", {
    token: state.adminToken,
    body: {
      title: `E2E Incident ${runID}`,
      severity: "critical",
      environment: "prod",
      systemName: "payments",
      componentName: "api",
      summary: "Synthetic E2E incident for final acceptance.",
      eventIds: [state.ids.event],
      evidenceKeys: [],
      rootCauses: [
        { summary: "数据库连接池耗尽", score: 0.72, details: { source: "e2e" } },
      ],
    },
  });
  assertEqual(incident.status, 200, "create incident status");
  state.ids.incident = must(incident.body, "data.incident.id");

  const similar = await request("GET", `/api/incidents/${state.ids.incident}/similar?limit=5`, {
    token: state.adminToken,
  });
  assertEqual(similar.status, 200, "similar incident status");
}

async function createTopology() {
  const serviceNode = {
    nodeKey: `service:prod:payments:api:${runID}`,
    kind: "service",
    name: "payments-api",
    environment: "prod",
    namespace: "payments",
    sourceType: "e2e",
  };
  const dbNode = {
    nodeKey: `database:prod:payments:postgres:${runID}`,
    kind: "database",
    name: "payments-postgres",
    environment: "prod",
    namespace: "payments",
    sourceType: "e2e",
  };
  for (const node of [serviceNode, dbNode]) {
    const upsert = await request("POST", "/api/topology/nodes", {
      token: state.adminToken,
      body: node,
    });
    assertEqual(upsert.status, 200, `upsert topology node ${node.kind}`);
  }
  const edge = await request("POST", "/api/topology/edges", {
    token: state.adminToken,
    body: {
      edgeKey: `depends_on:${serviceNode.nodeKey}->${dbNode.nodeKey}`,
      fromNodeKey: serviceNode.nodeKey,
      toNodeKey: dbNode.nodeKey,
      edgeType: "depends_on",
      confidence: 0.9,
      sourceType: "e2e",
    },
  });
  assertEqual(edge.status, 200, "upsert topology edge");

  const graph = await request("GET", "/api/topology/graph?environment=prod&limit=50", {
    token: state.adminToken,
  });
  assertEqual(graph.status, 200, "topology graph status");
  assert(must(graph.body, "data.nodes").some((node) => node.nodeKey === serviceNode.nodeKey), "topology node in graph");
}

async function checkAudit() {
  const audit = await request("GET", "/api/audit-logs?limit=20", {
    token: state.adminToken,
  });
  assertEqual(audit.status, 200, "audit list status");
  const logs = must(audit.body, "data");
  assert(Array.isArray(logs) && logs.length > 0, "audit logs exist");
}

async function checkRBACIsolation() {
  const forbiddenUsers = await request("GET", "/api/users", { token: state.userToken });
  assertEqual(forbiddenUsers.status, 403, "operator cannot list users");

  const forbiddenDataSource = await request("POST", "/api/data-sources", {
    token: state.userToken,
    body: {
      name: `forbidden-${runID}`,
      sourceType: "prometheus",
      config: { baseUrl: "https://example.com/prometheus" },
      credential: { bearerToken: "nope" },
    },
  });
  assertEqual(forbiddenDataSource.status, 403, "operator cannot create data source");
}

async function request(method, path, options = {}) {
  const headers = {
    "x-request-id": `req-e2e-${runID}-${Math.random().toString(16).slice(2)}`,
    ...(options.headers || {}),
  };
  let body;
  if (options.token) {
    headers.authorization = `Bearer ${options.token}`;
  }
  if (options.form) {
    body = options.form;
  } else if (options.body !== undefined) {
    headers["content-type"] = "application/json";
    body = JSON.stringify(options.body);
  }
  const response = await fetch(baseURL + path, { method, headers, body });
  const text = await response.text();
  let parsed = {};
  if (text) {
    try {
      parsed = JSON.parse(text);
    } catch {
      parsed = { raw: text };
    }
  }
  return { status: response.status, body: parsed };
}

function must(value, path) {
  let cursor = value;
  for (const part of path.split(".")) {
    if (cursor == null) {
      throw new Error(`missing ${path}`);
    }
    cursor = /^\d+$/.test(part) ? cursor[Number(part)] : cursor[part];
  }
  if (cursor === undefined || cursor === null) {
    throw new Error(`missing ${path}`);
  }
  return cursor;
}

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

function assertEqual(actual, expected, message) {
  if (actual !== expected) {
    throw new Error(`${message}: got ${JSON.stringify(actual)}, want ${JSON.stringify(expected)}`);
  }
}

function assertOneOf(actual, expected, message) {
  if (!expected.includes(actual)) {
    throw new Error(`${message}: got ${JSON.stringify(actual)}, want one of ${JSON.stringify(expected)}`);
  }
}

function env(name, fallback) {
  return process.env[name] || fallback;
}

if (process.argv.includes("--help") || process.argv.includes("-h")) {
  const script = basename(process.argv[1] || "scripts/e2e.mjs");
  console.log(`Usage: node ${script}

Environment:
  E2E_BASE_URL          API base URL, default http://127.0.0.1:8080
  E2E_ADMIN_USERNAME    initial admin username, default admin
  E2E_ADMIN_PASSWORD    initial admin password, default initial-admin-password
  E2E_STRICT_EXTERNAL   set to 1 to require data source test endpoints
`);
} else {
  main();
}
