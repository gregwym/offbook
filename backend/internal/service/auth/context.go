// Package auth owns sessions, password hashing, and the session middleware
// that gates /api/v1/*. The user_id that downstream services rely on flows
// through context.Context — never through request bodies or query strings.
package auth

import "context"

type ctxKey int

const (
	userIDKey ctxKey = iota
	userKey
)

// WithUser stores a fully-loaded user on the context. Used by middleware.
func WithUser(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// UserID extracts the authenticated user_id from ctx. Returns (0, false) if
// no session is bound. Services should treat that as a programmer error —
// the middleware should have rejected the request before reaching them.
func UserID(ctx context.Context) (int64, bool) {
	v, ok := ctx.Value(userIDKey).(int64)
	return v, ok
}

// MustUserID panics if no user_id is bound. Use only inside handlers/services
// that are guaranteed to run behind RequireSession middleware.
func MustUserID(ctx context.Context) int64 {
	id, ok := UserID(ctx)
	if !ok {
		panic("auth: no user_id in context (middleware misconfigured?)")
	}
	return id
}
