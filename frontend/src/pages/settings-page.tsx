import { FormEvent, ReactNode, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  CheckCircle2,
  DatabaseZap,
  KeyRound,
  Loader2,
  Network,
  Save,
  ServerCog,
  Settings2,
  TestTube2,
} from "lucide-react";

import {
  createDataSource,
  createLLMConfig,
  listDataSources,
  listLLMConfigs,
  testDataSource,
  testLLMConfig,
  toAPIErrorMessage,
  type DataSource,
  type DataSourceType,
  type LLMConfig,
} from "@/api/config";
import { Button } from "@/components/ui/button";
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
    config: { baseUrl: "https://es.example.com", index: "logs-*" },
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
    },
    credential: { bearerToken: "replace-me" },
  },
  {
    type: "prometheus",
    title: "Prometheus 数据源",
    description: "Prometheus HTTP API，用于 instant/range 指标查询。",
    icon: ServerCog,
    config: { baseUrl: "https://prometheus.example.com" },
    credential: { bearerToken: "replace-me" },
  },
];

const defaultLLMForm = {
  name: "default-openai-compatible",
  provider: "openai-compatible",
  baseUrl: "https://api.openai.example/v1",
  model: "gpt-compatible-model",
  apiKey: "",
  temperature: "0.2",
  enabled: true,
  isDefault: true,
};

export function SettingsPage() {
  const queryClient = useQueryClient();
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [llmForm, setLLMForm] = useState(defaultLLMForm);
  const [selectedType, setSelectedType] =
    useState<DataSourceType>("elasticsearch");
  const selectedTemplate = useMemo(
    () => dataSourceKinds.find((item) => item.type === selectedType)!,
    [selectedType],
  );
  const [sourceForm, setSourceForm] = useState(() =>
    sourceDefaults(selectedTemplate),
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
    mutationFn: createLLMConfig,
    onSuccess: (config) => {
      setNotice(`LLM 配置 ${config.name} 已保存。`);
      setError(null);
      setLLMForm((current) => ({ ...current, apiKey: "" }));
      queryClient.invalidateQueries({ queryKey: ["settings", "llm-configs"] });
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const sourceMutation = useMutation({
    mutationFn: createDataSource,
    onSuccess: (source) => {
      setNotice(`${sourceLabel(source.sourceType)} ${source.name} 已保存。`);
      setError(null);
      setSourceForm(sourceDefaults(selectedTemplate));
      queryClient.invalidateQueries({ queryKey: ["settings", "data-sources"] });
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const testLLMMutation = useMutation({
    mutationFn: (id: number) => testLLMConfig(id),
    onSuccess: () => {
      setNotice("LLM Test API 调用完成。");
      setError(null);
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  const testSourceMutation = useMutation({
    mutationFn: testDataSource,
    onSuccess: (result) => {
      setNotice(result.ok ? result.message : "数据源测试未通过。");
      setError(null);
    },
    onError: (err) => setError(toAPIErrorMessage(err)),
  });

  function selectType(type: DataSourceType) {
    const template = dataSourceKinds.find((item) => item.type === type)!;
    setSelectedType(type);
    setSourceForm(sourceDefaults(template));
  }

  function submitLLM(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    llmMutation.mutate({
      name: llmForm.name,
      provider: llmForm.provider,
      baseUrl: llmForm.baseUrl,
      model: llmForm.model,
      apiKey: llmForm.apiKey,
      temperature: Number(llmForm.temperature),
      enabled: llmForm.enabled,
      isDefault: llmForm.isDefault,
    });
  }

  function submitDataSource(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const config = parseJSON(sourceForm.configJSON, "Config JSON");
    const credential = parseOptionalJSON(
      sourceForm.credentialJSON,
      "Credential JSON",
    );
    if (!config.ok || !credential.ok) {
      setError(config.error ?? credential.error ?? "JSON 格式不正确。");
      return;
    }
    sourceMutation.mutate({
      name: sourceForm.name,
      sourceType: selectedType,
      environment: sourceForm.environment,
      systemName: sourceForm.systemName,
      componentName: sourceForm.componentName,
      config: config.value,
      credential: credential.value,
      enabled: sourceForm.enabled,
      readOnly: true,
    });
  }

  const llmConfigs = llmQuery.data ?? [];
  const dataSources = dataSourcesQuery.data ?? [];

  return (
    <div className="mx-auto max-w-[1700px] space-y-6">
      <section className="flex flex-col justify-between gap-4 xl:flex-row xl:items-end">
        <div>
          <p className="text-sm font-medium text-cyan-700">Settings</p>
          <h1 className="mt-1 text-3xl font-semibold tracking-tight text-slate-950">
            配置中心
          </h1>
          <p className="mt-2 max-w-3xl text-sm leading-6 text-slate-500">
            管理 LLM、日志、Kubernetes 和 Prometheus
            数据源。凭据仅提交到后端加密存储，页面只展示“已配置”状态。
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

      <section className="grid gap-6 xl:grid-cols-[0.95fr_1.05fr]">
        <Card className="border-slate-200/80 shadow-none">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <KeyRound className="size-5 text-cyan-600" />
              LLM 配置
            </CardTitle>
            <CardDescription>
              支持 OpenAI-compatible、DeepSeek、Qwen 等 provider。API Key
              保存后不会明文回显。
            </CardDescription>
          </CardHeader>
          <CardContent>
            <form className="space-y-4" onSubmit={submitLLM}>
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
                <Field label="API Key">
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
              <Button type="submit" disabled={llmMutation.isPending}>
                {llmMutation.isPending ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <Save className="size-4" />
                )}
                保存 LLM
              </Button>
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

            <form className="space-y-4" onSubmit={submitDataSource}>
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
                <span className="rounded-full bg-slate-100 px-3 py-1 text-xs font-medium text-slate-600">
                  强制只读
                </span>
              </div>
              <Button type="submit" disabled={sourceMutation.isPending}>
                {sourceMutation.isPending ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <Save className="size-4" />
                )}
                保存数据源
              </Button>
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
          {llmConfigs.map((item) => (
            <LLMConfigRow
              key={item.id}
              item={item}
              testing={testLLMMutation.isPending}
              onTest={() => testLLMMutation.mutate(item.id)}
            />
          ))}
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
              onTest={() => testSourceMutation.mutate(item.id)}
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
  onTest,
}: {
  item: LLMConfig;
  testing: boolean;
  onTest: () => void;
}) {
  return (
    <div className="rounded-xl border border-slate-200 bg-white p-4">
      <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
        <div>
          <p className="font-semibold text-slate-950">{item.name}</p>
          <p className="mt-1 text-sm text-slate-500">
            {item.provider} · {item.model} · {item.baseUrl}
          </p>
          <div className="mt-3 flex flex-wrap gap-2">
            <Badge active={item.enabled}>enabled</Badge>
            <Badge active={item.isDefault}>default</Badge>
            <Badge active={item.apiKeyConfigured}>api key configured</Badge>
          </div>
        </div>
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
  );
}

function DataSourceRow({
  item,
  testing,
  onTest,
}: {
  item: DataSource;
  testing: boolean;
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
          </div>
        </div>
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
  };
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
