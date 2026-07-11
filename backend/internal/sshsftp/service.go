package sshsftp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"aiops-platform/backend/internal/model"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const (
	defaultSSHPort       = 22
	defaultConnectTimout = 10 * time.Second
	defaultMaxBytes      = 1 << 20
	maxReadBytes         = 10 << 20
)

var (
	ErrForbidden         = errors.New("sftp access forbidden")
	ErrInvalidInput      = errors.New("invalid input")
	ErrUnsupportedSource = errors.New("unsupported sftp data source")
	ErrPathTraversal     = errors.New("path traversal is not allowed")
	ErrPathNotAllowed    = errors.New("path is outside allowlist")
	ErrSensitivePath     = errors.New("sensitive path is not allowed")
	ErrFileTooLarge      = errors.New("file too large")
)

type Repository interface {
	FindDataSourceByID(ctx context.Context, id int64) (*model.DataSource, error)
}

type SecretManager interface {
	Decrypt(value string) (string, error)
}

type Service struct {
	repository Repository
	secrets    SecretManager
	dialer     SFTPClientFactory
}

type SFTPClientFactory interface {
	Open(ctx context.Context, config ConnectionConfig) (SFTPClient, error)
}

type SFTPClient interface {
	Open(path string) (io.ReadCloser, error)
	Lstat(path string) (FileInfo, error)
	ReadLink(path string) (string, error)
	Close() error
}

type FileInfo interface {
	IsDir() bool
	Size() int64
	Mode() os.FileMode
}

type ConnectionConfig struct {
	Host       string
	Port       int
	Username   string
	Password   string
	PrivateKey string
	Passphrase string
	Timeout    time.Duration
}

type Config struct {
	Host          string   `json:"host"`
	Port          int      `json:"port"`
	Username      string   `json:"username"`
	Allowlist     []string `json:"pathAllowlist"`
	MaxBytes      int64    `json:"maxBytes"`
	ConnectTimout int      `json:"connectTimeoutMs"`
}

type Credential struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	PrivateKey string `json:"privateKey"`
	Passphrase string `json:"passphrase"`
}

type ReadInput struct {
	DataSourceID int64
	Path         string
	MaxBytes     int64
}

type ReadResult struct {
	DataSourceID int64  `json:"dataSourceId"`
	Path         string `json:"path"`
	Size         int64  `json:"size"`
	Content      string `json:"content"`
	Truncated    bool   `json:"truncated"`
}

func NewService(repository Repository, secrets SecretManager, dialer SFTPClientFactory) *Service {
	if dialer == nil {
		dialer = realSFTPDialer{}
	}
	return &Service{repository: repository, secrets: secrets, dialer: dialer}
}

func (s *Service) ReadFile(ctx context.Context, actor *model.AppUser, input ReadInput) (*ReadResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	if input.DataSourceID <= 0 {
		return nil, ErrInvalidInput
	}
	dataSource, err := s.repository.FindDataSourceByID(ctx, input.DataSourceID)
	if err != nil {
		return nil, err
	}
	if !dataSource.Enabled {
		return nil, ErrForbidden
	}
	if dataSource.SourceType != model.DataSourceTypeSSH {
		return nil, ErrUnsupportedSource
	}
	config, err := parseConfig(dataSource.Config)
	if err != nil {
		return nil, err
	}
	maxBytes := config.MaxBytes
	if input.MaxBytes > 0 && input.MaxBytes < maxBytes {
		maxBytes = input.MaxBytes
	}
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	if maxBytes > maxReadBytes {
		maxBytes = maxReadBytes
	}
	credential, err := s.loadCredential(dataSource)
	if err != nil {
		return nil, err
	}
	connection := ConnectionConfig{
		Host:       config.Host,
		Port:       config.Port,
		Username:   firstNonEmpty(credential.Username, config.Username),
		Password:   credential.Password,
		PrivateKey: credential.PrivateKey,
		Passphrase: credential.Passphrase,
		Timeout:    time.Duration(config.ConnectTimout) * time.Millisecond,
	}
	if connection.Timeout <= 0 {
		connection.Timeout = defaultConnectTimout
	}
	client, err := s.dialer.Open(ctx, connection)
	if err != nil {
		return nil, fmt.Errorf("open sftp connection: %w", err)
	}
	defer client.Close()
	cleaned, err := SecurePath(client, input.Path, config.Allowlist)
	if err != nil {
		return nil, err
	}
	info, err := client.Lstat(cleaned)
	if err != nil {
		return nil, fmt.Errorf("stat sftp file: %w", err)
	}
	if info.IsDir() {
		return nil, ErrInvalidInput
	}
	if info.Size() > maxBytes {
		return nil, ErrFileTooLarge
	}
	file, err := client.Open(cleaned)
	if err != nil {
		return nil, fmt.Errorf("open sftp file: %w", err)
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read sftp file: %w", err)
	}
	truncated := int64(len(content)) > maxBytes
	if truncated {
		content = content[:maxBytes]
	}
	if !utf8.Valid(content) {
		return nil, ErrInvalidInput
	}
	return &ReadResult{DataSourceID: dataSource.ID, Path: cleaned, Size: int64(len(content)), Content: string(content), Truncated: truncated}, nil
}

func SecurePath(client SFTPClient, rawPath string, allowlist []string) (string, error) {
	if strings.TrimSpace(rawPath) == "" || !strings.HasPrefix(rawPath, "/") {
		return "", ErrInvalidInput
	}
	if containsDotDot(rawPath) {
		return "", ErrPathTraversal
	}
	cleaned := path.Clean(rawPath)
	if isSensitivePath(cleaned) {
		return "", ErrSensitivePath
	}
	normalizedAllowlist, err := normalizeAllowlist(allowlist)
	if err != nil {
		return "", err
	}
	if !withinAllowlist(cleaned, normalizedAllowlist) {
		return "", ErrPathNotAllowed
	}
	resolved, err := resolveSymlinks(client, cleaned, normalizedAllowlist, 0)
	if err != nil {
		return "", err
	}
	if isSensitivePath(resolved) {
		return "", ErrSensitivePath
	}
	if !withinAllowlist(resolved, normalizedAllowlist) {
		return "", ErrPathNotAllowed
	}
	return resolved, nil
}

func parseConfig(raw []byte) (Config, error) {
	var config Config
	if err := json.Unmarshal(raw, &config); err != nil {
		return Config{}, ErrInvalidInput
	}
	config.Host = strings.TrimSpace(config.Host)
	if config.Host == "" || len(config.Host) > 255 {
		return Config{}, ErrInvalidInput
	}
	if config.Port == 0 {
		config.Port = defaultSSHPort
	}
	if config.Port < 1 || config.Port > 65535 {
		return Config{}, ErrInvalidInput
	}
	if config.MaxBytes <= 0 {
		config.MaxBytes = defaultMaxBytes
	}
	if len(config.Allowlist) == 0 {
		return Config{}, ErrInvalidInput
	}
	if _, err := normalizeAllowlist(config.Allowlist); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (s *Service) loadCredential(dataSource *model.DataSource) (Credential, error) {
	if dataSource.Credential == nil || dataSource.Credential.EncryptedPayload == "" || s.secrets == nil {
		return Credential{}, nil
	}
	plaintext, err := s.secrets.Decrypt(dataSource.Credential.EncryptedPayload)
	if err != nil {
		return Credential{}, fmt.Errorf("decrypt ssh credential: %w", err)
	}
	var credential Credential
	if err := json.Unmarshal([]byte(plaintext), &credential); err != nil {
		return Credential{}, ErrInvalidInput
	}
	return credential, nil
}

func normalizeAllowlist(allowlist []string) ([]string, error) {
	normalized := make([]string, 0, len(allowlist))
	for _, candidate := range allowlist {
		if strings.TrimSpace(candidate) == "" || !strings.HasPrefix(candidate, "/") || containsDotDot(candidate) {
			return nil, ErrInvalidInput
		}
		cleaned := path.Clean(candidate)
		if isSensitivePath(cleaned) {
			return nil, ErrSensitivePath
		}
		normalized = append(normalized, cleaned)
	}
	return normalized, nil
}

func containsDotDot(value string) bool {
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func isSensitivePath(value string) bool {
	cleaned := path.Clean(value)
	if cleaned == "/etc" || strings.HasPrefix(cleaned, "/etc/") ||
		cleaned == "/root" || strings.HasPrefix(cleaned, "/root/") ||
		cleaned == "/proc" || strings.HasPrefix(cleaned, "/proc/") ||
		cleaned == "/sys" || strings.HasPrefix(cleaned, "/sys/") {
		return true
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == ".ssh" {
			return true
		}
	}
	return false
}

func withinAllowlist(candidate string, allowlist []string) bool {
	for _, allowed := range allowlist {
		if candidate == allowed || strings.HasPrefix(candidate, strings.TrimRight(allowed, "/")+"/") {
			return true
		}
	}
	return false
}

func resolveSymlinks(client SFTPClient, cleaned string, allowlist []string, depth int) (string, error) {
	if depth > 16 {
		return "", ErrPathNotAllowed
	}
	parts := strings.Split(strings.TrimPrefix(cleaned, "/"), "/")
	current := "/"
	for index, part := range parts {
		if part == "" {
			continue
		}
		if current == "/" {
			current = "/" + part
		} else {
			current = current + "/" + part
		}
		info, err := client.Lstat(current)
		if err != nil {
			if index == len(parts)-1 {
				return current, nil
			}
			return "", err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		target, err := client.ReadLink(current)
		if err != nil {
			return "", err
		}
		if !strings.HasPrefix(target, "/") {
			target = path.Clean(path.Join(path.Dir(current), target))
		}
		if containsDotDot(target) || isSensitivePath(target) || !withinAllowlist(path.Clean(target), allowlist) {
			return "", ErrPathNotAllowed
		}
		remaining := strings.Join(parts[index+1:], "/")
		next := path.Clean(target)
		if remaining != "" {
			next = path.Clean(next + "/" + remaining)
		}
		return resolveSymlinks(client, next, allowlist, depth+1)
	}
	return path.Clean(current), nil
}

type realSFTPDialer struct{}

func (realSFTPDialer) Open(ctx context.Context, config ConnectionConfig) (SFTPClient, error) {
	auth := []ssh.AuthMethod{}
	if config.Password != "" {
		auth = append(auth, ssh.Password(config.Password))
	}
	if config.PrivateKey != "" {
		signer, err := parsePrivateKey(config.PrivateKey, config.Passphrase)
		if err != nil {
			return nil, err
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if config.Username == "" || len(auth) == 0 {
		return nil, ErrInvalidInput
	}
	sshConfig := &ssh.ClientConfig{
		User:            config.Username,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         config.Timeout,
	}
	address := net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
	sshClient, err := ssh.Dial("tcp", address, sshConfig)
	if err != nil {
		return nil, err
	}
	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		_ = sshClient.Close()
		return nil, err
	}
	return &realSFTPClient{sftp: sftpClient, ssh: sshClient}, nil
}

func parsePrivateKey(privateKey, passphrase string) (ssh.Signer, error) {
	if passphrase != "" {
		return ssh.ParsePrivateKeyWithPassphrase([]byte(privateKey), []byte(passphrase))
	}
	return ssh.ParsePrivateKey([]byte(privateKey))
}

type realSFTPClient struct {
	sftp *sftp.Client
	ssh  *ssh.Client
}

func (c *realSFTPClient) Open(path string) (io.ReadCloser, error) {
	return c.sftp.Open(path)
}

func (c *realSFTPClient) Lstat(path string) (FileInfo, error) {
	return c.sftp.Lstat(path)
}

func (c *realSFTPClient) ReadLink(path string) (string, error) {
	return c.sftp.ReadLink(path)
}

func (c *realSFTPClient) Close() error {
	return errors.Join(c.sftp.Close(), c.ssh.Close())
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
