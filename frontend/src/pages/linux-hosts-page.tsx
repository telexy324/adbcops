import { useState, type ReactNode, type SelectHTMLAttributes } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  CheckCircle2,
  Download,
  FileSpreadsheet,
  KeyRound,
  Layers3,
  Loader2,
  Play,
  Plus,
  RefreshCw,
  Server,
  ShieldAlert,
  Square,
  Upload,
  X,
} from "lucide-react";

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
import {
  cancelLinuxBatchTest,
  confirmLinuxHostKey,
  confirmLinuxImport,
  createCredentialGroup,
  createLinuxHost,
  downloadLinuxBatchTest,
  getLinuxBatchTest,
  listCredentialGroups,
  listHostGroups,
  listHostProfiles,
  listLinuxHosts,
  previewLinuxImport,
  setLinuxHostEnabled,
  startLinuxBatchTest,
  toAPIErrorMessage,
  updateCredentialGroup,
  updateLinuxHost,
  type CredentialGroup,
  type ImportPreview,
  type LinuxHost,
  type SaveCredentialGroup,
  type SaveLinuxHost,
} from "@/api/linux";
import { cn } from "@/lib/utils";

type Tab = "hosts" | "credentials" | "catalog" | "import" | "batch";
const tabs: Array<{ id: Tab; label: string }> = [
  { id: "hosts", label: "主机" },
  { id: "credentials", label: "凭据组" },
  { id: "catalog", label: "分组与配置" },
  { id: "import", label: "批量导入" },
  { id: "batch", label: "批量测试" },
];

const emptyHost: SaveLinuxHost = {
  name: "",
  host: "",
  port: 22,
  environment: "",
  systemName: "",
  componentName: "",
  username: "",
  authType: "password",
  password: "",
  privateKey: "",
  privateKeyPassphrase: "",
  hostKeyPolicy: "strict",
  hostKeyAlgorithm: "",
  hostKeyFingerprint: "",
  tags: [],
  enabled: true,
};
const emptyCredential: SaveCredentialGroup = {
  name: "",
  authType: "password",
  username: "",
  password: "",
  privateKey: "",
  privateKeyPassphrase: "",
  enabled: true,
};

export function LinuxHostsPage() {
  const client = useQueryClient();
  const [tab, setTab] = useState<Tab>("hosts");
  const [hostForm, setHostForm] = useState<SaveLinuxHost | null>(null);
  const [editingHost, setEditingHost] = useState<number | null>(null);
  const [credentialForm, setCredentialForm] =
    useState<SaveCredentialGroup | null>(null);
  const [editingCredential, setEditingCredential] = useState<number | null>(
    null,
  );
  const [selected, setSelected] = useState<number[]>([]);
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const hosts = useQuery({
    queryKey: ["linux", "hosts"],
    queryFn: listLinuxHosts,
  });
  const credentials = useQuery({
    queryKey: ["linux", "credentials"],
    queryFn: listCredentialGroups,
  });
  const groups = useQuery({
    queryKey: ["linux", "groups"],
    queryFn: listHostGroups,
  });
  const profiles = useQuery({
    queryKey: ["linux", "profiles"],
    queryFn: listHostProfiles,
  });
  const refresh = () => client.invalidateQueries({ queryKey: ["linux"] });
  const saveHost = useMutation({
    mutationFn: ({ id, data }: { id: number | null; data: SaveLinuxHost }) =>
      id ? updateLinuxHost(id, data) : createLinuxHost(data),
    onSuccess: (host) => {
      setNotice(`主机 ${host.name} 已保存`);
      setError(null);
      setHostForm(null);
      setEditingHost(null);
      refresh();
    },
    onError: (e) => setError(toAPIErrorMessage(e)),
  });
  const toggleHost = useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) =>
      setLinuxHostEnabled(id, enabled),
    onSuccess: refresh,
    onError: (e) => setError(toAPIErrorMessage(e)),
  });
  const saveCredential = useMutation({
    mutationFn: ({
      id,
      data,
    }: {
      id: number | null;
      data: SaveCredentialGroup;
    }) => (id ? updateCredentialGroup(id, data) : createCredentialGroup(data)),
    onSuccess: (item) => {
      setNotice(`凭据组 ${item.name} 已保存`);
      setCredentialForm(null);
      setEditingCredential(null);
      refresh();
    },
    onError: (e) => setError(toAPIErrorMessage(e)),
  });
  const confirmKey = useMutation({
    mutationFn: ({
      id,
      algorithm,
      fingerprint,
    }: {
      id: number;
      algorithm: string;
      fingerprint: string;
    }) => confirmLinuxHostKey(id, algorithm, fingerprint),
    onSuccess: refresh,
    onError: (e) => setError(toAPIErrorMessage(e)),
  });
  function edit(host: LinuxHost) {
    setEditingHost(host.id);
    setHostForm({
      name: host.name,
      host: host.host,
      port: host.port,
      environment: host.environment ?? "",
      systemName: host.systemName ?? "",
      componentName: host.componentName ?? "",
      username: host.username ?? "",
      authType: host.authType,
      password: "",
      privateKey: "",
      privateKeyPassphrase: "",
      credentialGroupId: host.credentialGroupId,
      hostKeyPolicy:
        host.hostKeyPolicy === "trust_on_first_use"
          ? "trust_on_first_use"
          : "strict",
      hostKeyAlgorithm: host.hostKeyAlgorithm ?? "",
      hostKeyFingerprint: host.hostKeyFingerprint ?? "",
      profileId: host.profileId,
      tags: Array.isArray(host.tags) ? host.tags : [],
      enabled: host.enabled,
    });
  }
  const mismatch = (hosts.data ?? []).filter(
    (host) => host.hostKeyStatus === "mismatch",
  ).length;
  return (
    <div className="mx-auto max-w-[1500px] space-y-6">
      <section className="overflow-hidden rounded-2xl bg-[#071827] px-6 py-7 text-white shadow-xl sm:px-8">
        <div className="flex flex-wrap items-start justify-between gap-5">
          <div>
            <div className="mb-3 flex items-center gap-2 text-xs font-semibold uppercase tracking-[.2em] text-cyan-300">
              <Server className="size-4" />
              Linux Fleet
            </div>
            <h1 className="text-2xl font-semibold sm:text-3xl">
              Linux 主机配置
            </h1>
            <p className="mt-2 max-w-2xl text-sm leading-6 text-slate-300">
              集中管理只读 SSH
              连接、共享凭据、批量导入和连接验证。平台不会提供任意命令执行入口。
            </p>
          </div>
          <div className="grid grid-cols-3 gap-3 text-center">
            <Metric label="主机" value={hosts.data?.length ?? 0} />
            <Metric
              label="在线"
              value={
                (hosts.data ?? []).filter(
                  (h) => h.connectionStatus === "success",
                ).length
              }
            />
            <Metric label="密钥异常" value={mismatch} danger={mismatch > 0} />
          </div>
        </div>
      </section>
      {mismatch > 0 && (
        <div className="flex items-start gap-3 rounded-xl border border-red-300 bg-red-50 p-4 text-sm text-red-900">
          <ShieldAlert className="mt-0.5 size-5 shrink-0" />
          <div>
            <p className="font-semibold">
              检测到 {mismatch} 台主机 SSH Host Key 发生变化
            </p>
            <p className="mt-1 text-red-700">
              连接已阻断。请核对指纹后由管理员确认，不会自动接受新密钥。
            </p>
          </div>
        </div>
      )}
      {(notice || error) && (
        <div
          className={cn(
            "flex items-center justify-between rounded-xl border px-4 py-3 text-sm",
            error
              ? "border-red-200 bg-red-50 text-red-800"
              : "border-emerald-200 bg-emerald-50 text-emerald-800",
          )}
        >
          <span>{error ?? notice}</span>
          <button
            onClick={() => {
              setError(null);
              setNotice(null);
            }}
            aria-label="关闭提示"
          >
            <X className="size-4" />
          </button>
        </div>
      )}
      <div className="flex gap-1 overflow-x-auto rounded-xl border bg-white p-1.5">
        {tabs.map((item) => (
          <button
            key={item.id}
            onClick={() => setTab(item.id)}
            className={cn(
              "rounded-lg px-4 py-2 text-sm font-medium whitespace-nowrap",
              tab === item.id
                ? "bg-slate-900 text-white"
                : "text-slate-600 hover:bg-slate-100",
            )}
          >
            {item.label}
          </button>
        ))}
      </div>
      {tab === "hosts" && (
        <HostPanel
          hosts={hosts.data ?? []}
          loading={hosts.isLoading}
          selected={selected}
          onSelected={setSelected}
          onNew={() => {
            setEditingHost(null);
            setHostForm({ ...emptyHost });
          }}
          onEdit={edit}
          onToggle={(id, enabled) => toggleHost.mutate({ id, enabled })}
          onConfirm={(host) =>
            host.pendingHostKeyAlgorithm &&
            host.pendingHostKeyFingerprint &&
            confirmKey.mutate({
              id: host.id,
              algorithm: host.pendingHostKeyAlgorithm,
              fingerprint: host.pendingHostKeyFingerprint,
            })
          }
        />
      )}
      {tab === "credentials" && (
        <CredentialPanel
          items={credentials.data ?? []}
          onNew={() => {
            setEditingCredential(null);
            setCredentialForm({ ...emptyCredential });
          }}
          onEdit={(item) => {
            setEditingCredential(item.id);
            setCredentialForm({
              name: item.name,
              authType: item.authType,
              username: item.username,
              password: "",
              privateKey: "",
              privateKeyPassphrase: "",
              enabled: item.enabled,
            });
          }}
        />
      )}
      {tab === "catalog" && (
        <CatalogPanel
          groups={groups.data ?? []}
          profiles={profiles.data ?? []}
        />
      )}
      {tab === "import" && <ImportPanel onDone={refresh} onError={setError} />}
      {tab === "batch" && (
        <BatchPanel
          hosts={hosts.data ?? []}
          selected={selected}
          onSelected={setSelected}
          onError={setError}
        />
      )}
      {hostForm && (
        <HostEditor
          value={hostForm}
          editing={editingHost !== null}
          credentials={credentials.data ?? []}
          profiles={profiles.data ?? []}
          pending={saveHost.isPending}
          onChange={setHostForm}
          onClose={() => setHostForm(null)}
          onSave={() =>
            saveHost.mutate({
              id: editingHost,
              data: prepareHostPayload(hostForm, editingHost !== null),
            })
          }
        />
      )}
      {credentialForm && (
        <CredentialEditor
          value={credentialForm}
          editing={editingCredential !== null}
          pending={saveCredential.isPending}
          onChange={setCredentialForm}
          onClose={() => {
            setCredentialForm(null);
            setEditingCredential(null);
          }}
          onSave={() =>
            saveCredential.mutate({
              id: editingCredential,
              data: prepareCredentialPayload(credentialForm),
            })
          }
        />
      )}
    </div>
  );
}

function HostPanel({
  hosts,
  loading,
  selected,
  onSelected,
  onNew,
  onEdit,
  onToggle,
  onConfirm,
}: {
  hosts: LinuxHost[];
  loading: boolean;
  selected: number[];
  onSelected: (v: number[]) => void;
  onNew: () => void;
  onEdit: (h: LinuxHost) => void;
  onToggle: (id: number, enabled: boolean) => void;
  onConfirm: (h: LinuxHost) => void;
}) {
  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between">
        <div>
          <CardTitle>主机清单</CardTitle>
          <CardDescription>
            已保存连接的状态、认证方式和 Host Key 信任状态。
          </CardDescription>
        </div>
        <Button onClick={onNew}>
          <Plus className="size-4" />
          新增主机
        </Button>
      </CardHeader>
      <CardContent>
        <div className="overflow-x-auto">
          <table className="w-full min-w-[900px] text-left text-sm">
            <thead className="border-y bg-slate-50 text-xs uppercase tracking-wide text-slate-500">
              <tr>
                <th className="p-3">
                  <input
                    type="checkbox"
                    aria-label="选择全部主机"
                    checked={
                      hosts.length > 0 && selected.length === hosts.length
                    }
                    onChange={(e) =>
                      onSelected(e.target.checked ? hosts.map((h) => h.id) : [])
                    }
                  />
                </th>
                <th>主机</th>
                <th>环境 / 系统</th>
                <th>认证</th>
                <th>连接</th>
                <th>Host Key</th>
                <th>最近测试</th>
                <th className="text-right">操作</th>
              </tr>
            </thead>
            <tbody>
              {loading && (
                <tr>
                  <td colSpan={8} className="p-8 text-center text-slate-500">
                    加载中…
                  </td>
                </tr>
              )}
              {hosts.map((host) => (
                <tr key={host.id} className="border-b last:border-0">
                  <td className="p-3">
                    <input
                      type="checkbox"
                      aria-label={`选择 ${host.name}`}
                      checked={selected.includes(host.id)}
                      onChange={(e) =>
                        onSelected(
                          e.target.checked
                            ? [...new Set([...selected, host.id])]
                            : selected.filter((id) => id !== host.id),
                        )
                      }
                    />
                  </td>
                  <td className="py-3">
                    <p className="font-semibold">{host.name}</p>
                    <p className="font-mono text-xs text-slate-500">
                      {host.host}:{host.port}
                    </p>
                  </td>
                  <td>
                    <p>{host.environment || "未设置"}</p>
                    <p className="text-xs text-slate-500">
                      {host.systemName || host.componentName || "—"}
                    </p>
                  </td>
                  <td>
                    <Badge tone="slate">
                      {host.authType === "private_key" ? "私钥" : "密码"}
                    </Badge>
                    <p className="mt-1 text-xs text-slate-500">
                      {host.credentialGroupId ? "共享凭据组" : "独立凭据"}
                    </p>
                  </td>
                  <td>
                    <Status value={host.connectionStatus} />
                  </td>
                  <td>
                    <HostKeyStatus host={host} />
                  </td>
                  <td className="text-xs text-slate-500">
                    {host.lastTestAt
                      ? new Date(host.lastTestAt).toLocaleString()
                      : "尚未测试"}
                  </td>
                  <td>
                    <div className="flex justify-end gap-2">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => onEdit(host)}
                      >
                        编辑
                      </Button>
                      {(host.hostKeyStatus === "pending" ||
                        host.hostKeyStatus === "mismatch") &&
                        host.pendingHostKeyFingerprint && (
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => onConfirm(host)}
                          >
                            确认指纹
                          </Button>
                        )}
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => onToggle(host.id, !host.enabled)}
                      >
                        {host.enabled ? "禁用" : "启用"}
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {!loading && hosts.length === 0 && (
            <Empty text="还没有 Linux 主机，先添加一台只读 SSH 主机。" />
          )}
        </div>
      </CardContent>
    </Card>
  );
}

function HostEditor({
  value,
  editing,
  credentials,
  profiles,
  pending,
  onChange,
  onClose,
  onSave,
}: {
  value: SaveLinuxHost;
  editing: boolean;
  credentials: CredentialGroup[];
  profiles: Array<{ id: number; name: string }>;
  pending: boolean;
  onChange: (v: SaveLinuxHost) => void;
  onClose: () => void;
  onSave: () => void;
}) {
  const set = (key: keyof SaveLinuxHost, v: unknown) =>
    onChange({ ...value, [key]: v });
  const usingGroup = !!value.credentialGroupId;
  return (
    <Overlay
      title={editing ? "编辑 Linux 主机" : "新增 Linux 主机"}
      description={
        editing
          ? "已保存的密码和私钥不会回显。留空表示继续使用原凭据。"
          : "配置受 Host Key 保护的只读 SSH 连接。"
      }
      onClose={onClose}
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          onSave();
        }}
        className="space-y-5"
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="名称">
            <Input
              required
              value={value.name}
              onChange={(e) => set("name", e.target.value)}
            />
          </Field>
          <Field label="环境">
            <Input
              value={value.environment}
              onChange={(e) => set("environment", e.target.value)}
            />
          </Field>
          <Field label="主机地址">
            <Input
              required
              value={value.host}
              onChange={(e) => set("host", e.target.value)}
            />
          </Field>
          <Field label="SSH 端口">
            <Input
              required
              type="number"
              min={1}
              max={65535}
              value={value.port}
              onChange={(e) => set("port", Number(e.target.value))}
            />
          </Field>
          <Field label="系统">
            <Input
              value={value.systemName}
              onChange={(e) => set("systemName", e.target.value)}
            />
          </Field>
          <Field label="组件">
            <Input
              value={value.componentName}
              onChange={(e) => set("componentName", e.target.value)}
            />
          </Field>
        </div>
        <div className="rounded-xl border bg-slate-50 p-4">
          <p className="mb-3 text-sm font-semibold">认证</p>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="共享凭据组">
              <Select
                value={value.credentialGroupId ?? ""}
                onChange={(e) =>
                  set(
                    "credentialGroupId",
                    e.target.value ? Number(e.target.value) : undefined,
                  )
                }
              >
                <option value="">使用独立凭据</option>
                {credentials
                  .filter((c) => c.enabled)
                  .map((c) => (
                    <option key={c.id} value={c.id}>
                      {c.name} ·{" "}
                      {c.authType === "private_key" ? "私钥" : "密码"}
                    </option>
                  ))}
              </Select>
            </Field>
            {!usingGroup && (
              <Field label="认证方式">
                <Select
                  value={value.authType}
                  onChange={(e) => set("authType", e.target.value)}
                >
                  <option value="password">密码</option>
                  <option value="private_key">私钥</option>
                </Select>
              </Field>
            )}
            {!usingGroup && (
              <Field label="用户名">
                <Input
                  required
                  value={value.username}
                  onChange={(e) => set("username", e.target.value)}
                />
              </Field>
            )}
            {!usingGroup && value.authType === "password" && (
              <Field label={editing ? "新密码（留空不修改）" : "密码"}>
                <Input
                  required={!editing}
                  type="password"
                  autoComplete="new-password"
                  value={value.password}
                  onChange={(e) => set("password", e.target.value)}
                />
              </Field>
            )}
            {!usingGroup && value.authType === "private_key" && (
              <>
                <Field
                  label={editing ? "新私钥（留空不修改）" : "OpenSSH 私钥"}
                  wide
                >
                  <textarea
                    required={!editing}
                    value={value.privateKey}
                    onChange={(e) => set("privateKey", e.target.value)}
                    className="min-h-28 w-full rounded-md border bg-white p-3 font-mono text-xs"
                    placeholder="私钥不会在编辑页面回显"
                  />
                </Field>
                <Field label="私钥口令">
                  <Input
                    type="password"
                    autoComplete="new-password"
                    value={value.privateKeyPassphrase}
                    onChange={(e) =>
                      set("privateKeyPassphrase", e.target.value)
                    }
                  />
                </Field>
              </>
            )}
          </div>
        </div>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Host Key 策略">
            <Select
              value={value.hostKeyPolicy}
              onChange={(e) => set("hostKeyPolicy", e.target.value)}
            >
              <option value="strict">Strict</option>
              <option value="trust_on_first_use">首次连接后确认</option>
            </Select>
          </Field>
          <Field label="主机配置 Profile">
            <Select
              value={value.profileId ?? ""}
              onChange={(e) =>
                set(
                  "profileId",
                  e.target.value ? Number(e.target.value) : undefined,
                )
              }
            >
              <option value="">默认配置</option>
              {profiles.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="Host Key 算法">
            <Input
              value={value.hostKeyAlgorithm}
              onChange={(e) => set("hostKeyAlgorithm", e.target.value)}
              placeholder="ssh-ed25519"
            />
          </Field>
          <Field label="Host Key 指纹">
            <Input
              value={value.hostKeyFingerprint}
              onChange={(e) => set("hostKeyFingerprint", e.target.value)}
              placeholder="SHA256:…"
            />
          </Field>
        </div>
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={value.enabled}
            onChange={(e) => set("enabled", e.target.checked)}
          />
          保存后启用
        </label>
        <div className="flex justify-end gap-3">
          <Button type="button" variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button disabled={pending}>
            {pending && <Loader2 className="size-4 animate-spin" />}保存主机
          </Button>
        </div>
      </form>
    </Overlay>
  );
}

function CredentialPanel({
  items,
  onNew,
  onEdit,
}: {
  items: CredentialGroup[];
  onNew: () => void;
  onEdit: (item: CredentialGroup) => void;
}) {
  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between">
        <div>
          <CardTitle>共享凭据组</CardTitle>
          <CardDescription>
            凭据按版本加密保存，查询接口永远不返回明文。
          </CardDescription>
        </div>
        <Button onClick={onNew}>
          <Plus className="size-4" />
          新增凭据组
        </Button>
      </CardHeader>
      <CardContent>
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {items.map((item) => (
            <div key={item.id} className="rounded-xl border p-4">
              <div className="flex items-start justify-between">
                <div className="rounded-lg bg-cyan-50 p-2 text-cyan-700">
                  <KeyRound className="size-5" />
                </div>
                <Badge tone={item.enabled ? "green" : "slate"}>
                  {item.enabled ? "启用" : "停用"}
                </Badge>
              </div>
              <h3 className="mt-4 font-semibold">{item.name}</h3>
              <p className="mt-1 text-sm text-slate-500">
                {item.username} ·{" "}
                {item.authType === "private_key" ? "私钥" : "密码"}
              </p>
              <p className="mt-3 text-xs text-slate-400">
                凭据版本 v{item.version} ·{" "}
                {item.credentialConfigured ? "已配置" : "未配置"}
              </p>
              <Button
                className="mt-4"
                size="sm"
                variant="outline"
                onClick={() => onEdit(item)}
              >
                编辑
              </Button>
            </div>
          ))}
          {items.length === 0 && <Empty text="尚未创建共享凭据组。" />}
        </div>
      </CardContent>
    </Card>
  );
}

function CredentialEditor({
  value,
  editing,
  pending,
  onChange,
  onClose,
  onSave,
}: {
  value: SaveCredentialGroup;
  editing: boolean;
  pending: boolean;
  onChange: (v: SaveCredentialGroup) => void;
  onClose: () => void;
  onSave: () => void;
}) {
  const set = (key: keyof SaveCredentialGroup, v: unknown) =>
    onChange({ ...value, [key]: v });
  return (
    <Overlay
      title={editing ? "编辑共享凭据组" : "新增共享凭据组"}
      description={
        editing
          ? "已保存的密码和私钥不会回显，留空表示继续使用原凭据。"
          : "凭据组不保存具体主机地址，可供同一授权范围内的主机复用。"
      }
      onClose={onClose}
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          onSave();
        }}
        className="space-y-4"
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="名称">
            <Input
              required
              value={value.name}
              onChange={(e) => set("name", e.target.value)}
            />
          </Field>
          <Field label="认证方式">
            <Select
              value={value.authType}
              onChange={(e) => set("authType", e.target.value)}
            >
              <option value="password">密码</option>
              <option value="private_key">私钥</option>
            </Select>
          </Field>
          <Field label="用户名">
            <Input
              required
              value={value.username}
              onChange={(e) => set("username", e.target.value)}
            />
          </Field>
          {value.authType === "password" ? (
            <Field label={editing ? "新密码（留空不修改）" : "密码"}>
              <Input
                required={!editing}
                type="password"
                autoComplete="new-password"
                value={value.password}
                onChange={(e) => set("password", e.target.value)}
              />
            </Field>
          ) : (
            <>
              <Field
                label={
                  editing ? "新 OpenSSH 私钥（留空不修改）" : "OpenSSH 私钥"
                }
                wide
              >
                <textarea
                  required={!editing}
                  value={value.privateKey}
                  onChange={(e) => set("privateKey", e.target.value)}
                  className="min-h-32 w-full rounded-md border p-3 font-mono text-xs"
                />
              </Field>
              <Field label="私钥口令">
                <Input
                  type="password"
                  value={value.privateKeyPassphrase}
                  onChange={(e) => set("privateKeyPassphrase", e.target.value)}
                />
              </Field>
            </>
          )}
        </div>
        <div className="flex justify-end gap-3">
          <Button type="button" variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button disabled={pending}>保存凭据组</Button>
        </div>
      </form>
    </Overlay>
  );
}

function CatalogPanel({
  groups,
  profiles,
}: {
  groups: Array<{
    id: number;
    name: string;
    description?: string;
    environment?: string;
    members?: unknown[];
  }>;
  profiles: Array<{
    id: number;
    name: string;
    description?: string;
    collectors: string[];
    builtIn: boolean;
  }>;
}) {
  return (
    <div className="grid gap-6 lg:grid-cols-2">
      <Card>
        <CardHeader>
          <CardTitle>Host Groups</CardTitle>
          <CardDescription>
            用于批量授权、测试和分析的逻辑主机分组。
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          {groups.map((group) => (
            <div
              key={group.id}
              className="flex items-center gap-3 rounded-xl border p-4"
            >
              <Layers3 className="size-5 text-cyan-600" />
              <div>
                <p className="font-semibold">{group.name}</p>
                <p className="text-xs text-slate-500">
                  {group.environment || "全部环境"} ·{" "}
                  {group.members?.length ?? 0} 台主机
                </p>
              </div>
            </div>
          ))}
          {groups.length === 0 && <Empty text="暂无主机分组。" />}
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>Host Profiles</CardTitle>
          <CardDescription>
            定义不同角色主机默认启用的安全采集器。
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          {profiles.map((profile) => (
            <div key={profile.id} className="rounded-xl border p-4">
              <div className="flex items-center justify-between">
                <p className="font-semibold">{profile.name}</p>
                {profile.builtIn && <Badge tone="cyan">内置</Badge>}
              </div>
              <p className="mt-1 text-xs text-slate-500">
                {profile.description || "Linux 基础采集配置"}
              </p>
              <div className="mt-3 flex flex-wrap gap-1.5">
                {profile.collectors.map((item) => (
                  <Badge key={item} tone="slate">
                    {item}
                  </Badge>
                ))}
              </div>
            </div>
          ))}
        </CardContent>
      </Card>
    </div>
  );
}

function ImportPanel({
  onDone,
  onError,
}: {
  onDone: () => void;
  onError: (v: string) => void;
}) {
  const [file, setFile] = useState<File | null>(null);
  const [strategy, setStrategy] = useState("skip");
  const [mapping, setMapping] = useState("");
  const [preview, setPreview] = useState<ImportPreview | null>(null);
  const previewMutation = useMutation({
    mutationFn: () =>
      previewLinuxImport(
        file!,
        strategy,
        mapping.trim() ? JSON.parse(mapping) : {},
      ),
    onSuccess: setPreview,
    onError: (e) => onError(toAPIErrorMessage(e)),
  });
  const confirmMutation = useMutation({
    mutationFn: () => confirmLinuxImport(preview!.token),
    onSuccess: () => {
      setPreview(null);
      setFile(null);
      onDone();
    },
    onError: (e) => onError(toAPIErrorMessage(e)),
  });
  const issues = preview?.rows.flatMap((row) => row.issues ?? []) ?? [];
  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>批量导入向导</CardTitle>
          <CardDescription>
            支持 CSV、TSV、XLSX。必须 Preview 并确认后才写入数据库，最多 5000
            行。
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 lg:grid-cols-[1fr_240px_1fr_auto]">
          <Field label="文件">
            <Input
              type="file"
              aria-label="文件"
              accept=".csv,.tsv,.xlsx"
              onChange={(e) => {
                setFile(e.target.files?.[0] ?? null);
                setPreview(null);
              }}
            />
          </Field>
          <Field label="重复策略">
            <Select
              value={strategy}
              onChange={(e) => setStrategy(e.target.value)}
            >
              <option value="skip">跳过（默认，不覆盖凭据）</option>
              <option value="update_metadata">更新元数据</option>
              <option value="replace_connection_config">替换连接配置</option>
              <option value="create_as_disabled">创建为禁用</option>
            </Select>
          </Field>
          <Field label="列映射 JSON（可选）">
            <Input
              value={mapping}
              onChange={(e) => setMapping(e.target.value)}
              placeholder='{"host":"IP Address"}'
            />
          </Field>
          <div className="flex items-end">
            <Button
              disabled={!file || previewMutation.isPending}
              onClick={() => previewMutation.mutate()}
            >
              {previewMutation.isPending ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Upload className="size-4" />
              )}
              生成 Preview
            </Button>
          </div>
        </CardContent>
      </Card>
      {preview && (
        <Card>
          <CardHeader className="flex-row items-center justify-between">
            <div>
              <CardTitle>Preview</CardTitle>
              <CardDescription>
                {preview.total} 行 · {preview.valid} 有效 · {preview.invalid}{" "}
                错误 · {preview.duplicates} 重复
              </CardDescription>
            </div>
            <div className="flex gap-2">
              {issues.length > 0 && (
                <Button
                  variant="outline"
                  onClick={() => downloadIssues(issues)}
                >
                  <Download className="size-4" />
                  下载错误行
                </Button>
              )}
              <Button
                disabled={preview.invalid > 0 || confirmMutation.isPending}
                onClick={() => confirmMutation.mutate()}
              >
                <CheckCircle2 className="size-4" />
                确认导入
              </Button>
            </div>
          </CardHeader>
          <CardContent>
            <div className="overflow-x-auto">
              <table className="w-full min-w-[700px] text-sm">
                <thead className="border-y bg-slate-50 text-left text-xs text-slate-500">
                  <tr>
                    <th className="p-3">行</th>
                    <th>名称</th>
                    <th>地址</th>
                    <th>认证</th>
                    <th>动作</th>
                    <th>校验</th>
                  </tr>
                </thead>
                <tbody>
                  {preview.rows.slice(0, 100).map((row) => (
                    <tr key={row.row} className="border-b">
                      <td className="p-3">{row.row}</td>
                      <td>{row.name || "—"}</td>
                      <td className="font-mono text-xs">
                        {row.host}:{row.port}
                      </td>
                      <td>
                        {row.authType} ·{" "}
                        {row.credentialConfigured ? "已配置" : "未配置"}
                      </td>
                      <td>{row.action}</td>
                      <td>
                        {row.issues?.length ? (
                          <span className="text-red-700">
                            {row.issues
                              .map((i) => redactImportIssueMessage(i.message))
                              .join("；")}
                          </span>
                        ) : (
                          <span className="text-emerald-700">通过</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}

function BatchPanel({
  hosts,
  selected,
  onSelected,
  onError,
}: {
  hosts: LinuxHost[];
  selected: number[];
  onSelected: (v: number[]) => void;
  onError: (v: string) => void;
}) {
  const [jobId, setJobId] = useState<string | null>(null);
  const start = useMutation({
    mutationFn: () => startLinuxBatchTest(selected),
    onSuccess: (job) => setJobId(job.id),
    onError: (e) => onError(toAPIErrorMessage(e)),
  });
  const job = useQuery({
    queryKey: ["linux", "batch", jobId],
    queryFn: () => getLinuxBatchTest(jobId!),
    enabled: !!jobId,
    refetchInterval: (q) => (q.state.data?.status === "running" ? 1000 : false),
  });
  const cancel = useMutation({
    mutationFn: () => cancelLinuxBatchTest(jobId!),
    onSuccess: () => job.refetch(),
    onError: (e) => onError(toAPIErrorMessage(e)),
  });
  async function download(format: "json" | "csv") {
    try {
      saveBlob(
        await downloadLinuxBatchTest(jobId!, format),
        `linux-batch-${jobId}.${format}`,
      );
    } catch (e) {
      onError(toAPIErrorMessage(e));
    }
  }
  return (
    <div className="grid gap-6 lg:grid-cols-[1fr_1.2fr]">
      <Card>
        <CardHeader>
          <CardTitle>选择测试主机</CardTitle>
          <CardDescription>
            已选择 {selected.length} / {hosts.length}，单次最多 500 台，默认并发
            10。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="max-h-96 space-y-2 overflow-y-auto">
            {hosts.map((host) => (
              <label
                key={host.id}
                className="flex items-center gap-3 rounded-lg border p-3 text-sm"
              >
                <input
                  type="checkbox"
                  checked={selected.includes(host.id)}
                  onChange={(e) =>
                    onSelected(
                      e.target.checked
                        ? [...new Set([...selected, host.id])]
                        : selected.filter((id) => id !== host.id),
                    )
                  }
                />
                <span className="font-medium">{host.name}</span>
                <span className="ml-auto font-mono text-xs text-slate-500">
                  {host.host}:{host.port}
                </span>
              </label>
            ))}
          </div>
          <Button
            className="mt-4 w-full"
            disabled={
              selected.length === 0 || selected.length > 500 || start.isPending
            }
            onClick={() => start.mutate()}
          >
            <Play className="size-4" />
            开始连接测试
          </Button>
        </CardContent>
      </Card>
      <Card>
        <CardHeader className="flex-row items-center justify-between">
          <div>
            <CardTitle>测试进度</CardTitle>
            <CardDescription>
              {job.data
                ? `${job.data.completed} / ${job.data.total} 已完成`
                : "尚未启动批量测试"}
            </CardDescription>
          </div>
          {job.data?.status === "running" && (
            <Button variant="outline" onClick={() => cancel.mutate()}>
              <Square className="size-4" />
              取消
            </Button>
          )}
        </CardHeader>
        <CardContent>
          {job.data ? (
            <>
              <div className="h-2 overflow-hidden rounded-full bg-slate-100">
                <div
                  className="h-full bg-cyan-500 transition-all"
                  style={{ width: `${job.data.progress}%` }}
                />
              </div>
              <div className="mt-4 grid grid-cols-4 gap-2 text-center">
                <Metric label="总数" value={job.data.total} />
                <Metric label="成功" value={job.data.success} />
                <Metric
                  label="失败"
                  value={job.data.failed}
                  danger={job.data.failed > 0}
                />
                <Metric label="取消" value={job.data.cancelled} />
              </div>
              <div className="mt-5 max-h-72 overflow-y-auto rounded-lg border">
                {job.data.items.map((item) => (
                  <div
                    key={item.hostId}
                    className="flex items-center gap-3 border-b px-3 py-2 text-sm last:border-0"
                  >
                    <Status value={item.status} />
                    <span>Host #{item.hostId}</span>
                    <span className="ml-auto text-xs text-slate-500">
                      {item.errorCode || `${item.latencyMs ?? 0}ms`}
                    </span>
                  </div>
                ))}
              </div>
              {job.data.status !== "running" && (
                <div className="mt-4 flex gap-2">
                  <Button variant="outline" onClick={() => download("json")}>
                    <Download className="size-4" />
                    JSON
                  </Button>
                  <Button variant="outline" onClick={() => download("csv")}>
                    <FileSpreadsheet className="size-4" />
                    CSV
                  </Button>
                </div>
              )}
            </>
          ) : (
            <Empty text="选择主机并开始测试后，这里会显示实时进度。" />
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function HostKeyStatus({ host }: { host: LinuxHost }) {
  if (host.hostKeyStatus === "mismatch")
    return (
      <span className="inline-flex items-center gap-1 font-semibold text-red-700">
        <ShieldAlert className="size-4" />
        指纹不匹配
      </span>
    );
  if (host.hostKeyStatus === "trusted")
    return <Badge tone="green">已信任</Badge>;
  if (host.hostKeyStatus === "pending")
    return <Badge tone="amber">待确认</Badge>;
  return <Badge tone="slate">未验证</Badge>;
}
function Status({ value }: { value: string }) {
  const success = ["success", "completed", "healthy"].includes(value);
  const bad = ["failed", "host_key_mismatch", "mismatch", "critical"].includes(
    value,
  );
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 text-xs font-semibold",
        success ? "text-emerald-700" : bad ? "text-red-700" : "text-slate-500",
      )}
    >
      {success ? (
        <CheckCircle2 className="size-3.5" />
      ) : bad ? (
        <AlertTriangle className="size-3.5" />
      ) : (
        <RefreshCw className="size-3.5" />
      )}
      {labelStatus(value)}
    </span>
  );
}
function Badge({
  children,
  tone,
}: {
  children: ReactNode;
  tone: "slate" | "green" | "cyan" | "amber";
}) {
  const tones = {
    slate: "bg-slate-100 text-slate-600",
    green: "bg-emerald-50 text-emerald-700",
    cyan: "bg-cyan-50 text-cyan-700",
    amber: "bg-amber-50 text-amber-700",
  };
  return (
    <span
      className={cn(
        "inline-flex rounded-full px-2 py-1 text-[11px] font-semibold",
        tones[tone],
      )}
    >
      {children}
    </span>
  );
}
function Metric({
  label,
  value,
  danger,
}: {
  label: string;
  value: number;
  danger?: boolean;
}) {
  return (
    <div
      className={cn(
        "min-w-20 rounded-xl border border-white/10 bg-white/5 px-3 py-2",
        danger && "bg-red-500/15",
      )}
    >
      <p className="text-lg font-semibold">{value}</p>
      <p className="text-[10px] uppercase tracking-wide text-slate-400">
        {label}
      </p>
    </div>
  );
}
function Field({
  label,
  children,
  wide,
}: {
  label: string;
  children: ReactNode;
  wide?: boolean;
}) {
  return (
    <div className={wide ? "sm:col-span-2" : ""}>
      <Label className="mb-1.5 block">{label}</Label>
      {children}
    </div>
  );
}
function Select(props: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      {...props}
      className={cn(
        "h-10 w-full rounded-md border bg-white px-3 text-sm",
        props.className,
      )}
    />
  );
}
function Overlay({
  title,
  description,
  onClose,
  children,
}: {
  title: string;
  description: string;
  onClose: () => void;
  children: ReactNode;
}) {
  return (
    <div className="fixed inset-0 z-[70] grid place-items-center overflow-y-auto bg-slate-950/60 p-4 backdrop-blur-sm">
      <div className="my-8 w-full max-w-3xl rounded-2xl bg-white shadow-2xl">
        <div className="flex items-start justify-between border-b p-6">
          <div>
            <h2 className="text-xl font-semibold">{title}</h2>
            <p className="mt-1 text-sm text-slate-500">{description}</p>
          </div>
          <Button variant="ghost" size="icon" onClick={onClose}>
            <X className="size-5" />
          </Button>
        </div>
        <div className="p-6">{children}</div>
      </div>
    </div>
  );
}

function prepareHostPayload(
  value: SaveLinuxHost,
  editing: boolean,
): SaveLinuxHost {
  const payload = { ...value };
  for (const key of [
    "password",
    "privateKey",
    "privateKeyPassphrase",
  ] as const) {
    if (!payload[key]?.trim()) delete payload[key];
  }
  if (editing) {
    delete payload.hostKeyAlgorithm;
    delete payload.hostKeyFingerprint;
  }
  if (payload.credentialGroupId) {
    delete payload.username;
    delete payload.password;
    delete payload.privateKey;
    delete payload.privateKeyPassphrase;
  }
  return payload;
}
function prepareCredentialPayload(
  value: SaveCredentialGroup,
): SaveCredentialGroup {
  const payload = { ...value };
  for (const key of [
    "password",
    "privateKey",
    "privateKeyPassphrase",
  ] as const) {
    if (!payload[key]?.trim()) delete payload[key];
  }
  return payload;
}
function Empty({ text }: { text: string }) {
  return (
    <div className="col-span-full py-10 text-center text-sm text-slate-500">
      {text}
    </div>
  );
}
function labelStatus(value: string) {
  return (
    (
      {
        success: "成功",
        failed: "失败",
        unknown: "未知",
        pending: "等待",
        running: "进行中",
        cancelled: "已取消",
        host_key_mismatch: "密钥异常",
      } as Record<string, string>
    )[value] ?? value
  );
}
function downloadIssues(
  issues: Array<{ row: number; field?: string; code: string; message: string }>,
) {
  const rows = [
    "row,field,code,message",
    ...issues.map((i) =>
      [i.row, i.field ?? "", i.code, redactImportIssueMessage(i.message)]
        .map(csvCell)
        .join(","),
    ),
  ];
  saveBlob(
    new Blob([rows.join("\n")], { type: "text/csv;charset=utf-8" }),
    "linux-import-errors.csv",
  );
}
export function redactImportIssueMessage(message: string) {
  return message
    .replace(/-----BEGIN [^-]+-----[\s\S]*?-----END [^-]+-----/gi, "[REDACTED]")
    .replace(
      /((?:password|passphrase|private[_-]?key|token|secret)\s*[:=]\s*)([^\s,;]+)/gi,
      "$1[REDACTED]",
    );
}
function csvCell(value: unknown) {
  let text = String(value);
  if (/^[=+\-@]/.test(text)) text = `'${text}`;
  return `"${text.replaceAll('"', '""')}"`;
}
function saveBlob(blob: Blob, name: string) {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = name;
  anchor.click();
  URL.revokeObjectURL(url);
}
