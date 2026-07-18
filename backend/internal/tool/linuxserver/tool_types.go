package linuxserver

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const (
	LinuxAuthPassword   = "password"
	LinuxAuthPrivateKey = "private_key"

	HostKeyStrict             = "strict"
	HostKeyTrustOnFirstUse    = "trust_on_first_use"
	HostKeyInsecureSkipVerify = "insecure_skip_verify"

	ErrorDNSFailed         = "DNS_FAILED"
	ErrorConnectTimeout    = "CONNECT_TIMEOUT"
	ErrorConnectionRefused = "CONNECTION_REFUSED"
	ErrorHostKeyMismatch   = "HOST_KEY_MISMATCH"
	ErrorAuthFailed        = "AUTH_FAILED"
	ErrorPermissionDenied  = "PERMISSION_DENIED"
	ErrorCommandNotFound   = "COMMAND_NOT_FOUND"
	ErrorCommandTimeout    = "COMMAND_TIMEOUT"
	ErrorUnsupportedOS     = "UNSUPPORTED_OS"
	ErrorUnknown           = "UNKNOWN"
)

var (
	ErrInvalidConnection           = errors.New("invalid linux server connection")
	ErrAuthenticationFailed        = errors.New("SSH authentication failed")
	ErrHostKeyMismatch             = errors.New("SSH host key mismatch")
	ErrHostKeyConfirmationRequired = errors.New("SSH host key confirmation is required")
	ErrInsecureHostKeyPolicy       = errors.New("insecure SSH host key policy is disabled")
	ErrConnectionPoolClosed        = errors.New("SSH connection pool is closed")
)

type LinuxServerConnection struct {
	Host                  string `json:"host"`
	Port                  int    `json:"port"`
	Username              string `json:"username"`
	AuthType              string `json:"authType"`
	Password              string `json:"-"`
	PrivateKey            string `json:"-"`
	PrivateKeyPassword    string `json:"-"`
	HostKeyPolicy         string `json:"hostKeyPolicy"`
	HostKeyAlgorithm      string `json:"hostKeyAlgorithm,omitempty"`
	HostKeyFingerprint    string `json:"hostKeyFingerprint,omitempty"`
	CredentialVersion     string `json:"-"`
	ConnectTimeoutSeconds int    `json:"connectTimeoutSeconds,omitempty"`
	MaxConcurrentCommands int    `json:"maxConcurrentCommands,omitempty"`
}

type LinuxCollectRequest struct {
	HostID     int64           `json:"hostId"`
	Collector  string          `json:"collector"`
	Parameters json.RawMessage `json:"parameters,omitempty"`
	TimeStart  *time.Time      `json:"timeStart,omitempty"`
	TimeEnd    *time.Time      `json:"timeEnd,omitempty"`
}

type LinuxCollectResult struct {
	Collector      string          `json:"collector"`
	CommandVersion string          `json:"commandVersion"`
	Status         string          `json:"status"`
	Data           json.RawMessage `json:"data"`
	Warnings       []string        `json:"warnings,omitempty"`
	CollectedAt    time.Time       `json:"collectedAt"`
	DurationMs     int64           `json:"durationMs"`
	Truncated      bool            `json:"truncated"`
}

type LinuxConnectionTestResult struct {
	Status               string `json:"status"`
	LatencyMs            int64  `json:"latencyMs"`
	ServerVersion        string `json:"serverVersion,omitempty"`
	AuthMethod           string `json:"authMethod"`
	HostKeyAlgorithm     string `json:"hostKeyAlgorithm,omitempty"`
	HostKeyFingerprint   string `json:"hostKeyFingerprint,omitempty"`
	ConfirmationRequired bool   `json:"confirmationRequired"`
	ErrorCode            string `json:"errorCode,omitempty"`
	Message              string `json:"message,omitempty"`
}

type LinuxServerTool interface {
	Test(ctx context.Context, conn LinuxServerConnection) (*LinuxConnectionTestResult, error)
	DetectPlatform(ctx context.Context, conn LinuxServerConnection) (*LinuxPlatformInfo, error)
	Collect(ctx context.Context, conn LinuxServerConnection, req LinuxCollectRequest) (*LinuxCollectResult, error)
}

type HostKeyObservation struct {
	Algorithm   string
	Fingerprint string
}
