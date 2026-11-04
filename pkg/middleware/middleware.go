package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cbs-bank/enterprise-portfolio-api/pkg/database"
	"github.com/cbs-bank/enterprise-portfolio-api/pkg/model"
	"github.com/cbs-bank/enterprise-portfolio-api/pkg/repository"
	"github.com/cbs-bank/enterprise-portfolio-api/pkg/service"
	"github.com/cbs-bank/enterprise-portfolio-api/pkg/problem"
	"github.com/go-chi/chi/v5/middleware"
)

type ctxKey string

const (
	CtxActorIDKey ctxKey = "actor_id"
	CtxRoleKey    ctxKey = "actor_role"
	CtxEnvModeKey ctxKey = "env_mode"
)

// OriginWhitelistGuard enforces that browser/web requests originate strictly from whitelisted domains
// (e.g., https://playground.api.saefulrohman.dev and https://cbs.api.saefulrohman.dev).
func OriginWhitelistGuard(allowedOrigins []string, strict bool) func(http.Handler) http.Handler {
	allowedSet := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		clean := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(o, "/")))
		if clean != "" {
			allowedSet[clean] = true
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strict {
				next.ServeHTTP(w, r)
				return
			}

			origin := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(r.Header.Get("Origin"), "/")))
			if origin != "" && !allowedSet[origin] {
				problem.Render(w, r, http.StatusForbidden, "Origin Not Whitelisted (WAF Policy)",
					fmt.Sprintf("Origin '%s' is not authorized to access this Core Banking API. Whitelisted playground: https://playground.api.saefulrohman.dev", origin))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// EnvironmentScope tags the request context and response headers with the active ledger environment
// ("PRODUCTION" for /v1 vs "TRIAL_SANDBOX" for /trial/v1, /dev/v1, and /sandbox/v1).
// It also blocks Trial API Keys (cbs_trial_*) from executing against the Production (/v1) route.
func EnvironmentScope(envMode string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-CBS-Environment", envMode)

			apiKey := strings.TrimSpace(r.Header.Get("X-API-Key"))
			if envMode == "PRODUCTION" && strings.HasPrefix(apiKey, "cbs_trial_") {
				problem.Render(w, r, http.StatusForbidden, "Environment Mismatch (Trial Key on Production Route)",
					"Trial/Playground API Key ('cbs_trial_*') is not permitted on the Production Core Ledger (/v1). Use '/trial/v1' or '/dev/v1' for API Playground and Postman testing.")
				return
			}

			ctx := context.WithValue(r.Context(), CtxEnvModeKey, envMode)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// StructuredLogger logs every HTTP request in JSON format with trace_id and latency metrics.
func StructuredLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		traceID := r.Header.Get("X-Request-ID")
		if traceID == "" {
			traceID = middleware.GetReqID(r.Context())
		}
		w.Header().Set("X-Request-ID", traceID)

		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		logEntry := map[string]interface{}{
			"timestamp":   time.Now().UTC().Format(time.RFC3339),
			"trace_id":    traceID,
			"method":      r.Method,
			"path":        r.URL.Path,
			"status":      ww.Status(),
			"duration_ms": time.Since(start).Milliseconds(),
			"client_ip":   r.RemoteAddr,
			"user_agent":  r.UserAgent(),
		}
		if jsonLog, err := json.Marshal(logEntry); err == nil {
			fmt.Println(string(jsonLog))
		}
	})
}

// SlidingWindowRateLimit enforces rate limiting per API key or client IP using the Redis Lua script.
func SlidingWindowRateLimit(store *database.Store, repo *repository.Repository, defaultLimit int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			limit := defaultLimit
			identifier := "ip:" + r.RemoteAddr

			if rawKey := strings.TrimSpace(r.Header.Get("X-API-Key")); rawKey != "" {
				if ak, err := repo.ValidateAPIKey(r.Context(), rawKey); err == nil {
					limit = ak.RateLimitPerMin
					identifier = "apikey:" + ak.ID
				}
			}

			allowed, remaining := store.AllowSlidingWindow(r.Context(), identifier, limit, time.Minute)
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))

			if !allowed {
				problem.Render(w, r, http.StatusTooManyRequests, "Rate Limit Exceeded",
					fmt.Sprintf("Sliding window rate limit of %d requests per minute exceeded. Please retry later.", limit))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

type bodyRecorder struct {
	http.ResponseWriter
	status int
	buf    bytes.Buffer
}

func (br *bodyRecorder) WriteHeader(code int) {
	br.status = code
	br.ResponseWriter.WriteHeader(code)
}

func (br *bodyRecorder) Write(b []byte) (int, error) {
	br.buf.Write(b)
	return br.ResponseWriter.Write(b)
}

// Idempotency enforces Core Banking-grade exactly-once execution using Redis Idempotency Keys.
func Idempotency(store *database.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			idempotencyKey := strings.TrimSpace(r.Header.Get("X-Idempotency-Key"))
			if idempotencyKey == "" {
				problem.Render(w, r, http.StatusBadRequest, "Missing Idempotency Key",
					"Header 'X-Idempotency-Key' is mandatory for all financial mutation operations.")
				return
			}

			ctx := r.Context()
			redisKey := fmt.Sprintf("idempotency:%s", idempotencyKey)

			// 1. Check if response is already cached
			if val, ok := store.Get(ctx, redisKey); ok && val != "" {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Cache-Hit", "Idempotency-Replay")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(val))
				return
			}

			// 2. Acquire short in-flight lock to prevent concurrent duplicate mutations
			lockKey := fmt.Sprintf("idempotency:lock:%s", idempotencyKey)
			if !store.SetNX(ctx, lockKey, "LOCKED", 10*time.Second) {
				problem.Render(w, r, http.StatusConflict, "Idempotency Conflict",
					"A financial transaction with the same 'X-Idempotency-Key' is currently in flight.")
				return
			}
			defer store.Del(ctx, lockKey)

			rec := &bodyRecorder{
				ResponseWriter: w,
				status:         http.StatusOK,
			}
			next.ServeHTTP(rec, r)

			// 3. Cache successful 2xx responses for 24 hours
			if rec.status >= 200 && rec.status < 300 && rec.buf.Len() > 0 {
				store.Set(ctx, redisKey, rec.buf.String(), 24*time.Hour)
			}
		})
	}
}

// RequireRoles enforces Zero-Trust JWT or SNAP-BI API Key authentication and checks Role-Based Access Control.
func RequireRoles(svc *service.Service, repo *repository.Repository, allowedRoles ...model.UserRole) func(http.Handler) http.Handler {
	roleSet := make(map[model.UserRole]bool, len(allowedRoles))
	for _, role := range allowedRoles {
		roleSet[role] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var actorID string
			var actorRole model.UserRole

			authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
			apiKeyHeader := strings.TrimSpace(r.Header.Get("X-API-Key"))

			if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
				tokenStr := strings.TrimSpace(authHeader[7:])
				claims, err := svc.VerifyJWT(tokenStr)
				if err != nil || claims.TokenType != "access" {
					problem.Render(w, r, http.StatusUnauthorized, "Unauthorized",
						"Invalid or expired Bearer JWT access token.")
					return
				}
				actorID = claims.UserID
				actorRole = claims.Role
			} else if apiKeyHeader != "" {
				ak, err := repo.ValidateAPIKey(r.Context(), apiKeyHeader)
				if err != nil {
					problem.Render(w, r, http.StatusUnauthorized, "Invalid API Key",
						"The provided 'X-API-Key' is invalid, inactive, or expired.")
					return
				}
				actorID = ak.ID
				// Valid SNAP-BI Sandbox Partner Key grants testing access across sandbox endpoints
				actorRole = model.RoleAdmin
			} else {
				problem.Render(w, r, http.StatusUnauthorized, "Authentication Required",
					"Provide a valid 'Authorization: Bearer <token>' or 'X-API-Key' header.")
				return
			}

			if len(roleSet) > 0 && !roleSet[actorRole] {
				problem.Render(w, r, http.StatusForbidden, "Insufficient Permissions",
					fmt.Sprintf("Role '%s' is not authorized to execute this banking operation.", actorRole))
				return
			}

			ctx := context.WithValue(r.Context(), CtxActorIDKey, actorID)
			ctx = context.WithValue(ctx, CtxRoleKey, actorRole)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
