package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestLinuxHostCoreMigrationContract(t *testing.T) {
	t.Parallel()
	sqlBytes, err := os.ReadFile("000046_linux_host_core.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(sqlBytes)
	for _, table := range []string{
		"credential_group", "linux_host_profile", "linux_host", "linux_host_group", "linux_host_group_member",
	} {
		if !strings.Contains(sql, "CREATE TABLE "+table) {
			t.Errorf("migration does not create %s", table)
		}
	}
	for _, profile := range []string{
		"generic_linux", "java_application", "nginx_server", "redis_server", "tidb_server", "nacos_server", "kubernetes_node", "database_server",
	} {
		if !strings.Contains(sql, "('"+profile+"'") {
			t.Errorf("migration does not seed %s", profile)
		}
	}
	if !strings.Contains(sql, "ON CONFLICT (name) DO NOTHING") {
		t.Error("built-in profile seed is not repeatable")
	}
	if !strings.Contains(sql, "scope - ARRAY['environments', 'systemNames']") {
		t.Error("credential group scope keys are not constrained")
	}
	if !strings.Contains(sql, "ON linux_host(environment, host, port) NULLS NOT DISTINCT") {
		t.Error("environment + host + port uniqueness is missing")
	}
	for _, sourceType := range []string{
		"elasticsearch", "opensearch", "prometheus", "kubernetes", "ssh", "http", "nacos", "redis", "tidb", "nginx", "linux_server", "linux_server_group",
	} {
		if !strings.Contains(sql, "'"+sourceType+"'") {
			t.Errorf("data_source constraint does not include %s", sourceType)
		}
	}
	for _, forbiddenColumn := range []string{"password TEXT", "private_key TEXT", "private_key_passphrase TEXT"} {
		if strings.Contains(strings.ToLower(sql), forbiddenColumn) {
			t.Errorf("migration contains plaintext credential column %q", forbiddenColumn)
		}
	}
}
