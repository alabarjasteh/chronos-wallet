package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Addr                     string
	DatabaseURL              string
	WorkerCount              int
	WorkerPollInterval       time.Duration
	ReconcileInterval        time.Duration
	ProcessingTimeout        time.Duration
	BankTimeout              time.Duration
	BankMockFailureRate      float64
	BankMockNetworkErrorRate float64
}

func Load() (Config, error) {
	cfg := Config{
		Addr:                     envString("ADDR", ":8080"),
		DatabaseURL:              envString("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/chronos_wallet?sslmode=disable"),
		WorkerCount:              envInt("WORKER_COUNT", 4),
		WorkerPollInterval:       envDuration("WORKER_POLL_INTERVAL", time.Second),
		ReconcileInterval:        envDuration("RECONCILE_INTERVAL", 10*time.Second),
		ProcessingTimeout:        envDuration("PROCESSING_TIMEOUT", 2*time.Minute),
		BankTimeout:              envDuration("BANK_TIMEOUT", 3*time.Second),
		BankMockFailureRate:      envFloat("BANK_MOCK_FAILURE_RATE", 0.10),
		BankMockNetworkErrorRate: envFloat("BANK_MOCK_NETWORK_ERROR_RATE", 0.10),
	}
	if cfg.WorkerCount < 1 {
		return Config{}, fmt.Errorf("WORKER_COUNT must be >= 1")
	}
	if cfg.BankMockFailureRate < 0 || cfg.BankMockFailureRate > 1 {
		return Config{}, fmt.Errorf("BANK_MOCK_FAILURE_RATE must be between 0 and 1")
	}
	if cfg.BankMockNetworkErrorRate < 0 || cfg.BankMockNetworkErrorRate > 1 {
		return Config{}, fmt.Errorf("BANK_MOCK_NETWORK_ERROR_RATE must be between 0 and 1")
	}
	return cfg, nil
}

func envString(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envFloat(key string, fallback float64) float64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
