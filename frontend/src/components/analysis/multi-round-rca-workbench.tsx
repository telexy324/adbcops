import { FormEvent, ReactNode, useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  Ban,
  BookOpen,
  CheckCircle2,
  ChevronRight,
  CircleDot,
  Clock3,
  Database,
  FileText,
  History,
  Loader2,
  Network,
  Play,
  RefreshCcw,
  RotateCcw,
  Search,
  ShieldAlert,
  Square,
} from "lucide-react";

import { toAPIErrorMessage } from "@/api/analysis";
import {
  cancelRCARun,
  createRCARun,
  getRCADetail,
  getRCAReport,
  listRCARuns,
  orchestrateRCARun,
  recoverRCARun,
  type RCAAction,
  type RCADatabaseDiagnosis,
  type RCADetail,
  type RCAEvidence,
  type RCAOrchestratorResult,
  type RCAReport,
  type RCARun,
  type RCATopologyCandidate,
  type RCATopologyInvestigation,
} from "@/api/rca";
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

const activeRunStorageKey = "adbcops.activeRcaRunId";

type Props = {
  demoMode?: boolean;
  initialNodeKey?: string;
};

type AnalysisForm = {
  question: string;
  environment: string;
  serviceName: string;
  timeStart: string;
  timeEnd: string;
};

export function MultiRoundRCAWorkbench({
  demoMode = false,
  initialNodeKey = "",
}: Props) {
  const queryClient = useQueryClient();
  const defaults = useMemo(defaultAnalysisRange, []);
  const [form, setForm] = useState<AnalysisForm>({
    question: "订单服务变慢，请查询可能原因",
    environment: "prod",
    serviceName: "",
    timeStart: defaults.start,
    timeEnd: defaults.end,
  });
  const [activeRunId, setActiveRunId] = useState<number | null>(() =>
    initialActiveRunID(demoMode),
  );
  const [orchestration, setOrchestration] =
    useState<RCAOrchestratorResult | null>(
      demoMode ? demoOrchestratorResult : null,
    );
  const [selectedEvidence, setSelectedEvidence] = useState<RCAEvidence | null>(
    null,
  );
  const [notice, setNotice] = useState<string | null>(
    demoMode ? "演示数据已加载：三轮调查与 RCA 报告均可展开查看。" : null,
  );
  const [error, setError] = useState<string | null>(null);

  const historyQuery = useQuery({
    queryKey: ["rca", "runs"],
    queryFn: () => listRCARuns(20),
    enabled: !demoMode,
    refetchInterval: 10_000,
  });

  const detailQuery = useQuery({
    queryKey: ["rca", "detail", activeRunId],
    queryFn: () => getRCADetail(activeRunId!),
    enabled: !demoMode && Boolean(activeRunId),
    refetchInterval: (query) => {
      const detail = query.state.data;
      return detail && runStillActive(detail.run) ? 1_500 : false;
    },
  });

  const detail = demoMode ? demoDetail : detailQuery.data;
  const run = detail?.run ?? orchestration?.run;
  const shouldLoadReport = Boolean(
    activeRunId && run && !runStillActive(run) && run.status !== "cancelled",
  );
  const reportQuery = useQuery({
    queryKey: ["rca", "report", activeRunId],
    queryFn: () => getRCAReport(activeRunId!),
    enabled: !demoMode && shouldLoadReport,
  });
  const report =
    (demoMode ? demoReport : undefined) ??
    orchestration?.report ??
    reportQuery.data;

  useEffect(() => {
    if (!activeRunId || demoMode) {
      return;
    }
    window.localStorage.setItem(activeRunStorageKey, String(activeRunId));
    const url = new URL(window.location.href);
    url.searchParams.set("runId", String(activeRunId));
    window.history.replaceState({}, "", url);
  }, [activeRunId, demoMode]);

  useEffect(() => {
    if (detailQuery.isError) {
      setError(toAPIErrorMessage(detailQuery.error));
    }
  }, [detailQuery.error, detailQuery.isError]);

  const orchestrateMutation = useMutation({
    mutationFn: orchestrateRCARun,
    onSuccess: (result) => {
      setOrchestration(result);
      setNotice(`RCA 已停止：${stopReasonLabel(result.stopReason)}。`);
      setError(null);
      void refreshRCAQueries(queryClient, result.run.id);
    },
    onError: (cause) => {
      setError(toAPIErrorMessage(cause));
      if (activeRunId) {
        void refreshRCAQueries(queryClient, activeRunId);
      }
    },
  });

  const createMutation = useMutation({
    mutationFn: createRCARun,
    onSuccess: (created) => {
      setActiveRunId(created.id);
      setOrchestration(null);
      setSelectedEvidence(null);
      setNotice(`RCA #${created.id} 已创建，正在并行收集第一轮证据。`);
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ["rca", "runs"] });
      orchestrateMutation.mutate(created.id);
    },
    onError: (cause) => setError(toAPIErrorMessage(cause)),
  });

  const cancelMutation = useMutation({
    mutationFn: cancelRCARun,
    onSuccess: (cancelled) => {
      setNotice(`RCA #${cancelled.id} 已取消；后端不会继续调度新的 Skill。`);
      setError(null);
      void refreshRCAQueries(queryClient, cancelled.id);
    },
    onError: (cause) => setError(toAPIErrorMessage(cause)),
  });

  const recoverMutation = useMutation({
    mutationFn: recoverRCARun,
    onSuccess: (recovery) => {
      setNotice(
        `已保留 ${recovery.skippedActionIds.length} 个成功动作，准备重试 ${recovery.retryableActionIds.length} 个动作。`,
      );
      setError(null);
      void refreshRCAQueries(queryClient, recovery.run.id);
      orchestrateMutation.mutate(recovery.run.id);
    },
    onError: (cause) => setError(toAPIErrorMessage(cause)),
  });

  const scopeAmbiguities = scopeHints(form);
  const invalidWindow =
    !form.timeStart ||
    !form.timeEnd ||
    new Date(form.timeEnd).getTime() <= new Date(form.timeStart).getTime();
  const historyRuns = demoMode ? demoHistory : (historyQuery.data ?? []);
  const isRunning = Boolean(run && runStillActive(run));

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (invalidWindow || !form.question.trim()) {
      setError("请填写问题，并确认结束时间晚于开始时间。");
      return;
    }
    if (demoMode) {
      setActiveRunId(demoDetail.run.id);
      setOrchestration(demoOrchestratorResult);
      setNotice("演示 RCA 已重新加载。");
      return;
    }
    createMutation.mutate({
      query: form.question.trim(),
      maxRounds: 3,
      timeoutSeconds: 900,
      scope: {
        environment: optionalValue(form.environment),
        serviceName: optionalValue(form.serviceName),
        serviceQuery: form.serviceName.trim()
          ? undefined
          : form.question.trim(),
        topologyNodeKey: optionalValue(initialNodeKey),
        from: new Date(form.timeStart).toISOString(),
        to: new Date(form.timeEnd).toISOString(),
      },
    });
  }

  function selectHistory(item: RCARun) {
    setActiveRunId(item.id);
    setOrchestration(null);
    setSelectedEvidence(null);
    setNotice(`已加载历史 RCA #${item.id}。`);
    setError(null);
  }

  return (
    <section className="space-y-5" aria-label="多轮智能分析">
      <Card className="overflow-hidden border-slate-300 bg-slate-950 text-white shadow-xl">
        <CardHeader className="border-b border-white/10 bg-[radial-gradient(circle_at_top_right,rgba(198,168,111,0.22),transparent_42%)]">
          <div className="flex flex-col justify-between gap-4 lg:flex-row lg:items-start">
            <div>
              <div className="flex items-center gap-2 text-xs font-semibold uppercase tracking-[0.18em] text-brand-300">
                <CircleDot className="size-4" />
                Evidence-driven RCA
              </div>
              <CardTitle className="mt-3 text-2xl text-white">
                多轮智能运维分析
              </CardTitle>
              <CardDescription className="mt-2 max-w-3xl text-slate-300">
                第一轮并行采集日志、指标和知识，第二轮沿拓扑调查相关组件，第三轮按证据深入数据库；所有动作只读且可追溯。
              </CardDescription>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <SafetyBadge>只读 Skill</SafetyBadge>
              <SafetyBadge>最多 3 轮</SafetyBadge>
              <SafetyBadge>无自动修复</SafetyBadge>
            </div>
          </div>
        </CardHeader>
        <CardContent className="grid gap-5 p-5 xl:grid-cols-[minmax(0,1fr)_320px]">
          <form className="space-y-4" onSubmit={submit}>
            <div>
              <Label htmlFor="rca-question" className="text-slate-200">
                你想调查什么？
              </Label>
              <textarea
                id="rca-question"
                aria-label="智能分析问题"
                className="mt-2 min-h-24 w-full rounded-xl border border-white/15 bg-white/5 px-4 py-3 text-sm leading-6 text-white outline-none placeholder:text-slate-500 focus:border-brand-300 focus:ring-2 focus:ring-brand-300/20"
                value={form.question}
                onChange={(event) =>
                  setForm((current) => ({
                    ...current,
                    question: event.target.value,
                  }))
                }
                placeholder="例如：订单服务变慢，请查询可能原因"
              />
            </div>
            <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
              <DarkField label="环境（可选）">
                <DarkInput
                  aria-label="RCA 环境"
                  value={form.environment}
                  placeholder="prod"
                  onChange={(value) =>
                    setForm((current) => ({
                      ...current,
                      environment: value,
                    }))
                  }
                />
              </DarkField>
              <DarkField label="服务（可选）">
                <DarkInput
                  aria-label="RCA 服务"
                  value={form.serviceName}
                  placeholder="从问题中解析"
                  onChange={(value) =>
                    setForm((current) => ({
                      ...current,
                      serviceName: value,
                    }))
                  }
                />
              </DarkField>
              <DarkField label="开始时间">
                <DarkInput
                  aria-label="RCA 开始时间"
                  type="datetime-local"
                  value={form.timeStart}
                  onChange={(value) =>
                    setForm((current) => ({
                      ...current,
                      timeStart: value,
                    }))
                  }
                />
              </DarkField>
              <DarkField label="结束时间">
                <DarkInput
                  aria-label="RCA 结束时间"
                  type="datetime-local"
                  value={form.timeEnd}
                  onChange={(value) =>
                    setForm((current) => ({
                      ...current,
                      timeEnd: value,
                    }))
                  }
                />
              </DarkField>
            </div>

            <div className="grid gap-3 md:grid-cols-[1fr_auto] md:items-end">
              <ScopePreview
                form={form}
                initialNodeKey={initialNodeKey}
                hints={scopeAmbiguities}
                invalidWindow={invalidWindow}
              />
              <Button
                type="submit"
                size="lg"
                className="h-12 bg-brand-300 text-slate-950 hover:bg-brand-200"
                disabled={
                  createMutation.isPending ||
                  orchestrateMutation.isPending ||
                  invalidWindow ||
                  !form.question.trim()
                }
              >
                {createMutation.isPending || orchestrateMutation.isPending ? (
                  <Loader2 className="mr-2 size-4 animate-spin" />
                ) : (
                  <Play className="mr-2 size-4" />
                )}
                开始三轮分析
              </Button>
            </div>
          </form>

          <HistoryPanel
            runs={historyRuns}
            activeRunId={activeRunId}
            loading={historyQuery.isLoading}
            onSelect={selectHistory}
          />
        </CardContent>
      </Card>

      {(notice || error) && (
        <WorkbenchAlert message={error ?? notice!} error={Boolean(error)} />
      )}

      {run ? (
        <>
          <RunToolbar
            run={run}
            running={isRunning}
            cancelling={cancelMutation.isPending}
            recovering={
              recoverMutation.isPending || orchestrateMutation.isPending
            }
            onCancel={() => cancelMutation.mutate(run.id)}
            onRecover={() => recoverMutation.mutate(run.id)}
            onRefresh={() => {
              void refreshRCAQueries(queryClient, run.id);
              setNotice(`正在刷新 RCA #${run.id}。`);
            }}
          />
          <RoundTimeline
            detail={detail}
            orchestration={orchestration}
            running={isRunning}
            onEvidence={setSelectedEvidence}
          />
          <div className="grid gap-5 xl:grid-cols-[minmax(0,1.45fr)_minmax(340px,0.55fr)]">
            <div className="space-y-5">
              <TopologyExpansion
                investigation={
                  orchestration?.rounds.find((item) => item.roundNumber === 2)
                    ?.topologyInvestigation
                }
                actions={detail?.actions ?? []}
              />
              <SlowSQLPanel
                diagnosis={
                  orchestration?.rounds.find((item) => item.roundNumber === 3)
                    ?.databaseDiagnosis
                }
                evidence={detail?.evidence ?? []}
                onEvidence={setSelectedEvidence}
              />
            </div>
            <EvidencePanel
              evidence={detail?.evidence ?? []}
              selected={selectedEvidence}
              onSelect={setSelectedEvidence}
            />
          </div>
          {report && (
            <FinalReport
              report={report}
              onEvidenceID={(id) => {
                const item = detail?.evidence.find(
                  (evidence) => evidence.id === id,
                );
                if (item) {
                  setSelectedEvidence(item);
                }
              }}
            />
          )}
        </>
      ) : (
        <EmptyRunState />
      )}
    </section>
  );
}

function ScopePreview({
  form,
  initialNodeKey,
  hints,
  invalidWindow,
}: {
  form: AnalysisForm;
  initialNodeKey: string;
  hints: string[];
  invalidWindow: boolean;
}) {
  return (
    <div className="rounded-xl border border-white/10 bg-white/[0.035] p-3">
      <div className="flex items-center gap-2 text-xs font-semibold text-slate-300">
        <Search className="size-4 text-brand-300" />
        执行前 Scope
      </div>
      <div className="mt-2 flex flex-wrap gap-2 text-xs">
        <ScopeChip label="环境" value={form.environment || "待解析"} />
        <ScopeChip label="服务" value={form.serviceName || "从问题解析"} />
        <ScopeChip
          label="时间"
          value={`${formatLocalInput(form.timeStart)} — ${formatLocalInput(form.timeEnd)}`}
        />
        {initialNodeKey && (
          <ScopeChip label="拓扑节点" value={initialNodeKey} />
        )}
      </div>
      {(hints.length > 0 || invalidWindow) && (
        <div className="mt-3 space-y-1 text-xs text-amber-200">
          {hints.map((hint) => (
            <p key={hint}>· {hint}</p>
          ))}
          {invalidWindow && <p>· 时间范围无效，请检查开始和结束时间。</p>}
        </div>
      )}
    </div>
  );
}

function HistoryPanel({
  runs,
  activeRunId,
  loading,
  onSelect,
}: {
  runs: RCARun[];
  activeRunId: number | null;
  loading: boolean;
  onSelect: (run: RCARun) => void;
}) {
  return (
    <aside className="rounded-xl border border-white/10 bg-white/[0.035] p-3">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2 text-sm font-semibold">
          <History className="size-4 text-brand-300" />
          历史 RCA
        </div>
        <span className="text-xs text-slate-500">{runs.length} 条</span>
      </div>
      <div className="mt-3 max-h-64 space-y-2 overflow-y-auto">
        {loading && (
          <div className="flex items-center gap-2 py-4 text-xs text-slate-400">
            <Loader2 className="size-4 animate-spin" />
            加载历史记录
          </div>
        )}
        {!loading && runs.length === 0 && (
          <p className="py-4 text-xs text-slate-500">暂无历史 RCA。</p>
        )}
        {runs.map((item) => (
          <button
            key={item.id}
            type="button"
            className={cn(
              "w-full rounded-lg border px-3 py-2 text-left transition-colors",
              activeRunId === item.id
                ? "border-brand-300/50 bg-brand-300/10"
                : "border-white/10 bg-black/10 hover:bg-white/5",
            )}
            onClick={() => onSelect(item)}
          >
            <div className="flex items-center justify-between gap-2">
              <span className="text-xs font-medium text-white">
                RCA #{item.id}
              </span>
              <StatusBadge status={item.status} dark />
            </div>
            <p className="mt-1 line-clamp-2 text-xs leading-5 text-slate-400">
              {item.query}
            </p>
          </button>
        ))}
      </div>
    </aside>
  );
}

function RunToolbar({
  run,
  running,
  cancelling,
  recovering,
  onCancel,
  onRecover,
  onRefresh,
}: {
  run: RCARun;
  running: boolean;
  cancelling: boolean;
  recovering: boolean;
  onCancel: () => void;
  onRecover: () => void;
  onRefresh: () => void;
}) {
  const recoverable =
    ["failed", "timed_out", "partial_success"].includes(run.status) &&
    Boolean(run.finishedAt);
  return (
    <Card className="border-slate-200 shadow-none">
      <CardContent className="flex flex-col justify-between gap-4 p-4 md:flex-row md:items-center">
        <div className="flex flex-wrap items-center gap-3">
          <div>
            <p className="text-xs text-slate-500">当前运行</p>
            <p className="font-semibold text-slate-950">RCA #{run.id}</p>
          </div>
          <StatusBadge status={run.status} />
          <span className="text-xs text-slate-500">
            Round {run.currentRound}/{run.maxRounds}
          </span>
          {run.stopReason && (
            <span className="text-xs text-slate-500">
              停止：{stopReasonLabel(run.stopReason)}
            </span>
          )}
        </div>
        <div className="flex flex-wrap gap-2">
          <Button type="button" variant="outline" size="sm" onClick={onRefresh}>
            <RefreshCcw className="mr-2 size-4" />
            刷新
          </Button>
          {running && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="border-rose-200 text-rose-700 hover:bg-rose-50 hover:text-rose-800"
              disabled={cancelling}
              onClick={onCancel}
            >
              {cancelling ? (
                <Loader2 className="mr-2 size-4 animate-spin" />
              ) : (
                <Square className="mr-2 size-4" />
              )}
              取消分析
            </Button>
          )}
          {recoverable && (
            <Button
              type="button"
              size="sm"
              disabled={recovering}
              onClick={onRecover}
            >
              {recovering ? (
                <Loader2 className="mr-2 size-4 animate-spin" />
              ) : (
                <RotateCcw className="mr-2 size-4" />
              )}
              恢复并重试
            </Button>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

function RoundTimeline({
  detail,
  orchestration,
  running,
  onEvidence,
}: {
  detail?: RCADetail;
  orchestration: RCAOrchestratorResult | null;
  running: boolean;
  onEvidence: (item: RCAEvidence) => void;
}) {
  const rounds = [1, 2, 3];
  return (
    <div className="grid gap-4 xl:grid-cols-3">
      {rounds.map((number) => {
        const round = detail?.rounds.find(
          (item) => item.roundNumber === number,
        );
        const live = orchestration?.rounds.find(
          (item) => item.roundNumber === number,
        );
        const actions = round
          ? (detail?.actions.filter((item) => item.roundId === round.id) ?? [])
          : [];
        const evidence = round
          ? (detail?.evidence.filter((item) => item.rcaRoundId === round.id) ??
            [])
          : [];
        const status =
          round?.status ??
          (running && number === (detail?.run.currentRound ?? 0) + 1
            ? "pending"
            : "waiting");
        return (
          <Card
            key={number}
            className={cn(
              "border-slate-200 shadow-none",
              status === "running" && "ring-2 ring-brand-300",
            )}
          >
            <CardHeader className="pb-3">
              <div className="flex items-start justify-between gap-3">
                <div>
                  <p className="text-xs font-semibold text-brand-700">
                    ROUND {number}
                  </p>
                  <CardTitle className="mt-1 text-base">
                    {roundTitle(number)}
                  </CardTitle>
                </div>
                <StatusBadge status={status} />
              </div>
              <CardDescription>{roundDescription(number)}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div>
                <SectionLabel>并行查询</SectionLabel>
                <div className="mt-2 space-y-2">
                  {actions.length === 0 ? (
                    <EmptyLine
                      text={
                        status === "waiting"
                          ? "等待前序 Evidence"
                          : "正在生成只读查询计划"
                      }
                    />
                  ) : (
                    actions.map((action) => (
                      <ActionRow key={action.id} action={action} />
                    ))
                  )}
                </div>
              </div>
              <div>
                <SectionLabel>证据摘要</SectionLabel>
                <div className="mt-2 space-y-2">
                  {evidence.length === 0 ? (
                    <EmptyLine text="尚未产生 Evidence" />
                  ) : (
                    evidence.slice(0, 4).map((item) => (
                      <button
                        key={item.id}
                        type="button"
                        className="w-full rounded-lg bg-slate-50 p-2 text-left hover:bg-slate-100"
                        onClick={() => onEvidence(item)}
                      >
                        <div className="flex items-center gap-2">
                          <EvidenceKindBadge kind={item.evidenceKind} />
                          <span className="truncate text-xs text-slate-700">
                            {item.summary}
                          </span>
                        </div>
                      </button>
                    ))
                  )}
                </div>
              </div>
              <HypothesisChanges
                hypotheses={
                  live?.plan?.hypotheses ??
                  round?.inputHypotheses.map((item) => ({
                    id: item.id ?? "",
                    summary: item.summary,
                    confidence: item.confidence,
                    supportingEvidenceIds: item.evidenceIds,
                    contradictingEvidenceIds: [],
                  })) ??
                  []
                }
                rejected={round?.rejectedHypotheses ?? []}
              />
              <NextReason
                liveReasons={live?.plan?.nextActions.map((item) => item.reason)}
                nextSkills={round?.nextActions.map((item) => item.skillName)}
                stopReason={live?.plan?.stopReason}
              />
              {(live?.errors?.length ?? 0) > 0 && (
                <StateMessage
                  tone="danger"
                  title="本轮部分查询失败"
                  items={live?.errors ?? []}
                />
              )}
            </CardContent>
          </Card>
        );
      })}
    </div>
  );
}

function ActionRow({ action }: { action: RCAAction }) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-lg border border-slate-200 px-3 py-2">
      <div className="min-w-0">
        <p className="truncate text-xs font-medium text-slate-800">
          {skillLabel(action.skillName)}
        </p>
        <p className="mt-0.5 text-[11px] text-slate-500">
          Skill · attempt {action.attempt}
        </p>
      </div>
      <StatusBadge status={action.status} compact />
    </div>
  );
}

function HypothesisChanges({
  hypotheses,
  rejected,
}: {
  hypotheses: Array<{
    id?: string;
    summary: string;
    confidence: number;
  }>;
  rejected: Array<{ id?: string; summary: string; confidence: number }>;
}) {
  return (
    <div>
      <SectionLabel>假设变化</SectionLabel>
      <div className="mt-2 space-y-2">
        {hypotheses.length === 0 && rejected.length === 0 ? (
          <EmptyLine text="等待 Evidence 形成假设" />
        ) : (
          <>
            {hypotheses.slice(0, 3).map((item, index) => (
              <div
                key={`${item.id}-${index}`}
                className="rounded-lg border border-blue-100 bg-blue-50 p-2"
              >
                <p className="text-xs text-blue-900">{item.summary}</p>
                <p className="mt-1 text-[11px] text-blue-600">
                  HYPOTHESIS · 候选 · {Math.round(item.confidence * 100)}%
                </p>
              </div>
            ))}
            {rejected.slice(0, 2).map((item, index) => (
              <div
                key={`rejected-${item.id}-${index}`}
                className="rounded-lg border border-slate-200 bg-slate-50 p-2"
              >
                <p className="text-xs text-slate-500 line-through">
                  {item.summary}
                </p>
                <p className="mt-1 text-[11px] text-slate-500">已被反证驳回</p>
              </div>
            ))}
          </>
        )}
      </div>
    </div>
  );
}

function NextReason({
  liveReasons,
  nextSkills,
  stopReason,
}: {
  liveReasons?: string[];
  nextSkills?: string[];
  stopReason?: string;
}) {
  const reasons = (liveReasons ?? []).filter(Boolean);
  return (
    <div className="rounded-lg border border-dashed border-slate-300 p-3">
      <SectionLabel>{stopReason ? "停止判断" : "下一步原因"}</SectionLabel>
      {stopReason ? (
        <p className="mt-2 text-xs leading-5 text-slate-600">
          {stopReasonLabel(stopReason)}
        </p>
      ) : reasons.length > 0 ? (
        <p className="mt-2 text-xs leading-5 text-slate-600">{reasons[0]}</p>
      ) : (nextSkills?.length ?? 0) > 0 ? (
        <p className="mt-2 text-xs leading-5 text-slate-600">
          需要继续调用 {nextSkills?.map(skillLabel).join("、")} 补充证据。
        </p>
      ) : (
        <p className="mt-2 text-xs text-slate-400">等待本轮规划结果。</p>
      )}
    </div>
  );
}

function TopologyExpansion({
  investigation,
  actions,
}: {
  investigation?: RCATopologyInvestigation;
  actions: RCAAction[];
}) {
  const snapshot = investigation ?? topologyFromActions(actions);
  const nodes = snapshot?.candidates ?? [];
  if (!snapshot && !actions.some((item) => isTopologySkill(item.skillName))) {
    return null;
  }
  return (
    <Card className="border-slate-200 shadow-none">
      <CardHeader>
        <div className="flex items-center justify-between gap-3">
          <div>
            <CardTitle className="flex items-center gap-2 text-base">
              <Network className="size-5 text-violet-600" />
              拓扑扩展过程
            </CardTitle>
            <CardDescription>
              仅调查与前序 Evidence 相关且具有只读绑定的依赖。
            </CardDescription>
          </div>
          {snapshot?.fallbackUsed && (
            <span className="rounded-full bg-amber-100 px-2 py-1 text-xs text-amber-800">
              使用显式 Scope 降级
            </span>
          )}
        </div>
      </CardHeader>
      <CardContent>
        <div className="overflow-x-auto rounded-xl border border-slate-200 bg-slate-50 p-5">
          <div className="flex min-w-max items-center gap-3">
            <TopologyNode
              name={snapshot?.rootNodeKey || "待解析根服务"}
              kind="root"
              selected
            />
            {nodes.length > 0 ? (
              nodes.slice(0, 6).map((node) => (
                <div key={node.nodeKey} className="flex items-center gap-3">
                  <div className="flex items-center gap-1 text-slate-400">
                    <div className="h-px w-8 bg-slate-300" />
                    <ChevronRight className="size-4" />
                  </div>
                  <TopologyNode
                    name={node.name || node.nodeKey}
                    kind={node.kind || node.componentType}
                    selected={node.selected}
                    stale={["stale", "expired"].includes(node.freshness)}
                    conflict={node.conflict}
                  />
                </div>
              ))
            ) : (
              <div className="flex items-center gap-3">
                <div className="h-px w-10 bg-slate-300" />
                <span className="text-xs text-slate-500">
                  正在解析 Alias 与上下游依赖
                </span>
              </div>
            )}
          </div>
        </div>
        <div className="mt-3 flex flex-wrap gap-2 text-xs">
          {(snapshot?.observedAliases ?? []).map((alias) => (
            <span
              key={alias}
              className="rounded-full bg-violet-50 px-2 py-1 text-violet-700"
            >
              Alias · {alias}
            </span>
          ))}
          {(snapshot?.conflicts ?? []).map((conflict) => (
            <span
              key={conflict}
              className="rounded-full bg-rose-50 px-2 py-1 text-rose-700"
            >
              冲突 · {conflict}
            </span>
          ))}
        </div>
        {(snapshot?.missingEvidence.length ?? 0) > 0 && (
          <StateMessage
            tone="warning"
            title="拓扑证据缺口"
            items={snapshot?.missingEvidence ?? []}
          />
        )}
      </CardContent>
    </Card>
  );
}

function SlowSQLPanel({
  diagnosis,
  evidence,
  onEvidence,
}: {
  diagnosis?: RCADatabaseDiagnosis;
  evidence: RCAEvidence[];
  onEvidence: (item: RCAEvidence) => void;
}) {
  const tidbEvidence = evidence.filter(
    (item) =>
      item.sourceType.toLowerCase() === "tidb" ||
      item.sourceSkill?.toLowerCase().includes("tidb"),
  );
  if (!diagnosis && tidbEvidence.length === 0) {
    return null;
  }
  const fingerprint =
    diagnosis?.assessment?.highestImpactFingerprint ||
    diagnosis?.sqlFingerprint ||
    findFirstString(
      tidbEvidence.map((item) => item.content),
      ["sql_fingerprint", "digest"],
    );
  const safeSQL = diagnosis?.sanitizedSql
    ? sanitizeSQLForDisplay(diagnosis.sanitizedSql)
    : "";
  return (
    <Card className="border-slate-200 shadow-none">
      <CardHeader>
        <div className="flex items-start justify-between gap-3">
          <div>
            <CardTitle className="flex items-center gap-2 text-base">
              <Database className="size-5 text-amber-600" />
              Slow SQL 深度诊断
            </CardTitle>
            <CardDescription>
              页面仅显示 SQL 指纹或脱敏结构，不提供任意 SQL 输入与执行入口。
            </CardDescription>
          </div>
          <span className="rounded-full bg-amber-100 px-2 py-1 text-xs font-medium text-amber-900">
            {diagnosis?.provider ?? "TiDB"}
          </span>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-3 sm:grid-cols-2">
          <MetricBox
            label="最高影响指纹"
            value={fingerprint || "等待慢查询指纹"}
            mono
          />
          <MetricBox
            label="诊断置信度"
            value={diagnosis?.assessment?.confidence ?? "待评估"}
          />
        </div>
        {safeSQL && (
          <div className="rounded-xl bg-slate-950 p-3">
            <p className="text-[11px] font-semibold uppercase tracking-wider text-slate-500">
              Sanitized SQL
            </p>
            <code className="mt-2 block whitespace-pre-wrap text-xs leading-5 text-emerald-300">
              {safeSQL}
            </code>
          </div>
        )}
        {(diagnosis?.assessment?.categories.length ?? 0) > 0 && (
          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {diagnosis?.assessment?.categories.map((category) => (
              <div
                key={`${category.category}-${category.sourceSkill}`}
                className="flex items-center justify-between rounded-lg border border-slate-200 px-3 py-2"
              >
                <span className="text-xs text-slate-700">
                  {diagnosisCategoryLabel(category.category)}
                </span>
                {category.collected ? (
                  <CheckCircle2 className="size-4 text-emerald-600" />
                ) : (
                  <AlertCircle className="size-4 text-amber-600" />
                )}
              </div>
            ))}
          </div>
        )}
        <div className="flex flex-wrap gap-2">
          {tidbEvidence.map((item) => (
            <button
              key={item.id}
              type="button"
              className="rounded-full border border-amber-200 bg-amber-50 px-3 py-1.5 text-xs text-amber-800 hover:bg-amber-100"
              onClick={() => onEvidence(item)}
            >
              Evidence #{item.id} · {skillLabel(item.sourceSkill ?? "tidb")}
            </button>
          ))}
        </div>
        {((diagnosis?.missingEvidence.length ?? 0) > 0 ||
          (diagnosis?.assessment?.missingEvidence.length ?? 0) > 0) && (
          <StateMessage
            tone="warning"
            title="数据库证据缺口"
            items={[
              ...(diagnosis?.missingEvidence ?? []),
              ...(diagnosis?.assessment?.missingEvidence ?? []),
            ]}
          />
        )}
      </CardContent>
    </Card>
  );
}

function EvidencePanel({
  evidence,
  selected,
  onSelect,
}: {
  evidence: RCAEvidence[];
  selected: RCAEvidence | null;
  onSelect: (item: RCAEvidence) => void;
}) {
  const selectedItem = selected ?? evidence[0];
  return (
    <Card className="h-fit border-slate-200 shadow-none xl:sticky xl:top-4">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <FileText className="size-5 text-brand-700" />
          Evidence 详情
        </CardTitle>
        <CardDescription>
          FACT、RULE、KNOWLEDGE、HYPOTHESIS 使用独立标签。
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="flex max-h-36 flex-wrap gap-2 overflow-y-auto">
          {evidence.map((item) => (
            <button
              key={item.id}
              type="button"
              className={cn(
                "rounded-lg border px-2.5 py-1.5 text-xs",
                selectedItem?.id === item.id
                  ? "border-brand-400 bg-brand-50 text-brand-800"
                  : "border-slate-200 text-slate-600 hover:bg-slate-50",
              )}
              onClick={() => onSelect(item)}
            >
              #{item.id} {evidenceKindLabel(item.evidenceKind)}
            </button>
          ))}
        </div>
        {selectedItem ? (
          <div className="space-y-3 rounded-xl border border-slate-200 p-3">
            <div className="flex flex-wrap items-center gap-2">
              <EvidenceKindBadge kind={selectedItem.evidenceKind} />
              <span className="text-xs text-slate-500">
                {selectedItem.sourceType}
              </span>
              {selectedItem.sourceSkill && (
                <span className="text-xs text-slate-500">
                  · {skillLabel(selectedItem.sourceSkill)}
                </span>
              )}
            </div>
            <p className="text-sm leading-6 text-slate-800">
              {selectedItem.summary}
            </p>
            <pre className="max-h-72 overflow-auto rounded-lg bg-slate-950 p-3 text-xs leading-5 text-slate-200">
              {safeEvidencePreview(selectedItem)}
            </pre>
            <a
              className="inline-flex items-center text-xs font-medium text-brand-700 hover:underline"
              href={`/api/evidence/${selectedItem.id}`}
              target="_blank"
              rel="noreferrer"
            >
              打开 Evidence #{selectedItem.id}
              <ChevronRight className="ml-1 size-3" />
            </a>
          </div>
        ) : (
          <EmptyLine text="运行后可在这里查看证据详情" />
        )}
      </CardContent>
    </Card>
  );
}

function FinalReport({
  report,
  onEvidenceID,
}: {
  report: RCAReport;
  onEvidenceID: (id: number) => void;
}) {
  return (
    <Card className="border-slate-300 shadow-none">
      <CardHeader className="border-b border-slate-200 bg-white">
        <div className="flex flex-col justify-between gap-3 md:flex-row md:items-start">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.16em] text-brand-700">
              Final RCA Report
            </p>
            <CardTitle className="mt-2 text-xl">最终分析报告</CardTitle>
            <CardDescription className="mt-2 max-w-4xl">
              {report.conclusion}
            </CardDescription>
          </div>
          <div className="flex gap-2">
            <StatusBadge status={report.status} />
            {report.incomplete && (
              <span className="rounded-full bg-amber-100 px-3 py-1 text-xs font-semibold text-amber-900">
                Evidence 不完整
              </span>
            )}
          </div>
        </div>
      </CardHeader>
      <CardContent className="grid gap-5 p-5 xl:grid-cols-3">
        <div className="space-y-4 xl:col-span-2">
          <div>
            <SectionLabel>根因候选（按证据强度）</SectionLabel>
            <div className="mt-2 space-y-3">
              {report.rootCauseCandidates.length === 0 ? (
                <StateMessage
                  tone="warning"
                  title="暂无法定位"
                  items={["当前没有具备充分 Evidence 支撑的根因候选。"]}
                />
              ) : (
                report.rootCauseCandidates.map((candidate, index) => (
                  <div
                    key={candidate.id}
                    className="rounded-xl border border-slate-200 p-4"
                  >
                    <div className="flex flex-wrap items-center justify-between gap-3">
                      <div className="flex items-center gap-2">
                        <span className="grid size-7 place-items-center rounded-full bg-slate-950 text-xs font-semibold text-white">
                          {index + 1}
                        </span>
                        <p className="font-medium text-slate-900">
                          {candidate.summary}
                        </p>
                      </div>
                      <span className="text-xs text-slate-500">
                        {candidate.evidenceStrength} ·{" "}
                        {Math.round(candidate.confidence * 100)}%
                      </span>
                    </div>
                    <div className="mt-3 grid gap-3 md:grid-cols-2">
                      <EvidenceLinks
                        label="支持 Evidence"
                        items={candidate.supportingEvidence}
                        tone="support"
                        onEvidenceID={onEvidenceID}
                      />
                      <EvidenceLinks
                        label="反证"
                        items={candidate.contradictingEvidence}
                        tone="against"
                        onEvidenceID={onEvidenceID}
                      />
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>
          <div>
            <SectionLabel>排查过程</SectionLabel>
            <div className="mt-2 grid gap-2 md:grid-cols-3">
              {report.investigation.map((item) => (
                <div
                  key={item.roundNumber}
                  className="rounded-xl border border-slate-200 p-3"
                >
                  <div className="flex items-center justify-between">
                    <span className="text-xs font-semibold">
                      Round {item.roundNumber}
                    </span>
                    <StatusBadge status={item.status} compact />
                  </div>
                  <p className="mt-2 text-xs leading-5 text-slate-600">
                    查询 {item.checked.length} 项，形成 {item.findings.length}{" "}
                    条 Evidence。
                  </p>
                  {(item.continueReason || item.stopReason) && (
                    <p className="mt-2 text-xs leading-5 text-slate-500">
                      {item.stopReason
                        ? `停止：${stopReasonLabel(item.stopReason)}`
                        : `继续：${item.continueReason}`}
                    </p>
                  )}
                </div>
              ))}
            </div>
          </div>
        </div>
        <div className="space-y-4">
          {report.missingEvidence.length > 0 && (
            <StateMessage
              tone="warning"
              title="Missing Evidence"
              items={report.missingEvidence}
            />
          )}
          <div className="rounded-xl border border-slate-200 p-4">
            <SectionLabel>建议（不会自动执行）</SectionLabel>
            <ul className="mt-2 space-y-2">
              {report.suggestions.map((item, index) => (
                <li
                  key={`${item.summary}-${index}`}
                  className="flex gap-2 text-xs leading-5 text-slate-600"
                >
                  <ShieldAlert className="mt-0.5 size-4 shrink-0 text-brand-700" />
                  {item.summary}
                </li>
              ))}
            </ul>
          </div>
          <div className="rounded-xl bg-slate-950 p-4 text-white">
            <SectionLabel light>追溯信息</SectionLabel>
            <div className="mt-3 grid grid-cols-2 gap-2 text-xs text-slate-300">
              <TraceValue label="RCA Run" value={report.runId} />
              <TraceValue
                label="停止原因"
                value={stopReasonLabel(report.stopReason)}
              />
              <TraceValue label="FACT" value={report.evidence.facts.length} />
              <TraceValue label="RULE" value={report.evidence.rules.length} />
              <TraceValue
                label="KNOWLEDGE"
                value={report.evidence.knowledge.length}
              />
              <TraceValue
                label="HYPOTHESIS"
                value={report.evidence.hypotheses.length}
              />
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

function EvidenceLinks({
  label,
  items,
  tone,
  onEvidenceID,
}: {
  label: string;
  items: Array<{ id: number; summary: string }>;
  tone: "support" | "against";
  onEvidenceID: (id: number) => void;
}) {
  return (
    <div
      className={cn(
        "rounded-lg p-3",
        tone === "support" ? "bg-emerald-50" : "bg-rose-50",
      )}
    >
      <p
        className={cn(
          "text-xs font-semibold",
          tone === "support" ? "text-emerald-800" : "text-rose-800",
        )}
      >
        {label}
      </p>
      {items.length === 0 ? (
        <p className="mt-2 text-xs text-slate-500">暂无</p>
      ) : (
        <div className="mt-2 space-y-1">
          {items.map((item) => (
            <button
              key={item.id}
              type="button"
              className="block w-full truncate text-left text-xs text-slate-700 hover:underline"
              onClick={() => onEvidenceID(item.id)}
            >
              #{item.id} · {item.summary}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function EmptyRunState() {
  return (
    <div className="rounded-2xl border border-dashed border-slate-300 bg-white/60 px-6 py-12 text-center">
      <div className="mx-auto grid size-12 place-items-center rounded-full bg-slate-100">
        <Search className="size-5 text-slate-500" />
      </div>
      <h2 className="mt-4 font-semibold text-slate-900">
        输入问题后开始 Evidence 驱动调查
      </h2>
      <p className="mx-auto mt-2 max-w-xl text-sm leading-6 text-slate-500">
        默认时间范围已显示在执行前 Scope 中。运行后，每个并行 Skill、Evidence
        和假设变化都会独立展示。
      </p>
    </div>
  );
}

function WorkbenchAlert({
  message,
  error,
}: {
  message: string;
  error: boolean;
}) {
  const permission = error && /403|forbidden|权限/i.test(message);
  const timeout = error && /timeout|超时/i.test(message);
  return (
    <div
      role="status"
      className={cn(
        "flex items-start gap-3 rounded-xl border px-4 py-3 text-sm",
        permission
          ? "border-fuchsia-200 bg-fuchsia-50 text-fuchsia-800"
          : timeout
            ? "border-violet-200 bg-violet-50 text-violet-800"
            : error
              ? "border-rose-200 bg-rose-50 text-rose-800"
              : "border-emerald-200 bg-emerald-50 text-emerald-800",
      )}
    >
      {permission ? (
        <Ban className="mt-0.5 size-4 shrink-0" />
      ) : error ? (
        <AlertCircle className="mt-0.5 size-4 shrink-0" />
      ) : (
        <CheckCircle2 className="mt-0.5 size-4 shrink-0" />
      )}
      <div>
        <p className="font-medium">
          {permission
            ? "权限不足"
            : timeout
              ? "运行超时"
              : error
                ? "请求失败"
                : "状态更新"}
        </p>
        <p className="mt-0.5">{message}</p>
      </div>
    </div>
  );
}

function StateMessage({
  tone,
  title,
  items,
}: {
  tone: "warning" | "danger";
  title: string;
  items: string[];
}) {
  return (
    <div
      className={cn(
        "mt-3 rounded-xl border p-3",
        tone === "warning"
          ? "border-amber-200 bg-amber-50 text-amber-900"
          : "border-rose-200 bg-rose-50 text-rose-900",
      )}
    >
      <p className="text-xs font-semibold">{title}</p>
      <ul className="mt-2 space-y-1 text-xs leading-5">
        {Array.from(new Set(items)).map((item) => (
          <li key={item}>· {item}</li>
        ))}
      </ul>
    </div>
  );
}

function TopologyNode({
  name,
  kind,
  selected,
  stale = false,
  conflict = false,
}: {
  name: string;
  kind: string;
  selected: boolean;
  stale?: boolean;
  conflict?: boolean;
}) {
  return (
    <div
      className={cn(
        "w-40 rounded-xl border bg-white p-3 shadow-sm",
        selected
          ? "border-violet-300 ring-2 ring-violet-100"
          : "border-slate-200",
        stale && "border-amber-300 bg-amber-50",
        conflict && "border-rose-300 bg-rose-50",
      )}
    >
      <Network className="size-4 text-violet-600" />
      <p className="mt-2 truncate text-xs font-semibold text-slate-800">
        {name}
      </p>
      <p className="mt-1 text-[11px] text-slate-500">
        {kind} {selected ? "· 已选择" : "· 候选"}
      </p>
    </div>
  );
}

function MetricBox({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="rounded-xl bg-slate-50 p-3">
      <p className="text-[11px] text-slate-500">{label}</p>
      <p
        className={cn(
          "mt-1 break-all text-sm font-semibold text-slate-900",
          mono && "font-mono text-xs",
        )}
      >
        {value}
      </p>
    </div>
  );
}

function StatusBadge({
  status,
  compact = false,
  dark = false,
}: {
  status: string;
  compact?: boolean;
  dark?: boolean;
}) {
  const normalized = status || "waiting";
  const styles: Record<string, string> = {
    success: "bg-emerald-100 text-emerald-800",
    completed: "bg-emerald-100 text-emerald-800",
    running: "bg-blue-100 text-blue-800",
    pending: "bg-blue-100 text-blue-800",
    partial_success: "bg-amber-100 text-amber-900",
    failed: "bg-rose-100 text-rose-800",
    timed_out: "bg-violet-100 text-violet-800",
    cancelled: "bg-slate-200 text-slate-700",
    skipped: "bg-slate-100 text-slate-600",
    waiting: dark
      ? "bg-white/10 text-slate-300"
      : "bg-slate-100 text-slate-500",
  };
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-full font-medium",
        compact ? "px-2 py-0.5 text-[10px]" : "px-2.5 py-1 text-xs",
        styles[normalized] ?? styles.waiting,
      )}
    >
      {statusLabel(normalized)}
    </span>
  );
}

function EvidenceKindBadge({ kind }: { kind?: string }) {
  const normalized = (kind || "fact").toLowerCase();
  const styles: Record<string, string> = {
    fact: "bg-emerald-100 text-emerald-800",
    rule: "bg-blue-100 text-blue-800",
    knowledge: "bg-violet-100 text-violet-800",
    model_hypothesis: "bg-amber-100 text-amber-900",
  };
  return (
    <span
      className={cn(
        "shrink-0 rounded px-1.5 py-0.5 text-[10px] font-bold",
        styles[normalized] ?? styles.fact,
      )}
    >
      {evidenceKindLabel(normalized)}
    </span>
  );
}

function SafetyBadge({ children }: { children: ReactNode }) {
  return (
    <span className="rounded-full border border-white/10 bg-white/5 px-3 py-1 text-xs text-slate-300">
      {children}
    </span>
  );
}

function ScopeChip({ label, value }: { label: string; value: string }) {
  return (
    <span className="rounded-full border border-white/10 bg-black/20 px-2.5 py-1 text-slate-300">
      <span className="text-slate-500">{label}</span> · {value}
    </span>
  );
}

function DarkField({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  return (
    <div>
      <Label className="text-xs text-slate-400">{label}</Label>
      <div className="mt-1.5">{children}</div>
    </div>
  );
}

function DarkInput({
  value,
  onChange,
  placeholder,
  type = "text",
  "aria-label": ariaLabel,
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  type?: string;
  "aria-label": string;
}) {
  return (
    <input
      aria-label={ariaLabel}
      type={type}
      value={value}
      placeholder={placeholder}
      className="h-11 w-full rounded-lg border border-white/15 bg-white/5 px-3 text-sm text-white outline-none placeholder:text-slate-600 focus:border-brand-300"
      onChange={(event) => onChange(event.target.value)}
    />
  );
}

function SectionLabel({
  children,
  light = false,
}: {
  children: ReactNode;
  light?: boolean;
}) {
  return (
    <p
      className={cn(
        "text-[11px] font-semibold uppercase tracking-wider",
        light ? "text-slate-400" : "text-slate-500",
      )}
    >
      {children}
    </p>
  );
}

function EmptyLine({ text }: { text: string }) {
  return (
    <p className="rounded-lg border border-dashed border-slate-200 px-3 py-2 text-xs text-slate-400">
      {text}
    </p>
  );
}

function TraceValue({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div>
      <p className="text-[10px] uppercase text-slate-500">{label}</p>
      <p className="mt-1 truncate text-slate-200">{value}</p>
    </div>
  );
}

function defaultAnalysisRange() {
  const end = new Date();
  end.setSeconds(0, 0);
  const start = new Date(end.getTime() - 30 * 60 * 1000);
  return { start: toLocalDateTime(start), end: toLocalDateTime(end) };
}

function toLocalDateTime(value: Date) {
  const offset = value.getTimezoneOffset() * 60_000;
  return new Date(value.getTime() - offset).toISOString().slice(0, 16);
}

function formatLocalInput(value: string) {
  if (!value) {
    return "未设置";
  }
  return value.replace("T", " ");
}

function initialActiveRunID(demoMode: boolean) {
  if (demoMode) {
    return demoDetail.run.id;
  }
  const params = new URLSearchParams(window.location.search);
  const candidate =
    params.get("runId") ?? window.localStorage.getItem(activeRunStorageKey);
  const value = Number(candidate);
  return Number.isSafeInteger(value) && value > 0 ? value : null;
}

function runStillActive(run: RCARun) {
  return (
    !run.finishedAt &&
    ["pending", "running", "partial_success"].includes(run.status)
  );
}

function scopeHints(form: AnalysisForm) {
  const result: string[] = [];
  if (!form.serviceName.trim()) {
    result.push(
      "服务未明确，将从自然语言问题中解析；存在多个候选时报告会标记歧义。",
    );
  }
  if (!form.environment.trim()) {
    result.push("环境未指定，数据源选择可能产生歧义。");
  }
  return result;
}

function optionalValue(value: string) {
  const normalized = value.trim();
  return normalized || undefined;
}

async function refreshRCAQueries(
  client: ReturnType<typeof useQueryClient>,
  runId: number,
) {
  await Promise.all([
    client.invalidateQueries({ queryKey: ["rca", "runs"] }),
    client.invalidateQueries({ queryKey: ["rca", "detail", runId] }),
    client.invalidateQueries({ queryKey: ["rca", "report", runId] }),
  ]);
}

function roundTitle(number: number) {
  return (
    {
      1: "多源证据采集",
      2: "拓扑引导调查",
      3: "深度根因验证",
    }[number] ?? `第 ${number} 轮`
  );
}

function roundDescription(number: number) {
  return (
    {
      1: "日志、指标、知识库并行采集",
      2: "沿相关上下游组件继续验证",
      3: "慢 SQL、锁、热点和执行计划",
    }[number] ?? ""
  );
}

function statusLabel(status: string) {
  const labels: Record<string, string> = {
    waiting: "等待",
    pending: "待执行",
    running: "运行中",
    success: "成功",
    completed: "已完成",
    partial_success: "部分成功",
    failed: "失败",
    timed_out: "超时",
    cancelled: "已取消",
    skipped: "已跳过",
  };
  return labels[status] ?? status;
}

function evidenceKindLabel(kind?: string) {
  const normalized = (kind || "fact").toLowerCase();
  return (
    {
      fact: "FACT",
      rule: "RULE",
      knowledge: "KNOWLEDGE",
      model_hypothesis: "HYPOTHESIS",
    }[normalized] ?? normalized.toUpperCase()
  );
}

function skillLabel(skill: string) {
  return skill.replaceAll("_", " ");
}

function stopReasonLabel(reason: string) {
  const labels: Record<string, string> = {
    confirmed_by_multi_source_evidence: "多源证据已达到确认阈值",
    no_new_evidence_or_actions: "没有新的证据或安全动作",
    max_rounds_reached: "已达到三轮上限",
    skill_call_budget_exhausted: "Skill Call 预算已用尽",
    token_budget_exhausted: "Token 预算已用尽",
    context_budget_exhausted: "上下文预算已用尽",
    wall_time_budget_exhausted: "运行时间预算已用尽",
    user_cancelled: "用户取消",
    critical_scope_unresolved: "关键 Scope 未解析",
    round_one_failed: "第一轮证据采集失败",
  };
  return (labels[reason] ?? reason) || "未记录";
}

function diagnosisCategoryLabel(category: string) {
  const labels: Record<string, string> = {
    slow_sql_impact: "慢 SQL 影响",
    connection_pressure: "连接压力",
    resource_pressure: "资源压力",
    lock_contention: "锁竞争",
    hot_region_pressure: "热点 Region",
    statistics_anomaly: "统计信息异常",
    plan_regression: "执行计划退化",
  };
  return labels[category] ?? category;
}

function isTopologySkill(skill: string) {
  return (
    skill.includes("topology") ||
    skill.includes("dependencies") ||
    skill === "find_topology_node"
  );
}

function topologyFromActions(
  actions: RCAAction[],
): RCATopologyInvestigation | undefined {
  const dependency = actions.find(
    (item) => item.skillName === "find_dependencies",
  );
  if (!dependency) {
    return undefined;
  }
  const output = asRecord(unwrapResult(dependency.output));
  const rootNodeKey = stringValue(output.rootKey);
  const dependencies = Array.isArray(output.dependencies)
    ? output.dependencies
    : [];
  const candidates: RCATopologyCandidate[] = dependencies.map(
    (value, index) => {
      const item = asRecord(value);
      const node = asRecord(item.node);
      return {
        nodeKey: stringValue(node.nodeKey) || `candidate-${index + 1}`,
        name: stringValue(node.name) || stringValue(node.nodeKey),
        kind: stringValue(node.kind) || "component",
        componentType: stringValue(node.kind) || "component",
        sourceType: stringValue(node.sourceType) || "topology",
        hops: numberValue(item.hops),
        edgeType: stringValue(item.dependencyType),
        direction: "downstream",
        confidence: 0,
        score: 0,
        freshness: "unknown",
        aliasMatched: false,
        conflict: false,
        selected: true,
        topologyEvidenceIds: dependency.evidenceIds,
      };
    },
  );
  return {
    rootNodeKey,
    observedAliases: [],
    candidates,
    selected: candidates,
    missingEvidence: dependency.errorCode ? [dependency.errorCode] : [],
    conflicts: [],
    fallbackUsed: false,
  };
}

function unwrapResult(value: unknown): unknown {
  const record = asRecord(value);
  return record.result ?? value;
}

function safeEvidencePreview(item: RCAEvidence) {
  if (
    item.sourceType.toLowerCase() === "tidb" ||
    item.sourceSkill?.toLowerCase().includes("tidb")
  ) {
    const fingerprint = findFirstString(
      [item.content],
      ["sql_fingerprint", "digest"],
    );
    const query = findFirstString([item.content], ["query", "sql"]);
    return JSON.stringify(
      {
        fingerprint: fingerprint || undefined,
        sanitizedSql: query ? sanitizeSQLForDisplay(query) : undefined,
        notice: "TiDB Evidence 仅展示指纹与脱敏 SQL 结构。",
      },
      null,
      2,
    );
  }
  return JSON.stringify(redactSensitiveObject(item.content), null, 2);
}

function sanitizeSQLForDisplay(value: string) {
  return value
    .replace(/\/\*[\s\S]*?\*\//g, " ")
    .replace(/--[^\r\n]*/g, " ")
    .replace(/'(?:''|\\.|[^'])*'|"(?:""|\\.|[^"])*"/g, "?")
    .replace(/\b\d+(?:\.\d+)?\b/g, "?")
    .replace(/\s+/g, " ")
    .trim()
    .slice(0, 2048);
}

function redactSensitiveObject(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map(redactSensitiveObject);
  }
  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value).map(([key, nested]) => [
        key,
        /(password|passwd|pwd|secret|token|authorization|cookie|credential|api.?key|username|account|email|phone|mobile)/i.test(
          key,
        )
          ? "[REDACTED]"
          : redactSensitiveObject(nested),
      ]),
    );
  }
  if (typeof value === "string") {
    return value.replace(
      /(password|passwd|pwd|token|secret|authorization|api.?key)(\s*[:=]\s*)[^\s,;]+/gi,
      "$1$2[REDACTED]",
    );
  }
  return value;
}

function findFirstString(values: unknown[], keys: string[]): string {
  for (const value of values) {
    const found = findString(
      value,
      new Set(keys.map((key) => key.toLowerCase())),
    );
    if (found) {
      return found;
    }
  }
  return "";
}

function findString(value: unknown, keys: Set<string>): string {
  if (Array.isArray(value)) {
    for (const item of value) {
      const found = findString(item, keys);
      if (found) {
        return found;
      }
    }
    return "";
  }
  if (value && typeof value === "object") {
    for (const [key, nested] of Object.entries(value)) {
      if (keys.has(key.toLowerCase()) && typeof nested === "string") {
        return nested;
      }
    }
    for (const nested of Object.values(value)) {
      const found = findString(nested, keys);
      if (found) {
        return found;
      }
    }
  }
  return "";
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function stringValue(value: unknown) {
  return typeof value === "string" ? value : "";
}

function numberValue(value: unknown) {
  return typeof value === "number" ? value : 0;
}

const demoRun: RCARun = {
  id: 31204,
  userId: 1,
  workflowRunId: 8801,
  status: "partial_success",
  query: "订单服务变慢，请查询可能原因",
  scope: {
    serviceName: "order-service",
    environment: "prod",
    from: "2026-07-28T05:30:00Z",
    to: "2026-07-28T06:00:00Z",
  },
  currentRound: 3,
  maxRounds: 3,
  stopReason: "max_rounds_reached",
  startedAt: "2026-07-28T05:31:00Z",
  finishedAt: "2026-07-28T05:34:20Z",
  createdAt: "2026-07-28T05:30:58Z",
  updatedAt: "2026-07-28T05:34:20Z",
};

const demoEvidence: RCAEvidence[] = [
  {
    id: 401,
    evidenceKey: "rca_31204_round1_logs",
    sourceType: "elasticsearch",
    summary: "订单服务数据库调用超时在当前窗口升至基线的 6.4 倍",
    content: { timeoutCount: 326, baselineCount: 51 },
    evidenceKind: "fact",
    sourceSkill: "query_logs",
    rcaRunId: 31204,
    rcaRoundId: 1,
    rcaActionId: 1,
    createdAt: "2026-07-28T05:31:20Z",
  },
  {
    id: 402,
    evidenceKey: "rca_31204_round1_metrics",
    sourceType: "prometheus",
    summary: "order-service P95 延迟与数据库等待时间同步升高",
    content: { p95Ms: 2380, baselineP95Ms: 410 },
    evidenceKind: "fact",
    sourceSkill: "compare_metric_baseline",
    rcaRunId: 31204,
    rcaRoundId: 1,
    rcaActionId: 2,
    createdAt: "2026-07-28T05:31:22Z",
  },
  {
    id: 403,
    evidenceKey: "rca_31204_round1_knowledge",
    sourceType: "knowledge",
    summary: "历史 Incident 指出连接长时间占用通常需要继续检查慢 SQL 与锁等待",
    content: { document: "数据库连接池调优与故障处置规范 v2.1" },
    evidenceKind: "knowledge",
    sourceSkill: "hybrid_search_knowledge",
    rcaRunId: 31204,
    rcaRoundId: 1,
    rcaActionId: 3,
    createdAt: "2026-07-28T05:31:25Z",
  },
  {
    id: 404,
    evidenceKey: "rca_31204_round2_topology",
    sourceType: "topology",
    summary: "order-service 的可靠下游依赖 order-db 已绑定只读 TiDB 数据源",
    content: {
      rootKey: "service:order-service",
      dependency: "database:order-db",
    },
    evidenceKind: "rule",
    sourceSkill: "find_dependencies",
    rcaRunId: 31204,
    rcaRoundId: 2,
    rcaActionId: 4,
    createdAt: "2026-07-28T05:32:10Z",
  },
  {
    id: 405,
    evidenceKey: "rca_31204_round3_slow_sql",
    sourceType: "tidb",
    summary: "最高影响慢 SQL 指纹累计数据库时间 84.5 秒",
    content: {
      digest: "a49c8d2f7e13",
      query: "SELECT * FROM orders WHERE customer_id=? AND status=?",
      total_query_time: 84.5,
      execution_count: 120,
    },
    evidenceKind: "fact",
    sourceSkill: "query_tidb_slow_queries",
    rcaRunId: 31204,
    rcaRoundId: 3,
    rcaActionId: 5,
    createdAt: "2026-07-28T05:33:10Z",
  },
  {
    id: 406,
    evidenceKey: "rca_31204_round3_locks",
    sourceType: "tidb",
    summary: "同一时间窗存在持续锁等待，覆盖最高影响慢 SQL 指纹",
    content: { waitingTransactions: 8, blockingTransactions: 2 },
    evidenceKind: "rule",
    sourceSkill: "query_tidb_lock_waits",
    rcaRunId: 31204,
    rcaRoundId: 3,
    rcaActionId: 6,
    createdAt: "2026-07-28T05:33:15Z",
  },
];

const demoDetail: RCADetail = {
  run: demoRun,
  rounds: [
    {
      id: 1,
      runId: 31204,
      roundNumber: 1,
      status: "success",
      inputHypotheses: [],
      newEvidenceIds: [401, 402, 403],
      rejectedHypotheses: [],
      nextActions: [
        { actionKey: "topology", skillName: "find_dependencies", input: {} },
      ],
      startedAt: "2026-07-28T05:31:00Z",
      finishedAt: "2026-07-28T05:31:30Z",
    },
    {
      id: 2,
      runId: 31204,
      roundNumber: 2,
      status: "success",
      inputHypotheses: [
        {
          id: "database-latency",
          summary: "数据库依赖是服务变慢的主要方向",
          confidence: 0.72,
          evidenceIds: [401, 402],
        },
      ],
      newEvidenceIds: [404],
      rejectedHypotheses: [],
      nextActions: [
        {
          actionKey: "slow-sql",
          skillName: "query_tidb_slow_queries",
          input: {},
        },
      ],
      startedAt: "2026-07-28T05:32:00Z",
      finishedAt: "2026-07-28T05:32:35Z",
    },
    {
      id: 3,
      runId: 31204,
      roundNumber: 3,
      status: "partial_success",
      inputHypotheses: [
        {
          id: "slow-sql-lock",
          summary: "慢 SQL 与锁等待共同放大请求排队",
          confidence: 0.86,
          evidenceIds: [405, 406],
        },
      ],
      newEvidenceIds: [405, 406],
      rejectedHypotheses: [
        {
          id: "cpu",
          summary: "订单服务 CPU 饱和",
          confidence: 0.18,
          evidenceIds: [402],
        },
      ],
      nextActions: [],
      errorCode: "partial_evidence",
      startedAt: "2026-07-28T05:33:00Z",
      finishedAt: "2026-07-28T05:34:10Z",
    },
  ],
  actions: [
    demoAction(1, 1, "query_logs", "success", [401]),
    demoAction(2, 1, "compare_metric_baseline", "success", [402]),
    demoAction(3, 1, "hybrid_search_knowledge", "success", [403]),
    {
      ...demoAction(4, 2, "find_dependencies", "success", [404]),
      output: {
        rootKey: "service:order-service",
        dependencies: [
          {
            node: {
              nodeKey: "database:order-db",
              name: "order-db",
              kind: "database",
              sourceType: "manual",
            },
            dependencyType: "depends_on",
            hops: 1,
          },
        ],
      },
    },
    demoAction(5, 3, "query_tidb_slow_queries", "success", [405]),
    demoAction(6, 3, "query_tidb_lock_waits", "success", [406]),
    {
      ...demoAction(7, 3, "explain_tidb_sql", "partial_success", []),
      errorCode: "upstream_unavailable",
    },
  ],
  evidence: demoEvidence,
  rootCauseCandidates: [
    {
      id: 1,
      runId: 31204,
      roundId: 3,
      summary: "慢 SQL 与锁等待共同放大订单请求排队",
      confidence: 0.86,
      evidenceIds: [401, 402, 405, 406],
      rejected: false,
    },
  ],
};

const demoTopology: RCATopologyInvestigation = {
  rootNodeKey: "service:order-service",
  observedAliases: ["order-db"],
  candidates: [
    {
      nodeKey: "database:order-db",
      name: "order-db",
      kind: "database",
      componentType: "tidb",
      sourceType: "manual",
      hops: 1,
      edgeType: "depends_on",
      direction: "downstream",
      confidence: 0.94,
      score: 0.92,
      freshness: "fresh",
      aliasMatched: true,
      conflict: false,
      selected: true,
      dataSourceId: 7,
      bindingStatus: "accepted",
      topologyEvidenceIds: [404],
    },
    {
      nodeKey: "redis:order-cache",
      name: "order-cache",
      kind: "redis",
      componentType: "redis",
      sourceType: "kubernetes",
      hops: 1,
      edgeType: "calls",
      direction: "downstream",
      confidence: 0.72,
      score: 0.53,
      freshness: "fresh",
      aliasMatched: false,
      conflict: false,
      selected: false,
      topologyEvidenceIds: [404],
    },
  ],
  selected: [],
  missingEvidence: [],
  conflicts: [],
  fallbackUsed: false,
};
demoTopology.selected = [demoTopology.candidates[0]];

const demoDatabase: RCADatabaseDiagnosis = {
  provider: "tidb",
  sourceType: "tidb",
  dataSourceId: 7,
  serviceName: "order-service",
  environment: "prod",
  windowMinutes: 30,
  correlationDimensions: [
    "service",
    "time_window",
    "trace",
    "call_volume",
    "baseline",
  ],
  sqlFingerprint: "sha256:a49c8d2f7e13",
  sanitizedSql: "SELECT * FROM orders WHERE customer_id=? AND status=?",
  missingEvidence: [],
  supportingEvidenceIds: [401, 402, 404],
  assessment: {
    status: "partial_success",
    highestImpactFingerprint: "a49c8d2f7e13",
    categories: [
      {
        category: "slow_sql_impact",
        sourceSkill: "query_tidb_slow_queries",
        collected: true,
        evidenceIds: [405],
      },
      {
        category: "lock_contention",
        sourceSkill: "query_tidb_lock_waits",
        collected: true,
        evidenceIds: [406],
      },
      {
        category: "plan_regression",
        sourceSkill: "explain_tidb_sql",
        collected: false,
        evidenceIds: [],
      },
    ],
    evidenceIds: [405, 406],
    missingEvidence: ["execution plan evidence; index failure is not asserted"],
    rootCauseEligible: true,
    confidence: "medium",
    conclusion: "慢 SQL 与锁等待是具备多源证据的 contributing factor。",
  },
};

const demoReport: RCAReport = {
  version: "rca-report-v1",
  runId: 31204,
  status: "partial_success",
  query: demoRun.query,
  scope: demoRun.scope,
  impactScope: {
    serviceName: "order-service",
    environment: "prod",
    windowStart: "2026-07-28T05:30:00Z",
    windowEnd: "2026-07-28T06:00:00Z",
    entities: ["order-service", "order-db"],
  },
  evidence: {
    facts: [401, 402, 405].map((id) => demoReportEvidence(id)),
    rules: [404, 406].map((id) => demoReportEvidence(id)),
    knowledge: [403].map((id) => demoReportEvidence(id)),
    hypotheses: [],
  },
  rootCauseCandidates: [
    {
      id: 1,
      summary: "慢 SQL 与锁等待共同放大订单请求排队",
      status: "candidate",
      confidence: 0.86,
      evidenceStrength: "strong",
      evidenceStrengthScore: 0.91,
      supportingEvidence: [401, 402, 405, 406].map((id) =>
        demoReportEvidence(id),
      ),
      contradictingEvidence: [],
    },
  ],
  rejectedHypotheses: [
    {
      id: "cpu",
      summary: "订单服务 CPU 饱和",
      confidence: 0.18,
      evidence: [demoReportEvidence(402)],
      status: "rejected",
    },
  ],
  investigation: [
    {
      roundNumber: 1,
      status: "success",
      checked: [],
      findings: [401, 402, 403].map((id) => demoReportEvidence(id)),
      continueReason: "日志和指标共同指向数据库依赖，需要沿拓扑继续调查。",
    },
    {
      roundNumber: 2,
      status: "success",
      checked: [],
      findings: [demoReportEvidence(404)],
      continueReason: "order-db 已确认，需要验证慢 SQL、锁和执行计划。",
    },
    {
      roundNumber: 3,
      status: "partial_success",
      checked: [],
      findings: [405, 406].map((id) => demoReportEvidence(id)),
      stopReason: "max_rounds_reached",
    },
  ],
  missingEvidence: ["execution plan evidence; index failure is not asserted"],
  incomplete: true,
  conclusion:
    "当前最高优先级根因候选：慢 SQL 与锁等待共同放大订单请求排队（置信度 86%，证据强度：strong）。该结论仍是候选，需人工确认。",
  suggestions: [
    {
      summary:
        "由数据库负责人复核慢 SQL 指纹、锁等待和脱敏执行计划后再制定优化方案。",
      evidenceIds: [405, 406],
      advisoryOnly: true,
      autoExecute: false,
    },
  ],
  riskNotices: ["报告包含缺失执行计划证据，不断言索引失效。"],
  stopReason: "max_rounds_reached",
};

const demoOrchestratorResult: RCAOrchestratorResult = {
  version: "rca-orchestrator-v1",
  run: demoRun,
  report: demoReport,
  stopReason: "max_rounds_reached",
  degraded: true,
  rounds: [
    {
      roundNumber: 1,
      status: "success",
      actionIds: [1, 2, 3],
      evidenceIds: [401, 402, 403],
    },
    {
      roundNumber: 2,
      status: "success",
      topologyInvestigation: demoTopology,
      actionIds: [4],
      evidenceIds: [404],
    },
    {
      roundNumber: 3,
      status: "partial_success",
      databaseDiagnosis: demoDatabase,
      actionIds: [5, 6, 7],
      evidenceIds: [405, 406],
      errors: ["explain_tidb_sql:upstream_unavailable"],
    },
  ],
};

const demoHistory: RCARun[] = [
  demoRun,
  {
    ...demoRun,
    id: 31203,
    status: "success",
    query: "支付服务 5xx 突增的原因是什么？",
    stopReason: "confirmed_by_multi_source_evidence",
  },
];

function demoAction(
  id: number,
  roundId: number,
  skillName: string,
  status: string,
  evidenceIds: number[],
): RCAAction {
  return {
    id,
    runId: 31204,
    roundId,
    actionKey: `demo-${skillName}`,
    skillName,
    status,
    input: {},
    evidenceIds,
    sensitiveRead: true,
    attempt: 1,
  };
}

function demoReportEvidence(id: number) {
  const item = demoEvidence.find((evidence) => evidence.id === id)!;
  return {
    id: item.id,
    evidenceKey: item.evidenceKey,
    kind: item.evidenceKind ?? "fact",
    sourceType: item.sourceType,
    sourceSkill: item.sourceSkill,
    summary: item.summary,
    confidence: item.confidence,
    roundId: item.rcaRoundId,
    actionId: item.rcaActionId,
    url: `/api/evidence/${item.id}`,
  };
}
