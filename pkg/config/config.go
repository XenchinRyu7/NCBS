package config

import (
	"bufio"
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

// loadDotEnv reads a local .env file if present without overriding already-set OS environment variables.
func loadDotEnv() {
	for _, path := range []string{".env", "../.env"} {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			idx := strings.IndexByte(line, '=')
			if idx <= 0 {
				continue
			}
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			val = strings.Trim(val, `"'`)
			if os.Getenv(key) == "" {
				_ = os.Setenv(key, val)
			}
		}
		_ = f.Close()
		break
	}
}

// Load reads configuration from environment variables with enterprise defaults for cbs.api.saefulrohman.dev.
func Load() *Config {
	loadDotEnv()

	rateLim := 60
	if v := os.Getenv("DEFAULT_RATE_LIMIT"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			rateLim = parsed
		}
	}

	defaultOrigins := "https://playground.api.saefulrohman.dev"
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
