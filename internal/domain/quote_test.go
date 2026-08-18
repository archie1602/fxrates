package domain_test

import (
	"errors"
	"testing"

	"fxrates/internal/domain"
)

func TestParseUpdateStatus(t *testing.T) {
	for _, status := range []domain.UpdateStatus{
		domain.UpdatePending,
		domain.UpdateProcessing,
		domain.UpdateCompleted,
		domain.UpdateFailed,
	} {
		t.Run(string(status), func(t *testing.T) {
			got, err := domain.ParseUpdateStatus(string(status))
			if err != nil {
				t.Fatalf("ParseUpdateStatus returned unexpected error: %v", err)
			}
			if got != status {
				t.Errorf("ParseUpdateStatus() = %q, want %q", got, status)
			}
		})
	}

	if _, err := domain.ParseUpdateStatus("unknown"); !errors.Is(err, domain.ErrInvalidUpdateStatus) {
		t.Fatalf("ParseUpdateStatus() error = %v, want %v", err, domain.ErrInvalidUpdateStatus)
	}
}

func TestParseUpdateFailureCode(t *testing.T) {
	got, err := domain.ParseUpdateFailureCode(string(domain.UpdateFailureRateProvider))
	if err != nil {
		t.Fatalf("ParseUpdateFailureCode returned unexpected error: %v", err)
	}
	if got != domain.UpdateFailureRateProvider {
		t.Errorf("ParseUpdateFailureCode() = %q, want %q", got, domain.UpdateFailureRateProvider)
	}

	if _, err := domain.ParseUpdateFailureCode("internal_error"); !errors.Is(
		err,
		domain.ErrInvalidUpdateFailureCode,
	) {
		t.Fatalf("ParseUpdateFailureCode() error = %v, want %v", err, domain.ErrInvalidUpdateFailureCode)
	}
}
