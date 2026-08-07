package service

import (
	"time"

	"github.com/google/uuid"
)

type TimeProvider interface {
	NowUTC() time.Time
}

type UUIDGenerator interface {
	New() (uuid.UUID, error)
}

type SystemTimeProvider struct{}

func (SystemTimeProvider) NowUTC() time.Time {
	return time.Now().UTC()
}

type UUIDv7Generator struct{}

func (UUIDv7Generator) New() (uuid.UUID, error) {
	return uuid.NewV7()
}
