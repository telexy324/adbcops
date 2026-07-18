import { apiClient } from "@/api/client";
import { toAPIErrorMessage } from "@/api/knowledge";

type Envelope<T> = { code: number; message: string; data: T };

export type LinuxHost = {
  id: number;
  name: string;
  host: string;
  port: number;
  environment?: string;
  systemName?: string;
  componentName?: string;
  username?: string;
  authType: "password" | "private_key";
  credentialGroupId?: number | null;
  credentialConfigured: boolean;
  hostKeyPolicy: "strict" | "trust_on_first_use" | "insecure_skip_verify";
  hostKeyAlgorithm?: string;
  hostKeyFingerprint?: string;
  hostKeyStatus: "unverified" | "pending" | "trusted" | "mismatch";
  pendingHostKeyAlgorithm?: string;
  pendingHostKeyFingerprint?: string;
  profileId?: number | null;
  tags?: string[];
  enabled: boolean;
  connectionStatus: string;
  lastTestAt?: string;
  lastErrorCode?: string;
  lastErrorMessage?: string;
};

export type SaveLinuxHost = {
  name: string;
  host: string;
  port: number;
  environment?: string;
  systemName?: string;
  componentName?: string;
  username?: string;
  authType: "password" | "private_key";
  password?: string;
  privateKey?: string;
  privateKeyPassphrase?: string;
  credentialGroupId?: number | null;
  hostKeyPolicy: "strict" | "trust_on_first_use";
  hostKeyAlgorithm?: string;
  hostKeyFingerprint?: string;
  profileId?: number | null;
  tags: string[];
  enabled: boolean;
};

export type CredentialGroup = {
  id: number;
  name: string;
  authType: "password" | "private_key";
  username: string;
  credentialConfigured: boolean;
  enabled: boolean;
  version: number;
  scope?: { environments?: string[]; systemNames?: string[] };
};

export type SaveCredentialGroup = {
  name: string;
  authType: "password" | "private_key";
  username: string;
  password?: string;
  privateKey?: string;
  privateKeyPassphrase?: string;
  enabled: boolean;
};

export type HostGroup = {
  id: number;
  name: string;
  description?: string;
  environment?: string;
  systemName?: string;
  members?: LinuxHost[];
};
export type HostProfile = {
  id: number;
  name: string;
  description?: string;
  collectors: string[];
  builtIn: boolean;
  enabled: boolean;
};
export type ImportIssue = {
  row: number;
  field?: string;
  code: string;
  message: string;
};
export type ImportPreviewRow = {
  row: number;
  name: string;
  host: string;
  port: number;
  environment?: string;
  authType: string;
  credentialGroupName?: string;
  credentialConfigured: boolean;
  action: string;
  existingHostId?: number;
  issues?: ImportIssue[];
};
export type ImportPreview = {
  token: string;
  strategy: string;
  total: number;
  valid: number;
  invalid: number;
  duplicates: number;
  expiresAt: string;
  rows: ImportPreviewRow[];
  transactionPolicy: string;
};
export type ImportResult = {
  total: number;
  created: number;
  updated: number;
  skipped: number;
  failed: number;
  items: Array<{
    row: number;
    status: string;
    hostId?: number;
    code?: string;
    message?: string;
  }>;
};
export type BatchTestJob = {
  id: string;
  status: string;
  total: number;
  completed: number;
  success: number;
  failed: number;
  cancelled: number;
  progress: number;
  items: Array<{
    hostId: number;
    status: string;
    latencyMs?: number;
    serverVersion?: string;
    authMethod?: string;
    errorCode?: string;
    message?: string;
  }>;
};

export async function listLinuxHosts() {
  return (await apiClient.get<Envelope<LinuxHost[]>>("/api/linux/hosts")).data
    .data;
}
export async function createLinuxHost(data: SaveLinuxHost) {
  return (
    await apiClient.post<Envelope<LinuxHost>>(
      "/api/linux/hosts",
      cleanSecrets(data),
    )
  ).data.data;
}
export async function updateLinuxHost(
  id: number,
  data: Partial<SaveLinuxHost>,
) {
  return (
    await apiClient.put<Envelope<LinuxHost>>(
      `/api/linux/hosts/${id}`,
      cleanSecrets(data),
    )
  ).data.data;
}
export async function setLinuxHostEnabled(id: number, enabled: boolean) {
  return (
    await apiClient.post<Envelope<LinuxHost>>(
      `/api/linux/hosts/${id}/${enabled ? "enable" : "disable"}`,
    )
  ).data.data;
}
export async function confirmLinuxHostKey(
  id: number,
  algorithm: string,
  fingerprint: string,
) {
  return (
    await apiClient.post<Envelope<LinuxHost>>(
      `/api/linux/hosts/${id}/host-key/confirm`,
      { algorithm, fingerprint },
    )
  ).data.data;
}
export async function listCredentialGroups() {
  return (
    await apiClient.get<Envelope<CredentialGroup[]>>(
      "/api/linux/credential-groups",
    )
  ).data.data;
}
export async function createCredentialGroup(data: SaveCredentialGroup) {
  return (
    await apiClient.post<Envelope<CredentialGroup>>(
      "/api/linux/credential-groups",
      cleanSecrets(data),
    )
  ).data.data;
}
export async function updateCredentialGroup(
  id: number,
  data: Partial<SaveCredentialGroup>,
) {
  return (
    await apiClient.put<Envelope<CredentialGroup>>(
      `/api/linux/credential-groups/${id}`,
      cleanSecrets(data),
    )
  ).data.data;
}
export async function listHostGroups() {
  return (await apiClient.get<Envelope<HostGroup[]>>("/api/linux/host-groups"))
    .data.data;
}
export async function listHostProfiles() {
  return (
    await apiClient.get<Envelope<HostProfile[]>>("/api/linux/host-profiles")
  ).data.data;
}

export async function previewLinuxImport(
  file: File,
  strategy: string,
  columnMapping: Record<string, string>,
) {
  const form = new FormData();
  form.append("file", file);
  form.append("strategy", strategy);
  form.append("columnMapping", JSON.stringify(columnMapping));
  return (
    await apiClient.post<Envelope<ImportPreview>>(
      "/api/linux/hosts/import/preview",
      form,
    )
  ).data.data;
}
export async function confirmLinuxImport(token: string) {
  return (
    await apiClient.post<Envelope<ImportResult>>(
      "/api/linux/hosts/import/confirm",
      { token },
    )
  ).data.data;
}
export async function startLinuxBatchTest(hostIds: number[]) {
  return (
    await apiClient.post<Envelope<BatchTestJob>>(
      "/api/linux/hosts/batch-test",
      { hostIds },
    )
  ).data.data;
}
export async function getLinuxBatchTest(id: string) {
  return (
    await apiClient.get<Envelope<BatchTestJob>>(
      `/api/linux/hosts/batch-test/${id}`,
    )
  ).data.data;
}
export async function cancelLinuxBatchTest(id: string) {
  return (
    await apiClient.post<Envelope<BatchTestJob>>(
      `/api/linux/hosts/batch-test/${id}/cancel`,
    )
  ).data.data;
}
export async function downloadLinuxBatchTest(
  id: string,
  format: "json" | "csv",
) {
  return (
    await apiClient.get<Blob>(`/api/linux/hosts/batch-test/${id}/download`, {
      params: { format },
      responseType: "blob",
    })
  ).data;
}

function cleanSecrets<T extends object>(input: T): T {
  const payload = { ...input } as T & Record<string, unknown>;
  for (const key of ["password", "privateKey", "privateKeyPassphrase"])
    if (typeof payload[key] === "string" && !(payload[key] as string).trim())
      delete payload[key];
  return payload;
}

export { toAPIErrorMessage };
