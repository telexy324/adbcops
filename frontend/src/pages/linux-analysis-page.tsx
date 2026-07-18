import { useMemo, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  AlertTriangle,
  ArrowLeft,
  Bot,
  BrainCircuit,
  CheckCircle2,
  CircleDashed,
  Clock3,
  Database,
  FileSearch,
  GitBranch,
  Layers3,
  Loader2,
  Network,
  Play,
  Server,
  ShieldAlert,
  Siren,
} from "lucide-react";
import { Link, useParams } from "react-router-dom";

import {
  createLinuxHostIncident,
  getLinuxTopology,
  hostIDFromSourceRef,
  listLinuxEvents,
  listLinuxEvidence,
  workflowIncludesHost,
  type LinuxEvent,
  type LinuxEvidence,
} from "@/api/linux-analysis";
import { listLinuxHosts, type LinuxHost } from "@/api/linux";
import {
  listWorkflowRuns,
  listWorkflows,
  runWorkflow,
  toAPIErrorMessage,
  type WorkflowNodeRun,
  type WorkflowRun,
} from "@/api/workflows";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

type DiagnosisType = "basic" | "cpu" | "memory" | "disk" | "network" | "batch";
type DetailTab =
  | "overview"
  | "health"
  | "cpu"
  | "memory"
  | "filesystem"
  | "disk_io"
  | "network"
  | "processes"
  | "services"
  | "logs"
  | "kernel"
  | "evidence"
  | "topology"
  | "history";

type FindingType = "FACT" | "RULE" | "HYPOTHESIS";
type Finding = {
  type: FindingType;
  summary: string;
  evidenceRef?: string;
  evidence?: unknown;
};

const diagnosisOptions: Array<{
  id: DiagnosisType;
  label: string;
  workflow: string;
  collectors: string[];
}> = [
  {
    id: "basic",
    label: "基础诊断",
    workflow: "linux_basic_host_diagnosis_workflow",
    collectors: [
      "overview",
      "cpu",
      "memory",
      "filesystem",
      "network",
      "process",
      "systemd",
      "time",
      "kernel",
    ],
  },
  {
    id: "cpu",
    label: "CPU",
    workflow: "linux_cpu_diagnosis_workflow",
    collectors: ["cpu", "memory", "disk_io", "kernel"],
  },
  {
    id: "memory",
    label: "Memory",
    workflow: "linux_memory_diagnosis_workflow",
    collectors: ["memory", "kernel", "system_logs"],
  },
  {
    id: "disk",
    label: "Disk",
    workflow: "linux_disk_diagnosis_workflow",
    collectors: ["filesystem", "disk_io", "kernel"],
  },
  {
    id: "network",
    label: "Network",
    workflow: "linux_network_diagnosis_workflow",
    collectors: ["network", "kernel", "topology"],
  },
  {
    id: "batch",
    label: "Batch Health",
    workflow: "linux_batch_health_workflow",
    collectors: ["system_overview"],
  },
];

const detailTabs: Array<{ id: DetailTab; label: string; tokens?: string[] }> = [
  { id: "overview", label: "Overview" },
  { id: "health", label: "Health" },
  { id: "cpu", label: "CPU", tokens: ["cpu"] },
  { id: "memory", label: "Memory", tokens: ["memory", "swap"] },
  {
    id: "filesystem",
    label: "Filesystem",
    tokens: ["filesystem", "capacity", "inode"],
  },
  { id: "disk_io", label: "Disk IO", tokens: ["disk_io", "disk-io"] },
  { id: "network", label: "Network", tokens: ["network"] },
  { id: "processes", label: "Processes", tokens: ["process"] },
  { id: "services", label: "Services", tokens: ["systemd", "service"] },
  { id: "logs", label: "System Logs", tokens: ["system_log", "logs"] },
  { id: "kernel", label: "Kernel Events", tokens: ["kernel"] },
  { id: "evidence", label: "Evidence" },
  { id: "topology", label: "Topology" },
  { id: "history", label: "History" },
];

export function LinuxAnalysisPage() {
  const params = useParams();
  const initialHostID = Number(params.hostId || 0);
  const client = useQueryClient();
  const [selectedHostIDs, setSelectedHostIDs] = useState<number[]>(
    initialHostID > 0 ? [initialHostID] : [],
  );
  const [diagnosis, setDiagnosis] = useState<DiagnosisType>("basic");
  const [tab, setTab] = useState<DetailTab>("overview");
  const [question, setQuestion] = useState("请分析当前健康状态并说明证据缺口");
  const [currentRun, setCurrentRun] = useState<WorkflowRun | null>(null);
  const [incidentOpen, setIncidentOpen] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const hostsQuery = useQuery({
    queryKey: ["linux", "hosts"],
    queryFn: listLinuxHosts,
  });
  const workflowsQuery = useQuery({
    queryKey: ["workflows"],
    queryFn: listWorkflows,
  });
  const runsQuery = useQuery({
    queryKey: ["workflow-runs"],
    queryFn: listWorkflowRuns,
  });
  const primaryHost = (hostsQuery.data ?? []).find(
    (host) => host.id === selectedHostIDs[0],
  );
  const evidenceQuery = useQuery({
    queryKey: ["linux", "analysis", "evidence", primaryHost?.id],
    queryFn: () => listLinuxEvidence(),
    enabled: !!primaryHost,
  });
  const eventsQuery = useQuery({
    queryKey: ["linux", "analysis", "events", primaryHost?.id],
    queryFn: () =>
      listLinuxEvents({
        resourceName: primaryHost?.name,
        environment: primaryHost?.environment,
      }),
    enabled: !!primaryHost,
  });
  const topologyQuery = useQuery({
    queryKey: ["linux", "analysis", "topology", primaryHost?.environment],
    queryFn: () => getLinuxTopology(primaryHost?.environment),
    enabled: !!primaryHost,
  });

  const hostEvidence = useMemo(
    () =>
      (evidenceQuery.data ?? []).filter(
        (record) => hostIDFromSourceRef(record.sourceRef) === primaryHost?.id,
      ),
    [evidenceQuery.data, primaryHost?.id],
  );
  const hostEvents = useMemo(
    () =>
      (eventsQuery.data ?? []).filter(
        (event) =>
          event.host === primaryHost?.host ||
          event.resourceName === primaryHost?.name ||
          event.sourceId === String(primaryHost?.id),
      ),
    [eventsQuery.data, primaryHost],
  );
  const hostRuns = useMemo(
    () =>
      (runsQuery.data ?? []).filter(
        (run) => primaryHost && workflowIncludesHost(run, primaryHost.id),
      ),
    [runsQuery.data, primaryHost],
  );
  const displayRun = currentRun ?? hostRuns[0] ?? null;
  const findings = useMemo(() => extractFindings(displayRun), [displayRun]);
  const health = deriveHealth(displayRun, findings);
  const selectedDiagnosis = diagnosisOptions.find(
    (item) => item.id === diagnosis,
  )!;

  const runMutation = useMutation({
    mutationFn: async () => {
      if (selectedHostIDs.length === 0) throw new Error("请至少选择一台主机");
      if (diagnosis !== "batch" && selectedHostIDs.length !== 1)
        throw new Error("单主机诊断只能选择一台主机");
      const workflow = workflowsQuery.data?.find(
        (item) => item.name === selectedDiagnosis.workflow && item.enabled,
      );
      if (!workflow)
        throw new Error(`未找到已启用 Workflow：${selectedDiagnosis.workflow}`);
      const input =
        diagnosis === "batch"
          ? {
              hostIds: selectedHostIDs,
              query: question,
              diagnosisType: diagnosis,
            }
          : {
              hostId: selectedHostIDs[0],
              query: question,
              diagnosisType: diagnosis,
            };
      return runWorkflow(workflow.id, input);
    },
    onSuccess: (run) => {
      setCurrentRun(run);
      setNotice(`Workflow Run #${run.id} 已完成：${run.status}`);
      setError(null);
      client.invalidateQueries({ queryKey: ["workflow-runs"] });
      client.invalidateQueries({ queryKey: ["linux", "analysis"] });
    },
    onError: (reason) => setError(toAPIErrorMessage(reason)),
  });

  return (
    <div className="mx-auto max-w-[1700px] space-y-6">
      <section className="overflow-hidden rounded-2xl bg-[#071827] px-6 py-6 text-white shadow-xl sm:px-8">
        <div className="flex flex-wrap items-start justify-between gap-5">
          <div>
            <Link
              to="/linux-hosts"
              className="mb-4 inline-flex items-center gap-2 text-xs font-semibold text-cyan-300 hover:text-cyan-200"
            >
              <ArrowLeft className="size-4" /> 返回主机配置
            </Link>
            <p className="flex items-center gap-2 text-xs font-semibold uppercase tracking-[.2em] text-cyan-300">
              <Activity className="size-4" /> Linux Analysis
            </p>
            <h1 className="mt-2 text-2xl font-semibold sm:text-3xl">
              Linux 主机分析
            </h1>
            <p className="mt-2 max-w-3xl text-sm leading-6 text-slate-300">
              只读 Collector、规则、Evidence、知识引用和 LLM
              推测分层展示。证据不足时明确标记 unknown，不会推断为健康。
            </p>
          </div>
          <HealthBadge status={health} />
        </div>
      </section>

      {(notice || error) && (
        <div
          role="status"
          className={cn(
            "rounded-xl border px-4 py-3 text-sm",
            error
              ? "border-rose-200 bg-rose-50 text-rose-700"
              : "border-emerald-200 bg-emerald-50 text-emerald-700",
          )}
        >
          {error ?? notice}
        </div>
      )}

      <section className="grid gap-6 xl:grid-cols-[360px_1fr]">
        <Card className="h-fit">
          <CardHeader>
            <CardTitle>分析范围</CardTitle>
            <CardDescription>
              单主机诊断选择一台；Batch Health 可选择多台。
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-5">
            <div>
              <Label>主机</Label>
              <div className="mt-2 max-h-52 space-y-2 overflow-y-auto rounded-xl border p-2">
                {(hostsQuery.data ?? []).map((host) => (
                  <HostChoice
                    key={host.id}
                    host={host}
                    checked={selectedHostIDs.includes(host.id)}
                    onChange={(checked) =>
                      setSelectedHostIDs(
                        updateSelection(
                          selectedHostIDs,
                          host.id,
                          checked,
                          diagnosis === "batch",
                        ),
                      )
                    }
                  />
                ))}
                {!hostsQuery.isLoading &&
                  (hostsQuery.data?.length ?? 0) === 0 && (
                    <EmptyState text="暂无 Linux 主机" />
                  )}
              </div>
            </div>
            <Field label="诊断类型">
              <select
                aria-label="诊断类型"
                className="h-10 w-full rounded-md border bg-white px-3 text-sm"
                value={diagnosis}
                onChange={(event) => {
                  const value = event.target.value as DiagnosisType;
                  setDiagnosis(value);
                  if (value !== "batch" && selectedHostIDs.length > 1)
                    setSelectedHostIDs(selectedHostIDs.slice(0, 1));
                }}
              >
                {diagnosisOptions.map((option) => (
                  <option key={option.id} value={option.id}>
                    {option.label}
                  </option>
                ))}
              </select>
            </Field>
            <Field label="分析问题">
              <textarea
                aria-label="分析问题"
                className="min-h-24 w-full rounded-md border bg-white p-3 text-sm"
                value={question}
                onChange={(event) => setQuestion(event.target.value)}
              />
            </Field>
            <div>
              <p className="text-xs font-semibold uppercase tracking-wide text-slate-500">
                Collector Preview
              </p>
              <div className="mt-2 flex flex-wrap gap-1.5">
                {selectedDiagnosis.collectors.map((collector) => (
                  <Pill key={collector}>{collector}</Pill>
                ))}
              </div>
            </div>
            <Button
              className="w-full"
              disabled={runMutation.isPending || selectedHostIDs.length === 0}
              onClick={() => runMutation.mutate()}
            >
              {runMutation.isPending ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Play className="size-4" />
              )}
              {diagnosis === "batch" ? "运行批量健康检查" : "运行主机诊断"}
            </Button>
            <p className="text-xs leading-5 text-slate-500">
              时间范围由 Collector 使用受控参数确定，不提供任意命令或 argv
              输入。
            </p>
          </CardContent>
        </Card>

        <div className="min-w-0 space-y-6">
          {primaryHost ? (
            <HostSummary
              host={primaryHost}
              health={health}
              onIncident={() => setIncidentOpen(true)}
            />
          ) : (
            <Card>
              <CardContent className="py-16">
                <EmptyState text="选择一台主机查看详情，或选择多台运行 Batch Health。" />
              </CardContent>
            </Card>
          )}
          {displayRun && (
            <WorkflowProgress
              run={displayRun}
              pending={runMutation.isPending}
            />
          )}
          {diagnosis === "batch" && selectedHostIDs.length > 0 && (
            <BatchReport
              hostIDs={selectedHostIDs}
              hosts={hostsQuery.data ?? []}
              run={displayRun}
              pending={runMutation.isPending}
            />
          )}
          {primaryHost && diagnosis !== "batch" && (
            <>
              <div className="flex gap-1 overflow-x-auto rounded-xl border bg-white p-1.5">
                {detailTabs.map((item) => (
                  <button
                    key={item.id}
                    className={cn(
                      "rounded-lg px-3 py-2 text-sm font-medium whitespace-nowrap",
                      tab === item.id
                        ? "bg-slate-900 text-white"
                        : "text-slate-600 hover:bg-slate-100",
                    )}
                    onClick={() => setTab(item.id)}
                  >
                    {item.label}
                  </button>
                ))}
              </div>
              <DetailContent
                tab={tab}
                host={primaryHost}
                run={displayRun}
                findings={findings}
                health={health}
                evidence={hostEvidence}
                events={hostEvents}
                topology={topologyQuery.data}
              />
            </>
          )}
        </div>
      </section>
      {incidentOpen && primaryHost && (
        <IncidentDialog
          host={primaryHost}
          evidence={hostEvidence}
          events={hostEvents}
          onClose={() => setIncidentOpen(false)}
          onCreated={(id) => {
            setIncidentOpen(false);
            setNotice(`Incident #${id} 已创建并关联当前主机证据`);
          }}
          onError={(message) => setError(message)}
        />
      )}
    </div>
  );
}

function HostChoice({
  host,
  checked,
  onChange,
}: {
  host: LinuxHost;
  checked: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <label
      className={cn(
        "flex cursor-pointer items-center gap-3 rounded-lg border px-3 py-2.5 text-sm",
        checked && "border-cyan-300 bg-cyan-50",
      )}
    >
      <input
        type="checkbox"
        checked={checked}
        onChange={(event) => onChange(event.target.checked)}
      />
      <span className="min-w-0">
        <span className="block truncate font-semibold">{host.name}</span>
        <span className="block font-mono text-[11px] text-slate-500">
          {host.host}:{host.port}
        </span>
      </span>
      <span className="ml-auto">
        <SmallStatus value={host.connectionStatus} />
      </span>
    </label>
  );
}

function HostSummary({
  host,
  health,
  onIncident,
}: {
  host: LinuxHost;
  health: string;
  onIncident: () => void;
}) {
  return (
    <Card>
      <CardContent className="grid gap-5 p-5 lg:grid-cols-[1fr_auto] lg:items-center">
        <div className="flex min-w-0 items-start gap-4">
          <div className="rounded-xl bg-cyan-50 p-3 text-cyan-700">
            <Server className="size-6" />
          </div>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="text-xl font-semibold">{host.name}</h2>
              <HealthBadge status={health} compact />
            </div>
            <p className="mt-1 font-mono text-xs text-slate-500">
              {host.host}:{host.port}
            </p>
            <p className="mt-2 text-sm text-slate-500">
              {host.environment || "未设置环境"} ·{" "}
              {host.systemName || "未设置系统"} ·{" "}
              {host.componentName || "未设置组件"}
            </p>
          </div>
        </div>
        <Button variant="outline" onClick={onIncident}>
          <Siren className="size-4" /> 发起 Incident
        </Button>
      </CardContent>
    </Card>
  );
}

function WorkflowProgress({
  run,
  pending,
}: {
  run: WorkflowRun;
  pending: boolean;
}) {
  const nodes = run.nodeRuns ?? [];
  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between">
        <div>
          <CardTitle className="flex items-center gap-2">
            <GitBranch className="size-5 text-cyan-600" /> Workflow Progress
          </CardTitle>
          <CardDescription>
            Run #{run.id} · {run.status}
          </CardDescription>
        </div>
        {pending && <Loader2 className="size-5 animate-spin text-cyan-600" />}
      </CardHeader>
      <CardContent>
        <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
          {nodes.map((node) => (
            <div
              key={node.id}
              className="flex items-center gap-3 rounded-xl border px-3 py-3"
            >
              <NodeStatus status={node.status} />
              <div className="min-w-0">
                <p className="truncate text-sm font-semibold">
                  {humanize(node.nodeId)}
                </p>
                <p className="text-xs text-slate-500">
                  {node.nodeType} · {node.status}
                </p>
              </div>
            </div>
          ))}
        </div>
        {nodes.length === 0 && <EmptyState text="运行记录尚无节点明细" />}
      </CardContent>
    </Card>
  );
}

function DetailContent({
  tab,
  host,
  run,
  findings,
  health,
  evidence,
  events,
  topology,
}: {
  tab: DetailTab;
  host: LinuxHost;
  run: WorkflowRun | null;
  findings: Finding[];
  health: string;
  evidence: LinuxEvidence[];
  events: LinuxEvent[];
  topology?: {
    nodes: Array<{
      nodeKey: string;
      kind: string;
      name: string;
      properties?: Record<string, unknown>;
    }>;
    edges: Array<{
      edgeKey: string;
      fromNodeKey: string;
      toNodeKey: string;
      edgeType: string;
      confidence?: number;
      sourceType: string;
    }>;
  };
}) {
  if (tab === "overview") return <OverviewPanel host={host} run={run} />;
  if (tab === "health")
    return <FindingsPanel findings={findings} health={health} />;
  if (tab === "evidence") return <EvidencePanel records={evidence} />;
  if (tab === "history") return <HistoryPanel run={run} events={events} />;
  if (tab === "topology")
    return <TopologyPanel host={host} topology={topology} />;
  const definition = detailTabs.find((item) => item.id === tab);
  const matching = (run?.nodeRuns ?? []).filter((node) =>
    definition?.tokens?.some((token) =>
      node.nodeId.toLowerCase().includes(token),
    ),
  );
  return <ResourcePanel title={definition?.label ?? tab} nodes={matching} />;
}

function OverviewPanel({
  host,
  run,
}: {
  host: LinuxHost;
  run: WorkflowRun | null;
}) {
  const rows = [
    ["连接状态", host.connectionStatus],
    ["Host Key", host.hostKeyStatus],
    ["环境", host.environment || "未设置"],
    ["系统", host.systemName || "未设置"],
    ["组件", host.componentName || "未设置"],
    ["最近测试", formatTime(host.lastTestAt)],
    ["最近诊断", formatTime(run?.finishedAt || run?.createdAt)],
  ];
  return (
    <Card>
      <CardHeader>
        <CardTitle>Host Overview</CardTitle>
        <CardDescription>
          仅显示主机元数据和状态，不展示用户名、凭据或完整密钥。
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        {rows.map(([label, value]) => (
          <div key={label} className="rounded-xl border bg-slate-50 p-4">
            <p className="text-xs font-semibold text-slate-500">{label}</p>
            <p className="mt-2 text-sm font-semibold">{value}</p>
          </div>
        ))}
      </CardContent>
    </Card>
  );
}

function FindingsPanel({
  findings,
  health,
}: {
  findings: Finding[];
  health: string;
}) {
  const groups: FindingType[] = ["FACT", "RULE", "HYPOTHESIS"];
  return (
    <div className="space-y-4">
      {health === "unknown" && <UnknownBanner />}
      <div className="grid gap-4 xl:grid-cols-3">
        {groups.map((type) => (
          <Card key={type}>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                {type === "FACT" ? (
                  <Database className="size-5 text-cyan-600" />
                ) : type === "RULE" ? (
                  <FileSearch className="size-5 text-amber-600" />
                ) : (
                  <BrainCircuit className="size-5 text-violet-600" />
                )}
                {type}
              </CardTitle>
              <CardDescription>
                {type === "FACT"
                  ? "Collector 直接观测"
                  : type === "RULE"
                    ? "确定性规则结论"
                    : "模型推测，需人工验证"}
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              {findings
                .filter((finding) => finding.type === type)
                .map((finding, index) => (
                  <FindingCard
                    key={`${finding.summary}-${index}`}
                    finding={finding}
                  />
                ))}
              {findings.every((finding) => finding.type !== type) && (
                <EmptyState text={`暂无 ${type} 结果`} />
              )}
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  );
}

function FindingCard({ finding }: { finding: Finding }) {
  return (
    <div className="rounded-xl border p-3">
      <p className="text-sm leading-6">{finding.summary}</p>
      {finding.evidenceRef && (
        <p className="mt-2 break-all font-mono text-[11px] text-cyan-700">
          Evidence: {finding.evidenceRef}
        </p>
      )}
    </div>
  );
}

function ResourcePanel({
  title,
  nodes,
}: {
  title: string;
  nodes: WorkflowNodeRun[];
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>
          显示结构化、脱敏后的 Collector 与规则输出。
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {nodes.map((node) => (
          <div key={node.id} className="rounded-xl border p-4">
            <div className="mb-3 flex items-center justify-between">
              <p className="font-semibold">{humanize(node.nodeId)}</p>
              <SmallStatus value={node.status} />
            </div>
            <SafeObject value={node.output} />
          </div>
        ))}
        {nodes.length === 0 && (
          <EmptyState text="尚未采集该资源；状态为 unknown，而不是 healthy。" />
        )}
      </CardContent>
    </Card>
  );
}

function EvidencePanel({ records }: { records: LinuxEvidence[] }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Evidence</CardTitle>
        <CardDescription>
          结构化证据；原始命令输出和敏感命令行不会显示。
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {records.map((record) => (
          <div key={record.evidenceKey} className="rounded-xl border p-4">
            <div className="flex flex-wrap items-start justify-between gap-2">
              <div>
                <p className="font-semibold">
                  {record.title || record.summary}
                </p>
                <p className="mt-1 text-sm text-slate-600">{record.summary}</p>
              </div>
              {record.confidence != null && (
                <Pill>{Math.round(record.confidence * 100)}% confidence</Pill>
              )}
            </div>
            <p className="mt-2 break-all font-mono text-[11px] text-cyan-700">
              {record.evidenceKey}
            </p>
            <div className="mt-3">
              <SafeObject value={record.content} />
            </div>
          </div>
        ))}
        {records.length === 0 && (
          <EmptyState text="暂无持久化 Evidence；诊断状态保持 unknown。" />
        )}
      </CardContent>
    </Card>
  );
}

function HistoryPanel({
  run,
  events,
}: {
  run: WorkflowRun | null;
  events: LinuxEvent[];
}) {
  return (
    <div className="grid gap-4 xl:grid-cols-2">
      <Card>
        <CardHeader>
          <CardTitle>Workflow History</CardTitle>
        </CardHeader>
        <CardContent>
          {run ? (
            <div className="rounded-xl border p-4">
              <p className="font-semibold">Run #{run.id}</p>
              <p className="mt-1 text-sm text-slate-500">
                {run.status} · {formatTime(run.createdAt)}
              </p>
            </div>
          ) : (
            <EmptyState text="暂无诊断历史" />
          )}
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>Linux Events</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2">
          {events.map((event) => (
            <div key={event.id} className="rounded-xl border p-3">
              <div className="flex items-center justify-between gap-2">
                <p className="text-sm font-semibold">{event.summary}</p>
                <SmallStatus value={event.status} />
              </div>
              <p className="mt-1 text-xs text-slate-500">
                {event.eventType} · {formatTime(event.eventTime)} · ×
                {event.occurrenceCount}
              </p>
            </div>
          ))}
          {events.length === 0 && <EmptyState text="暂无 Linux Event" />}
        </CardContent>
      </Card>
    </div>
  );
}

function TopologyPanel({
  host,
  topology,
}: {
  host: LinuxHost;
  topology?: {
    nodes: Array<{
      nodeKey: string;
      kind: string;
      name: string;
      properties?: Record<string, unknown>;
    }>;
    edges: Array<{
      edgeKey: string;
      fromNodeKey: string;
      toNodeKey: string;
      edgeType: string;
      confidence?: number;
      sourceType: string;
    }>;
  };
}) {
  const hostNode = topology?.nodes.find(
    (node) =>
      node.kind === "host" &&
      (node.name === host.name ||
        node.properties?.hostId === host.id ||
        node.properties?.managementIp === host.host),
  );
  const edges = hostNode
    ? (topology?.edges.filter(
        (edge) =>
          edge.fromNodeKey === hostNode.nodeKey ||
          edge.toNodeKey === hostNode.nodeKey,
      ) ?? [])
    : [];
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Network className="size-5 text-cyan-600" /> Host Topology
        </CardTitle>
        <CardDescription>关系显示来源与置信度。</CardDescription>
      </CardHeader>
      <CardContent>
        {hostNode ? (
          <div className="space-y-3">
            <div className="rounded-xl border bg-slate-50 p-4">
              <p className="font-semibold">{hostNode.name}</p>
              <p className="mt-1 break-all font-mono text-xs text-slate-500">
                {hostNode.nodeKey}
              </p>
            </div>
            {edges.map((edge) => (
              <div
                key={edge.edgeKey}
                className="flex flex-wrap items-center gap-2 rounded-xl border p-3 text-sm"
              >
                <Pill>{edge.edgeType}</Pill>
                <span className="break-all text-slate-600">
                  {edge.fromNodeKey} → {edge.toNodeKey}
                </span>
                <span className="ml-auto text-xs text-slate-500">
                  {edge.sourceType} ·{" "}
                  {edge.confidence == null
                    ? "confidence unknown"
                    : `${Math.round(edge.confidence * 100)}%`}
                </span>
              </div>
            ))}
          </div>
        ) : (
          <EmptyState text="尚未同步该 Host 的 Topology" />
        )}
      </CardContent>
    </Card>
  );
}

function BatchReport({
  hostIDs,
  hosts,
  run,
  pending,
}: {
  hostIDs: number[];
  hosts: LinuxHost[];
  run: WorkflowRun | null;
  pending: boolean;
}) {
  const outcomes = batchOutcomes(hostIDs, run);
  const counts = { healthy: 0, warning: 0, critical: 0, unknown: 0, failed: 0 };
  outcomes.forEach((status) => counts[status]++);
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Layers3 className="size-5 text-cyan-600" /> Batch Report
        </CardTitle>
        <CardDescription>
          总数包含成功、unknown 和采集失败主机，不遗漏失败样本。
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 xl:grid-cols-6">
          <ReportMetric label="总数" value={hostIDs.length} />
          <ReportMetric label="健康" value={counts.healthy} tone="green" />
          <ReportMetric label="警告" value={counts.warning} tone="amber" />
          <ReportMetric label="严重" value={counts.critical} tone="red" />
          <ReportMetric label="Unknown" value={counts.unknown} tone="slate" />
          <ReportMetric label="失败" value={counts.failed} tone="red" />
        </div>
        {pending && (
          <div className="mt-5 flex items-center gap-2 text-sm text-cyan-700">
            <Loader2 className="size-4 animate-spin" /> 批量诊断执行中
          </div>
        )}
        <div className="mt-5 overflow-x-auto">
          <table className="w-full min-w-[640px] text-left text-sm">
            <thead className="border-y bg-slate-50 text-xs text-slate-500">
              <tr>
                <th className="p-3">主机</th>
                <th>地址</th>
                <th>结果</th>
                <th>说明</th>
              </tr>
            </thead>
            <tbody>
              {hostIDs.map((hostID) => {
                const host = hosts.find((item) => item.id === hostID);
                const status = outcomes.get(hostID) ?? "failed";
                return (
                  <tr key={hostID} className="border-b">
                    <td className="p-3 font-semibold">
                      {host?.name || `Host #${hostID}`}
                    </td>
                    <td className="font-mono text-xs text-slate-500">
                      {host ? `${host.host}:${host.port}` : "—"}
                    </td>
                    <td>
                      <SmallStatus value={status} />
                    </td>
                    <td className="text-xs text-slate-500">
                      {status === "failed"
                        ? "未返回主机结果，计入失败"
                        : status === "unknown"
                          ? "证据不足，不计为健康"
                          : "已返回结构化结果"}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </CardContent>
    </Card>
  );
}

function IncidentDialog({
  host,
  evidence,
  events,
  onClose,
  onCreated,
  onError,
}: {
  host: LinuxHost;
  evidence: LinuxEvidence[];
  events: LinuxEvent[];
  onClose: () => void;
  onCreated: (id: number) => void;
  onError: (message: string) => void;
}) {
  const [title, setTitle] = useState(`${host.name} Linux 主机异常`);
  const [severity, setSeverity] = useState<"critical" | "warning" | "info">(
    "warning",
  );
  const [summary, setSummary] = useState(
    `从 Linux Host ${host.name} 发起，等待进一步确认影响和根因。`,
  );
  const mutation = useMutation({
    mutationFn: () =>
      createLinuxHostIncident({
        title,
        severity,
        summary,
        hostId: host.id,
        environment: host.environment,
        systemName: host.systemName,
        componentName: host.componentName,
        eventIds: events.map((event) => event.id),
        evidenceKeys: evidence.map((record) => record.evidenceKey),
      }),
    onSuccess: (detail) => onCreated(detail.incident.id),
    onError: (reason) => onError(toAPIErrorMessage(reason)),
  });
  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-slate-950/60 p-4">
      <Card className="w-full max-w-xl">
        <CardHeader>
          <CardTitle>从 Host 发起 Incident</CardTitle>
          <CardDescription>
            自动关联当前主机的 Event 和 Evidence，不包含凭据或原始命令。
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <Field label="标题">
            <Input
              value={title}
              onChange={(event) => setTitle(event.target.value)}
            />
          </Field>
          <Field label="严重级别">
            <select
              aria-label="严重级别"
              className="h-10 w-full rounded-md border px-3 text-sm"
              value={severity}
              onChange={(event) =>
                setSeverity(event.target.value as typeof severity)
              }
            >
              <option value="critical">Critical</option>
              <option value="warning">Warning</option>
              <option value="info">Info</option>
            </select>
          </Field>
          <Field label="摘要">
            <textarea
              aria-label="Incident 摘要"
              className="min-h-28 w-full rounded-md border p-3 text-sm"
              value={summary}
              onChange={(event) => setSummary(event.target.value)}
            />
          </Field>
          <p className="text-xs text-slate-500">
            将关联 {events.length} 个 Event、{evidence.length} 条 Evidence。
          </p>
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={onClose}>
              取消
            </Button>
            <Button
              disabled={!title.trim() || mutation.isPending}
              onClick={() => mutation.mutate()}
            >
              {mutation.isPending && (
                <Loader2 className="size-4 animate-spin" />
              )}
              创建 Incident
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

function SafeObject({ value }: { value: unknown }) {
  const safe = sanitizeForDisplay(value);
  if (
    safe == null ||
    (typeof safe === "object" && Object.keys(safe as object).length === 0)
  )
    return <p className="text-sm text-slate-500">无可展示的结构化数据</p>;
  return (
    <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-slate-950 p-3 font-mono text-[11px] leading-5 text-slate-200">
      {JSON.stringify(safe, null, 2)}
    </pre>
  );
}

export function sanitizeForDisplay(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(sanitizeForDisplay);
  if (!value || typeof value !== "object") return value;
  const result: Record<string, unknown> = {};
  for (const [key, child] of Object.entries(value as Record<string, unknown>)) {
    const normalized = key.toLowerCase().replace(/[_\-\s]/g, "");
    if (
      [
        "password",
        "privatekey",
        "privatekeypassphrase",
        "credential",
        "credentialid",
        "command",
        "commandline",
        "argv",
        "stdout",
        "stderr",
        "raw",
        "rawoutput",
        "rawcommandoutput",
      ].includes(normalized)
    )
      continue;
    result[key] = sanitizeForDisplay(child);
  }
  return result;
}

export function extractFindings(run: WorkflowRun | null): Finding[] {
  if (!run) return [];
  const findings: Finding[] = [];
  const seen = new Set<string>();
  const visit = (value: unknown) => {
    if (Array.isArray(value)) {
      value.forEach(visit);
      return;
    }
    if (!value || typeof value !== "object") return;
    const object = value as Record<string, unknown>;
    const type = String(object.type || object.findingType || "").toUpperCase();
    const summary =
      typeof object.summary === "string"
        ? object.summary
        : typeof object.message === "string"
          ? object.message
          : "";
    if (["FACT", "RULE", "HYPOTHESIS"].includes(type) && summary) {
      const key = `${type}:${summary}:${String(object.evidenceRef || "")}`;
      if (!seen.has(key)) {
        findings.push({
          type: type as FindingType,
          summary,
          evidenceRef:
            typeof object.evidenceRef === "string"
              ? object.evidenceRef
              : undefined,
          evidence: sanitizeForDisplay(object.evidence),
        });
        seen.add(key);
      }
    }
    Object.values(object).forEach(visit);
  };
  visit(run.output);
  run.nodeRuns?.forEach((node) => visit(node.output));
  return findings;
}

function deriveHealth(run: WorkflowRun | null, findings: Finding[]) {
  if (!run || ["failed", "cancelled", "partial_success"].includes(run.status))
    return "unknown";
  const text = findings
    .map((finding) => finding.summary.toLowerCase())
    .join(" ");
  if (text.includes("critical")) return "critical";
  if (text.includes("warning")) return "warning";
  if (
    text.includes("healthy") &&
    findings.some((finding) => finding.type === "FACT")
  )
    return "healthy";
  return "unknown";
}

function batchOutcomes(hostIDs: number[], run: WorkflowRun | null) {
  const result = new Map<
    number,
    "healthy" | "warning" | "critical" | "unknown" | "failed"
  >();
  hostIDs.forEach((id) => result.set(id, "failed"));
  const visit = (value: unknown, parentSummary = "") => {
    if (Array.isArray(value)) {
      value.forEach((item) => visit(item, parentSummary));
      return;
    }
    if (!value || typeof value !== "object") return;
    const object = value as Record<string, unknown>;
    const summary =
      typeof object.summary === "string" ? object.summary : parentSummary;
    const hostId =
      typeof object.hostId === "number" ? object.hostId : undefined;
    const rawStatus =
      typeof object.status === "string" ? object.status.toLowerCase() : "";
    if (hostId && result.has(hostId)) {
      const status = summary.toLowerCase().includes("failed")
        ? "failed"
        : rawStatus === "success" || rawStatus === "healthy"
          ? "healthy"
          : rawStatus === "warning"
            ? "warning"
            : rawStatus === "critical"
              ? "critical"
              : "unknown";
      result.set(hostId, status);
    }
    Object.values(object).forEach((child) => visit(child, summary));
  };
  if (run) {
    visit(run.output);
    run.nodeRuns?.forEach((node) => visit(node.output));
  }
  return result;
}

function updateSelection(
  current: number[],
  id: number,
  checked: boolean,
  multi: boolean,
) {
  if (!checked) return current.filter((value) => value !== id);
  if (!multi) return [id];
  return [...new Set([...current, id])];
}

function HealthBadge({
  status,
  compact = false,
}: {
  status: string;
  compact?: boolean;
}) {
  const styles =
    status === "healthy"
      ? "border-emerald-300 bg-emerald-50 text-emerald-700"
      : status === "critical"
        ? "border-red-300 bg-red-50 text-red-700"
        : status === "warning"
          ? "border-amber-300 bg-amber-50 text-amber-700"
          : "border-slate-300 bg-slate-100 text-slate-600";
  return (
    <div
      className={cn(
        "inline-flex items-center gap-2 rounded-full border font-semibold",
        compact ? "px-2.5 py-1 text-xs" : "px-4 py-2 text-sm",
        styles,
      )}
    >
      {status === "healthy" ? (
        <CheckCircle2 className="size-4" />
      ) : status === "unknown" ? (
        <CircleDashed className="size-4" />
      ) : (
        <AlertTriangle className="size-4" />
      )}
      {status === "unknown" ? "UNKNOWN · 证据不足" : status.toUpperCase()}
    </div>
  );
}

function UnknownBanner() {
  return (
    <div className="flex items-start gap-3 rounded-xl border border-slate-300 bg-slate-100 p-4 text-sm text-slate-700">
      <CircleDashed className="mt-0.5 size-5 shrink-0" />
      <div>
        <p className="font-semibold">当前健康状态为 UNKNOWN</p>
        <p className="mt-1 text-slate-600">
          尚未获得足够 FACT/Evidence，或诊断仅部分完成。UNKNOWN 不会被计为
          healthy。
        </p>
      </div>
    </div>
  );
}
function NodeStatus({ status }: { status: string }) {
  return status === "success" ? (
    <CheckCircle2 className="size-5 text-emerald-600" />
  ) : status === "failed" || status === "cancelled" ? (
    <AlertTriangle className="size-5 text-red-600" />
  ) : status === "partial_success" ? (
    <ShieldAlert className="size-5 text-amber-600" />
  ) : (
    <Clock3 className="size-5 text-slate-400" />
  );
}
function SmallStatus({ value }: { value: string }) {
  const tone = ["success", "healthy", "completed"].includes(value)
    ? "text-emerald-700"
    : ["failed", "critical", "host_key_mismatch"].includes(value)
      ? "text-red-700"
      : value === "warning"
        ? "text-amber-700"
        : "text-slate-500";
  return (
    <span className={cn("text-xs font-semibold", tone)}>
      {value === "unknown" ? "UNKNOWN" : humanize(value)}
    </span>
  );
}
function Pill({ children }: { children: ReactNode }) {
  return (
    <span className="inline-flex rounded-full bg-cyan-50 px-2.5 py-1 text-[11px] font-semibold text-cyan-700">
      {children}
    </span>
  );
}
function ReportMetric({
  label,
  value,
  tone = "slate",
}: {
  label: string;
  value: number;
  tone?: "slate" | "green" | "amber" | "red";
}) {
  const styles = {
    slate: "bg-slate-50 text-slate-700",
    green: "bg-emerald-50 text-emerald-700",
    amber: "bg-amber-50 text-amber-700",
    red: "bg-red-50 text-red-700",
  };
  return (
    <div className={cn("rounded-xl p-3 text-center", styles[tone])}>
      <p className="text-2xl font-semibold">{value}</p>
      <p className="mt-1 text-xs font-semibold">{label}</p>
    </div>
  );
}
function EmptyState({ text }: { text: string }) {
  return (
    <div className="py-8 text-center text-sm text-slate-500">
      <Bot className="mx-auto mb-2 size-5 text-slate-400" />
      {text}
    </div>
  );
}
function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="space-y-2">
      <Label>{label}</Label>
      {children}
    </div>
  );
}
function humanize(value: string) {
  return value.replace(/[_-]+/g, " ");
}
function formatTime(value?: string) {
  return value ? new Date(value).toLocaleString() : "尚无记录";
}
