import { toAPIErrorMessage } from "@/api/knowledge";
import { apiClient } from "@/api/client";

type ApiEnvelope<T> = {
  code: number;
  message: string;
  data: T;
};

export type WorkflowNode = {
  id: string;
  type: "start" | "end" | "agent" | "skill" | "condition" | "merge";
  name?: string;
  agentName?: string;
  skillName?: string;
  config?: unknown;
};

export type WorkflowEdge = {
  from: string;
  to: string;
  condition?: string;
};

export type WorkflowDefinition = {
  name: string;
  version: string;
  description?: string;
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
  variables?: unknown;
};

export type WorkflowRecord = {
  id: number;
  name: string;
  version: string;
  description?: string;
  definition: WorkflowDefinition;
  enabled: boolean;
  createdBy?: number;
  createdAt: string;
  updatedAt: string;
};

export type WorkflowValidationResult = {
  valid: boolean;
  errors: string[];
  warnings: string[];
};

export type WorkflowNodeRun = {
  id: number;
  nodeId: string;
  nodeType: string;
  status: string;
  input?: unknown;
  output?: unknown;
  errorMessage?: string;
  attempt: number;
  startedAt?: string;
  finishedAt?: string;
};

export type WorkflowRun = {
  id: number;
  workflowId?: number;
  userId?: number;
  conversationId?: number;
  incidentId?: number;
  status: string;
  input?: unknown;
  output?: unknown;
  errorMessage?: string;
  startedAt?: string;
  finishedAt?: string;
  createdAt: string;
  nodeRuns?: WorkflowNodeRun[];
};

export async function listWorkflows() {
  const response =
    await apiClient.get<ApiEnvelope<WorkflowRecord[]>>("/api/workflows");
  return response.data.data;
}

export async function validateWorkflow(input: {
  name: string;
  version: string;
  description?: string;
  definition: WorkflowDefinition;
  enabled?: boolean;
}) {
  const response = await apiClient.post<ApiEnvelope<WorkflowValidationResult>>(
    "/api/workflows/0/validate",
    input,
  );
  return response.data.data;
}

export async function createWorkflow(input: {
  name: string;
  version: string;
  description?: string;
  definition: WorkflowDefinition;
  enabled?: boolean;
}) {
  const response = await apiClient.post<ApiEnvelope<WorkflowRecord>>(
    "/api/workflows",
    input,
  );
  return response.data.data;
}

export async function runWorkflow(workflowId: number, input: unknown) {
  const response = await apiClient.post<ApiEnvelope<WorkflowRun>>(
    `/api/workflows/${workflowId}/run`,
    { input },
  );
  return response.data.data;
}

export async function listWorkflowRuns() {
  const response =
    await apiClient.get<ApiEnvelope<WorkflowRun[]>>("/api/workflow-runs");
  return response.data.data;
}

export { toAPIErrorMessage };
