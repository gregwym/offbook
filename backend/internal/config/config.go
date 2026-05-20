package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/joho/godotenv"
)

// AppEnv is the environment label that drives DB selection and any other
// per-env behavior. See ResolveDatabaseURL.
const (
	AppEnvDev  = "dev"
	AppEnvTest = "test"
	AppEnvProd = "prod"
)

type Config struct {
	AppEnv         string // dev | test | prod (see Resolve* helpers)
	Port           string
	DatabaseURL    string
	FrontendURL    string
	MigrationsPath string
	SessionSecret  string

	// Plaid (M3+). Optional as a group; if PlaidClientID is set then
	// PlaidSecret and PlaidTokenKey must also be set or Load() returns an error.
	PlaidClientID string
	PlaidSecret   string
	PlaidEnv      string // sandbox | development | production
	PlaidTokenKey []byte // 32 raw bytes (decoded from PLAID_TOKEN_KEY hex)

	// AI providers (M7+). Both optional — when absent, the corresponding
	// provider is unavailable in the settings UI rather than fatal at boot.
	ClaudeAPIKey  string
	OllamaBaseURL string
}

// PlaidConfigured reports whether the Plaid surface is enabled.
// Handlers that touch Plaid should refuse the request when this is false.
func (c Config) PlaidConfigured() bool {
	return c.PlaidClientID != "" && c.PlaidSecret != "" && len(c.PlaidTokenKey) == 32
}

// ClaudeConfigured reports whether the Claude provider can be constructed.
// The AI service hides Claude from settings when this is false.
func (c Config) ClaudeConfigured() bool {
	return c.ClaudeAPIKey != ""
}

// ResolveAppEnv normalizes and validates APP_ENV. Empty → "dev" (the default
// for a fresh local checkout).
func ResolveAppEnv(raw string) (string, error) {
	if raw == "" {
		return AppEnvDev, nil
	}
	switch raw {
	case AppEnvDev, AppEnvTest, AppEnvProd:
		return raw, nil
	default:
		return "", fmt.Errorf("APP_ENV must be %s|%s|%s, got %q",
			AppEnvDev, AppEnvTest, AppEnvProd, raw)
	}
}

// ResolveDatabaseURL picks the DB URL for the given app env. Explicit
// DATABASE_URL always wins (this is how docker-compose and prod inject the
// connection string). When unset:
//   - dev → local Postgres, database "offbook_dev"
//   - test → local Postgres, database "offbook_test"
//   - prod → no default; returns an error (force operators to be explicit)
//
// This is the structural fix for #183: every env resolves its own URL through
// one place, so the dev DB and the test DB can never accidentally collide.
func ResolveDatabaseURL(appEnv, explicitURL string) (string, error) {
	if explicitURL != "" {
		return explicitURL, nil
	}
	switch appEnv {
	case AppEnvDev:
		return "postgres://offbook:offbook@localhost:5432/offbook_dev?sslmode=disable", nil
	case AppEnvTest:
		return "postgres://offbook:offbook@localhost:5432/offbook_test?sslmode=disable", nil
	case AppEnvProd:
		return "", errors.New("APP_ENV=prod requires DATABASE_URL to be set explicitly (no default)")
	default:
		return "", fmt.Errorf("unknown APP_ENV %q", appEnv)
	}
}

// Load reads env vars (with .env support) and validates required combinations.
// Fails fast on misconfiguration so the server never starts with broken
// invariants (e.g. PLAID_CLIENT_ID set but no encryption key — would silently
// store bearer tokens in plaintext if not for this guard, see ADR-0010).
func Load() (Config, error) {
	// Load .env from cwd, then repo root (one level up), so commands work
	// whether run from backend/ or the repo root. godotenv stops on the first
	// missing file, so call once per candidate and ignore not-found errors.
	loadDotenv(".env")
	loadDotenv("../.env")

	appEnv, err := ResolveAppEnv(os.Getenv("APP_ENV"))
	if err != nil {
		return Config{}, err
	}
	dbURL, err := ResolveDatabaseURL(appEnv, os.Getenv("DATABASE_URL"))
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		AppEnv:         appEnv,
		Port:           getenv("PORT", "8000"),
		DatabaseURL:    dbURL,
		FrontendURL:    getenv("FRONTEND_URL", "http://localhost:5173"),
		MigrationsPath: getenv("MIGRATIONS_PATH", "migrations"),
		SessionSecret:  os.Getenv("SESSION_SECRET"),
		PlaidClientID:  os.Getenv("PLAID_CLIENT_ID"),
		PlaidSecret:    os.Getenv("PLAID_SECRET"),
		PlaidEnv:       getenv("PLAID_ENV", "sandbox"),
		ClaudeAPIKey:   os.Getenv("CLAUDE_API_KEY"),
		OllamaBaseURL:  getenv("OLLAMA_BASE_URL", "http://localhost:11434"),
	}

	if cfg.PlaidClientID != "" {
		if cfg.PlaidSecret == "" {
			return Config{}, errors.New("PLAID_CLIENT_ID is set but PLAID_SECRET is empty")
		}
		keyHex := os.Getenv("PLAID_TOKEN_KEY")
		if keyHex == "" {
			return Config{}, errors.New("PLAID_CLIENT_ID is set but PLAID_TOKEN_KEY is empty (see ADR-0010; generate with `openssl rand -hex 32`)")
		}
		key, err := hex.DecodeString(keyHex)
		if err != nil {
			return Config{}, fmt.Errorf("PLAID_TOKEN_KEY must be hex: %w", err)
		}
		if len(key) != 32 {
			return Config{}, fmt.Errorf("PLAID_TOKEN_KEY must decode to 32 bytes (AES-256), got %d", len(key))
		}
		cfg.PlaidTokenKey = key
		switch cfg.PlaidEnv {
		case "sandbox", "development", "production":
		default:
			return Config{}, fmt.Errorf("PLAID_ENV must be sandbox|development|production, got %q", cfg.PlaidEnv)
		}
	}

	return cfg, nil
}

// MustLoad is a thin wrapper for main(), where any error is fatal.
func MustLoad() Config {
	cfg, err := Load()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "config: "+err.Error())
		os.Exit(2)
	}
	return cfg
}

func loadDotenv(path string) {
	if err := godotenv.Load(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		// Real parse error — surface via stderr so the user notices, but don't crash.
		// (Most callers want to keep going if the file is malformed.)
		_, _ = os.Stderr.WriteString("dotenv load " + path + ": " + err.Error() + "\n")
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
