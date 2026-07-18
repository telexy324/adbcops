import { apiClient } from "@/api/client";
import type { TopologyGraph } from "@/api/operations";
import type { WorkflowRun } from "@/api/workflows";

type Envelope<T> = { code: number; message: string; data: T };

export type LinuxEvidence = {
  id: number;
  evidenceKey: string;
  sourceType: string;
  sourceRef?: unknown;
  observedAt?: string;
  title?: string;
  summary: string;
  content?: unknown;
  confidence?: number;
  sensitivity?: string;
  createdAt: string;
};

export type LinuxEvent = {
  id: number;
  eventTime: string;
  sourceType: string;
  sourceId?: string;
  eventType: string;
  severity?: string;
  status: string;
  environment?: string;
  systemName?: string;
  componentName?: string;
  resourceName?: string;
  host?: string;
  summary: string;
  payload?: unknown;
  occurrenceCount: number;
  firstSeenAt: string;
  lastSeenAt: string;
};

export type CreatedIncident = {
  incident: {
    id: number;
    title: string;
    severity: string;
    status: string;
  };
};

export async function listLinuxEvidence(limit = 500): Promise<LinuxEvidence[]> {
  const response = await apiClient.get<Envelope<LinuxEvidence[]>>(
    "/api/evidence",
    { params: { sourceType: "linux_server", limit } },
  );
  return response.data.data.map(normalizeEvidence);
}

export async function listLinuxEvents(input: {
  resourceName?: string;
  environment?: string;
  limit?: number;
}): Promise<LinuxEvent[]> {
  const response = await apiClient.get<Envelope<LinuxEvent[]>>("/api/events", {
    params: {
      resourceName: input.resourceName,
      environment: input.environment,
      limit: input.limit ?? 300,
    },
  });
  return response.data.data.map((event) => ({
    ...event,
    payload: decodeJSONField(event.payload),
  }));
}

export async function getLinuxTopology(environment?: string) {
  const response = await apiClient.get<Envelope<TopologyGraph>>(
    "/api/topology/graph",
    { params: { environment, limit: 500 } },
  );
  return response.data.data;
}

export async function createLinuxHostIncident(input: {
  title: string;
  severity: "critical" | "warning" | "info";
  environment?: string;
  systemName?: string;
  componentName?: string;
  summary: string;
  eventIds: number[];
  evidenceKeys: string[];
  hostId: number;
}) {
  const response = await apiClient.post<Envelope<CreatedIncident>>(
    "/api/incidents",
    {
      title: input.title,
      severity: input.severity,
      status: "open",
      environment: input.environment,
      systemName: input.systemName,
      componentName: input.componentName,
      summary: input.summary,
      tags: ["linux", `host:${input.hostId}`],
      eventIds: input.eventIds,
      evidenceKeys: input.evidenceKeys,
      rootCauses: [],
    },
  );
  return response.data.data;
}

export function normalizeEvidence(record: LinuxEvidence): LinuxEvidence {
  return {
    ...record,
    sourceRef: decodeJSONField(record.sourceRef),
    content: decodeJSONField(record.content),
  };
}

export function decodeJSONField(value: unknown): unknown {
  if (value == null || typeof value === "object") return value;
  if (typeof value !== "string" || !value.trim()) return value;
  try {
    return JSON.parse(value);
  } catch {
    try {
      return JSON.parse(decodeURIComponent(escape(window.atob(value))));
    } catch {
      return undefined;
    }
  }
}

export function hostIDFromSourceRef(sourceRef: unknown): number | undefined {
  const normalized = decodeJSONField(sourceRef);
  if (!normalized || typeof normalized !== "object") return undefined;
  const value = (normalized as Record<string, unknown>).hostId;
  return typeof value === "number" ? value : undefined;
}

export function workflowIncludesHost(run: WorkflowRun, hostId: number) {
  const input = decodeJSONField(run.input);
  if (!input || typeof input !== "object") return false;
  const body = input as Record<string, unknown>;
  return (
    body.hostId === hostId ||
    (Array.isArray(body.hostIds) && body.hostIds.includes(hostId))
  );
}
