package config

import (
	"os"
	"strconv"
	"strings"
)

// Config holds the runtime configuration for the Nusantara Core Banking System API.
type Config struct {
	AppEnv              string
	Port                string
	PublicBaseURL       string
	AllowedOrigins      []string
	StrictOriginGuard   bool
	DatabaseURL         string
	UpstashRedisURL     string
	JWTSecret           string
	APIKeySalt          string
	ProductionMasterKey string
	DefaultRateLim      int
}

// Load reads configuration from environment variables with enterprise defaults for cbs.api.saefulrohman.dev.
func Load() *Config {
	rateLim := 60
	if v := os.Getenv("DEFAULT_RATE_LIMIT"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			rateLim = parsed
		}
	}

	defaultOrigins := "https://playground.api.saefulrohman.dev,https://cbs.api.saefulrohman.dev,https://saefulrohman.dev,http://localhost:3000,http://localhost:5173,http://localhost:8080"
	rawOrigins := getEnv("ALLOWED_ORIGINS", defaultOrigins)
	var origins []string
	for _, o := range strings.Split(rawOrigins, ",") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(o, "/"))
		if trimmed != "" {
			origins = append(origins, trimmed)
		}
	}

	strictOrigin := getEnv("STRICT_ORIGIN_GUARD", "true") == "true"

	return &Config{
		AppEnv:              getEnv("APP_ENV", "production"),
		Port:                getEnv("PORT", "8080"),
		PublicBaseURL:       getEnv("API_BASE_URL", "https://cbs.api.saefulrohman.dev"),
		AllowedOrigins:      origins,
		StrictOriginGuard:   strictOrigin,
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		UpstashRedisURL:     os.Getenv("UPSTASH_REDIS_URL"),
		JWTSecret:           getEnv("JWT_SECRET", "enterprise-portfolio-default-jwt-secret-key-64-chars-minimum-2026"),
		APIKeySalt:          getEnv("API_KEY_SALT", "enterprise-banking-grade-salt-2026"),
		ProductionMasterKey: os.Getenv("PRODUCTION_MASTER_KEY"),
		DefaultRateLim:      rateLim,
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
