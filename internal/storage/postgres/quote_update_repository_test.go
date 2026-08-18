package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"fxrates/internal/domain"
	"fxrates/internal/service"
)

func TestNewQuoteUpdateRepositoryValidatesDependencies(t *testing.T) {
	t.Run("requires database", func(t *testing.T) {
		if _, err := NewQuoteUpdateRepository(nil, time.Second); err == nil {
			t.Fatal("NewQuoteUpdateRepository returned nil error for a nil database")
		}
	})

	t.Run("requires positive query timeout", func(t *testing.T) {
		if _, err := NewQuoteUpdateRepository(new(pgxpool.Pool), 0); err == nil {
			t.Fatal("NewQuoteUpdateRepository returned nil error for a zero query timeout")
		}
	})

	t.Run("accepts valid dependencies", func(t *testing.T) {
		repository, err := NewQuoteUpdateRepository(new(pgxpool.Pool), time.Second)
		if err != nil {
			t.Fatalf("NewQuoteUpdateRepository returned unexpected error: %v", err)
		}
		if repository.queryTimeout != time.Second {
			t.Errorf("query timeout = %v, want %v", repository.queryTimeout, time.Second)
		}
	})
}

func TestCreateOrGetValidatesDomainValues(t *testing.T) {
	repository := &QuoteUpdateRepository{}
	validID := uuid.MustParse("01900000-0000-7000-8000-000000000001")
	zeroKey := uuid.Nil

	tests := []struct {
		name           string
		updateID       uuid.UUID
		pair           domain.Pair
		idempotencyKey *uuid.UUID
		wantErr        error
	}{
		{
			name:    "zero update ID",
			pair:    "EUR/MXN",
			wantErr: nil,
		},
		{
			name:     "unsupported pair",
			updateID: validID,
			pair:     "EUR/GBP",
			wantErr:  domain.ErrUnsupportedCurrency,
		},
		{
			name:           "zero idempotency key",
			updateID:       validID,
			pair:           "EUR/MXN",
			idempotencyKey: &zeroKey,
			wantErr:        nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := repository.CreateOrGet(
				context.Background(),
				test.updateID,
				test.pair,
				test.idempotencyKey,
			)
			if err == nil {
				t.Fatal("CreateOrGet returned nil error")
			}
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("CreateOrGet error = %v, want wrapped %v", err, test.wantErr)
			}
		})
	}
}

func TestFailUpdateValidatesFailure(t *testing.T) {
	repository := &QuoteUpdateRepository{}
	claim := service.ClaimedQuoteUpdate{}

	if _, err := repository.FailUpdate(
		context.Background(),
		claim,
		service.QuoteUpdateFailure{Code: "unknown", Message: "failed"},
	); !errors.Is(err, domain.ErrInvalidUpdateFailureCode) {
		t.Fatalf("FailUpdate error = %v, want %v", err, domain.ErrInvalidUpdateFailureCode)
	}

	if _, err := repository.FailUpdate(
		context.Background(),
		claim,
		service.QuoteUpdateFailure{Code: domain.UpdateFailureRateProvider},
	); err == nil {
		t.Fatal("FailUpdate returned nil error for an empty failure message")
	}
}

func TestRequeueStaleProcessingUpdatesRequiresPositiveTimeout(t *testing.T) {
	repository := &QuoteUpdateRepository{}

	if _, err := repository.RequeueStaleProcessingUpdates(context.Background(), 0); err == nil {
		t.Fatal("RequeueStaleProcessingUpdates returned nil error for a zero timeout")
	}
}

func TestQueryContextUsesConfiguredTimeout(t *testing.T) {
	timeout := time.Minute
	repository := &QuoteUpdateRepository{queryTimeout: timeout}
	startedAt := time.Now()

	ctx, cancel := repository.queryContext(context.Background())
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("query context does not have a deadline")
	}
	if deadline.Before(startedAt.Add(timeout)) || deadline.After(time.Now().Add(timeout)) {
		t.Errorf("query deadline = %v, want approximately %v after start", deadline, timeout)
	}
}
