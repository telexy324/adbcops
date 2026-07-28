import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactElement } from "react";

import {
  cancelRCARun,
  createRCARun,
  getRCADetail,
  getRCAReport,
  listRCARuns,
  orchestrateRCARun,
  recoverRCARun,
} from "@/api/rca";
import type { RCARun } from "@/api/rca";
import { MultiRoundRCAWorkbench } from "@/components/analysis/multi-round-rca-workbench";

vi.mock("@/api/analysis", () => ({
  toAPIErrorMessage: vi.fn((error: unknown) =>
    error instanceof Error ? error.message : "请求失败",
  ),
}));

vi.mock("@/api/rca", () => ({
  cancelRCARun: vi.fn(),
  createRCARun: vi.fn(),
  getRCADetail: vi.fn(),
  getRCAReport: vi.fn(),
  listRCARuns: vi.fn(),
  orchestrateRCARun: vi.fn(),
  recoverRCARun: vi.fn(),
}));

const mockedCreate = vi.mocked(createRCARun);
const mockedList = vi.mocked(listRCARuns);
const mockedDetail = vi.mocked(getRCADetail);
const mockedOrchestrate = vi.mocked(orchestrateRCARun);
const mockedCancel = vi.mocked(cancelRCARun);
const mockedRecover = vi.mocked(recoverRCARun);
const mockedReport = vi.mocked(getRCAReport);

beforeEach(() => {
  vi.clearAllMocks();
  window.localStorage.clear();
  window.history.replaceState({}, "", "/analysis");
  mockedList.mockResolvedValue([]);
});

describe("MultiRoundRCAWorkbench", () => {
  it("shows the complete three-round demo with topology, sanitized SQL, evidence kinds and final report", () => {
    renderWorkbench(<MultiRoundRCAWorkbench demoMode />);

    expect(
      screen.getByDisplayValue("订单服务变慢，请查询可能原因"),
    ).toBeInTheDocument();
    expect(
      (screen.getByLabelText("RCA 开始时间") as HTMLInputElement).value,
    ).toMatch(/T/);
    expect(screen.getByText("多源证据采集")).toBeInTheDocument();
    expect(screen.getByText("拓扑引导调查")).toBeInTheDocument();
    expect(screen.getByText("深度根因验证")).toBeInTheDocument();
    expect(screen.getByText("拓扑扩展过程")).toBeInTheDocument();
    expect(screen.getByText("order-db")).toBeInTheDocument();
    expect(screen.getByText("a49c8d2f7e13")).toBeInTheDocument();
    expect(
      screen.getByText("SELECT * FROM orders WHERE customer_id=? AND status=?"),
    ).toBeInTheDocument();
    for (const kind of ["FACT", "RULE", "KNOWLEDGE", "HYPOTHESIS"]) {
      expect(screen.getAllByText(kind).length).toBeGreaterThan(0);
    }
    expect(screen.getByText("最终分析报告")).toBeInTheDocument();
    expect(screen.getAllByText("部分成功").length).toBeGreaterThan(0);
    expect(screen.getByText("Evidence 不完整")).toBeInTheDocument();
    expect(
      screen.getAllByText(/execution plan evidence/).length,
    ).toBeGreaterThan(0);
    expect(
      screen.queryByText("alice@example.com", { exact: false }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /执行 SQL|自动修复/ }),
    ).not.toBeInTheDocument();
  });

  it("creates a run with visible scope, polls the run and cancels without scheduling from the UI", async () => {
    const user = userEvent.setup();
    const pendingRun = testRun({ id: 88, status: "pending" });
    const runningRun = testRun({
      id: 88,
      status: "running",
      currentRound: 1,
      startedAt: "2026-07-28T05:00:00Z",
    });
    mockedCreate.mockResolvedValue(pendingRun);
    mockedDetail.mockResolvedValue({
      run: runningRun,
      rounds: [
        {
          id: 11,
          runId: 88,
          roundNumber: 1,
          status: "running",
          inputHypotheses: [],
          newEvidenceIds: [],
          rejectedHypotheses: [],
          nextActions: [],
        },
      ],
      actions: [
        testAction(1, "query_logs", "running"),
        testAction(2, "compare_metric_baseline", "success"),
      ],
      evidence: [],
      rootCauseCandidates: [],
    });
    mockedOrchestrate.mockImplementation(() => new Promise(() => undefined));
    mockedCancel.mockResolvedValue({
      ...runningRun,
      status: "cancelled",
      finishedAt: "2026-07-28T05:02:00Z",
    });

    renderWorkbench(<MultiRoundRCAWorkbench />);
    await user.clear(screen.getByLabelText("RCA 服务"));
    await user.type(screen.getByLabelText("RCA 服务"), "order-service");
    await user.click(screen.getByRole("button", { name: "开始三轮分析" }));

    await waitFor(() => expect(mockedCreate).toHaveBeenCalledTimes(1));
    const request = mockedCreate.mock.calls[0][0];
    expect(request.query).toBe("订单服务变慢，请查询可能原因");
    expect(request.scope.serviceName).toBe("order-service");
    expect(request.scope.from).toEqual(expect.stringMatching(/Z$/));
    expect(request.scope.to).toEqual(expect.stringMatching(/Z$/));
    await waitFor(() => expect(mockedOrchestrate.mock.calls[0]?.[0]).toBe(88));
    await waitFor(() =>
      expect(window.localStorage.getItem("adbcops.activeRcaRunId")).toBe("88"),
    );

    expect(await screen.findByText("query logs")).toBeInTheDocument();
    expect(screen.getByText("compare metric baseline")).toBeInTheDocument();
    expect(screen.getAllByText("运行中").length).toBeGreaterThan(0);
    await user.click(screen.getByRole("button", { name: "取消分析" }));
    await waitFor(() => expect(mockedCancel.mock.calls[0]?.[0]).toBe(88));
    expect(
      await screen.findByText("RCA #88 已取消；后端不会继续调度新的 Skill。"),
    ).toBeInTheDocument();
  });

  it("restores a timed-out run from local storage and retries only through the recover endpoint", async () => {
    const user = userEvent.setup();
    window.localStorage.setItem("adbcops.activeRcaRunId", "91");
    const timedOut = testRun({
      id: 91,
      status: "timed_out",
      currentRound: 2,
      stopReason: "wall_time_budget_exhausted",
      finishedAt: "2026-07-28T05:02:00Z",
    });
    mockedDetail.mockResolvedValue({
      run: timedOut,
      rounds: [],
      actions: [],
      evidence: [],
      rootCauseCandidates: [],
    });
    mockedReport.mockResolvedValue(testReport(91, "timed_out"));
    mockedRecover.mockResolvedValue({
      run: { ...timedOut, status: "running", finishedAt: undefined },
      skippedActionIds: [1, 2],
      retryableActionIds: [3],
    });
    mockedOrchestrate.mockImplementation(() => new Promise(() => undefined));

    renderWorkbench(<MultiRoundRCAWorkbench />);

    expect(await screen.findByText("RCA #91")).toBeInTheDocument();
    expect(screen.getAllByText("超时").length).toBeGreaterThan(0);
    await user.click(screen.getByRole("button", { name: "恢复并重试" }));
    await waitFor(() => expect(mockedRecover.mock.calls[0]?.[0]).toBe(91));
    await waitFor(() => expect(mockedOrchestrate.mock.calls[0]?.[0]).toBe(91));
    expect(
      await screen.findByText("已保留 2 个成功动作，准备重试 1 个动作。"),
    ).toBeInTheDocument();
  });

  it("renders permission failures distinctly", async () => {
    const user = userEvent.setup();
    mockedCreate.mockRejectedValue(new Error("403 forbidden: 权限不足"));
    renderWorkbench(<MultiRoundRCAWorkbench />);
    await user.click(screen.getByRole("button", { name: "开始三轮分析" }));
    expect(await screen.findByText("权限不足")).toBeInTheDocument();
    expect(screen.getByText("403 forbidden: 权限不足")).toBeInTheDocument();
  });
});

function renderWorkbench(element: ReactElement) {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={client}>{element}</QueryClientProvider>,
  );
}

function testRun(overrides: Partial<RCARun> = {}): RCARun {
  return { ...baseRun(), ...overrides };
}

function baseRun(): RCARun {
  return {
    id: 1,
    userId: 1,
    status: "pending",
    query: "订单服务变慢，请查询可能原因",
    scope: { serviceName: "order-service", environment: "prod" },
    currentRound: 0,
    maxRounds: 3,
    createdAt: "2026-07-28T05:00:00Z",
    updatedAt: "2026-07-28T05:00:00Z",
  };
}

function testAction(id: number, skillName: string, status: string) {
  return {
    id,
    runId: 88,
    roundId: 11,
    actionKey: `action-${id}`,
    skillName,
    status,
    input: {},
    evidenceIds: [],
    sensitiveRead: true,
    attempt: 1,
  };
}

function testReport(runId: number, status: string) {
  return {
    version: "rca-report-v1",
    runId,
    status,
    query: "订单服务变慢",
    scope: {},
    impactScope: { entities: [] },
    evidence: { facts: [], rules: [], knowledge: [], hypotheses: [] },
    rootCauseCandidates: [],
    rejectedHypotheses: [],
    investigation: [],
    missingEvidence: ["Prometheus data source binding is missing"],
    incomplete: true,
    conclusion: "暂无法定位：当前没有具备充分 Evidence 支撑的根因候选。",
    suggestions: [],
    riskNotices: [],
    stopReason: "wall_time_budget_exhausted",
  };
}
