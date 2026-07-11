package config

import (
	"net/url"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("APP_ENV", "")
	t.Setenv("APP_PORT", "")
	t.Setenv("APP_TIMEZONE", "")
	setDatabaseEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Environment != defaultEnvironment {
		t.Fatalf("Environment = %q, want %q", cfg.Environment, defaultEnvironment)
	}
	if cfg.Port != defaultPort {
		t.Fatalf("Port = %d, want %d", cfg.Port, defaultPort)
	}
	if cfg.Timezone != defaultTimezone {
		t.Fatalf("Timezone = %q, want %q", cfg.Timezone, defaultTimezone)
	}
	if cfg.Address() != ":8080" {
		t.Fatalf("Address() = %q, want %q", cfg.Address(), ":8080")
	}
	if cfg.FileStorage.LocalFileDir != defaultLocalFileDir || cfg.FileStorage.MaxUploadBytes != defaultMaxUploadBytes {
		t.Fatalf("FileStorage = %+v", cfg.FileStorage)
	}
	if cfg.RAG.ChunkSize != defaultRAGChunkSize || cfg.RAG.ChunkOverlap != defaultRAGChunkOverlap {
		t.Fatalf("RAG = %+v", cfg.RAG)
	}
}

func TestLoadFromEnvironment(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("APP_PORT", "9090")
	t.Setenv("APP_TIMEZONE", "UTC")
	t.Setenv("DB_HOST", "db.internal")
	t.Setenv("DB_PORT", "5433")
	t.Setenv("DB_USER", "aiops-user")
	t.Setenv("DB_PASSWORD", "p@ss:/?#[]")
	t.Setenv("DB_NAME", "aiops-test")
	t.Setenv("DB_SSLMODE", "require")
	t.Setenv("LOCAL_FILE_DIR", "/tmp/adbcops-test-uploads")
	t.Setenv("MAX_UPLOAD_BYTES", "1024")
	t.Setenv("RAG_CHUNK_SIZE", "120")
	t.Setenv("RAG_CHUNK_OVERLAP", "20")
	setAuthEnv(t)
	setCredentialEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Environment != "test" || cfg.Port != 9090 || cfg.Timezone != "UTC" {
		t.Fatal("Load() did not return the configured application values")
	}
	parsedDSN, err := url.Parse(cfg.Database.DSN())
	if err != nil {
		t.Fatalf("parse database DSN: %v", err)
	}
	password, ok := parsedDSN.User.Password()
	if !ok || password != "p@ss:/?#[]" {
		t.Fatal("database DSN did not safely encode the password")
	}
	if parsedDSN.Host != "db.internal:5433" || parsedDSN.Path != "/aiops-test" {
		t.Fatalf("database DSN target = %q%q", parsedDSN.Host, parsedDSN.Path)
	}
	if cfg.FileStorage.LocalFileDir != "/tmp/adbcops-test-uploads" || cfg.FileStorage.MaxUploadBytes != 1024 {
		t.Fatalf("FileStorage = %+v", cfg.FileStorage)
	}
	if cfg.RAG.ChunkSize != 120 || cfg.RAG.ChunkOverlap != 20 {
		t.Fatalf("RAG = %+v", cfg.RAG)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name     string
		port     string
		timezone string
	}{
		{name: "non numeric port", port: "http", timezone: "UTC"},
		{name: "port out of range", port: "65536", timezone: "UTC"},
		{name: "invalid timezone", port: "8080", timezone: "Mars/Olympus"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setDatabaseEnv(t)
			t.Setenv("APP_PORT", tt.port)
			t.Setenv("APP_TIMEZONE", tt.timezone)
			if _, err := Load(); err == nil {
				t.Fatal("Load() error = nil, want error")
			}
		})
	}
}

func TestLoadRejectsInvalidDatabaseConfig(t *testing.T) {
	tests := []struct {
		name     string
		password string
		port     string
		sslMode  string
	}{
		{name: "missing password", password: "", port: "5432", sslMode: "disable"},
		{name: "invalid port", password: "test-password", port: "70000", sslMode: "disable"},
		{name: "invalid ssl mode", password: "test-password", port: "5432", sslMode: "sometimes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("APP_TIMEZONE", "UTC")
			t.Setenv("DB_PASSWORD", tt.password)
			t.Setenv("DB_PORT", tt.port)
			t.Setenv("DB_SSLMODE", tt.sslMode)
			if _, err := Load(); err == nil {
				t.Fatal("Load() error = nil, want error")
			}
		})
	}
}

func TestLoadRejectsInvalidAuthConfig(t *testing.T) {
	tests := []struct {
		name          string
		jwtSecret     string
		expiry        string
		adminUsername string
		adminPassword string
	}{
		{name: "short JWT secret", jwtSecret: "short", expiry: "12", adminUsername: "admin", adminPassword: "test-admin-password"},
		{name: "invalid expiry", jwtSecret: "test-jwt-secret-with-at-least-32-characters", expiry: "0", adminUsername: "admin", adminPassword: "test-admin-password"},
		{name: "missing admin username", jwtSecret: "test-jwt-secret-with-at-least-32-characters", expiry: "12", adminUsername: "", adminPassword: "test-admin-password"},
		{name: "short admin password", jwtSecret: "test-jwt-secret-with-at-least-32-characters", expiry: "12", adminUsername: "admin", adminPassword: "short"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setDatabaseEnv(t)
			t.Setenv("JWT_SECRET", tt.jwtSecret)
			t.Setenv("JWT_EXPIRE_HOURS", tt.expiry)
			t.Setenv("INITIAL_ADMIN_USERNAME", tt.adminUsername)
			t.Setenv("INITIAL_ADMIN_PASSWORD", tt.adminPassword)
			if _, err := Load(); err == nil {
				t.Fatal("Load() error = nil, want error")
			}
		})
	}
}

func setDatabaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DB_HOST", "")
	t.Setenv("DB_PORT", "")
	t.Setenv("DB_USER", "")
	t.Setenv("DB_PASSWORD", "test-only-password")
	t.Setenv("DB_NAME", "")
	t.Setenv("DB_SSLMODE", "")
	setAuthEnv(t)
}

func setAuthEnv(t *testing.T) {
	t.Helper()
	t.Setenv("JWT_SECRET", "test-jwt-secret-with-at-least-32-characters")
	t.Setenv("JWT_EXPIRE_HOURS", "")
	t.Setenv("INITIAL_ADMIN_USERNAME", "admin")
	t.Setenv("INITIAL_ADMIN_PASSWORD", "test-admin-password")
	setCredentialEnv(t)
}

func setCredentialEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CREDENTIAL_MASTER_KEY", "test-credential-master-key-32-bytes")
	t.Setenv("CREDENTIAL_KEY_VERSION", "v1")
}
