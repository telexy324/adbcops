import { apiClient } from "@/api/client";
import { toAPIErrorMessage } from "@/api/knowledge";

type ApiEnvelope<T> = {
  code: number;
  message: string;
  data: T;
};

export type LLMConfig = {
  id: number;
  name: string;
  provider: string;
  baseUrl: string;
  model: string;
  purpose: "chat" | "embedding" | "rerank";
  temperature: number;
  enabled: boolean;
  isDefault: boolean;
  apiKeyConfigured: boolean;
  createdAt: string;
  updatedAt: string;
};

export type SaveLLMConfigInput = {
  name: string;
  provider: string;
  baseUrl: string;
  model: string;
  purpose: "chat" | "embedding" | "rerank";
  apiKey?: string;
  temperature: number;
  enabled: boolean;
  isDefault: boolean;
};

export type DataSourceType = "elasticsearch" | "kubernetes" | "prometheus";

export type DataSource = {
  id: number;
  name: string;
  sourceType: DataSourceType | string;
  environment?: string;
  systemName?: string;
  componentName?: string;
  config: unknown;
  credentialConfigured: boolean;
  enabled: boolean;
  readOnly: boolean;
  createdAt: string;
  updatedAt: string;
};

export type SaveDataSourceInput = {
  name: string;
  sourceType: DataSourceType;
  environment?: string;
  systemName?: string;
  componentName?: string;
  config: unknown;
  credential?: unknown;
  enabled: boolean;
  readOnly: boolean;
};

export type DataSourceTestResult = {
  ok: boolean;
  sourceType: string;
  credentialConfigured: boolean;
  message: string;
};

export async function listLLMConfigs() {
  const response =
    await apiClient.get<ApiEnvelope<LLMConfig[]>>("/api/llm-configs");
  return response.data.data;
}

export async function createLLMConfig(input: SaveLLMConfigInput) {
  const payload = { ...input };
  if (!payload.apiKey?.trim()) {
    delete payload.apiKey;
  }
  const response = await apiClient.post<ApiEnvelope<LLMConfig>>(
    "/api/llm-configs",
    payload,
  );
  return response.data.data;
}

export async function testLLMConfig(id: number, prompt = "Say ok.") {
  const response = await apiClient.post<ApiEnvelope<unknown>>(
    `/api/llm-configs/${id}/test`,
    { prompt },
  );
  return response.data.data;
}

export async function listDataSources() {
  const response =
    await apiClient.get<ApiEnvelope<DataSource[]>>("/api/data-sources");
  return response.data.data;
}

export async function createDataSource(input: SaveDataSourceInput) {
  const response = await apiClient.post<ApiEnvelope<DataSource>>(
    "/api/data-sources",
    input,
  );
  return response.data.data;
}

export async function testDataSource(id: number) {
  const response = await apiClient.post<ApiEnvelope<DataSourceTestResult>>(
    `/api/data-sources/${id}/test`,
  );
  return response.data.data;
}

export { toAPIErrorMessage };
