package linuxhost

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	appmiddleware "aiops-platform/backend/internal/middleware"
	"aiops-platform/backend/internal/model"
	"golang.org/x/crypto/ssh"
)

const (
	HostKeyOperationTest    = "test"
	HostKeyOperationCollect = "collect"
)

var (
	ErrHostKeyConfirmationRequired = errors.New("SSH host key confirmation is required")
	ErrHostKeyMismatch             = errors.New("SSH host key mismatch")
	ErrInsecureHostKeyDisabled     = errors.New("insecure SSH host key policy is disabled")
	ErrInvalidHostKey              = errors.New("invalid SSH host key")
)

type HostKeyEventRecorder interface {
	UpsertOpsEvent(ctx context.Context, event *model.OpsEvent) (*model.OpsEvent, error)
}

type HostKeyAuditRecorder interface {
	CreateAuditLog(ctx context.Context, log *model.AuditLog) error
}

type HostKeyVerification struct {
	Allowed              bool   `json:"allowed"`
	Status               string `json:"status"`
	Algorithm            string `json:"algorithm"`
	Fingerprint          string `json:"fingerprint"`
	ConfirmationRequired bool   `json:"confirmationRequired"`
}

type ConfirmHostKeyInput struct {
	Algorithm   string `json:"algorithm"`
	Fingerprint string `json:"fingerprint"`
}

func (s *Service) WithHostKeySecurityRecorders(events HostKeyEventRecorder, audits HostKeyAuditRecorder) *Service {
	s.events = events
	s.audits = audits
	return s
}

// PreflightHostKey prevents formal collection from opening an SSH connection
// before an administrator has explicitly trusted the server key.
func (s *Service) PreflightHostKey(ctx context.Context, hostID int64, operation string) error {
	if !validHostKeyOperation(operation) || hostID <= 0 {
		return ErrInvalidHostKey
	}
	host, err := s.repository.FindLinuxHostByID(ctx, hostID)
	if err != nil {
		return err
	}
	if host.HostKeyPolicy == model.LinuxHostKeyPolicyInsecure {
		return ErrInsecureHostKeyDisabled
	}
	if host.HostKeyPolicy != model.LinuxHostKeyPolicyStrict && host.HostKeyPolicy != model.LinuxHostKeyPolicyTrustOnFirstUse {
		return ErrInvalidHostKey
	}
	if host.HostKeyStatus == model.LinuxHostKeyStatusMismatch {
		return ErrHostKeyMismatch
	}
	if operation == HostKeyOperationCollect && !trustedHostKeyConfigured(host) {
		return ErrHostKeyConfirmationRequired
	}
	return nil
}

// VerifyObservedHostKey is designed to be called by an ssh.HostKeyCallback.
// Test connections may observe a first key, while collection requires a key
// that was already confirmed before the connection started.
func (s *Service) VerifyObservedHostKey(ctx context.Context, hostID int64, operation, algorithm, fingerprint string) (*HostKeyVerification, error) {
	if !validHostKeyOperation(operation) || hostID <= 0 {
		return nil, ErrInvalidHostKey
	}
	algorithm, err := normalizeHostKeyAlgorithm(algorithm)
	if err != nil {
		return nil, err
	}
	fingerprint, err = normalizeHostKeyFingerprint(fingerprint)
	if err != nil {
		return nil, err
	}
	host, err := s.repository.FindLinuxHostByID(ctx, hostID)
	if err != nil {
		return nil, err
	}
	if !host.Enabled {
		return nil, ErrInvalidInput
	}
	if host.HostKeyPolicy == model.LinuxHostKeyPolicyInsecure {
		return nil, ErrInsecureHostKeyDisabled
	}
	if host.HostKeyPolicy != model.LinuxHostKeyPolicyStrict && host.HostKeyPolicy != model.LinuxHostKeyPolicyTrustOnFirstUse {
		return nil, ErrInvalidHostKey
	}

	if host.HostKeyStatus == model.LinuxHostKeyStatusMismatch {
		return nil, ErrHostKeyMismatch
	}
	if trustedHostKeyConfigured(host) {
		if *host.HostKeyAlgorithm != algorithm || *host.HostKeyFingerprint != fingerprint {
			return nil, s.recordHostKeyMismatch(ctx, host, algorithm, fingerprint)
		}
		return &HostKeyVerification{
			Allowed: true, Status: model.LinuxHostKeyStatusTrusted,
			Algorithm: algorithm, Fingerprint: fingerprint,
		}, nil
	}

	if host.PendingHostKeyFingerprint != nil || host.PendingHostKeyAlgorithm != nil {
		if host.PendingHostKeyFingerprint == nil || host.PendingHostKeyAlgorithm == nil ||
			*host.PendingHostKeyAlgorithm != algorithm || *host.PendingHostKeyFingerprint != fingerprint {
			return nil, s.recordHostKeyMismatch(ctx, host, algorithm, fingerprint)
		}
		if operation == HostKeyOperationCollect {
			return nil, ErrHostKeyConfirmationRequired
		}
		return &HostKeyVerification{
			Allowed: true, Status: model.LinuxHostKeyStatusPending,
			Algorithm: algorithm, Fingerprint: fingerprint, ConfirmationRequired: true,
		}, nil
	}

	if operation == HostKeyOperationCollect {
		return nil, ErrHostKeyConfirmationRequired
	}
	now := time.Now().UTC()
	updated, err := s.repository.RecordLinuxHostKeyCandidate(ctx, host.ID, algorithm, fingerprint, now)
	if err != nil {
		return nil, err
	}
	return &HostKeyVerification{
		Allowed: true, Status: updated.HostKeyStatus,
		Algorithm: algorithm, Fingerprint: fingerprint, ConfirmationRequired: true,
	}, nil
}

func (s *Service) SSHHostKeyCallback(ctx context.Context, hostID int64, operation string) ssh.HostKeyCallback {
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		_, err := s.VerifyObservedHostKey(ctx, hostID, operation, key.Type(), ssh.FingerprintSHA256(key))
		return err
	}
}

func (s *Service) ConfirmHostKey(ctx context.Context, actor *model.AppUser, hostID int64, input ConfirmHostKeyInput) (*HostView, error) {
	if err := requireAdmin(actor); err != nil {
		return nil, err
	}
	if hostID <= 0 {
		return nil, ErrInvalidHostKey
	}
	algorithm, err := normalizeHostKeyAlgorithm(input.Algorithm)
	if err != nil {
		return nil, err
	}
	fingerprint, err := normalizeHostKeyFingerprint(input.Fingerprint)
	if err != nil {
		return nil, err
	}
	host, err := s.repository.FindLinuxHostByID(ctx, hostID)
	if err != nil {
		return nil, err
	}
	if host.PendingHostKeyAlgorithm == nil || host.PendingHostKeyFingerprint == nil ||
		*host.PendingHostKeyAlgorithm != algorithm || *host.PendingHostKeyFingerprint != fingerprint ||
		(host.HostKeyStatus != model.LinuxHostKeyStatusPending && host.HostKeyStatus != model.LinuxHostKeyStatusMismatch) {
		return nil, ErrHostKeyConfirmationRequired
	}
	now := time.Now().UTC()
	updated, err := s.repository.ConfirmLinuxHostKey(ctx, hostID, algorithm, fingerprint, actor.ID, now)
	if err != nil {
		return nil, err
	}
	if auditErr := s.recordHostKeyAudit(ctx, updated, actor, model.AuditActionManagement, http.StatusOK, "host key confirmed"); auditErr != nil {
		return nil, auditErr
	}
	view := hostToView(updated)
	return &view, nil
}

func (s *Service) recordHostKeyMismatch(ctx context.Context, host *model.LinuxHost, algorithm, fingerprint string) error {
	now := time.Now().UTC()
	updated, updateErr := s.repository.RecordLinuxHostKeyMismatch(ctx, host.ID, algorithm, fingerprint, now)
	if updateErr != nil {
		return errors.Join(ErrHostKeyMismatch, updateErr)
	}
	payload, _ := json.Marshal(map[string]any{
		"hostId": host.ID, "trustedAlgorithm": host.HostKeyAlgorithm,
		"trustedFingerprint": host.HostKeyFingerprint, "observedAlgorithm": algorithm,
		"observedFingerprint": fingerprint, "requiresAdminConfirmation": true,
	})
	severity := "high"
	status := model.EventStatusFiring
	sourceID := strconv.FormatInt(host.ID, 10)
	eventFingerprint := fmt.Sprintf("linux_ssh_host_key_changed:%d", host.ID)
	resourceKind := "linux_host"
	event := &model.OpsEvent{
		EventTime: now, SourceType: model.EventSourceLinuxSSH, SourceID: &sourceID,
		EventType: model.EventTypeLinuxSSHHostKeyChanged, Severity: &severity, Status: status,
		Environment: host.Environment, SystemName: host.SystemName, ComponentName: host.ComponentName,
		ResourceKind: &resourceKind, ResourceName: &host.Name, Host: &host.Host,
		Fingerprint: &eventFingerprint, Summary: "SSH host key changed; connection blocked pending administrator review",
		Payload: payload, OccurrenceCount: 1, FirstSeenAt: now, LastSeenAt: now,
	}
	var recorderErrors []error
	if s.events == nil {
		recorderErrors = append(recorderErrors, errors.New("host key event recorder is not configured"))
	} else if _, err := s.events.UpsertOpsEvent(ctx, event); err != nil {
		recorderErrors = append(recorderErrors, fmt.Errorf("record host key mismatch event: %w", err))
	}
	if err := s.recordHostKeyAudit(ctx, updated, nil, model.AuditActionSecurity, http.StatusConflict, "host key mismatch"); err != nil {
		recorderErrors = append(recorderErrors, err)
	}
	return errors.Join(append([]error{ErrHostKeyMismatch}, recorderErrors...)...)
}

func (s *Service) recordHostKeyAudit(ctx context.Context, host *model.LinuxHost, actor *model.AppUser, action string, status int, outcome string) error {
	if s.audits == nil {
		return errors.New("host key audit recorder is not configured")
	}
	metadata, _ := json.Marshal(map[string]any{
		"host_id": host.ID, "host_key_status": host.HostKeyStatus,
		"outcome": outcome, "error_code": host.LastErrorCode,
	})
	requestID := appmiddleware.GetRequestIDFromContext(ctx)
	if requestID == "" {
		requestID = fmt.Sprintf("internal-host-key-%d-%d", host.ID, time.Now().UTC().UnixNano())
	}
	entry := &model.AuditLog{
		RequestID: requestID, Method: "SSH", Path: "/internal/linux/host-key/verify",
		Route: "/internal/linux/host-key/verify", Action: action, Resource: "linux_host",
		StatusCode: status, Metadata: metadata,
	}
	if actor != nil {
		entry.UserID = &actor.ID
		entry.Username = &actor.Username
	}
	if err := s.audits.CreateAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("record host key audit: %w", err)
	}
	return nil
}

func trustedHostKeyConfigured(host *model.LinuxHost) bool {
	return host.HostKeyStatus == model.LinuxHostKeyStatusTrusted &&
		host.HostKeyAlgorithm != nil && host.HostKeyFingerprint != nil
}

func validHostKeyOperation(operation string) bool {
	return operation == HostKeyOperationTest || operation == HostKeyOperationCollect
}

func normalizeHostKeyAlgorithm(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 100 || strings.ContainsAny(value, " \t\r\n") {
		return "", ErrInvalidHostKey
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') && char != '-' && char != '_' && char != '@' && char != '.' {
			return "", ErrInvalidHostKey
		}
	}
	return value, nil
}

func normalizeHostKeyFingerprint(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "SHA256:") {
		return "", ErrInvalidHostKey
	}
	digest, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, "SHA256:"))
	if err != nil || len(digest) != 32 {
		return "", ErrInvalidHostKey
	}
	return value, nil
}
