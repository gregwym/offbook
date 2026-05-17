package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
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
}

// PlaidConfigured reports whether the Plaid surface is enabled.
// Handlers that touch Plaid should refuse the request when this is false.
func (c Config) PlaidConfigured() bool {
	return c.PlaidClientID != "" && c.PlaidSecret != "" && len(c.PlaidTokenKey) == 32
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

	cfg := Config{
		Port:           getenv("PORT", "8000"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		FrontendURL:    getenv("FRONTEND_URL", "http://localhost:5173"),
		MigrationsPath: getenv("MIGRATIONS_PATH", "migrations"),
		SessionSecret:  os.Getenv("SESSION_SECRET"),
		PlaidClientID:  os.Getenv("PLAID_CLIENT_ID"),
		PlaidSecret:    os.Getenv("PLAID_SECRET"),
		PlaidEnv:       getenv("PLAID_ENV", "sandbox"),
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
