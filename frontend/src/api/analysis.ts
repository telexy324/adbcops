import { apiClient } from "@/api/client";
import { toAPIErrorMessage } from "@/api/knowledge";

type ApiEnvelope<T> = {
  code: number;
  message: string;
  data: T;
};

export type AnalysisTask = {
  id: number;
  userId: number;
  conversationId?: number;
  taskType: string;
  question: string;
  status: string;
  summary?: string;
  result?: unknown;
  errorMessage?: string;
  createdAt: string;
  updatedAt: string;
};

export type EvidenceItem = {
  sourceType?: string;
  sourceId?: string;
  observedAt?: string;
  summary?: string;
  rawReference?: unknown;
  confidence?: number;
};

export type CitationItem = {
  documentId?: number;
  chunkId?: number;
  sourceTitle?: string;
  sourceSection?: string;
  snippet?: string;
};

export type GeneralAnalysisResponse = {
  taskId?: number;
  summary?: string;
  facts?: string[];
  evidence?: EvidenceItem[];
  citations?: CitationItem[];
  rootCauseCandidates?: string[];
  missingEvidence?: string[];
  confidence?: number;
};

export type K8sRuleFinding = {
  id: string;
  severity: string;
  category: string;
  title: string;
  description: string;
  evidenceKeys: string[];
  suggestion?: string;
};

export type PodDiagnosisResponse = {
  dataSourceId: number;
  namespace: string;
  pod: {
    name: string;
    phase: string;
    nodeName?: string;
    containers?: Array<{
      name: string;
      ready: boolean;
      restartCount: number;
      state?: string;
      reason?: string;
      lastReason?: string;
    }>;
  };
  events?: Array<{ type?: string; reason?: string; message?: string; count?: number }>;
  services?: Array<{ name: string; type?: string }>;
  endpoints?: Array<{ name: string; addresses?: string[] }>;
  ingresses?: Array<{ name: string; hosts?: string[] }>;
  rules?: K8sRuleFinding[];
};

export type MetricQueryResponse = {
  dataSourceId: number;
  query: string;
  range: boolean;
  series: Array<{
    metric: Record<string, string>;
    points: Array<{ timestamp: string; value: number; rawValue: string }>;
  }>;
  warnings?: string[];
  limit: { maxSeries: number; maxPoints: number };
};

export type AlertmanagerResponse = {
  received: number;
  events: Array<{
    id: number;
    fingerprint: string;
    status: string;
    severity?: string;
    summary: string;
    occurrenceCount: number;
    resolvedAt?: string;
  }>;
};

export async function runGeneralAnalysis(input: {
  conversationId?: number;
  question: string;
  dataSourceIds: number[];
  scope?: Record<string, unknown>;
}) {
  const response = await apiClient.post<ApiEnvelope<GeneralAnalysisResponse>>(
    "/api/analysis/general",
    input,
  );
  return response.data.data;
}

export async function listAnalysisTasks() {
  const response =
    await apiClient.get<ApiEnvelope<AnalysisTask[]>>("/api/analysis/tasks");
  return response.data.data;
}

export async function diagnosePod(input: {
  dataSourceId: number;
  namespace: string;
  podName: string;
  includeNode?: boolean;
  includePreviousLogs?: boolean;
  logTailLines?: number;
  logMaxBytes?: number;
}) {
  const response = await apiClient.post<ApiEnvelope<PodDiagnosisResponse>>(
    "/api/analysis/k8s/pod-diagnose",
    input,
  );
  return response.data.data;
}

export async function queryMetrics(input: {
  dataSourceId: number;
  query: string;
  range: boolean;
  start?: string;
  end?: string;
  stepSeconds?: number;
  maxSeries?: number;
  maxPoints?: number;
}) {
  const response = await apiClient.post<ApiEnvelope<MetricQueryResponse>>(
    "/api/analysis/metrics/query",
    input,
  );
  return response.data.data;
}

export async function sendAlertmanagerWebhook(input: unknown) {
  const response = await apiClient.post<ApiEnvelope<AlertmanagerResponse>>(
    "/api/events/alertmanager",
    input,
  );
  return response.data.data;
}

export { toAPIErrorMessage };
