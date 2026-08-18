package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"fxrates/internal/domain"
)

func TestQuoteUpdateWorkerProcessesPendingUpdate(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 30, 0, 0, time.UTC)
	update := domain.QuoteUpdate{
		ID:     uuid.MustParse("01900000-0000-7000-8000-000000000001"),
		Pair:   "EUR/MXN",
		Status: domain.UpdateProcessing,
	}
	snapshot := RateSnapshot{
		Pair:     update.Pair,
		Rate:     "19.909",
		RateDate: time.Date(2026, time.August, 7, 0, 0, 0, 0, time.UTC),
	}
	repository := &processorRepositoryStub{update: update, found: true}
	provider := &rateProviderStub{snapshot: snapshot}
	worker, err := NewQuoteUpdateWorker(
		repository,
		provider,
		fixedClock{now: now},
		discardLogger(),
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewQuoteUpdateWorker returned unexpected error: %v", err)
	}

	processed, err := worker.processNext(context.Background())
	if err != nil {
		t.Fatalf("processNext returned unexpected error: %v", err)
	}
	if !processed {
		t.Fatal("processNext reported that no update was processed")
	}
	if provider.requestedPair != update.Pair {
		t.Errorf("provider pair = %q, want %q", provider.requestedPair, update.Pair)
	}

	wantQuote := domain.Quote{
		UpdateID:  update.ID,
		Pair:      snapshot.Pair,
		Rate:      snapshot.Rate,
		RateDate:  snapshot.RateDate,
		FetchedAt: now,
	}
	if !repository.completeCalled || repository.completedQuote != wantQuote {
		t.Errorf("completed quote = %+v, want %+v", repository.completedQuote, wantQuote)
	}
	if repository.completedClaim.LeaseToken != testLeaseToken {
		t.Errorf("completion lease token = %d, want %d", repository.completedClaim.LeaseToken, testLeaseToken)
	}
}

func TestQuoteUpdateWorkerMarksProviderFailure(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 30, 0, 0, time.UTC)
	update := domain.QuoteUpdate{
		ID:     uuid.MustParse("01900000-0000-7000-8000-000000000001"),
		Pair:   "EUR/MXN",
		Status: domain.UpdateProcessing,
	}
	providerErr := errors.New("provider unavailable")
	repository := &processorRepositoryStub{update: update, found: true}
	worker, err := NewQuoteUpdateWorker(
		repository,
		&rateProviderStub{err: providerErr},
		fixedClock{now: now},
		discardLogger(),
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewQuoteUpdateWorker returned unexpected error: %v", err)
	}

	processed, err := worker.processNext(context.Background())
	if !processed {
		t.Fatal("processNext reported that no update was processed")
	}
	if !errors.Is(err, providerErr) {
		t.Fatalf("processNext error = %v, want wrapped %v", err, providerErr)
	}
	if repository.failedID != update.ID {
		t.Errorf("failed update ID = %s, want %s", repository.failedID, update.ID)
	}
	if repository.failedClaim.LeaseToken != testLeaseToken {
		t.Errorf("failure lease token = %d, want %d", repository.failedClaim.LeaseToken, testLeaseToken)
	}
	if repository.failure.Code != domain.UpdateFailureRateProvider {
		t.Errorf("failure code = %q, want %q", repository.failure.Code, domain.UpdateFailureRateProvider)
	}
	if repository.failure.Message != rateProviderFailureMessage {
		t.Errorf("failure message = %q, want %q", repository.failure.Message, rateProviderFailureMessage)
	}
	if repository.completeCalled {
		t.Error("CompleteUpdate was called for a failed update")
	}
}

func TestQuoteUpdateWorkerRejectsRateOutsideStoragePrecision(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 30, 0, 0, time.UTC)
	update := domain.QuoteUpdate{
		ID:     uuid.MustParse("01900000-0000-7000-8000-000000000001"),
		Pair:   "EUR/MXN",
		Status: domain.UpdateProcessing,
	}
	repository := &processorRepositoryStub{update: update, found: true}
	worker, err := NewQuoteUpdateWorker(
		repository,
		&rateProviderStub{snapshot: RateSnapshot{
			Pair:     update.Pair,
			Rate:     "1234567890123456789",
			RateDate: now,
		}},
		fixedClock{now: now},
		discardLogger(),
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewQuoteUpdateWorker returned unexpected error: %v", err)
	}

	processed, err := worker.processNext(context.Background())
	if !processed {
		t.Fatal("processNext reported that no update was processed")
	}
	if !errors.Is(err, domain.ErrInvalidRate) {
		t.Fatalf("processNext error = %v, want wrapped %v", err, domain.ErrInvalidRate)
	}
	if repository.failedID != update.ID {
		t.Errorf("failed update ID = %s, want %s", repository.failedID, update.ID)
	}
	if repository.completeCalled {
		t.Error("CompleteUpdate was called for an invalid rate")
	}
}

func TestQuoteUpdateWorkerDiscardsStaleCompletion(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 30, 0, 0, time.UTC)
	update := domain.QuoteUpdate{
		ID:     uuid.MustParse("01900000-0000-7000-8000-000000000001"),
		Pair:   "EUR/MXN",
		Status: domain.UpdateProcessing,
	}
	repository := &processorRepositoryStub{update: update, found: true, staleCompletion: true}
	worker, err := NewQuoteUpdateWorker(
		repository,
		&rateProviderStub{snapshot: RateSnapshot{
			Pair:     update.Pair,
			Rate:     "19.909",
			RateDate: now,
		}},
		fixedClock{now: now},
		discardLogger(),
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewQuoteUpdateWorker returned unexpected error: %v", err)
	}

	processed, err := worker.processNext(context.Background())
	if err != nil {
		t.Fatalf("processNext returned unexpected error: %v", err)
	}
	if !processed {
		t.Fatal("processNext reported that no update was processed")
	}
}

func TestQuoteUpdateWorkerDiscardsFailureForStaleClaim(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 30, 0, 0, time.UTC)
	update := domain.QuoteUpdate{
		ID:     uuid.MustParse("01900000-0000-7000-8000-000000000001"),
		Pair:   "EUR/MXN",
		Status: domain.UpdateProcessing,
	}
	repository := &processorRepositoryStub{update: update, found: true, staleFailure: true}
	worker, err := NewQuoteUpdateWorker(
		repository,
		&rateProviderStub{err: errors.New("provider unavailable")},
		fixedClock{now: now},
		discardLogger(),
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewQuoteUpdateWorker returned unexpected error: %v", err)
	}

	processed, err := worker.processNext(context.Background())
	if err != nil {
		t.Fatalf("processNext returned unexpected error: %v", err)
	}
	if !processed {
		t.Fatal("processNext reported that no update was processed")
	}
}

func TestQuoteUpdateWorkerClassifiesRepositoryErrorsAsInfrastructureFailures(t *testing.T) {
	repositoryErr := errors.New("database unavailable")
	worker, err := NewQuoteUpdateWorker(
		&processorRepositoryStub{takeErr: repositoryErr},
		&rateProviderStub{},
		fixedClock{},
		discardLogger(),
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewQuoteUpdateWorker returned unexpected error: %v", err)
	}

	_, err = worker.processNext(context.Background())
	var infrastructureErr *workerInfrastructureError
	if !errors.As(err, &infrastructureErr) {
		t.Fatalf("processNext error = %v, want workerInfrastructureError", err)
	}
	if !errors.Is(err, repositoryErr) {
		t.Fatalf("processNext error = %v, want wrapped %v", err, repositoryErr)
	}
}

func TestWorkerInfrastructureBackoffIsBounded(t *testing.T) {
	const base = time.Second

	for range 100 {
		first := workerInfrastructureBackoff(base, 1)
		if first < base/2 || first > base {
			t.Fatalf("first backoff = %v, want between %v and %v", first, base/2, base)
		}

		capped := workerInfrastructureBackoff(base, 10)
		if capped < maxWorkerInfrastructureBackoff/2 || capped > maxWorkerInfrastructureBackoff {
			t.Fatalf(
				"capped backoff = %v, want between %v and %v",
				capped,
				maxWorkerInfrastructureBackoff/2,
				maxWorkerInfrastructureBackoff,
			)
		}
	}
}

func TestQuoteUpdateRecoveryWorkerPassesProcessingTimeout(t *testing.T) {
	processingTimeout := 30 * time.Second
	repository := &recoveryRepositoryStub{}
	worker, err := NewQuoteUpdateRecoveryWorker(
		repository,
		discardLogger(),
		time.Minute,
		processingTimeout,
	)
	if err != nil {
		t.Fatalf("NewQuoteUpdateRecoveryWorker returned unexpected error: %v", err)
	}

	worker.requeueStaleUpdates(context.Background())

	if repository.processingTimeout != processingTimeout {
		t.Errorf("processing timeout = %v, want %v", repository.processingTimeout, processingTimeout)
	}
}

type processorRepositoryStub struct {
	update          domain.QuoteUpdate
	found           bool
	completedClaim  ClaimedQuoteUpdate
	completedQuote  domain.Quote
	completeCalled  bool
	staleCompletion bool
	failedClaim     ClaimedQuoteUpdate
	failedID        uuid.UUID
	failure         QuoteUpdateFailure
	staleFailure    bool
	takeErr         error
}

const testLeaseToken ProcessingLeaseToken = 7

func (s *processorRepositoryStub) TakeNextPendingUpdate(
	context.Context,
) (ClaimedQuoteUpdate, bool, error) {
	return ClaimedQuoteUpdate{Update: s.update, LeaseToken: testLeaseToken}, s.found, s.takeErr
}

func (s *processorRepositoryStub) CompleteUpdate(
	_ context.Context,
	claim ClaimedQuoteUpdate,
	quote domain.Quote,
) (bool, error) {
	s.completeCalled = true
	s.completedClaim = claim
	s.completedQuote = quote
	return !s.staleCompletion, nil
}

func (s *processorRepositoryStub) FailUpdate(
	_ context.Context,
	claim ClaimedQuoteUpdate,
	failure QuoteUpdateFailure,
) (bool, error) {
	s.failedClaim = claim
	s.failedID = claim.Update.ID
	s.failure = failure
	return !s.staleFailure, nil
}

type rateProviderStub struct {
	snapshot      RateSnapshot
	err           error
	requestedPair domain.Pair
}

func (s *rateProviderStub) FetchRate(_ context.Context, pair domain.Pair) (RateSnapshot, error) {
	s.requestedPair = pair
	return s.snapshot, s.err
}

type recoveryRepositoryStub struct {
	processingTimeout time.Duration
}

func (s *recoveryRepositoryStub) RequeueStaleProcessingUpdates(
	_ context.Context,
	processingTimeout time.Duration,
) (int64, error) {
	s.processingTimeout = processingTimeout
	return 0, nil
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) NowUTC() time.Time {
	return c.now
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
