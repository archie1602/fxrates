package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	defaultHTTPAddr               = ":8080"
	defaultShutdownTimeout        = 10 * time.Second
	defaultDatabaseTimeout        = 5 * time.Second
	defaultDatabaseQueryTimeout   = 3 * time.Second
	defaultFrankfurterBaseURL     = "https://api.frankfurter.dev"
	defaultFrankfurterTimeout     = 5 * time.Second
	defaultFrankfurterMaxAttempts = 3
	defaultFrankfurterRetryDelay  = 250 * time.Millisecond
	defaultFrankfurterMaxDelay    = 5 * time.Second
	defaultWorkerPollInterval     = 500 * time.Millisecond
	defaultRecoveryInterval       = 10 * time.Second
	defaultProcessingTimeout      = 30 * time.Second
)

type Config struct {
	HTTPAddr               string
	DatabaseURL            string
	DatabaseTimeout        time.Duration
	DatabaseQueryTimeout   time.Duration
	ShutdownTimeout        time.Duration
	FrankfurterBaseURL     string
	FrankfurterTimeout     time.Duration
	FrankfurterMaxAttempts int
	FrankfurterRetryDelay  time.Duration
	FrankfurterMaxDelay    time.Duration
	WorkerPollInterval     time.Duration
	RecoveryInterval       time.Duration
	ProcessingTimeout      time.Duration
}

func Load() (Config, error) {
	databaseTimeout, err := durationOrDefault("DATABASE_TIMEOUT", defaultDatabaseTimeout)
	if err != nil {
		return Config{}, err
	}
	databaseQueryTimeout, err := durationOrDefault("DATABASE_QUERY_TIMEOUT", defaultDatabaseQueryTimeout)
	if err != nil {
		return Config{}, err
	}
	shutdownTimeout, err := durationOrDefault("SHUTDOWN_TIMEOUT", defaultShutdownTimeout)
	if err != nil {
		return Config{}, err
	}
	frankfurterTimeout, err := durationOrDefault("FRANKFURTER_TIMEOUT", defaultFrankfurterTimeout)
	if err != nil {
		return Config{}, err
	}
	frankfurterMaxAttempts, err := intOrDefault("FRANKFURTER_MAX_ATTEMPTS", defaultFrankfurterMaxAttempts)
	if err != nil {
		return Config{}, err
	}
	frankfurterRetryDelay, err := durationOrDefault("FRANKFURTER_RETRY_DELAY", defaultFrankfurterRetryDelay)
	if err != nil {
		return Config{}, err
	}
	frankfurterMaxDelay, err := durationOrDefault("FRANKFURTER_RETRY_MAX_DELAY", defaultFrankfurterMaxDelay)
	if err != nil {
		return Config{}, err
	}
	workerPollInterval, err := durationOrDefault("WORKER_POLL_INTERVAL", defaultWorkerPollInterval)
	if err != nil {
		return Config{}, err
	}
	recoveryInterval, err := durationOrDefault("RECOVERY_INTERVAL", defaultRecoveryInterval)
	if err != nil {
		return Config{}, err
	}
	processingTimeout, err := durationOrDefault("PROCESSING_TIMEOUT", defaultProcessingTimeout)
	if err != nil {
		return Config{}, err
	}

	return Config{
		HTTPAddr:               envOrDefault("HTTP_ADDR", defaultHTTPAddr),
		DatabaseURL:            os.Getenv("DATABASE_URL"),
		DatabaseTimeout:        databaseTimeout,
		DatabaseQueryTimeout:   databaseQueryTimeout,
		ShutdownTimeout:        shutdownTimeout,
		FrankfurterBaseURL:     envOrDefault("FRANKFURTER_BASE_URL", defaultFrankfurterBaseURL),
		FrankfurterTimeout:     frankfurterTimeout,
		FrankfurterMaxAttempts: frankfurterMaxAttempts,
		FrankfurterRetryDelay:  frankfurterRetryDelay,
		FrankfurterMaxDelay:    frankfurterMaxDelay,
		WorkerPollInterval:     workerPollInterval,
		RecoveryInterval:       recoveryInterval,
		ProcessingTimeout:      processingTimeout,
	}, nil
}

func (c Config) Validate() error {
	if c.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	if c.DatabaseTimeout <= 0 {
		return errors.New("DATABASE_TIMEOUT must be positive")
	}
	if c.DatabaseQueryTimeout <= 0 {
		return errors.New("DATABASE_QUERY_TIMEOUT must be positive")
	}
	if c.ShutdownTimeout <= 0 {
		return errors.New("SHUTDOWN_TIMEOUT must be positive")
	}
	if c.FrankfurterTimeout <= 0 {
		return errors.New("FRANKFURTER_TIMEOUT must be positive")
	}
	if c.FrankfurterMaxAttempts < 1 || c.FrankfurterMaxAttempts > 10 {
		return errors.New("FRANKFURTER_MAX_ATTEMPTS must be between 1 and 10")
	}
	if c.FrankfurterRetryDelay < 0 {
		return errors.New("FRANKFURTER_RETRY_DELAY must not be negative")
	}
	if c.FrankfurterMaxDelay <= 0 {
		return errors.New("FRANKFURTER_RETRY_MAX_DELAY must be positive")
	}
	if c.FrankfurterRetryDelay > c.FrankfurterMaxDelay {
		return errors.New("FRANKFURTER_RETRY_DELAY must not exceed FRANKFURTER_RETRY_MAX_DELAY")
	}
	if c.WorkerPollInterval <= 0 {
		return errors.New("WORKER_POLL_INTERVAL must be positive")
	}
	if c.RecoveryInterval <= 0 {
		return errors.New("RECOVERY_INTERVAL must be positive")
	}
	if !processingTimeoutCoversRetries(c) {
		return errors.New("PROCESSING_TIMEOUT must exceed the maximum Frankfurter retry duration")
	}

	return nil
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

func durationOrDefault(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid duration: %w", key, err)
	}

	return duration, nil
}

func intOrDefault(key string, fallback int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid integer: %w", key, err)
	}

	return parsed, nil
}

func processingTimeoutCoversRetries(c Config) bool {
	remaining := c.ProcessingTimeout
	for attempt := 1; attempt <= c.FrankfurterMaxAttempts; attempt++ {
		if remaining <= c.FrankfurterTimeout {
			return false
		}
		remaining -= c.FrankfurterTimeout

		if attempt < c.FrankfurterMaxAttempts {
			if remaining <= c.FrankfurterMaxDelay {
				return false
			}
			remaining -= c.FrankfurterMaxDelay
		}
	}

	return true
}
