import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  CheckCircle2,
  GitMerge,
  Loader2,
  Play,
  Save,
  Settings2,
  ShieldAlert,
  Sparkles,
} from "lucide-react";

import {
  listTopologyConflicts,
  listTopologyNodeTypes,
  listTopologyRelationTypes,
  listTopologySources,
  listTopologySyncRuns,
  previewTopologySourceMapping,
  resolveTopologyConflict,
  runTopologySourceSync,
  toAPIErrorMessage,
  updateTopologyRelationType,
  updateTopologySource,
  type MappingPreviewResult,
  type TopologyConflict,
  type TopologyRelationType,
  type TopologySourceConfig,
  type TopologySyncRun,
} from "@/api/operations";
import { Button, buttonVariants } from "@/components/ui/button";
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

const sampleMapping = {
  nodeMappings: [
    {
      name: "service",
      entityPath: "$.services[*]",
      targetNodeType: "service",
      externalKeyTemplate: "{{name}}",
      nameTemplate: "{{name}}",
      attributes: { namespace: "{{namespace}}" },
      aliases: ["{{namespace}}/{{name}}"],
    },
  ],
  edgeMappings: [
    {
      name: "service-dependency",
      entityPath: "$.dependencies[*]",
      sourceLookup: {
        nodeType: "service",
        externalKeyTemplate: "{{from}}",
      },
      targetLookup: {
        nodeType: "service",
        externalKeyTemplate: "{{to}}",
      },
      relationType: "depends_on",
      confidence: 0.85,
    },
  ],
};

const sampleData = {
  services: [
    { name: "payment-api", namespace: "prod" },
    { name: "orders-api", namespace: "prod" },
  ],
  dependencies: [{ from: "payment-api", to: "orders-api" }],
};

export function TopologyConfigurationPage() {
  const queryClient = useQueryClient();
  const [selectedSourceId, setSelectedSourceId] = useState<number | null>(null);
  const [mappingText, setMappingText] = useState(
    JSON.stringify(sampleMapping, null, 2),
  );
  const [sampleText, setSampleText] = useState(
    JSON.stringify(sampleData, null, 2),
  );
  const [preview, setPreview] = useState<MappingPreviewResult | null>(null);
  const [previewValidFor, setPreviewValidFor] = useState<string | null>(null);
  const [relationWarningId, setRelationWarningId] = useState<number | null>(
    null,
  );
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const nodeTypesQuery = useQuery({
    queryKey: ["topology", "node-types"],
    queryFn: listTopologyNodeTypes,
  });
  const relationTypesQuery = useQuery({
    queryKey: ["topology", "relation-types"],
    queryFn: listTopologyRelationTypes,
  });
  const sourcesQuery = useQuery({
    queryKey: ["topology", "sources"],
    queryFn: listTopologySources,
  });
  const syncRunsQuery = useQuery({
    queryKey: ["topology", "sync-runs", selectedSourceId],
    queryFn: () =>
      listTopologySyncRuns({
        sourceConfigId: selectedSourceId ?? undefined,
        limit: 12,
      }),
  });
  const conflictsQuery = useQuery({
    queryKey: ["topology", "conflicts", "open"],
    queryFn: () => listTopologyConflicts({ status: "open", limit: 12 }),
  });

  useEffect(() => {
    if (selectedSourceId === null && sourcesQuery.data?.length) {
      setSelectedSourceId(sourcesQuery.data[0].id);
      setMappingText(
        JSON.stringify(
          normalizeObject(sourcesQuery.data[0].mappingRules),
          null,
          2,
        ),
      );
    }
  }, [selectedSourceId, sourcesQuery.data]);

  const selectedSource = useMemo(
    () => sourcesQuery.data?.find((source) => source.id === selectedSourceId),
    [selectedSourceId, sourcesQuery.data],
  );

  const previewMutation = useMutation({
    mutationFn: previewTopologySourceMapping,
    onSuccess: (result) => {
      setPreview(result);
      setPreviewValidFor(mappingText);
      setNotice(
        `Preview 完成：${result.nodes.length} 个节点、${result.edges.length} 条关系。`,
      );
      setError(null);
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const saveMappingMutation = useMutation({
    mutationFn: updateTopologySource,
    onSuccess: (source) => {
      setNotice(`Mapping 已保存到 Source：${source.name}`);
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ["topology", "sources"] });
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const runMutation = useMutation({
    mutationFn: runTopologySourceSync,
    onSuccess: (run) => {
      setNotice(`同步已触发：run #${run.id}，状态 ${run.status}`);
      setError(null);
      void queryClient.invalidateQueries({
        queryKey: ["topology", "sync-runs"],
      });
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const relationMutation = useMutation({
    mutationFn: updateTopologyRelationType,
    onSuccess: () => {
      setRelationWarningId(null);
      setNotice("Relation Type 已更新，传播语义会影响 RCA 和 Blast Radius。");
      setError(null);
      void queryClient.invalidateQueries({
        queryKey: ["topology", "relation-types"],
      });
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const resolveMutation = useMutation({
    mutationFn: resolveTopologyConflict,
    onSuccess: () => {
      setNotice("冲突已处理。");
      setError(null);
      void queryClient.invalidateQueries({
        queryKey: ["topology", "conflicts"],
      });
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const canSaveMapping =
    Boolean(selectedSource) &&
    previewValidFor === mappingText &&
    preview !== null;

  function parseJSON(value: string) {
    try {
      return { ok: true as const, value: JSON.parse(value) as unknown };
    } catch (err) {
      return {
        ok: false as const,
        message: err instanceof Error ? err.message : "JSON 格式错误",
      };
    }
  }

  function runPreview() {
    if (!selectedSourceId) {
      setError("请先选择一个 Topology Source。");
      return;
    }
    const mapping = parseJSON(mappingText);
    const sample = parseJSON(sampleText);
    if (!mapping.ok) {
      setError(mapping.message || "Mapping Rules JSON 格式错误");
      return;
    }
    if (!sample.ok) {
      setError(sample.message || "Sample Data JSON 格式错误");
      return;
    }
    previewMutation.mutate({
      sourceId: selectedSourceId,
      mappingRules: mapping.value,
      sampleData: sample.value,
      limit: 20,
    });
  }

  function saveMapping() {
    if (!selectedSource) {
      return;
    }
    const mapping = parseJSON(mappingText);
    if (!mapping.ok) {
      setError(mapping.message);
      return;
    }
    saveMappingMutation.mutate({
      id: selectedSource.id,
      data: {
        name: selectedSource.name,
        sourceType: selectedSource.sourceType,
        dataSourceId: selectedSource.dataSourceId,
        enabled: selectedSource.enabled,
        priority: selectedSource.priority,
        schedule: selectedSource.schedule,
        scope: selectedSource.scope ?? {},
        mappingRules: mapping.value,
        staleAfterSeconds: selectedSource.staleAfterSeconds,
        deleteAfterSeconds: selectedSource.deleteAfterSeconds,
      },
    });
  }

  return (
    <div className="mx-auto max-w-[1900px] space-y-6">
      <section className="flex flex-col justify-between gap-4 xl:flex-row xl:items-end">
        <div>
          <p className="text-sm font-medium text-cyan-700">
            Topology Configuration
          </p>
          <h1 className="mt-2 text-3xl font-semibold tracking-tight text-slate-950">
            拓扑配置中心
          </h1>
          <p className="mt-3 max-w-3xl text-sm leading-6 text-slate-600">
            管理 Type Catalog、Source Mapping、同步运行和冲突处理。Mapping 必须
            Preview 通过后才能保存，敏感字段在预览中默认脱敏展示。
          </p>
        </div>
        <Link
          className={cn(buttonVariants({ variant: "outline" }))}
          to="/topology"
        >
          返回拓扑地图
        </Link>
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

      <div className="grid gap-6 2xl:grid-cols-[390px_minmax(0,1fr)_390px]">
        <div className="space-y-6">
          <TypeCatalogCard
            nodeTypes={nodeTypesQuery.data ?? []}
            relationTypes={relationTypesQuery.data ?? []}
            loading={nodeTypesQuery.isLoading || relationTypesQuery.isLoading}
            warningId={relationWarningId}
            onWarn={setRelationWarningId}
            onConfirm={(relation) =>
              relationMutation.mutate({
                id: relation.id,
                data: {
                  typeKey: relation.typeKey,
                  displayName: relation.displayName,
                  semantics: relation.semantics,
                  failurePropagation:
                    relation.failurePropagation === "none"
                      ? "src_to_dst"
                      : "none",
                  defaultDirection: relation.defaultDirection,
                  propagatesFailure: !relation.propagatesFailure,
                  allowedSourceTypes: normalizeObject(
                    relation.allowedSourceTypes,
                  ),
                  allowedTargetTypes: normalizeObject(
                    relation.allowedTargetTypes,
                  ),
                  style: normalizeObject(relation.style),
                  enabled: relation.enabled,
                },
              })
            }
          />
        </div>

        <div className="space-y-6">
          <SourceWizardCard
            sources={sourcesQuery.data ?? []}
            selectedSource={selectedSource}
            selectedSourceId={selectedSourceId}
            mappingText={mappingText}
            sampleText={sampleText}
            preview={preview}
            canSaveMapping={canSaveMapping}
            loading={
              sourcesQuery.isLoading ||
              previewMutation.isPending ||
              saveMappingMutation.isPending ||
              runMutation.isPending
            }
            onSelectSource={(sourceId) => {
              setSelectedSourceId(sourceId);
              const source = sourcesQuery.data?.find(
                (item) => item.id === sourceId,
              );
              if (source) {
                setMappingText(
                  JSON.stringify(normalizeObject(source.mappingRules), null, 2),
                );
                setPreview(null);
                setPreviewValidFor(null);
              }
            }}
            onMappingChange={(value) => {
              setMappingText(value);
              setPreviewValidFor(null);
            }}
            onSampleChange={setSampleText}
            onPreview={runPreview}
            onSave={saveMapping}
            onRun={() => {
              if (selectedSourceId) {
                runMutation.mutate({ sourceId: selectedSourceId });
              }
            }}
          />

          <SyncRunsCard
            runs={syncRunsQuery.data ?? []}
            loading={syncRunsQuery.isLoading}
          />
        </div>

        <ConflictCenterCard
          conflicts={conflictsQuery.data ?? []}
          loading={conflictsQuery.isLoading || resolveMutation.isPending}
          onResolve={(conflict) =>
            resolveMutation.mutate({
              conflictId: conflict.id,
              action: "ignore",
              note: "resolved from topology configuration UI",
            })
          }
        />
      </div>
    </div>
  );
}

function TypeCatalogCard({
  nodeTypes,
  relationTypes,
  loading,
  warningId,
  onWarn,
  onConfirm,
}: {
  nodeTypes: Array<{
    id: number;
    typeKey: string;
    displayName: string;
    category?: string;
    enabled: boolean;
    builtIn: boolean;
  }>;
  relationTypes: TopologyRelationType[];
  loading: boolean;
  warningId: number | null;
  onWarn: (id: number | null) => void;
  onConfirm: (relation: TopologyRelationType) => void;
}) {
  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-3">
          <div className="grid size-10 place-items-center rounded-xl bg-cyan-50 text-cyan-700">
            <Settings2 className="size-5" aria-hidden="true" />
          </div>
          <div>
            <CardTitle>Type Catalog</CardTitle>
            <CardDescription>节点类型与关系传播语义。</CardDescription>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-5">
        {loading && <InlineHint text="加载 Type Catalog..." />}
        <div>
          <SectionTitle>Node Types</SectionTitle>
          <div className="grid gap-2">
            {nodeTypes.slice(0, 8).map((type) => (
              <div
                key={type.id}
                className="rounded-xl border border-slate-200 bg-white p-3 text-sm"
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="font-medium text-slate-900">
                    {type.displayName}
                  </span>
                  <StatusPill ok={type.enabled} />
                </div>
                <p className="mt-1 font-mono text-xs text-slate-500">
                  {type.typeKey} · {type.category ?? "uncategorized"} ·{" "}
                  {type.builtIn ? "built-in" : "custom"}
                </p>
              </div>
            ))}
          </div>
        </div>

        <div>
          <SectionTitle>Relation Types</SectionTitle>
          <div className="grid gap-2">
            {relationTypes.slice(0, 8).map((relation) => (
              <div
                key={relation.id}
                className="rounded-xl border border-slate-200 bg-slate-50 p-3 text-sm"
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="font-medium text-slate-900">
                    {relation.displayName}
                  </span>
                  <span
                    className={cn(
                      "rounded-full px-2 py-0.5 text-[11px] font-medium",
                      relation.propagatesFailure
                        ? "bg-amber-100 text-amber-800"
                        : "bg-slate-200 text-slate-600",
                    )}
                  >
                    {relation.failurePropagation}
                  </span>
                </div>
                <p className="mt-1 text-xs text-slate-500">
                  {relation.typeKey} · {relation.semantics}
                </p>
                {warningId === relation.id ? (
                  <div className="mt-3 rounded-lg border border-amber-200 bg-amber-50 p-3 text-xs text-amber-800">
                    <div className="flex gap-2">
                      <ShieldAlert className="mt-0.5 size-4 shrink-0" />
                      <p>
                        修改 propagation 会影响 Blast Radius、RCA
                        关联分和影响路径，请确认这是有意的类型语义变更。
                      </p>
                    </div>
                    <div className="mt-3 flex gap-2">
                      <Button size="sm" onClick={() => onConfirm(relation)}>
                        确认修改
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => onWarn(null)}
                      >
                        取消
                      </Button>
                    </div>
                  </div>
                ) : (
                  <Button
                    className="mt-3 w-full"
                    size="sm"
                    variant="outline"
                    onClick={() => onWarn(relation.id)}
                  >
                    修改 propagation
                  </Button>
                )}
              </div>
            ))}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

function SourceWizardCard({
  sources,
  selectedSource,
  selectedSourceId,
  mappingText,
  sampleText,
  preview,
  canSaveMapping,
  loading,
  onSelectSource,
  onMappingChange,
  onSampleChange,
  onPreview,
  onSave,
  onRun,
}: {
  sources: TopologySourceConfig[];
  selectedSource?: TopologySourceConfig;
  selectedSourceId: number | null;
  mappingText: string;
  sampleText: string;
  preview: MappingPreviewResult | null;
  canSaveMapping: boolean;
  loading: boolean;
  onSelectSource: (sourceId: number) => void;
  onMappingChange: (value: string) => void;
  onSampleChange: (value: string) => void;
  onPreview: () => void;
  onSave: () => void;
  onRun: () => void;
}) {
  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-3">
          <div className="grid size-10 place-items-center rounded-xl bg-violet-50 text-violet-700">
            <Sparkles className="size-5" aria-hidden="true" />
          </div>
          <div>
            <CardTitle>Source Wizard / Mapping Preview</CardTitle>
            <CardDescription>
              选择 Source，预览 Mapping 后保存并运行同步。
            </CardDescription>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <Field label="Topology Source">
          <select
            value={selectedSourceId ?? ""}
            onChange={(event) => onSelectSource(Number(event.target.value))}
            className="h-11 w-full rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <option value="" disabled>
              选择已有 Source
            </option>
            {sources.map((source) => (
              <option key={source.id} value={source.id}>
                {source.name} · {source.sourceType}
              </option>
            ))}
          </select>
        </Field>

        {selectedSource && (
          <div className="grid gap-3 rounded-xl border border-slate-200 bg-slate-50 p-3 text-sm md:grid-cols-4">
            <Info label="Type" value={selectedSource.sourceType} />
            <Info label="Priority" value={`${selectedSource.priority}`} />
            <Info label="Schedule" value={selectedSource.schedule ?? "-"} />
            <Info
              label="Enabled"
              value={selectedSource.enabled ? "yes" : "no"}
            />
          </div>
        )}

        <div className="grid gap-4 xl:grid-cols-2">
          <Field label="Mapping Rules JSON">
            <textarea
              value={mappingText}
              onChange={(event) => onMappingChange(event.target.value)}
              className="min-h-96 w-full rounded-xl border border-slate-200 bg-slate-950 p-4 font-mono text-xs leading-5 text-slate-100 outline-none focus-visible:ring-2 focus-visible:ring-cyan-400"
              spellCheck={false}
            />
          </Field>
          <Field label="Sample Data JSON">
            <textarea
              value={sampleText}
              onChange={(event) => onSampleChange(event.target.value)}
              className="min-h-96 w-full rounded-xl border border-slate-200 bg-white p-4 font-mono text-xs leading-5 text-slate-700 outline-none focus-visible:ring-2 focus-visible:ring-cyan-400"
              spellCheck={false}
            />
          </Field>
        </div>

        <div className="flex flex-wrap gap-2">
          <Button onClick={onPreview} disabled={loading || !selectedSourceId}>
            {loading && <Loader2 className="size-4 animate-spin" />}
            Preview Mapping
          </Button>
          <Button
            variant="outline"
            onClick={onSave}
            disabled={loading || !canSaveMapping}
          >
            <Save className="size-4" />
            保存 Mapping
          </Button>
          <Button
            variant="outline"
            onClick={onRun}
            disabled={loading || !selectedSourceId}
          >
            <Play className="size-4" />
            Run Sync
          </Button>
        </div>

        {!canSaveMapping && (
          <div className="rounded-xl border border-amber-200 bg-amber-50 p-3 text-xs text-amber-800">
            Preview 后才能保存 Mapping；Mapping 修改后需要重新 Preview。
          </div>
        )}

        <PreviewResultCard preview={preview} />
      </CardContent>
    </Card>
  );
}

function PreviewResultCard({
  preview,
}: {
  preview: MappingPreviewResult | null;
}) {
  if (!preview) {
    return <InlineHint text="尚未执行 Mapping Preview。" />;
  }
  return (
    <div className="space-y-3 rounded-xl border border-cyan-200 bg-cyan-50/50 p-4">
      <div className="grid gap-3 text-sm md:grid-cols-4">
        <Info label="Nodes" value={`${preview.nodes.length}`} />
        <Info label="Edges" value={`${preview.edges.length}`} />
        <Info label="Unresolved" value={`${preview.unresolved.length}`} />
        <Info label="Truncated" value={preview.truncated ? "yes" : "no"} />
      </div>
      <div className="grid gap-3 xl:grid-cols-2">
        <PreviewList
          title="Preview Nodes"
          items={preview.nodes.slice(0, 8).map((node) => ({
            key: node.nodeKey,
            title: node.name,
            subtitle: `${node.nodeType} · ${safeJson(node.attributes)}`,
          }))}
        />
        <PreviewList
          title="Preview Edges"
          items={preview.edges.slice(0, 8).map((edge) => ({
            key: `${edge.fromNodeKey}-${edge.toNodeKey}`,
            title: `${edge.fromNodeKey} → ${edge.toNodeKey}`,
            subtitle: `${edge.relationType} · ${edge.confidence ?? "-"}`,
          }))}
        />
      </div>
      {preview.warnings.length > 0 && (
        <div className="rounded-lg border border-amber-200 bg-amber-50 p-3 text-xs text-amber-800">
          {preview.warnings.map((warning) => (
            <p key={warning}>{warning}</p>
          ))}
        </div>
      )}
    </div>
  );
}

function SyncRunsCard({
  runs,
  loading,
}: {
  runs: TopologySyncRun[];
  loading: boolean;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>同步进度</CardTitle>
        <CardDescription>最近同步运行、发现数量、冲突和告警。</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {loading && <InlineHint text="加载同步记录..." />}
        {!loading && runs.length === 0 && <InlineHint text="暂无同步记录。" />}
        {runs.map((run) => (
          <div
            key={run.id}
            className="rounded-xl border border-slate-200 bg-white p-3 text-sm"
          >
            <div className="flex items-center justify-between gap-2">
              <span className="font-medium text-slate-900">Run #{run.id}</span>
              <span
                className={cn(
                  "rounded-full px-2 py-0.5 text-[11px] font-medium",
                  run.status === "success"
                    ? "bg-emerald-100 text-emerald-700"
                    : run.status === "failed"
                      ? "bg-rose-100 text-rose-700"
                      : "bg-amber-100 text-amber-700",
                )}
              >
                {run.status}
              </span>
            </div>
            <p className="mt-2 text-xs text-slate-500">
              discovered {run.discoveredNodes} nodes / {run.discoveredEdges}{" "}
              edges · conflicts {run.conflictCount} · warnings{" "}
              {run.warningCount}
            </p>
            <p className="mt-1 text-xs text-slate-400">
              {run.startedAt ?? run.createdAt}
            </p>
          </div>
        ))}
      </CardContent>
    </Card>
  );
}

function ConflictCenterCard({
  conflicts,
  loading,
  onResolve,
}: {
  conflicts: TopologyConflict[];
  loading: boolean;
  onResolve: (conflict: TopologyConflict) => void;
}) {
  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-3">
          <div className="grid size-10 place-items-center rounded-xl bg-rose-50 text-rose-700">
            <GitMerge className="size-5" aria-hidden="true" />
          </div>
          <div>
            <CardTitle>Conflict Center</CardTitle>
            <CardDescription>查看和处理拓扑来源冲突。</CardDescription>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        {loading && <InlineHint text="加载冲突..." />}
        {!loading && conflicts.length === 0 && (
          <div className="rounded-xl border border-emerald-200 bg-emerald-50 p-4 text-sm text-emerald-700">
            <CheckCircle2 className="mr-2 inline size-4" />
            暂无打开的冲突。
          </div>
        )}
        {conflicts.map((conflict) => (
          <div
            key={conflict.id}
            className="rounded-xl border border-rose-200 bg-rose-50/50 p-3 text-sm"
          >
            <div className="flex items-start gap-2">
              <AlertTriangle className="mt-0.5 size-4 shrink-0 text-rose-600" />
              <div className="min-w-0 flex-1">
                <div className="flex items-center justify-between gap-2">
                  <p className="font-medium text-slate-900">
                    {conflict.conflictType}
                  </p>
                  <span className="rounded-full bg-white px-2 py-0.5 text-[11px] text-rose-700">
                    {conflict.status}
                  </span>
                </div>
                <p className="mt-2 text-xs leading-5 text-slate-600">
                  {conflict.description}
                </p>
                <pre className="mt-3 max-h-36 overflow-auto rounded-lg bg-slate-950 p-3 text-xs text-slate-100">
                  {safeJson(redactSensitive(conflict.candidates))}
                </pre>
                <Button
                  className="mt-3 w-full"
                  variant="outline"
                  size="sm"
                  onClick={() => onResolve(conflict)}
                  disabled={loading}
                >
                  标记忽略
                </Button>
              </div>
            </div>
          </div>
        ))}
      </CardContent>
    </Card>
  );
}

function PreviewList({
  title,
  items,
}: {
  title: string;
  items: Array<{ key: string; title: string; subtitle: string }>;
}) {
  return (
    <div>
      <SectionTitle>{title}</SectionTitle>
      <div className="space-y-2">
        {items.map((item) => (
          <div
            key={item.key}
            className="rounded-lg border border-slate-200 bg-white p-3"
          >
            <p className="truncate text-sm font-medium text-slate-900">
              {item.title}
            </p>
            <p className="mt-1 truncate text-xs text-slate-500">
              {item.subtitle}
            </p>
          </div>
        ))}
      </div>
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

function SectionTitle({ children }: { children: ReactNode }) {
  return (
    <p className="mb-2 text-xs font-semibold uppercase tracking-wide text-slate-500">
      {children}
    </p>
  );
}

function InlineHint({ text }: { text: string }) {
  return (
    <div className="rounded-xl border border-dashed border-slate-200 bg-slate-50 p-4 text-sm text-slate-500">
      {text}
    </div>
  );
}

function StatusPill({ ok }: { ok: boolean }) {
  return (
    <span
      className={cn(
        "rounded-full px-2 py-0.5 text-[11px] font-medium",
        ok ? "bg-emerald-100 text-emerald-700" : "bg-slate-200 text-slate-600",
      )}
    >
      {ok ? "enabled" : "disabled"}
    </span>
  );
}

function Info({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <p className="text-xs text-slate-500">{label}</p>
      <p className="mt-1 truncate font-medium text-slate-900">{value}</p>
    </div>
  );
}

function normalizeObject(value: unknown): Record<string, unknown> {
  if (typeof value === "string") {
    try {
      return normalizeObject(JSON.parse(value) as unknown);
    } catch {
      return {};
    }
  }
  if (value && typeof value === "object" && !Array.isArray(value)) {
    return value as Record<string, unknown>;
  }
  return {};
}

function safeJson(value: unknown) {
  if (value === undefined || value === null) {
    return "{}";
  }
  return JSON.stringify(redactSensitive(value), null, 2);
}

function redactSensitive(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map(redactSensitive);
  }
  if (value && typeof value === "object") {
    const result: Record<string, unknown> = {};
    for (const [key, item] of Object.entries(value)) {
      if (/secret|token|password|apikey|api_key|credential/i.test(key)) {
        result[key] = "******";
      } else {
        result[key] = redactSensitive(item);
      }
    }
    return result;
  }
  return value;
}
