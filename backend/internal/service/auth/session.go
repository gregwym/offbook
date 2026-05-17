package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

// SessionCookieName is the cookie that carries the opaque session token.
const SessionCookieName = "offbook_session"

// SessionTTL is how long an issued session remains valid. Sliding extension
// happens on every authenticated request via Touch.
const SessionTTL = 30 * 24 * time.Hour

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionExpired  = errors.New("session expired")
)

// NewToken returns 32 bytes of random hex. Caller stores HashToken(token);
// the raw token only ever lives in the cookie (sessions) or in the link sent
// to the invitee (household invites).
func NewToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// HashToken returns the canonical DB-side representation of a session token:
// HMAC-SHA256(token, SESSION_SECRET). A DB dump alone cannot be replayed
// against the API without also knowing SESSION_SECRET.
func HashToken(token, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}
