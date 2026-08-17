package frankfurter_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fxrates/internal/provider/frankfurter"
	"fxrates/internal/service"
)

func TestClientFetchRate(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("request method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v2/rate/EUR/MXN" {
			t.Errorf("request path = %q, want %q", r.URL.Path, "/v2/rate/EUR/MXN")
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept header = %q, want application/json", r.Header.Get("Accept"))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"date":"2026-08-07","base":"EUR","quote":"MXN","rate":19.909}`))
	}))

	got, err := client.FetchRate(context.Background(), "EUR/MXN")
	if err != nil {
		t.Fatalf("FetchRate returned unexpected error: %v", err)
	}
	want := service.RateSnapshot{
		Pair:     "EUR/MXN",
		Rate:     "19.909",
		RateDate: time.Date(2026, time.August, 7, 0, 0, 0, 0, time.UTC),
	}
	if got != want {
		t.Errorf("FetchRate() = %+v, want %+v", got, want)
	}
}

func TestClientFetchRateReturnsUpstreamError(t *testing.T) {
	var attempts atomic.Int32
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"message":"temporarily unavailable"}`))
	}))

	_, err := client.FetchRate(context.Background(), "EUR/MXN")
	var upstreamErr *frankfurter.UpstreamError
	if !errors.As(err, &upstreamErr) {
		t.Fatalf("FetchRate() error = %v, want *frankfurter.UpstreamError", err)
	}
	if upstreamErr.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("upstream status = %d, want %d", upstreamErr.StatusCode, http.StatusServiceUnavailable)
	}
	if upstreamErr.Message != "temporarily unavailable" {
		t.Errorf("upstream message = %q, want %q", upstreamErr.Message, "temporarily unavailable")
	}
	if attempts.Load() != 3 {
		t.Errorf("request attempts = %d, want 3", attempts.Load())
	}
}

func TestClientFetchRateDoesNotRetryBadRequest(t *testing.T) {
	var attempts atomic.Int32
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"invalid request"}`))
	}))

	_, err := client.FetchRate(context.Background(), "EUR/MXN")
	var upstreamErr *frankfurter.UpstreamError
	if !errors.As(err, &upstreamErr) {
		t.Fatalf("FetchRate() error = %v, want *frankfurter.UpstreamError", err)
	}
	if attempts.Load() != 1 {
		t.Errorf("request attempts = %d, want 1", attempts.Load())
	}
}

func TestClientFetchRateRetriesTransportError(t *testing.T) {
	var attempts atomic.Int32
	transportErr := errors.New("connection reset")
	client, err := frankfurter.New(
		&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts.Add(1)
			return nil, transportErr
		})},
		"https://example.test",
		frankfurter.RetryPolicy{
			MaxAttempts:  3,
			InitialDelay: 0,
			MaxDelay:     time.Second,
		},
	)
	if err != nil {
		t.Fatalf("frankfurter.New returned unexpected error: %v", err)
	}

	_, err = client.FetchRate(context.Background(), "EUR/MXN")
	if !errors.Is(err, transportErr) {
		t.Fatalf("FetchRate() error = %v, want wrapped %v", err, transportErr)
	}
	if attempts.Load() != 3 {
		t.Errorf("request attempts = %d, want 3", attempts.Load())
	}
}

func TestClientFetchRateRejectsInvalidRate(t *testing.T) {
	var attempts atomic.Int32
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"date":"2026-08-07","base":"EUR","quote":"MXN","rate":0}`))
	}))

	_, err := client.FetchRate(context.Background(), "EUR/MXN")
	if err == nil || !strings.Contains(err.Error(), "invalid rate") {
		t.Fatalf("FetchRate() error = %v, want invalid rate error", err)
	}
	if attempts.Load() != 1 {
		t.Errorf("request attempts = %d, want 1", attempts.Load())
	}
}

func TestClientFetchRateHonorsCanceledContext(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.FetchRate(ctx, "EUR/MXN")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FetchRate() error = %v, want %v", err, context.Canceled)
	}
}

func TestClientFetchRateRejectsRetryAfterAboveMaximum(t *testing.T) {
	var attempts atomic.Int32
	client := newTestClientWithPolicy(
		t,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			attempts.Add(1)
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message":"rate limited"}`))
		}),
		frankfurter.RetryPolicy{
			MaxAttempts:  3,
			InitialDelay: 0,
			MaxDelay:     time.Second,
		},
	)

	_, err := client.FetchRate(context.Background(), "EUR/MXN")
	var upstreamErr *frankfurter.UpstreamError
	if !errors.As(err, &upstreamErr) {
		t.Fatalf("FetchRate() error = %v, want wrapped *frankfurter.UpstreamError", err)
	}
	if !strings.Contains(err.Error(), "exceeds configured maximum") {
		t.Fatalf("FetchRate() error = %v, want maximum delay error", err)
	}
	if attempts.Load() != 1 {
		t.Errorf("request attempts = %d, want 1", attempts.Load())
	}
}

func newTestClient(t *testing.T, handler http.Handler) *frankfurter.Client {
	return newTestClientWithPolicy(t, handler, frankfurter.RetryPolicy{
		MaxAttempts:  3,
		InitialDelay: 0,
		MaxDelay:     time.Second,
	})
}

func newTestClientWithPolicy(
	t *testing.T,
	handler http.Handler,
	retry frankfurter.RetryPolicy,
) *frankfurter.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := frankfurter.New(
		server.Client(),
		server.URL,
		retry,
	)
	if err != nil {
		t.Fatalf("frankfurter.New returned unexpected error: %v", err)
	}
	return client
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
