package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"fxrates/internal/domain"
)

var (
	ErrQuoteUpdateNotFound    = errors.New("quote update not found")
	ErrQuoteNotFound          = errors.New("completed quote not found")
	ErrIdempotencyKeyConflict = errors.New("idempotency key was already used for another currency pair")
)

type QuoteUpdateRepository interface {
	CreateOrGet(
		ctx context.Context,
		update domain.QuoteUpdate,
		idempotencyKey *uuid.UUID,
	) (domain.QuoteUpdate, error)
	GetByID(ctx context.Context, updateID uuid.UUID) (domain.QuoteUpdateResult, bool, error)
	GetLatest(ctx context.Context, pair domain.Pair) (domain.Quote, bool, error)
}

type QuoteService struct {
	updates       QuoteUpdateRepository
	timeProvider  TimeProvider
	uuidGenerator UUIDGenerator
}

func NewQuoteService(
	updates QuoteUpdateRepository,
	timeProvider TimeProvider,
	uuidGenerator UUIDGenerator,
) *QuoteService {
	return &QuoteService{
		updates:       updates,
		timeProvider:  timeProvider,
		uuidGenerator: uuidGenerator,
	}
}

func (s *QuoteService) CreateQuoteUpdate(
	ctx context.Context,
	pair domain.Pair,
	idempotencyKey *uuid.UUID,
) (domain.QuoteUpdate, error) {
	id, err := s.uuidGenerator.New()
	if err != nil {
		return domain.QuoteUpdate{}, fmt.Errorf("generate quote update id: %w", err)
	}

	now := s.timeProvider.NowUTC()
	update := domain.QuoteUpdate{
		ID:        id,
		Pair:      pair,
		Status:    domain.UpdatePending,
		CreatedAt: now,
		UpdatedAt: now,
	}

	stored, err := s.updates.CreateOrGet(ctx, update, idempotencyKey)
	if err != nil {
		return domain.QuoteUpdate{}, fmt.Errorf("create quote update: %w", err)
	}
	if stored.Pair != pair {
		return domain.QuoteUpdate{}, ErrIdempotencyKeyConflict
	}

	return stored, nil
}

func (s *QuoteService) GetQuoteUpdate(ctx context.Context, updateID uuid.UUID) (domain.QuoteUpdateResult, error) {
	result, found, err := s.updates.GetByID(ctx, updateID)
	if err != nil {
		return domain.QuoteUpdateResult{}, fmt.Errorf("get quote update: %w", err)
	}
	if !found {
		return domain.QuoteUpdateResult{}, ErrQuoteUpdateNotFound
	}

	return result, nil
}

func (s *QuoteService) GetLatest(ctx context.Context, pair domain.Pair) (domain.Quote, error) {
	quote, found, err := s.updates.GetLatest(ctx, pair)
	if err != nil {
		return domain.Quote{}, fmt.Errorf("get latest quote: %w", err)
	}
	if !found {
		return domain.Quote{}, ErrQuoteNotFound
	}

	return quote, nil
}
