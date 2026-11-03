package config

import (
	"os"
	"strconv"
)

// Config holds the runtime configuration for the Enterprise Portfolio Core API.
type Config struct {
	AppEnv          string
	Port            string
	DatabaseURL     string
	UpstashRedisURL string
	JWTSecret       string
	APIKeySalt      string
	DefaultRateLim  int
}

// Load reads configuration from environment variables with safe defaults for local/demo execution.
func Load() *Config {
	rateLim := 60
	if v := os.Getenv("DEFAULT_RATE_LIMIT"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			rateLim = parsed
		}
	}

	return &Config{
		AppEnv:          getEnv("APP_ENV", "development"),
		Port:            getEnv("PORT", "8080"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		UpstashRedisURL: os.Getenv("UPSTASH_REDIS_URL"),
		JWTSecret:       getEnv("JWT_SECRET", "enterprise-portfolio-default-jwt-secret-key-64-chars-minimum-2026"),
		APIKeySalt:      getEnv("API_KEY_SALT", "enterprise-banking-grade-salt-2026"),
		DefaultRateLim:  rateLim,
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

