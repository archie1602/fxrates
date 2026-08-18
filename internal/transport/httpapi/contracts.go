package httpapi

import (
	"context"

	"github.com/google/uuid"

	"fxrates/internal/domain"
	"fxrates/internal/service"
)

type QuoteUpdateRequester interface {
	CreateQuoteUpdate(
		ctx context.Context,
		pair domain.Pair,
		idempotencyKey *uuid.UUID,
	) (service.CreateQuoteUpdateResult, error)
	GetQuoteUpdate(ctx context.Context, updateID uuid.UUID) (domain.QuoteUpdateResult, error)
	GetLatest(ctx context.Context, pair domain.Pair) (domain.Quote, error)
}

type ReadinessChecker interface {
	Ready(ctx context.Context) error
}
