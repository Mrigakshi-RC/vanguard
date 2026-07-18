package config

import (
	"os"
	"strconv"
)

type Config struct {
	HTTPAddr          string
	RedisAddr         string
	RedisListKey      string
	PostgresDSN       string
	RateLimitRate     int
	RateLimitCapacity int
	RateLimitEnabled  bool
	RetryMaxAttempts  int
	RetryBaseDelay    int
	RetryMaxDelay     int
	RedisDLQKey       string
}

func Load() Config {
	return Config{
		HTTPAddr:          envOr("VANGUARD_HTTP_ADDR", ":8080"),
		RedisAddr:         envOr("VANGUARD_REDIS_ADDR", "localhost:6379"),
		RedisListKey:      envOr("VANGUARD_REDIS_LIST_KEY", "vanguard:events:ingest"),
		PostgresDSN:       envOr("VANGUARD_POSTGRES_DSN", "postgres://vanguard:vanguard@localhost:5432/vanguard?sslmode=disable"),
		RateLimitRate:     envAsInt("VANGUARD_RATE_LIMIT_RATE", 10),
		RateLimitCapacity: envAsInt("VANGUARD_RATE_LIMIT_CAPACITY", 100),
		RateLimitEnabled:  envAsBool("VANGUARD_RATE_LIMIT_ENABLED", false),
		RetryMaxAttempts:  envAsInt("VANGUARD_RETRY_MAX_ATTEMPTS", 5),
		RetryBaseDelay:    envAsInt("VANGUARD_RETRY_BASE_DELAY", 1),
		RetryMaxDelay:     envAsInt("VANGUARD_RETRY_MAX_DELAY", 30),
		RedisDLQKey:       envOr("VANGUARD_REDIS_DLQ_KEY", "vanguard:events:dlq"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envAsInt(key string, fallback int) int {
	valueStr := os.Getenv(key)
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}
	return fallback
}

func envAsBool(key string, fallback bool) bool {
	valueStr := os.Getenv(key)
	if value, err := strconv.ParseBool(valueStr); err == nil {
		return value
	}
	return fallback
}
