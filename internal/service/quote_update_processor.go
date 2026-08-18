package service

import (
	"context"

	"fxrates/internal/domain"
)

type ProcessingLeaseToken int64

type ClaimedQuoteUpdate struct {
	Update     domain.QuoteUpdate
	LeaseToken ProcessingLeaseToken
}

type QuoteUpdateFailure struct {
	Code    domain.UpdateFailureCode
	Message string
}

type QuoteUpdateProcessorRepository interface {
	TakeNextPendingUpdate(
		ctx context.Context,
	) (ClaimedQuoteUpdate, bool, error)

	CompleteUpdate(
		ctx context.Context,
		claim ClaimedQuoteUpdate,
		quote domain.Quote,
	) (bool, error)

	FailUpdate(
		ctx context.Context,
		claim ClaimedQuoteUpdate,
		failure QuoteUpdateFailure,
	) (bool, error)
}
