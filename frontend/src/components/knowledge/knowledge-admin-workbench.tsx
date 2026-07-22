import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Braces,
  CheckCircle2,
  DatabaseZap,
  FileStack,
  FlaskConical,
  Layers3,
  Loader2,
  Plus,
  Rocket,
  ShieldCheck,
  UploadCloud,
} from "lucide-react";

import {
  buildEmbeddingIndex,
  chunkDocumentVersion,
  createEmbeddingIndex,
  createQualityEvaluation,
  createRetrievalTestCase,
  createStructuredQualityStandard,
  getEmbeddingStatus,
  getParsedStructure,
  getPublicationGate,
  listChunkStrategies,
  listDocumentVersions,
  listStructuredQualityStandards,
  listRetrievalTestCases,
  listVersionChunks,
  parseDocumentVersion,
  publishDocumentVersion,
  publishQualityEvaluation,
  publicationGateLabel,
  publishStructuredQualityStandard,
  retryEmbeddingIndex,
  runRetrievalLab,
  runRetrievalSmoke,
  toAPIErrorMessage,
  toPublicationGateErrorMessage,
  uploadDocumentVersion,
  type DocumentBlock,
  type KnowledgeDocument,
  type RetrievalEvaluationRun,
  type StructuredQualityCriterion,
  type StructuredQualityStandard,
} from "@/api/knowledge";
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

type Tab = "document" | "standard" | "retrieval";

const initialCriterion: StructuredQualityCriterion = {
  criterionKey: "completeness",
  name: "内容完整性",
  weight: 1,
  maxScore: 100,
  scoringMethod: "rule",
  order: 1,
  rules: [
    {
      ruleKey: "required_sections",
      name: "必需章节",
      ruleType: "section_presence",
      severity: "high",
      maxScore: 100,
      required: true,
      hardGate: false,
      description: "Runbook 必须包含操作、验证和回滚所需章节。",
      evidenceRequirement: { required: "block" },
      detectorConfig: { sections: ["步骤", "验证", "回滚"] },
      order: 1,
    },
  ],
};

export function KnowledgeAdminWorkbench({
  document,
}: {
  document: KnowledgeDocument | null;
}) {
  const [tab, setTab] = useState<Tab>("document");
  return (
    <Card className="border-slate-300 bg-slate-950 text-slate-50 shadow-none">
      <CardHeader>
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div>
            <CardTitle className="flex items-center gap-2 text-white">
              <ShieldCheck className="size-5 text-cyan-300" /> 管理工作台
            </CardTitle>
            <CardDescription className="mt-2 text-slate-400">
              版本、AST、评分标准、Chunk、Embedding、Retrieval 与发布门禁。
            </CardDescription>
          </div>
          <div className="flex rounded-lg bg-white/5 p-1">
            {(
              [
                ["document", "文档流水线"],
                ["standard", "标准 Builder"],
                ["retrieval", "Retrieval Lab"],
              ] as const
            ).map(([value, label]) => (
              <button
                key={value}
                type="button"
                onClick={() => setTab(value)}
                className={cn(
                  "rounded-md px-3 py-2 text-xs font-medium",
                  tab === value
                    ? "bg-cyan-300 text-slate-950"
                    : "text-slate-400 hover:text-white",
                )}
              >
                {label}
              </button>
            ))}
          </div>
        </div>
      </CardHeader>
      <CardContent>
        {tab === "document" && <DocumentPipeline document={document} />}
        {tab === "standard" && <StandardBuilder />}
        {tab === "retrieval" && <RetrievalLab document={document} />}
      </CardContent>
    </Card>
  );
}

function DocumentPipeline({
  document,
}: {
  document: KnowledgeDocument | null;
}) {
  const queryClient = useQueryClient();
  const [versionID, setVersionID] = useState<number>();
  const [strategyID, setStrategyID] = useState<number>();
  const [versionLabel, setVersionLabel] = useState("v2.0");
  const [versionFile, setVersionFile] = useState<File | null>(null);
  const [embeddingConfig, setEmbeddingConfig] = useState("");
  const [dimension, setDimension] = useState("1536");
  const [qualityProfileID, setQualityProfileID] = useState("");
  const [evaluationMode, setEvaluationMode] = useState<
    "deterministic" | "hybrid" | "llm"
  >("hybrid");
  const [evaluationID, setEvaluationID] = useState<number>();
  const [message, setMessage] = useState("");

  const versions = useQuery({
    queryKey: ["knowledge", "versions", document?.id],
    queryFn: () => listDocumentVersions(document!.id),
    enabled: Boolean(document),
  });
  const strategies = useQuery({
    queryKey: ["knowledge", "chunk-strategies"],
    queryFn: listChunkStrategies,
  });

  useEffect(() => {
    const items = versions.data ?? [];
    if (!items.length) {
      setVersionID(undefined);
      return;
    }
    if (!items.some((item) => item.id === versionID)) {
      setVersionID(document?.currentPublishedVersionId ?? items[0].id);
    }
  }, [document?.currentPublishedVersionId, versionID, versions.data]);
  useEffect(() => {
    if (!strategyID && strategies.data?.[0])
      setStrategyID(strategies.data[0].id);
  }, [strategies.data, strategyID]);

  const parsed = useQuery({
    queryKey: ["knowledge", "parsed", versionID],
    queryFn: () => getParsedStructure(versionID!),
    enabled: Boolean(versionID),
    retry: false,
  });
  const chunks = useQuery({
    queryKey: ["knowledge", "version-chunks", versionID, strategyID],
    queryFn: () => listVersionChunks(versionID!, strategyID),
    enabled: Boolean(versionID && strategyID),
  });
  const embeddings = useQuery({
    queryKey: ["knowledge", "embedding-status", versionID, strategyID],
    queryFn: () => getEmbeddingStatus(versionID!, strategyID!),
    enabled: Boolean(versionID && strategyID),
    retry: false,
  });
  const gate = useQuery({
    queryKey: ["knowledge", "publication-gate", versionID],
    queryFn: () => getPublicationGate(versionID!),
    enabled: Boolean(versionID),
    retry: false,
  });

  const refreshPipeline = async () => {
    await Promise.all([
      queryClient.invalidateQueries({
        queryKey: ["knowledge", "versions", document?.id],
      }),
      queryClient.invalidateQueries({
        queryKey: ["knowledge", "parsed", versionID],
      }),
      queryClient.invalidateQueries({
        queryKey: ["knowledge", "version-chunks", versionID],
      }),
      queryClient.invalidateQueries({
        queryKey: ["knowledge", "embedding-status", versionID],
      }),
      queryClient.invalidateQueries({
        queryKey: ["knowledge", "publication-gate", versionID],
      }),
    ]);
  };
  const upload = useMutation({
    mutationFn: () =>
      uploadDocumentVersion({
        documentId: document!.id,
        version: versionLabel,
        file: versionFile!,
      }),
    onSuccess: async (version) => {
      setVersionID(version.id);
      setVersionFile(null);
      setMessage("新版本已创建，历史版本未被覆盖。");
      await refreshPipeline();
    },
    onError: (error) => setMessage(toAPIErrorMessage(error)),
  });
  const parse = useMutation({
    mutationFn: () => parseDocumentVersion(versionID!),
    onSuccess: async (result) => {
      setVersionID(result.version.id);
      setMessage("解析完成，AST 与 Parse Quality 已刷新。");
      await refreshPipeline();
    },
    onError: (error) => setMessage(toAPIErrorMessage(error)),
  });
  const chunk = useMutation({
    mutationFn: () =>
      chunkDocumentVersion({ versionId: versionID!, strategyId: strategyID! }),
    onSuccess: async () => {
      setMessage("Chunk Set 已生成。");
      await refreshPipeline();
    },
    onError: (error) => setMessage(toAPIErrorMessage(error)),
  });
  const createIndex = useMutation({
    mutationFn: () =>
      createEmbeddingIndex({
        documentVersionId: versionID!,
        strategyId: strategyID!,
        embeddingConfigId: Number(embeddingConfig),
        dimension: Number(dimension),
      }),
    onSuccess: async () => {
      setMessage("Embedding Index Job 已创建。");
      await refreshPipeline();
    },
    onError: (error) => setMessage(toAPIErrorMessage(error)),
  });
  const buildIndex = useMutation({
    mutationFn: buildEmbeddingIndex,
    onSuccess: async () => {
      setMessage("Embedding Index 构建完成。");
      await refreshPipeline();
    },
    onError: async (error) => {
      setMessage(toAPIErrorMessage(error));
      await refreshPipeline();
    },
  });
  const retryIndex = useMutation({
    mutationFn: retryEmbeddingIndex,
    onSuccess: async () => {
      setMessage("Embedding Index 重试完成。");
      await refreshPipeline();
    },
    onError: async (error) => {
      setMessage(toAPIErrorMessage(error));
      await refreshPipeline();
    },
  });
  const evaluate = useMutation({
    mutationFn: () =>
      createQualityEvaluation({
        documentVersionId: versionID!,
        qualityProfileId: Number(qualityProfileID),
        mode: evaluationMode,
        force: true,
      }),
    onSuccess: async (evaluation) => {
      setEvaluationID(evaluation.id);
      setMessage(
        `质量评分 #${evaluation.id} 已完成，Gate: ${evaluation.gateStatus}。`,
      );
      await refreshPipeline();
    },
    onError: (error) => setMessage(toAPIErrorMessage(error)),
  });
  const publishEvaluation = useMutation({
    mutationFn: () => publishQualityEvaluation(evaluationID!),
    onSuccess: async () => {
      setMessage("评分证据已 Review 并发布。");
      await refreshPipeline();
    },
    onError: (error) => setMessage(toAPIErrorMessage(error)),
  });
  const publish = useMutation({
    mutationFn: () =>
      publishDocumentVersion(versionID!, "Knowledge Center 管理工作台发布"),
    onSuccess: async () => {
      setMessage("版本已发布，旧版本已转为 superseded。");
      await refreshPipeline();
      await queryClient.invalidateQueries({
        queryKey: ["knowledge", "documents"],
      });
    },
    onError: async (error) => {
      setMessage(toPublicationGateErrorMessage(error));
      await queryClient.invalidateQueries({
        queryKey: ["knowledge", "publication-gate", versionID],
      });
    },
  });

  if (!document) return <Empty text="请先从文档列表选择一份文档。" />;
  const selected = versions.data?.find((version) => version.id === versionID);

  return (
    <div className="space-y-5">
      {message && (
        <p
          role="status"
          className="rounded-lg bg-white/10 px-3 py-2 text-sm text-cyan-100"
        >
          {message}
        </p>
      )}
      <div className="grid gap-4 xl:grid-cols-[0.8fr_1.2fr]">
        <div className="space-y-4">
          <Panel
            title="Document Versions"
            icon={<FileStack className="size-4" />}
          >
            <select
              aria-label="文档版本"
              value={versionID ?? ""}
              onChange={(event) => setVersionID(Number(event.target.value))}
              className="w-full rounded-md border border-white/10 bg-slate-900 px-3 py-2 text-sm"
            >
              {(versions.data ?? []).map((version) => (
                <option key={version.id} value={version.id}>
                  {version.version} · r{version.revisionNo} · {version.status}
                </option>
              ))}
            </select>
            <div className="grid gap-2 sm:grid-cols-2">
              <Input
                className="border-white/10 bg-slate-900"
                value={versionLabel}
                onChange={(event) => setVersionLabel(event.target.value)}
                aria-label="新版本号"
              />
              <Input
                className="border-white/10 bg-slate-900"
                type="file"
                accept=".md,.txt,.docx,.xlsx,.pdf"
                onChange={(event) =>
                  setVersionFile(event.target.files?.[0] ?? null)
                }
                aria-label="新版本文件"
              />
            </div>
            <Button
              size="sm"
              onClick={() => upload.mutate()}
              disabled={
                !versionFile || !versionLabel.trim() || upload.isPending
              }
            >
              <UploadCloud className="size-4" /> 上传新版本
            </Button>
          </Panel>

          <Panel title="Parse & Chunk" icon={<Layers3 className="size-4" />}>
            <div className="flex flex-wrap gap-2">
              <Button
                size="sm"
                variant="outline"
                onClick={() => parse.mutate()}
                disabled={!versionID || parse.isPending}
              >
                解析 AST
              </Button>
              <select
                aria-label="Chunk Strategy"
                value={strategyID ?? ""}
                onChange={(event) => setStrategyID(Number(event.target.value))}
                className="rounded-md border border-white/10 bg-slate-900 px-3 text-sm"
              >
                {(strategies.data ?? []).map((strategy) => (
                  <option key={strategy.id} value={strategy.id}>
                    {strategy.name} · {strategy.version}
                  </option>
                ))}
              </select>
              <Button
                size="sm"
                variant="outline"
                onClick={() => chunk.mutate()}
                disabled={!versionID || !strategyID || chunk.isPending}
              >
                生成 Chunk
              </Button>
            </div>
            <p className="text-xs text-slate-400">
              Parse: {selected?.parseQuality?.level ?? "未解析"} · Blocks{" "}
              {parsed.data?.parseQuality.blockCount ?? 0} · Chunks{" "}
              {chunks.data?.chunkCount ?? 0}
            </p>
          </Panel>

          <Panel
            title="Embedding Status"
            icon={<DatabaseZap className="size-4" />}
          >
            <div className="grid gap-2 sm:grid-cols-2">
              <Input
                className="border-white/10 bg-slate-900"
                type="number"
                placeholder="Embedding Config ID"
                value={embeddingConfig}
                onChange={(event) => setEmbeddingConfig(event.target.value)}
              />
              <Input
                className="border-white/10 bg-slate-900"
                type="number"
                placeholder="Dimension"
                value={dimension}
                onChange={(event) => setDimension(event.target.value)}
              />
            </div>
            <Button
              size="sm"
              variant="outline"
              onClick={() => createIndex.mutate()}
              disabled={
                !versionID ||
                !strategyID ||
                !Number(embeddingConfig) ||
                !Number(dimension)
              }
            >
              创建 Index Job
            </Button>
            <div className="space-y-2">
              {(embeddings.data?.indexes ?? []).map((index) => (
                <div
                  key={index.id}
                  className="space-y-2 rounded-md bg-white/5 p-2 text-xs"
                >
                  <div className="flex items-center justify-between gap-2">
                    <span>
                      #{index.id} {index.modelName}@{index.modelRevision}
                    </span>
                    <span>
                      {index.status} · {index.embeddedCount}/{index.chunkCount}
                    </span>
                    {(index.status === "pending" ||
                      index.status === "stale") && (
                      <Button
                        size="sm"
                        onClick={() => buildIndex.mutate(index.id)}
                        disabled={buildIndex.isPending}
                      >
                        构建
                      </Button>
                    )}
                    {index.status === "failed" && (
                      <Button
                        size="sm"
                        onClick={() => retryIndex.mutate(index.id)}
                        disabled={retryIndex.isPending}
                      >
                        重试
                      </Button>
                    )}
                  </div>
                  {index.errorMessage && (
                    <p className="break-words text-rose-300">
                      {index.errorMessage}
                    </p>
                  )}
                </div>
              ))}
            </div>
          </Panel>
          <Panel
            title="Quality Evaluation"
            icon={<ShieldCheck className="size-4" />}
          >
            <div className="grid gap-2 sm:grid-cols-2">
              <Input
                className="border-white/10 bg-slate-900"
                type="number"
                placeholder="Published Profile ID"
                value={qualityProfileID}
                onChange={(event) => setQualityProfileID(event.target.value)}
              />
              <select
                value={evaluationMode}
                onChange={(event) =>
                  setEvaluationMode(event.target.value as typeof evaluationMode)
                }
                className="rounded-md border border-white/10 bg-slate-900 px-3 text-sm"
              >
                <option value="deterministic">deterministic</option>
                <option value="hybrid">hybrid</option>
                <option value="llm">llm</option>
              </select>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button
                size="sm"
                variant="outline"
                onClick={() => evaluate.mutate()}
                disabled={
                  !versionID || !Number(qualityProfileID) || evaluate.isPending
                }
              >
                运行评分
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={() => publishEvaluation.mutate()}
                disabled={!evaluationID || publishEvaluation.isPending}
              >
                发布评分 Review
              </Button>
              {evaluationID && (
                <a
                  href={`/knowledge/evaluations/${evaluationID}/review`}
                  className="inline-flex items-center rounded-md px-3 text-xs text-cyan-300 hover:bg-white/5"
                >
                  查看证据 #{evaluationID}
                </a>
              )}
            </div>
          </Panel>
        </div>

        <div className="space-y-4">
          <Panel title="Parsed AST" icon={<Braces className="size-4" />}>
            <div className="max-h-72 overflow-auto rounded-lg bg-black/20 p-3">
              {(parsed.data?.blocks ?? []).length ? (
                <BlockTree blocks={parsed.data!.blocks} />
              ) : (
                <Empty text="解析后在这里查看保留层级、页码和属性的 AST。" />
              )}
            </div>
          </Panel>
          <Panel title="Chunk Inspector" icon={<Layers3 className="size-4" />}>
            <div className="max-h-64 space-y-2 overflow-auto">
              {(chunks.data?.chunks ?? []).map((item) => (
                <div
                  key={item.id}
                  className="rounded-lg bg-white/5 p-3 text-xs text-slate-300"
                >
                  <p className="font-medium text-cyan-200">
                    ID {item.id} · #{item.chunkIndex} · {item.chunkType} ·{" "}
                    {item.sourceSection ?? "未命名章节"}
                  </p>
                  <p className="mt-1 whitespace-pre-wrap leading-5">
                    {item.content}
                  </p>
                  <p className="mt-2 text-slate-500">
                    blocks {JSON.stringify(item.sourceBlockIds)} · hash{" "}
                    {item.contentHash?.slice(0, 12)}
                  </p>
                </div>
              ))}
              {!chunks.data?.chunkCount && (
                <Empty text="选择 Strategy 并生成 Chunk Set 后可检查边界和来源。" />
              )}
            </div>
          </Panel>
          <Panel title="Publish Gate" icon={<Rocket className="size-4" />}>
            <div className="grid gap-2 sm:grid-cols-5">
              {(
                gate.data?.checks ??
                ["parse", "quality", "embedding", "retrieval", "review"].map(
                  (name) => ({ name, passed: false, message: "等待检查" }),
                )
              ).map((check) => (
                <div
                  key={check.name}
                  title={check.message}
                  className={cn(
                    "rounded-lg border p-2 text-center text-xs",
                    check.passed
                      ? "border-emerald-400/30 bg-emerald-400/10 text-emerald-200"
                      : "border-rose-400/30 bg-rose-400/10 text-rose-200",
                  )}
                >
                  {check.passed ? (
                    <CheckCircle2 className="mx-auto mb-1 size-4" />
                  ) : (
                    <span className="mb-1 block">●</span>
                  )}
                  {publicationGateLabel(check.name)}
                </div>
              ))}
            </div>
            {gate.data && !gate.data.canPublish && (
              <div className="space-y-1 text-xs text-rose-200">
                {gate.data.checks
                  .filter((check) => !check.passed)
                  .map((check) => (
                    <p key={check.name}>
                      {publicationGateLabel(check.name)}：{check.message}
                    </p>
                  ))}
              </div>
            )}
            <Button
              onClick={() => publish.mutate()}
              disabled={!gate.data?.canPublish || publish.isPending}
            >
              <Rocket className="size-4" /> 发布当前版本
            </Button>
          </Panel>
        </div>
      </div>
    </div>
  );
}

function StandardBuilder() {
  const queryClient = useQueryClient();
  const [name, setName] = useState("Runbook 质量标准");
  const [version, setVersion] = useState("v1.0");
  const [profileKey, setProfileKey] = useState("runbook_default");
  const [passScore, setPassScore] = useState("70");
  const [warningScore, setWarningScore] = useState("60");
  const [criterion, setCriterion] = useState(initialCriterion);
  const [message, setMessage] = useState("");
  const standards = useQuery({
    queryKey: ["knowledge", "structured-standards"],
    queryFn: listStructuredQualityStandards,
  });
  const standard: StructuredQualityStandard = useMemo(
    () => ({
      name,
      version,
      profiles: [
        {
          profileKey,
          name: `${name} Profile`,
          applicableDocTypes: ["runbook"],
          totalScore: criterion.maxScore,
          passScore: Number(passScore),
          warningScore: Number(warningScore),
          criteria: [criterion],
        },
      ],
    }),
    [criterion, name, passScore, profileKey, version, warningScore],
  );
  const thresholdsValid =
    Number.isFinite(Number(passScore)) &&
    Number.isFinite(Number(warningScore)) &&
    Number(warningScore) >= 0 &&
    Number(warningScore) <= Number(passScore) &&
    Number(passScore) <= criterion.maxScore;
  const create = useMutation({
    mutationFn: () => createStructuredQualityStandard(standard),
    onSuccess: async () => {
      setMessage("结构化评分标准草稿已保存。");
      await queryClient.invalidateQueries({
        queryKey: ["knowledge", "structured-standards"],
      });
    },
    onError: (error) => setMessage(toAPIErrorMessage(error)),
  });
  const publishStandard = useMutation({
    mutationFn: publishStructuredQualityStandard,
    onSuccess: async (item) => {
      setMessage(`标准 ${item.name} ${item.version} 已发布，可用于评分。`);
      await queryClient.invalidateQueries({
        queryKey: ["knowledge", "structured-standards"],
      });
    },
    onError: (error) => setMessage(toAPIErrorMessage(error)),
  });
  const rule = criterion.rules[0];
  return (
    <div className="grid gap-5 xl:grid-cols-[1fr_0.8fr]">
      <Panel
        title="Quality Standard Builder"
        icon={<Braces className="size-4" />}
      >
        {message && (
          <p role="status" className="rounded-md bg-white/10 p-2 text-xs">
            {message}
          </p>
        )}
        <div className="grid gap-3 sm:grid-cols-5">
          <DarkField label="标准名称">
            <Input
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
          </DarkField>
          <DarkField label="版本">
            <Input
              value={version}
              onChange={(event) => setVersion(event.target.value)}
            />
          </DarkField>
          <DarkField label="Profile Key">
            <Input
              value={profileKey}
              onChange={(event) => setProfileKey(event.target.value)}
            />
          </DarkField>
          <DarkField label="通过分数">
            <Input
              type="number"
              min="1"
              max={criterion.maxScore}
              step="0.1"
              value={passScore}
              onChange={(event) => setPassScore(event.target.value)}
            />
          </DarkField>
          <DarkField label="警告分数">
            <Input
              type="number"
              min="0"
              max={passScore || criterion.maxScore}
              step="0.1"
              value={warningScore}
              onChange={(event) => setWarningScore(event.target.value)}
            />
          </DarkField>
        </div>
        <div className="rounded-lg border border-white/10 p-3">
          <div className="mb-3 flex items-center gap-2 text-sm font-medium">
            <Plus className="size-4" /> Criterion
          </div>
          <div className="grid gap-3 sm:grid-cols-4">
            <DarkField label="Key">
              <Input
                value={criterion.criterionKey}
                onChange={(event) =>
                  setCriterion({
                    ...criterion,
                    criterionKey: event.target.value,
                  })
                }
              />
            </DarkField>
            <DarkField label="名称">
              <Input
                value={criterion.name}
                onChange={(event) =>
                  setCriterion({ ...criterion, name: event.target.value })
                }
              />
            </DarkField>
            <DarkField label="权重">
              <Input
                type="number"
                step="0.1"
                value={criterion.weight}
                onChange={(event) =>
                  setCriterion({
                    ...criterion,
                    weight: Number(event.target.value),
                  })
                }
              />
            </DarkField>
            <DarkField label="满分">
              <Input
                type="number"
                value={criterion.maxScore}
                onChange={(event) =>
                  setCriterion({
                    ...criterion,
                    maxScore: Number(event.target.value),
                    rules: [{ ...rule, maxScore: Number(event.target.value) }],
                  })
                }
              />
            </DarkField>
          </div>
          <div className="mt-3 rounded-md bg-white/5 p-3">
            <p className="mb-3 text-xs font-semibold uppercase tracking-wide text-cyan-300">
              Rule
            </p>
            <div className="grid gap-3 sm:grid-cols-3">
              <DarkField label="Rule Key">
                <Input
                  value={rule.ruleKey}
                  onChange={(event) =>
                    setCriterion({
                      ...criterion,
                      rules: [{ ...rule, ruleKey: event.target.value }],
                    })
                  }
                />
              </DarkField>
              <DarkField label="名称">
                <Input
                  value={rule.name}
                  onChange={(event) =>
                    setCriterion({
                      ...criterion,
                      rules: [{ ...rule, name: event.target.value }],
                    })
                  }
                />
              </DarkField>
              <DarkField label="类型">
                <select
                  value={rule.ruleType}
                  onChange={(event) =>
                    setCriterion({
                      ...criterion,
                      rules: [{ ...rule, ruleType: event.target.value }],
                    })
                  }
                  className="h-10 w-full rounded-md border border-white/10 bg-slate-900 px-3 text-sm"
                >
                  <option value="section_presence">section_presence</option>
                  <option value="pattern">pattern</option>
                  <option value="safety">safety</option>
                  <option value="semantic">semantic</option>
                  <option value="manual">manual</option>
                </select>
              </DarkField>
            </div>
            <label className="mt-3 flex items-center gap-2 text-xs">
              <input
                type="checkbox"
                checked={rule.hardGate}
                onChange={(event) =>
                  setCriterion({
                    ...criterion,
                    rules: [{ ...rule, hardGate: event.target.checked }],
                  })
                }
              />{" "}
              Hard Gate
            </label>
          </div>
        </div>
        <Button
          onClick={() => create.mutate()}
          disabled={
            !name.trim() ||
            !version.trim() ||
            !thresholdsValid ||
            create.isPending
          }
        >
          {create.isPending ? (
            <Loader2 className="size-4 animate-spin" />
          ) : (
            <CheckCircle2 className="size-4" />
          )}{" "}
          保存结构化草稿
        </Button>
      </Panel>
      <Panel title="标准版本" icon={<FileStack className="size-4" />}>
        <div className="space-y-2">
          {(standards.data ?? []).map((item) => (
            <div key={item.id} className="rounded-lg bg-white/5 p-3 text-sm">
              <div className="flex justify-between">
                <span>{item.name}</span>
                <span className="text-cyan-300">{item.status}</span>
              </div>
              <p className="mt-1 text-xs text-slate-500">
                {item.version} · {item.profiles?.length ?? 0} profiles
              </p>
              {(item.profiles ?? []).map((profile) => (
                <p
                  key={profile.id ?? profile.profileKey}
                  className="mt-1 text-xs text-slate-400"
                >
                  Profile ID {profile.id ?? "draft"} · {profile.profileKey} ·
                  通过 {profile.passScore} / 警告 {profile.warningScore}
                </p>
              ))}
              {item.status === "draft" && item.id && (
                <Button
                  className="mt-2"
                  size="sm"
                  variant="outline"
                  onClick={() => publishStandard.mutate(item.id!)}
                  disabled={publishStandard.isPending}
                >
                  发布标准
                </Button>
              )}
            </div>
          ))}
          {!standards.data?.length && <Empty text="暂无结构化标准。" />}
        </div>
      </Panel>
    </div>
  );
}

function RetrievalLab({ document }: { document: KnowledgeDocument | null }) {
  const [versionID, setVersionID] = useState("");
  const [embeddingID, setEmbeddingID] = useState("");
  const [rerankID, setRerankID] = useState("");
  const [strategyID, setStrategyID] = useState("");
  const [runs, setRuns] = useState<RetrievalEvaluationRun[]>([]);
  const [question, setQuestion] = useState("");
  const [category, setCategory] = useState<
    "title" | "core_step" | "error_code" | "scenario" | "irrelevant"
  >("title");
  const [expectedChunks, setExpectedChunks] = useState("");
  const [message, setMessage] = useState("");
  const versions = useQuery({
    queryKey: ["knowledge", "versions", document?.id],
    queryFn: () => listDocumentVersions(document!.id),
    enabled: Boolean(document),
  });
  const testCases = useQuery({
    queryKey: ["knowledge", "retrieval-test-cases", versionID],
    queryFn: () => listRetrievalTestCases(Number(versionID)),
    enabled: Boolean(positive(versionID)),
  });
  useEffect(() => {
    if (!versionID && versions.data?.[0])
      setVersionID(String(versions.data[0].id));
  }, [versionID, versions.data]);
  const config = {
    embeddingConfigId: positive(embeddingID),
    rerankConfigId: positive(rerankID),
    chunkStrategyId: positive(strategyID),
    limit: 5,
  };
  const smoke = useMutation({
    mutationFn: () =>
      runRetrievalSmoke({ documentVersionId: Number(versionID), ...config }),
    onSuccess: (run) => {
      setRuns([run]);
      setMessage("Smoke Test 已完成。");
    },
    onError: (error) => setMessage(toAPIErrorMessage(error)),
  });
  const lab = useMutation({
    mutationFn: () =>
      runRetrievalLab({
        documentVersionId: Number(versionID),
        variants: [
          {
            name: "lexical-only",
            disableEmbedding: true,
            disableRerank: true,
            limit: 5,
          },
          { name: "dense-no-rerank", ...config, disableRerank: true },
          { name: "dense-rerank", ...config },
        ],
      }),
    onSuccess: (items) => {
      setRuns(items);
      setMessage("三组检索配置对比完成。");
    },
    onError: (error) => setMessage(toAPIErrorMessage(error)),
  });
  const createCase = useMutation({
    mutationFn: () => {
      const chunkIDs = expectedChunks
        .split(",")
        .map((value) => Number(value.trim()))
        .filter((value) => Number.isInteger(value) && value > 0);
      const irrelevant = category === "irrelevant";
      return createRetrievalTestCase({
        documentId: document!.id,
        documentVersionId: Number(versionID),
        question,
        category,
        expectedDocumentIds: irrelevant ? [] : [document!.id],
        expectedChunkIds: irrelevant ? [] : chunkIDs,
        expectedSections: [],
        mustIncludeFacts: [],
        mustNotInclude: [],
        expectNoAnswer: irrelevant,
        source: "manual",
        enabled: true,
      });
    },
    onSuccess: async () => {
      setQuestion("");
      setExpectedChunks("");
      setMessage("评测问题已保存；Smoke 需要覆盖五个分类。");
      await testCases.refetch();
    },
    onError: (error) => setMessage(toAPIErrorMessage(error)),
  });
  if (!document) return <Empty text="选择文档后运行候选版本的检索评测。" />;
  return (
    <div className="space-y-4">
      {message && (
        <p role="status" className="rounded-md bg-white/10 p-2 text-sm">
          {message}
        </p>
      )}
      <div className="grid gap-3 md:grid-cols-4">
        <select
          aria-label="评测版本"
          value={versionID}
          onChange={(event) => setVersionID(event.target.value)}
          className="rounded-md border border-white/10 bg-slate-900 px-3 text-sm"
        >
          {(versions.data ?? []).map((version) => (
            <option key={version.id} value={version.id}>
              {version.version} · {version.status}
            </option>
          ))}
        </select>
        <Input
          placeholder="Embedding Config ID"
          value={embeddingID}
          onChange={(event) => setEmbeddingID(event.target.value)}
        />
        <Input
          placeholder="Rerank Config ID"
          value={rerankID}
          onChange={(event) => setRerankID(event.target.value)}
        />
        <Input
          placeholder="Strategy ID"
          value={strategyID}
          onChange={(event) => setStrategyID(event.target.value)}
        />
      </div>
      <div className="flex gap-2">
        <Button
          onClick={() => smoke.mutate()}
          disabled={!positive(versionID) || smoke.isPending}
        >
          <ShieldCheck className="size-4" /> Smoke Test
        </Button>
        <Button
          variant="outline"
          onClick={() => lab.mutate()}
          disabled={!positive(versionID) || lab.isPending}
        >
          <FlaskConical className="size-4" /> 对比 Dense / Lexical / Rerank
        </Button>
      </div>
      <Panel title="Smoke Test Cases" icon={<Plus className="size-4" />}>
        <div className="grid gap-3 md:grid-cols-[0.8fr_1.5fr_1fr_auto]">
          <select
            value={category}
            onChange={(event) =>
              setCategory(event.target.value as typeof category)
            }
            className="rounded-md border border-white/10 bg-slate-900 px-3 text-sm"
          >
            <option value="title">title</option>
            <option value="core_step">core_step</option>
            <option value="error_code">error_code</option>
            <option value="scenario">scenario</option>
            <option value="irrelevant">irrelevant</option>
          </select>
          <Input
            placeholder="评测问题"
            value={question}
            onChange={(event) => setQuestion(event.target.value)}
          />
          <Input
            placeholder="Expected Chunk IDs，逗号分隔"
            value={expectedChunks}
            onChange={(event) => setExpectedChunks(event.target.value)}
            disabled={category === "irrelevant"}
          />
          <Button
            onClick={() => createCase.mutate()}
            disabled={
              !question.trim() || !positive(versionID) || createCase.isPending
            }
          >
            添加问题
          </Button>
        </div>
        <p className="text-xs text-slate-500">
          当前版本 {testCases.data?.length ?? 0} 个问题 · 已覆盖{" "}
          {[
            ...new Set((testCases.data ?? []).map((item) => item.category)),
          ].join(", ") || "无"}
        </p>
      </Panel>
      <div className="grid gap-3 lg:grid-cols-3">
        {runs.map((run) => (
          <div
            key={run.id}
            className="rounded-lg border border-white/10 bg-white/5 p-3 text-xs"
          >
            <div className="flex justify-between text-sm font-medium">
              <span>{run.name}</span>
              <span
                className={run.passed ? "text-emerald-300" : "text-rose-300"}
              >
                {run.passed ? "PASS" : "FAIL"}
              </span>
            </div>
            <p className="mt-2 text-slate-300">
              Recall@K {pct(run.metrics.recallAtK)} · MRR {pct(run.metrics.mrr)}{" "}
              · nDCG {pct(run.metrics.ndcgAtK)}
            </p>
            <p className="mt-1 text-slate-500">
              Citation {pct(run.metrics.citationAccuracy)} · Embedding{" "}
              {run.embeddingModel ?? "off"} · Rerank {run.rerankModel ?? "off"}
            </p>
          </div>
        ))}
      </div>
    </div>
  );
}

function BlockTree({
  blocks,
  depth = 0,
}: {
  blocks: DocumentBlock[];
  depth?: number;
}) {
  return (
    <div className="space-y-2">
      {blocks.map((block) => (
        <div
          key={block.id}
          style={{ marginLeft: depth * 12 }}
          className="border-l border-cyan-300/20 pl-3 text-xs"
        >
          <p className="font-medium text-cyan-200">
            {block.type} · {block.id} {block.page ? `· page ${block.page}` : ""}
          </p>
          <p className="mt-1 whitespace-pre-wrap text-slate-300">
            {block.text}
          </p>
          {block.children?.length ? (
            <BlockTree blocks={block.children} depth={depth + 1} />
          ) : null}
        </div>
      ))}
    </div>
  );
}
function Panel({
  title,
  icon,
  children,
}: {
  title: string;
  icon: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="space-y-3 rounded-xl border border-white/10 bg-white/[0.035] p-4">
      <div className="flex items-center gap-2 text-sm font-semibold text-white">
        {icon}
        {title}
      </div>
      {children}
    </div>
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
    <label className="space-y-1.5 text-xs text-slate-400">
      <span>{label}</span>
      {children}
    </label>
  );
}
function Empty({ text }: { text: string }) {
  return (
    <p className="rounded-lg border border-dashed border-white/10 p-4 text-center text-xs text-slate-500">
      {text}
    </p>
  );
}
function positive(value: string) {
  const result = Number(value);
  return Number.isInteger(result) && result > 0 ? result : undefined;
}
function pct(value?: number) {
  return `${((value ?? 0) * 100).toFixed(1)}%`;
}
