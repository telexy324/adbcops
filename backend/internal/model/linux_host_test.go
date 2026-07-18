package model

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestValidateCredentialGroupScope(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		scope   string
		wantErr bool
	}{
		{name: "empty", scope: ""},
		{name: "null", scope: "null"},
		{name: "both dimensions", scope: `{"environments":["prod"],"systemNames":["payment"]}`},
		{name: "unknown dimension", scope: `{"clusters":["prod"]}`, wantErr: true},
		{name: "wrong value type", scope: `{"environments":"prod"}`, wantErr: true},
		{name: "empty value", scope: `{"systemNames":[""]}`, wantErr: true},
		{name: "duplicate value", scope: `{"environments":["prod","prod"]}`, wantErr: true},
		{name: "not object", scope: `[]`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCredentialGroupScope(json.RawMessage(tt.scope))
			if got := errors.Is(err, ErrInvalidCredentialGroupScope); got != tt.wantErr {
				t.Fatalf("ValidateCredentialGroupScope() error = %v, invalid = %v, want %v", err, got, tt.wantErr)
			}
		})
	}
}

func TestLinuxHostJSONDoesNotExposeCredential(t *testing.T) {
	t.Parallel()
	credentialID := int64(42)
	payload, err := json.Marshal(LinuxHost{
		ID:           1,
		Name:         "server-1",
		Host:         "10.0.0.1",
		CredentialID: &credentialID,
		Credential: &CredentialSecret{
			ID:               credentialID,
			EncryptedPayload: "ciphertext",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(payload)
	for _, forbidden := range []string{"credentialId", "ciphertext", "encryptedPayload"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("host JSON leaked %q: %s", forbidden, serialized)
		}
	}
}

func TestCredentialGroupJSONOnlyReportsConfiguredState(t *testing.T) {
	t.Parallel()
	payload, err := json.Marshal(CredentialGroup{
		ID:                   1,
		CredentialID:         42,
		CredentialConfigured: true,
		Credential: &CredentialSecret{
			ID:               42,
			EncryptedPayload: "ciphertext",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(payload)
	if !strings.Contains(serialized, `"credentialConfigured":true`) {
		t.Fatalf("configured state missing: %s", serialized)
	}
	for _, forbidden := range []string{"credentialId", "ciphertext", "encryptedPayload"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("credential group JSON leaked %q: %s", forbidden, serialized)
		}
	}
}
