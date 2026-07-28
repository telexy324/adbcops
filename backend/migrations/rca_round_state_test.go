package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestRCARoundStateMigrationContractAndRollback(t *testing.T) {
	t.Parallel()
	upBytes, err := os.ReadFile("000050_rca_round_state.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	downBytes, err := os.ReadFile("000050_rca_round_state.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := string(upBytes)
	down := string(downBytes)
	for _, table := range []string{"rca_run", "rca_round", "rca_action", "rca_root_cause_candidate"} {
		if !strings.Contains(up, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Errorf("up migration does not create %s", table)
		}
		if !strings.Contains(down, "DROP TABLE IF EXISTS "+table) {
			t.Errorf("down migration does not drop %s", table)
		}
	}
	for _, column := range []string{
		"rca_run_id", "rca_round_id", "rca_action_id", "evidence_kind", "entity",
		"window_start", "window_end", "source_skill", "data_source_id", "owner_user_id",
	} {
		if !strings.Contains(up, "ADD COLUMN IF NOT EXISTS "+column) {
			t.Errorf("up migration does not add evidence.%s", column)
		}
		if !strings.Contains(down, "DROP COLUMN IF EXISTS "+column) {
			t.Errorf("down migration does not remove evidence.%s", column)
		}
	}
	if !strings.Contains(up, "jsonb_array_length(evidence_ids) > 0") {
		t.Error("root cause candidate evidence constraint is missing")
	}
	if !strings.Contains(up, "'partial_success'") {
		t.Error("partial_success status is missing")
	}
}
