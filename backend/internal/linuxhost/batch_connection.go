package linuxhost

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"aiops-platform/backend/internal/model"
	linuxserver "aiops-platform/backend/internal/tool/linuxserver"
)

const (
	DefaultBatchTestConcurrency = 10
	MaxBatchTestHosts           = 500
	DefaultBatchTestTimeout     = 300 * time.Second

	BatchJobRunning   = "running"
	BatchJobCompleted = "completed"
	BatchJobCancelled = "cancelled"
	BatchJobFailed    = "failed"
)

var (
	ErrBatchTooManyHosts = errors.New("linux batch connection test exceeds 500 hosts")
	ErrBatchEmpty        = errors.New("linux batch connection test has no matching hosts")
	ErrBatchNotFound     = errors.New("linux batch connection test not found")
	ErrBatchRunning      = errors.New("linux batch connection test is still running")
)

type BatchTestFilter struct {
	Environment   string `json:"environment,omitempty"`
	SystemName    string `json:"systemName,omitempty"`
	ComponentName string `json:"componentName,omitempty"`
	Tag           string `json:"tag,omitempty"`
	Enabled       *bool  `json:"enabled,omitempty"`
}

type BatchTestSelection struct {
	HostIDs  []int64         `json:"hostIds,omitempty"`
	GroupIDs []int64         `json:"groupIds,omitempty"`
	Filter   BatchTestFilter `json:"filter,omitempty"`
}

type BatchTestTarget struct {
	HostID     int64
	Connection linuxserver.LinuxServerConnection
	ErrorCode  string
	Message    string
}

type BatchTargetResolver interface {
	ResolveBatchTestTargets(context.Context, BatchTestSelection) ([]BatchTestTarget, error)
}

type LinuxConnectionTester interface {
	Test(context.Context, linuxserver.LinuxServerConnection) (*linuxserver.LinuxConnectionTestResult, error)
}

type BatchTestResultRecorder interface {
	RecordBatchTestResult(context.Context, BatchTestItem) error
}

type BatchTestItem struct {
	HostID        int64  `json:"hostId"`
	Status        string `json:"status"`
	LatencyMs     int64  `json:"latencyMs,omitempty"`
	ServerVersion string `json:"serverVersion,omitempty"`
	AuthMethod    string `json:"authMethod,omitempty"`
	ErrorCode     string `json:"errorCode,omitempty"`
	Message       string `json:"message,omitempty"`
}

type BatchTestJob struct {
	ID          string          `json:"id"`
	Status      string          `json:"status"`
	Total       int             `json:"total"`
	Completed   int             `json:"completed"`
	Success     int             `json:"success"`
	Failed      int             `json:"failed"`
	Cancelled   int             `json:"cancelled"`
	Progress    float64         `json:"progress"`
	Items       []BatchTestItem `json:"items"`
	CreatedAt   time.Time       `json:"createdAt"`
	CompletedAt *time.Time      `json:"completedAt,omitempty"`
}

type batchJobState struct {
	job    BatchTestJob
	cancel context.CancelFunc
	done   chan struct{}
}

type BatchTestManager struct {
	mu          sync.RWMutex
	resolver    BatchTargetResolver
	tester      LinuxConnectionTester
	jobs        map[string]*batchJobState
	concurrency int
	timeout     time.Duration
	nextID      atomic.Uint64
	now         func() time.Time
	recorder    BatchTestResultRecorder
}

func NewBatchTestManager(resolver BatchTargetResolver, tester LinuxConnectionTester, concurrency int, timeout time.Duration) (*BatchTestManager, error) {
	if resolver == nil || tester == nil {
		return nil, ErrInvalidInput
	}
	if concurrency <= 0 {
		concurrency = DefaultBatchTestConcurrency
	}
	if concurrency > 50 {
		return nil, ErrInvalidInput
	}
	if timeout <= 0 {
		timeout = DefaultBatchTestTimeout
	}
	manager := &BatchTestManager{resolver: resolver, tester: tester, jobs: map[string]*batchJobState{}, concurrency: concurrency, timeout: timeout, now: time.Now}
	manager.recorder, _ = resolver.(BatchTestResultRecorder)
	return manager, nil
}

func (m *BatchTestManager) Start(ctx context.Context, actor *model.AppUser, selection BatchTestSelection) (*BatchTestJob, error) {
	if actor == nil || actor.Role != model.RoleAdmin {
		return nil, ErrAdminRequired
	}
	targets, err := m.resolver.ResolveBatchTestTargets(ctx, selection)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, ErrBatchEmpty
	}
	if len(targets) > MaxBatchTestHosts {
		return nil, ErrBatchTooManyHosts
	}
	id := fmt.Sprintf("linux-batch-%d-%d", m.now().UnixMilli(), m.nextID.Add(1))
	runCtx, cancel := context.WithTimeout(context.Background(), m.timeout)
	state := &batchJobState{job: BatchTestJob{ID: id, Status: BatchJobRunning, Total: len(targets), Items: make([]BatchTestItem, len(targets)), CreatedAt: m.now().UTC()}, cancel: cancel, done: make(chan struct{})}
	for i, target := range targets {
		state.job.Items[i] = BatchTestItem{HostID: target.HostID, Status: "pending"}
	}
	m.mu.Lock()
	m.jobs[id] = state
	m.mu.Unlock()
	go m.run(runCtx, state, targets)
	return cloneBatchJob(&state.job), nil
}

func (m *BatchTestManager) run(ctx context.Context, state *batchJobState, targets []BatchTestTarget) {
	defer close(state.done)
	defer state.cancel()
	type indexedTarget struct {
		index  int
		target BatchTestTarget
	}
	queue := make(chan indexedTarget)
	var workers sync.WaitGroup
	for worker := 0; worker < minInt(m.concurrency, len(targets)); worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range queue {
				m.testOne(ctx, state, item.index, item.target)
			}
		}()
	}
	for index, target := range targets {
		select {
		case queue <- indexedTarget{index, target}:
			targets[index].Connection = linuxserver.LinuxServerConnection{}
		case <-ctx.Done():
			for remaining := index; remaining < len(targets); remaining++ {
				m.finishItem(state, remaining, BatchTestItem{HostID: targets[remaining].HostID, Status: "cancelled", ErrorCode: "CANCELLED", Message: "connection test was cancelled"})
			}
			close(queue)
			workers.Wait()
			m.finishJob(state, true)
			return
		}
	}
	close(queue)
	workers.Wait()
	m.finishJob(state, ctx.Err() != nil)
}

func (m *BatchTestManager) testOne(ctx context.Context, state *batchJobState, index int, target BatchTestTarget) {
	if ctx.Err() != nil {
		m.finishItem(state, index, BatchTestItem{HostID: target.HostID, Status: "cancelled", ErrorCode: "CANCELLED", Message: "connection test was cancelled"})
		return
	}
	if target.ErrorCode != "" {
		item := BatchTestItem{HostID: target.HostID, Status: "failed", ErrorCode: target.ErrorCode, Message: target.Message}
		if m.recorder != nil {
			_ = m.recorder.RecordBatchTestResult(context.Background(), item)
		}
		m.finishItem(state, index, item)
		return
	}
	result, err := m.tester.Test(ctx, target.Connection)
	item := BatchTestItem{HostID: target.HostID, Status: "failed", ErrorCode: linuxserver.ErrorUnknown, Message: "SSH connection failed"}
	if result != nil {
		item.LatencyMs, item.ServerVersion, item.AuthMethod = result.LatencyMs, result.ServerVersion, result.AuthMethod
		if result.Status == linuxserver.CommandStatusSuccess {
			item.Status, item.ErrorCode, item.Message = "success", "", ""
		} else {
			item.ErrorCode, item.Message = safeBatchError(result.ErrorCode, result.Message)
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) && ctx.Err() != nil {
		item.Status, item.ErrorCode, item.Message = "cancelled", "CANCELLED", "connection test was cancelled"
	}
	if m.recorder != nil && item.Status != "cancelled" {
		_ = m.recorder.RecordBatchTestResult(context.Background(), item)
	}
	m.finishItem(state, index, item)
}

func (m *BatchTestManager) finishItem(state *batchJobState, index int, item BatchTestItem) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if state.job.Items[index].Status != "pending" {
		return
	}
	state.job.Items[index] = item
	state.job.Completed++
	switch item.Status {
	case "success":
		state.job.Success++
	case "failed":
		state.job.Failed++
	case "cancelled":
		state.job.Cancelled++
	}
	state.job.Progress = float64(state.job.Completed) * 100 / float64(state.job.Total)
}

func (m *BatchTestManager) finishJob(state *batchJobState, cancelled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cancelled || state.job.Cancelled > 0 {
		state.job.Status = BatchJobCancelled
	} else {
		state.job.Status = BatchJobCompleted
	}
	now := m.now().UTC()
	state.job.CompletedAt = &now
}

func (m *BatchTestManager) Get(actor *model.AppUser, id string) (*BatchTestJob, error) {
	if actor == nil || actor.Role != model.RoleAdmin {
		return nil, ErrAdminRequired
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	state, ok := m.jobs[id]
	if !ok {
		return nil, ErrBatchNotFound
	}
	return cloneBatchJob(&state.job), nil
}

func (m *BatchTestManager) Cancel(ctx context.Context, actor *model.AppUser, id string) (*BatchTestJob, error) {
	if actor == nil || actor.Role != model.RoleAdmin {
		return nil, ErrAdminRequired
	}
	m.mu.RLock()
	state, ok := m.jobs[id]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrBatchNotFound
	}
	state.cancel()
	select {
	case <-state.done:
		return m.Get(actor, id)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *BatchTestManager) Export(actor *model.AppUser, id, format string) ([]byte, string, error) {
	job, err := m.Get(actor, id)
	if err != nil {
		return nil, "", err
	}
	if job.Status == BatchJobRunning {
		return nil, "", ErrBatchRunning
	}
	if format == "" || format == "json" {
		payload, err := json.MarshalIndent(job, "", "  ")
		return payload, "application/json", err
	}
	if format != "csv" {
		return nil, "", ErrInvalidInput
	}
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	_ = writer.Write([]string{"host_id", "status", "latency_ms", "server_version", "auth_method", "error_code", "message"})
	for _, item := range job.Items {
		_ = writer.Write([]string{fmt.Sprint(item.HostID), item.Status, fmt.Sprint(item.LatencyMs), csvSafe(item.ServerVersion), csvSafe(item.AuthMethod), item.ErrorCode, csvSafe(item.Message)})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, "", err
	}
	return buffer.Bytes(), "text/csv; charset=utf-8", nil
}

func csvSafe(value string) string {
	if value != "" && strings.ContainsRune("=+-@", rune(value[0])) {
		return "'" + value
	}
	return value
}

func cloneBatchJob(job *BatchTestJob) *BatchTestJob {
	clone := *job
	clone.Items = append([]BatchTestItem(nil), job.Items...)
	return &clone
}

func safeBatchError(code, message string) (string, string) {
	allowed := map[string]string{
		linuxserver.ErrorDNSFailed: "host name resolution failed", linuxserver.ErrorConnectTimeout: "SSH connection timed out",
		linuxserver.ErrorConnectionRefused: "SSH connection was refused", linuxserver.ErrorHostKeyMismatch: "SSH host key verification failed",
		linuxserver.ErrorAuthFailed: "SSH authentication failed", linuxserver.ErrorPermissionDenied: "permission denied",
		linuxserver.ErrorCommandNotFound: "required command is unavailable", linuxserver.ErrorCommandTimeout: "command timed out",
		linuxserver.ErrorUnsupportedOS: "operating system is unsupported", linuxserver.ErrorUnknown: "SSH connection failed",
	}
	if safe, ok := allowed[code]; ok {
		return code, safe
	}
	_ = message
	return linuxserver.ErrorUnknown, "SSH connection failed"
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type batchConnectionRepository interface {
	ListLinuxHosts(context.Context, bool) ([]model.LinuxHost, error)
	ListLinuxHostIDsByGroupIDs(context.Context, []int64) ([]int64, error)
	FindLinuxHostWithCredential(context.Context, int64) (*model.LinuxHost, error)
	RecordLinuxHostConnectionTest(context.Context, int64, string, *string, *string, time.Time) error
}

func (s *Service) RecordBatchTestResult(ctx context.Context, item BatchTestItem) error {
	repository, ok := s.repository.(batchConnectionRepository)
	if !ok {
		return ErrInvalidInput
	}
	status := "failed"
	var code, message *string
	if item.Status == "success" {
		status = "success"
	} else {
		code = optionalString(item.ErrorCode)
		message = optionalString(item.Message)
	}
	return repository.RecordLinuxHostConnectionTest(ctx, item.HostID, status, code, message, time.Now().UTC())
}

func (s *Service) ResolveBatchTestTargets(ctx context.Context, selection BatchTestSelection) ([]BatchTestTarget, error) {
	repository, ok := s.repository.(batchConnectionRepository)
	if !ok || s.secrets == nil {
		return nil, ErrInvalidInput
	}
	hosts, err := repository.ListLinuxHosts(ctx, false)
	if err != nil {
		return nil, err
	}
	selected := map[int64]bool{}
	for _, id := range selection.HostIDs {
		if id > 0 {
			selected[id] = true
		}
	}
	groupIDs, err := repository.ListLinuxHostIDsByGroupIDs(ctx, selection.GroupIDs)
	if err != nil {
		return nil, err
	}
	for _, id := range groupIDs {
		selected[id] = true
	}
	hasFilter := selection.Filter.Environment != "" || selection.Filter.SystemName != "" || selection.Filter.ComponentName != "" || selection.Filter.Tag != "" || selection.Filter.Enabled != nil
	for _, host := range hosts {
		if hasFilter && matchesBatchFilter(host, selection.Filter) {
			selected[host.ID] = true
		}
	}
	if len(selection.HostIDs) == 0 && len(selection.GroupIDs) == 0 && !hasFilter {
		for _, host := range hosts {
			if host.Enabled {
				selected[host.ID] = true
			}
		}
	}
	ids := make([]int64, 0, len(selected))
	for id := range selected {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if len(ids) > MaxBatchTestHosts {
		return nil, ErrBatchTooManyHosts
	}
	targets := make([]BatchTestTarget, 0, len(ids))
	for _, id := range ids {
		target := BatchTestTarget{HostID: id}
		host, e := repository.FindLinuxHostWithCredential(ctx, id)
		if e != nil {
			target.ErrorCode = linuxserver.ErrorUnknown
			target.Message = "host configuration could not be loaded"
		} else {
			target.Connection, e = s.connectionForHost(host)
			if e != nil {
				target.ErrorCode = linuxserver.ErrorAuthFailed
				target.Message = "host credential could not be resolved"
			}
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func (s *Service) connectionForHost(host *model.LinuxHost) (linuxserver.LinuxServerConnection, error) {
	secret := host.Credential
	username := ""
	version := ""
	authType := host.AuthType
	if host.Username != nil {
		username = *host.Username
	}
	if host.CredentialGroupID != nil {
		if host.CredentialGroup == nil || host.CredentialGroup.Credential == nil || !host.CredentialGroup.Enabled {
			return linuxserver.LinuxServerConnection{}, ErrInvalidInput
		}
		secret = host.CredentialGroup.Credential
		username = host.CredentialGroup.Username
		authType = host.CredentialGroup.CredentialType
		version = fmt.Sprintf("group-%d-v%d", host.CredentialGroup.ID, host.CredentialGroup.Version)
	}
	if secret == nil {
		return linuxserver.LinuxServerConnection{}, ErrInvalidInput
	}
	if secret.KeyVersion != nil {
		version += ":" + *secret.KeyVersion
	}
	plaintext, err := s.secrets.Decrypt(secret.EncryptedPayload)
	if err != nil {
		return linuxserver.LinuxServerConnection{}, err
	}
	defer func() { plaintext = "" }()
	var credential map[string]string
	if json.Unmarshal([]byte(plaintext), &credential) != nil {
		return linuxserver.LinuxServerConnection{}, ErrInvalidInput
	}
	if username == "" {
		username = credential["username"]
	}
	hostKeyAlgorithm := pointerValue(host.HostKeyAlgorithm)
	hostKeyFingerprint := pointerValue(host.HostKeyFingerprint)
	// A key observed by a successful connection test is safe to pin for the
	// first formal collection without a separate confirmation click. A later
	// key change still fails in the SSH callback because the observed key must
	// match this exact candidate.
	if hostKeyFingerprint == "" && host.HostKeyStatus == model.LinuxHostKeyStatusPending {
		hostKeyAlgorithm = pointerValue(host.PendingHostKeyAlgorithm)
		hostKeyFingerprint = pointerValue(host.PendingHostKeyFingerprint)
	}
	return linuxserver.LinuxServerConnection{Host: host.Host, Port: host.Port, Username: username, AuthType: authType, Password: credential["password"], PrivateKey: credential["privateKey"], PrivateKeyPassword: credential["privateKeyPassphrase"], HostKeyPolicy: host.HostKeyPolicy, HostKeyAlgorithm: hostKeyAlgorithm, HostKeyFingerprint: hostKeyFingerprint, CredentialVersion: version}, nil
}

func matchesBatchFilter(host model.LinuxHost, filter BatchTestFilter) bool {
	if filter.Environment != "" && !strings.EqualFold(pointerValue(host.Environment), filter.Environment) {
		return false
	}
	if filter.SystemName != "" && !strings.EqualFold(pointerValue(host.SystemName), filter.SystemName) {
		return false
	}
	if filter.ComponentName != "" && !strings.EqualFold(pointerValue(host.ComponentName), filter.ComponentName) {
		return false
	}
	if filter.Enabled != nil && host.Enabled != *filter.Enabled {
		return false
	}
	if filter.Tag != "" {
		var tags []string
		if json.Unmarshal(host.Tags, &tags) != nil {
			return false
		}
		found := false
		for _, tag := range tags {
			if strings.EqualFold(tag, filter.Tag) {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}
