package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr        string
	BaseURL         string
	DatabasePath    string
	CodeLength      int
	RequestTimeout  time.Duration
	ShutdownTimeout time.Duration
	MaxBodyBytes    int64
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:        env("HTTP_ADDR", ":8080"),
		BaseURL:         strings.TrimRight(env("BASE_URL", "http://localhost:8080"), "/"),
		DatabasePath:    env("DATABASE_PATH", "data/urls.db"),
		CodeLength:      7,
		RequestTimeout:  5 * time.Second,
		ShutdownTimeout: 10 * time.Second,
		MaxBodyBytes:    1 << 20,
	}

	var err error
	if cfg.CodeLength, err = envInt("CODE_LENGTH", cfg.CodeLength); err != nil {
		return Config{}, err
	}
	if cfg.RequestTimeout, err = envDuration("REQUEST_TIMEOUT", cfg.RequestTimeout); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = envDuration("SHUTDOWN_TIMEOUT", cfg.ShutdownTimeout); err != nil {
		return Config{}, err
	}
	if cfg.MaxBodyBytes, err = envInt64("MAX_BODY_BYTES", cfg.MaxBodyBytes); err != nil {
		return Config{}, err
	}

	if cfg.HTTPAddr == "" {
		return Config{}, fmt.Errorf("HTTP_ADDR cannot be empty")
	}
	if cfg.DatabasePath == "" {
		return Config{}, fmt.Errorf("DATABASE_PATH cannot be empty")
	}
	if cfg.CodeLength < 4 || cfg.CodeLength > 32 {
		return Config{}, fmt.Errorf("CODE_LENGTH must be between 4 and 32")
	}
	if cfg.RequestTimeout <= 0 || cfg.ShutdownTimeout <= 0 {
		return Config{}, fmt.Errorf("timeouts must be positive")
	}
	if cfg.MaxBodyBytes < 1 {
		return Config{}, fmt.Errorf("MAX_BODY_BYTES must be positive")
	}

	baseURL, err := url.Parse(cfg.BaseURL)
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return Config{}, fmt.Errorf("BASE_URL must be an absolute URL")
	}

	return cfg, nil
}

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return strings.TrimSpace(value)
	}
	return fallback
}

func envInt(key string, fallback int) (int, error) {
	value := env(key, "")
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return parsed, nil
}

func envInt64(key string, fallback int64) (int64, error) {
	value := env(key, "")
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return parsed, nil
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	value := env(key, "")
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration: %w", key, err)
	}
	return parsed, nil
}
