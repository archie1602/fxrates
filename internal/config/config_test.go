package config_test

import (
	"strings"
	"testing"
	"time"

	"fxrates/internal/config"
)

func TestLoadParsesValues(t *testing.T) {
	resetDurationEnvironment(t)
	t.Setenv("WORKER_POLL_INTERVAL", "750ms")
	t.Setenv("DATABASE_QUERY_TIMEOUT", "2s")
	t.Setenv("FRANKFURTER_MAX_ATTEMPTS", "4")
	t.Setenv("FRANKFURTER_RETRY_DELAY", "100ms")
	t.Setenv("FRANKFURTER_RETRY_MAX_DELAY", "2s")
	t.Setenv("WORKER_COUNT", "6")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load returned unexpected error: %v", err)
	}
	if cfg.WorkerPollInterval != 750*time.Millisecond {
		t.Errorf("WorkerPollInterval = %v, want %v", cfg.WorkerPollInterval, 750*time.Millisecond)
	}
	if cfg.DatabaseQueryTimeout != 2*time.Second {
		t.Errorf("DatabaseQueryTimeout = %v, want %v", cfg.DatabaseQueryTimeout, 2*time.Second)
	}
	if cfg.FrankfurterMaxAttempts != 4 || cfg.FrankfurterRetryDelay != 100*time.Millisecond || cfg.FrankfurterMaxDelay != 2*time.Second {
		t.Errorf(
			"retry policy = %d attempts with %v initial and %v maximum delay",
			cfg.FrankfurterMaxAttempts,
			cfg.FrankfurterRetryDelay,
			cfg.FrankfurterMaxDelay,
		)
	}
	if cfg.WorkerCount != 6 {
		t.Errorf("WorkerCount = %d, want 6", cfg.WorkerCount)
	}
}

func TestValidateRejectsInsufficientProcessingTimeout(t *testing.T) {
	resetDurationEnvironment(t)
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("FRANKFURTER_TIMEOUT", "5s")
	t.Setenv("FRANKFURTER_MAX_ATTEMPTS", "3")
	t.Setenv("FRANKFURTER_RETRY_DELAY", "250ms")
	t.Setenv("FRANKFURTER_RETRY_MAX_DELAY", "5s")
	t.Setenv("PROCESSING_TIMEOUT", "15s")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load returned unexpected error: %v", err)
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "PROCESSING_TIMEOUT") {
		t.Fatalf("Validate error = %v, want retry budget error", err)
	}
}

func TestValidateRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "non-positive database query timeout", key: "DATABASE_QUERY_TIMEOUT", value: "0s"},
		{name: "worker count above limit", key: "WORKER_COUNT", value: "33"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resetDurationEnvironment(t)
			t.Setenv("DATABASE_URL", "postgres://example")
			t.Setenv(test.key, test.value)

			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("Load returned unexpected error: %v", err)
			}
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("Validate error = %v, want error mentioning %s", err, test.key)
			}
		})
	}
}

func TestLoadUsesDefaultWorkerCount(t *testing.T) {
	resetDurationEnvironment(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load returned unexpected error: %v", err)
	}
	if cfg.WorkerCount != 4 {
		t.Errorf("WorkerCount = %d, want 4", cfg.WorkerCount)
	}
}

func TestLoadRejectsInvalidValue(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{key: "DATABASE_TIMEOUT", value: "soon"},
		{key: "FRANKFURTER_MAX_ATTEMPTS", value: "many"},
	}

	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			resetDurationEnvironment(t)
			t.Setenv(test.key, test.value)

			_, err := config.Load()
			if err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("Load error = %v, want error mentioning %s", err, test.key)
			}
		})
	}
}

func resetDurationEnvironment(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		"DATABASE_TIMEOUT",
		"DATABASE_QUERY_TIMEOUT",
		"SHUTDOWN_TIMEOUT",
		"FRANKFURTER_TIMEOUT",
		"FRANKFURTER_RETRY_DELAY",
		"FRANKFURTER_RETRY_MAX_DELAY",
		"WORKER_POLL_INTERVAL",
		"RECOVERY_INTERVAL",
		"PROCESSING_TIMEOUT",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("FRANKFURTER_MAX_ATTEMPTS", "")
	t.Setenv("WORKER_COUNT", "")
}
