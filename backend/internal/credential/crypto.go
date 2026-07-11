package credential

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var ErrInvalidCiphertext = errors.New("invalid encrypted credential")

type Manager struct {
	aead       cipher.AEAD
	keyVersion string
}

type envelope struct {
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
	KeyVersion string `json:"key_version"`
}

func NewManager(masterKey, keyVersion string) (*Manager, error) {
	if len(masterKey) < 32 {
		return nil, fmt.Errorf("credential master key must contain at least 32 characters")
	}
	if keyVersion == "" {
		return nil, fmt.Errorf("credential key version is required")
	}
	digest := sha256.Sum256([]byte(masterKey))
	block, err := aes.NewCipher(digest[:])
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create AES-GCM cipher: %w", err)
	}
	return &Manager{aead: aead, keyVersion: keyVersion}, nil
}

func (m *Manager) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, m.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("create nonce: %w", err)
	}
	ciphertext := m.aead.Seal(nil, nonce, []byte(plaintext), []byte(m.keyVersion))
	payload, err := json.Marshal(envelope{
		Nonce:      base64.RawURLEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawURLEncoding.EncodeToString(ciphertext),
		KeyVersion: m.keyVersion,
	})
	if err != nil {
		return "", fmt.Errorf("encode encrypted credential: %w", err)
	}
	return string(payload), nil
}

func (m *Manager) Decrypt(value string) (string, error) {
	var payload envelope
	if err := json.Unmarshal([]byte(value), &payload); err != nil {
		return "", ErrInvalidCiphertext
	}
	if payload.KeyVersion == "" || payload.Nonce == "" || payload.Ciphertext == "" {
		return "", ErrInvalidCiphertext
	}
	nonce, err := base64.RawURLEncoding.DecodeString(payload.Nonce)
	if err != nil {
		return "", ErrInvalidCiphertext
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(payload.Ciphertext)
	if err != nil {
		return "", ErrInvalidCiphertext
	}
	plaintext, err := m.aead.Open(nil, nonce, ciphertext, []byte(payload.KeyVersion))
	if err != nil {
		return "", ErrInvalidCiphertext
	}
	return string(plaintext), nil
}
