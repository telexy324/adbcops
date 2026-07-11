package credential

import (
	"strings"
	"testing"
)

func TestManagerEncryptDecrypt(t *testing.T) {
	manager, err := NewManager("test-credential-master-key-32-bytes", "v1")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	encrypted, err := manager.Encrypt("secret-api-key")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if strings.Contains(encrypted, "secret-api-key") {
		t.Fatal("encrypted credential contains plaintext")
	}
	decrypted, err := manager.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if decrypted != "secret-api-key" {
		t.Fatalf("decrypted = %q", decrypted)
	}
}
