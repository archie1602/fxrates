package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"fxrates/internal/domain"
	"fxrates/internal/service"
	"fxrates/internal/transport/httpapi"
)

func TestCreateQuoteUpdate(t *testing.T) {
	updateID := uuid.MustParse("01900000-0000-7000-8000-000000000001")
	idempotencyKey := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	createdAt := time.Date(2026, time.August, 7, 12, 30, 0, 0, time.UTC)
	serviceStub := &quoteServiceStub{
		createResult: domain.QuoteUpdate{
			ID:        updateID,
			Pair:      "EUR/MXN",
			Status:    domain.UpdatePending,
			CreatedAt: createdAt,
		},
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/quote-updates",
		strings.NewReader(`{"pair":"eur/mxn"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", idempotencyKey.String())
	response := httptest.NewRecorder()

	newTestHandler(serviceStub).ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusAccepted)
	}
	if serviceStub.createdPair != "EUR/MXN" {
		t.Errorf("service pair = %q, want %q", serviceStub.createdPair, "EUR/MXN")
	}
	if serviceStub.idempotencyKey == nil || *serviceStub.idempotencyKey != idempotencyKey {
		t.Errorf("service idempotency key = %v, want %v", serviceStub.idempotencyKey, idempotencyKey)
	}
	wantLocation := "/api/v1/quote-updates/" + updateID.String()
	if response.Header().Get("Location") != wantLocation {
		t.Errorf("Location = %q, want %q", response.Header().Get("Location"), wantLocation)
	}
}

func TestReadiness(t *testing.T) {
	t.Run("returns OK when PostgreSQL is ready", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/readyz", nil)

		newTestHandlerWithReadiness(&quoteServiceStub{}, readinessCheckerStub{}).
			ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
		}
	})

	t.Run("returns unavailable when PostgreSQL is not ready", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/readyz", nil)

		newTestHandlerWithReadiness(
			&quoteServiceStub{},
			readinessCheckerStub{err: errors.New("database unavailable")},
		).ServeHTTP(response, request)

		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
		}
		if code := decodeErrorCode(t, response); code != "not_ready" {
			t.Errorf("error code = %q, want %q", code, "not_ready")
		}
	})
}

func TestCreateQuoteUpdateIdempotencyErrors(t *testing.T) {
	tests := []struct {
		name            string
		idempotencyKey  string
		serviceErr      error
		wantStatus      int
		wantCode        string
		wantCreateCalls int
	}{
		{
			name:            "rejects invalid key",
			idempotencyKey:  "not-a-uuid",
			wantStatus:      http.StatusBadRequest,
			wantCode:        "invalid_idempotency_key",
			wantCreateCalls: 0,
		},
		{
			name:            "maps key conflict to unprocessable content",
			idempotencyKey:  "10000000-0000-4000-8000-000000000001",
			serviceErr:      service.ErrIdempotencyKeyConflict,
			wantStatus:      http.StatusUnprocessableEntity,
			wantCode:        "idempotency_key_conflict",
			wantCreateCalls: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serviceStub := &quoteServiceStub{createErr: test.serviceErr}
			request := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/quote-updates",
				strings.NewReader(`{"pair":"EUR/MXN"}`),
			)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", test.idempotencyKey)
			response := httptest.NewRecorder()

			newTestHandler(serviceStub).ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if code := decodeErrorCode(t, response); code != test.wantCode {
				t.Errorf("error code = %q, want %q", code, test.wantCode)
			}
			if serviceStub.createCalls != test.wantCreateCalls {
				t.Errorf("CreateQuoteUpdate calls = %d, want %d", serviceStub.createCalls, test.wantCreateCalls)
			}
		})
	}
}

func TestGetFailedQuoteUpdate(t *testing.T) {
	updateID := uuid.MustParse("01900000-0000-7000-8000-000000000001")
	updatedAt := time.Date(2026, time.August, 7, 12, 30, 0, 0, time.UTC)
	failureMessage := "fetch rate: provider unavailable"
	serviceStub := &quoteServiceStub{
		getResult: domain.QuoteUpdateResult{
			Update: domain.QuoteUpdate{
				ID:        updateID,
				Pair:      "EUR/MXN",
				Status:    domain.UpdateFailed,
				Error:     failureMessage,
				UpdatedAt: updatedAt,
			},
		},
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/quote-updates/"+updateID.String(),
		nil,
	)
	response := httptest.NewRecorder()

	newTestHandler(serviceStub).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var body struct {
		Status       domain.UpdateStatus `json:"status"`
		UpdatedAt    time.Time           `json:"updated_at"`
		ErrorMessage *string             `json:"error_message"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != domain.UpdateFailed {
		t.Errorf("status body = %q, want %q", body.Status, domain.UpdateFailed)
	}
	if body.UpdatedAt != updatedAt {
		t.Errorf("updated_at = %v, want %v", body.UpdatedAt, updatedAt)
	}
	if body.ErrorMessage == nil || *body.ErrorMessage != failureMessage {
		t.Errorf("error_message = %v, want %q", body.ErrorMessage, failureMessage)
	}
}

func TestGetLatestQuote(t *testing.T) {
	fetchedAt := time.Date(2026, time.August, 7, 12, 30, 0, 0, time.UTC)
	serviceStub := &quoteServiceStub{
		latestResult: domain.Quote{
			UpdateID:  uuid.MustParse("01900000-0000-7000-8000-000000000001"),
			Pair:      "USD/MXN",
			Rate:      "17.2364",
			RateDate:  time.Date(2026, time.August, 7, 0, 0, 0, 0, time.UTC),
			FetchedAt: fetchedAt,
		},
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/quotes/latest?pair=usd%2Fmxn",
		nil,
	)
	response := httptest.NewRecorder()

	newTestHandler(serviceStub).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if serviceStub.latestPair != "USD/MXN" {
		t.Errorf("service pair = %q, want %q", serviceStub.latestPair, "USD/MXN")
	}
	var body struct {
		Rate      string    `json:"rate"`
		RateDate  string    `json:"rate_date"`
		FetchedAt time.Time `json:"fetched_at"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Rate != "17.2364" || body.RateDate != "2026-08-07" || body.FetchedAt != fetchedAt {
		t.Errorf("latest quote response = %+v", body)
	}
}

func TestGetQuoteUpdateNotFound(t *testing.T) {
	updateID := uuid.MustParse("01900000-0000-7000-8000-000000000001")
	serviceStub := &quoteServiceStub{getErr: service.ErrQuoteUpdateNotFound}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/quote-updates/"+updateID.String(),
		nil,
	)
	response := httptest.NewRecorder()

	newTestHandler(serviceStub).ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
	if code := decodeErrorCode(t, response); code != "quote_update_not_found" {
		t.Errorf("error code = %q, want %q", code, "quote_update_not_found")
	}
}

func decodeErrorCode(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	return body.Error.Code
}

type quoteServiceStub struct {
	createResult   domain.QuoteUpdate
	createErr      error
	createCalls    int
	createdPair    domain.Pair
	idempotencyKey *uuid.UUID
	getResult      domain.QuoteUpdateResult
	getErr         error
	latestResult   domain.Quote
	latestErr      error
	latestPair     domain.Pair
}

type readinessCheckerStub struct {
	err error
}

func (s readinessCheckerStub) Ready(context.Context) error {
	return s.err
}

func (s *quoteServiceStub) CreateQuoteUpdate(
	_ context.Context,
	pair domain.Pair,
	idempotencyKey *uuid.UUID,
) (domain.QuoteUpdate, error) {
	s.createCalls++
	s.createdPair = pair
	s.idempotencyKey = idempotencyKey
	return s.createResult, s.createErr
}

func (s *quoteServiceStub) GetQuoteUpdate(
	context.Context,
	uuid.UUID,
) (domain.QuoteUpdateResult, error) {
	return s.getResult, s.getErr
}

func (s *quoteServiceStub) GetLatest(_ context.Context, pair domain.Pair) (domain.Quote, error) {
	s.latestPair = pair
	return s.latestResult, s.latestErr
}

func newTestHandler(serviceStub httpapi.QuoteUpdateRequester) http.Handler {
	return newTestHandlerWithReadiness(serviceStub, readinessCheckerStub{})
}

func newTestHandlerWithReadiness(
	serviceStub httpapi.QuoteUpdateRequester,
	readiness httpapi.ReadinessChecker,
) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return httpapi.NewHandler(serviceStub, readiness, logger).Routes()
}
