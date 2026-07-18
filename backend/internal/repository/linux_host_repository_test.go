package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"aiops-platform/backend/internal/model"
	"github.com/jackc/pgx/v5/pgconn"
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

func TestMapLinuxMutationError(t *testing.T) {
	t.Parallel()
	duplicate := mapLinuxMutationError(&pgconn.PgError{Code: "23505", ConstraintName: "uq_linux_host_environment_address"}, false)
	if !errors.Is(duplicate, ErrLinuxResourceConflict) {
		t.Fatalf("duplicate error = %v", duplicate)
	}
	referenced := mapLinuxMutationError(&pgconn.PgError{Code: "23503", ConstraintName: "fk_linux_host_credential_group"}, true)
	if !errors.Is(referenced, ErrCredentialGroupReferenced) {
		t.Fatalf("referenced error = %v", referenced)
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
