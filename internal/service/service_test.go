package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"fxrates/internal/domain"
	"fxrates/internal/service"
)

func TestQuoteServiceCreateQuoteUpdate(t *testing.T) {
	updateID := uuid.MustParse("01900000-0000-7000-8000-000000000001")
	idempotencyKey := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	now := time.Date(2026, time.August, 7, 12, 30, 0, 0, time.UTC)
	pair := domain.Pair("EUR/MXN")
	want := domain.QuoteUpdate{
		ID:        updateID,
		Pair:      pair,
		Status:    domain.UpdatePending,
		CreatedAt: now,
		UpdatedAt: now,
	}

	repository := &quoteUpdateRepositoryStub{}
	quoteService := service.NewQuoteService(
		repository,
		fixedTimeProvider{now: now},
		uuidGeneratorStub{id: updateID},
	)

	got, err := quoteService.CreateQuoteUpdate(context.Background(), pair, &idempotencyKey)
	if err != nil {
		t.Fatalf("CreateQuoteUpdate returned unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("CreateQuoteUpdate() = %+v, want %+v", got, want)
	}
	if repository.createdUpdate != want {
		t.Errorf("stored update = %+v, want %+v", repository.createdUpdate, want)
	}
	if repository.idempotencyKey == nil || *repository.idempotencyKey != idempotencyKey {
		t.Errorf("stored idempotency key = %v, want %v", repository.idempotencyKey, idempotencyKey)
	}
}

func TestQuoteServiceCreateQuoteUpdateWithExistingIdempotencyKey(t *testing.T) {
	idempotencyKey := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	existing := domain.QuoteUpdate{
		ID:     uuid.MustParse("01900000-0000-7000-8000-000000000001"),
		Pair:   "EUR/MXN",
		Status: domain.UpdateProcessing,
	}

	t.Run("returns existing update for the same pair", func(t *testing.T) {
		quoteService := service.NewQuoteService(
			&quoteUpdateRepositoryStub{storedUpdate: existing},
			fixedTimeProvider{},
			uuidGeneratorStub{id: uuid.MustParse("01900000-0000-7000-8000-000000000002")},
		)

		got, err := quoteService.CreateQuoteUpdate(
			context.Background(),
			"EUR/MXN",
			&idempotencyKey,
		)
		if err != nil {
			t.Fatalf("CreateQuoteUpdate returned unexpected error: %v", err)
		}
		if got != existing {
			t.Errorf("CreateQuoteUpdate() = %+v, want %+v", got, existing)
		}
	})

	t.Run("rejects the same key for another pair", func(t *testing.T) {
		quoteService := service.NewQuoteService(
			&quoteUpdateRepositoryStub{storedUpdate: existing},
			fixedTimeProvider{},
			uuidGeneratorStub{id: uuid.MustParse("01900000-0000-7000-8000-000000000002")},
		)

		_, err := quoteService.CreateQuoteUpdate(
			context.Background(),
			"USD/MXN",
			&idempotencyKey,
		)
		if !errors.Is(err, service.ErrIdempotencyKeyConflict) {
			t.Fatalf("CreateQuoteUpdate() error = %v, want %v", err, service.ErrIdempotencyKeyConflict)
		}
	})
}

func TestQuoteServiceCreateQuoteUpdateErrors(t *testing.T) {
	generatorErr := errors.New("generator unavailable")
	repositoryErr := errors.New("database unavailable")

	tests := []struct {
		name          string
		generatorErr  error
		repositoryErr error
		wantErr       error
	}{
		{
			name:         "UUID generator error",
			generatorErr: generatorErr,
			wantErr:      generatorErr,
		},
		{
			name:          "repository error",
			repositoryErr: repositoryErr,
			wantErr:       repositoryErr,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &quoteUpdateRepositoryStub{createErr: test.repositoryErr}
			quoteService := service.NewQuoteService(
				repository,
				fixedTimeProvider{},
				uuidGeneratorStub{
					id:  uuid.MustParse("01900000-0000-7000-8000-000000000001"),
					err: test.generatorErr,
				},
			)

			_, err := quoteService.CreateQuoteUpdate(context.Background(), "EUR/MXN", nil)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("CreateQuoteUpdate() error = %v, want wrapped %v", err, test.wantErr)
			}
		})
	}
}

func TestQuoteServiceGetQuoteUpdate(t *testing.T) {
	updateID := uuid.MustParse("01900000-0000-7000-8000-000000000001")
	want := domain.QuoteUpdateResult{
		Update: domain.QuoteUpdate{
			ID:     updateID,
			Pair:   "EUR/MXN",
			Status: domain.UpdateCompleted,
		},
		Quote: &domain.Quote{
			UpdateID: updateID,
			Pair:     "EUR/MXN",
			Rate:     "19.909",
		},
	}

	t.Run("returns result", func(t *testing.T) {
		repository := &quoteUpdateRepositoryStub{
			getByIDResult: want,
			getByIDFound:  true,
		}
		quoteService := service.NewQuoteService(repository, fixedTimeProvider{}, uuidGeneratorStub{})

		got, err := quoteService.GetQuoteUpdate(context.Background(), updateID)
		if err != nil {
			t.Fatalf("GetQuoteUpdate returned unexpected error: %v", err)
		}
		if got.Update != want.Update || got.Quote == nil || *got.Quote != *want.Quote {
			t.Errorf("GetQuoteUpdate() = %+v, want %+v", got, want)
		}
	})

	t.Run("returns not found error", func(t *testing.T) {
		quoteService := service.NewQuoteService(
			&quoteUpdateRepositoryStub{},
			fixedTimeProvider{},
			uuidGeneratorStub{},
		)

		_, err := quoteService.GetQuoteUpdate(context.Background(), updateID)
		if !errors.Is(err, service.ErrQuoteUpdateNotFound) {
			t.Fatalf("GetQuoteUpdate() error = %v, want %v", err, service.ErrQuoteUpdateNotFound)
		}
	})
}

func TestQuoteServiceGetLatest(t *testing.T) {
	pair := domain.Pair("USD/MXN")
	want := domain.Quote{
		UpdateID:  uuid.MustParse("01900000-0000-7000-8000-000000000002"),
		Pair:      pair,
		Rate:      "17.2364",
		RateDate:  time.Date(2026, time.August, 7, 0, 0, 0, 0, time.UTC),
		FetchedAt: time.Date(2026, time.August, 7, 12, 30, 0, 0, time.UTC),
	}

	t.Run("returns latest quote", func(t *testing.T) {
		repository := &quoteUpdateRepositoryStub{
			getLatestQuote: want,
			getLatestFound: true,
		}
		quoteService := service.NewQuoteService(repository, fixedTimeProvider{}, uuidGeneratorStub{})

		got, err := quoteService.GetLatest(context.Background(), pair)
		if err != nil {
			t.Fatalf("GetLatest returned unexpected error: %v", err)
		}
		if got != want {
			t.Errorf("GetLatest() = %+v, want %+v", got, want)
		}
	})

	t.Run("returns not found error", func(t *testing.T) {
		quoteService := service.NewQuoteService(
			&quoteUpdateRepositoryStub{},
			fixedTimeProvider{},
			uuidGeneratorStub{},
		)

		_, err := quoteService.GetLatest(context.Background(), pair)
		if !errors.Is(err, service.ErrQuoteNotFound) {
			t.Fatalf("GetLatest() error = %v, want %v", err, service.ErrQuoteNotFound)
		}
	})
}

type quoteUpdateRepositoryStub struct {
	createdUpdate  domain.QuoteUpdate
	storedUpdate   domain.QuoteUpdate
	idempotencyKey *uuid.UUID
	createErr      error
	getByIDResult  domain.QuoteUpdateResult
	getByIDFound   bool
	getLatestQuote domain.Quote
	getLatestFound bool
}

func (s *quoteUpdateRepositoryStub) CreateOrGet(
	_ context.Context,
	update domain.QuoteUpdate,
	idempotencyKey *uuid.UUID,
) (domain.QuoteUpdate, error) {
	s.createdUpdate = update
	s.idempotencyKey = idempotencyKey
	if s.createErr != nil {
		return domain.QuoteUpdate{}, s.createErr
	}
	if s.storedUpdate.ID != uuid.Nil {
		return s.storedUpdate, nil
	}

	return update, nil
}

func (s *quoteUpdateRepositoryStub) GetByID(
	context.Context,
	uuid.UUID,
) (domain.QuoteUpdateResult, bool, error) {
	return s.getByIDResult, s.getByIDFound, nil
}

func (s *quoteUpdateRepositoryStub) GetLatest(
	context.Context,
	domain.Pair,
) (domain.Quote, bool, error) {
	return s.getLatestQuote, s.getLatestFound, nil
}

type fixedTimeProvider struct {
	now time.Time
}

func (p fixedTimeProvider) NowUTC() time.Time {
	return p.now
}

type uuidGeneratorStub struct {
	id  uuid.UUID
	err error
}

func (g uuidGeneratorStub) New() (uuid.UUID, error) {
	return g.id, g.err
}
