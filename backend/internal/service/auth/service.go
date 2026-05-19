package auth

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

// Domain errors. Handlers map these to HTTP status codes.
var (
	ErrEmailRequired      = errors.New("email is required")
	ErrInvalidEmail       = errors.New("email is invalid")
	ErrPasswordTooShort   = errors.New("password must be at least 8 characters")
	ErrInvalidSignupMode  = errors.New("invalid signup_mode")
	ErrSignupClosed       = errors.New("signup is invite-only on this instance")
	ErrInstanceConfigured = errors.New("instance already configured")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidScope       = errors.New("invalid scope")
	ErrInviteRequired     = errors.New("invite_token is required")
	ErrInviteUnavailable  = errors.New("invite acceptance not configured on this instance")
)

// MinPasswordLen is intentionally modest — Offbook is a local-first app and
// users can always rotate. The bigger lever is bcryptCost in password.go.
const MinPasswordLen = 8

type SignupInput struct {
	Email    string
	Password string
}

// SignupWithInviteInput is the body for POST /auth/signup-with-invite.
// Works regardless of instance signup_mode — the invite token is the gate.
type SignupWithInviteInput struct {
	Email       string
	Password    string
	InviteToken string
}

// InviteAcceptor is the slice of household.Service the auth service needs
// to consume invites during signup. Defining it here (vs importing
// household directly) keeps the dependency arrow one-directional and lets
// tests stub the acceptor without spinning up a real household stack.
//
// Method names mirror household.Service so the production wiring can pass
// `*household.Service` directly without an adapter.
type InviteAcceptor interface {
	// ValidateInvite returns nil if the token would be acceptable for a
	// new user. Returns the same error sentinels household.AcceptInvite
	// would (ErrInviteNotFound / ErrInviteExpired / ErrInviteAlreadyAccepted).
	ValidateInvite(ctx context.Context, rawToken string) error
	// AcceptInviteForUser consumes the token on the new user's behalf.
	AcceptInviteForUser(ctx context.Context, userID int64, rawToken string) error
}

// SetupAdminInput is accepted by /setup/admin. It both creates the first
// admin and chooses the instance's signup_mode in one step.
type SetupAdminInput struct {
	Email      string
	Password   string
	SignupMode string
}

type SigninResult struct {
	User    *model.User
	Token   string // raw cookie token — return to handler so it can set the cookie
	Expires time.Time
}

// Service owns auth orchestration. It receives repos as interfaces so tests
// can swap implementations. Token-hashing requires SESSION_SECRET, injected at
// construction so the package itself never reads env vars.
type Service struct {
	users    repository.UserRepository
	sessions repository.SessionRepository
	config   repository.InstanceConfigRepository
	invites  InviteAcceptor // optional — nil disables SignupWithInvite
	secret   string
	now      func() time.Time
}

func NewService(
	users repository.UserRepository,
	sessions repository.SessionRepository,
	config repository.InstanceConfigRepository,
	secret string,
) *Service {
	return &Service{
		users:    users,
		sessions: sessions,
		config:   config,
		secret:   secret,
		now:      time.Now,
	}
}

// WithInviteAcceptor wires the household-side invite acceptor for the
// SignupWithInvite path. Router-level wiring uses this; tests that don't
// exercise invite-signup leave it nil.
func (s *Service) WithInviteAcceptor(a InviteAcceptor) *Service {
	s.invites = a
	return s
}

// SetClock lets tests freeze time for session-expiry assertions.
func (s *Service) SetClock(fn func() time.Time) { s.now = fn }

// SetupAdmin creates the first user as admin and writes instance_config in a
// single step. Returns ErrInstanceConfigured if any user already exists —
// this endpoint is single-shot by design.
func (s *Service) SetupAdmin(ctx context.Context, in SetupAdminInput) (*SigninResult, error) {
	if err := validateSignupMode(in.SignupMode); err != nil {
		return nil, err
	}
	n, err := s.users.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count users: %w", err)
	}
	if n > 0 {
		return nil, ErrInstanceConfigured
	}
	if err := validateCredentials(in.Email, in.Password); err != nil {
		return nil, err
	}

	hash, err := HashPassword(in.Password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	u := &model.User{
		Email:        normalizeEmail(in.Email),
		PasswordHash: hash,
		IsAdmin:      true,
		LastScope:    model.ScopePersonal,
		DefaultScope: model.ScopePersonal,
	}
	if err := s.users.Create(ctx, u); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	if err := s.config.Upsert(ctx, &model.InstanceConfig{SignupMode: in.SignupMode}); err != nil {
		return nil, fmt.Errorf("write instance_config: %w", err)
	}
	return s.issueSession(ctx, u)
}

// Signup creates a regular (non-admin) user, gated by instance_config.signup_mode.
// In invite_only mode this endpoint always rejects — invite-acceptance lives
// in the household issue (see ADR-0007 / issue #45).
func (s *Service) Signup(ctx context.Context, in SignupInput) (*SigninResult, error) {
	cfg, err := s.config.Get(ctx)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// /setup/admin hasn't run — signup is meaningless until then.
			return nil, ErrInstanceConfigured
		}
		return nil, fmt.Errorf("get instance_config: %w", err)
	}
	if cfg.SignupMode != model.SignupModeLocalMultiTenant {
		return nil, ErrSignupClosed
	}
	if err := validateCredentials(in.Email, in.Password); err != nil {
		return nil, err
	}

	hash, err := HashPassword(in.Password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	u := &model.User{
		Email:        normalizeEmail(in.Email),
		PasswordHash: hash,
		LastScope:    model.ScopePersonal,
		DefaultScope: model.ScopePersonal,
	}
	if err := s.users.Create(ctx, u); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return s.issueSession(ctx, u)
}

// SignupWithInvite creates a non-admin user and consumes the supplied
// household invite token in one flow. Works regardless of the instance's
// signup_mode — the valid invite IS the gate, so this is the canonical
// onboarding path in invite_only mode.
//
// Sequencing: validate token (peek-only) → validate credentials → create
// user → accept invite for new user. If acceptance races and fails
// post-create, we delete the freshly-created user so the operator doesn't
// see an orphan account.
func (s *Service) SignupWithInvite(ctx context.Context, in SignupWithInviteInput) (*SigninResult, error) {
	if s.invites == nil {
		return nil, ErrInviteUnavailable
	}
	if strings.TrimSpace(in.InviteToken) == "" {
		return nil, ErrInviteRequired
	}
	// Confirm /setup/admin has run — otherwise there can't be any invites.
	if _, err := s.config.Get(ctx); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrInstanceConfigured
		}
		return nil, fmt.Errorf("get instance_config: %w", err)
	}
	if err := s.invites.ValidateInvite(ctx, in.InviteToken); err != nil {
		return nil, err
	}
	if err := validateCredentials(in.Email, in.Password); err != nil {
		return nil, err
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	u := &model.User{
		Email:        normalizeEmail(in.Email),
		PasswordHash: hash,
		LastScope:    model.ScopePersonal,
		DefaultScope: model.ScopePersonal,
	}
	if err := s.users.Create(ctx, u); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	if err := s.invites.AcceptInviteForUser(ctx, u.ID, in.InviteToken); err != nil {
		// Race / rejection between probe and accept. Clean up the orphan
		// user so retries with a fresh token work without an email collision.
		_ = s.users.HardDelete(ctx, u.ID)
		return nil, err
	}
	return s.issueSession(ctx, u)
}

func (s *Service) Signin(ctx context.Context, email, password string) (*SigninResult, error) {
	u, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	if err := CheckPassword(u.PasswordHash, password); err != nil {
		return nil, ErrInvalidCredentials
	}
	return s.issueSession(ctx, u)
}

// Signout deletes the session bound to the given raw token. Idempotent.
func (s *Service) Signout(ctx context.Context, rawToken string) error {
	return s.sessions.DeleteByTokenHash(ctx, HashToken(rawToken, s.secret))
}

// Authenticate is the workhorse called by middleware. Returns the user bound
// to the raw cookie token, or an error if the session is missing/expired.
// Touches last_used_at on each successful call (sliding session).
func (s *Service) Authenticate(ctx context.Context, rawToken string) (*model.User, error) {
	if rawToken == "" {
		return nil, ErrSessionNotFound
	}
	hash := HashToken(rawToken, s.secret)
	sess, err := s.sessions.GetByTokenHash(ctx, hash)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	now := s.now()
	if !sess.ExpiresAt.After(now) {
		// Best-effort cleanup; don't fail the request on it.
		_ = s.sessions.DeleteByTokenHash(ctx, hash)
		return nil, ErrSessionExpired
	}
	u, err := s.users.GetByID(ctx, sess.UserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// User was deleted but session lingered — invalidate the session.
			_ = s.sessions.DeleteByTokenHash(ctx, hash)
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	// Sliding touch — fire-and-forget; staleness of a single request is harmless.
	_ = s.sessions.Touch(ctx, sess.ID, now)
	return u, nil
}

// IsBootstrapped returns true once at least one user exists. Used to gate
// /setup/admin and to inform the frontend whether to show first-boot UI.
func (s *Service) IsBootstrapped(ctx context.Context) (bool, error) {
	n, err := s.users.Count(ctx)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// InstanceConfig is a read-through to the config row (or ErrNotFound when
// /setup/admin hasn't run).
func (s *Service) InstanceConfig(ctx context.Context) (*model.InstanceConfig, error) {
	return s.config.Get(ctx)
}

// issueSession mints a fresh token, stores its hash, and returns the raw
// token so the handler can set the cookie.
func (s *Service) issueSession(ctx context.Context, u *model.User) (*SigninResult, error) {
	token, err := NewToken()
	if err != nil {
		return nil, fmt.Errorf("token: %w", err)
	}
	now := s.now()
	expires := now.Add(SessionTTL)
	sess := &model.Session{
		UserID:     u.ID,
		TokenHash:  HashToken(token, s.secret),
		ExpiresAt:  expires,
		CreatedAt:  now,
		LastUsedAt: now,
	}
	if err := s.sessions.Create(ctx, sess); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &SigninResult{User: u, Token: token, Expires: expires}, nil
}

func validateCredentials(email, password string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return ErrEmailRequired
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return ErrInvalidEmail
	}
	if len(password) < MinPasswordLen {
		return ErrPasswordTooShort
	}
	return nil
}

func validateSignupMode(mode string) error {
	switch mode {
	case model.SignupModeLocalMultiTenant, model.SignupModeInviteOnly:
		return nil
	default:
		return ErrInvalidSignupMode
	}
}

func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
