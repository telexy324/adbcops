package linuxserver

import (
	"context"
	"testing"
	"time"
)

func TestConnectionPoolLimitsConcurrentConnectionsPerHost(t *testing.T) {
	first, second := newToolFakeClient(), newToolFakeClient()
	dialer := &toolFakeDialer{clients: []RemoteClient{first, second}}
	pool := NewConnectionPool(dialer, PoolOptions{PerHostMaxConnections: 2})
	connection := confirmedConnection()
	lease1, err := pool.Acquire(context.Background(), connection, false)
	if err != nil {
		t.Fatal(err)
	}
	lease2, err := pool.Acquire(context.Background(), connection, false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := pool.Acquire(ctx, connection, false); err != context.DeadlineExceeded {
		t.Fatalf("third Acquire() error = %v, want deadline exceeded", err)
	}
	lease1.Release(false)
	lease2.Release(false)
	lease3, err := pool.Acquire(context.Background(), connection, false)
	if err != nil {
		t.Fatalf("Acquire(after release) error = %v", err)
	}
	lease3.Release(false)
	if dialer.calls != 2 {
		t.Fatalf("dial calls = %d, want 2", dialer.calls)
	}
}

func TestConnectionPoolDoesNotReuseAcrossCredentialVersion(t *testing.T) {
	oldClient, newClient := newToolFakeClient(), newToolFakeClient()
	dialer := &toolFakeDialer{clients: []RemoteClient{oldClient, newClient}}
	pool := NewConnectionPool(dialer, PoolOptions{PerHostMaxConnections: 1})
	oldConnection := confirmedConnection()
	oldConnection.CredentialVersion = "v1"
	lease, err := pool.Acquire(context.Background(), oldConnection, false)
	if err != nil {
		t.Fatal(err)
	}
	lease.Release(false)
	newConnection := oldConnection
	newConnection.CredentialVersion = "v2"
	lease, err = pool.Acquire(context.Background(), newConnection, false)
	if err != nil {
		t.Fatal(err)
	}
	lease.Release(false)
	if !oldClient.closed || dialer.calls != 2 {
		t.Fatalf("old closed = %v, dial calls = %d", oldClient.closed, dialer.calls)
	}
}
