package linuxserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"sync"
	"time"
)

type PoolOptions struct {
	PerHostMaxConnections int
	IdleTimeout           time.Duration
	MaxLifetime           time.Duration
}

type ConnectionPool struct {
	mu      sync.Mutex
	dialer  SSHDialer
	options PoolOptions
	hosts   map[string]*hostPool
	closed  bool
	now     func() time.Time
}

type hostPool struct {
	slots         chan struct{}
	credentialKey string
	idle          []idleClient
}

type idleClient struct {
	client    RemoteClient
	createdAt time.Time
	idleSince time.Time
}

type ConnectionLease struct {
	pool          *ConnectionPool
	hostKey       string
	credentialKey string
	client        RemoteClient
	created       time.Time
	once          sync.Once
}

func NewConnectionPool(dialer SSHDialer, options PoolOptions) *ConnectionPool {
	if dialer == nil {
		dialer = RealSSHDialer{}
	}
	if options.PerHostMaxConnections <= 0 {
		options.PerHostMaxConnections = 2
	}
	if options.PerHostMaxConnections > 16 {
		options.PerHostMaxConnections = 16
	}
	if options.IdleTimeout <= 0 {
		options.IdleTimeout = 30 * time.Second
	}
	if options.MaxLifetime <= 0 {
		options.MaxLifetime = 5 * time.Minute
	}
	return &ConnectionPool{dialer: dialer, options: options, hosts: map[string]*hostPool{}, now: time.Now}
}

func (p *ConnectionPool) Acquire(ctx context.Context, conn LinuxServerConnection, allowUnconfirmedHostKey bool) (*ConnectionLease, error) {
	normalized, err := normalizeConnection(conn, allowUnconfirmedHostKey)
	if err != nil {
		return nil, err
	}
	hostKey := normalized.Host + ":" + strconv.Itoa(normalized.Port)
	credentialKey := connectionCredentialKey(normalized)

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, ErrConnectionPoolClosed
	}
	host := p.hosts[hostKey]
	if host == nil {
		limit := p.options.PerHostMaxConnections
		if normalized.MaxConcurrentCommands > 0 && normalized.MaxConcurrentCommands < limit {
			limit = normalized.MaxConcurrentCommands
		}
		host = &hostPool{slots: make(chan struct{}, limit), credentialKey: credentialKey}
		p.hosts[hostKey] = host
	}
	if host.credentialKey != credentialKey {
		for _, idle := range host.idle {
			_ = idle.client.Close()
		}
		host.idle = nil
		host.credentialKey = credentialKey
	}
	p.mu.Unlock()

	select {
	case host.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	now := p.now()
	p.mu.Lock()
	for len(host.idle) > 0 {
		last := len(host.idle) - 1
		idle := host.idle[last]
		host.idle = host.idle[:last]
		if now.Sub(idle.idleSince) <= p.options.IdleTimeout && now.Sub(idle.createdAt) <= p.options.MaxLifetime {
			p.mu.Unlock()
			return &ConnectionLease{pool: p, hostKey: hostKey, credentialKey: credentialKey, client: idle.client, created: idle.createdAt}, nil
		}
		_ = idle.client.Close()
	}
	p.mu.Unlock()

	client, err := p.dialer.Dial(ctx, normalized, allowUnconfirmedHostKey)
	if err != nil {
		<-host.slots
		return nil, err
	}
	return &ConnectionLease{pool: p, hostKey: hostKey, credentialKey: credentialKey, client: client, created: now}, nil
}

func (l *ConnectionLease) Client() RemoteClient { return l.client }

func (l *ConnectionLease) Release(discard bool) {
	l.once.Do(func() {
		p := l.pool
		p.mu.Lock()
		host := p.hosts[l.hostKey]
		if host == nil || p.closed || discard || host.credentialKey != l.credentialKey || p.now().Sub(l.created) > p.options.MaxLifetime {
			p.mu.Unlock()
			_ = l.client.Close()
			if host != nil {
				<-host.slots
			}
			return
		}
		host.idle = append(host.idle, idleClient{client: l.client, createdAt: l.created, idleSince: p.now()})
		p.mu.Unlock()
		<-host.slots
	})
}

func (p *ConnectionPool) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	var clients []RemoteClient
	for _, host := range p.hosts {
		for _, idle := range host.idle {
			clients = append(clients, idle.client)
		}
		host.idle = nil
	}
	p.mu.Unlock()
	var first error
	for _, client := range clients {
		if err := client.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func connectionCredentialKey(conn LinuxServerConnection) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s",
		conn.Username, conn.AuthType, conn.CredentialVersion, conn.Password, conn.PrivateKey+"\x00"+conn.PrivateKeyPassword,
		conn.HostKeyAlgorithm, conn.HostKeyFingerprint)))
	return hex.EncodeToString(digest[:])
}
