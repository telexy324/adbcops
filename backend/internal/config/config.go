package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultEnvironment       = "dev"
	defaultLogLevel          = "info"
	defaultPort              = 8080
	defaultTimezone          = "Asia/Shanghai"
	defaultDBHost            = "127.0.0.1"
	defaultDBPort            = 5432
	defaultDBUser            = "postgres"
	defaultDBName            = "aiops"
	defaultDBSSLMode         = "disable"
	defaultJWTExpiry         = 12
	defaultLocalFileDir      = "./data/uploads"
	defaultMaxUploadBytes    = 52428800
	defaultRAGChunkSize      = 800
	defaultRAGChunkOverlap   = 100
	defaultParseMaxPages     = 1000
	defaultParseMaxBlocks    = 50000
	defaultParseTimeout      = 120
	defaultWriteTimeout      = 300
	defaultDocumentPassScore = 70
)

var allowedSSLMode = map[string]struct{}{
	"disable":     {},
	"allow":       {},
	"prefer":      {},
	"require":     {},
	"verify-ca":   {},
	"verify-full": {},
}

// DatabaseConfig contains PostgreSQL connection settings. Password must never
// be logged or serialized in API responses.
type DatabaseConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	SSLMode  string
}

// AuthConfig contains authentication secrets and bootstrap settings. Secret
// values must never be logged or returned from APIs.
type AuthConfig struct {
	JWTSecret            string
	JWTExpiry            time.Duration
	InitialAdminUsername string
	InitialAdminPassword string
}

// CredentialConfig contains encryption settings for persisted secrets.
type CredentialConfig struct {
	MasterKey  string
	KeyVersion string
}

type FileStorageConfig struct {
	LocalFileDir   string
	MaxUploadBytes int64
}

type RAGConfig struct {
	ChunkSize    int
	ChunkOverlap int
}

type KnowledgeParseConfig struct {
	MaxPages  int
	MaxBlocks int
	Timeout   time.Duration
}

type HTTPServerConfig struct {
	WriteTimeout time.Duration
}

type KnowledgeQualityConfig struct {
	DocumentPassScore int
}

// DSN returns a PostgreSQL URL. The returned value contains the database
// password and must only be passed directly to database drivers.
func (c DatabaseConfig) DSN() string {
	query := url.Values{}
	query.Set("sslmode", c.SSLMode)

	return (&url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.User, c.Password),
		Host:     net.JoinHostPort(c.Host, strconv.Itoa(c.Port)),
		Path:     c.Name,
		RawQuery: query.Encode(),
	}).String()
}

// Config contains the process-level settings needed by the HTTP server.
type Config struct {
	Environment      string
	LogLevel         string
	Port             int
	Timezone         string
	Database         DatabaseConfig
	Auth             AuthConfig
	Credential       CredentialConfig
	FileStorage      FileStorageConfig
	RAG              RAGConfig
	KnowledgeParse   KnowledgeParseConfig
	HTTPServer       HTTPServerConfig
	KnowledgeQuality KnowledgeQualityConfig
}

// Load reads configuration from environment variables and validates it.
func Load() (Config, error) {
	logLevel, err := loadLogLevel()
	if err != nil {
		return Config{}, err
	}
	port, err := loadPort(os.Getenv("APP_PORT"))
	if err != nil {
		return Config{}, err
	}

	timezone := valueOrDefault(os.Getenv("APP_TIMEZONE"), defaultTimezone)
	if _, err := time.LoadLocation(timezone); err != nil {
		return Config{}, fmt.Errorf("invalid APP_TIMEZONE %q: %w", timezone, err)
	}
	database, err := loadDatabaseConfig()
	if err != nil {
		return Config{}, err
	}
	auth, err := loadAuthConfig()
	if err != nil {
		return Config{}, err
	}
	credential, err := loadCredentialConfig()
	if err != nil {
		return Config{}, err
	}
	fileStorage, err := loadFileStorageConfig()
	if err != nil {
		return Config{}, err
	}
	rag, err := loadRAGConfig()
	if err != nil {
		return Config{}, err
	}
	knowledgeParse, err := loadKnowledgeParseConfig()
	if err != nil {
		return Config{}, err
	}
	httpServer, err := loadHTTPServerConfig()
	if err != nil {
		return Config{}, err
	}
	knowledgeQuality, err := loadKnowledgeQualityConfig()
	if err != nil {
		return Config{}, err
	}

	return Config{
		Environment:      valueOrDefault(os.Getenv("APP_ENV"), defaultEnvironment),
		LogLevel:         logLevel,
		Port:             port,
		Timezone:         timezone,
		Database:         database,
		Auth:             auth,
		Credential:       credential,
		FileStorage:      fileStorage,
		RAG:              rag,
		KnowledgeParse:   knowledgeParse,
		HTTPServer:       httpServer,
		KnowledgeQuality: knowledgeQuality,
	}, nil
}

func loadLogLevel() (string, error) {
	level := strings.ToLower(strings.TrimSpace(valueOrDefault(os.Getenv("LOG_LEVEL"), defaultLogLevel)))
	switch level {
	case "debug", "info", "warn", "error":
		return level, nil
	default:
		return "", fmt.Errorf("invalid LOG_LEVEL %q: must be debug, info, warn, or error", level)
	}
}

func loadKnowledgeQualityConfig() (KnowledgeQualityConfig, error) {
	passScore, err := loadPositiveInt("KNOWLEDGE_DOCUMENT_PASS_SCORE", defaultDocumentPassScore, 100)
	if err != nil {
		return KnowledgeQualityConfig{}, err
	}
	return KnowledgeQualityConfig{DocumentPassScore: passScore}, nil
}

func loadHTTPServerConfig() (HTTPServerConfig, error) {
	writeTimeoutSeconds, err := loadPositiveInt("HTTP_SERVER_WRITE_TIMEOUT_SECONDS", defaultWriteTimeout, 3600)
	if err != nil {
		return HTTPServerConfig{}, err
	}
	return HTTPServerConfig{WriteTimeout: time.Duration(writeTimeoutSeconds) * time.Second}, nil
}

func loadKnowledgeParseConfig() (KnowledgeParseConfig, error) {
	maxPages, err := loadPositiveInt("KNOWLEDGE_PARSE_MAX_PAGES", defaultParseMaxPages, 100000)
	if err != nil {
		return KnowledgeParseConfig{}, err
	}
	maxBlocks, err := loadPositiveInt("KNOWLEDGE_PARSE_MAX_BLOCKS", defaultParseMaxBlocks, 1000000)
	if err != nil {
		return KnowledgeParseConfig{}, err
	}
	timeoutSeconds, err := loadPositiveInt("KNOWLEDGE_PARSE_TIMEOUT_SECONDS", defaultParseTimeout, 3600)
	if err != nil {
		return KnowledgeParseConfig{}, err
	}
	return KnowledgeParseConfig{MaxPages: maxPages, MaxBlocks: maxBlocks, Timeout: time.Duration(timeoutSeconds) * time.Second}, nil
}

func loadPositiveInt(name string, fallback, maximum int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 1 || parsed > maximum {
		return 0, fmt.Errorf("invalid %s %q: must be from 1 to %d", name, raw, maximum)
	}
	return parsed, nil
}

func loadAuthConfig() (AuthConfig, error) {
	jwtSecret := os.Getenv("JWT_SECRET")
	if len(jwtSecret) < 32 {
		return AuthConfig{}, fmt.Errorf("JWT_SECRET must contain at least 32 characters")
	}

	expiryHours := defaultJWTExpiry
	if raw := strings.TrimSpace(os.Getenv("JWT_EXPIRE_HOURS")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 168 {
			return AuthConfig{}, fmt.Errorf("invalid JWT_EXPIRE_HOURS %q: must be from 1 to 168", raw)
		}
		expiryHours = parsed
	}

	username := strings.TrimSpace(os.Getenv("INITIAL_ADMIN_USERNAME"))
	if username == "" || len(username) > 100 {
		return AuthConfig{}, fmt.Errorf("INITIAL_ADMIN_USERNAME is required and must not exceed 100 characters")
	}
	password := os.Getenv("INITIAL_ADMIN_PASSWORD")
	if len(password) < 12 || len(password) > 72 {
		return AuthConfig{}, fmt.Errorf("INITIAL_ADMIN_PASSWORD must contain 12 to 72 bytes")
	}

	return AuthConfig{
		JWTSecret:            jwtSecret,
		JWTExpiry:            time.Duration(expiryHours) * time.Hour,
		InitialAdminUsername: username,
		InitialAdminPassword: password,
	}, nil
}

func loadCredentialConfig() (CredentialConfig, error) {
	masterKey := os.Getenv("CREDENTIAL_MASTER_KEY")
	if len(masterKey) < 32 {
		return CredentialConfig{}, fmt.Errorf("CREDENTIAL_MASTER_KEY must contain at least 32 characters")
	}
	keyVersion := strings.TrimSpace(os.Getenv("CREDENTIAL_KEY_VERSION"))
	if keyVersion == "" || len(keyVersion) > 50 {
		return CredentialConfig{}, fmt.Errorf("CREDENTIAL_KEY_VERSION is required and must not exceed 50 characters")
	}
	return CredentialConfig{MasterKey: masterKey, KeyVersion: keyVersion}, nil
}

func loadFileStorageConfig() (FileStorageConfig, error) {
	localFileDir := strings.TrimSpace(os.Getenv("LOCAL_FILE_DIR"))
	if localFileDir == "" {
		localFileDir = defaultLocalFileDir
	}
	maxUploadBytes := int64(defaultMaxUploadBytes)
	if raw := strings.TrimSpace(os.Getenv("MAX_UPLOAD_BYTES")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 1 || parsed > defaultMaxUploadBytes {
			return FileStorageConfig{}, fmt.Errorf("invalid MAX_UPLOAD_BYTES %q: must be from 1 to %d", raw, defaultMaxUploadBytes)
		}
		maxUploadBytes = parsed
	}
	return FileStorageConfig{LocalFileDir: localFileDir, MaxUploadBytes: maxUploadBytes}, nil
}

func loadRAGConfig() (RAGConfig, error) {
	chunkSize := defaultRAGChunkSize
	if raw := strings.TrimSpace(os.Getenv("RAG_CHUNK_SIZE")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 50 || parsed > 10000 {
			return RAGConfig{}, fmt.Errorf("invalid RAG_CHUNK_SIZE %q: must be from 50 to 10000", raw)
		}
		chunkSize = parsed
	}
	chunkOverlap := defaultRAGChunkOverlap
	if raw := strings.TrimSpace(os.Getenv("RAG_CHUNK_OVERLAP")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 || parsed >= chunkSize {
			return RAGConfig{}, fmt.Errorf("invalid RAG_CHUNK_OVERLAP %q: must be from 0 to less than RAG_CHUNK_SIZE", raw)
		}
		chunkOverlap = parsed
	}
	return RAGConfig{ChunkSize: chunkSize, ChunkOverlap: chunkOverlap}, nil
}

// Address returns the TCP listen address for the HTTP server.
func (c Config) Address() string {
	return fmt.Sprintf(":%d", c.Port)
}

func loadPort(raw string) (int, error) {
	return loadNumericPort("APP_PORT", raw, defaultPort)
}

func loadNumericPort(name, raw string, fallback int) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}

	port, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid %s %q: must be an integer from 1 to 65535", name, raw)
	}

	return port, nil
}

func loadDatabaseConfig() (DatabaseConfig, error) {
	port, err := loadNumericPort("DB_PORT", os.Getenv("DB_PORT"), defaultDBPort)
	if err != nil {
		return DatabaseConfig{}, err
	}

	password := os.Getenv("DB_PASSWORD")
	if password == "" {
		return DatabaseConfig{}, fmt.Errorf("DB_PASSWORD is required")
	}

	sslMode := strings.ToLower(valueOrDefault(os.Getenv("DB_SSLMODE"), defaultDBSSLMode))
	if _, ok := allowedSSLMode[sslMode]; !ok {
		return DatabaseConfig{}, fmt.Errorf("invalid DB_SSLMODE %q", sslMode)
	}

	return DatabaseConfig{
		Host:     valueOrDefault(os.Getenv("DB_HOST"), defaultDBHost),
		Port:     port,
		User:     valueOrDefault(os.Getenv("DB_USER"), defaultDBUser),
		Password: password,
		Name:     valueOrDefault(os.Getenv("DB_NAME"), defaultDBName),
		SSLMode:  sslMode,
	}, nil
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
