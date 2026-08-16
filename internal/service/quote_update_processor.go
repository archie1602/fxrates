package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"fxrates/internal/domain"
)

type QuoteUpdateProcessorRepository interface {
	TakeNextPendingUpdate(
		ctx context.Context,
		startedAt time.Time,
	) (domain.QuoteUpdate, bool, error)

	CompleteUpdate(
		ctx context.Context,
		quote domain.Quote,
		completedAt time.Time,
	) error

	FailUpdate(
		ctx context.Context,
		updateID uuid.UUID,
		message string,
		failedAt time.Time,
	) error
}
