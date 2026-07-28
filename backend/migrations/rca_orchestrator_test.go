package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestRCAOrchestratorStopReasonMigrationAndRollback(t *testing.T) {
	t.Parallel()
	up, err := os.ReadFile("000051_rca_orchestrator_stop_reason.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("000051_rca_orchestrator_stop_reason.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(up), "ADD COLUMN IF NOT EXISTS stop_reason") {
		t.Fatal("up migration does not add rca_run.stop_reason")
	}
	if !strings.Contains(string(down), "DROP COLUMN IF EXISTS stop_reason") {
		t.Fatal("down migration does not remove rca_run.stop_reason")
	}
}
