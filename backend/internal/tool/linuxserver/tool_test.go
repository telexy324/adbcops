package linuxserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConnectionTestSupportsPasswordAndPrivateKeyWithoutCredentialEcho(t *testing.T) {
	for _, connection := range []LinuxServerConnection{
		testConnection("secret-password"),
		{
			Host: "host.example", Port: 22, Username: "ops", AuthType: LinuxAuthPrivateKey,
			PrivateKey: "test-key-material", PrivateKeyPassword: "key-secret", HostKeyPolicy: HostKeyTrustOnFirstUse,
		},
	} {
		client := newToolFakeClient()
		dialer := &toolFakeDialer{clients: []RemoteClient{client}}
		tool := testTool(t, dialer)
		result, err := tool.Test(context.Background(), connection)
		if err != nil {
			t.Fatalf("Test(%s) error = %v", connection.AuthType, err)
		}
		if result.Status != CommandStatusSuccess || !result.ConfirmationRequired || result.HostKeyFingerprint == "" {
			t.Fatalf("Test(%s) = %+v", connection.AuthType, result)
		}
		encoded, _ := json.Marshal(connection)
		for _, secret := range []string{connection.Password, connection.PrivateKey, connection.PrivateKeyPassword} {
			if secret != "" && bytes.Contains(encoded, []byte(secret)) {
				t.Fatalf("connection JSON leaked credential %q: %s", secret, encoded)
			}
		}
	}
}

func TestAuthenticationFailureIsNotRetried(t *testing.T) {
	dialer := &toolFakeDialer{err: ErrAuthenticationFailed}
	tool := testTool(t, dialer)
	result, err := tool.Test(context.Background(), testConnection("wrong-password"))
	if !errors.Is(err, ErrAuthenticationFailed) {
		t.Fatalf("Test() error = %v", err)
	}
	if dialer.calls != 1 {
		t.Fatalf("dial calls = %d, want 1", dialer.calls)
	}
	if result.ErrorCode != ErrorAuthFailed || strings.Contains(result.Message, "wrong-password") {
		t.Fatalf("result = %+v", result)
	}
}

func TestDetectPlatformUsesCatalogProbesAndReportsMissingCommand(t *testing.T) {
	client := newToolFakeClient()
	client.missing["iostat"] = true
	tool := testTool(t, &toolFakeDialer{clients: []RemoteClient{client}})
	connection := confirmedConnection()
	platform, err := tool.DetectPlatform(context.Background(), connection)
	if err != nil {
		t.Fatalf("DetectPlatform() error = %v", err)
	}
	if platform.ID != "ubuntu" || platform.VersionID != "22.04" || platform.Status != CapabilitySupported {
		t.Fatalf("platform = %+v", platform)
	}
	if platform.AvailableCommands["iostat"] || !platform.AvailableCommands["cat"] {
		t.Fatalf("available commands = %+v", platform.AvailableCommands)
	}
	for _, call := range client.calls {
		if call.executable == "sh" || call.executable == "bash" {
			t.Fatalf("platform detection invoked shell: %+v", call)
		}
	}
}

func TestCollectReturnsStructuredRedactedDataWithoutRawOutput(t *testing.T) {
	client := newToolFakeClient()
	client.outputs["cat /proc/meminfo"] = "MemTotal: 1024 kB\nMemAvailable: 512 kB\nPassword: super-secret\n"
	tool := testTool(t, &toolFakeDialer{clients: []RemoteClient{client}})
	result, err := tool.Collect(context.Background(), confirmedConnection(), LinuxCollectRequest{Collector: CollectorMemory})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if result.Status != CommandStatusPartial || result.CommandVersion == "" {
		t.Fatalf("result = %+v", result)
	}
	encoded, _ := json.Marshal(result)
	if bytes.Contains(encoded, []byte("super-secret")) || bytes.Contains(encoded, []byte(`"output"`)) {
		t.Fatalf("collect result exposed raw sensitive output: %s", encoded)
	}
	if !bytes.Contains(encoded, []byte(`"mem_total":1048576`)) || !bytes.Contains(encoded, []byte(`"mem_used_percent":50`)) {
		t.Fatalf("collect result is not structured: %s", encoded)
	}
}

func TestCollectTimeoutDiscardsConnectionAndReleasesPoolSlot(t *testing.T) {
	blocking := newToolFakeClient()
	blocking.blockExecutable = "free"
	replacement := newToolFakeClient()
	dialer := &toolFakeDialer{clients: []RemoteClient{blocking, replacement}}
	pool := NewConnectionPool(dialer, PoolOptions{PerHostMaxConnections: 1})
	tool, err := NewTool(NewBuiltinCatalog(), pool, SafeOutputParser{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result, err := tool.Collect(ctx, confirmedConnection(), LinuxCollectRequest{Collector: CollectorMemory})
	if err != nil {
		t.Fatalf("Collect(timeout) error = %v", err)
	}
	if result.Status != CommandStatusTimeout || !blocking.closed {
		t.Fatalf("timeout result = %+v, closed = %v", result, blocking.closed)
	}
	if _, err := tool.Test(context.Background(), confirmedConnection()); err != nil {
		t.Fatalf("Test(after timeout) error = %v", err)
	}
	if dialer.calls != 2 {
		t.Fatalf("dial calls = %d, want discarded connection to redial", dialer.calls)
	}
}

func TestCollectRejectsGenericAndInternalProbeCommands(t *testing.T) {
	tool := testTool(t, &toolFakeDialer{clients: []RemoteClient{newToolFakeClient()}})
	for _, key := range []string{"", "command", "shell", "platform.which"} {
		if _, err := tool.Collect(context.Background(), confirmedConnection(), LinuxCollectRequest{Collector: key}); !errors.Is(err, ErrCollectorNotFound) {
			t.Fatalf("Collect(%q) error = %v", key, err)
		}
	}
}

func TestCollectorsDegradeForMissingCommandAndPermissionDenied(t *testing.T) {
	missing := newToolFakeClient()
	missing.missing["free"] = true
	missing.outputs["cat /proc/meminfo"] = "MemTotal: 1024 kB\nMemAvailable: 512 kB\n"
	tool := testTool(t, &toolFakeDialer{clients: []RemoteClient{missing}})
	result, err := tool.Collect(context.Background(), confirmedConnection(), LinuxCollectRequest{Collector: CollectorMemory})
	if err != nil {
		t.Fatalf("Collect(missing command) error = %v", err)
	}
	if result.Status != CommandStatusPartial || !strings.Contains(strings.Join(result.Warnings, " "), "free") {
		t.Fatalf("missing command result = %+v", result)
	}

	permission := newToolFakeClient()
	permission.runErrors["dmesg"] = ErrRunnerPermission
	permission.outputs["journalctl -k -p warning --since -24h --no-pager"] = "kernel warning recovered"
	tool = testTool(t, &toolFakeDialer{clients: []RemoteClient{permission}})
	result, err = tool.Collect(context.Background(), confirmedConnection(), LinuxCollectRequest{Collector: CollectorKernelEvents})
	if err != nil {
		t.Fatalf("Collect(permission) error = %v", err)
	}
	joined := strings.Join(result.Warnings, " ")
	if result.Status != CommandStatusPartial || !strings.Contains(joined, "permission denied") {
		t.Fatalf("permission result = %+v", result)
	}
}

func TestCollectorsApplyTopNAndBoundedLogWindowToCatalogPlans(t *testing.T) {
	client := newToolFakeClient()
	client.outputs["ps -eo pid,stat,etime,comm"] = "PID STAT ELAPSED COMMAND\n1 S 1-00:00 init"
	tool := testTool(t, &toolFakeDialer{clients: []RemoteClient{client}})
	if _, err := tool.Collect(context.Background(), confirmedConnection(), LinuxCollectRequest{
		Collector: CollectorProcess, Parameters: json.RawMessage(`{"topN":100}`),
	}); err != nil {
		t.Fatalf("Collect(process) error = %v", err)
	}
	processCalls := 0
	for _, call := range client.calls {
		if call.executable == "ps" && len(call.args) > 0 && strings.Contains(strings.Join(call.args, " "), "--sort") {
			processCalls++
		}
	}
	if processCalls != 2 {
		t.Fatalf("sorted process calls = %d, want CPU and memory TopN plans", processCalls)
	}

	logsClient := newToolFakeClient()
	tool = testTool(t, &toolFakeDialer{clients: []RemoteClient{logsClient}})
	if _, err := tool.Collect(context.Background(), confirmedConnection(), LinuxCollectRequest{
		Collector: CollectorSystemLogs, Parameters: json.RawMessage(`{"service":"nginx.service","sinceHours":168}`),
	}); err != nil {
		t.Fatalf("Collect(logs) error = %v", err)
	}
	joinedCalls := ""
	for _, call := range logsClient.calls {
		joinedCalls += call.executable + " " + strings.Join(call.args, " ") + "\n"
	}
	if !strings.Contains(joinedCalls, "journalctl -p warning --since -168h --no-pager") ||
		!strings.Contains(joinedCalls, "journalctl -u nginx.service --since -168h --no-pager") {
		t.Fatalf("bounded journal calls missing:\n%s", joinedCalls)
	}
}

func testTool(t *testing.T, dialer SSHDialer) *Tool {
	t.Helper()
	tool, err := NewTool(NewBuiltinCatalog(), NewConnectionPool(dialer, PoolOptions{}), SafeOutputParser{})
	if err != nil {
		t.Fatal(err)
	}
	return tool
}

func testConnection(password string) LinuxServerConnection {
	return LinuxServerConnection{
		Host: "host.example", Port: 22, Username: "ops", AuthType: LinuxAuthPassword,
		Password: password, HostKeyPolicy: HostKeyTrustOnFirstUse,
	}
}

func confirmedConnection() LinuxServerConnection {
	connection := testConnection("secret-password")
	connection.HostKeyAlgorithm = "ssh-ed25519"
	connection.HostKeyFingerprint = "SHA256:test"
	return connection
}

type toolFakeDialer struct {
	mu      sync.Mutex
	clients []RemoteClient
	err     error
	calls   int
}

func (d *toolFakeDialer) Dial(_ context.Context, _ LinuxServerConnection, _ bool) (RemoteClient, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	if d.err != nil {
		return nil, d.err
	}
	if len(d.clients) == 0 {
		return nil, errors.New("no fake SSH client")
	}
	client := d.clients[0]
	d.clients = d.clients[1:]
	return client, nil
}

type commandCall struct {
	executable string
	args       []string
}

type toolFakeClient struct {
	mu              sync.Mutex
	calls           []commandCall
	outputs         map[string]string
	runErrors       map[string]error
	missing         map[string]bool
	blockExecutable string
	closed          bool
}

func newToolFakeClient() *toolFakeClient {
	return &toolFakeClient{outputs: map[string]string{}, runErrors: map[string]error{}, missing: map[string]bool{}}
}

func (c *toolFakeClient) Run(ctx context.Context, executable string, args []string, stdout, _ io.Writer) error {
	c.mu.Lock()
	c.calls = append(c.calls, commandCall{executable: executable, args: append([]string(nil), args...)})
	blocked := executable == c.blockExecutable
	c.mu.Unlock()
	if blocked {
		<-ctx.Done()
		return ctx.Err()
	}
	if executable == "which" {
		if len(args) != 1 || c.missing[args[0]] {
			return ErrRunnerCommandNotFound
		}
		_, _ = io.WriteString(stdout, "/usr/bin/"+args[0]+"\n")
		return nil
	}
	lookup := strings.TrimSpace(executable + " " + strings.Join(args, " "))
	if err := c.runErrors[lookup]; err != nil {
		return err
	}
	if err := c.runErrors[executable]; err != nil {
		return err
	}
	if output, ok := c.outputs[lookup]; ok {
		_, _ = io.WriteString(stdout, output)
		return nil
	}
	if output, ok := c.outputs[executable]; ok {
		_, _ = io.WriteString(stdout, output)
		return nil
	}
	switch executable {
	case "cat":
		_, _ = io.WriteString(stdout, "ID=ubuntu\nVERSION_ID=22.04\nPRETTY_NAME=Ubuntu 22.04 LTS\n")
	case "uname":
		_, _ = io.WriteString(stdout, "Linux host 5.15.0 x86_64 GNU/Linux\n")
	default:
		_, _ = io.WriteString(stdout, "ok\n")
	}
	return nil
}

func (c *toolFakeClient) ServerVersion() string { return "SSH-2.0-OpenSSH_9.6" }
func (c *toolFakeClient) HostKey() HostKeyObservation {
	return HostKeyObservation{Algorithm: "ssh-ed25519", Fingerprint: "SHA256:test"}
}
func (c *toolFakeClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}
