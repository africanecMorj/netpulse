package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	HTTPAddr             string
	DatabaseURL          string
	CheckInterval        time.Duration
	RequestTimeout       time.Duration
	MetricsRetentionDays int
	JWTSecret            string
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:             env("HTTP_ADDR", ":8080"),
		DatabaseURL:          os.Getenv("DATABASE_URL"),
		CheckInterval:        durationEnv("CHECK_INTERVAL", 10*time.Second),
		RequestTimeout:       durationEnv("REQUEST_TIMEOUT", 3*time.Second),
		MetricsRetentionDays: intEnv("METRICS_RETENTION_DAYS", 30),
		JWTSecret:            os.Getenv("JWT_SECRET"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if len(cfg.JWTSecret) < 32 {
		return Config{}, fmt.Errorf("JWT_SECRET must be at least 32 bytes")
	}
	if cfg.CheckInterval < 5*time.Second {
		return Config{}, fmt.Errorf("CHECK_INTERVAL must be at least 5s")
	}
	if cfg.RequestTimeout <= 0 {
		return Config{}, fmt.Errorf("REQUEST_TIMEOUT must be positive")
	}
	if cfg.MetricsRetentionDays < 1 {
		return Config{}, fmt.Errorf("METRICS_RETENTION_DAYS must be positive")
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
	}
	return fallback
}

func intEnv(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		var parsed int
		if _, err := fmt.Sscanf(value, "%d", &parsed); err == nil {
			return parsed
		}
	}
	return fallback
}
