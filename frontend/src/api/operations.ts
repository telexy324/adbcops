import { apiClient } from "@/api/client";
import { toAPIErrorMessage } from "@/api/knowledge";

type ApiEnvelope<T> = {
  code: number;
  message: string;
  data: T;
};

export type TopologyNode = {
  id: number;
  nodeKey: string;
  kind: string;
  name: string;
  displayName?: string;
  environment?: string;
  cluster?: string;
  namespace?: string;
  labels?: Record<string, unknown>;
  properties?: Record<string, unknown>;
  sourceType: string;
  sourceRef?: string;
  createdAt: string;
  updatedAt: string;
};

export type TopologyEdge = {
  id: number;
  edgeKey: string;
  fromNodeKey: string;
  toNodeKey: string;
  edgeType: string;
  confidence?: number;
  properties?: Record<string, unknown>;
  sourceType: string;
  sourceRef?: string;
  createdAt: string;
  updatedAt: string;
};

export type TopologyGraph = {
  nodes: TopologyNode[];
  edges: TopologyEdge[];
};

export type BlastRadius = TopologyGraph & {
  rootKey: string;
  direction: string;
  hops: number;
  cycleDetected: boolean;
};

export type Incident = {
  id: number;
  incidentKey: string;
  title: string;
  severity: string;
  status: string;
  environment?: string;
  systemName?: string;
  componentName?: string;
  summary?: string;
  startedAt?: string;
  resolvedAt?: string;
  createdAt: string;
  updatedAt: string;
};

export type IncidentEvent = {
  id: number;
  incidentId: number;
  eventId: number;
  createdAt: string;
};

export type IncidentEvidence = {
  id: number;
  incidentId: number;
  evidenceKey: string;
  createdAt: string;
};

export type RootCauseCandidate = {
  id: number;
  incidentId: number;
  summary: string;
  score: number;
  details?: Record<string, unknown>;
  confirmed: boolean;
  confirmedBy?: number;
  confirmedAt?: string;
  createdAt: string;
  updatedAt: string;
};

export type IncidentActivity = {
  id: number;
  incidentId: number;
  actorId?: number;
  action: string;
  detail?: Record<string, unknown>;
  createdAt: string;
};

export type IncidentDetail = {
  incident: Incident;
  events: IncidentEvent[];
  evidence: IncidentEvidence[];
  rootCauses: RootCauseCandidate[];
  activities: IncidentActivity[];
};

export type TimelineItem = {
  eventId: number;
  time: string;
  sourceType: string;
  eventType: string;
  severity?: string;
  status: string;
  environment?: string;
  systemName?: string;
  componentName?: string;
  namespace?: string;
  resourceKind?: string;
  resourceName?: string;
  summary: string;
  evidenceKeys?: string[];
  evidence?: Array<{
    evidenceKey: string;
    title?: string;
    summary?: string;
    sourceType?: string;
    confidence?: number;
  }>;
};

export type TimelineResponse = {
  from: string;
  to: string;
  timezone: string;
  anchorEventId?: number;
  items: TimelineItem[];
  sourceCounts: Record<string, number>;
  evidenceMissing?: string[];
};

export async function getTopologyGraph(limit = 80) {
  const response = await apiClient.get<ApiEnvelope<TopologyGraph>>(
    "/api/topology/graph",
    { params: { limit } },
  );
  return response.data.data;
}

export async function getBlastRadius(input: {
  nodeKey: string;
  direction?: "both" | "upstream" | "downstream";
  hops?: number;
  maxNodes?: number;
}) {
  const response = await apiClient.get<ApiEnvelope<BlastRadius>>(
    "/api/topology/blast-radius",
    {
      params: {
        nodeKey: input.nodeKey,
        direction: input.direction ?? "both",
        hops: input.hops ?? 2,
        maxNodes: input.maxNodes ?? 80,
      },
    },
  );
  return response.data.data;
}

export async function listIncidents() {
  const response =
    await apiClient.get<ApiEnvelope<Incident[]>>("/api/incidents");
  return response.data.data;
}

export async function getIncident(incidentId: number) {
  const response = await apiClient.get<ApiEnvelope<IncidentDetail>>(
    `/api/incidents/${incidentId}`,
  );
  return response.data.data;
}

export async function confirmRootCause(input: {
  incidentId: number;
  candidateId: number;
}) {
  const response = await apiClient.post<ApiEnvelope<RootCauseCandidate>>(
    `/api/incidents/${input.incidentId}/root-causes/${input.candidateId}/confirm`,
  );
  return response.data.data;
}

export async function getTimeline(anchorEventId: number) {
  const response = await apiClient.get<ApiEnvelope<TimelineResponse>>(
    "/api/timeline",
    {
      params: {
        anchorEventId,
        beforeMinutes: 120,
        afterMinutes: 30,
        includeEvidence: true,
      },
    },
  );
  return response.data.data;
}

export { toAPIErrorMessage };
