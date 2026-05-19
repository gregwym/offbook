package auth_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service/auth"
	"github.com/gregwym/offbook/backend/internal/service/household"
)

// stubInviteAcceptor records ValidateInvite and AcceptInviteForUser calls
// without needing the full household service stack. Tests configure the
// errors each method should return so the auth service's sequencing is
// observable from outside.
type stubInviteAcceptor struct {
	mu             sync.Mutex
	validateErr    error
	acceptErr      error
	validateCalls  int
	acceptCalls    int
	acceptedFor    int64
	acceptedToken  string
	validatedToken string
}

func (s *stubInviteAcceptor) ValidateInvite(_ context.Context, rawToken string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.validateCalls++
	s.validatedToken = rawToken
	return s.validateErr
}

func (s *stubInviteAcceptor) AcceptInviteForUser(_ context.Context, userID int64, rawToken string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acceptCalls++
	s.acceptedFor = userID
	s.acceptedToken = rawToken
	return s.acceptErr
}

// TestSignupWithInvite_HappyPath: valid token + valid credentials + admin
// already set up → new user is created, invite is consumed, session is
// returned. Works in invite_only mode (the canonical case).
func TestSignupWithInvite_HappyPath(t *testing.T) {
	svc, g := newTestService(t)
	truncateAuthState(t, g)
	ctx := context.Background()

	if _, err := svc.SetupAdmin(ctx, auth.SetupAdminInput{
		Email:      uniqueEmail("admin"),
		Password:   "a-strong-password",
		SignupMode: model.SignupModeInviteOnly,
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	stub := &stubInviteAcceptor{}
	svc.WithInviteAcceptor(stub)

	res, err := svc.SignupWithInvite(ctx, auth.SignupWithInviteInput{
		Email:       uniqueEmail("invitee"),
		Password:    "guest-password",
		InviteToken: "raw-token-abc",
	})
	if err != nil {
		t.Fatalf("SignupWithInvite: %v", err)
	}
	if res.User.ID == 0 {
		t.Errorf("user ID = 0, want > 0")
	}
	if res.Token == "" {
		t.Errorf("session token empty")
	}
	if stub.validateCalls != 1 {
		t.Errorf("validate calls = %d, want 1", stub.validateCalls)
	}
	if stub.acceptCalls != 1 {
		t.Errorf("accept calls = %d, want 1", stub.acceptCalls)
	}
	if stub.acceptedFor != res.User.ID {
		t.Errorf("accept user_id = %d, want %d", stub.acceptedFor, res.User.ID)
	}
	if stub.acceptedToken != "raw-token-abc" {
		t.Errorf("accept token = %q, want raw-token-abc", stub.acceptedToken)
	}
}

// TestSignupWithInvite_InvalidToken: validate fails → user never created.
func TestSignupWithInvite_InvalidToken(t *testing.T) {
	svc, g := newTestService(t)
	truncateAuthState(t, g)
	ctx := context.Background()

	if _, err := svc.SetupAdmin(ctx, auth.SetupAdminInput{
		Email:      uniqueEmail("admin"),
		Password:   "a-strong-password",
		SignupMode: model.SignupModeInviteOnly,
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	stub := &stubInviteAcceptor{validateErr: household.ErrInviteNotFound}
	svc.WithInviteAcceptor(stub)

	email := uniqueEmail("invitee")
	_, err := svc.SignupWithInvite(ctx, auth.SignupWithInviteInput{
		Email:       email,
		Password:    "guest-password",
		InviteToken: "bad-token",
	})
	if !errors.Is(err, household.ErrInviteNotFound) {
		t.Fatalf("err = %v, want ErrInviteNotFound", err)
	}
	if stub.acceptCalls != 0 {
		t.Errorf("acceptCalls = %d, want 0 (must not consume on invalid token)", stub.acceptCalls)
	}
	// Verify no user was created.
	if _, signinErr := svc.Signin(ctx, email, "guest-password"); !errors.Is(signinErr, auth.ErrInvalidCredentials) {
		t.Errorf("Signin after failed invite-signup err = %v, want ErrInvalidCredentials (no user)", signinErr)
	}
}

// TestSignupWithInvite_AcceptRace: validate passes but the accept call
// fails (race with another acceptor consuming the token). The user row
// must be deleted so retries with a fresh token don't hit a unique-email
// collision.
func TestSignupWithInvite_AcceptRace(t *testing.T) {
	svc, g := newTestService(t)
	truncateAuthState(t, g)
	ctx := context.Background()

	if _, err := svc.SetupAdmin(ctx, auth.SetupAdminInput{
		Email:      uniqueEmail("admin"),
		Password:   "a-strong-password",
		SignupMode: model.SignupModeInviteOnly,
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	stub := &stubInviteAcceptor{acceptErr: household.ErrInviteAlreadyAccepted}
	svc.WithInviteAcceptor(stub)

	email := uniqueEmail("invitee")
	_, err := svc.SignupWithInvite(ctx, auth.SignupWithInviteInput{
		Email:       email,
		Password:    "guest-password",
		InviteToken: "raced-token",
	})
	if !errors.Is(err, household.ErrInviteAlreadyAccepted) {
		t.Fatalf("err = %v, want ErrInviteAlreadyAccepted", err)
	}
	// User must be cleaned up so a retry with the SAME email + a fresh
	// token doesn't fail on the unique constraint.
	if _, signinErr := svc.Signin(ctx, email, "guest-password"); !errors.Is(signinErr, auth.ErrInvalidCredentials) {
		t.Errorf("user not cleaned up — Signin err = %v, want ErrInvalidCredentials", signinErr)
	}
}

// TestSignupWithInvite_RejectsEmptyToken: the gate fails before any DB call.
func TestSignupWithInvite_RejectsEmptyToken(t *testing.T) {
	svc, g := newTestService(t)
	truncateAuthState(t, g)
	stub := &stubInviteAcceptor{}
	svc.WithInviteAcceptor(stub)

	_, err := svc.SignupWithInvite(context.Background(), auth.SignupWithInviteInput{
		Email:       uniqueEmail("invitee"),
		Password:    "password",
		InviteToken: "  ",
	})
	if !errors.Is(err, auth.ErrInviteRequired) {
		t.Fatalf("err = %v, want ErrInviteRequired", err)
	}
	if stub.validateCalls != 0 {
		t.Errorf("validate called %d times on empty token; should short-circuit", stub.validateCalls)
	}
}

// TestSignupWithInvite_RejectsBadCredentials: validate passes, but
// credentials fail validation → user never created and accept never called.
func TestSignupWithInvite_RejectsBadCredentials(t *testing.T) {
	svc, g := newTestService(t)
	truncateAuthState(t, g)
	ctx := context.Background()

	if _, err := svc.SetupAdmin(ctx, auth.SetupAdminInput{
		Email:      uniqueEmail("admin"),
		Password:   "a-strong-password",
		SignupMode: model.SignupModeInviteOnly,
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	stub := &stubInviteAcceptor{}
	svc.WithInviteAcceptor(stub)

	_, err := svc.SignupWithInvite(ctx, auth.SignupWithInviteInput{
		Email:       "not-an-email",
		Password:    "short", // < MinPasswordLen
		InviteToken: "raw-token",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if stub.acceptCalls != 0 {
		t.Errorf("accept called %d times despite bad credentials", stub.acceptCalls)
	}
}

// TestSignupWithInvite_UnconfiguredService: ErrInviteUnavailable when the
// acceptor was never wired (defensive — keeps a misconfigured router
// from silently creating users).
func TestSignupWithInvite_UnconfiguredService(t *testing.T) {
	svc, g := newTestService(t)
	truncateAuthState(t, g)
	// Deliberately don't WithInviteAcceptor.

	_, err := svc.SignupWithInvite(context.Background(), auth.SignupWithInviteInput{
		Email:       uniqueEmail("invitee"),
		Password:    "guest-password",
		InviteToken: "raw-token",
	})
	if !errors.Is(err, auth.ErrInviteUnavailable) {
		t.Fatalf("err = %v, want ErrInviteUnavailable", err)
	}
}
