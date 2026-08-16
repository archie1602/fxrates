package service

import (
	"context"
	"time"

	"fxrates/internal/domain"
)

type RateSnapshot struct {
	Pair     domain.Pair
	Rate     domain.Rate
	RateDate time.Time
}

type RateProvider interface {
	FetchRate(ctx context.Context, pair domain.Pair) (RateSnapshot, error)
}
