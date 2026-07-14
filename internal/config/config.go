package config

type Config struct {
	HTTPAddr     string
	RedisAddr    string
	RedisListKey string
	PostgresDSN  string
}

func Load() Config {
	return Config{
		HTTPAddr:     ":8080",
		RedisAddr:    "localhost:6379",
		RedisListKey: "vanguard:events:ingest",
		PostgresDSN:  "postgres://vanguard:vanguard@localhost:5432/vanguard?sslmode=disable",
	}
}
