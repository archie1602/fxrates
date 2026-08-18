package postgres_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"fxrates/internal/domain"
	"fxrates/internal/service"
	storagepostgres "fxrates/internal/storage/postgres"
)

func TestIntegrationCreateOrGetConcurrentSameIdempotencyKey(t *testing.T) {
	repository := integrationRepository(t)
	idempotencyKey := integrationUUID(100)

	type result struct {
		update  domain.QuoteUpdate
		created bool
		err     error
	}

	const callerCount = 16
	start := make(chan struct{})
	results := make(chan result, callerCount)
	var callers sync.WaitGroup
	callers.Add(callerCount)

	for caller := 1; caller <= callerCount; caller++ {
		updateID := integrationUUID(caller)
		go func() {
			defer callers.Done()
			<-start

			stored, created, err := repository.CreateOrGet(
				context.Background(),
				updateID,
				"EUR/MXN",
				&idempotencyKey,
			)
			results <- result{update: stored, created: created, err: err}
		}()
	}

	close(start)
	callers.Wait()
	close(results)

	var storedID uuid.UUID
	createdCount := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("CreateOrGet returned an error: %v", result.err)
		}
		if result.created {
			createdCount++
		}
		if storedID == uuid.Nil {
			storedID = result.update.ID
			continue
		}
		if result.update.ID != storedID {
			t.Errorf("stored update ID = %s, want %s", result.update.ID, storedID)
		}
	}
	if createdCount != 1 {
		t.Errorf("newly created result count = %d, want 1", createdCount)
	}

	var count int
	if err := integrationDB.QueryRow(
		context.Background(),
		"SELECT count(*) FROM quote_updates WHERE idempotency_key = $1",
		idempotencyKey.String(),
	).Scan(&count); err != nil {
		t.Fatalf("count quote updates by idempotency key: %v", err)
	}
	if count != 1 {
		t.Errorf("stored update count = %d, want 1", count)
	}
}

func TestIntegrationCreateUsesDatabaseTime(t *testing.T) {
	repository := integrationRepository(t)
	before := integrationDatabaseTime(t)

	stored, created, err := repository.CreateOrGet(
		context.Background(),
		integrationUUID(10),
		"EUR/MXN",
		nil,
	)
	if err != nil {
		t.Fatalf("CreateOrGet returned an error: %v", err)
	}
	if !created {
		t.Fatal("CreateOrGet reported a new update as an idempotency replay")
	}
	after := integrationDatabaseTime(t)

	if stored.CreatedAt.Before(before) || stored.CreatedAt.After(after) {
		t.Errorf("created at = %v, want between %v and %v", stored.CreatedAt, before, after)
	}
	if !stored.UpdatedAt.Equal(stored.CreatedAt) {
		t.Errorf("updated at = %v, want created at %v", stored.UpdatedAt, stored.CreatedAt)
	}
}

func TestIntegrationReadyChecksSchemaVersion(t *testing.T) {
	repository := integrationRepository(t)

	if err := repository.Ready(context.Background()); err != nil {
		t.Fatalf("Ready returned an error for the current schema: %v", err)
	}
}

func TestIntegrationTakeNextPendingUpdateSingleClaim(t *testing.T) {
	repository := integrationRepository(t)
	pending := createPendingIntegrationUpdate(
		t,
		repository,
		20,
		"EUR/MXN",
	)

	type result struct {
		claim service.ClaimedQuoteUpdate
		found bool
		err   error
	}

	start := make(chan struct{})
	results := make(chan result, 2)
	var workers sync.WaitGroup
	workers.Add(2)

	for range 2 {
		go func() {
			defer workers.Done()
			<-start

			claim, found, err := repository.TakeNextPendingUpdate(context.Background())
			results <- result{claim: claim, found: found, err: err}
		}()
	}

	close(start)
	workers.Wait()
	close(results)

	foundCount := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("TakeNextPendingUpdate returned an error: %v", result.err)
		}
		if !result.found {
			continue
		}

		foundCount++
		if result.claim.Update.ID != pending.ID {
			t.Errorf("claimed update ID = %s, want %s", result.claim.Update.ID, pending.ID)
		}
		if result.claim.LeaseToken != 1 {
			t.Errorf("lease token = %d, want 1", result.claim.LeaseToken)
		}
	}
	if foundCount != 1 {
		t.Errorf("successful claim count = %d, want 1", foundCount)
	}
}

func TestIntegrationTakeNextPendingUpdateSkipsLockedHead(t *testing.T) {
	repository := integrationRepository(t)
	head := createPendingIntegrationUpdate(
		t,
		repository,
		30,
		"EUR/MXN",
	)
	tail := createPendingIntegrationUpdate(
		t,
		repository,
		31,
		"USD/MXN",
	)

	tx, err := integrationDB.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin locking transaction: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tx.Rollback(ctx)
	})

	var lockedID string
	if err := tx.QueryRow(
		context.Background(),
		"SELECT id::text FROM quote_updates WHERE id = $1 FOR UPDATE",
		head.ID.String(),
	).Scan(&lockedID); err != nil {
		t.Fatalf("lock head quote update: %v", err)
	}

	claim, found, err := repository.TakeNextPendingUpdate(context.Background())
	if err != nil {
		t.Fatalf("TakeNextPendingUpdate returned an error: %v", err)
	}
	if !found {
		t.Fatal("TakeNextPendingUpdate did not find the unlocked update")
	}
	if claim.Update.ID != tail.ID {
		t.Errorf("claimed update ID = %s, want unlocked tail %s", claim.Update.ID, tail.ID)
	}
}

func TestIntegrationStaleLeaseCannotFinalize(t *testing.T) {
	repository := integrationRepository(t)
	createPendingIntegrationUpdate(
		t,
		repository,
		40,
		"EUR/MXN",
	)

	staleClaim := takeIntegrationUpdate(t, repository)
	markIntegrationUpdateStale(t, staleClaim.Update.ID)
	requeued, err := repository.RequeueStaleProcessingUpdates(
		context.Background(),
		time.Hour,
	)
	if err != nil {
		t.Fatalf("RequeueStaleProcessingUpdates returned an error: %v", err)
	}
	if requeued != 1 {
		t.Fatalf("requeued update count = %d, want 1", requeued)
	}

	currentClaim := takeIntegrationUpdate(t, repository)
	if currentClaim.LeaseToken != staleClaim.LeaseToken+1 {
		t.Fatalf(
			"current lease token = %d, want %d",
			currentClaim.LeaseToken,
			staleClaim.LeaseToken+1,
		)
	}

	quote := integrationQuote(t, staleClaim, "18.500000000000", integrationTime(5))
	completed, err := repository.CompleteUpdate(
		context.Background(),
		staleClaim,
		quote,
	)
	if err != nil {
		t.Fatalf("CompleteUpdate with stale lease returned an error: %v", err)
	}
	if completed {
		t.Error("CompleteUpdate accepted a stale lease")
	}

	failed, err := repository.FailUpdate(
		context.Background(),
		staleClaim,
		integrationFailure(),
	)
	if err != nil {
		t.Fatalf("FailUpdate with stale lease returned an error: %v", err)
	}
	if failed {
		t.Error("FailUpdate accepted a stale lease")
	}

	var (
		status            string
		processingVersion int64
		rateCount         int
	)
	if err := integrationDB.QueryRow(
		context.Background(),
		`SELECT status, processing_version FROM quote_updates WHERE id = $1`,
		currentClaim.Update.ID.String(),
	).Scan(&status, &processingVersion); err != nil {
		t.Fatalf("read current processing state: %v", err)
	}
	if status != string(domain.UpdateProcessing) {
		t.Errorf("status = %q, want %q", status, domain.UpdateProcessing)
	}
	if processingVersion != int64(currentClaim.LeaseToken) {
		t.Errorf(
			"processing version = %d, want %d",
			processingVersion,
			currentClaim.LeaseToken,
		)
	}
	if err := integrationDB.QueryRow(
		context.Background(),
		"SELECT count(*) FROM exchange_rates WHERE update_id = $1",
		currentClaim.Update.ID.String(),
	).Scan(&rateCount); err != nil {
		t.Fatalf("count exchange rates: %v", err)
	}
	if rateCount != 0 {
		t.Errorf("stored rate count = %d, want 0", rateCount)
	}
}

func TestIntegrationCompleteUpdatePersistsResult(t *testing.T) {
	repository := integrationRepository(t)
	createPendingIntegrationUpdate(
		t,
		repository,
		50,
		"EUR/MXN",
	)
	claim := takeIntegrationUpdate(t, repository)
	quote := integrationQuote(t, claim, "18.500000000000", integrationTime(2))
	before := integrationDatabaseTime(t)

	completed, err := repository.CompleteUpdate(
		context.Background(),
		claim,
		quote,
	)
	if err != nil {
		t.Fatalf("CompleteUpdate returned an error: %v", err)
	}
	if !completed {
		t.Fatal("CompleteUpdate did not complete the current claim")
	}
	after := integrationDatabaseTime(t)

	result, found, err := repository.GetByID(context.Background(), claim.Update.ID)
	if err != nil {
		t.Fatalf("GetByID returned an error: %v", err)
	}
	if !found {
		t.Fatal("GetByID did not find the completed update")
	}
	if result.Update.Status != domain.UpdateCompleted {
		t.Errorf("status = %q, want %q", result.Update.Status, domain.UpdateCompleted)
	}
	if result.Update.UpdatedAt.Before(before) || result.Update.UpdatedAt.After(after) {
		t.Errorf("updated at = %v, want between %v and %v", result.Update.UpdatedAt, before, after)
	}
	if result.Quote == nil {
		t.Fatal("GetByID returned no quote for a completed update")
	}
	if result.Quote.UpdateID != quote.UpdateID {
		t.Errorf("quote update ID = %s, want %s", result.Quote.UpdateID, quote.UpdateID)
	}
	if result.Quote.Pair != quote.Pair {
		t.Errorf("quote pair = %q, want %q", result.Quote.Pair, quote.Pair)
	}
	if result.Quote.Rate != quote.Rate {
		t.Errorf("quote rate = %q, want %q", result.Quote.Rate, quote.Rate)
	}
	if !result.Quote.RateDate.Equal(quote.RateDate) {
		t.Errorf("quote rate date = %v, want %v", result.Quote.RateDate, quote.RateDate)
	}
	if !result.Quote.FetchedAt.Equal(quote.FetchedAt) {
		t.Errorf("quote fetched at = %v, want %v", result.Quote.FetchedAt, quote.FetchedAt)
	}

	var rateCount int
	if err := integrationDB.QueryRow(
		context.Background(),
		"SELECT count(*) FROM exchange_rates WHERE update_id = $1",
		claim.Update.ID.String(),
	).Scan(&rateCount); err != nil {
		t.Fatalf("count exchange rates: %v", err)
	}
	if rateCount != 1 {
		t.Errorf("stored rate count = %d, want 1", rateCount)
	}
}

func TestIntegrationCompleteUpdateRollsBackOnRateInsertFailure(t *testing.T) {
	repository := integrationRepository(t)
	createPendingIntegrationUpdate(
		t,
		repository,
		60,
		"EUR/MXN",
	)
	claim := takeIntegrationUpdate(t, repository)
	quote := domain.Quote{
		UpdateID:  claim.Update.ID,
		Pair:      claim.Update.Pair,
		Rate:      domain.Rate("0"),
		RateDate:  integrationTime(2),
		FetchedAt: integrationTime(3),
	}

	completed, err := repository.CompleteUpdate(
		context.Background(),
		claim,
		quote,
	)
	if err == nil {
		t.Fatal("CompleteUpdate returned nil error for an invalid database rate")
	}
	if completed {
		t.Error("CompleteUpdate completed an update after a rate insert failure")
	}

	result, found, err := repository.GetByID(context.Background(), claim.Update.ID)
	if err != nil {
		t.Fatalf("GetByID returned an error: %v", err)
	}
	if !found {
		t.Fatal("GetByID did not find the update after a rate insert failure")
	}
	if result.Update.Status != domain.UpdateProcessing {
		t.Errorf("status = %q, want %q", result.Update.Status, domain.UpdateProcessing)
	}
	if result.Quote != nil {
		t.Error("GetByID returned a quote after a rate insert failure")
	}
}

func TestIntegrationRequeueStaleProcessingUpdates(t *testing.T) {
	repository := integrationRepository(t)
	staleUpdate := createPendingIntegrationUpdate(
		t,
		repository,
		70,
		"EUR/MXN",
	)
	freshUpdate := createPendingIntegrationUpdate(
		t,
		repository,
		71,
		"USD/MXN",
	)

	staleClaim := takeIntegrationUpdate(t, repository)
	freshClaim := takeIntegrationUpdate(t, repository)
	if staleClaim.Update.ID != staleUpdate.ID {
		t.Fatalf("first claimed update ID = %s, want %s", staleClaim.Update.ID, staleUpdate.ID)
	}
	if freshClaim.Update.ID != freshUpdate.ID {
		t.Fatalf("second claimed update ID = %s, want %s", freshClaim.Update.ID, freshUpdate.ID)
	}
	markIntegrationUpdateStale(t, staleClaim.Update.ID)

	requeued, err := repository.RequeueStaleProcessingUpdates(
		context.Background(),
		time.Hour,
	)
	if err != nil {
		t.Fatalf("RequeueStaleProcessingUpdates returned an error: %v", err)
	}
	if requeued != 1 {
		t.Fatalf("requeued update count = %d, want 1", requeued)
	}

	reclaimed := takeIntegrationUpdate(t, repository)
	if reclaimed.Update.ID != staleUpdate.ID {
		t.Fatalf("reclaimed update ID = %s, want %s", reclaimed.Update.ID, staleUpdate.ID)
	}
	if reclaimed.LeaseToken != staleClaim.LeaseToken+1 {
		t.Errorf(
			"reclaimed lease token = %d, want %d",
			reclaimed.LeaseToken,
			staleClaim.LeaseToken+1,
		)
	}

	failed, err := repository.FailUpdate(
		context.Background(),
		reclaimed,
		integrationFailure(),
	)
	if err != nil {
		t.Fatalf("FailUpdate returned an error: %v", err)
	}
	if !failed {
		t.Fatal("FailUpdate did not fail the current claim")
	}

	failedResult, found, err := repository.GetByID(context.Background(), reclaimed.Update.ID)
	if err != nil {
		t.Fatalf("GetByID returned an error for failed update: %v", err)
	}
	if !found {
		t.Fatal("GetByID did not find the failed update")
	}
	if failedResult.Update.Status != domain.UpdateFailed {
		t.Errorf("failed status = %q, want %q", failedResult.Update.Status, domain.UpdateFailed)
	}
	if failedResult.Update.FailureCode != domain.UpdateFailureRateProvider {
		t.Errorf("failure code = %q, want %q", failedResult.Update.FailureCode, domain.UpdateFailureRateProvider)
	}
	if failedResult.Update.FailureMessage != "failed to fetch exchange rate" {
		t.Errorf(
			"failure message = %q, want %q",
			failedResult.Update.FailureMessage,
			"failed to fetch exchange rate",
		)
	}

	freshResult, found, err := repository.GetByID(context.Background(), freshClaim.Update.ID)
	if err != nil {
		t.Fatalf("GetByID returned an error for fresh update: %v", err)
	}
	if !found {
		t.Fatal("GetByID did not find the fresh update")
	}
	if freshResult.Update.Status != domain.UpdateProcessing {
		t.Errorf("fresh update status = %q, want %q", freshResult.Update.Status, domain.UpdateProcessing)
	}
}

func TestIntegrationGetLatestReturnsNewestCompletedQuote(t *testing.T) {
	repository := integrationRepository(t)

	createCompletedIntegrationQuote(
		t,
		repository,
		80,
		"EUR/MXN",
		"18.000000000000",
		integrationTime(1),
	)
	newQuote := createCompletedIntegrationQuote(
		t,
		repository,
		81,
		"EUR/MXN",
		"19.000000000000",
		integrationTime(3),
	)
	createCompletedIntegrationQuote(
		t,
		repository,
		82,
		"USD/MXN",
		"20.000000000000",
		integrationTime(5),
	)
	createPendingIntegrationUpdate(
		t,
		repository,
		83,
		"EUR/MXN",
	)

	latest, found, err := repository.GetLatest(context.Background(), "EUR/MXN")
	if err != nil {
		t.Fatalf("GetLatest returned an error: %v", err)
	}
	if !found {
		t.Fatal("GetLatest did not find a completed quote")
	}
	if latest.UpdateID != newQuote.UpdateID {
		t.Errorf("latest update ID = %s, want %s", latest.UpdateID, newQuote.UpdateID)
	}
	if latest.Rate != newQuote.Rate {
		t.Errorf("latest rate = %q, want %q", latest.Rate, newQuote.Rate)
	}

	if _, found, err := repository.GetLatest(
		context.Background(),
		"MXN/EUR",
	); err != nil {
		t.Fatalf("GetLatest for missing pair returned an error: %v", err)
	} else if found {
		t.Error("GetLatest found a quote for a missing pair")
	}

	if _, found, err := repository.GetByID(
		context.Background(),
		integrationUUID(99),
	); err != nil {
		t.Fatalf("GetByID for missing update returned an error: %v", err)
	} else if found {
		t.Error("GetByID found a missing update")
	}
}

func createPendingIntegrationUpdate(
	t *testing.T,
	repository *storagepostgres.QuoteUpdateRepository,
	sequence int,
	pair domain.Pair,
) domain.QuoteUpdate {
	t.Helper()

	stored, created, err := repository.CreateOrGet(
		context.Background(),
		integrationUUID(sequence),
		pair,
		nil,
	)
	if err != nil {
		t.Fatalf("create pending quote update: %v", err)
	}
	if !created {
		t.Fatal("create pending quote update: repository reported an idempotency replay")
	}

	return stored
}

func takeIntegrationUpdate(
	t *testing.T,
	repository *storagepostgres.QuoteUpdateRepository,
) service.ClaimedQuoteUpdate {
	t.Helper()

	claim, found, err := repository.TakeNextPendingUpdate(context.Background())
	if err != nil {
		t.Fatalf("take pending quote update: %v", err)
	}
	if !found {
		t.Fatal("take pending quote update: no update found")
	}

	return claim
}

func createCompletedIntegrationQuote(
	t *testing.T,
	repository *storagepostgres.QuoteUpdateRepository,
	sequence int,
	pair domain.Pair,
	rate string,
	fetchedAt time.Time,
) domain.Quote {
	t.Helper()

	createPendingIntegrationUpdate(
		t,
		repository,
		sequence,
		pair,
	)
	claim := takeIntegrationUpdate(t, repository)
	quote := integrationQuote(t, claim, rate, fetchedAt)

	completed, err := repository.CompleteUpdate(
		context.Background(),
		claim,
		quote,
	)
	if err != nil {
		t.Fatalf("complete quote update: %v", err)
	}
	if !completed {
		t.Fatal("complete quote update: claim was not completed")
	}

	return quote
}

func markIntegrationUpdateStale(t *testing.T, updateID uuid.UUID) {
	t.Helper()

	if _, err := integrationDB.Exec(
		context.Background(),
		`UPDATE quote_updates
		 SET updated_at = CURRENT_TIMESTAMP - INTERVAL '2 hours'
		 WHERE id = $1`,
		updateID.String(),
	); err != nil {
		t.Fatalf("mark quote update stale: %v", err)
	}
}

func integrationDatabaseTime(t *testing.T) time.Time {
	t.Helper()

	var now time.Time
	if err := integrationDB.QueryRow(context.Background(), "SELECT CURRENT_TIMESTAMP").Scan(&now); err != nil {
		t.Fatalf("read PostgreSQL time: %v", err)
	}

	return now
}

func integrationFailure() service.QuoteUpdateFailure {
	return service.QuoteUpdateFailure{
		Code:    domain.UpdateFailureRateProvider,
		Message: "failed to fetch exchange rate",
	}
}

func integrationQuote(
	t *testing.T,
	claim service.ClaimedQuoteUpdate,
	rate string,
	fetchedAt time.Time,
) domain.Quote {
	t.Helper()

	parsedRate, err := domain.ParseRate(rate)
	if err != nil {
		t.Fatalf("parse integration rate: %v", err)
	}

	return domain.Quote{
		UpdateID:  claim.Update.ID,
		Pair:      claim.Update.Pair,
		Rate:      parsedRate,
		RateDate:  time.Date(2026, time.August, 18, 0, 0, 0, 0, time.UTC),
		FetchedAt: fetchedAt,
	}
}

func integrationUUID(sequence int) uuid.UUID {
	return uuid.MustParse(fmt.Sprintf("00000000-0000-4000-8000-%012d", sequence))
}

func integrationTime(offsetSeconds int) time.Time {
	return time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC).
		Add(time.Duration(offsetSeconds) * time.Second)
}
