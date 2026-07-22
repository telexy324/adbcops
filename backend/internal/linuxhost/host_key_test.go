package linuxhost

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"aiops-platform/backend/internal/model"
)

func TestStrictHostKeyUsesObservedCandidateWithoutAdminConfirmation(t *testing.T) {
	store := newFakeRepository()
	store.hosts[1] = &model.LinuxHost{
		ID: 1, Name: "strict-host", Host: "10.0.0.1", Enabled: true,
		HostKeyPolicy: model.LinuxHostKeyPolicyStrict, HostKeyStatus: model.LinuxHostKeyStatusUnverified,
	}
	events, audits := &fakeHostKeyEvents{}, &fakeHostKeyAudits{}
	service := NewService(store, testCredentialManager(t), "v1").WithHostKeySecurityRecorders(events, audits)
	fingerprint := testFingerprint(1)

	if err := service.PreflightHostKey(context.Background(), 1, HostKeyOperationCollect); !errors.Is(err, ErrHostKeyConfirmationRequired) {
		t.Fatalf("PreflightHostKey(collect) error = %v", err)
	}
	observed, err := service.VerifyObservedHostKey(context.Background(), 1, HostKeyOperationTest, "ssh-ed25519", fingerprint)
	if err != nil || !observed.Allowed || observed.ConfirmationRequired || observed.Status != model.LinuxHostKeyStatusPending {
		t.Fatalf("VerifyObservedHostKey(test) = %+v, error = %v", observed, err)
	}
	if err := service.PreflightHostKey(context.Background(), 1, HostKeyOperationCollect); err != nil {
		t.Fatalf("PreflightHostKey(pending collect) error = %v", err)
	}
	verified, err := service.VerifyObservedHostKey(context.Background(), 1, HostKeyOperationCollect, "ssh-ed25519", fingerprint)
	if err != nil || !verified.Allowed || verified.ConfirmationRequired {
		t.Fatalf("VerifyObservedHostKey(pending collect) = %+v, error = %v", verified, err)
	}

	// Manual promotion to trusted remains available but is no longer required
	// before collection.
	user := &model.AppUser{ID: 2, Role: model.RoleUser}
	if _, err := service.ConfirmHostKey(context.Background(), user, 1, ConfirmHostKeyInput{Algorithm: "ssh-ed25519", Fingerprint: fingerprint}); !errors.Is(err, ErrAdminRequired) {
		t.Fatalf("ConfirmHostKey(user) error = %v", err)
	}
	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	confirmed, err := service.ConfirmHostKey(context.Background(), admin, 1, ConfirmHostKeyInput{Algorithm: "ssh-ed25519", Fingerprint: fingerprint})
	if err != nil || confirmed.HostKeyStatus != model.LinuxHostKeyStatusTrusted || confirmed.HostKeyConfirmedBy == nil {
		t.Fatalf("ConfirmHostKey(admin) = %+v, error = %v", confirmed, err)
	}
	if err := service.PreflightHostKey(context.Background(), 1, HostKeyOperationCollect); err != nil {
		t.Fatalf("PreflightHostKey(trusted collect) error = %v", err)
	}
	verified, err = service.VerifyObservedHostKey(context.Background(), 1, HostKeyOperationCollect, "ssh-ed25519", fingerprint)
	if err != nil || !verified.Allowed || verified.ConfirmationRequired {
		t.Fatalf("VerifyObservedHostKey(collect) = %+v, error = %v", verified, err)
	}
	if len(audits.logs) != 1 || audits.logs[0].Action != model.AuditActionManagement {
		t.Fatalf("confirmation audits = %+v", audits.logs)
	}
}

func TestTrustedHostKeyChangeBlocksConnectionAndRecordsSecurityArtifacts(t *testing.T) {
	store := newFakeRepository()
	trusted, changed := testFingerprint(2), testFingerprint(3)
	algorithm := "ssh-ed25519"
	store.hosts[7] = &model.LinuxHost{
		ID: 7, Name: "prod-host", Host: "10.0.0.7", Enabled: true,
		HostKeyPolicy: model.LinuxHostKeyPolicyStrict, HostKeyStatus: model.LinuxHostKeyStatusTrusted,
		HostKeyAlgorithm: &algorithm, HostKeyFingerprint: &trusted,
	}
	events, audits := &fakeHostKeyEvents{}, &fakeHostKeyAudits{}
	service := NewService(store, testCredentialManager(t), "v1").WithHostKeySecurityRecorders(events, audits)

	_, err := service.VerifyObservedHostKey(context.Background(), 7, HostKeyOperationCollect, algorithm, changed)
	if !errors.Is(err, ErrHostKeyMismatch) {
		t.Fatalf("VerifyObservedHostKey(changed) error = %v", err)
	}
	host := store.hosts[7]
	if host.HostKeyStatus != model.LinuxHostKeyStatusMismatch || *host.HostKeyFingerprint != trusted ||
		host.PendingHostKeyFingerprint == nil || *host.PendingHostKeyFingerprint != changed ||
		host.LastErrorCode == nil || *host.LastErrorCode != model.LinuxHostKeyMismatchErrorCode {
		t.Fatalf("mismatched host state = %+v", host)
	}
	if len(events.events) != 1 || events.events[0].EventType != model.EventTypeLinuxSSHHostKeyChanged ||
		events.events[0].Severity == nil || *events.events[0].Severity != "high" {
		t.Fatalf("host key events = %+v", events.events)
	}
	if len(audits.logs) != 1 || audits.logs[0].Action != model.AuditActionSecurity || audits.logs[0].StatusCode != 409 {
		t.Fatalf("host key audits = %+v", audits.logs)
	}
	if err := service.PreflightHostKey(context.Background(), 7, HostKeyOperationTest); !errors.Is(err, ErrHostKeyMismatch) {
		t.Fatalf("PreflightHostKey(after mismatch) error = %v", err)
	}
}

func TestTOFUAllowsObservedCandidateForCollection(t *testing.T) {
	store := newFakeRepository()
	store.hosts[3] = &model.LinuxHost{
		ID: 3, Name: "tofu-host", Host: "10.0.0.3", Enabled: true,
		HostKeyPolicy: model.LinuxHostKeyPolicyTrustOnFirstUse, HostKeyStatus: model.LinuxHostKeyStatusUnverified,
	}
	service := NewService(store, testCredentialManager(t), "v1").WithHostKeySecurityRecorders(&fakeHostKeyEvents{}, &fakeHostKeyAudits{})
	fingerprint := testFingerprint(4)
	if _, err := service.VerifyObservedHostKey(context.Background(), 3, HostKeyOperationCollect, "ssh-rsa", fingerprint); !errors.Is(err, ErrHostKeyConfirmationRequired) {
		t.Fatalf("TOFU collect error = %v", err)
	}
	result, err := service.VerifyObservedHostKey(context.Background(), 3, HostKeyOperationTest, "ssh-rsa", fingerprint)
	if err != nil || !result.Allowed || result.ConfirmationRequired {
		t.Fatalf("TOFU test = %+v, error = %v", result, err)
	}
	result, err = service.VerifyObservedHostKey(context.Background(), 3, HostKeyOperationCollect, "ssh-rsa", fingerprint)
	if err != nil || !result.Allowed || result.ConfirmationRequired {
		t.Fatalf("TOFU collect after observation = %+v, error = %v", result, err)
	}
}

func TestInsecureHostKeyPolicyIsDisabledByDefault(t *testing.T) {
	store := newFakeRepository()
	store.hosts[4] = &model.LinuxHost{
		ID: 4, Name: "insecure", Host: "10.0.0.4", Enabled: true,
		HostKeyPolicy: model.LinuxHostKeyPolicyInsecure, HostKeyStatus: model.LinuxHostKeyStatusUnverified,
	}
	service := NewService(store, testCredentialManager(t), "v1")
	if err := service.PreflightHostKey(context.Background(), 4, HostKeyOperationTest); !errors.Is(err, ErrInsecureHostKeyDisabled) {
		t.Fatalf("PreflightHostKey(insecure) error = %v", err)
	}
	if _, err := service.VerifyObservedHostKey(context.Background(), 4, HostKeyOperationTest, "ssh-ed25519", testFingerprint(5)); !errors.Is(err, ErrInsecureHostKeyDisabled) {
		t.Fatalf("VerifyObservedHostKey(insecure) error = %v", err)
	}
}

func TestInvalidHostKeyFingerprintIsRejected(t *testing.T) {
	if _, err := normalizeHostKeyFingerprint("MD5:aa:bb"); !errors.Is(err, ErrInvalidHostKey) {
		t.Fatalf("normalizeHostKeyFingerprint() error = %v", err)
	}
}

func testFingerprint(fill byte) string {
	return "SHA256:" + base64.RawStdEncoding.EncodeToString([]byte{
		fill, fill, fill, fill, fill, fill, fill, fill,
		fill, fill, fill, fill, fill, fill, fill, fill,
		fill, fill, fill, fill, fill, fill, fill, fill,
		fill, fill, fill, fill, fill, fill, fill, fill,
	})
}

type fakeHostKeyEvents struct{ events []*model.OpsEvent }

func (f *fakeHostKeyEvents) UpsertOpsEvent(_ context.Context, event *model.OpsEvent) (*model.OpsEvent, error) {
	copy := *event
	f.events = append(f.events, &copy)
	return &copy, nil
}

type fakeHostKeyAudits struct{ logs []*model.AuditLog }

func (f *fakeHostKeyAudits) CreateAuditLog(_ context.Context, log *model.AuditLog) error {
	copy := *log
	f.logs = append(f.logs, &copy)
	return nil
}
