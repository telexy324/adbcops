import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { DashboardPage } from "@/pages/dashboard-page";
import { KnowledgePage } from "@/pages/knowledge-page";
import { LoginPage } from "@/pages/login-page";

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
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });

    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>
          <KnowledgePage />
        </MemoryRouter>
      </QueryClientProvider>,
    );

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
