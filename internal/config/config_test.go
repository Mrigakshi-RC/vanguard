package config

import "testing"

func TestLoad_defaults(t *testing.T) {
	t.Setenv("VANGUARD_HTTP_ADDR", "")
	t.Setenv("VANGUARD_REDIS_ADDR", "")
	t.Setenv("VANGUARD_REDIS_LIST_KEY", "")
	t.Setenv("VANGUARD_POSTGRES_DSN", "")

	cfg := Load()

	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.RedisAddr != "localhost:6379" {
		t.Errorf("RedisAddr = %q, want localhost:6379", cfg.RedisAddr)
	}
	if cfg.RedisListKey != "vanguard:events:ingest" {
		t.Errorf("RedisListKey = %q, want vanguard:events:ingest", cfg.RedisListKey)
	}
	if cfg.PostgresDSN == "" {
		t.Error("PostgresDSN is empty")
	}
}

func TestLoad_envOverrides(t *testing.T) {
	t.Setenv("VANGUARD_HTTP_ADDR", ":9090")
	t.Setenv("VANGUARD_REDIS_ADDR", "redis:6379")
	t.Setenv("VANGUARD_REDIS_LIST_KEY", "custom:list")
	t.Setenv("VANGUARD_POSTGRES_DSN", "postgres://user:pass@db:5432/app?sslmode=disable")

	cfg := Load()

	if cfg.HTTPAddr != ":9090" {
		t.Errorf("HTTPAddr = %q, want :9090", cfg.HTTPAddr)
	}
	if cfg.RedisAddr != "redis:6379" {
		t.Errorf("RedisAddr = %q, want redis:6379", cfg.RedisAddr)
	}
	if cfg.RedisListKey != "custom:list" {
		t.Errorf("RedisListKey = %q, want custom:list", cfg.RedisListKey)
	}
	if cfg.PostgresDSN != "postgres://user:pass@db:5432/app?sslmode=disable" {
		t.Errorf("PostgresDSN = %q, want override DSN", cfg.PostgresDSN)
	}
}
