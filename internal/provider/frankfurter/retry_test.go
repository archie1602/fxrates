package frankfurter

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestExponentialBackoffLimit(t *testing.T) {
	policy := RetryPolicy{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     250 * time.Millisecond,
	}

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 1, want: 100 * time.Millisecond},
		{attempt: 2, want: 200 * time.Millisecond},
		{attempt: 3, want: 250 * time.Millisecond},
		{attempt: 10, want: 250 * time.Millisecond},
	}

	for _, test := range tests {
		if got := exponentialBackoffLimit(policy, test.attempt); got != test.want {
			t.Errorf("attempt %d limit = %v, want %v", test.attempt, got, test.want)
		}
	}
}

func TestCalculateRetryDelayUsesFullJitter(t *testing.T) {
	policy := RetryPolicy{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     time.Second,
	}

	for range 100 {
		delay, err := calculateRetryDelay(policy, 2, "", time.Time{})
		if err != nil {
			t.Fatalf("calculateRetryDelay returned unexpected error: %v", err)
		}
		if delay < 0 || delay >= 200*time.Millisecond {
			t.Fatalf("delay = %v, want full jitter in [0, 200ms)", delay)
		}
	}
}

func TestCalculateRetryDelayHonorsRetryAfter(t *testing.T) {
	policy := RetryPolicy{MaxDelay: 5 * time.Second}
	delay, err := calculateRetryDelay(policy, 1, "2", time.Time{})
	if err != nil {
		t.Fatalf("calculateRetryDelay returned unexpected error: %v", err)
	}
	if delay != 2*time.Second {
		t.Errorf("delay = %v, want 2s", delay)
	}
}

func TestCalculateRetryDelayRejectsRetryAfterAboveMaximum(t *testing.T) {
	policy := RetryPolicy{MaxDelay: time.Second}
	if _, err := calculateRetryDelay(policy, 1, "2", time.Time{}); err == nil {
		t.Fatal("calculateRetryDelay returned nil error for Retry-After above maximum")
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		value string
		want  time.Duration
		found bool
	}{
		{name: "delta seconds", value: "3", want: 3 * time.Second, found: true},
		{name: "HTTP date", value: now.Add(4 * time.Second).Format(http.TimeFormat), want: 4 * time.Second, found: true},
		{name: "past HTTP date", value: now.Add(-time.Second).Format(http.TimeFormat), want: 0, found: true},
		{name: "negative delta", value: "-1", found: false},
		{name: "malformed", value: "later", found: false},
		{name: "empty", value: "", found: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, found := parseRetryAfter(test.value, now)
			if found != test.found || got != test.want {
				t.Errorf("parseRetryAfter() = (%v, %v), want (%v, %v)", got, found, test.want, test.found)
			}
		})
	}
}

func TestWaitForRetryHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := waitForRetry(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("waitForRetry error = %v, want %v", err, context.Canceled)
	}
}
