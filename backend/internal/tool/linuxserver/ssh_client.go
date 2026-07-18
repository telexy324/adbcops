package linuxserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type RemoteClient interface {
	CommandRunner
	ServerVersion() string
	HostKey() HostKeyObservation
	Close() error
}

type SSHDialer interface {
	Dial(ctx context.Context, conn LinuxServerConnection, allowUnconfirmedHostKey bool) (RemoteClient, error)
}

type RealSSHDialer struct{}

func (RealSSHDialer) Dial(ctx context.Context, conn LinuxServerConnection, allowUnconfirmedHostKey bool) (RemoteClient, error) {
	conn, err := normalizeConnection(conn, allowUnconfirmedHostKey)
	if err != nil {
		return nil, err
	}
	auth, err := sshAuthMethod(conn)
	if err != nil {
		return nil, err
	}
	var observation HostKeyObservation
	hostKeyCallback := func(_ string, _ net.Addr, key ssh.PublicKey) error {
		observation = HostKeyObservation{Algorithm: key.Type(), Fingerprint: ssh.FingerprintSHA256(key)}
		if conn.HostKeyAlgorithm != "" && conn.HostKeyAlgorithm != observation.Algorithm {
			return ErrHostKeyMismatch
		}
		if conn.HostKeyFingerprint != "" && conn.HostKeyFingerprint != observation.Fingerprint {
			return ErrHostKeyMismatch
		}
		if conn.HostKeyFingerprint == "" && !allowUnconfirmedHostKey {
			return ErrHostKeyConfirmationRequired
		}
		return nil
	}
	timeout := time.Duration(conn.ConnectTimeoutSeconds) * time.Second
	dialer := net.Dialer{Timeout: timeout}
	address := net.JoinHostPort(conn.Host, strconv.Itoa(conn.Port))
	networkConnection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, classifyDialError(err)
	}
	deadline := time.Now().Add(timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = networkConnection.SetDeadline(deadline)
	config := &ssh.ClientConfig{
		User: conn.Username, Auth: []ssh.AuthMethod{auth}, HostKeyCallback: hostKeyCallback,
		Timeout: timeout,
	}
	clientConnection, channels, requests, err := ssh.NewClientConn(networkConnection, address, config)
	if err != nil {
		_ = networkConnection.Close()
		return nil, classifySSHError(err)
	}
	_ = networkConnection.SetDeadline(time.Time{})
	client := ssh.NewClient(clientConnection, channels, requests)
	return &realSSHClient{client: client, serverVersion: string(clientConnection.ServerVersion()), hostKey: observation}, nil
}

func normalizeConnection(conn LinuxServerConnection, allowUnconfirmedHostKey bool) (LinuxServerConnection, error) {
	conn.Host = strings.TrimSpace(conn.Host)
	conn.Username = strings.TrimSpace(conn.Username)
	conn.HostKeyPolicy = strings.TrimSpace(conn.HostKeyPolicy)
	conn.HostKeyAlgorithm = strings.TrimSpace(conn.HostKeyAlgorithm)
	conn.HostKeyFingerprint = strings.TrimSpace(conn.HostKeyFingerprint)
	if conn.Port == 0 {
		conn.Port = 22
	}
	if conn.ConnectTimeoutSeconds == 0 {
		conn.ConnectTimeoutSeconds = 10
	}
	if conn.Host == "" || len(conn.Host) > 255 || conn.Username == "" || conn.Port < 1 || conn.Port > 65535 ||
		conn.ConnectTimeoutSeconds < 1 || conn.ConnectTimeoutSeconds > 60 {
		return LinuxServerConnection{}, ErrInvalidConnection
	}
	if conn.HostKeyPolicy == "" {
		conn.HostKeyPolicy = HostKeyStrict
	}
	if conn.HostKeyPolicy == HostKeyInsecureSkipVerify {
		return LinuxServerConnection{}, ErrInsecureHostKeyPolicy
	}
	if conn.HostKeyPolicy != HostKeyStrict && conn.HostKeyPolicy != HostKeyTrustOnFirstUse {
		return LinuxServerConnection{}, ErrInvalidConnection
	}
	if !allowUnconfirmedHostKey && conn.HostKeyFingerprint == "" {
		return LinuxServerConnection{}, ErrHostKeyConfirmationRequired
	}
	switch conn.AuthType {
	case LinuxAuthPassword:
		if conn.Password == "" || conn.PrivateKey != "" || conn.PrivateKeyPassword != "" {
			return LinuxServerConnection{}, ErrInvalidConnection
		}
	case LinuxAuthPrivateKey:
		if conn.PrivateKey == "" || conn.Password != "" {
			return LinuxServerConnection{}, ErrInvalidConnection
		}
	default:
		return LinuxServerConnection{}, ErrInvalidConnection
	}
	return conn, nil
}

func sshAuthMethod(conn LinuxServerConnection) (ssh.AuthMethod, error) {
	if conn.AuthType == LinuxAuthPassword {
		return ssh.Password(conn.Password), nil
	}
	var signer ssh.Signer
	var err error
	if conn.PrivateKeyPassword != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(conn.PrivateKey), []byte(conn.PrivateKeyPassword))
	} else {
		signer, err = ssh.ParsePrivateKey([]byte(conn.PrivateKey))
	}
	if err != nil {
		return nil, fmt.Errorf("parse SSH private key: %w", ErrInvalidConnection)
	}
	return ssh.PublicKeys(signer), nil
}

type realSSHClient struct {
	client        *ssh.Client
	serverVersion string
	hostKey       HostKeyObservation
	closeOnce     sync.Once
	closeErr      error
}

func (c *realSSHClient) Run(ctx context.Context, executable string, args []string, stdout, stderr io.Writer) error {
	session, err := c.client.NewSession()
	if err != nil {
		return fmt.Errorf("create SSH session: %w", err)
	}
	defer session.Close()
	session.Stdout = stdout
	session.Stderr = stderr
	command := joinRemoteCommand(executable, args)
	if err := session.Start(command); err != nil {
		return classifyCommandError(err)
	}
	wait := make(chan error, 1)
	go func() { wait <- session.Wait() }()
	select {
	case err := <-wait:
		return classifyCommandError(err)
	case <-ctx.Done():
		_ = session.Close()
		<-wait
		return ctx.Err()
	}
}

func (c *realSSHClient) ServerVersion() string       { return c.serverVersion }
func (c *realSSHClient) HostKey() HostKeyObservation { return c.hostKey }
func (c *realSSHClient) Close() error {
	c.closeOnce.Do(func() { c.closeErr = c.client.Close() })
	return c.closeErr
}

// joinRemoteCommand encodes the already validated executable and argv as
// single-quoted SSH command protocol words. No user-provided shell program or
// raw command string is accepted by this package.
func joinRemoteCommand(executable string, args []string) string {
	words := make([]string, 0, len(args)+1)
	words = append(words, quoteSSHWord(executable))
	for _, argument := range args {
		words = append(words, quoteSSHWord(argument))
	}
	return strings.Join(words, " ")
}

func quoteSSHWord(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func classifyCommandError(err error) error {
	if err == nil {
		return nil
	}
	var exitError *ssh.ExitError
	if errors.As(err, &exitError) {
		switch exitError.ExitStatus() {
		case 126:
			return ErrRunnerPermission
		case 127:
			return ErrRunnerCommandNotFound
		}
	}
	return err
}

func classifySSHError(err error) error {
	if errors.Is(err, ErrHostKeyMismatch) || strings.Contains(err.Error(), ErrHostKeyMismatch.Error()) {
		return ErrHostKeyMismatch
	}
	if errors.Is(err, ErrHostKeyConfirmationRequired) || strings.Contains(err.Error(), ErrHostKeyConfirmationRequired.Error()) {
		return ErrHostKeyConfirmationRequired
	}
	var authError *ssh.ServerAuthError
	if errors.As(err, &authError) || strings.Contains(strings.ToLower(err.Error()), "unable to authenticate") {
		return ErrAuthenticationFailed
	}
	return err
}

func classifyDialError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w", ErrorConnectTimeout, err)
	}
	var dnsError *net.DNSError
	if errors.As(err, &dnsError) {
		return fmt.Errorf("%s: %w", ErrorDNSFailed, err)
	}
	var netError net.Error
	if errors.As(err, &netError) && netError.Timeout() {
		return fmt.Errorf("%s: %w", ErrorConnectTimeout, err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "connection refused") {
		return fmt.Errorf("%s: %w", ErrorConnectionRefused, err)
	}
	return err
}
