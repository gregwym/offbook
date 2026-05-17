package auth_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/db"
	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service/auth"
)

func loadRepoDotenv() {
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	for i := 0; i < 8; i++ {
		envPath := filepath.Join(dir, ".env")
		if _, err := os.Stat(envPath); err == nil {
			_ = godotenv.Load(envPath)
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	loadRepoDotenv()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		t.Skip("no DATABASE_URL set; skipping integration test")
	}
	g, err := db.Open(url)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.Ping(ctx, g); err != nil {
		t.Skipf("db.Ping: %v; skipping integration test", err)
	}
	return g
}

// newTestService wires a fresh Service against the real test DB. It does NOT
// truncate users — tests that exercise the "first user" path use truncateAuthState.
func newTestService(t *testing.T) (*auth.Service, *gorm.DB) {
	t.Helper()
	g := openTestDB(t)
	svc := auth.NewService(
		repository.NewUserRepository(g),
		repository.NewSessionRepository(g),
		repository.NewInstanceConfigRepository(g),
		"test-secret-do-not-use-in-prod",
	)
	return svc, g
}

// truncateAuthState wipes users/sessions/instance_config so tests that
// require a virgin instance ("first signup becomes admin") can run
// deterministically. CASCADE handles dependent FK rows from other domain
// tables created by other tests — but we don't expect those at this point
// because the auth tests run in their own package.
func truncateAuthState(t *testing.T, g *gorm.DB) {
	t.Helper()
	// Order matters where CASCADE isn't already in place.
	for _, stmt := range []string{
		`TRUNCATE TABLE ai_messages, ai_threads, ingestion_jobs, investments, savings_goals, budgets, transactions, account_shares, accounts, shared_goals, shared_budgets, household_invites, household_members, households, sessions, instance_config, users RESTART IDENTITY CASCADE`,
	} {
		if err := g.Exec(stmt).Error; err != nil {
			t.Fatalf("truncate: %v", err)
		}
	}
}

func uniqueEmail(prefix string) string {
	return fmt.Sprintf("%s-%d@example.test", prefix, time.Now().UnixNano())
}

func TestSetupAdmin_FirstCallSucceeds_SecondReturnsConflict(t *testing.T) {
	svc, g := newTestService(t)
	truncateAuthState(t, g)

	ctx := context.Background()
	res, err := svc.SetupAdmin(ctx, auth.SetupAdminInput{
		Email:      uniqueEmail("admin"),
		Password:   "a-strong-password",
		SignupMode: model.SignupModeInviteOnly,
	})
	if err != nil {
		t.Fatalf("first SetupAdmin: %v", err)
	}
	if !res.User.IsAdmin {
		t.Errorf("first user not marked admin")
	}
	if res.Token == "" {
		t.Errorf("expected session token from setup")
	}

	// Second call must reject — the endpoint is single-shot.
	_, err = svc.SetupAdmin(ctx, auth.SetupAdminInput{
		Email:      uniqueEmail("admin2"),
		Password:   "another-strong-password",
		SignupMode: model.SignupModeInviteOnly,
	})
	if !errors.Is(err, auth.ErrInstanceConfigured) {
		t.Errorf("second SetupAdmin err = %v, want ErrInstanceConfigured", err)
	}
}

func TestSetupAdmin_RejectsInvalidSignupMode(t *testing.T) {
	svc, g := newTestService(t)
	truncateAuthState(t, g)

	_, err := svc.SetupAdmin(context.Background(), auth.SetupAdminInput{
		Email:      uniqueEmail("admin"),
		Password:   "a-strong-password",
		SignupMode: "bogus",
	})
	if !errors.Is(err, auth.ErrInvalidSignupMode) {
		t.Errorf("err = %v, want ErrInvalidSignupMode", err)
	}
}

func TestSignup_GatedByInviteOnly(t *testing.T) {
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

	_, err := svc.Signup(ctx, auth.SignupInput{
		Email:    uniqueEmail("user"),
		Password: "another-strong-password",
	})
	if !errors.Is(err, auth.ErrSignupClosed) {
		t.Errorf("Signup in invite_only err = %v, want ErrSignupClosed", err)
	}
}

func TestSignup_AllowedInLocalMultiTenant(t *testing.T) {
	svc, g := newTestService(t)
	truncateAuthState(t, g)
	ctx := context.Background()

	if _, err := svc.SetupAdmin(ctx, auth.SetupAdminInput{
		Email:      uniqueEmail("admin"),
		Password:   "a-strong-password",
		SignupMode: model.SignupModeLocalMultiTenant,
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	res, err := svc.Signup(ctx, auth.SignupInput{
		Email:    uniqueEmail("user"),
		Password: "another-strong-password",
	})
	if err != nil {
		t.Fatalf("Signup: %v", err)
	}
	if res.User.IsAdmin {
		t.Errorf("non-first user must not be admin")
	}
	if res.Token == "" {
		t.Errorf("missing session token")
	}
}

func TestSignin_ValidCredentials_IssuesSession(t *testing.T) {
	svc, g := newTestService(t)
	truncateAuthState(t, g)
	ctx := context.Background()

	email := uniqueEmail("signin")
	pw := "a-strong-password"
	if _, err := svc.SetupAdmin(ctx, auth.SetupAdminInput{
		Email:      email,
		Password:   pw,
		SignupMode: model.SignupModeInviteOnly,
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	res, err := svc.Signin(ctx, email, pw)
	if err != nil {
		t.Fatalf("Signin: %v", err)
	}
	// Round-trip via Authenticate so we know the issued token resolves.
	got, err := svc.Authenticate(ctx, res.Token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got.ID != res.User.ID {
		t.Errorf("Authenticate returned uid %d, want %d", got.ID, res.User.ID)
	}
}

func TestSignin_BadPassword_RejectedWithGenericError(t *testing.T) {
	svc, g := newTestService(t)
	truncateAuthState(t, g)
	ctx := context.Background()

	email := uniqueEmail("signin")
	if _, err := svc.SetupAdmin(ctx, auth.SetupAdminInput{
		Email:      email,
		Password:   "a-strong-password",
		SignupMode: model.SignupModeInviteOnly,
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if _, err := svc.Signin(ctx, email, "wrong-password"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("bad-password err = %v, want ErrInvalidCredentials", err)
	}
	if _, err := svc.Signin(ctx, "ghost@example.test", "anything"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("unknown-email err = %v, want ErrInvalidCredentials", err)
	}
}

func TestAuthenticate_ExpiredSessionRejected(t *testing.T) {
	svc, g := newTestService(t)
	truncateAuthState(t, g)
	ctx := context.Background()

	// Freeze the clock far in the past for issuance.
	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return past })

	res, err := svc.SetupAdmin(ctx, auth.SetupAdminInput{
		Email:      uniqueEmail("expire"),
		Password:   "a-strong-password",
		SignupMode: model.SignupModeInviteOnly,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Jump the clock past the SessionTTL.
	svc.SetClock(func() time.Time { return past.Add(auth.SessionTTL + time.Hour) })
	if _, err := svc.Authenticate(ctx, res.Token); !errors.Is(err, auth.ErrSessionExpired) {
		t.Errorf("expired Authenticate err = %v, want ErrSessionExpired", err)
	}
}

func TestSignout_DeletesSession(t *testing.T) {
	svc, g := newTestService(t)
	truncateAuthState(t, g)
	ctx := context.Background()

	res, err := svc.SetupAdmin(ctx, auth.SetupAdminInput{
		Email:      uniqueEmail("signout"),
		Password:   "a-strong-password",
		SignupMode: model.SignupModeInviteOnly,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := svc.Signout(ctx, res.Token); err != nil {
		t.Fatalf("Signout: %v", err)
	}
	if _, err := svc.Authenticate(ctx, res.Token); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Errorf("post-signout Authenticate err = %v, want ErrSessionNotFound", err)
	}
}
