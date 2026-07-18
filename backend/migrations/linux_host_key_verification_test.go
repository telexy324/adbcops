package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestLinuxHostKeyVerificationMigrationContract(t *testing.T) {
	t.Parallel()
	sqlBytes, err := os.ReadFile("000047_linux_host_key_verification.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(sqlBytes)
	for _, column := range []string{
		"host_key_status", "pending_host_key_algorithm", "pending_host_key_fingerprint",
		"host_key_observed_at", "host_key_confirmed_at", "host_key_confirmed_by",
	} {
		if !strings.Contains(sql, column) {
			t.Errorf("migration does not contain %s", column)
		}
	}
	for _, status := range []string{"unverified", "pending", "trusted", "mismatch"} {
		if !strings.Contains(sql, "'"+status+"'") {
			t.Errorf("migration does not constrain status %s", status)
		}
	}
}
