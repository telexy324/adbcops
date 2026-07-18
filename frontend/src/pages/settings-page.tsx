import { FormEvent, ReactNode, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import {
  CheckCircle2,
  DatabaseZap,
  KeyRound,
  Loader2,
  Network,
  Pencil,
  Save,
  ServerCog,
  Settings2,
  TestTube2,
  X,
} from "lucide-react";

import {
  createDataSource,
  createLLMConfig,
  listDataSources,
  listLLMConfigs,
  testDataSource,
  testLLMConfig,
  toAPIErrorMessage,
  updateDataSource,
  updateLLMConfig,
  type DataSource,
  type DataSourceType,
  type LLMConfig,
  type SaveDataSourceInput,
  type SaveLLMConfigInput,
} from "@/api/config";
import { Button, buttonVariants } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

const dataSourceKinds: Array<{
  type: DataSourceType;
  title: string;
  description: string;
  icon: typeof DatabaseZap;
  config: unknown;
  credential: unknown;
}> = [
  {
    type: "elasticsearch",
    title: "日志数据源",
    description: "Elasticsearch / OpenSearch 兼容日志查询入口。",
    icon: DatabaseZap,
    config: {
      baseUrl: "https://es.example.com",
      index: "logs-*",
      insecureSkipTlsVerify: false,
    },
    credential: { username: "elastic", password: "replace-me" },
  },
  {
    type: "kubernetes",
    title: "K8s 数据源",
    description: "只读 Kubernetes API，用于 Pod 诊断和拓扑同步。",
    icon: Network,
    config: {
      apiServer: "https://kubernetes.example.com",
      allowedNamespaces: ["default", "prod"],
      insecureSkipTlsVerify: false,
    },
    credential: { bearerToken: "replace-me" },
  },
  {
    type: "prometheus",
    title: "Prometheus 数据源",
    description: "Prometheus HTTP API，用于 instant/range 指标查询。",
    icon: ServerCog,
    config: {
      baseUrl: "https://prometheus.example.com",
      insecureSkipTlsVerify: false,
    },
    credential: { bearerToken: "replace-me" },
  },
  {
    type: "nacos",
    title: "Nacos 数据源",
    description:
      "只读 Nacos OpenAPI，用于服务实例、配置元数据、配置变更和客户端连接诊断。",
    icon: Network,
    config: {
      baseUrl: "https://nacos.example.com",
      namespace: "prod",
      defaultGroup: "DEFAULT_GROUP",
      allowedNamespaces: ["prod"],
      allowedGroups: ["DEFAULT_GROUP"],
      allowConfigContent: false,
      insecureSkipTlsVerify: false,
    },
    credential: { username: "readonly", password: "replace-me" },
  },
  {
    type: "redis",
    title: "Redis 数据源",
    description:
      "只读 Redis standalone / Sentinel / Cluster，用于内存、连接池、主从和集群诊断。",
    icon: DatabaseZap,
    config: {
      mode: "cluster",
      endpoints: ["redis-1.example.com:6379"],
      allowValueRead: false,
      scanMaxIterations: 20,
      scanMaxKeys: 1000,
    },
    credential: { username: "readonly", password: "replace-me" },
  },
  {
    type: "tidb",
    title: "TiDB 数据源",
    description:
      "只读 TiDB SQL / Status API，用于慢 SQL、Processlist、锁等待、热点 Region 和 EXPLAIN。",
    icon: DatabaseZap,
    config: {
      dsn: "readonly@tcp(tidb.example.com:4000)/information_schema",
      statusBaseUrl: "https://tidb-status.example.com",
      explainAnalyzeEnabled: false,
      maxRows: 500,
    },
    credential: { username: "readonly", password: "replace-me" },
  },
  {
    type: "nginx",
    title: "Nginx 数据源",
    description:
      "只读 Nginx 日志、指标、Upstream 状态和配置元数据，用于 499/502/503/504 诊断。",
    icon: ServerCog,
    config: {
      baseUrl: "https://nginx-metadata.example.com",
      maskClientIp: true,
      configContentEnabled: false,
      logIndex: "nginx-*",
      insecureSkipTlsVerify: false,
    },
    credential: { bearerToken: "replace-me" },
  },
];

const defaultLLMForm = {
  name: "default-openai-compatible",
  provider: "openai-compatible",
  baseUrl: "https://api.openai.example/v1",
  model: "gpt-compatible-model",
  purpose: "chat" as LLMPurpose,
  apiKey: "",
  appKey: "",
  apiSecret: "",
  temperature: "0.2",
  enabled: true,
  isDefault: true,
};

type LLMPurpose = "chat" | "embedding" | "rerank";

type TestNotification = {
  success: boolean;
  message: string;
};

const llmPurposeKinds: Array<{
  purpose: LLMPurpose;
  title: string;
  description: string;
  defaultName: string;
  defaultModel: string;
  defaultTemperature: string;
}> = [
  {
    purpose: "chat",
    title: "LLM / Chat",
    description: "用于分析报告、查询改写、Agent 总结和知识库问答生成。",
    defaultName: "default-chat-model",
    defaultModel: "gpt-compatible-model",
    defaultTemperature: "0.2",
  },
  {
    purpose: "embedding",
    title: "Embedding 向量模型",
    description: "用于知识库持久化向量索引、语义召回和相似度排序。",
    defaultName: "default-embedding-model",
    defaultModel: "text-embedding-compatible-model",
    defaultTemperature: "0",
  },
  {
    purpose: "rerank",
    title: "Rerank 精排模型",
    description: "用于知识库候选片段精排；不可用时自动降级为本地重排。",
    defaultName: "default-rerank-model",
    defaultModel: "rerank-compatible-model",
    defaultTemperature: "0",
  },
];

export function SettingsPage() {
  const queryClient = useQueryClient();
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [testNotification, setTestNotification] =
    useState<TestNotification | null>(null);
  const [llmForm, setLLMForm] = useState(defaultLLMForm);
  const [editingLLMId, setEditingLLMId] = useState<number | null>(null);
  const [selectedType, setSelectedType] =
    useState<DataSourceType>("elasticsearch");
  const selectedTemplate = useMemo(
    () => dataSourceKinds.find((item) => item.type === selectedType)!,
    [selectedType],
  );
  const [sourceForm, setSourceForm] = useState(() =>
    sourceDefaults(selectedTemplate),
  );
  const [editingDataSourceId, setEditingDataSourceId] = useState<number | null>(
    null,
  );

  const llmQuery = useQuery({
    queryKey: ["settings", "llm-configs"],
    queryFn: listLLMConfigs,
  });
  const dataSourcesQuery = useQuery({
    queryKey: ["settings", "data-sources"],
    queryFn: listDataSources,
  });

  const llmMutation = useMutation({
    mutationFn: (input: { id: number | null; data: SaveLLMConfigInput }) =>
      input.id
        ? updateLLMConfig({ id: input.id, data: input.data })
        : createLLMConfig(input.data),
    onSuccess: (config) => {
      setNotice(
        `LLM 配置 ${config.name} 已${editingLLMId ? "更新" : "保存"}。`,
      );
      setError(null);
      setEditingLLMId(null);
      setLLMForm((current) => ({
        ...current,
        apiKey: "",
        appKey: "",
        apiSecret: "",
      }));
      queryClient.invalidateQueries({ queryKey: ["settings", "llm-configs"] });
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const sourceMutation = useMutation({
    mutationFn: (input: { id: number | null; data: SaveDataSourceInput }) =>
      input.id
        ? updateDataSource({ id: input.id, data: input.data })
        : createDataSource(input.data),
    onSuccess: (source) => {
      const message = `${sourceLabel(source.sourceType)} ${source.name} 已${
        editingDataSourceId ? "更新" : "保存"
      }。`;
      setNotice(null);
      setTestNotification({ success: true, message });
      setError(null);
      setEditingDataSourceId(null);
      setSourceForm(sourceDefaults(selectedTemplate));
      queryClient.invalidateQueries({ queryKey: ["settings", "data-sources"] });
    },
    onError: (err) => {
      const message = `${editingDataSourceId ? "更新" : "保存"}数据源失败：${toAPIErrorMessage(err)}`;
      setError(message);
      setTestNotification({ success: false, message });
    },
  });

  const testLLMMutation = useMutation({
    mutationFn: (input: { id: number; name: string }) =>
      testLLMConfig(input.id),
    onSuccess: (result, input) => {
      setTestNotification({
        success: result.ok,
        message: result.ok
          ? `模型配置“${input.name}”测试成功：${result.model}`
          : `模型配置“${input.name}”测试失败：${result.content || "服务未返回有效结果"}`,
      });
    },
    onError: (err, input) =>
      setTestNotification({
        success: false,
        message: `模型配置“${input.name}”测试失败：${toAPIErrorMessage(err)}`,
      }),
  });

  const testSourceMutation = useMutation({
    mutationFn: (input: { id: number; name: string }) =>
      testDataSource(input.id),
    onSuccess: (result, input) => {
      setTestNotification({
        success: result.ok,
        message: result.ok
          ? `数据源“${input.name}”测试成功：${result.message}`
          : `数据源“${input.name}”测试失败：${result.message || "连接未通过"}`,
      });
    },
    onError: (err, input) =>
      setTestNotification({
        success: false,
        message: `数据源“${input.name}”测试失败：${toAPIErrorMessage(err)}`,
      }),
  });

  function selectType(type: DataSourceType) {
    const template = dataSourceKinds.find((item) => item.type === type)!;
    setSelectedType(type);
    setEditingDataSourceId(null);
    setSourceForm(sourceDefaults(template));
  }

  function selectLLMPurpose(purpose: LLMPurpose) {
    const template = llmPurposeKinds.find((item) => item.purpose === purpose)!;
    setLLMForm((current) => ({
      ...current,
      name:
        current.purpose === purpose || current.name.trim() === ""
          ? current.name
          : template.defaultName,
      model:
        current.purpose === purpose || current.model.trim() === ""
          ? current.model
          : template.defaultModel,
      purpose,
      temperature: template.defaultTemperature,
      isDefault: true,
    }));
  }

  function submitLLM(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    llmMutation.mutate({
      id: editingLLMId,
      data: {
        name: llmForm.name,
        provider: llmForm.provider,
        baseUrl: llmForm.baseUrl,
        model: llmForm.model,
        purpose: llmForm.purpose,
        apiKey: llmForm.apiKey,
        appKey: llmForm.appKey,
        apiSecret: llmForm.apiSecret,
        temperature: Number(llmForm.temperature),
        enabled: llmForm.enabled,
        isDefault: llmForm.isDefault,
      },
    });
  }

  function submitDataSource(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!sourceForm.name.trim()) {
      const message = "数据源名称不能为空。";
      setError(message);
      setTestNotification({ success: false, message });
      return;
    }
    const config = parseJSON(sourceForm.configJSON, "Config JSON");
    const credential = parseOptionalJSON(
      sourceForm.credentialJSON,
      "Credential JSON",
    );
    if (!config.ok || !credential.ok) {
      const message = config.error ?? credential.error ?? "JSON 格式不正确。";
      setError(message);
      setTestNotification({ success: false, message });
      return;
    }
    if (!isJSONObject(config.value)) {
      const message = "Config JSON 必须是对象。";
      setError(message);
      setTestNotification({ success: false, message });
      return;
    }
    const configValue = {
      ...config.value,
      ...(supportsHTTPSConfig(selectedType)
        ? { insecureSkipTlsVerify: sourceForm.insecureSkipTLS }
        : {}),
    };
    setError(null);
    sourceMutation.mutate({
      id: editingDataSourceId,
      data: {
        name: sourceForm.name,
        sourceType: selectedType,
        environment: sourceForm.environment,
        systemName: sourceForm.systemName,
        componentName: sourceForm.componentName,
        config: configValue,
        credential: credential.value,
        enabled: sourceForm.enabled,
        readOnly: true,
      },
    });
  }

  function editLLMConfig(item: LLMConfig) {
    setEditingLLMId(item.id);
    setLLMForm({
      name: item.name,
      provider: item.provider,
      baseUrl: item.baseUrl,
      model: item.model,
      purpose: item.purpose,
      apiKey: "",
      appKey: "",
      apiSecret: "",
      temperature: String(item.temperature),
      enabled: item.enabled,
      isDefault: item.isDefault,
    });
    setNotice(`正在编辑 LLM 配置 ${item.name}。`);
    setError(null);
  }

  function cancelLLMEdit() {
    setEditingLLMId(null);
    setLLMForm(defaultLLMForm);
    setNotice(null);
  }

  function editDataSource(item: DataSource) {
    const type = toKnownDataSourceType(item.sourceType);
    if (!type) {
      setError(`暂不支持编辑未知数据源类型：${item.sourceType}`);
      return;
    }
    setSelectedType(type);
    setEditingDataSourceId(item.id);
    setSourceForm({
      name: item.name,
      environment: item.environment ?? "",
      systemName: item.systemName ?? "",
      componentName: item.componentName ?? "",
      configJSON: JSON.stringify(item.config ?? {}, null, 2),
      credentialJSON: "",
      enabled: item.enabled,
      insecureSkipTLS: readInsecureSkipTLS(item.config),
    });
    setNotice(`正在编辑数据源 ${item.name}。凭据不回显，留空表示不修改。`);
    setError(null);
  }

  function cancelDataSourceEdit() {
    setEditingDataSourceId(null);
    setSourceForm(sourceDefaults(selectedTemplate));
    setNotice(null);
  }

  const llmConfigs = llmQuery.data ?? [];
  const dataSources = dataSourcesQuery.data ?? [];
  const llmConfigsByPurpose = useMemo(
    () =>
      Object.fromEntries(
        llmPurposeKinds.map((item) => [
          item.purpose,
          llmConfigs.filter((config) => config.purpose === item.purpose),
        ]),
      ) as Record<LLMPurpose, LLMConfig[]>,
    [llmConfigs],
  );

  return (
    <div className="mx-auto max-w-[1700px] space-y-6">
      <section className="flex flex-col justify-between gap-4 xl:flex-row xl:items-end">
        <div>
          <p className="text-sm font-medium text-cyan-700">Settings</p>
          <h1 className="mt-1 text-3xl font-semibold tracking-tight text-slate-950">
            配置中心
          </h1>
          <p className="mt-2 max-w-3xl text-sm leading-6 text-slate-500">
            管理 Chat LLM、Embedding、Rerank
            模型，以及日志、Kubernetes、Prometheus
            和组件诊断数据源。凭据仅提交到后端加密存储，页面只展示“已配置”状态。
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <StatusPill label="Admin only" />
          <StatusPill label="凭据不回显" />
          <StatusPill label="只读数据源" />
        </div>
      </section>

      {(notice || error) && (
        <div
          role="status"
          className={cn(
            "rounded-xl border px-4 py-3 text-sm",
            error
              ? "border-rose-200 bg-rose-50 text-rose-700"
              : "border-emerald-200 bg-emerald-50 text-emerald-700",
          )}
        >
          {error ?? notice}
        </div>
      )}

      {testNotification && (
        <div
          role={testNotification.success ? "status" : "alert"}
          aria-live="polite"
          className={cn(
            "fixed bottom-6 right-6 z-50 flex max-w-md items-start gap-3 rounded-xl border px-4 py-3 text-sm shadow-lg",
            testNotification.success
              ? "border-emerald-200 bg-emerald-50 text-emerald-800"
              : "border-rose-200 bg-rose-50 text-rose-800",
          )}
        >
          {testNotification.success ? (
            <CheckCircle2 className="mt-0.5 size-4 shrink-0" />
          ) : (
            <X className="mt-0.5 size-4 shrink-0" />
          )}
          <span>{testNotification.message}</span>
          <button
            type="button"
            className="ml-auto rounded p-0.5 hover:bg-black/5"
            aria-label="关闭测试通知"
            onClick={() => setTestNotification(null)}
          >
            <X className="size-4" />
          </button>
        </div>
      )}

      <section className="grid gap-6 xl:grid-cols-[0.95fr_1.05fr]">
        <Card className="border-slate-200/80 shadow-none">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <KeyRound className="size-5 text-cyan-600" />
              LLM 配置
            </CardTitle>
            <CardDescription>
              支持分别配置 Chat、Embedding 和 Rerank 用途。知识库可在仅
              Chat、Chat + Embedding、Chat + Embedding + Rerank
              三种模式下运行，API Key 保存后不会明文回显。
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className="mb-5 grid gap-3 md:grid-cols-3">
              {llmPurposeKinds.map((item) => (
                <button
                  key={item.purpose}
                  type="button"
                  onClick={() => selectLLMPurpose(item.purpose)}
                  className={cn(
                    "rounded-xl border p-4 text-left transition-colors",
                    llmForm.purpose === item.purpose
                      ? "border-cyan-300 bg-cyan-50 text-cyan-900"
                      : "border-slate-200 bg-white text-slate-600 hover:bg-slate-50",
                  )}
                >
                  <KeyRound className="mb-3 size-5" aria-hidden="true" />
                  <p className="font-semibold">{item.title}</p>
                  <p className="mt-1 text-xs leading-5">{item.description}</p>
                </button>
              ))}
            </div>
            <form className="space-y-4" onSubmit={submitLLM}>
              {editingLLMId && (
                <div className="rounded-lg border border-cyan-200 bg-cyan-50 px-3 py-2 text-sm text-cyan-800">
                  正在编辑 #{editingLLMId}。Bearer Token、App Key / Secret
                  留空表示不修改。
                </div>
              )}
              <div className="grid gap-3 md:grid-cols-2">
                <Field label="名称">
                  <Input
                    value={llmForm.name}
                    onChange={(event) =>
                      setLLMForm((current) => ({
                        ...current,
                        name: event.target.value,
                      }))
                    }
                    required
                  />
                </Field>
                <Field label="Provider">
                  <select
                    className={selectClassName}
                    value={llmForm.provider}
                    onChange={(event) =>
                      setLLMForm((current) => ({
                        ...current,
                        provider: event.target.value,
                      }))
                    }
                  >
                    <option value="openai-compatible">openai-compatible</option>
                    <option value="deepseek">deepseek</option>
                    <option value="qwen">qwen</option>
                  </select>
                </Field>
                <Field label="Base URL">
                  <Input
                    value={llmForm.baseUrl}
                    onChange={(event) =>
                      setLLMForm((current) => ({
                        ...current,
                        baseUrl: event.target.value,
                      }))
                    }
                    required
                  />
                </Field>
                <Field label="模型">
                  <Input
                    value={llmForm.model}
                    onChange={(event) =>
                      setLLMForm((current) => ({
                        ...current,
                        model: event.target.value,
                      }))
                    }
                    required
                  />
                </Field>
                <Field label="用途">
                  <select
                    className={selectClassName}
                    value={llmForm.purpose}
                    onChange={(event) =>
                      selectLLMPurpose(event.target.value as LLMPurpose)
                    }
                  >
                    <option value="chat">LLM / Chat</option>
                    <option value="embedding">Embedding</option>
                    <option value="rerank">Rerank</option>
                  </select>
                </Field>
                <Field label="API Key / Bearer Token">
                  <Input
                    type="password"
                    value={llmForm.apiKey}
                    onChange={(event) =>
                      setLLMForm((current) => ({
                        ...current,
                        apiKey: event.target.value,
                      }))
                    }
                    placeholder="保存后不回显"
                  />
                </Field>
                <Field label="App Key（Qwen 网关可选）">
                  <Input
                    type="password"
                    value={llmForm.appKey}
                    onChange={(event) =>
                      setLLMForm((current) => ({
                        ...current,
                        appKey: event.target.value,
                      }))
                    }
                    placeholder="请求体 app_key，保存后不回显"
                  />
                </Field>
                <Field label="API Secret / App Secret（可选）">
                  <Input
                    type="password"
                    value={llmForm.apiSecret}
                    onChange={(event) =>
                      setLLMForm((current) => ({
                        ...current,
                        apiSecret: event.target.value,
                      }))
                    }
                    placeholder="Qwen 请求体 app_secret，保存后不回显"
                  />
                </Field>
                <Field label="Temperature">
                  <Input
                    type="number"
                    min="0"
                    max="2"
                    step="0.1"
                    value={llmForm.temperature}
                    onChange={(event) =>
                      setLLMForm((current) => ({
                        ...current,
                        temperature: event.target.value,
                      }))
                    }
                  />
                </Field>
              </div>
              <div className="flex flex-wrap gap-4 text-sm text-slate-600">
                <label className="inline-flex items-center gap-2">
                  <input
                    type="checkbox"
                    checked={llmForm.enabled}
                    onChange={(event) =>
                      setLLMForm((current) => ({
                        ...current,
                        enabled: event.target.checked,
                      }))
                    }
                  />
                  启用
                </label>
                <label className="inline-flex items-center gap-2">
                  <input
                    type="checkbox"
                    checked={llmForm.isDefault}
                    onChange={(event) =>
                      setLLMForm((current) => ({
                        ...current,
                        isDefault: event.target.checked,
                      }))
                    }
                  />
                  设为默认模型
                </label>
              </div>
              <div className="flex flex-wrap gap-2">
                <Button type="submit" disabled={llmMutation.isPending}>
                  {llmMutation.isPending ? (
                    <Loader2 className="size-4 animate-spin" />
                  ) : (
                    <Save className="size-4" />
                  )}
                  {editingLLMId ? "更新 LLM" : "保存 LLM"}
                </Button>
                {editingLLMId && (
                  <Button
                    type="button"
                    variant="outline"
                    onClick={cancelLLMEdit}
                  >
                    <X className="size-4" />
                    取消编辑
                  </Button>
                )}
              </div>
            </form>
          </CardContent>
        </Card>

        <Card className="border-slate-200/80 shadow-none">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Settings2 className="size-5 text-cyan-600" />
              数据源配置
            </CardTitle>
            <CardDescription>
              日志、K8s 和 Prometheus 数据源统一走后端 data_source 与
              credential_secret。
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className="mb-5 grid gap-3 md:grid-cols-3">
              {dataSourceKinds.map((item) => {
                const Icon = item.icon;
                return (
                  <button
                    key={item.type}
                    type="button"
                    onClick={() => selectType(item.type)}
                    className={cn(
                      "rounded-xl border p-4 text-left transition-colors",
                      selectedType === item.type
                        ? "border-cyan-300 bg-cyan-50 text-cyan-900"
                        : "border-slate-200 bg-white text-slate-600 hover:bg-slate-50",
                    )}
                  >
                    <Icon className="mb-3 size-5" aria-hidden="true" />
                    <p className="font-semibold">{item.title}</p>
                    <p className="mt-1 text-xs leading-5">{item.description}</p>
                  </button>
                );
              })}
            </div>

            <form className="space-y-4" onSubmit={submitDataSource} noValidate>
              {editingDataSourceId && (
                <div className="rounded-lg border border-cyan-200 bg-cyan-50 px-3 py-2 text-sm text-cyan-800">
                  正在编辑 #{editingDataSourceId}。Credential JSON
                  留空表示不修改已保存凭据。
                </div>
              )}
              <div className="grid gap-3 md:grid-cols-2">
                <Field label="名称">
                  <Input
                    value={sourceForm.name}
                    onChange={(event) =>
                      setSourceForm((current) => ({
                        ...current,
                        name: event.target.value,
                      }))
                    }
                    required
                  />
                </Field>
                <Field label="环境">
                  <Input
                    value={sourceForm.environment}
                    onChange={(event) =>
                      setSourceForm((current) => ({
                        ...current,
                        environment: event.target.value,
                      }))
                    }
                  />
                </Field>
                <Field label="系统">
                  <Input
                    value={sourceForm.systemName}
                    onChange={(event) =>
                      setSourceForm((current) => ({
                        ...current,
                        systemName: event.target.value,
                      }))
                    }
                  />
                </Field>
                <Field label="组件">
                  <Input
                    value={sourceForm.componentName}
                    onChange={(event) =>
                      setSourceForm((current) => ({
                        ...current,
                        componentName: event.target.value,
                      }))
                    }
                  />
                </Field>
              </div>
              <Field label="Config JSON">
                <textarea
                  className={textareaClassName}
                  value={sourceForm.configJSON}
                  onChange={(event) =>
                    setSourceForm((current) => ({
                      ...current,
                      configJSON: event.target.value,
                    }))
                  }
                  rows={7}
                  required
                />
              </Field>
              <Field label="Credential JSON">
                <textarea
                  className={textareaClassName}
                  value={sourceForm.credentialJSON}
                  onChange={(event) =>
                    setSourceForm((current) => ({
                      ...current,
                      credentialJSON: event.target.value,
                    }))
                  }
                  rows={5}
                  placeholder="保存后不回显"
                />
              </Field>
              <div className="flex flex-wrap items-center gap-4">
                <label className="inline-flex items-center gap-2 text-sm text-slate-600">
                  <input
                    type="checkbox"
                    checked={sourceForm.enabled}
                    onChange={(event) =>
                      setSourceForm((current) => ({
                        ...current,
                        enabled: event.target.checked,
                      }))
                    }
                  />
                  启用数据源
                </label>
                {supportsHTTPSConfig(selectedType) && (
                  <label className="inline-flex items-center gap-2 text-sm font-medium text-amber-700">
                    <input
                      type="checkbox"
                      checked={sourceForm.insecureSkipTLS}
                      onChange={(event) =>
                        setSourceForm((current) => ({
                          ...current,
                          insecureSkipTLS: event.target.checked,
                        }))
                      }
                    />
                    跳过 TLS 证书校验
                  </label>
                )}
                <span className="rounded-full bg-slate-100 px-3 py-1 text-xs font-medium text-slate-600">
                  强制只读
                </span>
              </div>
              <div className="flex flex-wrap gap-2">
                <Button type="submit" disabled={sourceMutation.isPending}>
                  {sourceMutation.isPending ? (
                    <Loader2 className="size-4 animate-spin" />
                  ) : (
                    <Save className="size-4" />
                  )}
                  {editingDataSourceId ? "更新数据源" : "保存数据源"}
                </Button>
                {editingDataSourceId && (
                  <Button
                    type="button"
                    variant="outline"
                    onClick={cancelDataSourceEdit}
                  >
                    <X className="size-4" />
                    取消编辑
                  </Button>
                )}
              </div>
            </form>
          </CardContent>
        </Card>
      </section>

      <section className="grid gap-6 xl:grid-cols-2">
        <ConfigList
          title="LLM 配置列表"
          loading={llmQuery.isLoading}
          empty="暂无 LLM 配置。"
        >
          <div className="space-y-4">
            {llmPurposeKinds.map((group) => (
              <div
                key={group.purpose}
                className="rounded-2xl border border-slate-200 bg-slate-50/70 p-3"
              >
                <div className="mb-3 flex flex-col justify-between gap-1 md:flex-row md:items-center">
                  <div>
                    <p className="font-semibold text-slate-900">
                      {group.title}
                    </p>
                    <p className="text-xs text-slate-500">
                      {group.description}
                    </p>
                  </div>
                  <span className="rounded-full bg-white px-3 py-1 text-xs font-medium text-slate-500 ring-1 ring-slate-200">
                    {llmConfigsByPurpose[group.purpose].length} 个配置
                  </span>
                </div>
                <div className="space-y-3">
                  {llmConfigsByPurpose[group.purpose].length === 0 ? (
                    <p className="rounded-xl border border-dashed border-slate-200 bg-white px-3 py-2 text-sm text-slate-500">
                      尚未配置 {group.title}。
                    </p>
                  ) : (
                    llmConfigsByPurpose[group.purpose].map((item) => (
                      <LLMConfigRow
                        key={item.id}
                        item={item}
                        testing={testLLMMutation.isPending}
                        onEdit={() => editLLMConfig(item)}
                        onTest={() =>
                          testLLMMutation.mutate({
                            id: item.id,
                            name: item.name,
                          })
                        }
                      />
                    ))
                  )}
                </div>
              </div>
            ))}
          </div>
        </ConfigList>

        <ConfigList
          title="数据源列表"
          loading={dataSourcesQuery.isLoading}
          empty="暂无数据源配置。"
        >
          {dataSources.map((item) => (
            <DataSourceRow
              key={item.id}
              item={item}
              testing={testSourceMutation.isPending}
              onEdit={() => editDataSource(item)}
              onTest={() =>
                testSourceMutation.mutate({ id: item.id, name: item.name })
              }
            />
          ))}
        </ConfigList>
      </section>
    </div>
  );
}

function LLMConfigRow({
  item,
  testing,
  onEdit,
  onTest,
}: {
  item: LLMConfig;
  testing: boolean;
  onEdit: () => void;
  onTest: () => void;
}) {
  return (
    <div className="rounded-xl border border-slate-200 bg-white p-4">
      <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
        <div>
          <p className="font-semibold text-slate-950">{item.name}</p>
          <p className="mt-1 text-sm text-slate-500">
            {item.provider} · {item.purpose} · {item.model} · {item.baseUrl}
          </p>
          <div className="mt-3 flex flex-wrap gap-2">
            <Badge active={item.enabled}>enabled</Badge>
            <Badge active={item.isDefault}>default</Badge>
            <Badge active={item.apiKeyConfigured}>api key configured</Badge>
            <Badge active={Boolean(item.appKeyConfigured)}>
              app key configured
            </Badge>
            <Badge active={item.apiSecretConfigured}>
              api secret configured
            </Badge>
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button type="button" variant="outline" size="sm" onClick={onEdit}>
            <Pencil className="size-4" />
            编辑
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={onTest}
            disabled={testing}
          >
            <TestTube2 className="size-4" />
            Test
          </Button>
        </div>
      </div>
    </div>
  );
}

function DataSourceRow({
  item,
  testing,
  onEdit,
  onTest,
}: {
  item: DataSource;
  testing: boolean;
  onEdit: () => void;
  onTest: () => void;
}) {
  return (
    <div className="rounded-xl border border-slate-200 bg-white p-4">
      <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
        <div>
          <p className="font-semibold text-slate-950">{item.name}</p>
          <p className="mt-1 text-sm text-slate-500">
            #{item.id} · {sourceLabel(item.sourceType)} ·{" "}
            {item.environment ?? "未设置环境"}
          </p>
          <p className="mt-2 line-clamp-2 text-xs text-slate-400">
            {JSON.stringify(item.config)}
          </p>
          <div className="mt-3 flex flex-wrap gap-2">
            <Badge active={item.enabled}>enabled</Badge>
            <Badge active={item.readOnly}>read only</Badge>
            <Badge active={item.credentialConfigured}>
              credential configured
            </Badge>
            {supportsHTTPSConfig(item.sourceType) && (
              <Badge active={readInsecureSkipTLS(item.config)}>
                skip tls verify
              </Badge>
            )}
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          {item.sourceType === "kubernetes" && item.enabled && (
            <Link
              to={`/topology/configuration?dataSourceId=${item.id}#k8s-import`}
              className={buttonVariants({ variant: "default", size: "sm" })}
            >
              <Network className="size-4" />
              导入拓扑
            </Link>
          )}
          <Button type="button" variant="outline" size="sm" onClick={onEdit}>
            <Pencil className="size-4" />
            编辑
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={onTest}
            disabled={testing}
          >
            <TestTube2 className="size-4" />
            Test
          </Button>
        </div>
      </div>
    </div>
  );
}

function ConfigList({
  title,
  loading,
  empty,
  children,
}: {
  title: string;
  loading: boolean;
  empty: string;
  children: ReactNode;
}) {
  const list = Array.isArray(children) ? children.filter(Boolean) : children;
  const isEmpty = Array.isArray(list) ? list.length === 0 : !list;
  return (
    <Card className="border-slate-200/80 shadow-none">
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>列表内容来自后端脱敏接口。</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {loading ? (
          <div className="flex items-center gap-2 text-sm text-slate-500">
            <Loader2 className="size-4 animate-spin" />
            加载中...
          </div>
        ) : isEmpty ? (
          <p className="rounded-xl border border-dashed border-slate-200 p-4 text-sm text-slate-500">
            {empty}
          </p>
        ) : (
          children
        )}
      </CardContent>
    </Card>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="space-y-2">
      <Label>{label}</Label>
      {children}
    </div>
  );
}

function StatusPill({ label }: { label: string }) {
  return (
    <span className="inline-flex items-center gap-1 rounded-full border border-cyan-200 bg-cyan-50 px-3 py-1 text-xs font-medium text-cyan-700">
      <CheckCircle2 className="size-3.5" aria-hidden="true" />
      {label}
    </span>
  );
}

function Badge({ active, children }: { active: boolean; children: ReactNode }) {
  return (
    <span
      className={cn(
        "rounded-full px-2.5 py-1 text-xs font-medium",
        active
          ? "bg-emerald-50 text-emerald-700"
          : "bg-slate-100 text-slate-500",
      )}
    >
      {children}
    </span>
  );
}

function sourceDefaults(template: (typeof dataSourceKinds)[number]) {
  return {
    name: `${template.type}-default`,
    environment: "prod",
    systemName: "",
    componentName: "",
    configJSON: JSON.stringify(template.config, null, 2),
    credentialJSON: JSON.stringify(template.credential, null, 2),
    enabled: true,
    insecureSkipTLS: readInsecureSkipTLS(template.config),
  };
}

function supportsHTTPSConfig(value: string) {
  return [
    "elasticsearch",
    "opensearch",
    "kubernetes",
    "prometheus",
    "http",
    "nacos",
    "nginx",
  ].includes(value);
}

function isJSONObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function readInsecureSkipTLS(config: unknown) {
  return isJSONObject(config) && config.insecureSkipTlsVerify === true;
}

function parseJSON(value: string, label: string) {
  try {
    return { ok: true as const, value: JSON.parse(value) as unknown };
  } catch {
    return { ok: false as const, error: `${label} 格式不正确。` };
  }
}

function parseOptionalJSON(value: string, label: string) {
  if (!value.trim()) {
    return { ok: true as const, value: undefined };
  }
  return parseJSON(value, label);
}

function toKnownDataSourceType(value: string): DataSourceType | null {
  const match = dataSourceKinds.find((item) => item.type === value);
  return match?.type ?? null;
}

function sourceLabel(value: string) {
  switch (value) {
    case "elasticsearch":
      return "日志数据源";
    case "kubernetes":
      return "K8s 数据源";
    case "prometheus":
      return "Prometheus 数据源";
    default:
      return value;
  }
}

const selectClassName =
  "flex h-11 w-full rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring";

const textareaClassName =
  "w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-xs leading-5 shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring";
