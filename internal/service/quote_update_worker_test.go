package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
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
	if repository.completedAt != now {
		t.Errorf("completion time = %v, want %v", repository.completedAt, now)
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
	if !strings.Contains(repository.failureMessage, providerErr.Error()) {
		t.Errorf("failure message = %q, want it to contain %q", repository.failureMessage, providerErr)
	}
	if repository.failedAt != now {
		t.Errorf("failure time = %v, want %v", repository.failedAt, now)
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

func TestQuoteUpdateRecoveryWorkerCalculatesStaleThreshold(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 30, 0, 0, time.UTC)
	processingTimeout := 30 * time.Second
	repository := &recoveryRepositoryStub{}
	worker, err := NewQuoteUpdateRecoveryWorker(
		repository,
		fixedClock{now: now},
		discardLogger(),
		time.Minute,
		processingTimeout,
	)
	if err != nil {
		t.Fatalf("NewQuoteUpdateRecoveryWorker returned unexpected error: %v", err)
	}

	worker.requeueStaleUpdates(context.Background())

	if repository.staleBefore != now.Add(-processingTimeout) {
		t.Errorf("stale threshold = %v, want %v", repository.staleBefore, now.Add(-processingTimeout))
	}
	if repository.requeuedAt != now {
		t.Errorf("requeue time = %v, want %v", repository.requeuedAt, now)
	}
}

type processorRepositoryStub struct {
	update          domain.QuoteUpdate
	found           bool
	completedClaim  ClaimedQuoteUpdate
	completedQuote  domain.Quote
	completedAt     time.Time
	completeCalled  bool
	staleCompletion bool
	failedClaim     ClaimedQuoteUpdate
	failedID        uuid.UUID
	failureMessage  string
	failedAt        time.Time
	staleFailure    bool
}

const testLeaseToken ProcessingLeaseToken = 7

func (s *processorRepositoryStub) TakeNextPendingUpdate(
	context.Context,
	time.Time,
) (ClaimedQuoteUpdate, bool, error) {
	return ClaimedQuoteUpdate{Update: s.update, LeaseToken: testLeaseToken}, s.found, nil
}

func (s *processorRepositoryStub) CompleteUpdate(
	_ context.Context,
	claim ClaimedQuoteUpdate,
	quote domain.Quote,
	completedAt time.Time,
) (bool, error) {
	s.completeCalled = true
	s.completedClaim = claim
	s.completedQuote = quote
	s.completedAt = completedAt
	return !s.staleCompletion, nil
}

func (s *processorRepositoryStub) FailUpdate(
	_ context.Context,
	claim ClaimedQuoteUpdate,
	message string,
	failedAt time.Time,
) (bool, error) {
	s.failedClaim = claim
	s.failedID = claim.Update.ID
	s.failureMessage = message
	s.failedAt = failedAt
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
	staleBefore time.Time
	requeuedAt  time.Time
}

func (s *recoveryRepositoryStub) RequeueStaleProcessingUpdates(
	_ context.Context,
	staleBefore time.Time,
	requeuedAt time.Time,
) (int64, error) {
	s.staleBefore = staleBefore
	s.requeuedAt = requeuedAt
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
