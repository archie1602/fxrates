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
	if cfg.FrankfurterMaxAttempts != 4 || cfg.FrankfurterRetryDelay != 100*time.Millisecond {
		t.Errorf(
			"retry policy = %d attempts with %v delay",
			cfg.FrankfurterMaxAttempts,
			cfg.FrankfurterRetryDelay,
		)
	}
}

func TestValidateRejectsInsufficientProcessingTimeout(t *testing.T) {
	resetDurationEnvironment(t)
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("FRANKFURTER_TIMEOUT", "5s")
	t.Setenv("FRANKFURTER_MAX_ATTEMPTS", "3")
	t.Setenv("FRANKFURTER_RETRY_DELAY", "250ms")
	t.Setenv("PROCESSING_TIMEOUT", "15s")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load returned unexpected error: %v", err)
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "PROCESSING_TIMEOUT") {
		t.Fatalf("Validate error = %v, want retry budget error", err)
	}
}

func TestValidateRejectsNonPositiveDatabaseQueryTimeout(t *testing.T) {
	resetDurationEnvironment(t)
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("DATABASE_QUERY_TIMEOUT", "0s")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load returned unexpected error: %v", err)
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "DATABASE_QUERY_TIMEOUT") {
		t.Fatalf("Validate error = %v, want database query timeout error", err)
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
		"WORKER_POLL_INTERVAL",
		"RECOVERY_INTERVAL",
		"PROCESSING_TIMEOUT",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("FRANKFURTER_MAX_ATTEMPTS", "")
}
