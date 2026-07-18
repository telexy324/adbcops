package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"aiops-platform/backend/internal/model"
)

func TestCreateCredentialGroupRejectsInvalidScopeBeforeDatabaseAccess(t *testing.T) {
	t.Parallel()
	repository := NewLinuxHostRepository(nil)
	err := repository.CreateCredentialGroup(context.Background(), &model.CredentialGroup{
		Scope: json.RawMessage(`{"unknown":["prod"]}`),
	}, &model.CredentialSecret{})
	if !errors.Is(err, model.ErrInvalidCredentialGroupScope) {
		t.Fatalf("CreateCredentialGroup() error = %v, want ErrInvalidCredentialGroupScope", err)
	}
}

func TestCreateLinuxHostRejectsConflictingCredentialSourcesBeforeDatabaseAccess(t *testing.T) {
	t.Parallel()
	repository := NewLinuxHostRepository(nil)
	groupID := int64(7)
	err := repository.CreateLinuxHost(context.Background(), &model.LinuxHost{
		CredentialGroupID: &groupID,
	}, &model.CredentialSecret{})
	if !errors.Is(err, ErrConflictingLinuxHostCredentials) {
		t.Fatalf("CreateLinuxHost() error = %v, want ErrConflictingLinuxHostCredentials", err)
	}
}

func TestMarkCredentialGroupsConfigured(t *testing.T) {
	t.Parallel()
	groups := []model.CredentialGroup{{CredentialID: 9}, {CredentialID: 0}}
	markCredentialGroupsConfigured(groups)
	if !groups[0].CredentialConfigured || groups[1].CredentialConfigured {
		t.Fatalf("configured flags = %v, %v", groups[0].CredentialConfigured, groups[1].CredentialConfigured)
	}
}
