import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";

import {
  createLinuxHostIncident,
  listLinuxEvents,
  listLinuxEvidence,
} from "@/api/linux-analysis";
import { listLinuxHosts } from "@/api/linux";
import { listWorkflowRuns, listWorkflows, runWorkflow } from "@/api/workflows";
import {
  LinuxAnalysisPage,
  sanitizeForDisplay,
} from "@/pages/linux-analysis-page";

vi.mock("@/api/linux", () => ({
  listLinuxHosts: vi.fn(),
}));

vi.mock("@/api/linux-analysis", () => ({
  createLinuxHostIncident: vi.fn(),
  getLinuxTopology: vi.fn().mockResolvedValue({ nodes: [], edges: [] }),
  hostIDFromSourceRef: (sourceRef: { hostId?: number }) => sourceRef?.hostId,
  listLinuxEvents: vi.fn(),
  listLinuxEvidence: vi.fn(),
  workflowIncludesHost: (
    run: { input?: { hostId?: number; hostIds?: number[] } },
    hostId: number,
  ) => run.input?.hostId === hostId || run.input?.hostIds?.includes(hostId),
}));

vi.mock("@/api/workflows", () => ({
  listWorkflowRuns: vi.fn(),
  listWorkflows: vi.fn(),
  runWorkflow: vi.fn(),
  toAPIErrorMessage: vi.fn((error: unknown) =>
    error instanceof Error ? error.message : "请求失败",
  ),
}));

const hosts = [
  {
    id: 7,
    name: "prod-app-01",
    host: "10.0.0.7",
    port: 22,
    environment: "prod",
    systemName: "payment",
    componentName: "payment-api",
    authType: "private_key" as const,
    credentialConfigured: true,
    hostKeyPolicy: "strict" as const,
    hostKeyStatus: "trusted" as const,
    enabled: true,
    connectionStatus: "success",
  },
  {
    id: 8,
    name: "prod-app-02",
    host: "10.0.0.8",
    port: 22,
    authType: "password" as const,
    credentialConfigured: true,
    hostKeyPolicy: "strict" as const,
    hostKeyStatus: "trusted" as const,
    enabled: true,
    connectionStatus: "unknown",
  },
];

const partialRun = {
  id: 21,
  workflowId: 1,
  status: "partial_success",
  input: { hostId: 7 },
  createdAt: "2026-07-18T01:30:00Z",
  nodeRuns: [
    {
      id: 1,
      nodeId: "collect_memory",
      nodeType: "skill",
      status: "partial_success",
      attempt: 1,
      output: {
        facts: [
          {
            type: "FACT",
            summary: "MemAvailable is 6.2%",
            evidenceRef: "linux.host.7.memory",
            evidence: {
              memAvailablePercent: 6.2,
              stdout: "TOP-SECRET raw command output",
            },
          },
          {
            type: "RULE",
            summary: "Memory pressure threshold matched",
            evidenceRef: "linux.host.7.memory",
          },
          {
            type: "HYPOTHESIS",
            summary: "A recent deployment may have increased heap usage",
          },
        ],
      },
    },
  ],
};

function renderPage(path = "/linux-analysis/7") {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route
            path="/linux-analysis/:hostId"
            element={<LinuxAnalysisPage />}
          />
          <Route path="/linux-analysis" element={<LinuxAnalysisPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("LinuxAnalysisPage", () => {
  beforeEach(() => {
    vi.mocked(listLinuxHosts).mockResolvedValue(hosts);
    vi.mocked(listWorkflows).mockResolvedValue([
      workflow(1, "linux_basic_host_diagnosis_workflow"),
      workflow(2, "linux_batch_health_workflow"),
    ]);
    vi.mocked(listWorkflowRuns).mockResolvedValue([partialRun]);
    vi.mocked(listLinuxEvidence).mockResolvedValue([
      {
        id: 1,
        evidenceKey: "linux.host.7.memory",
        sourceType: "linux_server",
        sourceRef: { hostId: 7, collector: "memory" },
        summary: "Structured memory evidence",
        content: {
          memAvailablePercent: 6.2,
          rawCommandOutput: "TOP-SECRET raw command output",
        },
        createdAt: "2026-07-18T01:30:00Z",
      },
    ]);
    vi.mocked(listLinuxEvents).mockResolvedValue([
      {
        id: 31,
        eventTime: "2026-07-18T01:30:00Z",
        sourceType: "linux_server",
        sourceId: "7",
        eventType: "linux_memory_pressure",
        status: "firing",
        resourceName: "prod-app-01",
        host: "10.0.0.7",
        summary: "Memory pressure",
        occurrenceCount: 1,
        firstSeenAt: "2026-07-18T01:30:00Z",
        lastSeenAt: "2026-07-18T01:30:00Z",
      },
    ]);
  });

  it("separates fact rule and hypothesis while keeping partial health unknown", async () => {
    const user = userEvent.setup();
    renderPage();

    await user.click(await screen.findByRole("button", { name: "Health" }));
    expect(screen.getByText("当前健康状态为 UNKNOWN")).toBeInTheDocument();
    expect(screen.getByText("MemAvailable is 6.2%")).toBeInTheDocument();
    expect(
      screen.getByText("Memory pressure threshold matched"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("A recent deployment may have increased heap usage"),
    ).toBeInTheDocument();
    expect(screen.getAllByText("FACT").length).toBeGreaterThan(0);
    expect(screen.getAllByText("RULE").length).toBeGreaterThan(0);
    expect(screen.getAllByText("HYPOTHESIS").length).toBeGreaterThan(0);
  });

  it("never renders raw command output or credential fields", async () => {
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole("button", { name: "Evidence" }));

    expect(
      screen.getAllByText("Structured memory evidence").length,
    ).toBeGreaterThan(0);
    expect(screen.queryByText(/TOP-SECRET/)).not.toBeInTheDocument();
    expect(
      sanitizeForDisplay({
        password: "secret",
        argv: ["sh", "-c"],
        nested: { stdout: "raw", value: 42 },
      }),
    ).toEqual({ nested: { value: 42 } });
  });

  it("includes a missing host in the batch failed count", async () => {
    const user = userEvent.setup();
    vi.mocked(runWorkflow).mockResolvedValue({
      id: 22,
      workflowId: 2,
      status: "partial_success",
      input: { hostIds: [7, 8] },
      createdAt: "2026-07-18T02:00:00Z",
      nodeRuns: [
        {
          id: 2,
          nodeId: "batch_diagnose",
          nodeType: "skill",
          status: "partial_success",
          attempt: 1,
          output: {
            facts: [
              {
                type: "FACT",
                summary: "Host overview collected",
                evidence: { hostId: 7, status: "success" },
              },
            ],
          },
        },
      ],
    });
    renderPage();
    await user.selectOptions(await screen.findByLabelText("诊断类型"), "batch");
    const secondHost = screen.getByText("prod-app-02").closest("label");
    await user.click(within(secondHost as HTMLElement).getByRole("checkbox"));
    await user.click(screen.getByRole("button", { name: "运行批量健康检查" }));

    expect(
      await screen.findByText(/Workflow Run #22 已完成/),
    ).toBeInTheDocument();
    const failedMetric = screen.getByText("失败").closest("div");
    expect(
      within(failedMetric as HTMLElement).getByText("1"),
    ).toBeInTheDocument();
    expect(screen.getByText("未返回主机结果，计入失败")).toBeInTheDocument();
  });

  it("creates an incident with current host evidence and events", async () => {
    const user = userEvent.setup();
    vi.mocked(createLinuxHostIncident).mockResolvedValue({
      incident: {
        id: 99,
        title: "incident",
        severity: "warning",
        status: "open",
      },
    });
    renderPage();
    await user.click(await screen.findByRole("button", { name: "Evidence" }));
    await screen.findAllByText("Structured memory evidence");
    await user.click(screen.getByRole("button", { name: "发起 Incident" }));
    await user.click(screen.getByRole("button", { name: "创建 Incident" }));

    expect(createLinuxHostIncident).toHaveBeenCalledWith(
      expect.objectContaining({
        hostId: 7,
        eventIds: [31],
        evidenceKeys: ["linux.host.7.memory"],
      }),
    );
    expect(await screen.findByText(/Incident #99 已创建/)).toBeInTheDocument();
  });
});

function workflow(id: number, name: string) {
  return {
    id,
    name,
    version: "v1",
    enabled: true,
    createdAt: "2026-07-18T00:00:00Z",
    updatedAt: "2026-07-18T00:00:00Z",
    definition: { name, version: "v1", nodes: [], edges: [] },
  };
}
