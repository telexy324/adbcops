import { apiClient } from "@/api/client";

type ApiEnvelope<T> = {
  code: number;
  message: string;
  data: T;
};

export type RCARun = {
  id: number;
  userId: number;
  conversationId?: number;
  incidentId?: number;
  workflowRunId?: number;
  status: string;
  query: string;
  scope: Record<string, unknown>;
  currentRound: number;
  maxRounds: number;
  timeoutAt?: string;
  cancelRequestedAt?: string;
  errorCode?: string;
  errorMessage?: string;
  stopReason?: string;
  startedAt?: string;
  finishedAt?: string;
  createdAt: string;
  updatedAt: string;
};

export type RCAHypothesis = {
  id?: string;
  summary: string;
  confidence: number;
  evidenceIds: number[];
};

export type RCANextAction = {
  actionKey: string;
  skillName: string;
  input: Record<string, unknown>;
};

export type RCARound = {
  id: number;
  runId: number;
  roundNumber: number;
  status: string;
  inputHypotheses: RCAHypothesis[];
  newEvidenceIds: number[];
  rejectedHypotheses: RCAHypothesis[];
  nextActions: RCANextAction[];
  errorCode?: string;
  startedAt?: string;
  finishedAt?: string;
};

export type RCAAction = {
  id: number;
  runId: number;
  roundId: number;
  actionKey: string;
  skillName: string;
  status: string;
  input: Record<string, unknown>;
  output?: unknown;
  evidenceIds: number[];
  errorCode?: string;
  errorMessage?: string;
  sensitiveRead: boolean;
  attempt: number;
  startedAt?: string;
  finishedAt?: string;
};

export type RCAEvidence = {
  id: number;
  evidenceKey: string;
  sourceType: string;
  sourceRef?: Record<string, unknown>;
  observedAt?: string;
  title?: string;
  summary: string;
  content?: unknown;
  confidence?: number;
  sensitivity?: string;
  rcaRunId?: number;
  rcaRoundId?: number;
  rcaActionId?: number;
  evidenceKind?: string;
  entity?: Record<string, unknown>;
  windowStart?: string;
  windowEnd?: string;
  sourceSkill?: string;
  dataSourceId?: number;
  createdAt: string;
};

export type RCARootCauseCandidateRecord = {
  id: number;
  runId: number;
  roundId: number;
  summary: string;
  confidence: number;
  evidenceIds: number[];
  rejected: boolean;
};

export type RCADetail = {
  run: RCARun;
  rounds: RCARound[];
  actions: RCAAction[];
  evidence: RCAEvidence[];
  rootCauseCandidates: RCARootCauseCandidateRecord[];
};

export type RCAPlannerHypothesis = {
  id: string;
  summary: string;
  confidence: number;
  supportingEvidenceIds: number[];
  contradictingEvidenceIds: number[];
  rationale?: string;
};

export type RCAPlannerAction = {
  actionKey: string;
  skillName: string;
  input: Record<string, unknown>;
  reason: string;
  targetEntity?: string;
  evidenceIds?: number[];
};

export type RCAPlannerResult = {
  hypotheses: RCAPlannerHypothesis[];
  missingEvidence: string[];
  nextActions: RCAPlannerAction[];
  shouldStop: boolean;
  stopReason: string;
  plannerDegraded: boolean;
  degradationReason?: string;
};

export type RCATopologyCandidate = {
  nodeKey: string;
  name: string;
  kind: string;
  componentType: string;
  sourceType: string;
  hops: number;
  edgeType?: string;
  direction: string;
  confidence: number;
  score: number;
  freshness: string;
  aliasMatched: boolean;
  conflict: boolean;
  selected: boolean;
  dataSourceId?: number;
  bindingStatus?: string;
  topologyEvidenceIds: number[];
};

export type RCATopologyInvestigation = {
  rootNodeKey?: string;
  observedAliases: string[];
  candidates: RCATopologyCandidate[];
  selected: RCATopologyCandidate[];
  missingEvidence: string[];
  conflicts: string[];
  fallbackUsed: boolean;
};

export type RCADatabaseDiagnosis = {
  provider: string;
  sourceType: string;
  dataSourceId: number;
  serviceName?: string;
  environment?: string;
  windowMinutes: number;
  correlationDimensions: string[];
  sqlFingerprint?: string;
  sanitizedSql?: string;
  missingEvidence: string[];
  supportingEvidenceIds: number[];
  assessment?: {
    status: string;
    highestImpactFingerprint?: string;
    categories: Array<{
      category: string;
      sourceSkill: string;
      collected: boolean;
      evidenceIds: number[];
    }>;
    evidenceIds: number[];
    missingEvidence: string[];
    rootCauseEligible: boolean;
    confidence: string;
    conclusion: string;
  };
};

export type RCAOrchestratorRound = {
  roundNumber: number;
  status: string;
  plan?: RCAPlannerResult;
  topologyInvestigation?: RCATopologyInvestigation;
  databaseDiagnosis?: RCADatabaseDiagnosis;
  actionIds: number[];
  evidenceIds: number[];
  errors?: string[];
};

export type RCAReportEvidenceRef = {
  id: number;
  evidenceKey: string;
  kind: string;
  sourceType: string;
  sourceSkill?: string;
  summary: string;
  confidence?: number;
  roundId?: number;
  actionId?: number;
  dataSourceId?: number;
  url: string;
};

export type RCAReport = {
  version: string;
  runId: number;
  status: string;
  query: string;
  scope: Record<string, unknown>;
  impactScope: {
    serviceName?: string;
    environment?: string;
    namespace?: string;
    windowStart?: string;
    windowEnd?: string;
    entities: string[];
  };
  evidence: {
    facts: RCAReportEvidenceRef[];
    rules: RCAReportEvidenceRef[];
    knowledge: RCAReportEvidenceRef[];
    hypotheses: RCAReportEvidenceRef[];
  };
  rootCauseCandidates: Array<{
    id: number;
    summary: string;
    status: string;
    confidence: number;
    evidenceStrength: string;
    evidenceStrengthScore: number;
    supportingEvidence: RCAReportEvidenceRef[];
    contradictingEvidence: RCAReportEvidenceRef[];
  }>;
  rejectedHypotheses: Array<{
    id?: string;
    summary: string;
    confidence: number;
    evidence: RCAReportEvidenceRef[];
    status: string;
  }>;
  investigation: Array<{
    roundNumber: number;
    status: string;
    checked: Array<{
      actionId: number;
      actionKey: string;
      skillName: string;
      status: string;
      evidenceIds: number[];
      errorCode?: string;
    }>;
    findings: RCAReportEvidenceRef[];
    continueReason?: string;
    stopReason?: string;
  }>;
  missingEvidence: string[];
  incomplete: boolean;
  conclusion: string;
  suggestions: Array<{
    summary: string;
    evidenceIds: number[];
    advisoryOnly: boolean;
    autoExecute: boolean;
  }>;
  riskNotices: string[];
  stopReason: string;
};

export type RCAOrchestratorResult = {
  version: string;
  run: RCARun;
  report?: RCAReport;
  stopReason: string;
  rounds: RCAOrchestratorRound[];
  degraded: boolean;
};

export async function createRCARun(input: {
  query: string;
  scope: Record<string, unknown>;
  maxRounds?: number;
  timeoutSeconds?: number;
}) {
  const response = await apiClient.post<ApiEnvelope<RCARun>>(
    "/api/rca/runs",
    input,
  );
  return response.data.data;
}

export async function listRCARuns(limit = 20) {
  const response = await apiClient.get<ApiEnvelope<RCARun[]>>("/api/rca/runs", {
    params: { limit },
  });
  return response.data.data;
}

export async function getRCADetail(runId: number) {
  const response = await apiClient.get<ApiEnvelope<RCADetail>>(
    `/api/rca/runs/${runId}`,
  );
  return response.data.data;
}

export async function orchestrateRCARun(runId: number) {
  const response = await apiClient.post<ApiEnvelope<RCAOrchestratorResult>>(
    `/api/rca/runs/${runId}/orchestrate`,
    {
      budget: {
        maxRounds: 3,
        maxSkillCallsPerRound: 12,
        maxSkillCalls: 24,
        maxConcurrentSkills: 4,
        maxTokens: 16000,
        maxContextBytes: 65536,
        maxWallTimeSeconds: 300,
        confidenceThreshold: 0.85,
      },
      useLlm: true,
    },
  );
  return response.data.data;
}

export async function cancelRCARun(runId: number) {
  const response = await apiClient.post<ApiEnvelope<RCARun>>(
    `/api/rca/runs/${runId}/cancel`,
  );
  return response.data.data;
}

export async function recoverRCARun(runId: number) {
  const response = await apiClient.post<
    ApiEnvelope<{
      run: RCARun;
      skippedActionIds: number[];
      retryableActionIds: number[];
    }>
  >(`/api/rca/runs/${runId}/recover`);
  return response.data.data;
}

export async function getRCAReport(runId: number) {
  const response = await apiClient.get<ApiEnvelope<RCAReport>>(
    `/api/rca/runs/${runId}/report`,
  );
  return response.data.data;
}
