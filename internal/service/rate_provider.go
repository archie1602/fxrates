package service

import (
	"context"
	"time"

	"fxrates/internal/domain"
)

// RateSnapshot is a provider's rate for a currency pair on its rate date.
// RateDate is a calendar date reported by the provider, not the time at
// which this service fetched or persisted the rate.
type RateSnapshot struct {
	Pair     domain.Pair
	Rate     domain.Rate
	RateDate time.Time
}

// RateProvider returns the current exchange rate for a currency pair.
type RateProvider interface {
	FetchRate(ctx context.Context, pair domain.Pair) (RateSnapshot, error)
}
