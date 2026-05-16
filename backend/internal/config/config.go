package config

import (
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port        string
	DatabaseURL string
	FrontendURL string
}

func Load() Config {
	_ = godotenv.Load()

	return Config{
		Port:        getenv("PORT", "8000"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		FrontendURL: getenv("FRONTEND_URL", "http://localhost:5173"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
