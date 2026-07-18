package linuxhost

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"aiops-platform/backend/internal/model"
	linuxserver "aiops-platform/backend/internal/tool/linuxserver"
)

func TestBatchConnectionLimitAndFailureIsolation(t *testing.T) {
	targets := make([]BatchTestTarget, 25)
	for i := range targets {
		targets[i] = BatchTestTarget{HostID: int64(i + 1), Connection: linuxserver.LinuxServerConnection{Password: "must-not-leak"}}
	}
	tester := &countingBatchTester{delay: 5 * time.Millisecond, fail: map[int]bool{3: true, 17: true}}
	manager, _ := NewBatchTestManager(staticBatchResolver{targets}, tester, 0, time.Second)
	job, err := manager.Start(context.Background(), importAdmin(), BatchTestSelection{})
	if err != nil {
		t.Fatal(err)
	}
	job = waitBatchJob(t, manager, job.ID)
	if job.Status != BatchJobCompleted || job.Success != 23 || job.Failed != 2 || job.Completed != 25 || job.Progress != 100 {
		t.Fatalf("job=%+v", job)
	}
	if tester.maxActive != DefaultBatchTestConcurrency {
		t.Fatalf("max concurrency=%d, want %d", tester.maxActive, DefaultBatchTestConcurrency)
	}
	payload, _, err := manager.Export(importAdmin(), job.ID, "json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "must-not-leak") {
		t.Fatal("export leaked credential")
	}
	csvPayload, contentType, err := manager.Export(importAdmin(), job.ID, "csv")
	if err != nil || !strings.HasPrefix(contentType, "text/csv") || !strings.Contains(string(csvPayload), "AUTH_FAILED") {
		t.Fatalf("csv=%s type=%s err=%v", csvPayload, contentType, err)
	}
}

func TestBatchConnectionRejectsMoreThan500Hosts(t *testing.T) {
	targets := make([]BatchTestTarget, MaxBatchTestHosts+1)
	for i := range targets {
		targets[i].HostID = int64(i + 1)
	}
	manager, _ := NewBatchTestManager(staticBatchResolver{targets}, &countingBatchTester{}, 10, time.Second)
	if _, err := manager.Start(context.Background(), importAdmin(), BatchTestSelection{}); !errors.Is(err, ErrBatchTooManyHosts) {
		t.Fatalf("error=%v", err)
	}
}

func TestBatchConnectionProgressAndCancelLeaveNoRunningItems(t *testing.T) {
	targets := make([]BatchTestTarget, 30)
	for i := range targets {
		targets[i].HostID = int64(i + 1)
	}
	tester := &countingBatchTester{block: true}
	manager, _ := NewBatchTestManager(staticBatchResolver{targets}, tester, 3, time.Minute)
	started, err := manager.Start(context.Background(), importAdmin(), BatchTestSelection{})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for tester.activeCount() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	progress, err := manager.Get(importAdmin(), started.ID)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Status != BatchJobRunning || progress.Total != 30 {
		t.Fatalf("progress=%+v", progress)
	}
	cancelled, err := manager.Cancel(context.Background(), importAdmin(), started.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != BatchJobCancelled || cancelled.Completed != cancelled.Total || cancelled.Cancelled != cancelled.Total {
		t.Fatalf("cancelled=%+v", cancelled)
	}
	for _, item := range cancelled.Items {
		if item.Status == "running" || item.Status == "pending" {
			t.Fatalf("leftover item=%+v", item)
		}
	}
}

func TestBatchConnectionGlobalTimeoutCompletesWithoutRunningState(t *testing.T) {
	targets := make([]BatchTestTarget, 8)
	for i := range targets {
		targets[i].HostID = int64(i + 1)
	}
	manager, _ := NewBatchTestManager(staticBatchResolver{targets}, &countingBatchTester{block: true}, 2, 20*time.Millisecond)
	job, err := manager.Start(context.Background(), importAdmin(), BatchTestSelection{})
	if err != nil {
		t.Fatal(err)
	}
	job = waitBatchJob(t, manager, job.ID)
	if job.Status != BatchJobCancelled || job.Completed != job.Total {
		t.Fatalf("job=%+v", job)
	}
}

func TestBatchConnectionTargetResolutionUnionsIDsGroupsAndFilter(t *testing.T) {
	store := &batchResolverStore{fakeRepository: newFakeRepository(), groupMembers: []int64{2}}
	prod := "prod"
	dev := "dev"
	store.hosts[1] = &model.LinuxHost{ID: 1, Host: "h1", Port: 22, Environment: &prod, Enabled: true}
	store.hosts[2] = &model.LinuxHost{ID: 2, Host: "h2", Port: 22, Environment: &dev, Enabled: true}
	store.hosts[3] = &model.LinuxHost{ID: 3, Host: "h3", Port: 22, Environment: &prod, Enabled: true}
	for id, host := range store.hosts {
		password := "pw"
		username := "ops"
		credential, err := NewService(store, testCredentialManager(t), "v1").encryptCredential(model.LinuxAuthTypePassword, username, &password, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		credential.ID = id
		store.secrets[id] = credential
		host.CredentialID = &credential.ID
		host.Credential = credential
		host.Username = &username
		host.AuthType = model.LinuxAuthTypePassword
		host.HostKeyPolicy = model.LinuxHostKeyPolicyTrustOnFirstUse
	}
	service := NewService(store, testCredentialManager(t), "v1")
	targets, err := service.ResolveBatchTestTargets(context.Background(), BatchTestSelection{HostIDs: []int64{1}, GroupIDs: []int64{7}, Filter: BatchTestFilter{Environment: "prod"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 3 || targets[0].HostID != 1 || targets[1].HostID != 2 || targets[2].HostID != 3 {
		t.Fatalf("targets=%+v", targets)
	}
	for _, target := range targets {
		if target.Connection.Password != "pw" || target.Connection.Username != "ops" {
			t.Fatalf("connection unresolved for %d", target.HostID)
		}
	}
}

func TestBatchConnectionResolvesCredentialGroupWithoutExposingSecret(t *testing.T) {
	manager := testCredentialManager(t)
	password := "group-secret"
	credential, err := NewService(newFakeRepository(), manager, "v1").encryptCredential(model.LinuxAuthTypePassword, "shared", &password, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	credential.ID = 11
	groupID := int64(7)
	host := &model.LinuxHost{ID: 1, Host: "group-host", Port: 22, AuthType: model.LinuxAuthTypePassword, CredentialGroupID: &groupID, CredentialGroup: &model.CredentialGroup{ID: groupID, CredentialType: model.LinuxAuthTypePassword, Username: "shared", Credential: credential, Enabled: true, Version: 3}, HostKeyPolicy: model.LinuxHostKeyPolicyTrustOnFirstUse}
	service := NewService(newFakeRepository(), manager, "v1")
	connection, err := service.connectionForHost(host)
	if err != nil {
		t.Fatal(err)
	}
	if connection.Username != "shared" || connection.Password != "group-secret" || !strings.Contains(connection.CredentialVersion, "group-7-v3") {
		t.Fatalf("connection=%+v", connection)
	}
	payload, _ := json.Marshal(connection)
	if strings.Contains(string(payload), "group-secret") {
		t.Fatal("connection JSON leaked group credential")
	}
}

type staticBatchResolver struct{ targets []BatchTestTarget }

func (s staticBatchResolver) ResolveBatchTestTargets(context.Context, BatchTestSelection) ([]BatchTestTarget, error) {
	return s.targets, nil
}

type countingBatchTester struct {
	mu                sync.Mutex
	active, maxActive int
	calls             int
	delay             time.Duration
	block             bool
	fail              map[int]bool
}

func (t *countingBatchTester) Test(ctx context.Context, _ linuxserver.LinuxServerConnection) (*linuxserver.LinuxConnectionTestResult, error) {
	t.mu.Lock()
	t.active++
	t.calls++
	call := t.calls
	if t.active > t.maxActive {
		t.maxActive = t.active
	}
	t.mu.Unlock()
	defer func() { t.mu.Lock(); t.active--; t.mu.Unlock() }()
	if t.block {
		<-ctx.Done()
		return &linuxserver.LinuxConnectionTestResult{Status: linuxserver.CommandStatusFailed, ErrorCode: linuxserver.ErrorConnectTimeout}, ctx.Err()
	}
	if t.delay > 0 {
		time.Sleep(t.delay)
	}
	if t.fail[call] {
		return &linuxserver.LinuxConnectionTestResult{Status: linuxserver.CommandStatusFailed, ErrorCode: linuxserver.ErrorAuthFailed, Message: "raw secret details"}, errors.New("auth")
	}
	return &linuxserver.LinuxConnectionTestResult{Status: linuxserver.CommandStatusSuccess, LatencyMs: 5, ServerVersion: "SSH-2.0-test", AuthMethod: "password"}, nil
}
func (t *countingBatchTester) activeCount() int { t.mu.Lock(); defer t.mu.Unlock(); return t.active }

func waitBatchJob(t *testing.T, m *BatchTestManager, id string) *BatchTestJob {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, err := m.Get(importAdmin(), id)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status != BatchJobRunning {
			return job
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("batch job did not finish")
	return nil
}

type batchResolverStore struct {
	*fakeRepository
	groupMembers []int64
}

func (s *batchResolverStore) RecordLinuxHostConnectionTest(context.Context, int64, string, *string, *string, time.Time) error {
	return nil
}

func (s *batchResolverStore) ListLinuxHostIDsByGroupIDs(context.Context, []int64) ([]int64, error) {
	return append([]int64(nil), s.groupMembers...), nil
}
func (s *batchResolverStore) FindLinuxHostWithCredential(_ context.Context, id int64) (*model.LinuxHost, error) {
	host := s.hosts[id]
	if host == nil {
		return nil, errors.New("not found")
	}
	copy := *host
	if copy.CredentialID != nil {
		copy.Credential = s.secrets[*copy.CredentialID]
	}
	return &copy, nil
}

func TestBatchResultJSONHasNoConnectionMaterial(t *testing.T) {
	item := BatchTestItem{HostID: 1, Status: "failed", ErrorCode: linuxserver.ErrorAuthFailed, Message: "SSH authentication failed"}
	payload, _ := json.Marshal(item)
	for _, key := range []string{"password", "privateKey", "credential"} {
		if strings.Contains(string(payload), key) {
			t.Fatalf("result leaked field %s: %s", key, payload)
		}
	}
}

func TestBatchErrorClassificationIsBoundedAndSafe(t *testing.T) {
	for _, code := range []string{linuxserver.ErrorDNSFailed, linuxserver.ErrorConnectTimeout, linuxserver.ErrorConnectionRefused, linuxserver.ErrorHostKeyMismatch, linuxserver.ErrorAuthFailed, linuxserver.ErrorPermissionDenied, linuxserver.ErrorCommandNotFound, linuxserver.ErrorCommandTimeout, linuxserver.ErrorUnsupportedOS, linuxserver.ErrorUnknown} {
		actual, message := safeBatchError(code, "password=secret raw failure")
		if actual != code || strings.Contains(message, "secret") {
			t.Errorf("classification %s => %s %q", code, actual, message)
		}
	}
	code, message := safeBatchError("UNRECOGNIZED", "privateKey=secret")
	if code != linuxserver.ErrorUnknown || strings.Contains(message, "secret") {
		t.Fatalf("unknown => %s %q", code, message)
	}
}

func TestBatchCSVNeutralizesSpreadsheetFormula(t *testing.T) {
	if got := csvSafe("=HYPERLINK(\"bad\")"); !strings.HasPrefix(got, "'=") {
		t.Fatalf("csvSafe=%q", got)
	}
}
