package config

import "os"

type Config struct {
	HTTPAddr     string
	RedisAddr    string
	RedisListKey string
	PostgresDSN  string
}

func Load() Config {
	return Config{
		HTTPAddr:     envOr("VANGUARD_HTTP_ADDR", ":8080"),
		RedisAddr:    envOr("VANGUARD_REDIS_ADDR", "localhost:6379"),
		RedisListKey: envOr("VANGUARD_REDIS_LIST_KEY", "vanguard:events:ingest"),
		PostgresDSN:  envOr("VANGUARD_POSTGRES_DSN", "postgres://vanguard:vanguard@localhost:5432/vanguard?sslmode=disable"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
