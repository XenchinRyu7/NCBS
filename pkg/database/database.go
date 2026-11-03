package database

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/cbs-bank/enterprise-portfolio-api/pkg/config"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

// Store wraps both PostgreSQL and Redis clients along with a resilient fallback cache/rate-limiter
// so the serverless binary can boot cleanly even before external credentials are provisioned.
type Store struct {
	DB    *sql.DB
	Redis *redis.Client

	mu          sync.RWMutex
	memKV       map[string]memEntry
	slidingLogs map[string][]int64
}

type memEntry struct {
	value     string
	expiresAt time.Time
}

// New initializes connections to Supabase PostgreSQL and Upstash Redis.
func New(cfg *config.Config) *Store {
	s := &Store{
		memKV:       make(map[string]memEntry),
		slidingLogs: make(map[string][]int64),
	}

	if cfg.DatabaseURL != "" {
		if db, err := sql.Open("postgres", cfg.DatabaseURL); err == nil {
			db.SetMaxOpenConns(10)
			db.SetMaxIdleConns(5)
			db.SetConnMaxLifetime(5 * time.Minute)
			s.DB = db
		}
	}

	if cfg.UpstashRedisURL != "" {
		if opt, err := redis.ParseURL(cfg.UpstashRedisURL); err == nil {
			s.Redis = redis.NewClient(opt)
		}
	}

	return s
}

// PingDB checks PostgreSQL connectivity.
func (s *Store) PingDB(ctx context.Context) string {
	if s.DB == nil {
		return "FALLBACK_MEMORY"
	}
	if err := s.DB.PingContext(ctx); err != nil {
		return "DOWN"
	}
	return "UP"
}

// PingRedis checks Upstash Redis connectivity.
func (s *Store) PingRedis(ctx context.Context) string {
	if s.Redis == nil {
		return "FALLBACK_MEMORY"
	}
	if err := s.Redis.Ping(ctx).Err(); err != nil {
		return "DOWN"
	}
	return "UP"
}

// Get retrieves a cached key from Redis or fallback memory.
func (s *Store) Get(ctx context.Context, key string) (string, bool) {
	if s.Redis != nil {
		val, err := s.Redis.Get(ctx, key).Result()
		if err == nil && val != "" {
			return val, true
		}
	}

	s.mu.RLock()
	entry, ok := s.memKV[key]
	s.mu.RUnlock()
	if !ok {
		return "", false
	}
	if time.Now().After(entry.expiresAt) {
		s.mu.Lock()
		delete(s.memKV, key)
		s.mu.Unlock()
		return "", false
	}
	return entry.value, true
}

// Set stores a key with TTL in Redis and fallback memory.
func (s *Store) Set(ctx context.Context, key, value string, ttl time.Duration) {
	if s.Redis != nil {
		_ = s.Redis.Set(ctx, key, value, ttl).Err()
	}

	s.mu.Lock()
	s.memKV[key] = memEntry{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
	s.mu.Unlock()
}

// SetNX atomically sets a key only if it does not already exist (distributed lock / idempotency lock).
func (s *Store) SetNX(ctx context.Context, key, value string, ttl time.Duration) bool {
	if s.Redis != nil {
		ok, err := s.Redis.SetNX(ctx, key, value, ttl).Result()
		if err == nil {
			return ok
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, exists := s.memKV[key]; exists && time.Now().Before(entry.expiresAt) {
		return false
	}
	s.memKV[key] = memEntry{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
	return true
}

// Del deletes a key from Redis and fallback memory.
func (s *Store) Del(ctx context.Context, key string) {
	if s.Redis != nil {
		_ = s.Redis.Del(ctx, key).Err()
	}
	s.mu.Lock()
	delete(s.memKV, key)
	s.mu.Unlock()
}

// slidingWindowLuaScript implements an atomic Sliding Window Log rate limiter in Redis.
const slidingWindowLuaScript = `
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local clearBefore = now - window

redis.call('ZREMRANGEBYSCORE', key, '-inf', clearBefore)
local amount = redis.call('ZCARD', key)

if amount < limit then
    redis.call('ZADD', key, now, tostring(now) .. '-' .. tostring(math.random(100000, 999999)))
    redis.call('PEXPIRE', key, window)
    return {1, limit - amount - 1}
else
    return {0, 0}
end
`

// AllowSlidingWindow evaluates the Sliding Window Log rate limit using Redis Lua script (or atomic fallback).
func (s *Store) AllowSlidingWindow(ctx context.Context, identifier string, limit int, window time.Duration) (allowed bool, remaining int) {
	nowMs := time.Now().UnixMilli()
	windowMs := window.Milliseconds()
	key := "ratelimit:sw:" + identifier

	if s.Redis != nil {
		res, err := s.Redis.Eval(ctx, slidingWindowLuaScript, []string{key}, nowMs, windowMs, limit).Int64Slice()
		if err == nil && len(res) == 2 {
			return res[0] == 1, int(res[1])
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	clearBefore := nowMs - windowMs
	timestamps := s.slidingLogs[key]
	valid := timestamps[:0]
	for _, ts := range timestamps {
		if ts > clearBefore {
			valid = append(valid, ts)
		}
	}

	if len(valid) >= limit {
		s.slidingLogs[key] = valid
		return false, 0
	}

	valid = append(valid, nowMs)
	s.slidingLogs[key] = valid
	return true, limit - len(valid)
}

