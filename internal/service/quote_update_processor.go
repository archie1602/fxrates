package service

import (
	"context"
	"time"

	"fxrates/internal/domain"
)

type ProcessingLeaseToken int64

type ClaimedQuoteUpdate struct {
	Update     domain.QuoteUpdate
	LeaseToken ProcessingLeaseToken
}

type QuoteUpdateProcessorRepository interface {
	TakeNextPendingUpdate(
		ctx context.Context,
		startedAt time.Time,
	) (ClaimedQuoteUpdate, bool, error)

	CompleteUpdate(
		ctx context.Context,
		claim ClaimedQuoteUpdate,
		quote domain.Quote,
		completedAt time.Time,
	) (bool, error)

	FailUpdate(
		ctx context.Context,
		claim ClaimedQuoteUpdate,
		message string,
		failedAt time.Time,
	) (bool, error)
}
