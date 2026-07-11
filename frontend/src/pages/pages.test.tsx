import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactElement } from "react";

import { AnalysisPage } from "@/pages/analysis-page";
import { DashboardPage } from "@/pages/dashboard-page";
import { KnowledgePage } from "@/pages/knowledge-page";
import { LoginPage } from "@/pages/login-page";
import { WorkflowPage } from "@/pages/workflow-page";

vi.mock("@/api/auth", () => ({
  login: vi.fn().mockRejectedValue(new Error("bad credentials")),
}));

vi.mock("@/api/knowledge", () => ({
  askKnowledge: vi.fn(),
  getDocumentChunks: vi.fn(),
  listDocuments: vi.fn().mockResolvedValue([]),
  reprocessDocument: vi.fn(),
  reviewAction: vi.fn(),
  reviewQuality: vi.fn(),
  toAPIErrorMessage: vi.fn(() => "请求失败"),
  uploadDocument: vi.fn(),
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
