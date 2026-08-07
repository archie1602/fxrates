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

func newTestClient(t *testing.T, handler http.Handler) *frankfurter.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := frankfurter.New(
		server.Client(),
		server.URL,
		frankfurter.RetryPolicy{MaxAttempts: 3, Delay: time.Millisecond},
	)
	if err != nil {
		t.Fatalf("frankfurter.New returned unexpected error: %v", err)
	}
	return client
}
