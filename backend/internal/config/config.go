package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	AppEnv          string
	HTTPAddress     string
	DatabaseURL     string
	DataDir         string
	LogLevel        string
	ShutdownTimeout time.Duration
}

func Load() (Config, error) {
	shutdownTimeout, err := time.ParseDuration(
		getEnv("SHUTDOWN_TIMEOUT", "10s"),
	)
	if err != nil {
		return Config{}, fmt.Errorf(
			"parse SHUTDOWN_TIMEOUT: %w",
			err,
		)
	}

	cfg := Config{
		AppEnv:          getEnv("APP_ENV", "development"),
		HTTPAddress:     getEnv("HTTP_ADDRESS", ":8080"),
		DatabaseURL:     strings.TrimSpace(os.Getenv("DATABASE_URL")),
		DataDir:         getEnv("DATA_DIR", "./data"),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
		ShutdownTimeout: shutdownTimeout,
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (cfg Config) Validate() error {
	switch cfg.AppEnv {
	case "development", "test", "uat", "production":
	default:
		return fmt.Errorf("unsupported APP_ENV %q", cfg.AppEnv)
	}

	if strings.TrimSpace(cfg.HTTPAddress) == "" {
		return fmt.Errorf("HTTP_ADDRESS cannot be empty")
	}

	if cfg.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	if strings.TrimSpace(cfg.DataDir) == "" {
		return fmt.Errorf("DATA_DIR cannot be empty")
	}

	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("unsupported LOG_LEVEL %q", cfg.LogLevel)
	}

	if cfg.ShutdownTimeout <= 0 {
		return fmt.Errorf("SHUTDOWN_TIMEOUT must be greater than zero")
	}

	return nil
}

func getEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	return value
}
