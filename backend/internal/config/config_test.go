package config

import (
	"strings"
	"testing"
)

func TestResolveAppEnv(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "empty defaults to dev", in: "", want: AppEnvDev},
		{name: "dev", in: AppEnvDev, want: AppEnvDev},
		{name: "qa", in: AppEnvQA, want: AppEnvQA},
		{name: "test", in: AppEnvTest, want: AppEnvTest},
		{name: "prod", in: AppEnvProd, want: AppEnvProd},
		{name: "garbage rejected", in: "staging", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveAppEnv(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got nil (returned %q)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveDatabaseURL(t *testing.T) {
	tests := []struct {
		name        string
		appEnv      string
		explicit    string
		wantSubstr  string // expected substring in successful URL
		wantErrLike string // expected substring in error message
	}{
		{
			name:       "explicit wins for dev",
			appEnv:     AppEnvDev,
			explicit:   "postgres://x:y@h:1/custom?sslmode=disable",
			wantSubstr: "/custom?",
		},
		{
			name:       "explicit wins for prod (the production path)",
			appEnv:     AppEnvProd,
			explicit:   "postgres://prod:prod@db.internal/offbook?sslmode=require",
			wantSubstr: "db.internal",
		},
		{
			name:       "dev default",
			appEnv:     AppEnvDev,
			wantSubstr: "/offbook_dev?",
		},
		{
			name:       "qa default",
			appEnv:     AppEnvQA,
			wantSubstr: "/offbook_qa?",
		},
		{
			name:       "test default",
			appEnv:     AppEnvTest,
			wantSubstr: "/offbook_test?",
		},
		{
			name:        "prod with no explicit URL is an error",
			appEnv:      AppEnvProd,
			wantErrLike: "DATABASE_URL",
		},
		{
			name:        "unknown env is an error",
			appEnv:      "staging",
			wantErrLike: "unknown",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveDatabaseURL(tc.appEnv, tc.explicit)
			if tc.wantErrLike != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil (URL=%q)", tc.wantErrLike, got)
				}
				if !strings.Contains(err.Error(), tc.wantErrLike) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErrLike)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(got, tc.wantSubstr) {
				t.Fatalf("URL %q does not contain %q", got, tc.wantSubstr)
			}
		})
	}
}

// TestLoad_DefaultsToDevDB verifies the integrated path: empty APP_ENV +
// empty DATABASE_URL → resolves to the dev DB. This is the structural fix
// for #183 — the dev path can't accidentally use whatever DATABASE_URL the
// shell happened to inherit from a test run.
func TestLoad_DefaultsToDevDB(t *testing.T) {
	t.Setenv("APP_ENV", "")
	t.Setenv("DATABASE_URL", "")
	// Strip secrets that would otherwise fail Plaid validation.
	t.Setenv("PLAID_CLIENT_ID", "")
	t.Setenv("PLAID_SECRET", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AppEnv != AppEnvDev {
		t.Errorf("AppEnv = %q, want %q", cfg.AppEnv, AppEnvDev)
	}
	if !strings.Contains(cfg.DatabaseURL, "/offbook_dev?") {
		t.Errorf("DatabaseURL = %q, want it to point at offbook_dev", cfg.DatabaseURL)
	}
}

func TestLoad_TestEnvUsesTestDB(t *testing.T) {
	t.Setenv("APP_ENV", AppEnvTest)
	t.Setenv("DATABASE_URL", "")
	t.Setenv("PLAID_CLIENT_ID", "")
	t.Setenv("PLAID_SECRET", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(cfg.DatabaseURL, "/offbook_test?") {
		t.Errorf("DatabaseURL = %q, want it to point at offbook_test", cfg.DatabaseURL)
	}
}

func TestLoad_QAEnvUsesQADB(t *testing.T) {
	t.Setenv("APP_ENV", AppEnvQA)
	t.Setenv("DATABASE_URL", "")
	t.Setenv("PLAID_CLIENT_ID", "")
	t.Setenv("PLAID_SECRET", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(cfg.DatabaseURL, "/offbook_qa?") {
		t.Errorf("DatabaseURL = %q, want it to point at offbook_qa", cfg.DatabaseURL)
	}
}

func TestLoad_RejectsPlaceholderSessionSecret(t *testing.T) {
	t.Setenv("APP_ENV", AppEnvQA)
	t.Setenv("DATABASE_URL", "")
	t.Setenv("SESSION_SECRET", "replace-with-qa-session-secret")
	t.Setenv("PLAID_CLIENT_ID", "")
	t.Setenv("PLAID_SECRET", "")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "placeholder") {
		t.Fatalf("Load error = %v, want placeholder SESSION_SECRET error", err)
	}
}

func TestLoad_ProdRequiresExplicitDatabaseURL(t *testing.T) {
	t.Setenv("APP_ENV", AppEnvProd)
	t.Setenv("DATABASE_URL", "")
	t.Setenv("PLAID_CLIENT_ID", "")
	t.Setenv("PLAID_SECRET", "")

	if _, err := Load(); err == nil {
		t.Fatal("want error when APP_ENV=prod and DATABASE_URL is empty; got nil")
	}
}
