import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactElement } from "react";

import { AnalysisPage } from "@/pages/analysis-page";
import { DashboardPage } from "@/pages/dashboard-page";
import { KnowledgePage } from "@/pages/knowledge-page";
import { LoginPage } from "@/pages/login-page";
import { OperationsPage } from "@/pages/operations-page";
import { SettingsPage } from "@/pages/settings-page";
import { TopologyConfigurationPage } from "@/pages/topology-configuration-page";
import { TopologyPage } from "@/pages/topology-page";
import { WorkflowPage } from "@/pages/workflow-page";

vi.mock("@/api/auth", () => ({
  login: vi.fn().mockRejectedValue(new Error("bad credentials")),
}));

vi.mock("@/api/knowledge", () => ({
  autoReviewQuality: vi.fn(),
  askKnowledge: vi.fn(),
  getDocumentChunks: vi.fn(),
  listQualityStandards: vi.fn().mockResolvedValue([]),
  listDocuments: vi.fn().mockResolvedValue([]),
  reprocessDocument: vi.fn(),
  reviewAction: vi.fn(),
  reviewQuality: vi.fn(),
  toAPIErrorMessage: vi.fn(() => "请求失败"),
  uploadDocument: vi.fn(),
  uploadQualityStandard: vi.fn(),
}));

vi.mock("@/api/analysis", () => ({
  diagnosePod: vi.fn(),
  listAnalysisTasks: vi.fn().mockResolvedValue([]),
  queryMetrics: vi.fn(),
  runGeneralAnalysis: vi.fn(),
  sendAlertmanagerWebhook: vi.fn(),
  toAPIErrorMessage: vi.fn(() => "请求失败"),
}));

vi.mock("@/api/workflows", () => ({
  createWorkflow: vi.fn(),
  listWorkflowRuns: vi.fn().mockResolvedValue([
    {
      id: 10,
      workflowId: 1,
      status: "success",
      createdAt: "2026-07-12T10:00:00Z",
      nodeRuns: [
        {
          id: 1,
          nodeId: "start",
          nodeType: "start",
          status: "success",
          attempt: 1,
        },
      ],
    },
  ]),
  listWorkflows: vi.fn().mockResolvedValue([
    {
      id: 1,
      name: "knowledge_qa_workflow",
      version: "v1",
      enabled: true,
      createdAt: "2026-07-12T10:00:00Z",
      updatedAt: "2026-07-12T10:00:00Z",
      definition: {
        name: "knowledge_qa_workflow",
        version: "v1",
        nodes: [],
        edges: [],
      },
    },
  ]),
  runWorkflow: vi.fn(),
  toAPIErrorMessage: vi.fn(() => "请求失败"),
  validateWorkflow: vi.fn().mockResolvedValue({
    valid: false,
    errors: ["agent node knowledge references unknown agent: missing_agent"],
    warnings: [],
  }),
}));

vi.mock("@/api/operations", () => ({
  createTopologySavedView: vi.fn().mockResolvedValue({
    id: 1,
    name: "生产服务依赖视图",
    ownerId: 1,
    visibility: "private",
    queryConfig: {},
    displayConfig: {},
    isDefault: false,
    createdAt: "2026-07-12T10:00:00Z",
    updatedAt: "2026-07-12T10:00:00Z",
  }),
  confirmRootCause: vi.fn(),
  expandTopology: vi.fn().mockResolvedValue({
    rootKey: "service:payment-api",
    direction: "both",
    depth: 2,
    evidenceKey: "",
    cycleDetected: false,
    truncated: false,
    paths: [
      {
        targetNodeKey: "db:orders",
        hops: 1,
        nodeKeys: ["service:payment-api", "db:orders"],
        edgeKeys: ["service:payment-api->db:orders"],
        confidence: 0.92,
        impactType: "dependency",
      },
    ],
    nodes: [
      {
        id: 1,
        nodeKey: "service:payment-api",
        kind: "service",
        name: "payment-api",
        sourceType: "manual",
        createdAt: "2026-07-12T10:00:00Z",
        updatedAt: "2026-07-12T10:00:00Z",
      },
      {
        id: 2,
        nodeKey: "db:orders",
        kind: "database",
        name: "orders",
        sourceType: "manual",
        createdAt: "2026-07-12T10:00:00Z",
        updatedAt: "2026-07-12T10:00:00Z",
      },
    ],
    edges: [
      {
        id: 1,
        edgeKey: "service:payment-api->db:orders",
        fromNodeKey: "service:payment-api",
        toNodeKey: "db:orders",
        edgeType: "depends_on",
        sourceType: "manual",
        createdAt: "2026-07-12T10:00:00Z",
        updatedAt: "2026-07-12T10:00:00Z",
      },
    ],
  }),
  getBlastRadius: vi.fn().mockResolvedValue({
    rootKey: "service:payment-api",
    direction: "both",
    hops: 2,
    cycleDetected: false,
    nodes: [],
    edges: [],
  }),
  getIncident: vi.fn().mockResolvedValue({
    incident: {
      id: 1,
      incidentKey: "INC-20260712-0001",
      title: "支付接口错误率升高",
      severity: "critical",
      status: "investigating",
      environment: "prod",
      systemName: "payment",
      componentName: "payment-api",
      summary: "错误率在发布后明显升高。",
      createdAt: "2026-07-12T10:00:00Z",
      updatedAt: "2026-07-12T10:00:00Z",
    },
    events: [
      { id: 1, incidentId: 1, eventId: 101, createdAt: "2026-07-12T10:00:00Z" },
    ],
    evidence: [
      {
        id: 1,
        incidentId: 1,
        evidenceKey: "event:101",
        createdAt: "2026-07-12T10:00:00Z",
      },
    ],
    rootCauses: [
      {
        id: 1,
        incidentId: 1,
        summary: "最近一次发布引入配置变更",
        score: 0.86,
        confirmed: false,
        createdAt: "2026-07-12T10:00:00Z",
        updatedAt: "2026-07-12T10:00:00Z",
      },
    ],
    activities: [],
  }),
  getSimilarIncidents: vi.fn().mockResolvedValue([
    {
      incident: {
        id: 2,
        incidentKey: "INC-20260701-0002",
        title: "支付服务发布后错误率升高",
        severity: "major",
        status: "resolved",
        environment: "prod",
        systemName: "payment",
        createdAt: "2026-07-01T10:00:00Z",
        updatedAt: "2026-07-01T11:00:00Z",
      },
      score: 0.78,
      reasons: ["error template matched", "shared payment tag"],
      advisoryOnly: true,
      notice: "仅供参考，不自动确认历史根因。",
    },
  ]),
  getTimeline: vi.fn().mockResolvedValue({
    from: "2026-07-12T08:00:00Z",
    to: "2026-07-12T10:30:00Z",
    timezone: "Asia/Shanghai",
    anchorEventId: 101,
    sourceCounts: { alertmanager: 1 },
    items: [
      {
        eventId: 101,
        time: "2026-07-12T10:00:00Z",
        sourceType: "alertmanager",
        eventType: "alert",
        severity: "critical",
        status: "firing",
        summary: "HighErrorRate firing",
      },
    ],
  }),
  getTopologyGraph: vi.fn().mockResolvedValue({
    nodes: [
      {
        id: 1,
        nodeKey: "service:payment-api",
        kind: "service",
        name: "payment-api",
        sourceType: "manual",
        createdAt: "2026-07-12T10:00:00Z",
        updatedAt: "2026-07-12T10:00:00Z",
      },
      {
        id: 2,
        nodeKey: "db:orders",
        kind: "database",
        name: "orders",
        sourceType: "manual",
        createdAt: "2026-07-12T10:00:00Z",
        updatedAt: "2026-07-12T10:00:00Z",
      },
    ],
    edges: [
      {
        id: 1,
        edgeKey: "service:payment-api->db:orders",
        fromNodeKey: "service:payment-api",
        toNodeKey: "db:orders",
        edgeType: "depends_on",
        sourceType: "manual",
        createdAt: "2026-07-12T10:00:00Z",
        updatedAt: "2026-07-12T10:00:00Z",
      },
    ],
  }),
  listTopologyConflicts: vi.fn().mockResolvedValue([
    {
      id: 1,
      conflictType: "node_attribute",
      status: "open",
      description: "payment-api owner differs across sources",
      candidates: [{ owner: "team-a", token: "secret-token" }],
      createdAt: "2026-07-12T10:00:00Z",
    },
  ]),
  listTopologyNodeTypes: vi.fn().mockResolvedValue([
    {
      id: 1,
      typeKey: "service",
      displayName: "Service",
      category: "runtime",
      enabled: true,
      builtIn: true,
      createdAt: "2026-07-12T10:00:00Z",
      updatedAt: "2026-07-12T10:00:00Z",
    },
  ]),
  listTopologyRelationTypes: vi.fn().mockResolvedValue([
    {
      id: 1,
      typeKey: "depends_on",
      displayName: "Depends On",
      semantics: "runtime_dep",
      failurePropagation: "src_to_dst",
      defaultDirection: "outbound",
      propagatesFailure: true,
      enabled: true,
      builtIn: true,
      createdAt: "2026-07-12T10:00:00Z",
      updatedAt: "2026-07-12T10:00:00Z",
    },
  ]),
  listTopologySources: vi.fn().mockResolvedValue([
    {
      id: 1,
      name: "prod-cmdb",
      sourceType: "cmdb",
      enabled: true,
      priority: 100,
      scope: {},
      mappingRules: {
        nodeMappings: [],
        edgeMappings: [],
      },
      staleAfterSeconds: 86400,
      deleteAfterSeconds: 604800,
      createdAt: "2026-07-12T10:00:00Z",
      updatedAt: "2026-07-12T10:00:00Z",
    },
  ]),
  listTopologySyncRuns: vi.fn().mockResolvedValue([
    {
      id: 1,
      sourceConfigId: 1,
      triggerType: "manual",
      status: "success",
      discoveredNodes: 2,
      discoveredEdges: 1,
      createdNodes: 1,
      updatedNodes: 1,
      staleNodes: 0,
      createdEdges: 1,
      updatedEdges: 0,
      staleEdges: 0,
      conflictCount: 1,
      warningCount: 0,
      createdAt: "2026-07-12T10:00:00Z",
    },
  ]),
  listTopologySavedViews: vi.fn().mockResolvedValue([
    {
      id: 1,
      name: "生产服务依赖视图",
      ownerId: 1,
      visibility: "private",
      queryConfig: { nodeKey: "service:payment-api", depth: 2 },
      displayConfig: { layout: "svg-layered" },
      isDefault: false,
      createdAt: "2026-07-12T10:00:00Z",
      updatedAt: "2026-07-12T10:00:00Z",
    },
  ]),
  listIncidents: vi.fn().mockResolvedValue([
    {
      id: 1,
      incidentKey: "INC-20260712-0001",
      title: "支付接口错误率升高",
      severity: "critical",
      status: "investigating",
      createdAt: "2026-07-12T10:00:00Z",
      updatedAt: "2026-07-12T10:00:00Z",
    },
  ]),
  previewTopologySourceMapping: vi.fn().mockResolvedValue({
    nodes: [
      {
        nodeKey: "service:payment-api",
        nodeType: "service",
        name: "payment-api",
        attributes: { namespace: "prod", token: "secret-token" },
      },
    ],
    edges: [
      {
        fromNodeKey: "service:payment-api",
        toNodeKey: "service:orders-api",
        relationType: "depends_on",
        confidence: 0.85,
      },
    ],
    unresolved: [],
    warnings: [],
    truncated: false,
  }),
  resolveTopologyConflict: vi.fn().mockResolvedValue({
    id: 1,
    conflictType: "node_attribute",
    status: "resolved",
    description: "resolved",
    createdAt: "2026-07-12T10:00:00Z",
  }),
  runTopologySourceSync: vi.fn().mockResolvedValue({
    id: 2,
    sourceConfigId: 1,
    triggerType: "manual",
    status: "running",
    discoveredNodes: 0,
    discoveredEdges: 0,
    createdNodes: 0,
    updatedNodes: 0,
    staleNodes: 0,
    createdEdges: 0,
    updatedEdges: 0,
    staleEdges: 0,
    conflictCount: 0,
    warningCount: 0,
    createdAt: "2026-07-12T10:00:00Z",
  }),
  toAPIErrorMessage: vi.fn(() => "请求失败"),
  updateTopologyRelationType: vi.fn(),
  updateTopologySource: vi.fn().mockResolvedValue({
    id: 1,
    name: "prod-cmdb",
    sourceType: "cmdb",
    enabled: true,
    priority: 100,
    staleAfterSeconds: 86400,
    deleteAfterSeconds: 604800,
    createdAt: "2026-07-12T10:00:00Z",
    updatedAt: "2026-07-12T10:00:00Z",
  }),
}));

vi.mock("@/api/config", () => ({
  createDataSource: vi.fn(),
  createLLMConfig: vi.fn(),
  listDataSources: vi.fn().mockResolvedValue([
    {
      id: 1,
      name: "prod-logs",
      sourceType: "elasticsearch",
      environment: "prod",
      config: { baseUrl: "https://es.example.com", index: "logs-*" },
      credentialConfigured: true,
      enabled: true,
      readOnly: true,
      createdAt: "2026-07-12T10:00:00Z",
      updatedAt: "2026-07-12T10:00:00Z",
    },
  ]),
  listLLMConfigs: vi.fn().mockResolvedValue([
    {
      id: 1,
      name: "default-llm",
      provider: "openai-compatible",
      baseUrl: "https://api.openai.example/v1",
      model: "ops-model",
      purpose: "chat",
      temperature: 0.2,
      enabled: true,
      isDefault: true,
      apiKeyConfigured: true,
      apiSecretConfigured: false,
      createdAt: "2026-07-12T10:00:00Z",
      updatedAt: "2026-07-12T10:00:00Z",
    },
    {
      id: 2,
      name: "default-embedding",
      provider: "openai-compatible",
      baseUrl: "https://api.openai.example/v1",
      model: "embedding-model",
      purpose: "embedding",
      temperature: 0,
      enabled: true,
      isDefault: true,
      apiKeyConfigured: true,
      apiSecretConfigured: true,
      createdAt: "2026-07-12T10:00:00Z",
      updatedAt: "2026-07-12T10:00:00Z",
    },
    {
      id: 3,
      name: "default-rerank",
      provider: "openai-compatible",
      baseUrl: "https://api.openai.example/v1",
      model: "rerank-model",
      purpose: "rerank",
      temperature: 0,
      enabled: true,
      isDefault: true,
      apiKeyConfigured: true,
      apiSecretConfigured: false,
      createdAt: "2026-07-12T10:00:00Z",
      updatedAt: "2026-07-12T10:00:00Z",
    },
  ]),
  testDataSource: vi.fn(),
  testLLMConfig: vi.fn(),
  toAPIErrorMessage: vi.fn(() => "请求失败"),
}));

function renderWithQueryClient(element: ReactElement) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>{element}</MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("LoginPage", () => {
  it("renders the login form and reports authentication failure", async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <LoginPage />
      </MemoryRouter>,
    );

    await user.type(screen.getByLabelText("用户名"), "admin");
    await user.type(screen.getByLabelText("密码"), "not-a-real-password");
    await user.click(screen.getByRole("button", { name: /登录平台/ }));

    expect(await screen.findByRole("status")).toHaveTextContent(
      "登录失败，请检查用户名、密码或账号状态。",
    );
  });
});

describe("DashboardPage", () => {
  it("does not present placeholder metrics as production data", () => {
    render(
      <MemoryRouter>
        <DashboardPage />
      </MemoryRouter>,
    );

    expect(
      screen.getByRole("heading", { name: "平台总览" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("当前处于工程初始化阶段，未接入生产数据。", {
        exact: false,
      }),
    ).toBeInTheDocument();
    expect(screen.getByText("暂无分析记录")).toBeInTheDocument();
  });
});

describe("KnowledgePage", () => {
  it("renders the knowledge workflow from upload to chat", () => {
    renderWithQueryClient(<KnowledgePage />);

    expect(
      screen.getByRole("heading", { name: "知识中心" }),
    ).toBeInTheDocument();
    expect(screen.getByText("上传文档")).toBeInTheDocument();
    expect(screen.getByText("详情 / 质检 / 审核")).toBeInTheDocument();
    expect(screen.getByText("发送问题")).toBeInTheDocument();
    expect(
      screen.getByText("只有 published 文档会进入正式问答召回。", {
        exact: false,
      }),
    ).toBeInTheDocument();
  });
});

describe("AnalysisPage", () => {
  it("renders analysis entries and the current-user task panel", () => {
    renderWithQueryClient(<AnalysisPage />);

    expect(
      screen.getByRole("heading", { name: "智能分析" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "日志分析" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "K8s 诊断" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "指标查询" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "告警输入" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "我的分析任务" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("普通用户只会看到自己的分析任务。", { exact: false }),
    ).toBeInTheDocument();
  });
});

describe("WorkflowPage", () => {
  it("renders builder, runs and backend validation errors", async () => {
    const user = userEvent.setup();
    renderWithQueryClient(<WorkflowPage />);

    expect(screen.getByRole("heading", { name: "工作流" })).toBeInTheDocument();
    expect(screen.getByText("Builder")).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "运行记录" }),
    ).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /后端校验/ }));

    expect(
      await screen.findByText(
        "agent node knowledge references unknown agent: missing_agent",
      ),
    ).toBeInTheDocument();
  });
});

describe("OperationsPage", () => {
  it("renders topology, blast radius and incident investigation panels", async () => {
    renderWithQueryClient(<OperationsPage />);

    expect(
      screen.getByRole("heading", { name: "拓扑 / 故障中心" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "拓扑图谱" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Blast Radius" }),
    ).toBeInTheDocument();
    expect(await screen.findByText("支付接口错误率升高")).toBeInTheDocument();
    expect(
      await screen.findByRole("heading", { name: "Incident Timeline" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Evidence" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Root Cause Candidates" }),
    ).toBeInTheDocument();
    expect(
      await screen.findByRole("heading", { name: "历史相似 Incident" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("仅供参考，不自动确认历史根因。"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "报告导出 Markdown" }),
    ).toBeInTheDocument();
  });
});

describe("TopologyPage", () => {
  it("renders topology map filters, legends and drawer placeholder", async () => {
    renderWithQueryClient(<TopologyPage />);

    expect(
      screen.getByRole("heading", { name: "拓扑地图" }),
    ).toBeInTheDocument();
    expect(
      await screen.findByRole("heading", { name: "Topology Map" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Filter / Expand")).toBeInTheDocument();
    expect(screen.getByText("Saved View")).toBeInTheDocument();
    expect(screen.getByText("图例")).toBeInTheDocument();
    expect(
      screen.getByText("点击拓扑图中的节点查看详情。"),
    ).toBeInTheDocument();
  });
});

describe("TopologyConfigurationPage", () => {
  it("renders type catalog, source wizard, sync runs and conflict center", async () => {
    renderWithQueryClient(<TopologyConfigurationPage />);

    expect(
      screen.getByRole("heading", { name: "拓扑配置中心" }),
    ).toBeInTheDocument();
    expect(await screen.findByText("Type Catalog")).toBeInTheDocument();
    expect(
      screen.getByText("Source Wizard / Mapping Preview"),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "Preview 后才能保存 Mapping；Mapping 修改后需要重新 Preview。",
      ),
    ).toBeInTheDocument();
    expect(screen.getByText("同步进度")).toBeInTheDocument();
    expect(screen.getByText("Conflict Center")).toBeInTheDocument();
    expect(await screen.findByText("prod-cmdb · cmdb")).toBeInTheDocument();
  });
});

describe("SettingsPage", () => {
  it("renders LLM and data source configuration panels", async () => {
    renderWithQueryClient(<SettingsPage />);

    expect(
      screen.getByRole("heading", { name: "配置中心" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "LLM 配置" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Embedding 向量模型")).toBeInTheDocument();
    expect(screen.getByText("Rerank 精排模型")).toBeInTheDocument();
    expect(screen.getByText("API Secret（可选）")).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "数据源配置" }),
    ).toBeInTheDocument();
    expect(screen.getByText("日志数据源")).toBeInTheDocument();
    expect(screen.getByText("K8s 数据源")).toBeInTheDocument();
    expect(screen.getByText("Prometheus 数据源")).toBeInTheDocument();
    expect(await screen.findByText("default-llm")).toBeInTheDocument();
    expect(await screen.findByText("default-embedding")).toBeInTheDocument();
    expect(await screen.findByText("default-rerank")).toBeInTheDocument();
    expect(await screen.findByText("prod-logs")).toBeInTheDocument();
  });
});
