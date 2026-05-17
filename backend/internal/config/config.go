package config

import (
	"errors"
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
}

func Load() Config {
	// Load .env from cwd, then repo root (one level up), so commands work
	// whether run from backend/ or the repo root. godotenv stops on the first
	// missing file, so call once per candidate and ignore not-found errors.
	loadDotenv(".env")
	loadDotenv("../.env")

	return Config{
		Port:           getenv("PORT", "8000"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		FrontendURL:    getenv("FRONTEND_URL", "http://localhost:5173"),
		MigrationsPath: getenv("MIGRATIONS_PATH", "migrations"),
		SessionSecret:  os.Getenv("SESSION_SECRET"),
	}
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
