import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, CheckCircle2, RefreshCw, ShieldAlert } from "lucide-react";
import { Link, useNavigate, useParams } from "react-router-dom";

import {
  getQualityEvaluation,
  getParsedStructure,
  listQualityOverrideAudits,
  listQualityRuleResults,
  overrideQualityRule,
  publishQualityEvaluation,
  rerunQualityEvaluation,
  toAPIErrorMessage,
  type QualityRuleResult,
  type DocumentBlock,
} from "@/api/knowledge";
import { getCurrentUser } from "@/api/client";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";

const findingStatuses = [
  "present",
  "missing",
  "partial",
  "conflicting",
  "outdated",
  "unsafe",
  "not_applicable",
  "manual_confirmation_required",
];

export function QualityReviewPage() {
  const { id } = useParams();
  const evaluationId = Number(id);
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [editing, setEditing] = useState<QualityRuleResult>();
  const [score, setScore] = useState("");
  const [status, setStatus] = useState("present");
  const [comment, setComment] = useState("");
  const [message, setMessage] = useState("");
  const [selectedBlockID, setSelectedBlockID] = useState<string>();
  const isAdmin = getCurrentUser()?.role === "admin";

  const evaluationQuery = useQuery({
    queryKey: ["quality-evaluation", evaluationId],
    queryFn: () => getQualityEvaluation(evaluationId),
    enabled: evaluationId > 0,
  });
  const resultsQuery = useQuery({
    queryKey: ["quality-rule-results", evaluationId],
    queryFn: () => listQualityRuleResults(evaluationId),
    enabled: evaluationId > 0,
  });
  const auditsQuery = useQuery({
    queryKey: ["quality-override-audits", evaluationId],
    queryFn: () => listQualityOverrideAudits(evaluationId),
    enabled: evaluationId > 0 && isAdmin,
  });
  const parsedQuery = useQuery({
    queryKey: [
      "quality-evidence-source",
      evaluationQuery.data?.documentVersionId,
    ],
    queryFn: () => getParsedStructure(evaluationQuery.data!.documentVersionId),
    enabled: Boolean(evaluationQuery.data?.documentVersionId),
    retry: false,
  });

  const refresh = async () => {
    await Promise.all([
      queryClient.invalidateQueries({
        queryKey: ["quality-evaluation", evaluationId],
      }),
      queryClient.invalidateQueries({
        queryKey: ["quality-rule-results", evaluationId],
      }),
      queryClient.invalidateQueries({
        queryKey: ["quality-override-audits", evaluationId],
      }),
    ]);
  };

  const overrideMutation = useMutation({
    mutationFn: () =>
      overrideQualityRule(evaluationId, {
        ruleResultId: editing?.id ?? 0,
        score: Number(score),
        status,
        comment,
      }),
    onSuccess: async () => {
      setEditing(undefined);
      setComment("");
      setMessage("人工评分已保存，聚合结果和审计记录已更新。");
      await refresh();
    },
    onError: (error) => setMessage(toAPIErrorMessage(error)),
  });
  const publishMutation = useMutation({
    mutationFn: () => publishQualityEvaluation(evaluationId),
    onSuccess: async () => {
      setMessage("评分结果已发布，后续修改需要重新评分生成新记录。");
      await refresh();
    },
    onError: (error) => setMessage(toAPIErrorMessage(error)),
  });
  const rerunMutation = useMutation({
    mutationFn: () => rerunQualityEvaluation(evaluationId),
    onSuccess: (next) => navigate(`/knowledge/evaluations/${next.id}/review`),
    onError: (error) => setMessage(toAPIErrorMessage(error)),
  });

  const groups = useMemo(() => {
    const grouped = new Map<string, QualityRuleResult[]>();
    for (const result of resultsQuery.data ?? []) {
      grouped.set(result.criterionKey, [
        ...(grouped.get(result.criterionKey) ?? []),
        result,
      ]);
    }
    return [...grouped.entries()];
  }, [resultsQuery.data]);

  if (!Number.isInteger(evaluationId) || evaluationId <= 0) {
    return <p role="alert">无效的评分记录 ID。</p>;
  }
  if (evaluationQuery.isLoading || resultsQuery.isLoading) {
    return <p role="status">正在加载评分记录…</p>;
  }
  if (evaluationQuery.error || resultsQuery.error || !evaluationQuery.data) {
    return (
      <p role="alert">
        {toAPIErrorMessage(evaluationQuery.error ?? resultsQuery.error)}
      </p>
    );
  }

  const evaluation = evaluationQuery.data;
  const immutable = evaluation.reviewStatus !== "draft";
  const sourceBlocks = flattenBlocks(parsedQuery.data?.blocks ?? []);
  const locateEvidence = (blockID: string) => {
    setSelectedBlockID(blockID);
    window.setTimeout(() => {
      document
        .getElementById(`source-block-${blockID}`)
        ?.scrollIntoView({ behavior: "smooth", block: "center" });
    });
  };

  return (
    <div className="mx-auto max-w-6xl space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <Link
            to="/knowledge"
            className="mb-2 inline-flex items-center gap-1 text-sm text-slate-500 hover:text-slate-900"
          >
            <ArrowLeft className="size-4" /> 返回知识中心
          </Link>
          <h1 className="text-2xl font-semibold">
            质量评分 Review #{evaluation.id}
          </h1>
          <p className="mt-1 text-sm text-slate-500">
            Profile v{evaluation.qualityProfileVersion} · {evaluation.source}
          </p>
        </div>
        {isAdmin && (
          <div className="flex gap-2">
            <Button
              variant="outline"
              onClick={() => rerunMutation.mutate()}
              disabled={rerunMutation.isPending}
            >
              <RefreshCw className="size-4" /> 重新评分
            </Button>
            <Button
              onClick={() => publishMutation.mutate()}
              disabled={immutable || publishMutation.isPending}
            >
              <CheckCircle2 className="size-4" /> 发布结果
            </Button>
          </div>
        )}
      </div>

      {message && (
        <p role="status" className="rounded-lg bg-slate-100 px-4 py-3 text-sm">
          {message}
        </p>
      )}

      <div className="grid gap-4 sm:grid-cols-3">
        <Metric label="总分" value={evaluation.totalScore?.toFixed(2) ?? "—"} />
        <Metric
          label="Gate"
          value={evaluation.gateStatus}
          blocked={evaluation.gateStatus === "blocked"}
        />
        <Metric label="Review 状态" value={evaluation.reviewStatus} />
      </div>

      {evaluation.result?.hardGateViolations?.length ? (
        <div className="flex gap-3 rounded-xl border border-red-200 bg-red-50 p-4 text-red-800">
          <ShieldAlert className="mt-0.5 size-5 shrink-0" />
          <div>
            <p className="font-medium">Hard Gate 已触发</p>
            <p className="text-sm">
              {evaluation.result.hardGateViolations.join("、")}
              。总分不能绕过阻断项。
            </p>
          </div>
        </div>
      ) : null}

      {groups.map(([criterion, results]) => (
        <Card key={criterion}>
          <CardHeader>
            <CardTitle>{criterion}</CardTitle>
            <CardDescription>
              {evaluation.result?.criterionScores?.[criterion]
                ? `${evaluation.result.criterionScores[criterion].score} / ${evaluation.result.criterionScores[criterion].maxScore}`
                : `${results.length} 条规则`}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {results.map((result) => (
              <div key={result.id} className="rounded-lg border p-4">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div>
                    <p className="font-medium">{result.ruleKey}</p>
                    <p className="mt-1 text-sm text-slate-500">
                      {result.status ?? "pending"} · {result.score ?? "—"} /{" "}
                      {result.maxScore ?? "—"} · {result.source}
                    </p>
                  </div>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={immutable || !isAdmin}
                    onClick={() => {
                      setEditing(result);
                      setScore(String(result.score ?? 0));
                      setStatus(result.status ?? "present");
                      setComment("");
                    }}
                  >
                    人工覆盖
                  </Button>
                </div>
                {result.deductionReason && (
                  <p className="mt-3 text-sm text-red-700">
                    {result.deductionReason}
                  </p>
                )}
                {(result.evidence ?? []).map((evidence, index) => (
                  <blockquote
                    key={index}
                    className="mt-3 border-l-2 pl-3 text-sm text-slate-600"
                  >
                    {evidence.quote || evidence.reason || "已记录证据定位"}
                    <span className="mt-1 block text-xs text-slate-400">
                      {evidence.sectionPath?.join(" / ") || "未标注章节"}
                      {evidence.page ? ` · 第 ${evidence.page} 页` : ""}
                    </span>
                    {evidence.blockId && (
                      <button
                        type="button"
                        className="mt-2 text-xs font-medium text-cyan-700 hover:underline"
                        onClick={() => locateEvidence(evidence.blockId!)}
                      >
                        定位原文 · {evidence.blockId}
                      </button>
                    )}
                  </blockquote>
                ))}
              </div>
            ))}
          </CardContent>
        </Card>
      ))}

      <Card>
        <CardHeader>
          <CardTitle>Evidence 原文定位</CardTitle>
          <CardDescription>
            证据通过 Block ID、章节路径和页码映射到解析后的文档结构。
          </CardDescription>
        </CardHeader>
        <CardContent className="max-h-96 space-y-2 overflow-auto">
          {sourceBlocks.length === 0 ? (
            <p className="text-sm text-slate-500">暂无可定位的 Parsed AST。</p>
          ) : (
            sourceBlocks.map((block) => (
              <button
                type="button"
                key={block.id}
                id={`source-block-${block.id}`}
                onClick={() => setSelectedBlockID(block.id)}
                className={`w-full rounded-lg border p-3 text-left text-sm ${selectedBlockID === block.id ? "border-cyan-400 bg-cyan-50 ring-2 ring-cyan-100" : "border-slate-200"}`}
              >
                <span className="text-xs font-semibold text-slate-500">
                  {block.id} · {block.type}
                  {block.page ? ` · 第 ${block.page} 页` : ""}
                </span>
                <span className="mt-1 block whitespace-pre-wrap text-slate-700">
                  {block.text}
                </span>
              </button>
            ))
          )}
        </CardContent>
      </Card>

      {isAdmin && editing && (
        <Card>
          <CardHeader>
            <CardTitle>人工覆盖：{editing.ruleKey}</CardTitle>
            <CardDescription>
              原因必填；保存后会重算总分与 Hard Gate，并写入审计。
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-4 sm:grid-cols-2">
            <Input
              type="number"
              min="0"
              max={editing.maxScore}
              step="0.01"
              value={score}
              onChange={(event) => setScore(event.target.value)}
              aria-label="覆盖分数"
            />
            <select
              value={status}
              onChange={(event) => setStatus(event.target.value)}
              aria-label="Finding 状态"
              className="h-11 rounded-md border bg-white px-3 text-sm"
            >
              {findingStatuses.map((value) => (
                <option key={value}>{value}</option>
              ))}
            </select>
            <textarea
              value={comment}
              onChange={(event) => setComment(event.target.value)}
              placeholder="填写人工判断依据（必填）"
              aria-label="覆盖原因"
              className="min-h-24 rounded-md border p-3 text-sm sm:col-span-2"
            />
            <div className="flex gap-2 sm:col-span-2">
              <Button
                onClick={() => overrideMutation.mutate()}
                disabled={
                  !comment.trim() || !score || overrideMutation.isPending
                }
              >
                保存覆盖
              </Button>
              <Button variant="ghost" onClick={() => setEditing(undefined)}>
                取消
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      {isAdmin && (
        <Card>
          <CardHeader>
            <CardTitle>人工覆盖审计</CardTitle>
            <CardDescription>
              保留修改前后值、原因、操作人和时间。
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {(auditsQuery.data ?? []).length === 0 ? (
              <p className="text-sm text-slate-500">暂无人工覆盖记录。</p>
            ) : (
              auditsQuery.data?.map((audit) => (
                <div key={audit.id} className="rounded-lg border p-3 text-sm">
                  <p>
                    Rule #{audit.ruleResultId}：{audit.previousScore ?? "—"} →{" "}
                    {audit.overriddenScore ?? "—"}，
                    {audit.previousStatus ?? "—"} →{" "}
                    {audit.overriddenStatus ?? "—"}
                  </p>
                  <p className="mt-1 text-slate-500">
                    {audit.comment} · 用户 #{audit.createdBy} ·{" "}
                    {new Date(audit.createdAt).toLocaleString()}
                  </p>
                </div>
              ))
            )}
          </CardContent>
        </Card>
      )}
    </div>
  );
}

function flattenBlocks(blocks: DocumentBlock[]): DocumentBlock[] {
  return blocks.flatMap((block) => [
    block,
    ...flattenBlocks(block.children ?? []),
  ]);
}

function Metric({
  label,
  value,
  blocked = false,
}: {
  label: string;
  value: string;
  blocked?: boolean;
}) {
  return (
    <Card>
      <CardContent className="pt-6">
        <p className="text-sm text-slate-500">{label}</p>
        <p
          className={`mt-2 text-2xl font-semibold ${blocked ? "text-red-700" : ""}`}
        >
          {value}
        </p>
      </CardContent>
    </Card>
  );
}
