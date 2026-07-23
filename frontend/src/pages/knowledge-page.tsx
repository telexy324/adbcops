import { FormEvent, ReactNode, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Archive,
  ArrowRight,
  Bot,
  CheckCircle2,
  FileText,
  FlaskConical,
  Loader2,
  MessageSquareText,
  RefreshCw,
  Send,
  ShieldCheck,
  UploadCloud,
  XCircle,
} from "lucide-react";
import { useNavigate } from "react-router-dom";

import {
  autoReviewQuality,
  askKnowledge,
  getDocumentChunks,
  listQualityStandards,
  listDocuments,
  reprocessDocument,
  runRetrievalLab,
  runRetrievalSmoke,
  reviewAction,
  reviewQuality,
  toAPIErrorMessage,
  uploadDocument,
  uploadQualityStandard,
  type AskResponse,
  type KnowledgeDocument,
  type QualityResult,
  type RetrievalEvaluationRun,
} from "@/api/knowledge";
import { Button } from "@/components/ui/button";
import { KnowledgeAdminWorkbench } from "@/components/knowledge/knowledge-admin-workbench";
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
import { getCurrentUser } from "@/api/client";

const defaultQualityJSON = JSON.stringify(
  {
    score: 85,
    summary: "结构清晰，排障步骤完整。",
    findings: ["包含系统范围", "包含处置步骤"],
    suggestions: ["补充负责人或维护人"],
  },
  null,
  2,
);

const statusTone: Record<string, string> = {
  draft: "bg-slate-100 text-slate-600",
  reviewing: "bg-amber-100 text-amber-700",
  published: "bg-emerald-100 text-emerald-700",
  rejected: "bg-rose-100 text-rose-700",
  archived: "bg-slate-200 text-slate-600",
  deprecated: "bg-violet-100 text-violet-700",
};

export function KnowledgePage() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [selectedID, setSelectedID] = useState<number | null>(null);
  const [uploadForm, setUploadForm] = useState({
    title: "",
    systemName: "",
    componentName: "",
    environment: "prod",
    docType: "runbook",
    version: "v1.0",
    tags: '["runbook"]',
  });
  const [file, setFile] = useState<File | null>(null);
  const [standardFile, setStandardFile] = useState<File | null>(null);
  const [standardTitle, setStandardTitle] = useState("");
  const [selectedStandardIDs, setSelectedStandardIDs] = useState<number[]>([]);
  const [useDefaultQuality, setUseDefaultQuality] = useState(true);
  const [qualityJSON, setQualityJSON] = useState(defaultQualityJSON);
  const [comment, setComment] = useState("质量达标，允许进入正式知识库。");
  const [question, setQuestion] = useState("数据库连接池耗尽时如何排查？");
  const [conversationID, setConversationID] = useState<number | undefined>();
  const [lastAnswer, setLastAnswer] = useState<AskResponse | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [evaluationID, setEvaluationID] = useState("");
  const [retrievalVersionID, setRetrievalVersionID] = useState("");
  const [retrievalEmbeddingID, setRetrievalEmbeddingID] = useState("");
  const [retrievalRerankID, setRetrievalRerankID] = useState("");
  const [retrievalStrategyID, setRetrievalStrategyID] = useState("");
  const [retrievalRevision, setRetrievalRevision] = useState("");
  const [retrievalRuns, setRetrievalRuns] = useState<RetrievalEvaluationRun[]>(
    [],
  );
  const isAdmin = getCurrentUser()?.role === "admin";

  const documentsQuery = useQuery({
    queryKey: ["knowledge", "documents"],
    queryFn: listDocuments,
  });

  const documents = documentsQuery.data ?? [];
  const standardsQuery = useQuery({
    queryKey: ["knowledge", "quality-standards"],
    queryFn: listQualityStandards,
    enabled: isAdmin,
  });
  const qualityStandards = standardsQuery.data ?? [];
  const selectedDocument = useMemo(
    () => documents.find((document) => document.id === selectedID) ?? null,
    [documents, selectedID],
  );

  const chunksQuery = useQuery({
    queryKey: ["knowledge", "chunks", selectedID],
    queryFn: () => getDocumentChunks(selectedID!),
    enabled: selectedID !== null && isAdmin,
  });

  const uploadMutation = useMutation({
    mutationFn: uploadDocument,
    onSuccess: (document) => {
      setSelectedID(document.id);
      setNotice("上传成功，下一步可以解析切片。");
      setError(null);
      void queryClient.invalidateQueries({
        queryKey: ["knowledge", "documents"],
      });
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const reprocessMutation = useMutation({
    mutationFn: reprocessDocument,
    onSuccess: () => {
      setNotice("解析切片完成。");
      setError(null);
      void Promise.all([
        queryClient.invalidateQueries({
          queryKey: ["knowledge", "chunks", selectedID],
        }),
        queryClient.invalidateQueries({
          queryKey: ["knowledge", "versions", selectedID],
        }),
      ]);
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const uploadStandardMutation = useMutation({
    mutationFn: uploadQualityStandard,
    onSuccess: (standard) => {
      setStandardTitle("");
      setStandardFile(null);
      setSelectedStandardIDs((current) => [...current, standard.id]);
      setNotice("评分标准上传成功。");
      setError(null);
      void queryClient.invalidateQueries({
        queryKey: ["knowledge", "quality-standards"],
      });
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const qualityMutation = useMutation({
    mutationFn: ({ id, result }: { id: number; result: QualityResult }) =>
      reviewQuality(id, result),
    onSuccess: (response) => {
      setSelectedID(response.document.id);
      setNotice(`质检完成，状态：${response.document.status}`);
      setError(null);
      void queryClient.invalidateQueries({
        queryKey: ["knowledge", "documents"],
      });
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const autoQualityMutation = useMutation({
    mutationFn: autoReviewQuality,
    onSuccess: (response) => {
      setSelectedID(response.document.id);
      if (response.qualityResult) {
        setQualityJSON(JSON.stringify(response.qualityResult, null, 2));
      }
      setNotice(`自动评分完成，状态：${response.document.status}`);
      setError(null);
      void queryClient.invalidateQueries({
        queryKey: ["knowledge", "documents"],
      });
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const actionMutation = useMutation({
    mutationFn: ({
      id,
      action,
      reviewComment,
    }: {
      id: number;
      action: "publish" | "reject" | "archive" | "deprecate";
      reviewComment: string;
    }) => reviewAction(id, action, reviewComment),
    onSuccess: (response) => {
      setSelectedID(response.document.id);
      setNotice(`审核动作已提交：${response.document.status}`);
      setError(null);
      void queryClient.invalidateQueries({
        queryKey: ["knowledge", "documents"],
      });
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const askMutation = useMutation({
    mutationFn: askKnowledge,
    onSuccess: (response) => {
      setLastAnswer(response);
      setConversationID(response.conversation.id);
      setNotice("问答完成，已写入会话与 QA 记录。");
      setError(null);
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const smokeMutation = useMutation({
    mutationFn: runRetrievalSmoke,
    onSuccess: (run) => {
      setRetrievalRuns([run]);
      setNotice(`Smoke Test 完成：${run.passed ? "通过" : "未通过"}`);
      setError(null);
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const labMutation = useMutation({
    mutationFn: runRetrievalLab,
    onSuccess: (runs) => {
      setRetrievalRuns(runs);
      setNotice("Retrieval Lab 对比完成。");
      setError(null);
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const retrievalVersion = Number(retrievalVersionID);
  const retrievalConfig = {
    embeddingConfigId: positiveNumber(retrievalEmbeddingID),
    embeddingModelRevision: retrievalRevision.trim() || undefined,
    rerankConfigId: positiveNumber(retrievalRerankID),
    chunkStrategyId: positiveNumber(retrievalStrategyID),
    limit: 5,
  };

  function submitUpload(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!file) {
      setError("请选择 .md、.txt、.docx 或 .xlsx 文件。");
      return;
    }
    uploadMutation.mutate({ file, ...uploadForm });
  }

  function submitStandard(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!standardFile) {
      setError("请选择评分标准文件。");
      return;
    }
    uploadStandardMutation.mutate({ file: standardFile, title: standardTitle });
  }

  function submitQuality(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selectedDocument) {
      return;
    }
    try {
      const parsed = JSON.parse(qualityJSON) as QualityResult;
      qualityMutation.mutate({ id: selectedDocument.id, result: parsed });
    } catch {
      setError("质检 JSON 格式不正确。");
    }
  }

  function runAutoQuality() {
    if (!selectedDocument) {
      return;
    }
    autoQualityMutation.mutate({
      documentId: selectedDocument.id,
      useDefault: useDefaultQuality,
      standardIds: selectedStandardIDs,
    });
  }

  function submitAsk(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    askMutation.mutate({ conversationId: conversationID, question, limit: 5 });
  }

  return (
    <div className="mx-auto max-w-[1600px] space-y-6">
      <section className="flex flex-col justify-between gap-4 xl:flex-row xl:items-end">
        <div>
          <p className="text-sm font-medium text-brand-700">Knowledge Center</p>
          <h1 className="mt-1 text-3xl font-semibold tracking-tight text-slate-950">
            知识中心
          </h1>
          <p className="mt-2 max-w-3xl text-sm leading-6 text-slate-500">
            覆盖上传、解析、质检、审核发布与 RAG 问答。只有 published
            文档会进入正式问答召回。
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <StatusPill label="上传" active />
          {isAdmin && <StatusPill label="切片" active />}
          {isAdmin && <StatusPill label="质检" active />}
          {isAdmin && <StatusPill label="审核" active />}
          <StatusPill label="Chat" active />
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

      {isAdmin && <KnowledgeAdminWorkbench document={selectedDocument} />}

      {isAdmin && (
        <Card className="border-brand-200 bg-brand-50/40 shadow-none">
          <CardContent className="flex flex-col gap-3 pt-6 sm:flex-row sm:items-center">
            <div className="min-w-0 flex-1">
              <p className="font-medium text-slate-900">
                Quality Evaluation Review
              </p>
              <p className="text-sm text-slate-500">
                查看分项证据、Hard Gate、人工覆盖审计，并发布或重新评分。
              </p>
            </div>
            <Input
              className="sm:w-48"
              type="number"
              min="1"
              value={evaluationID}
              onChange={(event) => setEvaluationID(event.target.value)}
              placeholder="Evaluation ID"
              aria-label="Quality Evaluation ID"
            />
            <Button
              type="button"
              disabled={
                !Number.isInteger(Number(evaluationID)) ||
                Number(evaluationID) <= 0
              }
              onClick={() =>
                navigate(`/knowledge/evaluations/${evaluationID}/review`)
              }
            >
              打开 Review <ArrowRight className="size-4" />
            </Button>
          </CardContent>
        </Card>
      )}

      {isAdmin && (
        <Card className="border-violet-200 bg-violet-50/40 shadow-none">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <FlaskConical className="size-5 text-violet-600" />
              Retrieval Evaluation Center
            </CardTitle>
            <CardDescription>
              在发布前运行 Smoke Test，或比较 lexical、默认配置与指定的
              Embedding / Rerank / Chunk Strategy。
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid gap-3 md:grid-cols-5">
              <Input
                type="number"
                min="1"
                placeholder="Version ID"
                value={retrievalVersionID}
                onChange={(event) => setRetrievalVersionID(event.target.value)}
              />
              <Input
                type="number"
                min="1"
                placeholder="Embedding Config"
                value={retrievalEmbeddingID}
                onChange={(event) =>
                  setRetrievalEmbeddingID(event.target.value)
                }
              />
              <Input
                type="number"
                min="1"
                placeholder="Rerank Config"
                value={retrievalRerankID}
                onChange={(event) => setRetrievalRerankID(event.target.value)}
              />
              <Input
                type="number"
                min="1"
                placeholder="Strategy ID"
                value={retrievalStrategyID}
                onChange={(event) => setRetrievalStrategyID(event.target.value)}
              />
              <Input
                placeholder="Embedding Revision"
                value={retrievalRevision}
                onChange={(event) => setRetrievalRevision(event.target.value)}
              />
            </div>
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                disabled={
                  !Number.isInteger(retrievalVersion) ||
                  retrievalVersion <= 0 ||
                  smokeMutation.isPending
                }
                onClick={() =>
                  smokeMutation.mutate({
                    documentVersionId: retrievalVersion,
                    ...retrievalConfig,
                  })
                }
              >
                {smokeMutation.isPending ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <ShieldCheck className="size-4" />
                )}
                运行 Smoke Test
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={
                  !Number.isInteger(retrievalVersion) ||
                  retrievalVersion <= 0 ||
                  labMutation.isPending
                }
                onClick={() =>
                  labMutation.mutate({
                    documentVersionId: retrievalVersion,
                    variants: [
                      {
                        name: "lexical-only",
                        disableEmbedding: true,
                        disableRerank: true,
                        limit: 5,
                      },
                      { name: "default-production", limit: 5 },
                      { name: "configured-variant", ...retrievalConfig },
                    ],
                  })
                }
              >
                {labMutation.isPending ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <FlaskConical className="size-4" />
                )}
                对比三组配置
              </Button>
            </div>
            {retrievalRuns.length > 0 && (
              <div className="grid gap-3 lg:grid-cols-3">
                {retrievalRuns.map((run) => (
                  <div
                    key={run.id}
                    className="rounded-lg border border-violet-200 bg-white p-3 text-xs text-slate-600"
                  >
                    <div className="flex items-center justify-between gap-2">
                      <p className="font-semibold text-slate-800">{run.name}</p>
                      <span
                        className={
                          run.passed ? "text-emerald-600" : "text-rose-600"
                        }
                      >
                        {run.passed ? "PASS" : "FAIL"}
                      </span>
                    </div>
                    <p className="mt-2">
                      Recall@K {percent(run.metrics.recallAtK)} · MRR{" "}
                      {percent(run.metrics.mrr)}
                    </p>
                    <p>
                      nDCG {percent(run.metrics.ndcgAtK)} · Citation{" "}
                      {percent(run.metrics.citationAccuracy)}
                    </p>
                    <p className="mt-1 text-slate-400">
                      Embedding {run.embeddingModel ?? "off/default"} · Rerank{" "}
                      {run.rerankModel ?? "off/default"} · Strategy{" "}
                      {run.chunkStrategyId ?? "default"}
                    </p>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      )}

      <section className="grid gap-6 xl:grid-cols-[0.9fr_1.15fr_1fr]">
        <Card className="border-slate-200/80 shadow-none">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <FileText className="size-5 text-brand-600" />
              文档列表
            </CardTitle>
            <CardDescription>上传后会以 draft 状态进入知识库。</CardDescription>
          </CardHeader>
          <CardContent className="space-y-5">
            <form className="space-y-3" onSubmit={submitUpload}>
              <Field label="标题">
                <Input
                  value={uploadForm.title}
                  placeholder="支付系统排障手册"
                  onChange={(event) =>
                    setUploadForm((current) => ({
                      ...current,
                      title: event.target.value,
                    }))
                  }
                />
              </Field>
              <div className="grid gap-3 sm:grid-cols-2">
                <Field label="系统">
                  <Input
                    value={uploadForm.systemName}
                    placeholder="payment"
                    onChange={(event) =>
                      setUploadForm((current) => ({
                        ...current,
                        systemName: event.target.value,
                      }))
                    }
                  />
                </Field>
                <Field label="环境">
                  <Input
                    value={uploadForm.environment}
                    onChange={(event) =>
                      setUploadForm((current) => ({
                        ...current,
                        environment: event.target.value,
                      }))
                    }
                  />
                </Field>
              </div>
              <div className="grid gap-3 sm:grid-cols-2">
                <Field label="组件">
                  <Input
                    value={uploadForm.componentName}
                    placeholder="payment-api"
                    onChange={(event) =>
                      setUploadForm((current) => ({
                        ...current,
                        componentName: event.target.value,
                      }))
                    }
                  />
                </Field>
                <Field label="类型">
                  <Input
                    value={uploadForm.docType}
                    onChange={(event) =>
                      setUploadForm((current) => ({
                        ...current,
                        docType: event.target.value,
                      }))
                    }
                  />
                </Field>
              </div>
              <Field label="标签 JSON">
                <Input
                  value={uploadForm.tags}
                  onChange={(event) =>
                    setUploadForm((current) => ({
                      ...current,
                      tags: event.target.value,
                    }))
                  }
                />
              </Field>
              <Field label="选择文件">
                <Input
                  type="file"
                  accept=".md,.txt,.docx,.xlsx,text/plain,text/markdown,application/vnd.openxmlformats-officedocument.wordprocessingml.document,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
                  onChange={(event) => setFile(event.target.files?.[0] ?? null)}
                />
              </Field>
              <Button className="w-full" disabled={uploadMutation.isPending}>
                {uploadMutation.isPending ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <UploadCloud className="size-4" />
                )}
                上传文档
              </Button>
            </form>

            {isAdmin && (
              <form
                className="space-y-3 rounded-xl border border-slate-200 bg-slate-50 p-3"
                onSubmit={submitStandard}
              >
                <div>
                  <p className="text-sm font-semibold text-slate-700">
                    评分标准
                  </p>
                  <p className="mt-1 text-xs text-slate-400">
                    支持 txt、md、docx、xlsx，上传后可参与自动评分。
                  </p>
                </div>
                <Field label="标准标题">
                  <Input
                    value={standardTitle}
                    placeholder="数据库知识库评分标准"
                    onChange={(event) => setStandardTitle(event.target.value)}
                  />
                </Field>
                <Field label="标准文件">
                  <Input
                    type="file"
                    accept=".md,.txt,.docx,.xlsx,text/plain,text/markdown,application/vnd.openxmlformats-officedocument.wordprocessingml.document,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
                    onChange={(event) =>
                      setStandardFile(event.target.files?.[0] ?? null)
                    }
                  />
                </Field>
                <Button
                  className="w-full"
                  variant="outline"
                  disabled={uploadStandardMutation.isPending}
                >
                  {uploadStandardMutation.isPending && (
                    <Loader2 className="size-4 animate-spin" />
                  )}
                  上传评分标准
                </Button>
              </form>
            )}

            <div className="space-y-2">
              {documentsQuery.isLoading && (
                <p className="text-sm text-slate-400">正在加载文档...</p>
              )}
              {documents.length === 0 && !documentsQuery.isLoading && (
                <p className="rounded-lg border border-dashed border-slate-200 bg-slate-50 p-4 text-sm text-slate-400">
                  暂无文档。上传一份 Markdown 或 TXT 后开始流程。
                </p>
              )}
              {documents.map((document) => (
                <button
                  key={document.id}
                  type="button"
                  onClick={() => setSelectedID(document.id)}
                  className={cn(
                    "w-full rounded-xl border p-3 text-left transition-colors",
                    selectedID === document.id
                      ? "border-brand-300 bg-brand-50"
                      : "border-slate-200 bg-white hover:bg-slate-50",
                  )}
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <p className="truncate text-sm font-semibold text-slate-800">
                        {document.title}
                      </p>
                      <p className="mt-1 truncate text-xs text-slate-400">
                        {document.fileName}
                      </p>
                    </div>
                    <Badge status={document.status} />
                  </div>
                  <p className="mt-2 text-xs text-slate-500">
                    质量分：{document.qualityScore}
                  </p>
                </button>
              ))}
            </div>
          </CardContent>
        </Card>

        <Card className="border-slate-200/80 shadow-none">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <ShieldCheck className="size-5 text-emerald-600" />
              {isAdmin ? "详情 / 质检 / 审核" : "文档详情"}
            </CardTitle>
            <CardDescription>
              {isAdmin
                ? "先解析切片，再提交质检 JSON，通过后发布。"
                : "查看已上传文档的版本与发布状态。"}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-5">
            {!selectedDocument ? (
              <div className="grid min-h-72 place-items-center rounded-xl border border-dashed border-slate-200 bg-slate-50 p-8 text-center text-sm text-slate-400">
                请选择左侧文档。
              </div>
            ) : (
              <>
                <div className="rounded-xl border border-slate-200 bg-slate-50 p-4">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div>
                      <h2 className="text-lg font-semibold text-slate-900">
                        {selectedDocument.title}
                      </h2>
                      <p className="mt-1 text-sm text-slate-500">
                        {selectedDocument.fileName} · {selectedDocument.version}
                      </p>
                    </div>
                    <Badge status={selectedDocument.status} />
                  </div>
                  <div className="mt-4 grid gap-3 text-xs text-slate-500 sm:grid-cols-2">
                    <Info label="系统" value={selectedDocument.systemName} />
                    <Info label="组件" value={selectedDocument.componentName} />
                    <Info label="环境" value={selectedDocument.environment} />
                    <Info
                      label="质量分"
                      value={String(selectedDocument.qualityScore)}
                    />
                  </div>
                </div>

                {isAdmin && (
                  <div className="flex flex-wrap gap-2">
                    <Button
                      variant="outline"
                      onClick={() =>
                        reprocessMutation.mutate(selectedDocument.id)
                      }
                      disabled={reprocessMutation.isPending}
                    >
                      <RefreshCw
                        className={cn(
                          "size-4",
                          reprocessMutation.isPending && "animate-spin",
                        )}
                      />
                      解析切片
                    </Button>
                    <ReviewButton
                      icon={<CheckCircle2 className="size-4" />}
                      label="发布"
                      action={() =>
                        actionMutation.mutate({
                          id: selectedDocument.id,
                          action: "publish",
                          reviewComment: comment,
                        })
                      }
                      disabled={actionMutation.isPending}
                    />
                    <ReviewButton
                      icon={<XCircle className="size-4" />}
                      label="驳回"
                      action={() =>
                        actionMutation.mutate({
                          id: selectedDocument.id,
                          action: "reject",
                          reviewComment: comment,
                        })
                      }
                      disabled={actionMutation.isPending}
                    />
                    <ReviewButton
                      icon={<Archive className="size-4" />}
                      label="归档"
                      action={() =>
                        actionMutation.mutate({
                          id: selectedDocument.id,
                          action: "archive",
                          reviewComment: comment,
                        })
                      }
                      disabled={actionMutation.isPending}
                    />
                  </div>
                )}

                {isAdmin && (
                  <Field label="审核备注">
                    <Input
                      value={comment}
                      onChange={(event) => setComment(event.target.value)}
                    />
                  </Field>
                )}

                {isAdmin && (
                  <div className="space-y-3 rounded-xl border border-emerald-100 bg-emerald-50/50 p-3">
                    <div>
                      <p className="text-sm font-semibold text-slate-700">
                        按评分标准自动评分
                      </p>
                      <p className="mt-1 text-xs text-slate-500">
                        可同时使用默认标准和自定义标准，结果会写入下方质检
                        JSON。
                      </p>
                    </div>
                    <label className="flex items-center gap-2 text-sm text-slate-600">
                      <input
                        type="checkbox"
                        checked={useDefaultQuality}
                        onChange={(event) =>
                          setUseDefaultQuality(event.target.checked)
                        }
                      />
                      使用默认评分标准
                    </label>
                    <div className="space-y-2">
                      <p className="text-xs font-semibold text-slate-500">
                        自定义标准
                      </p>
                      {qualityStandards.length === 0 && (
                        <p className="text-xs text-slate-400">
                          暂无自定义评分标准，可在左侧上传。
                        </p>
                      )}
                      {qualityStandards.map((standard) => (
                        <label
                          key={standard.id}
                          className="flex items-start gap-2 rounded-lg bg-white p-2 text-xs text-slate-600"
                        >
                          <input
                            type="checkbox"
                            checked={selectedStandardIDs.includes(standard.id)}
                            onChange={(event) =>
                              setSelectedStandardIDs((current) =>
                                event.target.checked
                                  ? [...current, standard.id]
                                  : current.filter((id) => id !== standard.id),
                              )
                            }
                          />
                          <span>
                            <span className="font-semibold text-slate-700">
                              {standard.title}
                            </span>
                            <span className="ml-2 text-slate-400">
                              {standard.fileName}
                            </span>
                          </span>
                        </label>
                      ))}
                    </div>
                    <Button
                      type="button"
                      variant="outline"
                      disabled={autoQualityMutation.isPending}
                      onClick={runAutoQuality}
                    >
                      {autoQualityMutation.isPending && (
                        <Loader2 className="size-4 animate-spin" />
                      )}
                      自动评分
                    </Button>
                  </div>
                )}

                {isAdmin && (
                  <form className="space-y-3" onSubmit={submitQuality}>
                    <Field label="质检 JSON">
                      <textarea
                        value={qualityJSON}
                        onChange={(event) => setQualityJSON(event.target.value)}
                        className="min-h-44 w-full rounded-md border border-input bg-white px-3 py-2 font-mono text-xs shadow-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
                      />
                    </Field>
                    <Button disabled={qualityMutation.isPending}>
                      {qualityMutation.isPending && (
                        <Loader2 className="size-4 animate-spin" />
                      )}
                      提交质检
                    </Button>
                  </form>
                )}

                {isAdmin && (
                  <div className="rounded-xl border border-slate-200">
                    <div className="flex items-center justify-between border-b border-slate-200 px-4 py-3">
                      <p className="text-sm font-semibold text-slate-700">
                        Chunk 预览
                      </p>
                      <span className="text-xs text-slate-400">
                        {chunksQuery.data?.chunkCount ?? 0} chunks
                      </span>
                    </div>
                    <div className="max-h-72 space-y-2 overflow-y-auto p-3">
                      {(chunksQuery.data?.chunks ?? [])
                        .slice(0, 5)
                        .map((chunk) => (
                          <div
                            key={chunk.id}
                            className="rounded-lg bg-slate-50 p-3 text-xs leading-5 text-slate-600"
                          >
                            <p className="mb-1 font-semibold text-slate-700">
                              #{chunk.chunkIndex} {chunk.sourceSection ?? ""}
                            </p>
                            {chunk.content}
                          </div>
                        ))}
                      {selectedID &&
                        !chunksQuery.isLoading &&
                        !chunksQuery.data?.chunkCount && (
                          <p className="p-3 text-sm text-slate-400">
                            暂无 chunk，点击“解析切片”生成。
                          </p>
                        )}
                    </div>
                  </div>
                )}
              </>
            )}
          </CardContent>
        </Card>

        <Card className="border-slate-200/80 shadow-none">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <MessageSquareText className="size-5 text-violet-600" />
              Chat
            </CardTitle>
            <CardDescription>
              向已发布知识库提问，并查看引用依据。
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <form className="space-y-3" onSubmit={submitAsk}>
              <Field label="问题">
                <textarea
                  value={question}
                  onChange={(event) => setQuestion(event.target.value)}
                  className="min-h-28 w-full rounded-md border border-input bg-white px-3 py-2 text-sm shadow-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
                />
              </Field>
              <Button className="w-full" disabled={askMutation.isPending}>
                {askMutation.isPending ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <Send className="size-4" />
                )}
                发送问题
              </Button>
            </form>

            <div className="rounded-xl border border-slate-200 bg-slate-50 p-4">
              <div className="flex items-center gap-2 text-sm font-semibold text-slate-800">
                <Bot className="size-4 text-violet-600" />
                回答
              </div>
              {lastAnswer ? (
                <div className="mt-3 space-y-4">
                  <p className="whitespace-pre-wrap text-sm leading-6 text-slate-700">
                    {lastAnswer.answer}
                  </p>
                  <div>
                    <p className="text-xs font-semibold uppercase tracking-wide text-slate-400">
                      Citations
                    </p>
                    <div className="mt-2 space-y-2">
                      {lastAnswer.citations.length === 0 && (
                        <p className="text-xs text-slate-400">
                          无引用，系统已明确说明无依据。
                        </p>
                      )}
                      {lastAnswer.citations.map((citation) => (
                        <div
                          key={`${citation.documentId}-${citation.chunkId}`}
                          className="rounded-lg bg-white p-3 text-xs text-slate-600 ring-1 ring-slate-200"
                        >
                          <p className="font-semibold text-slate-700">
                            [{citation.citationId}] · doc #{citation.documentId}{" "}
                            · version #{citation.documentVersionId} · chunks{" "}
                            {citation.chunkIds.join(", ")}
                          </p>
                          <p className="mt-1 leading-5">{citation.snippet}</p>
                        </div>
                      ))}
                    </div>
                  </div>
                  <p className="text-xs text-slate-400">
                    会话 #{lastAnswer.conversation.id} · QA #
                    {lastAnswer.qaRecordId} · 召回 {lastAnswer.recallCount}
                  </p>
                </div>
              ) : (
                <p className="mt-3 text-sm leading-6 text-slate-400">
                  发送一个问题后，这里会展示 answer、citations 与 QA 记录编号。
                </p>
              )}
            </div>
          </CardContent>
        </Card>
      </section>
    </div>
  );
}

function positiveNumber(value: string) {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : undefined;
}

function percent(value: number | undefined) {
  return `${((value ?? 0) * 100).toFixed(1)}%`;
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="space-y-1.5">
      <Label>{label}</Label>
      {children}
    </div>
  );
}

function Badge({ status }: { status: string }) {
  return (
    <span
      className={cn(
        "shrink-0 rounded-full px-2.5 py-1 text-[11px] font-semibold",
        statusTone[status] ?? "bg-slate-100 text-slate-600",
      )}
    >
      {status}
    </span>
  );
}

function Info({ label, value }: { label: string; value?: string }) {
  return (
    <div>
      <p className="text-slate-400">{label}</p>
      <p className="mt-1 font-medium text-slate-700">{value || "--"}</p>
    </div>
  );
}

function StatusPill({ label, active }: { label: string; active: boolean }) {
  return (
    <span
      className={cn(
        "rounded-full px-3 py-1 text-xs font-medium",
        active ? "bg-brand-50 text-brand-700" : "bg-slate-100 text-slate-400",
      )}
    >
      {label}
    </span>
  );
}

function ReviewButton({
  icon,
  label,
  action,
  disabled,
}: {
  icon: ReactNode;
  label: string;
  action: () => void;
  disabled: boolean;
}) {
  return (
    <Button
      type="button"
      variant="outline"
      onClick={action}
      disabled={disabled}
    >
      {icon}
      {label}
    </Button>
  );
}
