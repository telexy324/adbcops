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

export type TopologyNodeInput = {
  nodeKey?: string;
  kind: string;
  name: string;
  displayName?: string;
  environment?: string;
  cluster?: string;
  namespace?: string;
  labels?: Record<string, unknown>;
  properties?: Record<string, unknown>;
  sourceType?: string;
};

export type TopologyEdgeInput = {
  edgeKey?: string;
  fromNodeKey: string;
  toNodeKey: string;
  edgeType: string;
  confidence?: number;
  properties?: Record<string, unknown>;
  sourceType?: string;
};

export type BlastRadius = TopologyGraph & {
  rootKey: string;
  direction: string;
  hops: number;
  cycleDetected: boolean;
};

export type TopologyPath = {
  targetNodeKey: string;
  via?: string;
  hops: number;
  nodeKeys: string[];
  edgeKeys: string[];
  confidence: number;
  impactType: string;
  evidenceKey?: string;
};

export type ExpandTopologyResult = TopologyGraph & {
  rootKey: string;
  direction: string;
  depth: number;
  evidenceKey?: string;
  paths: TopologyPath[];
  cycleDetected: boolean;
  truncated: boolean;
};

export type TopologyDirection = "both" | "upstream" | "downstream";

export type ExpandTopologyInput = {
  nodeKey: string;
  depth?: number;
  direction?: TopologyDirection;
  maxNodes?: number;
  maxEdges?: number;
  onlyPropagating?: boolean;
  semantics?: string[];
  observedNodeKeys?: string[];
  environment?: string;
  cluster?: string;
  namespace?: string;
};

export type TopologySavedView = {
  id: number;
  name: string;
  description?: string;
  ownerId: number;
  visibility: "private" | "team" | "public";
  centerNodeId?: number;
  queryConfig: unknown;
  displayConfig: unknown;
  layoutData?: unknown;
  isDefault: boolean;
  createdAt: string;
  updatedAt: string;
};

export type TopologySavedViewInput = {
  name: string;
  description?: string;
  visibility?: "private" | "team" | "public";
  centerNodeId?: number;
  queryConfig?: Record<string, unknown>;
  displayConfig?: Record<string, unknown>;
  layoutData?: Record<string, unknown>;
  isDefault?: boolean;
};

export type TopologyNodeType = {
  id: number;
  typeKey: string;
  displayName: string;
  category?: string;
  icon?: string;
  defaultColor?: string;
  identityFields?: unknown;
  searchableFields?: unknown;
  defaultLabelTemplate?: string;
  detailFields?: unknown;
  enabled: boolean;
  builtIn: boolean;
  createdAt: string;
  updatedAt: string;
};

export type TopologyRelationType = {
  id: number;
  typeKey: string;
  displayName: string;
  semantics: string;
  failurePropagation: string;
  defaultDirection: string;
  propagatesFailure: boolean;
  allowedSourceTypes?: unknown;
  allowedTargetTypes?: unknown;
  style?: unknown;
  enabled: boolean;
  builtIn: boolean;
  createdAt: string;
  updatedAt: string;
};

export type TopologyRelationTypeInput = {
  typeKey: string;
  displayName: string;
  semantics: string;
  failurePropagation: string;
  defaultDirection: string;
  propagatesFailure?: boolean;
  allowedSourceTypes?: unknown;
  allowedTargetTypes?: unknown;
  style?: unknown;
  enabled?: boolean;
};

export type TopologySourceConfig = {
  id: number;
  name: string;
  sourceType: string;
  dataSourceId?: number;
  enabled: boolean;
  priority: number;
  schedule?: string;
  scope?: unknown;
  mappingRules?: unknown;
  staleAfterSeconds: number;
  deleteAfterSeconds: number;
  lastSyncAt?: string;
  nextSyncAt?: string;
  createdBy?: number;
  createdAt: string;
  updatedAt: string;
};

export type TopologySourceConfigInput = {
  name: string;
  sourceType: string;
  dataSourceId?: number;
  enabled?: boolean;
  priority?: number;
  schedule?: string;
  scope?: unknown;
  mappingRules?: unknown;
  staleAfterSeconds?: number;
  deleteAfterSeconds?: number;
};

export type MappingPreviewResult = {
  nodes: Array<{
    nodeKey: string;
    nodeType: string;
    name: string;
    attributes?: Record<string, unknown>;
    aliases?: string[];
  }>;
  edges: Array<{
    fromNodeKey: string;
    toNodeKey: string;
    relationType: string;
    confidence?: number;
  }>;
  unresolved: string[];
  warnings: string[];
  truncated: boolean;
};

export type TopologySyncRun = {
  id: number;
  sourceConfigId: number;
  triggerType: string;
  status: string;
  discoveredNodes: number;
  discoveredEdges: number;
  createdNodes: number;
  updatedNodes: number;
  staleNodes: number;
  createdEdges: number;
  updatedEdges: number;
  staleEdges: number;
  conflictCount: number;
  warningCount: number;
  errorMessage?: string;
  detail?: unknown;
  startedAt?: string;
  finishedAt?: string;
  createdAt: string;
};

export type TopologyConflict = {
  id: number;
  conflictType: string;
  status: string;
  nodeId?: number;
  edgeId?: number;
  sourceConfigId?: number;
  description: string;
  candidates?: unknown;
  resolution?: unknown;
  resolvedBy?: number;
  resolvedAt?: string;
  createdAt: string;
};

export type ResolveTopologyConflictInput = {
  conflictId: number;
  action: "prefer" | "merge" | "manual" | "ignore";
  note?: string;
  prefer?: string;
  manualValue?: unknown;
  mergePatch?: unknown;
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

export type SimilarIncident = {
  incident: Incident;
  score: number;
  reasons: string[];
  advisoryOnly: boolean;
  notice: string;
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

export async function createTopologyNode(input: TopologyNodeInput) {
  const response = await apiClient.post<ApiEnvelope<TopologyNode>>(
    "/api/topology/nodes",
    {
      ...input,
      sourceType: input.sourceType ?? "manual",
      labels: input.labels ?? {},
      properties: input.properties ?? {},
    },
  );
  return response.data.data;
}

export async function updateTopologyNode(input: {
  id: number;
  data: TopologyNodeInput;
}) {
  const response = await apiClient.put<ApiEnvelope<TopologyNode>>(
    `/api/topology/nodes/${input.id}`,
    {
      ...input.data,
      sourceType: input.data.sourceType ?? "manual",
      labels: input.data.labels ?? {},
      properties: input.data.properties ?? {},
    },
  );
  return response.data.data;
}

export async function deleteTopologyNode(id: number) {
  const response = await apiClient.delete<ApiEnvelope<{ deleted: boolean }>>(
    `/api/topology/nodes/${id}`,
  );
  return response.data.data;
}

export async function createTopologyEdge(input: TopologyEdgeInput) {
  const response = await apiClient.post<ApiEnvelope<TopologyEdge>>(
    "/api/topology/edges",
    {
      ...input,
      sourceType: input.sourceType ?? "manual",
      properties: input.properties ?? {},
    },
  );
  return response.data.data;
}

export async function updateTopologyEdge(input: {
  id: number;
  data: TopologyEdgeInput;
}) {
  const response = await apiClient.put<ApiEnvelope<TopologyEdge>>(
    `/api/topology/edges/${input.id}`,
    {
      ...input.data,
      sourceType: input.data.sourceType ?? "manual",
      properties: input.data.properties ?? {},
    },
  );
  return response.data.data;
}

export async function deleteTopologyEdge(id: number) {
  const response = await apiClient.delete<ApiEnvelope<{ deleted: boolean }>>(
    `/api/topology/edges/${id}`,
  );
  return response.data.data;
}

export async function expandTopology(input: ExpandTopologyInput) {
  const response = await apiClient.get<ApiEnvelope<ExpandTopologyResult>>(
    "/api/topology/expand",
    {
      params: {
        nodeKey: input.nodeKey,
        depth: input.depth ?? 2,
        direction: input.direction ?? "both",
        maxNodes: input.maxNodes ?? 200,
        maxEdges: input.maxEdges ?? 400,
        onlyPropagating: input.onlyPropagating ?? false,
        semantics: input.semantics?.join(","),
        observedNodeKeys: input.observedNodeKeys?.join(","),
        environment: input.environment,
        cluster: input.cluster,
        namespace: input.namespace,
      },
    },
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

export async function listTopologySavedViews(limit = 30) {
  const response = await apiClient.get<ApiEnvelope<TopologySavedView[]>>(
    "/api/topology/views",
    { params: { limit } },
  );
  return response.data.data;
}

export async function createTopologySavedView(input: TopologySavedViewInput) {
  const response = await apiClient.post<ApiEnvelope<TopologySavedView>>(
    "/api/topology/views",
    {
      name: input.name,
      description: input.description,
      visibility: input.visibility ?? "private",
      centerNodeId: input.centerNodeId,
      queryConfig: input.queryConfig ?? {},
      displayConfig: input.displayConfig ?? {
        layout: "svg-layered",
        showLabels: true,
      },
      layoutData: input.layoutData,
      isDefault: input.isDefault ?? false,
    },
  );
  return response.data.data;
}

export async function listTopologyNodeTypes() {
  const response = await apiClient.get<ApiEnvelope<TopologyNodeType[]>>(
    "/api/topology/node-types",
  );
  return response.data.data;
}

export async function listTopologyRelationTypes() {
  const response = await apiClient.get<ApiEnvelope<TopologyRelationType[]>>(
    "/api/topology/relation-types",
  );
  return response.data.data;
}

export async function updateTopologyRelationType(input: {
  id: number;
  data: TopologyRelationTypeInput;
}) {
  const response = await apiClient.put<ApiEnvelope<TopologyRelationType>>(
    `/api/topology/relation-types/${input.id}`,
    input.data,
  );
  return response.data.data;
}

export async function listTopologySources() {
  const response = await apiClient.get<ApiEnvelope<TopologySourceConfig[]>>(
    "/api/topology/sources",
  );
  return response.data.data;
}

export async function createTopologySource(input: TopologySourceConfigInput) {
  const response = await apiClient.post<ApiEnvelope<TopologySourceConfig>>(
    "/api/topology/sources",
    {
      name: input.name,
      sourceType: input.sourceType,
      dataSourceId: input.dataSourceId,
      enabled: input.enabled ?? true,
      priority: input.priority ?? 100,
      schedule: input.schedule,
      scope: input.scope ?? {},
      mappingRules: input.mappingRules ?? {},
      staleAfterSeconds: input.staleAfterSeconds ?? 86_400,
      deleteAfterSeconds: input.deleteAfterSeconds ?? 604_800,
    },
  );
  return response.data.data;
}

export async function updateTopologySource(input: {
  id: number;
  data: TopologySourceConfigInput;
}) {
  const response = await apiClient.put<ApiEnvelope<TopologySourceConfig>>(
    `/api/topology/sources/${input.id}`,
    {
      name: input.data.name,
      sourceType: input.data.sourceType,
      dataSourceId: input.data.dataSourceId,
      enabled: input.data.enabled ?? true,
      priority: input.data.priority ?? 100,
      schedule: input.data.schedule,
      scope: input.data.scope ?? {},
      mappingRules: input.data.mappingRules ?? {},
      staleAfterSeconds: input.data.staleAfterSeconds ?? 86_400,
      deleteAfterSeconds: input.data.deleteAfterSeconds ?? 604_800,
    },
  );
  return response.data.data;
}

export async function previewTopologySourceMapping(input: {
  sourceId: number;
  mappingRules: unknown;
  sampleData: unknown;
  limit?: number;
}) {
  const response = await apiClient.post<ApiEnvelope<MappingPreviewResult>>(
    `/api/topology/sources/${input.sourceId}/preview`,
    {
      mappingRules: input.mappingRules,
      sampleData: input.sampleData,
      limit: input.limit ?? 20,
    },
  );
  return response.data.data;
}

export async function runTopologySourceSync(input: {
  sourceId: number;
  triggerType?: "manual" | "scheduled";
  dryRun?: boolean;
}) {
  const response = await apiClient.post<ApiEnvelope<TopologySyncRun>>(
    `/api/topology/sources/${input.sourceId}/run`,
    {
      triggerType: input.triggerType ?? "manual",
      dryRun: input.dryRun ?? false,
    },
  );
  return response.data.data;
}

export async function listTopologySyncRuns(input?: {
  sourceConfigId?: number;
  limit?: number;
}) {
  const response = await apiClient.get<ApiEnvelope<TopologySyncRun[]>>(
    "/api/topology/sync-runs",
    {
      params: {
        sourceConfigId: input?.sourceConfigId,
        limit: input?.limit ?? 20,
      },
    },
  );
  return response.data.data;
}

export async function listTopologyConflicts(input?: {
  status?: string;
  conflictType?: string;
  limit?: number;
}) {
  const response = await apiClient.get<ApiEnvelope<TopologyConflict[]>>(
    "/api/topology/conflicts",
    {
      params: {
        status: input?.status,
        conflictType: input?.conflictType,
        limit: input?.limit ?? 30,
      },
    },
  );
  return response.data.data;
}

export async function resolveTopologyConflict(
  input: ResolveTopologyConflictInput,
) {
  const response = await apiClient.post<ApiEnvelope<TopologyConflict>>(
    `/api/topology/conflicts/${input.conflictId}/resolve`,
    {
      action: input.action,
      note: input.note,
      prefer: input.prefer,
      manualValue: input.manualValue,
      mergePatch: input.mergePatch,
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

export async function getSimilarIncidents(incidentId: number, limit = 5) {
  const response = await apiClient.get<ApiEnvelope<SimilarIncident[]>>(
    `/api/incidents/${incidentId}/similar`,
    { params: { limit } },
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
