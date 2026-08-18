package frankfurter_test

import (
	"context"
	"errors"
	"io"
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

func TestClientFetchRateRetriesInterruptedResponseBody(t *testing.T) {
	var attempts atomic.Int32
	client, err := frankfurter.New(
		&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempt := attempts.Add(1)
			body := io.Reader(strings.NewReader(`{"date":"2026-08-07","base":"EUR","quote":"MXN","rate":19.909}`))
			if attempt == 1 {
				body = &interruptedReader{data: []byte(`{"date":"2026-08-07"`)}
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(body),
				Header:     make(http.Header),
			}, nil
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

	got, err := client.FetchRate(context.Background(), "EUR/MXN")
	if err != nil {
		t.Fatalf("FetchRate returned unexpected error: %v", err)
	}
	if got.Rate != "19.909" {
		t.Errorf("FetchRate() rate = %q, want %q", got.Rate, "19.909")
	}
	if attempts.Load() != 2 {
		t.Errorf("request attempts = %d, want 2", attempts.Load())
	}
}

func TestClientFetchRateStopsAfterInterruptedResponseBodyRetries(t *testing.T) {
	var attempts atomic.Int32
	client, err := frankfurter.New(
		&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts.Add(1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(&interruptedReader{data: []byte(`{"date":"2026-08-07"`)}),
				Header:     make(http.Header),
			}, nil
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
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("FetchRate() error = %v, want wrapped %v", err, io.ErrUnexpectedEOF)
	}
	if attempts.Load() != 3 {
		t.Errorf("request attempts = %d, want 3", attempts.Load())
	}
}

func TestClientFetchRateDoesNotRetryOversizedResponse(t *testing.T) {
	var attempts atomic.Int32
	client, err := frankfurter.New(
		&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts.Add(1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", 65<<10))),
				Header:     make(http.Header),
			}, nil
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
	if err == nil || !strings.Contains(err.Error(), "response body exceeds size limit") {
		t.Fatalf("FetchRate() error = %v, want response size error", err)
	}
	if attempts.Load() != 1 {
		t.Errorf("request attempts = %d, want 1", attempts.Load())
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

func TestClientFetchRateRejectsInvalidSuccessfulResponse(t *testing.T) {
	tests := []struct {
		name         string
		responseBody string
		wantError    string
	}{
		{
			name:         "malformed JSON",
			responseBody: `{"date":`,
			wantError:    "decode response",
		},
		{
			name:         "multiple JSON values",
			responseBody: `{"date":"2026-08-07","base":"EUR","quote":"MXN","rate":19.909} {}`,
			wantError:    "one JSON object",
		},
		{
			name:         "mismatched pair",
			responseBody: `{"date":"2026-08-07","base":"USD","quote":"MXN","rate":19.909}`,
			wantError:    "does not match requested pair",
		},
		{
			name:         "invalid date",
			responseBody: `{"date":"2026-02-30","base":"EUR","quote":"MXN","rate":19.909}`,
			wantError:    "parse rate date",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var attempts atomic.Int32
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempts.Add(1)
				_, _ = w.Write([]byte(test.responseBody))
			}))

			_, err := client.FetchRate(context.Background(), "EUR/MXN")
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("FetchRate() error = %v, want error containing %q", err, test.wantError)
			}
			if attempts.Load() != 1 {
				t.Errorf("request attempts = %d, want 1", attempts.Load())
			}
		})
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

type interruptedReader struct {
	data []byte
}

func (r *interruptedReader) Read(buffer []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.ErrUnexpectedEOF
	}

	n := copy(buffer, r.data)
	r.data = r.data[n:]
	return n, nil
}
