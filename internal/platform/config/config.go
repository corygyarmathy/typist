// Package config loads runtime configuration from environment variables.
//
// Follows 12-factor principles: configuration is environment, secrets never
// live in source, and the zero-config defaults assume local development.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// devJWTSecret is the placeholder used in compose.yaml. Refused in production.
const devJWTSecret = "dev-only-change-me"

// Config holds all runtime configuration for the server.
type Config struct {
	HTTPAddr     string
	DatabaseURL  string
	JWTSecret    Secret // see Secret below - redacts itself in logs
	JWTAccessTTL time.Duration
	LogLevel     string
	Env          string // "development" | "production"
}

// Load reads configuration from the process environment.
// Returns an error if any required value is missing or invalid.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr: getDefault("HTTP_ADDR", ":8080"),
		LogLevel: getDefault("LOG_LEVEL", "info"),
		Env:      getDefault("APP_ENV", "development"),
	}

	var errs []string

	dbURL, err := getSecretEnv("DATABASE_URL")
	if err != nil {
		errs = append(errs, err.Error())
	}
	cfg.DatabaseURL = dbURL
	if cfg.DatabaseURL == "" {
		errs = append(errs, "DATABASE_URL is required")
	}

	jwt, err := getSecretEnv("JWT_SECRET")
	if err != nil {
		errs = append(errs, err.Error())
	}
	cfg.JWTSecret = Secret(jwt)
	if jwt == "" {
		errs = append(errs, "JWT_SECRET is required")
	}

	ttlRaw := getDefault("JWT_ACCESS_TTL", "24h")
	ttl, err := time.ParseDuration(ttlRaw)
	if err != nil {
		errs = append(errs, fmt.Sprintf("JWT_ACCESS_TTL %q is invalid: %v", ttlRaw, err))
	}
	cfg.JWTAccessTTL = ttl

	// Stop dev secrets from reaching production.
	if cfg.Env == "production" {
		if jwt == devJWTSecret {
			errs = append(errs, "JWT_SECRET is still the development placeholder")
		}
		if len(jwt) < 32 {
			errs = append(errs, "JWT_SECRET must be at least 32 bytes in production")
		}
	}

	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Sprintf("LOG_LEVEL %q is invalid (debug|info|warn|error)", cfg.LogLevel))
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return cfg, nil
}

func getDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getSecretEnv reads KEY, or reads the file at KEY_FILE if KEY is unset.
// This is what lets Docker secrets and sops-nix/systemd credentials both work.
func getSecretEnv(key string) (string, error) {
	if v := os.Getenv(key); v != "" {
		return v, nil
	}
	if path := os.Getenv(key + "_FILE"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("reading %s_FILE: %w", key, err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	return "", nil
}
