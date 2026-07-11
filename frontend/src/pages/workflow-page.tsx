import { FormEvent, ReactNode, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  CheckCircle2,
  GitBranch,
  Loader2,
  PlayCircle,
  RefreshCw,
  Workflow,
} from "lucide-react";

import {
  createWorkflow,
  listWorkflowRuns,
  listWorkflows,
  runWorkflow,
  toAPIErrorMessage,
  validateWorkflow,
  type WorkflowDefinition,
  type WorkflowNodeRun,
  type WorkflowRecord,
  type WorkflowRun,
  type WorkflowValidationResult,
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

const defaultDefinition: WorkflowDefinition = {
  name: "frontend_simple_dag",
  version: "v1",
  description: "前端创建的简单 DAG 示例",
  nodes: [
    { id: "start", type: "start", name: "Start" },
    {
      id: "knowledge",
      type: "agent",
      name: "Knowledge Agent",
      agentName: "knowledge_agent",
      config: {
        context: {
          query: "如何排查接口超时？",
        },
      },
    },
    { id: "end", type: "end", name: "End" },
  ],
  edges: [
    { from: "start", to: "knowledge" },
    { from: "knowledge", to: "end" },
  ],
};

const sampleRunInput = JSON.stringify(
  {
    query: "如何排查接口超时？",
  },
  null,
  2,
);

export function WorkflowPage() {
  const queryClient = useQueryClient();
  const [form, setForm] = useState({
    name: defaultDefinition.name,
    version: defaultDefinition.version,
    description: defaultDefinition.description ?? "",
  });
  const [definitionText, setDefinitionText] = useState(
    JSON.stringify(defaultDefinition, null, 2),
  );
  const [runInputText, setRunInputText] = useState(sampleRunInput);
  const [selectedWorkflowId, setSelectedWorkflowId] = useState<number | null>(
    null,
  );
  const [validation, setValidation] = useState<WorkflowValidationResult | null>(
    null,
  );
  const [selectedRun, setSelectedRun] = useState<WorkflowRun | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const workflowsQuery = useQuery({
    queryKey: ["workflows"],
    queryFn: listWorkflows,
  });
  const runsQuery = useQuery({
    queryKey: ["workflow-runs"],
    queryFn: listWorkflowRuns,
  });

  const selectedWorkflow = useMemo(
    () =>
      workflowsQuery.data?.find(
        (workflow) => workflow.id === selectedWorkflowId,
      ) ?? workflowsQuery.data?.[0],
    [selectedWorkflowId, workflowsQuery.data],
  );

  const validateMutation = useMutation({
    mutationFn: () =>
      validateWorkflow({
        name: form.name,
        version: form.version,
        description: form.description,
        definition: parseDefinition(definitionText),
        enabled: true,
      }),
    onSuccess: (result) => {
      setValidation(result);
      setNotice(result.valid ? "Workflow 校验通过。" : "Workflow 校验未通过。");
      setError(null);
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const createMutation = useMutation({
    mutationFn: () =>
      createWorkflow({
        name: form.name,
        version: form.version,
        description: form.description,
        definition: parseDefinition(definitionText),
        enabled: true,
      }),
    onSuccess: (workflow) => {
      setSelectedWorkflowId(workflow.id);
      setNotice(`已创建 Workflow：${workflow.name}@${workflow.version}`);
      setError(null);
      queryClient.invalidateQueries({ queryKey: ["workflows"] });
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const runMutation = useMutation({
    mutationFn: () => {
      if (!selectedWorkflow) {
        throw new Error("请先选择一个 Workflow。");
      }
      return runWorkflow(selectedWorkflow.id, parseJSON(runInputText));
    },
    onSuccess: (run) => {
      setSelectedRun(run);
      setNotice(`Workflow Run #${run.id} 已完成，状态：${run.status}`);
      setError(null);
      queryClient.invalidateQueries({ queryKey: ["workflow-runs"] });
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  function submitValidate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    try {
      validateMutation.mutate();
    } catch (err) {
      setError(toAPIErrorMessage(err));
    }
  }

  function submitCreate() {
    try {
      createMutation.mutate();
    } catch (err) {
      setError(toAPIErrorMessage(err));
    }
  }

  function submitRun(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    try {
      runMutation.mutate();
    } catch (err) {
      setError(toAPIErrorMessage(err));
    }
  }

  return (
    <div className="mx-auto max-w-[1700px] space-y-6">
      <section className="flex flex-col justify-between gap-4 xl:flex-row xl:items-end">
        <div>
          <p className="text-sm font-medium text-cyan-700">Workflow Center</p>
          <h1 className="mt-1 text-3xl font-semibold tracking-tight text-slate-950">
            工作流
          </h1>
          <p className="mt-2 max-w-3xl text-sm leading-6 text-slate-500">
            管理只读分析 Workflow。当前 Builder 使用 DSL JSON
            编辑，支持后端校验、创建、运行记录和节点状态查看。
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <StatusPill label="DAG 校验" />
          <StatusPill label="节点状态" />
          <StatusPill label="运行记录" />
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

      <section className="grid gap-6 xl:grid-cols-[0.95fr_1.05fr]">
        <Card className="border-slate-200/80 shadow-none">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <GitBranch className="size-5 text-cyan-600" />
              Builder
            </CardTitle>
            <CardDescription>
              创建简单 DAG，或故意改错节点引用来查看后端校验错误。
            </CardDescription>
          </CardHeader>
          <CardContent>
            <form className="space-y-4" onSubmit={submitValidate}>
              <div className="grid gap-3 sm:grid-cols-2">
                <Field label="名称">
                  <Input
                    value={form.name}
                    onChange={(event) =>
                      setForm((current) => ({
                        ...current,
                        name: event.target.value,
                      }))
                    }
                  />
                </Field>
                <Field label="版本">
                  <Input
                    value={form.version}
                    onChange={(event) =>
                      setForm((current) => ({
                        ...current,
                        version: event.target.value,
                      }))
                    }
                  />
                </Field>
              </div>
              <Field label="描述">
                <Input
                  value={form.description}
                  onChange={(event) =>
                    setForm((current) => ({
                      ...current,
                      description: event.target.value,
                    }))
                  }
                />
              </Field>
              <Field label="DSL JSON">
                <textarea
                  className="min-h-[420px] w-full rounded-lg border border-slate-200 bg-white px-3 py-2 font-mono text-xs leading-5 outline-none ring-cyan-500/20 transition focus:border-cyan-500 focus:ring-4"
                  value={definitionText}
                  onChange={(event) => setDefinitionText(event.target.value)}
                  spellCheck={false}
                />
              </Field>
              <div className="flex flex-wrap gap-2">
                <Button type="submit" disabled={validateMutation.isPending}>
                  {validateMutation.isPending ? (
                    <Loader2 className="mr-2 size-4 animate-spin" />
                  ) : (
                    <CheckCircle2 className="mr-2 size-4" />
                  )}
                  后端校验
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  disabled={createMutation.isPending}
                  onClick={submitCreate}
                >
                  {createMutation.isPending ? (
                    <Loader2 className="mr-2 size-4 animate-spin" />
                  ) : (
                    <Workflow className="mr-2 size-4" />
                  )}
                  创建 Workflow
                </Button>
              </div>
            </form>
          </CardContent>
        </Card>

        <div className="space-y-6">
          <ValidationPanel validation={validation} />
          <WorkflowList
            workflows={workflowsQuery.data ?? []}
            selectedId={selectedWorkflow?.id}
            loading={workflowsQuery.isLoading}
            onSelect={(workflow) => setSelectedWorkflowId(workflow.id)}
          />
          <RunPanel
            workflow={selectedWorkflow}
            runInputText={runInputText}
            onRunInputTextChange={setRunInputText}
            onSubmit={submitRun}
            running={runMutation.isPending}
          />
        </div>
      </section>

      <section className="grid gap-6 xl:grid-cols-[0.9fr_1.1fr]">
        <RunList
          runs={runsQuery.data ?? []}
          selectedRunId={selectedRun?.id}
          loading={runsQuery.isLoading}
          onRefresh={() =>
            queryClient.invalidateQueries({ queryKey: ["workflow-runs"] })
          }
          onSelect={setSelectedRun}
        />
        <RunDetail run={selectedRun ?? runsQuery.data?.[0] ?? null} />
      </section>
    </div>
  );
}

function ValidationPanel({
  validation,
}: {
  validation: WorkflowValidationResult | null;
}) {
  return (
    <Card className="border-slate-200/80 shadow-none">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <AlertTriangle className="size-5 text-amber-500" />
          后端校验结果
        </CardTitle>
      </CardHeader>
      <CardContent>
        {!validation ? (
          <p className="text-sm text-slate-500">尚未校验。</p>
        ) : validation.valid ? (
          <div className="rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm text-emerald-700">
            校验通过，可以创建或运行。
          </div>
        ) : (
          <div className="space-y-2">
            {validation.errors.map((item) => (
              <p
                key={item}
                className="rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700"
              >
                {item}
              </p>
            ))}
          </div>
        )}
        {validation?.warnings?.length ? (
          <div className="mt-3 space-y-2">
            {validation.warnings.map((item) => (
              <p
                key={item}
                className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-700"
              >
                {item}
              </p>
            ))}
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}

function WorkflowList({
  workflows,
  selectedId,
  loading,
  onSelect,
}: {
  workflows: WorkflowRecord[];
  selectedId?: number;
  loading: boolean;
  onSelect: (workflow: WorkflowRecord) => void;
}) {
  return (
    <Card className="border-slate-200/80 shadow-none">
      <CardHeader>
        <CardTitle>Workflow 列表</CardTitle>
        <CardDescription>
          包含服务启动时自动注册的内置 Workflow。
        </CardDescription>
      </CardHeader>
      <CardContent>
        {loading ? (
          <p className="text-sm text-slate-500">加载中...</p>
        ) : workflows.length === 0 ? (
          <p className="text-sm text-slate-500">暂无 Workflow。</p>
        ) : (
          <div className="grid gap-2">
            {workflows.map((workflow) => (
              <button
                key={workflow.id}
                type="button"
                onClick={() => onSelect(workflow)}
                className={cn(
                  "rounded-xl border px-4 py-3 text-left transition",
                  selectedId === workflow.id
                    ? "border-cyan-300 bg-cyan-50"
                    : "border-slate-200 bg-white hover:border-slate-300",
                )}
              >
                <div className="flex items-center justify-between gap-3">
                  <p className="font-medium text-slate-900">{workflow.name}</p>
                  <span className="rounded-full bg-slate-100 px-2 py-1 text-xs text-slate-600">
                    {workflow.version}
                  </span>
                </div>
                <p className="mt-1 line-clamp-2 text-xs text-slate-500">
                  {workflow.description ?? workflow.definition.description}
                </p>
              </button>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function RunPanel({
  workflow,
  runInputText,
  onRunInputTextChange,
  onSubmit,
  running,
}: {
  workflow?: WorkflowRecord;
  runInputText: string;
  onRunInputTextChange: (value: string) => void;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
  running: boolean;
}) {
  return (
    <Card className="border-slate-200/80 shadow-none">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <PlayCircle className="size-5 text-cyan-600" />
          运行 Workflow
        </CardTitle>
        <CardDescription>
          当前选择：{workflow ? `${workflow.name}@${workflow.version}` : "无"}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form className="space-y-3" onSubmit={onSubmit}>
          <Field label="运行输入 JSON">
            <textarea
              className="min-h-32 w-full rounded-lg border border-slate-200 bg-white px-3 py-2 font-mono text-xs leading-5 outline-none ring-cyan-500/20 transition focus:border-cyan-500 focus:ring-4"
              value={runInputText}
              onChange={(event) => onRunInputTextChange(event.target.value)}
              spellCheck={false}
            />
          </Field>
          <Button type="submit" disabled={!workflow || running}>
            {running ? (
              <Loader2 className="mr-2 size-4 animate-spin" />
            ) : (
              <PlayCircle className="mr-2 size-4" />
            )}
            执行
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

function RunList({
  runs,
  selectedRunId,
  loading,
  onRefresh,
  onSelect,
}: {
  runs: WorkflowRun[];
  selectedRunId?: number;
  loading: boolean;
  onRefresh: () => void;
  onSelect: (run: WorkflowRun) => void;
}) {
  return (
    <Card className="border-slate-200/80 shadow-none">
      <CardHeader className="flex flex-row items-start justify-between gap-3">
        <div>
          <CardTitle>运行记录</CardTitle>
          <CardDescription>查看 Workflow Run 和节点状态。</CardDescription>
        </div>
        <Button variant="outline" size="sm" onClick={onRefresh}>
          <RefreshCw className="mr-2 size-4" />
          刷新
        </Button>
      </CardHeader>
      <CardContent>
        {loading ? (
          <p className="text-sm text-slate-500">加载中...</p>
        ) : runs.length === 0 ? (
          <p className="text-sm text-slate-500">暂无运行记录。</p>
        ) : (
          <div className="space-y-2">
            {runs.slice(0, 8).map((run) => (
              <button
                key={run.id}
                type="button"
                onClick={() => onSelect(run)}
                className={cn(
                  "flex w-full items-center justify-between rounded-xl border px-4 py-3 text-left transition",
                  selectedRunId === run.id
                    ? "border-cyan-300 bg-cyan-50"
                    : "border-slate-200 bg-white hover:border-slate-300",
                )}
              >
                <div>
                  <p className="font-medium text-slate-900">Run #{run.id}</p>
                  <p className="text-xs text-slate-500">
                    Workflow #{run.workflowId ?? "-"} · {run.createdAt}
                  </p>
                </div>
                <StatusBadge status={run.status} />
              </button>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function RunDetail({ run }: { run: WorkflowRun | null }) {
  return (
    <Card className="border-slate-200/80 shadow-none">
      <CardHeader>
        <CardTitle>运行详情</CardTitle>
        <CardDescription>节点状态、输入输出摘要和错误信息。</CardDescription>
      </CardHeader>
      <CardContent>
        {!run ? (
          <p className="text-sm text-slate-500">选择一条运行记录查看详情。</p>
        ) : (
          <div className="space-y-5">
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-sm font-medium text-slate-900">
                Run #{run.id}
              </span>
              <StatusBadge status={run.status} />
              {run.errorMessage ? (
                <span className="text-sm text-rose-600">
                  {run.errorMessage}
                </span>
              ) : null}
            </div>
            <div className="grid gap-3 md:grid-cols-2">
              <SummaryBlock title="输入摘要" value={run.input} />
              <SummaryBlock title="输出摘要" value={run.output} />
            </div>
            <div className="space-y-3">
              <h3 className="text-sm font-semibold text-slate-900">节点状态</h3>
              {(run.nodeRuns ?? []).length === 0 ? (
                <p className="text-sm text-slate-500">暂无节点记录。</p>
              ) : (
                <div className="grid gap-3">
                  {(run.nodeRuns ?? []).map((node) => (
                    <NodeRunCard key={node.id} node={node} />
                  ))}
                </div>
              )}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function NodeRunCard({ node }: { node: WorkflowNodeRun }) {
  return (
    <div className="rounded-xl border border-slate-200 bg-white p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="font-medium text-slate-900">{node.nodeId}</p>
          <p className="text-xs text-slate-500">
            {node.nodeType} · attempt {node.attempt}
          </p>
        </div>
        <StatusBadge status={node.status} />
      </div>
      {node.errorMessage ? (
        <p className="mt-2 rounded-lg bg-rose-50 px-3 py-2 text-xs text-rose-700">
          {node.errorMessage}
        </p>
      ) : null}
      <div className="mt-3 grid gap-3 md:grid-cols-2">
        <SummaryBlock title="输入" value={node.input} compact />
        <SummaryBlock title="输出" value={node.output} compact />
      </div>
    </div>
  );
}

function SummaryBlock({
  title,
  value,
  compact,
}: {
  title: string;
  value: unknown;
  compact?: boolean;
}) {
  return (
    <div className="rounded-xl border border-slate-200 bg-slate-50 p-3">
      <p className="mb-2 text-xs font-medium uppercase tracking-wide text-slate-500">
        {title}
      </p>
      <pre
        className={cn(
          "overflow-auto whitespace-pre-wrap break-words rounded-lg bg-white p-3 font-mono text-xs leading-5 text-slate-700",
          compact ? "max-h-32" : "max-h-60",
        )}
      >
        {summarize(value)}
      </pre>
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const color =
    status === "success"
      ? "bg-emerald-50 text-emerald-700 ring-emerald-200"
      : status === "partial_success"
        ? "bg-amber-50 text-amber-700 ring-amber-200"
        : status === "failed" || status === "cancelled"
          ? "bg-rose-50 text-rose-700 ring-rose-200"
          : "bg-slate-100 text-slate-700 ring-slate-200";
  return (
    <span
      className={cn(
        "rounded-full px-2.5 py-1 text-xs font-medium ring-1",
        color,
      )}
    >
      {status}
    </span>
  );
}

function StatusPill({ label }: { label: string }) {
  return (
    <span className="rounded-full border border-cyan-200 bg-cyan-50 px-3 py-1 text-xs font-medium text-cyan-700">
      {label}
    </span>
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

function parseDefinition(text: string): WorkflowDefinition {
  const parsed = parseJSON(text);
  if (!isWorkflowDefinition(parsed)) {
    throw new Error("DSL JSON 必须包含 nodes 和 edges。");
  }
  return parsed;
}

function parseJSON(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    throw new Error("JSON 格式不正确。");
  }
}

function isWorkflowDefinition(value: unknown): value is WorkflowDefinition {
  if (!value || typeof value !== "object") {
    return false;
  }
  const candidate = value as Partial<WorkflowDefinition>;
  return Array.isArray(candidate.nodes) && Array.isArray(candidate.edges);
}

function summarize(value: unknown) {
  if (value === undefined || value === null || value === "") {
    return "无";
  }
  if (typeof value === "string") {
    return value.length > 800 ? `${value.slice(0, 800)}...` : value;
  }
  const text = JSON.stringify(value, null, 2);
  return text.length > 1200 ? `${text.slice(0, 1200)}...` : text;
}
