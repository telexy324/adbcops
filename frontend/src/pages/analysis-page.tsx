import { FormEvent, ReactNode, useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  Activity,
  AlertTriangle,
  BellRing,
  Braces,
  FileSearch,
  Loader2,
  Logs,
  Network,
  Quote,
  ServerCog,
  Sparkles,
} from "lucide-react";

import {
  diagnosePod,
  listAnalysisTasks,
  queryMetrics,
  runGeneralAnalysis,
  sendAlertmanagerWebhook,
  toAPIErrorMessage,
  type AlertmanagerResponse,
  type CitationItem,
  type EvidenceItem,
  type GeneralAnalysisResponse,
  type K8sRuleFinding,
  type MetricQueryResponse,
  type PodDiagnosisResponse,
} from "@/api/analysis";
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

const defaultAlertJSON = JSON.stringify(
  {
    receiver: "default",
    status: "firing",
    alerts: [
      {
        status: "firing",
        labels: {
          alertname: "HighErrorRate",
          severity: "critical",
          environment: "prod",
          system: "payment",
          service: "payment-api",
          namespace: "prod",
          pod: "payment-api-0",
        },
        annotations: {
          summary: "payment api error rate is high",
        },
        startsAt: "2026-07-12T10:00:00Z",
        fingerprint: "demo-fingerprint",
      },
    ],
  },
  null,
  2,
);

export function AnalysisPage() {
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [evidence, setEvidence] = useState<EvidenceItem[]>([]);
  const [citations, setCitations] = useState<CitationItem[]>([]);
  const [rules, setRules] = useState<K8sRuleFinding[]>([]);
  const [generalResult, setGeneralResult] =
    useState<GeneralAnalysisResponse | null>(null);
  const [podResult, setPodResult] = useState<PodDiagnosisResponse | null>(null);
  const [metricsResult, setMetricsResult] =
    useState<MetricQueryResponse | null>(null);
  const [alertResult, setAlertResult] = useState<AlertmanagerResponse | null>(
    null,
  );
  const [logForm, setLogForm] = useState({
    question: "支付接口 9 点后超时增多，可能是什么原因？",
    dataSourceIds: "1",
    environment: "prod",
    systemName: "payment",
    componentName: "payment-api",
  });
  const [k8sForm, setK8sForm] = useState({
    dataSourceId: "1",
    namespace: "prod",
    podName: "payment-api-0",
    logTailLines: "200",
    logMaxBytes: "65536",
    includePreviousLogs: true,
    includeNode: true,
  });
  const [metricsForm, setMetricsForm] = useState({
    dataSourceId: "1",
    query: "rate(http_requests_total[5m])",
    range: true,
    start: "2026-07-12T10:00:00+08:00",
    end: "2026-07-12T11:00:00+08:00",
    stepSeconds: "60",
    maxSeries: "20",
    maxPoints: "500",
  });
  const [alertJSON, setAlertJSON] = useState(defaultAlertJSON);

  const tasksQuery = useQuery({
    queryKey: ["analysis", "tasks"],
    queryFn: listAnalysisTasks,
  });

  const sortedTasks = useMemo(
    () => (tasksQuery.data ?? []).slice(0, 6),
    [tasksQuery.data],
  );

  const generalMutation = useMutation({
    mutationFn: runGeneralAnalysis,
    onSuccess: (response) => {
      setGeneralResult(response);
      setEvidence(response.evidence ?? []);
      setCitations(response.citations ?? []);
      setNotice("日志分析完成，已刷新证据和引用面板。");
      setError(null);
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const podMutation = useMutation({
    mutationFn: diagnosePod,
    onSuccess: (response) => {
      setPodResult(response);
      setRules(response.rules ?? []);
      setNotice("K8s 诊断完成，规则判断已更新。");
      setError(null);
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const metricsMutation = useMutation({
    mutationFn: queryMetrics,
    onSuccess: (response) => {
      setMetricsResult(response);
      setNotice("指标查询完成。");
      setError(null);
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const alertMutation = useMutation({
    mutationFn: sendAlertmanagerWebhook,
    onSuccess: (response) => {
      setAlertResult(response);
      setNotice("告警已写入统一事件，重复 fingerprint 会自动归并。");
      setError(null);
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  function submitGeneral(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    generalMutation.mutate({
      question: logForm.question,
      dataSourceIds: parseIDs(logForm.dataSourceIds),
      scope: {
        environment: logForm.environment,
        systemName: logForm.systemName,
        componentName: logForm.componentName,
      },
    });
  }

  function submitK8s(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    podMutation.mutate({
      dataSourceId: Number(k8sForm.dataSourceId),
      namespace: k8sForm.namespace,
      podName: k8sForm.podName,
      includeNode: k8sForm.includeNode,
      includePreviousLogs: k8sForm.includePreviousLogs,
      logTailLines: Number(k8sForm.logTailLines),
      logMaxBytes: Number(k8sForm.logMaxBytes),
    });
  }

  function submitMetrics(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    metricsMutation.mutate({
      dataSourceId: Number(metricsForm.dataSourceId),
      query: metricsForm.query,
      range: metricsForm.range,
      start: metricsForm.range ? metricsForm.start : undefined,
      end: metricsForm.range ? metricsForm.end : undefined,
      stepSeconds: Number(metricsForm.stepSeconds),
      maxSeries: Number(metricsForm.maxSeries),
      maxPoints: Number(metricsForm.maxPoints),
    });
  }

  function submitAlert(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    try {
      alertMutation.mutate(JSON.parse(alertJSON));
    } catch {
      setError("Alertmanager JSON 格式不正确。");
    }
  }

  return (
    <div className="mx-auto max-w-[1700px] space-y-6">
      <section className="flex flex-col justify-between gap-4 xl:flex-row xl:items-end">
        <div>
          <p className="text-sm font-medium text-cyan-700">Analysis Center</p>
          <h1 className="mt-1 text-3xl font-semibold tracking-tight text-slate-950">
            智能分析
          </h1>
          <p className="mt-2 max-w-3xl text-sm leading-6 text-slate-500">
            面向日志、Kubernetes、指标和告警的只读分析入口。任务列表调用后端“我的任务”接口，普通用户只会看到自己的分析任务。
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <StatusPill label="日志分析" />
          <StatusPill label="K8s 诊断" />
          <StatusPill label="指标查询" />
          <StatusPill label="告警输入" />
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

      <section className="grid gap-6 2xl:grid-cols-[1.15fr_0.85fr]">
        <div className="grid gap-6 xl:grid-cols-2">
          <Card className="border-slate-200/80 shadow-none">
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Logs className="size-5 text-cyan-600" />
                日志分析
              </CardTitle>
              <CardDescription>触发后端 general analysis，产出证据与引用。</CardDescription>
            </CardHeader>
            <CardContent>
              <form className="space-y-3" onSubmit={submitGeneral}>
                <Field label="问题">
                  <Textarea
                    value={logForm.question}
                    onChange={(value) =>
                      setLogForm((current) => ({ ...current, question: value }))
                    }
                  />
                </Field>
                <Field label="数据源 ID，逗号分隔">
                  <Input
                    value={logForm.dataSourceIds}
                    onChange={(event) =>
                      setLogForm((current) => ({
                        ...current,
                        dataSourceIds: event.target.value,
                      }))
                    }
                  />
                </Field>
                <div className="grid gap-3 sm:grid-cols-3">
                  <Field label="环境">
                    <Input
                      value={logForm.environment}
                      onChange={(event) =>
                        setLogForm((current) => ({
                          ...current,
                          environment: event.target.value,
                        }))
                      }
                    />
                  </Field>
                  <Field label="系统">
                    <Input
                      value={logForm.systemName}
                      onChange={(event) =>
                        setLogForm((current) => ({
                          ...current,
                          systemName: event.target.value,
                        }))
                      }
                    />
                  </Field>
                  <Field label="组件">
                    <Input
                      value={logForm.componentName}
                      onChange={(event) =>
                        setLogForm((current) => ({
                          ...current,
                          componentName: event.target.value,
                        }))
                      }
                    />
                  </Field>
                </div>
                <SubmitButton
                  pending={generalMutation.isPending}
                  label="运行日志分析"
                />
              </form>
            </CardContent>
          </Card>

          <Card className="border-slate-200/80 shadow-none">
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <ServerCog className="size-5 text-cyan-600" />
                K8s 诊断
              </CardTitle>
              <CardDescription>采集 Pod 上下文并展示规则判断。</CardDescription>
            </CardHeader>
            <CardContent>
              <form className="space-y-3" onSubmit={submitK8s}>
                <div className="grid gap-3 sm:grid-cols-3">
                  <Field label="数据源 ID">
                    <Input
                      value={k8sForm.dataSourceId}
                      onChange={(event) =>
                        setK8sForm((current) => ({
                          ...current,
                          dataSourceId: event.target.value,
                        }))
                      }
                    />
                  </Field>
                  <Field label="Namespace">
                    <Input
                      value={k8sForm.namespace}
                      onChange={(event) =>
                        setK8sForm((current) => ({
                          ...current,
                          namespace: event.target.value,
                        }))
                      }
                    />
                  </Field>
                  <Field label="Pod">
                    <Input
                      value={k8sForm.podName}
                      onChange={(event) =>
                        setK8sForm((current) => ({
                          ...current,
                          podName: event.target.value,
                        }))
                      }
                    />
                  </Field>
                </div>
                <div className="grid gap-3 sm:grid-cols-2">
                  <Field label="日志行数">
                    <Input
                      value={k8sForm.logTailLines}
                      onChange={(event) =>
                        setK8sForm((current) => ({
                          ...current,
                          logTailLines: event.target.value,
                        }))
                      }
                    />
                  </Field>
                  <Field label="日志字节">
                    <Input
                      value={k8sForm.logMaxBytes}
                      onChange={(event) =>
                        setK8sForm((current) => ({
                          ...current,
                          logMaxBytes: event.target.value,
                        }))
                      }
                    />
                  </Field>
                </div>
                <div className="flex flex-wrap gap-3 text-sm text-slate-600">
                  <Checkbox
                    checked={k8sForm.includePreviousLogs}
                    label="previous logs"
                    onChange={(value) =>
                      setK8sForm((current) => ({
                        ...current,
                        includePreviousLogs: value,
                      }))
                    }
                  />
                  <Checkbox
                    checked={k8sForm.includeNode}
                    label="包含 Node"
                    onChange={(value) =>
                      setK8sForm((current) => ({ ...current, includeNode: value }))
                    }
                  />
                </div>
                <SubmitButton pending={podMutation.isPending} label="诊断 Pod" />
              </form>
            </CardContent>
          </Card>

          <Card className="border-slate-200/80 shadow-none">
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Activity className="size-5 text-cyan-600" />
                指标查询
              </CardTitle>
              <CardDescription>Prometheus instant/range 查询统一展示。</CardDescription>
            </CardHeader>
            <CardContent>
              <form className="space-y-3" onSubmit={submitMetrics}>
                <div className="grid gap-3 sm:grid-cols-[0.4fr_1fr]">
                  <Field label="数据源 ID">
                    <Input
                      value={metricsForm.dataSourceId}
                      onChange={(event) =>
                        setMetricsForm((current) => ({
                          ...current,
                          dataSourceId: event.target.value,
                        }))
                      }
                    />
                  </Field>
                  <Field label="PromQL">
                    <Input
                      value={metricsForm.query}
                      onChange={(event) =>
                        setMetricsForm((current) => ({
                          ...current,
                          query: event.target.value,
                        }))
                      }
                    />
                  </Field>
                </div>
                <div className="grid gap-3 sm:grid-cols-2">
                  <Field label="开始">
                    <Input
                      value={metricsForm.start}
                      onChange={(event) =>
                        setMetricsForm((current) => ({
                          ...current,
                          start: event.target.value,
                        }))
                      }
                    />
                  </Field>
                  <Field label="结束">
                    <Input
                      value={metricsForm.end}
                      onChange={(event) =>
                        setMetricsForm((current) => ({
                          ...current,
                          end: event.target.value,
                        }))
                      }
                    />
                  </Field>
                </div>
                <div className="grid gap-3 sm:grid-cols-3">
                  <Field label="Step 秒">
                    <Input
                      value={metricsForm.stepSeconds}
                      onChange={(event) =>
                        setMetricsForm((current) => ({
                          ...current,
                          stepSeconds: event.target.value,
                        }))
                      }
                    />
                  </Field>
                  <Field label="Max Series">
                    <Input
                      value={metricsForm.maxSeries}
                      onChange={(event) =>
                        setMetricsForm((current) => ({
                          ...current,
                          maxSeries: event.target.value,
                        }))
                      }
                    />
                  </Field>
                  <Field label="Max Points">
                    <Input
                      value={metricsForm.maxPoints}
                      onChange={(event) =>
                        setMetricsForm((current) => ({
                          ...current,
                          maxPoints: event.target.value,
                        }))
                      }
                    />
                  </Field>
                </div>
                <Checkbox
                  checked={metricsForm.range}
                  label="Range query"
                  onChange={(value) =>
                    setMetricsForm((current) => ({ ...current, range: value }))
                  }
                />
                <SubmitButton
                  pending={metricsMutation.isPending}
                  label="查询指标"
                />
              </form>
            </CardContent>
          </Card>

          <Card className="border-slate-200/80 shadow-none">
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <BellRing className="size-5 text-cyan-600" />
                告警输入
              </CardTitle>
              <CardDescription>模拟 Alertmanager Webhook，写入统一事件。</CardDescription>
            </CardHeader>
            <CardContent>
              <form className="space-y-3" onSubmit={submitAlert}>
                <Field label="Alertmanager JSON">
                  <Textarea value={alertJSON} rows={12} onChange={setAlertJSON} />
                </Field>
                <SubmitButton
                  pending={alertMutation.isPending}
                  label="提交告警"
                />
              </form>
            </CardContent>
          </Card>
        </div>

        <div className="space-y-6">
          <Card className="border-slate-200/80 shadow-none">
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <FileSearch className="size-5 text-cyan-600" />
                证据面板
              </CardTitle>
              <CardDescription>展示日志分析事实和可观察证据。</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              {evidence.length === 0 ? (
                <EmptyState text="运行日志分析后会展示 evidence。" />
              ) : (
                evidence.map((item, index) => (
                  <PanelItem
                    key={`${item.sourceType}-${index}`}
                    title={item.summary ?? `Evidence #${index + 1}`}
                    meta={`${item.sourceType ?? "unknown"} · ${item.confidence ?? "--"}`}
                  />
                ))
              )}
            </CardContent>
          </Card>

          <Card className="border-slate-200/80 shadow-none">
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Quote className="size-5 text-cyan-600" />
                引用与规则
              </CardTitle>
              <CardDescription>展示 RAG 引用和 K8s 规则 evidence keys。</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              {citations.length === 0 && rules.length === 0 ? (
                <EmptyState text="暂无引用或规则判断。" />
              ) : (
                <>
                  {citations.map((item, index) => (
                    <PanelItem
                      key={`citation-${index}`}
                      title={item.sourceTitle ?? `Citation #${index + 1}`}
                      meta={item.snippet ?? "无摘要"}
                    />
                  ))}
                  {rules.map((rule) => (
                    <PanelItem
                      key={rule.id}
                      title={`${rule.severity.toUpperCase()} · ${rule.title}`}
                      meta={rule.evidenceKeys.join(", ")}
                    />
                  ))}
                </>
              )}
            </CardContent>
          </Card>

          <Card className="border-slate-200/80 shadow-none">
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Braces className="size-5 text-cyan-600" />
                最近结果
              </CardTitle>
              <CardDescription>用于快速核对接口返回。</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <ResultPreview
                title="日志分析"
                value={generalResult?.summary ?? generalResult}
              />
              <ResultPreview
                title="K8s"
                value={podResult && `${podResult.pod.name} · ${podResult.pod.phase}`}
              />
              <ResultPreview
                title="指标"
                value={
                  metricsResult &&
                  `${metricsResult.series.length} series · limit ${metricsResult.limit.maxSeries}/${metricsResult.limit.maxPoints}`
                }
              />
              <ResultPreview
                title="告警"
                value={
                  alertResult &&
                  `${alertResult.received} alerts · ${alertResult.events[0]?.status ?? "-"}`
                }
              />
            </CardContent>
          </Card>

          <Card className="border-slate-200/80 shadow-none">
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Network className="size-5 text-cyan-600" />
                我的分析任务
              </CardTitle>
              <CardDescription>后端按当前用户过滤；管理员可查看全部。</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              {tasksQuery.isLoading ? (
                <EmptyState text="正在加载任务..." />
              ) : sortedTasks.length === 0 ? (
                <EmptyState text="暂无分析任务。" />
              ) : (
                sortedTasks.map((task) => (
                  <PanelItem
                    key={task.id}
                    title={`#${task.id} ${task.question}`}
                    meta={`${task.taskType} · ${task.status} · user ${task.userId}`}
                  />
                ))
              )}
            </CardContent>
          </Card>
        </div>
      </section>
    </div>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block space-y-1.5">
      <Label className="text-xs text-slate-500">{label}</Label>
      {children}
    </label>
  );
}

function Textarea({
  value,
  onChange,
  rows = 4,
}: {
  value: string;
  onChange: (value: string) => void;
  rows?: number;
}) {
  return (
    <textarea
      value={value}
      rows={rows}
      onChange={(event) => onChange(event.target.value)}
      className="w-full rounded-md border border-slate-200 bg-white px-3 py-2 text-sm text-slate-900 shadow-sm outline-none transition-colors placeholder:text-slate-400 focus:border-cyan-500 focus:ring-2 focus:ring-cyan-500/20"
    />
  );
}

function Checkbox({
  checked,
  label,
  onChange,
}: {
  checked: boolean;
  label: string;
  onChange: (checked: boolean) => void;
}) {
  return (
    <label className="inline-flex items-center gap-2 text-sm text-slate-600">
      <input
        type="checkbox"
        checked={checked}
        onChange={(event) => onChange(event.target.checked)}
        className="size-4 rounded border-slate-300 text-cyan-600"
      />
      {label}
    </label>
  );
}

function SubmitButton({ pending, label }: { pending: boolean; label: string }) {
  return (
    <Button className="w-full gap-2" disabled={pending}>
      {pending ? (
        <Loader2 className="size-4 animate-spin" />
      ) : (
        <Sparkles className="size-4" />
      )}
      {label}
    </Button>
  );
}

function StatusPill({ label }: { label: string }) {
  return (
    <span className="rounded-full border border-cyan-200 bg-cyan-50 px-3 py-1 text-xs font-medium text-cyan-700">
      {label}
    </span>
  );
}

function PanelItem({ title, meta }: { title: string; meta: string }) {
  return (
    <div className="rounded-xl border border-slate-200 bg-white p-3">
      <p className="text-sm font-medium text-slate-800">{title}</p>
      <p className="mt-1 line-clamp-2 text-xs leading-5 text-slate-500">{meta}</p>
    </div>
  );
}

function EmptyState({ text }: { text: string }) {
  return (
    <div className="rounded-xl border border-dashed border-slate-200 bg-slate-50 p-4 text-center text-sm text-slate-400">
      <AlertTriangle className="mx-auto mb-2 size-4" />
      {text}
    </div>
  );
}

function ResultPreview({ title, value }: { title: string; value: unknown }) {
  return (
    <div className="rounded-lg bg-slate-50 px-3 py-2">
      <p className="text-xs font-medium text-slate-500">{title}</p>
      <p className="mt-1 truncate text-sm text-slate-800">
        {typeof value === "string" ? value : value ? JSON.stringify(value) : "暂无"}
      </p>
    </div>
  );
}

function parseIDs(value: string) {
  return value
    .split(",")
    .map((item) => Number(item.trim()))
    .filter((item) => Number.isFinite(item) && item > 0);
}
