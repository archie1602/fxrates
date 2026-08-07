package domain

import (
	"time"

	"github.com/google/uuid"
)

type UpdateStatus string

const (
	UpdatePending    UpdateStatus = "pending"
	UpdateProcessing UpdateStatus = "processing"
	UpdateCompleted  UpdateStatus = "completed"
	UpdateFailed     UpdateStatus = "failed"
)

type Pair string

type QuoteUpdate struct {
	ID        uuid.UUID
	Pair      Pair
	Status    UpdateStatus
	Error     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Quote struct {
	UpdateID  uuid.UUID
	Pair      Pair
	Rate      Rate
	RateDate  time.Time
	FetchedAt time.Time
}

type QuoteUpdateResult struct {
	Update QuoteUpdate
	Quote  *Quote
}
