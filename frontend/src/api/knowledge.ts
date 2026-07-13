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

export type Citation = {
  documentId: number;
  chunkId: number;
  chunkIndex: number;
  sourceTitle?: string;
  sourceSection?: string;
  snippet: string;
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

export async function listQualityStandards() {
  const response = await apiClient.get<ApiEnvelope<QualityStandard[]>>(
    "/api/documents/quality-standards",
  );
  return response.data.data;
}

export async function uploadQualityStandard(input: { file: File; title: string }) {
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

export async function reviewQuality(
  documentId: number,
  result: QualityResult,
) {
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

export function toAPIErrorMessage(error: unknown) {
  if (isAxiosError<{ message?: string }>(error)) {
    return error.response?.data?.message ?? error.message;
  }
  if (error instanceof Error) {
    return error.message;
  }
  return "请求失败";
}

function appendIfPresent(form: FormData, key: string, value?: string) {
  if (value && value.trim() !== "") {
    form.append(key, value.trim());
  }
}
