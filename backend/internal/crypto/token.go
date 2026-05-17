// Package crypto wraps the stdlib AEAD primitives we use for at-rest secret
// storage. Today's only caller is the Plaid integration (access_tokens —
// ADR-0010), but the API is intentionally provider-agnostic so future
// surfaces (refresh tokens, AI provider keys) can reuse it.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

// versionV1 prefixes the stored bytes so a future scheme rotation can decrypt
// old rows without a flag-day migration. Increment only when changing the
// cipher / nonce length / key size.
const versionV1 byte = 0x01

// SecretBox encrypts and decrypts byte slices with AES-256-GCM. Construct
// once at startup with the raw 32-byte key from config; share the value
// across goroutines (cipher.AEAD is safe for concurrent use).
type SecretBox struct {
	aead cipher.AEAD
}

// NewSecretBox builds a SecretBox from a 32-byte AES-256 key. Returns an
// error if the key is the wrong length so misconfiguration fails at startup,
// not on the first encrypt call.
func NewSecretBox(key []byte) (*SecretBox, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("crypto: key must be 32 bytes (AES-256), got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: aes.NewCipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: cipher.NewGCM: %w", err)
	}
	return &SecretBox{aead: aead}, nil
}

// Encrypt returns version || nonce || ciphertext||tag. Each call uses a fresh
// random nonce — nonce reuse with the same key in GCM is a key-compromise
// event, so we never accept a caller-supplied nonce.
func (s *SecretBox) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("crypto: read nonce: %w", err)
	}
	// 1 (version) + nonce + ciphertext + tag. Sealing in place into the
	// suffix avoids one allocation on the hot path.
	out := make([]byte, 1+len(nonce), 1+len(nonce)+len(plaintext)+s.aead.Overhead())
	out[0] = versionV1
	copy(out[1:], nonce)
	out = s.aead.Seal(out, nonce, plaintext, nil)
	return out, nil
}

// Decrypt is the inverse. Validates the version byte and authenticates the
// ciphertext; a tampered or truncated input returns an error rather than
// garbage plaintext.
func (s *SecretBox) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < 1+s.aead.NonceSize()+s.aead.Overhead() {
		return nil, errors.New("crypto: ciphertext too short")
	}
	if ciphertext[0] != versionV1 {
		return nil, fmt.Errorf("crypto: unknown ciphertext version 0x%02x", ciphertext[0])
	}
	nonce := ciphertext[1 : 1+s.aead.NonceSize()]
	body := ciphertext[1+s.aead.NonceSize():]
	out, err := s.aead.Open(nil, nonce, body, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: open: %w", err)
	}
	return out, nil
}
