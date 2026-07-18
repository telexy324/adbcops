import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import {
  listLinuxHosts,
  previewLinuxImport,
  updateLinuxHost,
} from "@/api/linux";
import {
  LinuxHostsPage,
  redactImportIssueMessage,
} from "@/pages/linux-hosts-page";

vi.mock("@/api/linux", () => ({
  cancelLinuxBatchTest: vi.fn(),
  confirmLinuxHostKey: vi.fn(),
  confirmLinuxImport: vi.fn(),
  createCredentialGroup: vi.fn(),
  createLinuxHost: vi.fn(),
  downloadLinuxBatchTest: vi.fn(),
  getLinuxBatchTest: vi.fn(),
  listCredentialGroups: vi.fn().mockResolvedValue([]),
  listHostGroups: vi.fn().mockResolvedValue([]),
  listHostProfiles: vi.fn().mockResolvedValue([]),
  listLinuxHosts: vi.fn(),
  previewLinuxImport: vi.fn(),
  setLinuxHostEnabled: vi.fn(),
  startLinuxBatchTest: vi.fn(),
  toAPIErrorMessage: vi.fn(() => "请求失败"),
  updateLinuxHost: vi.fn(),
  updateCredentialGroup: vi.fn(),
}));

const privateKeyHost = {
  id: 7,
  name: "prod-web-01",
  host: "10.0.0.7",
  port: 22,
  username: "ops",
  authType: "private_key" as const,
  credentialConfigured: true,
  hostKeyPolicy: "strict" as const,
  hostKeyAlgorithm: "ssh-ed25519",
  hostKeyFingerprint: "SHA256:trusted",
  hostKeyStatus: "mismatch" as const,
  pendingHostKeyAlgorithm: "ssh-ed25519",
  pendingHostKeyFingerprint: "SHA256:changed",
  tags: [],
  enabled: true,
  connectionStatus: "host_key_mismatch",
};

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <LinuxHostsPage />
    </QueryClientProvider>,
  );
}

describe("LinuxHostsPage", () => {
  beforeEach(() => {
    vi.mocked(listLinuxHosts).mockResolvedValue([privateKeyHost]);
  });

  it("prominently reports host key mismatch and exposes no command input", async () => {
    renderPage();

    expect(
      await screen.findByText(/SSH Host Key 发生变化/),
    ).toBeInTheDocument();
    expect(screen.getByText("指纹不匹配")).toBeInTheDocument();
    expect(
      screen.queryByRole("textbox", { name: /command|argv|命令/i }),
    ).toBeNull();
  });

  it("does not echo private keys and omits immutable host key fields on edit", async () => {
    const user = userEvent.setup();
    vi.mocked(updateLinuxHost).mockResolvedValue(privateKeyHost);
    renderPage();

    const row = (await screen.findByText("prod-web-01")).closest("tr");
    expect(row).not.toBeNull();
    await user.click(
      within(row as HTMLElement).getByRole("button", { name: "编辑" }),
    );

    expect(screen.getByText(/密码和私钥不会回显/)).toBeInTheDocument();
    expect(screen.getByPlaceholderText("私钥不会在编辑页面回显")).toHaveValue(
      "",
    );
    await user.click(screen.getByRole("button", { name: "保存主机" }));

    expect(updateLinuxHost).toHaveBeenCalledWith(
      7,
      expect.not.objectContaining({
        hostKeyAlgorithm: expect.anything(),
        hostKeyFingerprint: expect.anything(),
        privateKey: expect.anything(),
      }),
    );
  });

  it("requires an import preview and offers a redacted issue download", async () => {
    const user = userEvent.setup();
    vi.mocked(previewLinuxImport).mockResolvedValue({
      token: "preview-token",
      strategy: "skip",
      total: 1,
      valid: 0,
      invalid: 1,
      duplicates: 0,
      expiresAt: "2026-07-18T12:00:00Z",
      transactionPolicy: "all_or_nothing",
      rows: [
        {
          row: 2,
          name: "bad-host",
          host: "10.0.0.8",
          port: 22,
          authType: "password",
          credentialConfigured: false,
          action: "invalid",
          issues: [
            { row: 2, field: "password", code: "invalid", message: "凭据无效" },
          ],
        },
      ],
    });
    renderPage();

    await user.click(screen.getByRole("button", { name: "批量导入" }));
    await user.upload(
      screen.getByLabelText("文件"),
      new File(["name,host\nbad-host,10.0.0.8"], "hosts.csv", {
        type: "text/csv",
      }),
    );
    await user.click(screen.getByRole("button", { name: "生成 Preview" }));

    expect(await screen.findByText("Preview")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "下载错误行" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "确认导入" })).toBeDisabled();
    expect(previewLinuxImport).toHaveBeenCalledWith(
      expect.objectContaining({ name: "hosts.csv" }),
      "skip",
      {},
    );
    expect(
      redactImportIssueMessage(
        "password=hunter2 token:abc -----BEGIN PRIVATE KEY----- secret -----END PRIVATE KEY-----",
      ),
    ).toBe("password=[REDACTED] token:[REDACTED] [REDACTED]");
  });
});
