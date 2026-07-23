import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  CheckCircle2,
  Clipboard,
  FileDown,
  GitBranch,
  Loader2,
  Network,
  Radius,
  Siren,
} from "lucide-react";

import {
  confirmRootCause,
  getBlastRadius,
  getIncident,
  getSimilarIncidents,
  getTimeline,
  getTopologyGraph,
  listIncidents,
  toAPIErrorMessage,
  type BlastRadius,
  type IncidentDetail,
  type SimilarIncident,
  type TimelineItem,
  type TopologyGraph,
  type TopologyNode,
} from "@/api/operations";
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

export function OperationsPage() {
  const queryClient = useQueryClient();
  const [selectedIncidentId, setSelectedIncidentId] = useState<number | null>(
    null,
  );
  const [blastNodeKey, setBlastNodeKey] = useState("service:payment-api");
  const [blastResult, setBlastResult] = useState<BlastRadius | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const graphQuery = useQuery({
    queryKey: ["topology", "graph"],
    queryFn: () => getTopologyGraph(80),
  });

  const incidentsQuery = useQuery({
    queryKey: ["incidents"],
    queryFn: listIncidents,
  });

  useEffect(() => {
    if (selectedIncidentId === null && incidentsQuery.data?.length) {
      setSelectedIncidentId(incidentsQuery.data[0].id);
    }
  }, [incidentsQuery.data, selectedIncidentId]);

  const incidentQuery = useQuery({
    queryKey: ["incidents", selectedIncidentId],
    queryFn: () => getIncident(selectedIncidentId ?? 0),
    enabled: selectedIncidentId !== null,
  });

  const anchorEventId = incidentQuery.data?.events[0]?.eventId;
  const timelineQuery = useQuery({
    queryKey: ["timeline", anchorEventId],
    queryFn: () => getTimeline(anchorEventId ?? 0),
    enabled: Boolean(anchorEventId),
  });

  const similarQuery = useQuery({
    queryKey: ["incidents", selectedIncidentId, "similar"],
    queryFn: () => getSimilarIncidents(selectedIncidentId ?? 0, 5),
    enabled: selectedIncidentId !== null,
  });

  const blastMutation = useMutation({
    mutationFn: getBlastRadius,
    onSuccess: (result) => {
      setBlastResult(result);
      setNotice(`Blast Radius 已计算：影响 ${result.nodes.length} 个节点。`);
      setError(null);
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const confirmMutation = useMutation({
    mutationFn: confirmRootCause,
    onSuccess: () => {
      setNotice("根因候选已确认。");
      setError(null);
      void queryClient.invalidateQueries({
        queryKey: ["incidents", selectedIncidentId],
      });
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const markdown = useMemo(
    () =>
      buildIncidentMarkdown(
        incidentQuery.data,
        timelineQuery.data?.items,
        similarQuery.data,
      ),
    [incidentQuery.data, timelineQuery.data?.items, similarQuery.data],
  );

  async function copyMarkdown() {
    if (!markdown) {
      return;
    }
    await navigator.clipboard?.writeText(markdown);
    setNotice("Markdown 报告已复制到剪贴板。");
  }

  return (
    <div className="mx-auto max-w-[1800px] space-y-6">
      <section className="flex flex-col justify-between gap-4 xl:flex-row xl:items-end">
        <div>
          <p className="text-sm font-medium text-brand-700">
            Operations Intelligence
          </p>
          <h1 className="mt-2 text-3xl font-semibold tracking-tight text-slate-950">
            拓扑 / 故障中心
          </h1>
          <p className="mt-3 max-w-3xl text-sm leading-6 text-slate-600">
            将服务拓扑、Blast Radius、Incident Timeline、Evidence
            和根因候选放在同一个工作台里， 用于告警归并后的快速研判与报告输出。
          </p>
        </div>
        <div className="grid grid-cols-3 gap-3 rounded-2xl border border-slate-200 bg-white p-3 text-center shadow-sm">
          <Metric label="拓扑节点" value={graphQuery.data?.nodes.length ?? 0} />
          <Metric label="拓扑边" value={graphQuery.data?.edges.length ?? 0} />
          <Metric label="故障单" value={incidentsQuery.data?.length ?? 0} />
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

      <div className="grid gap-6 2xl:grid-cols-[minmax(0,1.05fr)_minmax(520px,0.95fr)]">
        <div className="space-y-6">
          <TopologyGraphCard
            graph={graphQuery.data}
            loading={graphQuery.isLoading}
            error={
              graphQuery.error ? toAPIErrorMessage(graphQuery.error) : null
            }
          />
          <BlastRadiusCard
            nodeKey={blastNodeKey}
            onNodeKeyChange={setBlastNodeKey}
            result={blastResult}
            loading={blastMutation.isPending}
            onAnalyze={() =>
              blastMutation.mutate({
                nodeKey: blastNodeKey.trim(),
                direction: "both",
                hops: 2,
                maxNodes: 80,
              })
            }
          />
        </div>

        <div className="space-y-6">
          <IncidentSelector
            incidents={incidentsQuery.data ?? []}
            selectedIncidentId={selectedIncidentId}
            onSelect={setSelectedIncidentId}
            loading={incidentsQuery.isLoading}
          />

          <IncidentWorkspace
            detail={incidentQuery.data}
            loading={incidentQuery.isLoading}
            timelineItems={timelineQuery.data?.items ?? []}
            timelineLoading={timelineQuery.isLoading}
            similarIncidents={similarQuery.data ?? []}
            similarLoading={similarQuery.isLoading}
            markdown={markdown}
            onCopyMarkdown={copyMarkdown}
            confirmingId={
              confirmMutation.variables?.candidateId &&
              confirmMutation.isPending
                ? confirmMutation.variables.candidateId
                : null
            }
            onConfirmRootCause={(candidateId) => {
              if (!selectedIncidentId) {
                return;
              }
              confirmMutation.mutate({
                incidentId: selectedIncidentId,
                candidateId,
              });
            }}
          />
        </div>
      </div>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: number }) {
  return (
    <div className="min-w-24 rounded-xl bg-slate-50 px-4 py-3">
      <p className="text-2xl font-semibold text-slate-950">{value}</p>
      <p className="mt-1 text-xs text-slate-500">{label}</p>
    </div>
  );
}

function TopologyGraphCard({
  graph,
  loading,
  error,
}: {
  graph?: TopologyGraph;
  loading: boolean;
  error: string | null;
}) {
  const groupedNodes = useMemo(() => {
    const groups = new Map<string, TopologyNode[]>();
    for (const node of graph?.nodes ?? []) {
      const items = groups.get(node.kind) ?? [];
      items.push(node);
      groups.set(node.kind, items);
    }
    return Array.from(groups.entries()).sort(([left], [right]) =>
      left.localeCompare(right),
    );
  }, [graph?.nodes]);

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-3">
          <div className="grid size-10 place-items-center rounded-xl bg-brand-50 text-brand-700">
            <Network className="size-5" aria-hidden="true" />
          </div>
          <div>
            <CardTitle>拓扑图谱</CardTitle>
            <CardDescription>
              按资源类型分组展示节点，并保留服务依赖边的方向。
            </CardDescription>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        {loading && <LoadingLine text="加载拓扑图谱..." />}
        {error && <EmptyLine text={error} tone="danger" />}
        {!loading && !error && graph?.nodes.length === 0 && (
          <EmptyLine text="暂无拓扑数据，可先同步 K8s 或导入依赖关系。" />
        )}
        <div className="grid gap-3 xl:grid-cols-2">
          {groupedNodes.map(([kind, nodes]) => (
            <div
              key={kind}
              className="rounded-xl border border-slate-200 bg-slate-50 p-3"
            >
              <div className="mb-3 flex items-center justify-between">
                <p className="text-sm font-semibold text-slate-800">{kind}</p>
                <span className="rounded-full bg-white px-2 py-1 text-xs text-slate-500">
                  {nodes.length} nodes
                </span>
              </div>
              <div className="space-y-2">
                {nodes.slice(0, 8).map((node) => (
                  <NodePill key={node.nodeKey} node={node} />
                ))}
              </div>
            </div>
          ))}
        </div>
        <EdgeList graph={graph} maxItems={10} />
      </CardContent>
    </Card>
  );
}

function BlastRadiusCard({
  nodeKey,
  onNodeKeyChange,
  result,
  loading,
  onAnalyze,
}: {
  nodeKey: string;
  onNodeKeyChange: (value: string) => void;
  result: BlastRadius | null;
  loading: boolean;
  onAnalyze: () => void;
}) {
  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-3">
          <div className="grid size-10 place-items-center rounded-xl bg-amber-50 text-amber-700">
            <Radius className="size-5" aria-hidden="true" />
          </div>
          <div>
            <CardTitle>Blast Radius</CardTitle>
            <CardDescription>
              输入节点 key，向上下游各追踪 2 跳，判断潜在影响范围。
            </CardDescription>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-3 md:grid-cols-[1fr_auto]">
          <div className="space-y-2">
            <Label htmlFor="blast-node-key">节点 key</Label>
            <Input
              id="blast-node-key"
              value={nodeKey}
              onChange={(event) => onNodeKeyChange(event.target.value)}
              placeholder="service:payment-api"
            />
          </div>
          <Button
            className="self-end"
            onClick={onAnalyze}
            disabled={loading || nodeKey.trim() === ""}
          >
            {loading && <Loader2 className="size-4 animate-spin" />}
            计算影响面
          </Button>
        </div>

        {result ? (
          <div className="space-y-3 rounded-xl border border-amber-200 bg-amber-50/60 p-4">
            <div className="grid gap-3 text-sm md:grid-cols-4">
              <MetricMini label="根节点" value={result.rootKey} />
              <MetricMini label="方向" value={result.direction} />
              <MetricMini label="跳数" value={`${result.hops}`} />
              <MetricMini
                label="环路"
                value={result.cycleDetected ? "检测到" : "未发现"}
              />
            </div>
            <div className="grid gap-3 md:grid-cols-2">
              {result.nodes.slice(0, 10).map((node) => (
                <NodePill key={node.nodeKey} node={node} />
              ))}
            </div>
            <EdgeList graph={result} maxItems={8} />
          </div>
        ) : (
          <EmptyLine text="尚未计算。可使用默认节点或复制拓扑图谱里的 nodeKey。" />
        )}
      </CardContent>
    </Card>
  );
}

function IncidentSelector({
  incidents,
  selectedIncidentId,
  onSelect,
  loading,
}: {
  incidents: Array<{
    id: number;
    title: string;
    severity: string;
    status: string;
  }>;
  selectedIncidentId: number | null;
  onSelect: (id: number) => void;
  loading: boolean;
}) {
  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-3">
          <div className="grid size-10 place-items-center rounded-xl bg-rose-50 text-rose-700">
            <Siren className="size-5" aria-hidden="true" />
          </div>
          <div>
            <CardTitle>故障单</CardTitle>
            <CardDescription>
              选择一个 Incident 后查看时间线、证据和根因候选。
            </CardDescription>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        {loading && <LoadingLine text="加载故障单..." />}
        {!loading && incidents.length === 0 && (
          <EmptyLine text="暂无故障单，可先从关联分析提升为 Incident。" />
        )}
        <div className="grid gap-2">
          {incidents.slice(0, 8).map((incident) => (
            <button
              key={incident.id}
              type="button"
              onClick={() => onSelect(incident.id)}
              className={cn(
                "rounded-xl border p-3 text-left transition-colors",
                selectedIncidentId === incident.id
                  ? "border-brand-300 bg-brand-50"
                  : "border-slate-200 bg-white hover:bg-slate-50",
              )}
            >
              <div className="flex items-center justify-between gap-3">
                <p className="font-medium text-slate-900">{incident.title}</p>
                <span className="rounded-full bg-slate-100 px-2 py-1 text-xs text-slate-600">
                  {incident.status}
                </span>
              </div>
              <p className="mt-1 text-xs uppercase tracking-wide text-rose-600">
                {incident.severity}
              </p>
            </button>
          ))}
        </div>
      </CardContent>
    </Card>
  );
}

function IncidentWorkspace({
  detail,
  loading,
  timelineItems,
  timelineLoading,
  similarIncidents,
  similarLoading,
  markdown,
  onCopyMarkdown,
  confirmingId,
  onConfirmRootCause,
}: {
  detail?: IncidentDetail;
  loading: boolean;
  timelineItems: TimelineItem[];
  timelineLoading: boolean;
  similarIncidents: SimilarIncident[];
  similarLoading: boolean;
  markdown: string;
  onCopyMarkdown: () => void;
  confirmingId: number | null;
  onConfirmRootCause: (candidateId: number) => void;
}) {
  if (loading) {
    return (
      <Card>
        <CardContent className="pt-6">
          <LoadingLine text="加载 Incident 详情..." />
        </CardContent>
      </Card>
    );
  }

  if (!detail) {
    return (
      <Card>
        <CardContent className="pt-6">
          <EmptyLine text="请选择一个 Incident。" />
        </CardContent>
      </Card>
    );
  }

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <div className="flex flex-col justify-between gap-3 md:flex-row md:items-start">
            <div>
              <CardTitle>{detail.incident.title}</CardTitle>
              <CardDescription>
                {detail.incident.incidentKey} ·{" "}
                {detail.incident.environment ?? "unknown"} ·{" "}
                {detail.incident.systemName ?? "unknown system"}
              </CardDescription>
            </div>
            <div className="flex flex-wrap gap-2">
              <Badge>{detail.incident.severity}</Badge>
              <Badge>{detail.incident.status}</Badge>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <p className="text-sm leading-6 text-slate-600">
            {detail.incident.summary || "暂无摘要。"}
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Incident Timeline</CardTitle>
          <CardDescription>
            优先使用统一 Timeline API；没有锚点事件时展示 Incident 活动。
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          {timelineLoading && <LoadingLine text="加载时间线..." />}
          {!timelineLoading && timelineItems.length > 0
            ? timelineItems
                .slice(0, 12)
                .map((item) => (
                  <TimelineRow
                    key={`${item.eventId}-${item.time}`}
                    time={item.time}
                    title={item.summary}
                    meta={`${item.sourceType} / ${item.eventType} / ${item.status}`}
                    severity={item.severity}
                  />
                ))
            : detail.activities
                .slice(0, 12)
                .map((activity) => (
                  <TimelineRow
                    key={activity.id}
                    time={activity.createdAt}
                    title={activity.action}
                    meta={formatJSON(activity.detail)}
                  />
                ))}
        </CardContent>
      </Card>

      <div className="grid gap-6 xl:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Evidence</CardTitle>
            <CardDescription>
              故障单关联的证据 key，可继续回溯原始日志、指标或事件。
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-2">
            {detail.evidence.length === 0 && (
              <EmptyLine text="暂无关联证据。" />
            )}
            {detail.evidence.map((item) => (
              <div
                key={item.id}
                className="rounded-xl border border-slate-200 bg-white p-3"
              >
                <p className="font-mono text-xs text-slate-900">
                  {item.evidenceKey}
                </p>
                <p className="mt-1 text-xs text-slate-500">
                  linked at {formatDate(item.createdAt)}
                </p>
              </div>
            ))}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Root Cause Candidates</CardTitle>
            <CardDescription>
              支持在前端确认候选根因，便于沉淀历史案例。
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {detail.rootCauses.length === 0 && (
              <EmptyLine text="暂无根因候选。" />
            )}
            {detail.rootCauses.map((candidate) => (
              <div
                key={candidate.id}
                className="rounded-xl border border-slate-200 bg-white p-3"
              >
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <p className="font-medium text-slate-900">
                      {candidate.summary}
                    </p>
                    <p className="mt-1 text-xs text-slate-500">
                      score {(candidate.score * 100).toFixed(0)}%
                    </p>
                  </div>
                  {candidate.confirmed ? (
                    <span className="inline-flex items-center gap-1 rounded-full bg-emerald-50 px-2 py-1 text-xs text-emerald-700">
                      <CheckCircle2 className="size-3" />
                      confirmed
                    </span>
                  ) : (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => onConfirmRootCause(candidate.id)}
                      disabled={confirmingId === candidate.id}
                    >
                      {confirmingId === candidate.id && (
                        <Loader2 className="size-3 animate-spin" />
                      )}
                      确认
                    </Button>
                  )}
                </div>
                {candidate.details && (
                  <pre className="mt-3 max-h-32 overflow-auto rounded-lg bg-slate-50 p-2 text-xs text-slate-600">
                    {formatJSON(candidate.details)}
                  </pre>
                )}
              </div>
            ))}
          </CardContent>
        </Card>
      </div>

      <SimilarIncidentsCard
        incidents={similarIncidents}
        loading={similarLoading}
      />

      <Card>
        <CardHeader>
          <div className="flex flex-col justify-between gap-3 md:flex-row md:items-center">
            <div>
              <CardTitle>报告导出 Markdown</CardTitle>
              <CardDescription>
                生成可复制的故障复盘草稿，包含时间线、证据和根因候选。
              </CardDescription>
            </div>
            <Button
              variant="outline"
              onClick={onCopyMarkdown}
              disabled={!markdown}
            >
              <Clipboard className="size-4" />
              复制 Markdown
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          <textarea
            readOnly
            value={markdown}
            className="min-h-72 w-full rounded-xl border border-slate-200 bg-slate-950 p-4 font-mono text-xs leading-5 text-slate-100 shadow-inner outline-none"
            aria-label="Markdown 报告"
          />
        </CardContent>
      </Card>
    </div>
  );
}

function NodePill({ node }: { node: TopologyNode }) {
  return (
    <div className="rounded-lg border border-slate-200 bg-white p-3">
      <div className="flex items-center gap-2">
        <GitBranch className="size-4 text-brand-600" aria-hidden="true" />
        <p className="truncate text-sm font-medium text-slate-900">
          {node.displayName || node.name}
        </p>
      </div>
      <p className="mt-1 truncate font-mono text-xs text-slate-500">
        {node.nodeKey}
      </p>
      {(node.namespace || node.environment) && (
        <p className="mt-1 text-xs text-slate-500">
          {[node.environment, node.namespace].filter(Boolean).join(" / ")}
        </p>
      )}
    </div>
  );
}

function SimilarIncidentsCard({
  incidents,
  loading,
}: {
  incidents: SimilarIncident[];
  loading: boolean;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>历史相似 Incident</CardTitle>
        <CardDescription>
          基于标签、错误模板和文本相似度匹配；仅供参考，不会自动确认历史根因。
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {loading && <LoadingLine text="匹配历史 Incident..." />}
        {!loading && incidents.length === 0 && (
          <EmptyLine text="暂无相似历史案例。" />
        )}
        {!loading &&
          incidents.map((item) => (
            <div
              key={item.incident.id}
              className="rounded-xl border border-amber-200 bg-amber-50/60 p-3"
            >
              <div className="flex flex-col justify-between gap-2 md:flex-row md:items-start">
                <div>
                  <p className="font-medium text-slate-900">
                    {item.incident.title}
                  </p>
                  <p className="mt-1 text-xs text-slate-600">
                    {item.incident.incidentKey} ·{" "}
                    {item.incident.environment ?? "unknown"} ·{" "}
                    {item.incident.status}
                  </p>
                </div>
                <Badge className="bg-amber-100 text-amber-800">
                  {`${(item.score * 100).toFixed(0)}% similar`}
                </Badge>
              </div>
              <p className="mt-3 text-xs font-medium text-amber-800">
                {item.notice || "仅供参考，不自动确认历史根因。"}
              </p>
              {item.reasons.length > 0 && (
                <ul className="mt-2 list-disc space-y-1 pl-5 text-xs text-slate-600">
                  {item.reasons.map((reason) => (
                    <li key={reason}>{reason}</li>
                  ))}
                </ul>
              )}
            </div>
          ))}
      </CardContent>
    </Card>
  );
}

function EdgeList({
  graph,
  maxItems,
}: {
  graph?: Pick<TopologyGraph, "edges">;
  maxItems: number;
}) {
  if (!graph?.edges.length) {
    return null;
  }
  return (
    <div className="rounded-xl border border-slate-200 bg-white p-3">
      <p className="mb-2 text-sm font-semibold text-slate-800">依赖边</p>
      <div className="space-y-2">
        {graph.edges.slice(0, maxItems).map((edge) => (
          <div
            key={edge.edgeKey}
            className="grid gap-2 rounded-lg bg-slate-50 p-2 text-xs text-slate-600 md:grid-cols-[1fr_auto_1fr]"
          >
            <span className="truncate font-mono">{edge.fromNodeKey}</span>
            <span className="text-brand-700">{edge.edgeType} →</span>
            <span className="truncate font-mono">{edge.toNodeKey}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

function TimelineRow({
  time,
  title,
  meta,
  severity,
}: {
  time: string;
  title: string;
  meta?: string;
  severity?: string;
}) {
  return (
    <div className="relative border-l border-slate-200 pb-4 pl-5 last:pb-0">
      <span className="absolute -left-[7px] top-1 grid size-3.5 place-items-center rounded-full bg-brand-500 ring-4 ring-white" />
      <div className="flex flex-col justify-between gap-1 md:flex-row md:items-start">
        <p className="font-medium text-slate-900">{title}</p>
        <p className="text-xs text-slate-500">{formatDate(time)}</p>
      </div>
      <p className="mt-1 text-xs text-slate-500">{meta}</p>
      {severity && <Badge className="mt-2">{severity}</Badge>}
    </div>
  );
}

function MetricMini({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg bg-white p-3">
      <p className="text-xs text-slate-500">{label}</p>
      <p className="mt-1 truncate text-sm font-semibold text-slate-900">
        {value}
      </p>
    </div>
  );
}

function Badge({
  children,
  className,
}: {
  children: string;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex w-fit rounded-full bg-slate-100 px-2 py-1 text-xs font-medium uppercase tracking-wide text-slate-700",
        className,
      )}
    >
      {children}
    </span>
  );
}

function LoadingLine({ text }: { text: string }) {
  return (
    <div className="flex items-center gap-2 rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-sm text-slate-600">
      <Loader2 className="size-4 animate-spin" aria-hidden="true" />
      {text}
    </div>
  );
}

function EmptyLine({
  text,
  tone = "muted",
}: {
  text: string;
  tone?: "muted" | "danger";
}) {
  return (
    <div
      className={cn(
        "flex items-center gap-2 rounded-xl border px-3 py-2 text-sm",
        tone === "danger"
          ? "border-rose-200 bg-rose-50 text-rose-700"
          : "border-slate-200 bg-slate-50 text-slate-500",
      )}
    >
      <AlertTriangle className="size-4" aria-hidden="true" />
      {text}
    </div>
  );
}

function buildIncidentMarkdown(
  detail?: IncidentDetail,
  timelineItems: TimelineItem[] = [],
  similarIncidents: SimilarIncident[] = [],
) {
  if (!detail) {
    return "";
  }

  const incident = detail.incident;
  const timeline = timelineItems.length
    ? timelineItems.map(
        (item) =>
          `- ${formatDate(item.time)} ${item.summary} (${item.sourceType}/${item.status})`,
      )
    : detail.activities.map(
        (item) =>
          `- ${formatDate(item.createdAt)} ${item.action}: ${formatJSON(item.detail)}`,
      );

  return [
    `# Incident Report: ${incident.title}`,
    "",
    `- Key: ${incident.incidentKey}`,
    `- Severity: ${incident.severity}`,
    `- Status: ${incident.status}`,
    `- Scope: ${[
      incident.environment,
      incident.systemName,
      incident.componentName,
    ]
      .filter(Boolean)
      .join(" / ")}`,
    "",
    "## Summary",
    "",
    incident.summary || "待补充。",
    "",
    "## Timeline",
    "",
    timeline.length ? timeline.join("\n") : "- 暂无时间线。",
    "",
    "## Evidence",
    "",
    detail.evidence.length
      ? detail.evidence.map((item) => `- ${item.evidenceKey}`).join("\n")
      : "- 暂无证据。",
    "",
    "## Root Cause Candidates",
    "",
    detail.rootCauses.length
      ? detail.rootCauses
          .map(
            (item) =>
              `- [${item.confirmed ? "x" : " "}] ${item.summary} (${(item.score * 100).toFixed(0)}%)`,
          )
          .join("\n")
      : "- 暂无候选根因。",
    "",
    "## Similar Incidents",
    "",
    similarIncidents.length
      ? similarIncidents
          .map(
            (item) =>
              `- ${item.incident.incidentKey}: ${item.incident.title} (${(item.score * 100).toFixed(0)}%) — ${item.notice || "仅供参考，不自动确认历史根因。"}`,
          )
          .join("\n")
      : "- 暂无相似历史案例。",
    "",
    "## Follow-ups",
    "",
    "- [ ] 补充影响范围和用户感知",
    "- [ ] 确认修复动作与防复发项",
  ].join("\n");
}

function formatDate(value?: string) {
  if (!value) {
    return "-";
  }
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value));
}

function formatJSON(value: unknown) {
  if (!value) {
    return "";
  }
  if (typeof value === "string") {
    return value;
  }
  return JSON.stringify(value, null, 2);
}
