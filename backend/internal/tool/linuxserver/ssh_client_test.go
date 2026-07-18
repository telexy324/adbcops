package linuxserver

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestJoinRemoteCommandQuotesEveryCatalogWord(t *testing.T) {
	command := joinRemoteCommand("systemctl", []string{"show", "service'name", "--no-pager"})
	if command != `'systemctl' 'show' 'service'"'"'name' '--no-pager'` {
		t.Fatalf("command = %q", command)
	}
}

func TestNormalizeConnectionRejectsCredentialConflictAndUnconfirmedCollection(t *testing.T) {
	connection := testConnection("secret")
	connection.PrivateKey = "also-a-key"
	if _, err := normalizeConnection(connection, true); err != ErrInvalidConnection {
		t.Fatalf("credential conflict error = %v", err)
	}
	connection.PrivateKey = ""
	if _, err := normalizeConnection(connection, false); err != ErrHostKeyConfirmationRequired {
		t.Fatalf("unconfirmed collection error = %v", err)
	}
	connection.HostKeyPolicy = HostKeyInsecureSkipVerify
	if _, err := normalizeConnection(connection, true); err != ErrInsecureHostKeyPolicy {
		t.Fatalf("insecure policy error = %v", err)
	}
}

func TestSSHAuthMethodParsesPlainAndPassphraseProtectedPrivateKeys(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	plainBlock, err := ssh.MarshalPrivateKey(privateKey, "test")
	if err != nil {
		t.Fatal(err)
	}
	protectedBlock, err := ssh.MarshalPrivateKeyWithPassphrase(privateKey, "test", []byte("key-passphrase"))
	if err != nil {
		t.Fatal(err)
	}
	for _, connection := range []LinuxServerConnection{
		{AuthType: LinuxAuthPrivateKey, PrivateKey: string(pem.EncodeToMemory(plainBlock))},
		{AuthType: LinuxAuthPrivateKey, PrivateKey: string(pem.EncodeToMemory(protectedBlock)), PrivateKeyPassword: "key-passphrase"},
	} {
		if _, err := sshAuthMethod(connection); err != nil {
			t.Fatalf("sshAuthMethod(protected=%v) error = %v", connection.PrivateKeyPassword != "", err)
		}
	}
}
