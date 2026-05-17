package crypto_test

import (
	"bytes"
	"testing"

	"github.com/gregwym/offbook/backend/internal/crypto"
)

func makeKey() []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = byte(i)
	}
	return k
}

func TestSecretBox_RoundTrip(t *testing.T) {
	box, err := crypto.NewSecretBox(makeKey())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	cases := [][]byte{
		[]byte("access-sandbox-xxx"),
		{},
		bytes.Repeat([]byte("A"), 4096),
	}
	for _, pt := range cases {
		ct, err := box.Encrypt(pt)
		if err != nil {
			t.Fatalf("encrypt %d bytes: %v", len(pt), err)
		}
		out, err := box.Decrypt(ct)
		if err != nil {
			t.Fatalf("decrypt %d bytes: %v", len(pt), err)
		}
		if !bytes.Equal(out, pt) {
			t.Fatalf("round-trip mismatch: got %q want %q", out, pt)
		}
	}
}

func TestSecretBox_NonceRandomized(t *testing.T) {
	// Same plaintext + same key MUST produce different ciphertexts every call
	// (or nonce reuse would silently break GCM security).
	box, err := crypto.NewSecretBox(makeKey())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	pt := []byte("plaid-access-token")
	a, err := box.Encrypt(pt)
	if err != nil {
		t.Fatalf("encrypt a: %v", err)
	}
	b, err := box.Encrypt(pt)
	if err != nil {
		t.Fatalf("encrypt b: %v", err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("two encryptions of the same plaintext produced identical ciphertext — nonce reuse")
	}
}

func TestSecretBox_TamperingDetected(t *testing.T) {
	box, err := crypto.NewSecretBox(makeKey())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ct, err := box.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	// Flip a byte deep in the ciphertext body — GCM should refuse to open.
	ct[len(ct)-1] ^= 0xFF
	if _, err := box.Decrypt(ct); err == nil {
		t.Fatal("decrypt of tampered ciphertext succeeded; AEAD broken")
	}
}

func TestSecretBox_RejectsUnknownVersion(t *testing.T) {
	box, err := crypto.NewSecretBox(makeKey())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ct, err := box.Encrypt([]byte("x"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	ct[0] = 0xFE // pretend it's a future scheme
	if _, err := box.Decrypt(ct); err == nil {
		t.Fatal("decrypt of unknown-version ciphertext succeeded")
	}
}

func TestNewSecretBox_RejectsBadKey(t *testing.T) {
	if _, err := crypto.NewSecretBox(make([]byte, 16)); err == nil {
		t.Fatal("expected error for 16-byte key")
	}
	if _, err := crypto.NewSecretBox(nil); err == nil {
		t.Fatal("expected error for nil key")
	}
}
