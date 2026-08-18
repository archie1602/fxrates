package service

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

type QuoteUpdateRecoveryRepository interface {
	RequeueStaleProcessingUpdates(
		ctx context.Context,
		processingTimeout time.Duration,
	) (int64, error)
}

type QuoteUpdateRecoveryWorker struct {
	updates           QuoteUpdateRecoveryRepository
	logger            *slog.Logger
	recoveryInterval  time.Duration
	processingTimeout time.Duration
}

func NewQuoteUpdateRecoveryWorker(
	updates QuoteUpdateRecoveryRepository,
	logger *slog.Logger,
	recoveryInterval time.Duration,
	processingTimeout time.Duration,
) (*QuoteUpdateRecoveryWorker, error) {
	if updates == nil {
		return nil, errors.New("create quote update recovery worker: update repository is required")
	}
	if logger == nil {
		return nil, errors.New("create quote update recovery worker: logger is required")
	}
	if recoveryInterval <= 0 {
		return nil, errors.New("create quote update recovery worker: recovery interval must be positive")
	}
	if processingTimeout <= 0 {
		return nil, errors.New("create quote update recovery worker: processing timeout must be positive")
	}

	return &QuoteUpdateRecoveryWorker{
		updates:           updates,
		logger:            logger,
		recoveryInterval:  recoveryInterval,
		processingTimeout: processingTimeout,
	}, nil
}

func (w *QuoteUpdateRecoveryWorker) Run(ctx context.Context) error {
	if ctx.Err() != nil {
		return nil
	}

	w.requeueStaleUpdates(ctx)

	ticker := time.NewTicker(w.recoveryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			w.requeueStaleUpdates(ctx)
		}
	}
}

func (w *QuoteUpdateRecoveryWorker) requeueStaleUpdates(ctx context.Context) {
	requeued, err := w.updates.RequeueStaleProcessingUpdates(ctx, w.processingTimeout)
	if err != nil {
		if ctx.Err() == nil {
			w.logger.Error("failed to recover stale quote updates", "error", err)
		}
		return
	}

	if requeued > 0 {
		w.logger.Info("stale quote updates requeued", "count", requeued)
	}
}
