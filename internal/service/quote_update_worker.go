package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"fxrates/internal/domain"
)

const (
	rateProviderFailureMessage     = "failed to fetch exchange rate"
	maxWorkerInfrastructureBackoff = 30 * time.Second
)

type workerInfrastructureError struct {
	err error
}

func (e *workerInfrastructureError) Error() string {
	return e.err.Error()
}

func (e *workerInfrastructureError) Unwrap() error {
	return e.err
}

type QuoteUpdateWorker struct {
	updates      QuoteUpdateProcessorRepository
	rates        RateProvider
	timeProvider TimeProvider
	logger       *slog.Logger
	pollInterval time.Duration
}

func NewQuoteUpdateWorker(
	updates QuoteUpdateProcessorRepository,
	rates RateProvider,
	timeProvider TimeProvider,
	logger *slog.Logger,
	pollInterval time.Duration,
) (*QuoteUpdateWorker, error) {
	if updates == nil {
		return nil, errors.New("create quote update worker: update repository is required")
	}
	if rates == nil {
		return nil, errors.New("create quote update worker: rate provider is required")
	}
	if timeProvider == nil {
		return nil, errors.New("create quote update worker: time provider is required")
	}
	if logger == nil {
		return nil, errors.New("create quote update worker: logger is required")
	}
	if pollInterval <= 0 {
		return nil, errors.New("create quote update worker: poll interval must be positive")
	}

	return &QuoteUpdateWorker{
		updates:      updates,
		rates:        rates,
		timeProvider: timeProvider,
		logger:       logger,
		pollInterval: pollInterval,
	}, nil
}

func (w *QuoteUpdateWorker) Run(ctx context.Context) error {
	infrastructureFailures := 0
	for {
		processed, err := w.processNext(ctx)
		if ctx.Err() != nil {
			return nil
		}

		if err != nil {
			delay := w.pollInterval
			var infrastructureErr *workerInfrastructureError
			if errors.As(err, &infrastructureErr) {
				infrastructureFailures++
				delay = workerInfrastructureBackoff(w.pollInterval, infrastructureFailures)
			} else {
				infrastructureFailures = 0
			}

			w.logger.Error("failed to process quote update", "error", err, "retry_in", delay)
			if err := waitForNextPoll(ctx, delay); err != nil {
				return nil
			}
			continue
		}
		infrastructureFailures = 0

		if processed {
			continue
		}

		if err := waitForNextPoll(ctx, w.pollInterval); err != nil {
			return nil
		}
	}
}

func (w *QuoteUpdateWorker) processNext(ctx context.Context) (bool, error) {
	claim, found, err := w.updates.TakeNextPendingUpdate(ctx)
	if err != nil {
		return false, &workerInfrastructureError{err: fmt.Errorf("take pending quote update: %w", err)}
	}
	if !found {
		return false, nil
	}
	update := claim.Update

	snapshot, err := w.rates.FetchRate(ctx, update.Pair)
	if err != nil {
		return true, w.failUpdate(ctx, claim, fmt.Errorf("fetch rate: %w", err))
	}
	if snapshot.Pair != update.Pair {
		return true, w.failUpdate(
			ctx,
			claim,
			fmt.Errorf("provider returned pair %s for requested pair %s", snapshot.Pair, update.Pair),
		)
	}
	validatedRate, err := domain.ParseRate(string(snapshot.Rate))
	if err != nil {
		return true, w.failUpdate(ctx, claim, fmt.Errorf("provider returned an invalid rate: %w", err))
	}

	fetchedAt := w.timeProvider.NowUTC()
	quote := domain.Quote{
		UpdateID:  update.ID,
		Pair:      snapshot.Pair,
		Rate:      validatedRate,
		RateDate:  snapshot.RateDate,
		FetchedAt: fetchedAt,
	}

	completed, err := w.updates.CompleteUpdate(ctx, claim, quote)
	if err != nil {
		return true, &workerInfrastructureError{
			err: fmt.Errorf("complete quote update %s: %w", update.ID, err),
		}
	}
	if !completed {
		w.logger.Warn(
			"discarded quote update result because processing lease is stale",
			"update_id", update.ID,
			"lease_token", claim.LeaseToken,
		)
		return true, nil
	}

	w.logger.Info(
		"quote update completed",
		"update_id", update.ID,
		"pair", update.Pair,
		"rate", snapshot.Rate,
		"rate_date", snapshot.RateDate.Format(time.DateOnly),
	)

	return true, nil
}

func (w *QuoteUpdateWorker) failUpdate(
	ctx context.Context,
	claim ClaimedQuoteUpdate,
	cause error,
) error {
	update := claim.Update
	failed, markFailedErr := w.updates.FailUpdate(
		ctx,
		claim,
		QuoteUpdateFailure{
			Code:    domain.UpdateFailureRateProvider,
			Message: rateProviderFailureMessage,
		},
	)
	if markFailedErr != nil {
		return &workerInfrastructureError{
			err: errors.Join(
				fmt.Errorf("process quote update %s: %w", update.ID, cause),
				fmt.Errorf("mark quote update as failed: %w", markFailedErr),
			),
		}
	}
	if !failed {
		w.logger.Warn(
			"discarded quote update failure because processing lease is stale",
			"update_id", update.ID,
			"lease_token", claim.LeaseToken,
			"cause", cause,
		)
		return nil
	}

	return fmt.Errorf("process quote update %s: %w", update.ID, cause)
}

func workerInfrastructureBackoff(base time.Duration, failures int) time.Duration {
	if failures < 1 {
		return base
	}

	limit := max(base, maxWorkerInfrastructureBackoff)
	ceiling := base
	for failure := 1; failure < failures && ceiling < limit; failure++ {
		if ceiling > limit/2 {
			ceiling = limit
			break
		}
		ceiling *= 2
	}

	floor := ceiling / 2
	if floor == ceiling {
		return ceiling
	}

	return floor + time.Duration(rand.Int64N(int64(ceiling-floor)+1))
}

func waitForNextPoll(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
