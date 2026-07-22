import { isAxiosError } from "axios";

import { apiClient } from "@/api/client";

type ApiEnvelope<T> = {
  code: number;
  message: string;
  data: T;
};

export type KnowledgeDocument = {
  id: number;
  title: string;
  fileName: string;
  fileType: string;
  systemName?: string;
  componentName?: string;
  environment?: string;
  docType?: string;
  version: string;
  status: string;
  tags?: unknown;
  summary?: string;
  qualityScore: number;
  createdBy?: number;
  createdAt: string;
  updatedAt: string;
  currentPublishedVersionId?: number;
};

export type DocumentVersion = {
  id: number;
  documentId: number;
  version: string;
  revisionNo: number;
  fileName: string;
  fileType: string;
  fileHash: string;
  parserName?: string;
  parserVersion?: string;
  language?: string;
  status: string;
  metadata?: Record<string, unknown>;
  parseQuality?: ParseQuality;
  documentSchema?: Record<string, unknown>;
  createdAt: string;
  updatedAt: string;
  publishedAt?: string;
  supersededAt?: string;
  deprecatedAt?: string;
};

export type ParseQuality = {
  parseSuccess: boolean;
  textCoverage: number;
  headingDetectionRate: number;
  tableDetectionCount: number;
  unknownBlockRatio: number;
  garbledTextRatio: number;
  emptyPageRatio: number;
  orderConfidence: number;
  metadataCompleteness: number;
  blockCount: number;
  level: string;
};

export type DocumentBlock = {
  id: string;
  type: string;
  level: number;
  text: string;
  page?: number;
  order: number;
  sectionPath?: string[];
  attributes?: Record<string, unknown>;
  children?: DocumentBlock[];
};

export type ParsedStructure = {
  version: DocumentVersion;
  parseQuality: ParseQuality;
  documentSchema: Record<string, unknown>;
  warnings: Array<{ code: string; message: string }>;
  blocks: DocumentBlock[];
};

export type ChunkStrategy = {
  id: number;
  name: string;
  version: string;
  config: Record<string, unknown>;
  enabled: boolean;
};

export type VersionChunk = KnowledgeChunk & {
  documentVersionId: number;
  strategyId: number;
  chunkType: string;
  sectionPath?: string[];
  sourceBlockIds: number[];
  contentHash: string;
  parentChunkId?: number;
};

export type EmbeddingIndex = {
  id: number;
  documentVersionId: number;
  strategyId: number;
  embeddingConfigId: number;
  modelName: string;
  modelRevision: string;
  dimension: number;
  status: string;
  chunkCount: number;
  embeddedCount: number;
  errorMessage?: string;
  completedAt?: string;
};

export type PublicationGate = {
  documentId: number;
  documentVersionId: number;
  canPublish: boolean;
  checks: Array<{
    name: "parse" | "quality" | "embedding" | "retrieval" | "review";
    passed: boolean;
    message: string;
    resourceId?: number;
  }>;
  evaluatedAt: string;
};

export type StructuredQualityRule = {
  ruleKey: string;
  name: string;
  ruleType: string;
  severity: string;
  maxScore: number;
  deduction?: number;
  required: boolean;
  hardGate: boolean;
  description?: string;
  evidenceRequirement?: Record<string, unknown>;
  detectorConfig?: Record<string, unknown>;
  order: number;
};

export type StructuredQualityCriterion = {
  criterionKey: string;
  name: string;
  weight: number;
  maxScore: number;
  scoringMethod: string;
  order: number;
  rules: StructuredQualityRule[];
};

export type StructuredQualityProfile = {
  id?: number;
  standardId?: number;
  profileKey: string;
  name: string;
  applicableDocTypes: string[];
  totalScore: number;
  passScore: number;
  warningScore: number;
  status?: string;
  criteria: StructuredQualityCriterion[];
};

export type StructuredQualityStandard = {
  id?: number;
  name: string;
  description?: string;
  version: string;
  status?: string;
  profiles: StructuredQualityProfile[];
};

export type KnowledgeChunk = {
  id: number;
  documentId: number;
  chunkIndex: number;
  content: string;
  sourceTitle?: string;
  sourceSection?: string;
  tokenCount: number;
  createdAt: string;
};

export type DocumentChunksResponse = {
  documentId: number;
  chunkCount: number;
  chunks: KnowledgeChunk[];
};

export type QualityResult = {
  score: number;
  summary: string;
  findings: string[];
  suggestions: string[];
  criteriaScores?: Array<{
    name: string;
    score: number;
    matched?: string[];
    missing?: string[];
    standard: string;
  }>;
  standards?: string[];
  source?: string;
};

export type QualityStandard = {
  id: number;
  title: string;
  fileName: string;
  fileType: string;
  enabled: boolean;
  createdBy?: number;
  createdAt: string;
  preview: string;
};

export type ReviewResponse = {
  document: KnowledgeDocument;
  qualityResult?: QualityResult;
  action?: string;
  canPublish: boolean;
};

export type QualityEvaluation = {
  id: number;
  documentVersionId: number;
  qualityProfileId: number;
  qualityProfileVersion: string;
  mode: "deterministic" | "hybrid" | "llm";
  selectedCriteria: string[];
  parseScore?: number;
  contentScore?: number;
  retrievalScore?: number;
  totalScore?: number;
  gateStatus: "pass" | "warning" | "blocked";
  level?: string;
  source: string;
  summary?: string;
  result?: {
    criterionScores?: Record<string, { score: number; maxScore: number }>;
    hardGateViolations?: string[];
  };
  status: string;
  reviewStatus: "draft" | "published" | "superseded";
  publishedBy?: number;
  publishedAt?: string;
  supersedesEvaluationId?: number;
  createdAt: string;
  completedAt?: string;
};

export type QualityRuleResult = {
  id: number;
  evaluationId: number;
  criterionKey: string;
  ruleKey: string;
  score?: number;
  maxScore?: number;
  status?: string;
  confidence?: number;
  evidence?: Array<{
    blockId?: string;
    sectionPath?: string[];
    page?: number;
    quote?: string;
    reason?: string;
  }>;
  deductionReason?: string;
  suggestion?: string;
  source: string;
  manuallyOverridden: boolean;
  overriddenBy?: number;
  overrideComment?: string;
};

export type QualityOverrideAudit = {
  id: number;
  evaluationId: number;
  ruleResultId: number;
  previousScore?: number;
  overriddenScore?: number;
  previousStatus?: string;
  overriddenStatus?: string;
  comment: string;
  createdBy: number;
  createdAt: string;
};

export type Citation = {
  citationId: string;
  documentId: number;
  documentVersionId: number;
  chunkId: number;
  chunkIds: number[];
  chunkIndex: number;
  sourceTitle?: string;
  sourceSection?: string;
  snippet: string;
};

export type RetrievalTrace = {
  queryUnderstanding: {
    normalizedQuery: string;
    keywords: string[];
    entities: string[];
    systemName: string;
    componentName: string;
    environment: string;
    docTypes: string[];
    timeSensitivity: string;
    mustHaveTerms: string[];
    negativeTerms: string[];
  };
  filters: {
    permissionScope: string;
    systemName?: string;
    componentName?: string;
    environment?: string;
    docTypes?: string[];
    mustHaveTerms?: string[];
    negativeTerms?: string[];
    evaluatedAt: string;
  };
  rrfK: number;
  channels: Array<{
    channel: string;
    count: number;
    degraded: boolean;
    error?: string;
  }>;
  candidates: Array<{
    chunkId: number;
    rrfScore: number;
    channelRanks: Array<{
      channel: string;
      rank: number;
      rawScore: number;
    }>;
  }>;
  rerank: {
    model?: string;
    inputCount: number;
    topN: number;
    degraded: boolean;
    error?: string;
    results: Array<{
      chunkId: number;
      rank: number;
      score: number;
      relevanceLabel: "high" | "medium" | "low";
    }>;
  };
  contextBuilder: {
    inputCount: number;
    deduplicatedCount: number;
    documentLimitedCount: number;
    mergedCount: number;
    parentExpandedCount: number;
    tokenBudget: number;
    tokensUsed: number;
    selectedCount: number;
    degraded: boolean;
    error?: string;
    blocks: Array<{
      citationId: string;
      documentId: number;
      documentVersionId: number;
      chunkIds: number[];
      retrievalTrace: Array<{
        chunkId: number;
        rrfScore: number;
        channelRanks: Array<{
          channel: string;
          rank: number;
          rawScore: number;
        }>;
        rerankRank: number;
        rerankScore: number;
      }>;
    }>;
  };
  configuration: {
    embeddingConfigId?: number;
    embeddingModel?: string;
    embeddingModelRevision?: string;
    rerankConfigId?: number;
    rerankModel?: string;
    chunkStrategyId?: number;
  };
};

export type AskResponse = {
  conversation: {
    id: number;
    title?: string;
    status: string;
  };
  message: {
    id: number;
    content: string;
  };
  qaRecordId: number;
  question: string;
  rewrittenQuery: string;
  answer: string;
  citations: Citation[];
  recallCount: number;
  retrievalTrace: RetrievalTrace;
};

export type RetrievalEvaluationMetrics = {
  recallAtK: number;
  mrr: number;
  ndcgAtK: number;
  hitRate: number;
  citationAccuracy: number;
  answerGroundedness: number;
  noAnswerPrecision: number;
  smokeCoverage: number;
};

export type RetrievalEvaluationRun = {
  id: number;
  mode: "smoke" | "lab";
  name: string;
  status: "running" | "completed" | "failed";
  documentVersionId?: number;
  embeddingConfigId?: number;
  embeddingModel?: string;
  embeddingModelRevision?: string;
  rerankConfigId?: number;
  rerankModel?: string;
  chunkStrategyId?: number;
  metrics: RetrievalEvaluationMetrics;
  caseCount: number;
  passed: boolean;
  createdAt: string;
  completedAt?: string;
};

export type RetrievalTestCase = {
  id: number;
  documentId?: number;
  documentVersionId?: number;
  question: string;
  category:
    "title" | "core_step" | "error_code" | "scenario" | "irrelevant" | "custom";
  expectedDocumentIds: number[];
  expectedChunkIds: number[];
  expectedSections: string[];
  mustIncludeFacts: string[];
  mustNotInclude: string[];
  expectNoAnswer: boolean;
  source: "manual" | "author" | "llm_reviewed" | "qa_feedback";
  enabled: boolean;
};

export type RetrievalRunConfig = {
  name?: string;
  caseIds?: number[];
  documentVersionId?: number;
  embeddingConfigId?: number;
  embeddingModelRevision?: string;
  rerankConfigId?: number;
  chunkStrategyId?: number;
  disableEmbedding?: boolean;
  disableRerank?: boolean;
  limit?: number;
  thresholds?: {
    minimumRecallAtK?: number;
    minimumCitationAccuracy?: number;
  };
};

export type UploadDocumentInput = {
  file: File;
  title: string;
  systemName?: string;
  componentName?: string;
  environment?: string;
  docType?: string;
  version?: string;
  tags?: string;
};

export async function listDocuments() {
  const response =
    await apiClient.get<ApiEnvelope<KnowledgeDocument[]>>("/api/documents");
  return response.data.data;
}

export async function uploadDocument(input: UploadDocumentInput) {
  const form = new FormData();
  form.append("file", input.file);
  form.append("title", input.title);
  appendIfPresent(form, "systemName", input.systemName);
  appendIfPresent(form, "componentName", input.componentName);
  appendIfPresent(form, "environment", input.environment);
  appendIfPresent(form, "docType", input.docType);
  appendIfPresent(form, "version", input.version);
  appendIfPresent(form, "tags", input.tags);
  const response = await apiClient.post<ApiEnvelope<KnowledgeDocument>>(
    "/api/documents/upload",
    form,
  );
  return response.data.data;
}

export async function listDocumentVersions(documentId: number) {
  const response = await apiClient.get<
    ApiEnvelope<{ items: DocumentVersion[]; count: number }>
  >(`/api/documents/${documentId}/versions`);
  return response.data.data.items;
}

export async function uploadDocumentVersion(input: {
  documentId: number;
  version: string;
  file: File;
}) {
  const form = new FormData();
  form.append("file", input.file);
  form.append("version", input.version);
  const response = await apiClient.post<ApiEnvelope<DocumentVersion>>(
    `/api/documents/${input.documentId}/versions/upload`,
    form,
  );
  return response.data.data;
}

export async function parseDocumentVersion(versionId: number) {
  const response = await apiClient.post<ApiEnvelope<ParsedStructure>>(
    `/api/knowledge/document-versions/${versionId}/parse`,
  );
  return response.data.data;
}

export async function getParsedStructure(versionId: number) {
  const response = await apiClient.get<ApiEnvelope<ParsedStructure>>(
    `/api/knowledge/document-versions/${versionId}/blocks`,
  );
  return response.data.data;
}

export async function listChunkStrategies() {
  const response = await apiClient.get<
    ApiEnvelope<{ items: ChunkStrategy[]; count: number }>
  >("/api/knowledge/chunk-strategies");
  return response.data.data.items;
}

export async function chunkDocumentVersion(input: {
  versionId: number;
  strategyId: number;
}) {
  const response = await apiClient.post<
    ApiEnvelope<{ chunks: VersionChunk[]; chunkCount: number }>
  >(`/api/knowledge/document-versions/${input.versionId}/chunk`, {
    strategyId: input.strategyId,
  });
  return response.data.data;
}

export async function listVersionChunks(
  versionId: number,
  strategyId?: number,
) {
  const response = await apiClient.get<
    ApiEnvelope<{ chunks: VersionChunk[]; chunkCount: number }>
  >(`/api/knowledge/document-versions/${versionId}/chunks`, {
    params: { strategyId },
  });
  return response.data.data;
}

export async function getEmbeddingStatus(
  versionId: number,
  strategyId: number,
) {
  const response = await apiClient.get<
    ApiEnvelope<{ ready: boolean; indexes: EmbeddingIndex[] }>
  >("/api/knowledge/indexes/status", {
    params: { documentVersionId: versionId, strategyId },
  });
  return response.data.data;
}

export async function createEmbeddingIndex(input: {
  documentVersionId: number;
  strategyId: number;
  embeddingConfigId: number;
  dimension: number;
  modelRevision?: string;
}) {
  const response = await apiClient.post<ApiEnvelope<EmbeddingIndex>>(
    "/api/knowledge/index-jobs",
    input,
  );
  return response.data.data;
}

export async function buildEmbeddingIndex(id: number) {
  const response = await apiClient.post<ApiEnvelope<EmbeddingIndex>>(
    `/api/knowledge/index-jobs/${id}/build`,
    { batchSize: 50 },
  );
  return response.data.data;
}

export async function retryEmbeddingIndex(id: number) {
  const response = await apiClient.post<ApiEnvelope<EmbeddingIndex>>(
    `/api/knowledge/index-jobs/${id}/retry`,
    { batchSize: 50 },
  );
  return response.data.data;
}

export async function getPublicationGate(versionId: number) {
  const response = await apiClient.get<ApiEnvelope<PublicationGate>>(
    `/api/knowledge/document-versions/${versionId}/publication-gate`,
  );
  return response.data.data;
}

export async function publishDocumentVersion(
  versionId: number,
  comment: string,
) {
  const response = await apiClient.post<
    ApiEnvelope<{ document: KnowledgeDocument; gate: PublicationGate }>
  >(`/api/knowledge/document-versions/${versionId}/publish`, { comment });
  return response.data.data;
}

export async function listStructuredQualityStandards() {
  const response = await apiClient.get<
    ApiEnvelope<{ items: StructuredQualityStandard[]; count: number }>
  >("/api/knowledge/quality-standards");
  return response.data.data.items;
}

export async function createStructuredQualityStandard(
  input: StructuredQualityStandard,
) {
  const response = await apiClient.post<ApiEnvelope<StructuredQualityStandard>>(
    "/api/knowledge/quality-standards",
    input,
  );
  return response.data.data;
}

export async function publishStructuredQualityStandard(id: number) {
  const response = await apiClient.post<ApiEnvelope<StructuredQualityStandard>>(
    `/api/knowledge/quality-standards/${id}/publish`,
  );
  return response.data.data;
}

export async function createQualityEvaluation(input: {
  documentVersionId: number;
  qualityProfileId: number;
  mode: "deterministic" | "hybrid" | "llm";
  selectedCriteria?: string[];
  force?: boolean;
}) {
  const response = await apiClient.post<ApiEnvelope<QualityEvaluation>>(
    "/api/knowledge/evaluations",
    input,
  );
  return response.data.data;
}

export async function listQualityStandards() {
  const response = await apiClient.get<ApiEnvelope<QualityStandard[]>>(
    "/api/documents/quality-standards",
  );
  return response.data.data;
}

export async function uploadQualityStandard(input: {
  file: File;
  title: string;
}) {
  const form = new FormData();
  form.append("file", input.file);
  form.append("title", input.title);
  const response = await apiClient.post<ApiEnvelope<QualityStandard>>(
    "/api/documents/quality-standards/upload",
    form,
  );
  return response.data.data;
}

export async function reprocessDocument(documentId: number) {
  const response = await apiClient.post<ApiEnvelope<DocumentChunksResponse>>(
    `/api/documents/${documentId}/reprocess`,
  );
  return response.data.data;
}

export async function getDocumentChunks(documentId: number) {
  const response = await apiClient.get<ApiEnvelope<DocumentChunksResponse>>(
    `/api/documents/${documentId}/chunks`,
  );
  return response.data.data;
}

export async function reviewQuality(documentId: number, result: QualityResult) {
  const response = await apiClient.post<ApiEnvelope<ReviewResponse>>(
    `/api/documents/${documentId}/review`,
    { result },
  );
  return response.data.data;
}

export async function autoReviewQuality(input: {
  documentId: number;
  useDefault: boolean;
  standardIds: number[];
}) {
  const response = await apiClient.post<ApiEnvelope<ReviewResponse>>(
    `/api/documents/${input.documentId}/review`,
    {
      autoQuality: true,
      useDefault: input.useDefault,
      standardIds: input.standardIds,
    },
  );
  return response.data.data;
}

export async function reviewAction(
  documentId: number,
  action: "publish" | "reject" | "archive" | "deprecate",
  comment: string,
) {
  const response = await apiClient.post<ApiEnvelope<ReviewResponse>>(
    `/api/documents/${documentId}/review`,
    { action, comment },
  );
  return response.data.data;
}

export async function askKnowledge(input: {
  conversationId?: number;
  question: string;
  limit?: number;
}) {
  const response = await apiClient.post<ApiEnvelope<AskResponse>>(
    "/api/knowledge/ask",
    input,
  );
  return response.data.data;
}

export async function runRetrievalSmoke(
  input: RetrievalRunConfig & {
    documentVersionId: number;
  },
) {
  const response = await apiClient.post<ApiEnvelope<RetrievalEvaluationRun>>(
    "/api/knowledge/retrieval-evaluations/smoke",
    input,
  );
  return response.data.data;
}

export async function runRetrievalLab(input: {
  documentVersionId?: number;
  caseIds?: number[];
  variants: RetrievalRunConfig[];
}) {
  const response = await apiClient.post<
    ApiEnvelope<{ runs: RetrievalEvaluationRun[]; count: number }>
  >("/api/knowledge/retrieval-evaluations/lab", input);
  return response.data.data.runs;
}

export async function listRetrievalEvaluationRuns(limit = 20) {
  const response = await apiClient.get<
    ApiEnvelope<{ items: RetrievalEvaluationRun[]; count: number }>
  >("/api/knowledge/retrieval-evaluations/runs", { params: { limit } });
  return response.data.data.items;
}

export async function listRetrievalTestCases(documentVersionId?: number) {
  const response = await apiClient.get<
    ApiEnvelope<{ items: RetrievalTestCase[]; count: number }>
  >("/api/knowledge/retrieval-evaluations/test-cases", {
    params: { documentVersionId },
  });
  return response.data.data.items;
}

export async function createRetrievalTestCase(
  input: Omit<RetrievalTestCase, "id">,
) {
  const response = await apiClient.post<ApiEnvelope<RetrievalTestCase>>(
    "/api/knowledge/retrieval-evaluations/test-cases",
    input,
  );
  return response.data.data;
}

export async function getQualityEvaluation(id: number) {
  const response = await apiClient.get<ApiEnvelope<QualityEvaluation>>(
    `/api/knowledge/evaluations/${id}`,
  );
  return response.data.data;
}

export async function listQualityRuleResults(id: number) {
  const response = await apiClient.get<
    ApiEnvelope<{ items: QualityRuleResult[]; count: number }>
  >(`/api/knowledge/evaluations/${id}/rule-results`);
  return response.data.data.items;
}

export async function listQualityOverrideAudits(id: number) {
  const response = await apiClient.get<
    ApiEnvelope<{ items: QualityOverrideAudit[]; count: number }>
  >(`/api/knowledge/evaluations/${id}/overrides`);
  return response.data.data.items;
}

export async function overrideQualityRule(
  evaluationId: number,
  input: {
    ruleResultId: number;
    score: number;
    status: string;
    comment: string;
  },
) {
  const response = await apiClient.post<ApiEnvelope<QualityEvaluation>>(
    `/api/knowledge/evaluations/${evaluationId}/override`,
    input,
  );
  return response.data.data;
}

export async function publishQualityEvaluation(id: number) {
  const response = await apiClient.post<ApiEnvelope<QualityEvaluation>>(
    `/api/knowledge/evaluations/${id}/publish`,
  );
  return response.data.data;
}

export async function rerunQualityEvaluation(id: number) {
  const response = await apiClient.post<ApiEnvelope<QualityEvaluation>>(
    `/api/knowledge/evaluations/${id}/rerun`,
  );
  return response.data.data;
}

export function toAPIErrorMessage(error: unknown) {
  if (isAxiosError<{ message?: string }>(error)) {
    return error.response?.data?.message ?? error.message;
  }
  if (error instanceof Error) {
    return error.message;
  }
  return "请求失败";
}

export function toPublicationGateErrorMessage(error: unknown) {
  if (
    isAxiosError<{ message?: string; data?: PublicationGate }>(error) &&
    error.response?.data?.data?.checks
  ) {
    const failed = error.response.data.data.checks
      .filter((check) => !check.passed)
      .map((check) => publicationGateLabel(check.name));
    if (failed.length) return `发布门禁未通过：${failed.join("、")}`;
  }
  return toAPIErrorMessage(error);
}

export function publicationGateLabel(name: string) {
  const labels: Record<string, string> = {
    parse: "文档解析",
    quality: "质量评分",
    embedding: "向量索引",
    retrieval: "检索冒烟测试",
    review: "人工审核",
  };
  return labels[name] ?? name;
}

function appendIfPresent(form: FormData, key: string, value?: string) {
  if (value && value.trim() !== "") {
    form.append(key, value.trim());
  }
}
