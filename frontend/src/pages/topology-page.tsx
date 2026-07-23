import { useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Background,
  type Connection,
  Controls,
  Handle,
  MarkerType,
  MiniMap,
  Position,
  ReactFlow,
  type Edge as FlowEdge,
  type Node as FlowNode,
  type NodeProps,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  BellRing,
  CheckCircle2,
  GitBranch,
  Loader2,
  Network,
  Pencil,
  Plus,
  Radius,
  Save,
  Search,
  Trash2,
  X,
  Zap,
} from "lucide-react";

import {
  createTopologyEdge,
  createTopologyNode,
  createTopologySavedView,
  deleteTopologyEdge,
  deleteTopologyNode,
  expandTopology,
  getBlastRadius,
  getTopologyGraph,
  listTopologySavedViews,
  toAPIErrorMessage,
  updateTopologyEdge,
  updateTopologyNode,
  type BlastRadius,
  type ExpandTopologyInput,
  type ExpandTopologyResult,
  type TopologyEdge,
  type TopologyEdgeInput,
  type TopologyDirection,
  type TopologyGraph,
  type TopologyNode,
  type TopologyNodeInput,
  type TopologySavedView,
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

type TopologyQueryState = {
  nodeKey: string;
  environment: string;
  cluster: string;
  namespace: string;
  direction: TopologyDirection;
  depth: number;
  maxNodes: number;
  onlyPropagating: boolean;
};

type TopologyFlowNodeData = {
  topologyNode: TopologyNode;
  isRoot: boolean;
};

type TopologyFlowNode = FlowNode<TopologyFlowNodeData, "topologyNode">;

type ManualNodeForm = {
  nodeKey: string;
  kind: string;
  name: string;
  displayName: string;
  environment: string;
  cluster: string;
  namespace: string;
};

type ManualEdgeForm = {
  fromNodeKey: string;
  toNodeKey: string;
  edgeType: string;
  confidence: string;
};

const defaultQuery: TopologyQueryState = {
  nodeKey: "service:payment-api",
  environment: "",
  cluster: "",
  namespace: "",
  direction: "both",
  depth: 2,
  maxNodes: 200,
  onlyPropagating: false,
};

const emptyNodeForm: ManualNodeForm = {
  nodeKey: "",
  kind: "service",
  name: "",
  displayName: "",
  environment: "prod",
  cluster: "",
  namespace: "",
};

const emptyEdgeForm: ManualEdgeForm = {
  fromNodeKey: "",
  toNodeKey: "",
  edgeType: "depends_on",
  confidence: "1",
};

const nodePalette = [
  "bg-brand-50 text-brand-700 border-brand-200",
  "bg-violet-50 text-violet-700 border-violet-200",
  "bg-emerald-50 text-emerald-700 border-emerald-200",
  "bg-amber-50 text-amber-700 border-amber-200",
  "bg-rose-50 text-rose-700 border-rose-200",
  "bg-slate-50 text-slate-700 border-slate-200",
];

const topologyNodeTypes = {
  topologyNode: TopologyFlowNodeCard,
};

export function TopologyPage() {
  const queryClient = useQueryClient();
  const [query, setQuery] = useState<TopologyQueryState>(defaultQuery);
  const [graphMode, setGraphMode] = useState<"graph" | "expand">("graph");
  const [expanded, setExpanded] = useState<ExpandTopologyResult | null>(null);
  const [blast, setBlast] = useState<BlastRadius | null>(null);
  const [selectedNodeKey, setSelectedNodeKey] = useState<string | null>(null);
  const [selectedEdgeKey, setSelectedEdgeKey] = useState<string | null>(null);
  const [editingNodeId, setEditingNodeId] = useState<number | null>(null);
  const [editingEdgeId, setEditingEdgeId] = useState<number | null>(null);
  const [nodeForm, setNodeForm] = useState<ManualNodeForm>(emptyNodeForm);
  const [edgeForm, setEdgeForm] = useState<ManualEdgeForm>(emptyEdgeForm);
  const [viewName, setViewName] = useState("生产服务依赖视图");
  const [visibility, setVisibility] =
    useState<TopologySavedView["visibility"]>("private");
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const graphQuery = useQuery({
    queryKey: ["topology", "graph", query.maxNodes],
    queryFn: () => getTopologyGraph(query.maxNodes),
  });

  const viewsQuery = useQuery({
    queryKey: ["topology", "views"],
    queryFn: () => listTopologySavedViews(30),
  });

  const expandMutation = useMutation({
    mutationFn: expandTopology,
    onSuccess: (result) => {
      setExpanded(result);
      setBlast(null);
      setGraphMode("expand");
      setSelectedNodeKey(result.rootKey);
      setNotice(
        `已展开 ${result.nodes.length} 个节点、${result.edges.length} 条关系。`,
      );
      setError(null);
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const blastMutation = useMutation({
    mutationFn: getBlastRadius,
    onSuccess: (result) => {
      setBlast(result);
      setExpanded(null);
      setGraphMode("expand");
      setSelectedNodeKey(result.rootKey);
      setNotice(`Blast Radius 已计算：影响 ${result.nodes.length} 个节点。`);
      setError(null);
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const saveMutation = useMutation({
    mutationFn: createTopologySavedView,
    onSuccess: (view) => {
      setNotice(`视图已保存：${view.name}`);
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ["topology", "views"] });
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const activeGraph: TopologyGraph = useMemo(() => {
    if (expanded) {
      return { nodes: expanded.nodes, edges: expanded.edges };
    }
    if (blast) {
      return { nodes: blast.nodes, edges: blast.edges };
    }
    return graphQuery.data ?? { nodes: [], edges: [] };
  }, [blast, expanded, graphQuery.data]);

  const selectedNode = useMemo(
    () => activeGraph.nodes.find((node) => node.nodeKey === selectedNodeKey),
    [activeGraph.nodes, selectedNodeKey],
  );
  const selectedEdge = useMemo(
    () =>
      activeGraph.edges.find(
        (edge) =>
          edgeKeyOf(edge) === selectedEdgeKey ||
          edge.edgeKey === selectedEdgeKey,
      ),
    [activeGraph.edges, selectedEdgeKey],
  );

  const kindLegend = useMemo(
    () =>
      Array.from(new Set(activeGraph.nodes.map((node) => node.kind))).sort(),
    [activeGraph.nodes],
  );
  const edgeLegend = useMemo(
    () =>
      Array.from(
        new Set(activeGraph.edges.map((edge) => edge.edgeType)),
      ).sort(),
    [activeGraph.edges],
  );

  const truncated = Boolean(expanded?.truncated);
  const loading =
    graphQuery.isLoading || expandMutation.isPending || blastMutation.isPending;

  function refreshGraph(message: string) {
    setNotice(message);
    setError(null);
    setExpanded(null);
    setBlast(null);
    setGraphMode("graph");
    void queryClient.invalidateQueries({ queryKey: ["topology", "graph"] });
  }

  const createNodeMutation = useMutation({
    mutationFn: createTopologyNode,
    onSuccess: (node) => {
      setSelectedNodeKey(node.nodeKey);
      setEditingNodeId(null);
      setNodeForm(emptyNodeForm);
      refreshGraph(`节点已创建：${node.name}`);
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const updateNodeMutation = useMutation({
    mutationFn: updateTopologyNode,
    onSuccess: (node) => {
      setSelectedNodeKey(node.nodeKey);
      setEditingNodeId(null);
      refreshGraph(`节点已更新：${node.name}`);
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const deleteNodeMutation = useMutation({
    mutationFn: deleteTopologyNode,
    onSuccess: () => {
      setSelectedNodeKey(null);
      setEditingNodeId(null);
      setNodeForm(emptyNodeForm);
      refreshGraph("节点已删除。");
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const createEdgeMutation = useMutation({
    mutationFn: createTopologyEdge,
    onSuccess: (edge) => {
      setSelectedEdgeKey(edgeKeyOf(edge));
      setEditingEdgeId(null);
      setEdgeForm(emptyEdgeForm);
      refreshGraph(`关系已创建：${edge.edgeType}`);
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const updateEdgeMutation = useMutation({
    mutationFn: updateTopologyEdge,
    onSuccess: (edge) => {
      setSelectedEdgeKey(edgeKeyOf(edge));
      setEditingEdgeId(null);
      refreshGraph(`关系已更新：${edge.edgeType}`);
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const deleteEdgeMutation = useMutation({
    mutationFn: deleteTopologyEdge,
    onSuccess: () => {
      setSelectedEdgeKey(null);
      setEditingEdgeId(null);
      setEdgeForm(emptyEdgeForm);
      refreshGraph("关系已删除。");
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const manualSaving =
    createNodeMutation.isPending ||
    updateNodeMutation.isPending ||
    deleteNodeMutation.isPending ||
    createEdgeMutation.isPending ||
    updateEdgeMutation.isPending ||
    deleteEdgeMutation.isPending;

  function patchQuery(patch: Partial<TopologyQueryState>) {
    setQuery((current) => ({ ...current, ...patch }));
  }

  function currentExpandInput(): ExpandTopologyInput {
    return {
      nodeKey: query.nodeKey.trim(),
      depth: query.depth,
      direction: query.direction,
      maxNodes: query.maxNodes,
      maxEdges: query.maxNodes * 2,
      onlyPropagating: query.onlyPropagating,
      environment: query.environment.trim() || undefined,
      cluster: query.cluster.trim() || undefined,
      namespace: query.namespace.trim() || undefined,
    };
  }

  function loadSavedView(view: TopologySavedView) {
    const config = normalizeObject(view.queryConfig);
    const nextQuery: TopologyQueryState = {
      ...query,
      nodeKey: stringValue(config.nodeKey, query.nodeKey),
      environment: stringValue(config.environment, ""),
      cluster: stringValue(config.cluster, ""),
      namespace: stringValue(config.namespace, ""),
      direction: directionValue(config.direction, "both"),
      depth: numberValue(config.depth, 2),
      maxNodes: numberValue(config.maxNodes, 200),
      onlyPropagating: Boolean(config.onlyPropagating),
    };
    setQuery(nextQuery);
    setViewName(view.name);
    setVisibility(view.visibility);
    setNotice(`已载入保存视图：${view.name}`);
  }

  function saveCurrentView() {
    const selected = activeGraph.nodes.find(
      (node) => node.nodeKey === query.nodeKey.trim(),
    );
    saveMutation.mutate({
      name: viewName.trim(),
      visibility,
      centerNodeId: selected?.id,
      queryConfig: currentExpandInput() as Record<string, unknown>,
      displayConfig: {
        layout: "react-flow-layered",
        showLabels: true,
        graphMode,
        selectedNodeKey,
      },
      layoutData: {
        nodeCount: activeGraph.nodes.length,
        edgeCount: activeGraph.edges.length,
      },
    });
  }

  function nodeInputFromForm(): TopologyNodeInput | null {
    const name = nodeForm.name.trim();
    const kind = nodeForm.kind.trim();
    if (!name || !kind) {
      setError("节点名称和类型不能为空。");
      return null;
    }
    return {
      nodeKey: nodeForm.nodeKey.trim() || undefined,
      kind,
      name,
      displayName: nodeForm.displayName.trim() || undefined,
      environment: nodeForm.environment.trim() || undefined,
      cluster: nodeForm.cluster.trim() || undefined,
      namespace: nodeForm.namespace.trim() || undefined,
      sourceType: "manual",
    };
  }

  function edgeInputFromForm(): TopologyEdgeInput | null {
    const fromNodeKey = edgeForm.fromNodeKey.trim();
    const toNodeKey = edgeForm.toNodeKey.trim();
    const edgeType = edgeForm.edgeType.trim();
    if (!fromNodeKey || !toNodeKey || !edgeType) {
      setError("关系的起点、终点和类型不能为空。");
      return null;
    }
    const confidence = Number(edgeForm.confidence);
    return {
      fromNodeKey,
      toNodeKey,
      edgeType,
      confidence:
        Number.isFinite(confidence) && confidence >= 0 && confidence <= 1
          ? confidence
          : 1,
      sourceType: "manual",
    };
  }

  function submitManualNode() {
    const input = nodeInputFromForm();
    if (!input) {
      return;
    }
    if (editingNodeId) {
      updateNodeMutation.mutate({ id: editingNodeId, data: input });
      return;
    }
    createNodeMutation.mutate(input);
  }

  function submitManualEdge() {
    const input = edgeInputFromForm();
    if (!input) {
      return;
    }
    if (editingEdgeId) {
      updateEdgeMutation.mutate({ id: editingEdgeId, data: input });
      return;
    }
    createEdgeMutation.mutate(input);
  }

  function loadNodeForEdit(node: TopologyNode) {
    setEditingNodeId(node.id);
    setSelectedNodeKey(node.nodeKey);
    setNodeForm({
      nodeKey: node.nodeKey,
      kind: node.kind,
      name: node.name,
      displayName: node.displayName ?? "",
      environment: node.environment ?? "",
      cluster: node.cluster ?? "",
      namespace: node.namespace ?? "",
    });
  }

  function loadEdgeForEdit(edge: TopologyEdge) {
    setEditingEdgeId(edge.id);
    setSelectedEdgeKey(edgeKeyOf(edge));
    setEdgeForm({
      fromNodeKey: edge.fromNodeKey,
      toNodeKey: edge.toNodeKey,
      edgeType: edge.edgeType,
      confidence: String(edge.confidence ?? 1),
    });
  }

  function createEdgeFromConnection(connection: Connection) {
    if (!connection.source || !connection.target) {
      return;
    }
    createEdgeMutation.mutate({
      fromNodeKey: connection.source,
      toNodeKey: connection.target,
      edgeType: edgeForm.edgeType.trim() || "depends_on",
      confidence: 1,
      sourceType: "manual",
    });
  }

  return (
    <div className="mx-auto max-w-[1900px] space-y-6">
      <section className="flex flex-col justify-between gap-4 xl:flex-row xl:items-end">
        <div>
          <p className="text-sm font-medium text-brand-700">Topology Map</p>
          <h1 className="mt-2 text-3xl font-semibold tracking-tight text-slate-950">
            拓扑地图
          </h1>
          <p className="mt-3 max-w-3xl text-sm leading-6 text-slate-600">
            支持筛选、方向/深度控制、Only
            Propagating、节点抽屉、展开、影响面和保存视图。当前使用 React Flow
            承载 200 节点内交互，支持缩放、拖拽、MiniMap 和 Fit View。
          </p>
        </div>
        <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
          <Link
            className={cn(buttonVariants({ variant: "outline" }))}
            to="/topology/configuration#k8s-import"
          >
            数据源导入 / 拓扑配置
          </Link>
          <div className="grid grid-cols-3 gap-3 rounded-2xl border border-slate-200 bg-white p-3 text-center shadow-sm">
            <Metric label="节点" value={activeGraph.nodes.length} />
            <Metric label="关系" value={activeGraph.edges.length} />
            <Metric label="视图" value={viewsQuery.data?.length ?? 0} />
          </div>
        </div>
      </section>

      {(notice || error || truncated) && (
        <div
          role="status"
          className={cn(
            "rounded-xl border px-4 py-3 text-sm",
            error
              ? "border-rose-200 bg-rose-50 text-rose-700"
              : truncated
                ? "border-amber-200 bg-amber-50 text-amber-800"
                : "border-emerald-200 bg-emerald-50 text-emerald-700",
          )}
        >
          {error ??
            (truncated
              ? "结果已截断：请降低深度、缩小范围或增加 maxNodes 后重试。"
              : notice)}
        </div>
      )}

      <div className="grid gap-6 2xl:grid-cols-[360px_minmax(0,1fr)_380px]">
        <div className="space-y-6">
          <FilterCard
            query={query}
            loading={loading}
            onChange={patchQuery}
            onExpand={() => expandMutation.mutate(currentExpandInput())}
            onBlast={() =>
              blastMutation.mutate({
                nodeKey: query.nodeKey.trim(),
                direction: query.direction,
                hops: query.depth,
                maxNodes: query.maxNodes,
              })
            }
            onResetGraph={() => {
              setExpanded(null);
              setBlast(null);
              setGraphMode("graph");
              setSelectedNodeKey(null);
            }}
          />

          <SavedViewsCard
            views={viewsQuery.data ?? []}
            loading={viewsQuery.isLoading}
            viewName={viewName}
            visibility={visibility}
            saving={saveMutation.isPending}
            onViewNameChange={setViewName}
            onVisibilityChange={setVisibility}
            onLoad={loadSavedView}
            onSave={saveCurrentView}
          />

          <ManualDrawCard
            nodeForm={nodeForm}
            edgeForm={edgeForm}
            editingNodeId={editingNodeId}
            editingEdgeId={editingEdgeId}
            selectedNode={selectedNode}
            selectedEdge={selectedEdge}
            nodes={activeGraph.nodes}
            saving={manualSaving}
            onNodeFormChange={(patch) =>
              setNodeForm((current) => ({ ...current, ...patch }))
            }
            onEdgeFormChange={(patch) =>
              setEdgeForm((current) => ({ ...current, ...patch }))
            }
            onSubmitNode={submitManualNode}
            onSubmitEdge={submitManualEdge}
            onResetNode={() => {
              setEditingNodeId(null);
              setNodeForm(emptyNodeForm);
            }}
            onResetEdge={() => {
              setEditingEdgeId(null);
              setEdgeForm(emptyEdgeForm);
            }}
            onLoadSelectedNode={() => {
              if (selectedNode) {
                loadNodeForEdit(selectedNode);
              }
            }}
            onLoadSelectedEdge={() => {
              if (selectedEdge) {
                loadEdgeForEdit(selectedEdge);
              }
            }}
            onDeleteNode={() => {
              if (editingNodeId) {
                deleteNodeMutation.mutate(editingNodeId);
              }
            }}
            onDeleteEdge={() => {
              if (editingEdgeId) {
                deleteEdgeMutation.mutate(editingEdgeId);
              }
            }}
          />
        </div>

        <TopologyCanvasCard
          graph={activeGraph}
          loading={loading}
          rootKey={expanded?.rootKey ?? blast?.rootKey ?? query.nodeKey}
          paths={expanded?.paths ?? []}
          selectedNodeKey={selectedNodeKey}
          selectedEdgeKey={selectedEdgeKey}
          onSelectNode={setSelectedNodeKey}
          onSelectEdge={(edgeKey) => setSelectedEdgeKey(edgeKey)}
          onConnect={createEdgeFromConnection}
        />

        <div className="space-y-6">
          <NodeDrawer
            node={selectedNode}
            onClose={() => setSelectedNodeKey(null)}
            onAnalyze={(nodeKey) => {
              patchQuery({ nodeKey });
              expandMutation.mutate({ ...currentExpandInput(), nodeKey });
            }}
          />

          <LegendCard kinds={kindLegend} edgeTypes={edgeLegend} />

          <PathsCard
            paths={expanded?.paths ?? []}
            selectedNodeKey={selectedNodeKey}
            onSelectNode={setSelectedNodeKey}
          />
        </div>
      </div>
    </div>
  );
}

function FilterCard({
  query,
  loading,
  onChange,
  onExpand,
  onBlast,
  onResetGraph,
}: {
  query: TopologyQueryState;
  loading: boolean;
  onChange: (patch: Partial<TopologyQueryState>) => void;
  onExpand: () => void;
  onBlast: () => void;
  onResetGraph: () => void;
}) {
  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-3">
          <div className="grid size-10 place-items-center rounded-xl bg-brand-50 text-brand-700">
            <Search className="size-5" aria-hidden="true" />
          </div>
          <div>
            <CardTitle>Filter / Expand</CardTitle>
            <CardDescription>
              按中心节点、范围和传播语义控制图谱。
            </CardDescription>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <Field label="中心 nodeKey">
          <Input
            value={query.nodeKey}
            onChange={(event) => onChange({ nodeKey: event.target.value })}
            placeholder="service:payment-api"
          />
        </Field>

        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Environment">
            <Input
              value={query.environment}
              onChange={(event) =>
                onChange({ environment: event.target.value })
              }
              placeholder="prod"
            />
          </Field>
          <Field label="Namespace">
            <Input
              value={query.namespace}
              onChange={(event) => onChange({ namespace: event.target.value })}
              placeholder="default"
            />
          </Field>
        </div>

        <Field label="Cluster">
          <Input
            value={query.cluster}
            onChange={(event) => onChange({ cluster: event.target.value })}
            placeholder="prod-a"
          />
        </Field>

        <div className="grid gap-3 sm:grid-cols-3">
          <Field label="Direction">
            <select
              value={query.direction}
              onChange={(event) =>
                onChange({ direction: event.target.value as TopologyDirection })
              }
              className="h-11 w-full rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <option value="both">both</option>
              <option value="upstream">upstream</option>
              <option value="downstream">downstream</option>
            </select>
          </Field>
          <Field label="Depth">
            <Input
              type="number"
              min={1}
              max={6}
              value={query.depth}
              onChange={(event) =>
                onChange({ depth: clampNumber(event.target.value, 1, 6) })
              }
            />
          </Field>
          <Field label="Max Nodes">
            <Input
              type="number"
              min={20}
              max={200}
              value={query.maxNodes}
              onChange={(event) =>
                onChange({ maxNodes: clampNumber(event.target.value, 20, 200) })
              }
            />
          </Field>
        </div>

        <label className="flex items-center gap-2 rounded-xl border border-slate-200 bg-slate-50 p-3 text-sm text-slate-700">
          <input
            type="checkbox"
            checked={query.onlyPropagating}
            onChange={(event) =>
              onChange({ onlyPropagating: event.target.checked })
            }
            className="size-4 rounded border-slate-300"
          />
          Only Propagating：只展示会传播影响的关系
        </label>

        <div className="grid gap-2">
          <Button
            onClick={onExpand}
            disabled={loading || !query.nodeKey.trim()}
          >
            {loading && <Loader2 className="size-4 animate-spin" />}
            Expand 拓扑
          </Button>
          <Button
            variant="outline"
            onClick={onBlast}
            disabled={loading || !query.nodeKey.trim()}
          >
            <Radius className="size-4" />
            Blast Radius
          </Button>
          <Button variant="ghost" onClick={onResetGraph}>
            返回全量图
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

function SavedViewsCard({
  views,
  loading,
  viewName,
  visibility,
  saving,
  onViewNameChange,
  onVisibilityChange,
  onLoad,
  onSave,
}: {
  views: TopologySavedView[];
  loading: boolean;
  viewName: string;
  visibility: TopologySavedView["visibility"];
  saving: boolean;
  onViewNameChange: (value: string) => void;
  onVisibilityChange: (value: TopologySavedView["visibility"]) => void;
  onLoad: (view: TopologySavedView) => void;
  onSave: () => void;
}) {
  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-3">
          <div className="grid size-10 place-items-center rounded-xl bg-violet-50 text-violet-700">
            <Save className="size-5" aria-hidden="true" />
          </div>
          <div>
            <CardTitle>Saved View</CardTitle>
            <CardDescription>保存当前查询参数与展示配置。</CardDescription>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <Field label="视图名称">
          <Input
            value={viewName}
            onChange={(event) => onViewNameChange(event.target.value)}
          />
        </Field>
        <Field label="可见性">
          <select
            value={visibility}
            onChange={(event) =>
              onVisibilityChange(
                event.target.value as TopologySavedView["visibility"],
              )
            }
            className="h-11 w-full rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <option value="private">private</option>
            <option value="team">team</option>
            <option value="public">public</option>
          </select>
        </Field>
        <Button
          className="w-full"
          onClick={onSave}
          disabled={saving || viewName.trim() === ""}
        >
          {saving && <Loader2 className="size-4 animate-spin" />}
          保存当前视图
        </Button>

        <div className="space-y-2">
          {loading && <InlineHint text="加载保存视图..." />}
          {!loading && views.length === 0 && (
            <InlineHint text="暂无保存视图。" />
          )}
          {views.map((view) => (
            <button
              type="button"
              key={view.id}
              onClick={() => onLoad(view)}
              className="w-full rounded-xl border border-slate-200 bg-white p-3 text-left text-sm transition-colors hover:bg-slate-50"
            >
              <div className="flex items-center justify-between gap-2">
                <span className="font-medium text-slate-900">{view.name}</span>
                <span className="rounded-full bg-slate-100 px-2 py-0.5 text-[11px] text-slate-500">
                  {view.visibility}
                </span>
              </div>
              <p className="mt-1 text-xs text-slate-500">
                {view.isDefault ? "默认视图 · " : ""}
                {new Date(view.updatedAt).toLocaleString()}
              </p>
            </button>
          ))}
        </div>
      </CardContent>
    </Card>
  );
}

function ManualDrawCard({
  nodeForm,
  edgeForm,
  editingNodeId,
  editingEdgeId,
  selectedNode,
  selectedEdge,
  nodes,
  saving,
  onNodeFormChange,
  onEdgeFormChange,
  onSubmitNode,
  onSubmitEdge,
  onResetNode,
  onResetEdge,
  onLoadSelectedNode,
  onLoadSelectedEdge,
  onDeleteNode,
  onDeleteEdge,
}: {
  nodeForm: ManualNodeForm;
  edgeForm: ManualEdgeForm;
  editingNodeId: number | null;
  editingEdgeId: number | null;
  selectedNode?: TopologyNode;
  selectedEdge?: TopologyEdge;
  nodes: TopologyNode[];
  saving: boolean;
  onNodeFormChange: (patch: Partial<ManualNodeForm>) => void;
  onEdgeFormChange: (patch: Partial<ManualEdgeForm>) => void;
  onSubmitNode: () => void;
  onSubmitEdge: () => void;
  onResetNode: () => void;
  onResetEdge: () => void;
  onLoadSelectedNode: () => void;
  onLoadSelectedEdge: () => void;
  onDeleteNode: () => void;
  onDeleteEdge: () => void;
}) {
  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-3">
          <div className="grid size-10 place-items-center rounded-xl bg-emerald-50 text-emerald-700">
            <Pencil className="size-5" aria-hidden="true" />
          </div>
          <div>
            <CardTitle>Manual Draw</CardTitle>
            <CardDescription>
              手工创建节点；在画布中拖拽节点连接点可绘制关系。
            </CardDescription>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="space-y-3 rounded-lg border border-slate-200 p-3">
          <div className="flex items-center justify-between gap-2">
            <SectionTitle>
              {editingNodeId ? "编辑节点" : "新建节点"}
            </SectionTitle>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={onLoadSelectedNode}
              disabled={!selectedNode || saving}
            >
              <Pencil className="size-4" />
              载入选中
            </Button>
          </div>
          <Field label="Name">
            <Input
              value={nodeForm.name}
              onChange={(event) =>
                onNodeFormChange({ name: event.target.value })
              }
              placeholder="payment-api"
            />
          </Field>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="Kind">
              <Input
                value={nodeForm.kind}
                onChange={(event) =>
                  onNodeFormChange({ kind: event.target.value })
                }
                placeholder="service"
              />
            </Field>
            <Field label="Environment">
              <Input
                value={nodeForm.environment}
                onChange={(event) =>
                  onNodeFormChange({ environment: event.target.value })
                }
                placeholder="prod"
              />
            </Field>
          </div>
          <Field label="Node Key">
            <Input
              value={nodeForm.nodeKey}
              onChange={(event) =>
                onNodeFormChange({ nodeKey: event.target.value })
              }
              placeholder="留空自动生成"
              disabled={Boolean(editingNodeId)}
            />
          </Field>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="Cluster">
              <Input
                value={nodeForm.cluster}
                onChange={(event) =>
                  onNodeFormChange({ cluster: event.target.value })
                }
              />
            </Field>
            <Field label="Namespace">
              <Input
                value={nodeForm.namespace}
                onChange={(event) =>
                  onNodeFormChange({ namespace: event.target.value })
                }
              />
            </Field>
          </div>
          <Field label="Display Name">
            <Input
              value={nodeForm.displayName}
              onChange={(event) =>
                onNodeFormChange({ displayName: event.target.value })
              }
            />
          </Field>
          <div className="grid grid-cols-3 gap-2">
            <Button
              type="button"
              onClick={onSubmitNode}
              disabled={saving || nodeForm.name.trim() === ""}
            >
              <Plus className="size-4" />
              保存
            </Button>
            <Button type="button" variant="outline" onClick={onResetNode}>
              重置
            </Button>
            <Button
              type="button"
              variant="outline"
              className="border-rose-200 text-rose-700 hover:bg-rose-50"
              onClick={onDeleteNode}
              disabled={saving || !editingNodeId}
            >
              <Trash2 className="size-4" />
              删除
            </Button>
          </div>
        </div>

        <div className="space-y-3 rounded-lg border border-slate-200 p-3">
          <div className="flex items-center justify-between gap-2">
            <SectionTitle>
              {editingEdgeId ? "编辑关系" : "新建关系"}
            </SectionTitle>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={onLoadSelectedEdge}
              disabled={!selectedEdge || saving}
            >
              <Pencil className="size-4" />
              载入选中
            </Button>
          </div>
          <Field label="From">
            <NodeKeySelect
              value={edgeForm.fromNodeKey}
              nodes={nodes}
              onChange={(value) => onEdgeFormChange({ fromNodeKey: value })}
            />
          </Field>
          <Field label="To">
            <NodeKeySelect
              value={edgeForm.toNodeKey}
              nodes={nodes}
              onChange={(value) => onEdgeFormChange({ toNodeKey: value })}
            />
          </Field>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="Relation Type">
              <Input
                value={edgeForm.edgeType}
                onChange={(event) =>
                  onEdgeFormChange({ edgeType: event.target.value })
                }
                placeholder="depends_on"
              />
            </Field>
            <Field label="Confidence">
              <Input
                type="number"
                min={0}
                max={1}
                step={0.1}
                value={edgeForm.confidence}
                onChange={(event) =>
                  onEdgeFormChange({ confidence: event.target.value })
                }
              />
            </Field>
          </div>
          <div className="grid grid-cols-3 gap-2">
            <Button
              type="button"
              onClick={onSubmitEdge}
              disabled={
                saving ||
                edgeForm.fromNodeKey.trim() === "" ||
                edgeForm.toNodeKey.trim() === ""
              }
            >
              <Plus className="size-4" />
              保存
            </Button>
            <Button type="button" variant="outline" onClick={onResetEdge}>
              重置
            </Button>
            <Button
              type="button"
              variant="outline"
              className="border-rose-200 text-rose-700 hover:bg-rose-50"
              onClick={onDeleteEdge}
              disabled={saving || !editingEdgeId}
            >
              <Trash2 className="size-4" />
              删除
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

function NodeKeySelect({
  value,
  nodes,
  onChange,
}: {
  value: string;
  nodes: TopologyNode[];
  onChange: (value: string) => void;
}) {
  return (
    <select
      value={value}
      onChange={(event) => onChange(event.target.value)}
      className="h-11 w-full rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
    >
      <option value="">选择节点</option>
      {nodes.map((node) => (
        <option key={node.nodeKey} value={node.nodeKey}>
          {node.name} · {node.nodeKey}
        </option>
      ))}
    </select>
  );
}

function TopologyCanvasCard({
  graph,
  loading,
  rootKey,
  paths,
  selectedNodeKey,
  selectedEdgeKey,
  onSelectNode,
  onSelectEdge,
  onConnect,
}: {
  graph: TopologyGraph;
  loading: boolean;
  rootKey: string;
  paths: ExpandTopologyResult["paths"];
  selectedNodeKey: string | null;
  selectedEdgeKey: string | null;
  onSelectNode: (nodeKey: string) => void;
  onSelectEdge: (edgeKey: string) => void;
  onConnect: (connection: Connection) => void;
}) {
  const { nodes, edges } = useMemo(
    () =>
      buildFlowElements(
        graph,
        rootKey,
        paths,
        selectedNodeKey,
        selectedEdgeKey,
      ),
    [graph, paths, rootKey, selectedNodeKey, selectedEdgeKey],
  );

  return (
    <Card className="overflow-hidden">
      <CardHeader className="border-b border-slate-200 bg-white/80">
        <div className="flex flex-col justify-between gap-3 md:flex-row md:items-center">
          <div className="flex items-center gap-3">
            <div className="grid size-10 place-items-center rounded-xl bg-slate-900 text-brand-200">
              <Network className="size-5" aria-hidden="true" />
            </div>
            <div>
              <CardTitle>Topology Map</CardTitle>
              <CardDescription>
                React Flow 画布，点击节点打开
                Drawer；关系箭头表示依赖/调用方向。
              </CardDescription>
            </div>
          </div>
          {loading && (
            <span className="inline-flex items-center gap-2 text-sm text-slate-500">
              <Loader2 className="size-4 animate-spin" />
              刷新图谱中...
            </span>
          )}
        </div>
      </CardHeader>
      <CardContent className="p-0">
        <div className="h-[720px] bg-slate-50">
          {graph.nodes.length === 0 ? (
            <div className="grid h-full place-items-center p-8 text-center text-sm text-slate-500">
              <div className="space-y-4">
                <p>
                  暂无拓扑数据，可从已保存的 K8s
                  数据源导入，或手工维护节点和关系。
                </p>
                <Link
                  to="/topology/configuration#k8s-import"
                  className={cn(buttonVariants({ variant: "default" }))}
                >
                  导入 K8s 拓扑
                </Link>
              </div>
            </div>
          ) : (
            <ReactFlow
              nodes={nodes}
              edges={edges}
              nodeTypes={topologyNodeTypes}
              fitView
              fitViewOptions={{ padding: 0.22, maxZoom: 1.2 }}
              minZoom={0.2}
              maxZoom={1.8}
              nodesDraggable
              nodesConnectable
              elementsSelectable
              onConnect={onConnect}
              onNodeClick={(_, node) => onSelectNode(node.id)}
              onEdgeClick={(_, edge) => onSelectEdge(edge.id)}
            >
              <Background gap={24} color="#cbd5e1" />
              <Controls showInteractive={false} />
              <MiniMap
                pannable
                zoomable
                nodeColor={(node) =>
                  node.id === rootKey
                    ? "#f59e0b"
                    : node.id === selectedNodeKey
                      ? "#c6a86f"
                      : "#94a3b8"
                }
                className="!bg-white/90"
              />
            </ReactFlow>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

function TopologyFlowNodeCard({ data, selected }: NodeProps<TopologyFlowNode>) {
  const node = data.topologyNode;
  return (
    <div
      className={cn(
        "relative w-[220px] rounded-lg border bg-white px-4 py-3 shadow-sm transition-shadow",
        selected
          ? "border-brand-400 shadow-brand-100"
          : data.isRoot
            ? "border-amber-400"
            : "border-slate-200",
      )}
    >
      <Handle
        type="target"
        position={Position.Left}
        className="!size-2 !border-slate-300 !bg-white"
      />
      <Handle
        type="source"
        position={Position.Right}
        className="!size-2 !border-slate-300 !bg-white"
      />
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="truncate text-sm font-semibold text-slate-950">
            {node.displayName || node.name}
          </p>
          <p className="mt-1 truncate font-mono text-[11px] text-slate-500">
            {node.nodeKey}
          </p>
        </div>
        <StatusDots node={node} />
      </div>
      <div className="mt-3 flex items-center justify-between gap-3">
        <span className="truncate rounded-full border border-brand-200 bg-brand-50 px-2 py-0.5 text-[11px] font-medium text-brand-700">
          {node.kind}
        </span>
        <span className="truncate text-[11px] text-slate-500">
          {node.namespace || node.environment || node.sourceType}
        </span>
      </div>
    </div>
  );
}

function StatusDots({ node }: { node: TopologyNode }) {
  const status = nodeStatus(node);
  const alerting = hasFlag(node, ["alert", "alerting", "hasAlert"]);
  const changed = hasFlag(node, ["change", "changed", "recentChange"]);
  return (
    <div
      className="mt-1 flex shrink-0 items-center gap-1.5"
      aria-label="节点状态徽标"
    >
      <span
        className={cn(
          "size-2.5 rounded-full",
          status === "healthy" ? "bg-emerald-500" : "bg-orange-500",
        )}
      />
      {alerting && <span className="size-2.5 rounded-full bg-rose-500" />}
      {changed && <span className="size-2.5 rounded-full bg-violet-500" />}
    </div>
  );
}

function NodeDrawer({
  node,
  onClose,
  onAnalyze,
}: {
  node?: TopologyNode;
  onClose: () => void;
  onAnalyze: (nodeKey: string) => void;
}) {
  if (!node) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Node Drawer</CardTitle>
          <CardDescription>点击拓扑图中的节点查看详情。</CardDescription>
        </CardHeader>
        <CardContent>
          <InlineHint text="未选择节点。" />
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex items-start justify-between gap-3">
          <div>
            <CardTitle>{node.displayName || node.name}</CardTitle>
            <CardDescription className="mt-1 font-mono">
              {node.nodeKey}
            </CardDescription>
          </div>
          <Button
            variant="ghost"
            size="icon"
            onClick={onClose}
            aria-label="关闭节点详情"
          >
            <X className="size-4" />
          </Button>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex flex-wrap gap-2">
          <StatusBadge
            icon={<CheckCircle2 className="size-3.5" />}
            text={nodeStatus(node)}
            tone="green"
          />
          <StatusBadge
            icon={<BellRing className="size-3.5" />}
            text={
              hasFlag(node, ["alert", "alerting", "hasAlert"])
                ? "alert"
                : "no alert"
            }
            tone="red"
          />
          <StatusBadge
            icon={<Zap className="size-3.5" />}
            text={
              hasFlag(node, ["change", "changed", "recentChange"])
                ? "changed"
                : "no change"
            }
            tone="violet"
          />
        </div>

        <div className="grid gap-2 text-sm">
          <InfoRow label="Kind" value={node.kind} />
          <InfoRow label="Env" value={node.environment || "-"} />
          <InfoRow label="Cluster" value={node.cluster || "-"} />
          <InfoRow label="Namespace" value={node.namespace || "-"} />
          <InfoRow label="Source" value={node.sourceType} />
        </div>

        <Button className="w-full" onClick={() => onAnalyze(node.nodeKey)}>
          <GitBranch className="size-4" />
          点击节点发起分析
        </Button>
        <Link
          className={cn(buttonVariants({ variant: "outline" }), "w-full")}
          to={`/analysis?nodeKey=${encodeURIComponent(node.nodeKey)}`}
        >
          跳转智能分析
        </Link>

        <JsonBlock title="Labels" value={node.labels} />
        <JsonBlock title="Properties" value={node.properties} />
      </CardContent>
    </Card>
  );
}

function LegendCard({
  kinds,
  edgeTypes,
}: {
  kinds: string[];
  edgeTypes: string[];
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>图例</CardTitle>
        <CardDescription>节点、关系与健康/告警/变更 Badge。</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div>
          <p className="mb-2 text-xs font-semibold uppercase tracking-wide text-slate-500">
            Nodes
          </p>
          <div className="flex flex-wrap gap-2">
            {(kinds.length ? kinds : ["service", "pod", "database"]).map(
              (kind, index) => (
                <span
                  key={kind}
                  className={cn(
                    "rounded-full border px-2.5 py-1 text-xs font-medium",
                    nodePalette[index % nodePalette.length],
                  )}
                >
                  {kind}
                </span>
              ),
            )}
          </div>
        </div>
        <div>
          <p className="mb-2 text-xs font-semibold uppercase tracking-wide text-slate-500">
            Relations
          </p>
          <div className="flex flex-wrap gap-2">
            {(edgeTypes.length ? edgeTypes : ["depends_on", "calls"]).map(
              (type) => (
                <span
                  key={type}
                  className="rounded-full border border-slate-200 bg-slate-50 px-2.5 py-1 text-xs text-slate-600"
                >
                  → {type}
                </span>
              ),
            )}
          </div>
        </div>
        <div className="grid gap-2 text-xs text-slate-600">
          <BadgeLegend color="bg-emerald-500" text="健康" />
          <BadgeLegend color="bg-rose-500" text="告警" />
          <BadgeLegend color="bg-violet-500" text="近期变更" />
        </div>
      </CardContent>
    </Card>
  );
}

function PathsCard({
  paths,
  selectedNodeKey,
  onSelectNode,
}: {
  paths: ExpandTopologyResult["paths"];
  selectedNodeKey: string | null;
  onSelectNode: (nodeKey: string) => void;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>传播路径</CardTitle>
        <CardDescription>Expand 返回的扁平化路径与置信度。</CardDescription>
      </CardHeader>
      <CardContent className="space-y-2">
        {paths.length === 0 && (
          <InlineHint text="暂无路径结果，执行 Expand 后显示。" />
        )}
        {paths.slice(0, 10).map((path) => (
          <button
            key={`${path.targetNodeKey}-${path.edgeKeys.join(",")}`}
            type="button"
            onClick={() => onSelectNode(path.targetNodeKey)}
            className={cn(
              "w-full rounded-xl border p-3 text-left text-sm transition-colors",
              selectedNodeKey === path.targetNodeKey
                ? "border-brand-300 bg-brand-50"
                : "border-slate-200 bg-white hover:bg-slate-50",
            )}
          >
            <div className="flex items-center justify-between gap-2">
              <span className="font-medium text-slate-900">
                {truncate(path.targetNodeKey, 32)}
              </span>
              <span className="rounded-full bg-slate-100 px-2 py-0.5 text-[11px] text-slate-500">
                {path.hops} hops
              </span>
            </div>
            <p className="mt-1 text-xs text-slate-500">
              {path.impactType || "unknown"} · confidence{" "}
              {(path.confidence * 100).toFixed(0)}%
            </p>
          </button>
        ))}
      </CardContent>
    </Card>
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
    <p className="text-xs font-semibold uppercase tracking-wide text-slate-500">
      {children}
    </p>
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

function InlineHint({ text }: { text: string }) {
  return (
    <div className="rounded-xl border border-dashed border-slate-200 bg-slate-50 p-4 text-sm text-slate-500">
      {text}
    </div>
  );
}

function StatusBadge({
  icon,
  text,
  tone,
}: {
  icon: ReactNode;
  text: string;
  tone: "green" | "red" | "violet";
}) {
  const classes = {
    green: "border-emerald-200 bg-emerald-50 text-emerald-700",
    red: "border-rose-200 bg-rose-50 text-rose-700",
    violet: "border-violet-200 bg-violet-50 text-violet-700",
  };
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium",
        classes[tone],
      )}
    >
      {icon}
      {text}
    </span>
  );
}

function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-lg bg-slate-50 px-3 py-2">
      <span className="text-slate-500">{label}</span>
      <span className="truncate font-medium text-slate-800">{value}</span>
    </div>
  );
}

function JsonBlock({
  title,
  value,
}: {
  title: string;
  value?: Record<string, unknown>;
}) {
  if (!value || Object.keys(value).length === 0) {
    return null;
  }
  return (
    <div>
      <p className="mb-2 text-xs font-semibold uppercase tracking-wide text-slate-500">
        {title}
      </p>
      <pre className="max-h-48 overflow-auto rounded-xl bg-slate-950 p-3 text-xs leading-5 text-slate-100">
        {JSON.stringify(value, null, 2)}
      </pre>
    </div>
  );
}

function BadgeLegend({ color, text }: { color: string; text: string }) {
  return (
    <div className="flex items-center gap-2">
      <span className={cn("size-2.5 rounded-full", color)} />
      <span>{text}</span>
    </div>
  );
}

function buildFlowElements(
  graph: TopologyGraph,
  rootKey: string,
  paths: ExpandTopologyResult["paths"],
  selectedNodeKey: string | null,
  selectedEdgeKey: string | null,
) {
  const layoutNodes = buildLayeredLayout(graph, rootKey, paths);
  const visibleKeys = new Set(layoutNodes.map((node) => node.nodeKey));
  const nodes: TopologyFlowNode[] = layoutNodes.map((node) => ({
    id: node.nodeKey,
    type: "topologyNode",
    position: { x: node.x, y: node.y },
    data: {
      topologyNode: node,
      isRoot: node.nodeKey === rootKey,
    },
    selected: node.nodeKey === selectedNodeKey,
    draggable: true,
  }));
  const edges: FlowEdge[] = graph.edges
    .filter(
      (edge) =>
        visibleKeys.has(edge.fromNodeKey) && visibleKeys.has(edge.toNodeKey),
    )
    .map((edge) => {
      const selected =
        selectedEdgeKey === edgeKeyOf(edge) ||
        selectedNodeKey === edge.fromNodeKey ||
        selectedNodeKey === edge.toNodeKey;
      return {
        id: edgeKeyOf(edge),
        source: edge.fromNodeKey,
        target: edge.toNodeKey,
        label: edge.edgeType,
        type: "smoothstep",
        animated: selected,
        markerEnd: {
          type: MarkerType.ArrowClosed,
          color: selected ? "#c6a86f" : "#64748b",
        },
        style: {
          stroke: selected ? "#c6a86f" : "#94a3b8",
          strokeWidth: selected ? 2.4 : 1.5,
        },
        labelStyle: {
          fill: "#475569",
          fontSize: 11,
          fontWeight: 500,
        },
        labelBgStyle: {
          fill: "#ffffff",
          fillOpacity: 0.86,
        },
      };
    });
  return { nodes, edges };
}

function buildLayeredLayout(
  graph: TopologyGraph,
  rootKey: string,
  paths: ExpandTopologyResult["paths"],
) {
  const levelByKey = new Map<string, number>();
  for (const path of paths) {
    path.nodeKeys.forEach((key, index) => {
      const existing = levelByKey.get(key);
      if (existing === undefined || index < existing) {
        levelByKey.set(key, index);
      }
    });
  }
  if (!levelByKey.has(rootKey)) {
    levelByKey.set(rootKey, 0);
  }

  for (const edge of graph.edges) {
    if (levelByKey.has(edge.fromNodeKey) && !levelByKey.has(edge.toNodeKey)) {
      levelByKey.set(
        edge.toNodeKey,
        (levelByKey.get(edge.fromNodeKey) ?? 0) + 1,
      );
    }
  }

  const fallbackColumns = Math.max(1, Math.ceil(Math.sqrt(graph.nodes.length)));
  const groups = new Map<number, TopologyNode[]>();
  graph.nodes.forEach((node, index) => {
    const level = levelByKey.get(node.nodeKey) ?? index % fallbackColumns;
    const group = groups.get(level) ?? [];
    group.push(node);
    groups.set(level, group);
  });

  const nodes: Array<TopologyNode & { x: number; y: number }> = [];
  const xGap = 330;
  const yGap = 126;
  Array.from(groups.entries())
    .sort(([leftLevel], [rightLevel]) => leftLevel - rightLevel)
    .forEach(([level, group]) => {
      group
        .sort((leftNode, rightNode) =>
          leftNode.nodeKey.localeCompare(rightNode.nodeKey),
        )
        .forEach((node, row) => {
          nodes.push({
            ...node,
            x: level * xGap,
            y: row * yGap,
          });
        });
    });
  return nodes;
}

function normalizeObject(value: unknown): Record<string, unknown> {
  if (typeof value === "string") {
    try {
      const parsed = JSON.parse(value) as unknown;
      return normalizeObject(parsed);
    } catch {
      return {};
    }
  }
  if (value && typeof value === "object" && !Array.isArray(value)) {
    return value as Record<string, unknown>;
  }
  return {};
}

function stringValue(value: unknown, fallback: string) {
  return typeof value === "string" ? value : fallback;
}

function numberValue(value: unknown, fallback: number) {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

function directionValue(
  value: unknown,
  fallback: TopologyDirection,
): TopologyDirection {
  return value === "upstream" || value === "downstream" || value === "both"
    ? value
    : fallback;
}

function edgeKeyOf(edge: TopologyEdge) {
  return (
    edge.edgeKey || `${edge.fromNodeKey}->${edge.toNodeKey}:${edge.edgeType}`
  ).toLowerCase();
}

function clampNumber(value: string, min: number, max: number) {
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) {
    return min;
  }
  return Math.min(max, Math.max(min, parsed));
}

function truncate(value: string, maxLength: number) {
  return value.length > maxLength ? `${value.slice(0, maxLength - 1)}…` : value;
}

function nodeStatus(node: TopologyNode) {
  const data = { ...node.labels, ...node.properties };
  const status = String(
    data.status ?? data.health ?? data.healthStatus ?? "",
  ).toLowerCase();
  if (["healthy", "ok", "ready", "running"].includes(status)) {
    return "healthy";
  }
  if (["unhealthy", "error", "critical", "warning"].includes(status)) {
    return "warning";
  }
  return "unknown";
}

function hasFlag(node: TopologyNode, keys: string[]) {
  const data = { ...node.labels, ...node.properties };
  return keys.some((key) => {
    const value = data[key];
    return value === true || value === "true" || value === "yes" || value === 1;
  });
}
