package domain

import (
	"errors"
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

var ErrInvalidUpdateStatus = errors.New("invalid quote update status")

func ParseUpdateStatus(value string) (UpdateStatus, error) {
	status := UpdateStatus(value)
	switch status {
	case UpdatePending, UpdateProcessing, UpdateCompleted, UpdateFailed:
		return status, nil
	default:
		return "", ErrInvalidUpdateStatus
	}
}

type UpdateFailureCode string

const UpdateFailureRateProvider UpdateFailureCode = "rate_provider_error"

var ErrInvalidUpdateFailureCode = errors.New("invalid quote update failure code")

func ParseUpdateFailureCode(value string) (UpdateFailureCode, error) {
	code := UpdateFailureCode(value)
	if code != UpdateFailureRateProvider {
		return "", ErrInvalidUpdateFailureCode
	}

	return code, nil
}

type Pair string

type QuoteUpdate struct {
	ID             uuid.UUID
	Pair           Pair
	Status         UpdateStatus
	FailureCode    UpdateFailureCode
	FailureMessage string
	CreatedAt      time.Time
	UpdatedAt      time.Time
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
