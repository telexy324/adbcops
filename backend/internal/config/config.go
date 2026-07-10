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
	defaultEnvironment = "dev"
	defaultPort        = 8080
	defaultTimezone    = "Asia/Shanghai"
	defaultDBHost      = "127.0.0.1"
	defaultDBPort      = 5432
	defaultDBUser      = "postgres"
	defaultDBName      = "aiops"
	defaultDBSSLMode   = "disable"
	defaultJWTExpiry   = 12
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
	Environment string
	Port        int
	Timezone    string
	Database    DatabaseConfig
	Auth        AuthConfig
}

// Load reads configuration from environment variables and validates it.
func Load() (Config, error) {
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

	return Config{
		Environment: valueOrDefault(os.Getenv("APP_ENV"), defaultEnvironment),
		Port:        port,
		Timezone:    timezone,
		Database:    database,
		Auth:        auth,
	}, nil
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
