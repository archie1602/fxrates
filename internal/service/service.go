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
		updateID uuid.UUID,
		pair domain.Pair,
		idempotencyKey *uuid.UUID,
	) (domain.QuoteUpdate, bool, error)
	GetByID(ctx context.Context, updateID uuid.UUID) (domain.QuoteUpdateResult, bool, error)
	GetLatest(ctx context.Context, pair domain.Pair) (domain.Quote, bool, error)
}

type QuoteService struct {
	updates       QuoteUpdateRepository
	uuidGenerator UUIDGenerator
}

type CreateQuoteUpdateResult struct {
	Update  domain.QuoteUpdate
	Created bool
}

func NewQuoteService(
	updates QuoteUpdateRepository,
	uuidGenerator UUIDGenerator,
) *QuoteService {
	return &QuoteService{
		updates:       updates,
		uuidGenerator: uuidGenerator,
	}
}

func (s *QuoteService) CreateQuoteUpdate(
	ctx context.Context,
	pair domain.Pair,
	idempotencyKey *uuid.UUID,
) (CreateQuoteUpdateResult, error) {
	validatedPair, err := domain.ParsePair(string(pair))
	if err != nil {
		return CreateQuoteUpdateResult{}, fmt.Errorf("validate quote update pair: %w", err)
	}

	id, err := s.uuidGenerator.New()
	if err != nil {
		return CreateQuoteUpdateResult{}, fmt.Errorf("generate quote update id: %w", err)
	}
	if id == uuid.Nil {
		return CreateQuoteUpdateResult{}, errors.New("generate quote update id: generator returned a zero UUID")
	}

	stored, created, err := s.updates.CreateOrGet(ctx, id, validatedPair, idempotencyKey)
	if err != nil {
		return CreateQuoteUpdateResult{}, fmt.Errorf("create quote update: %w", err)
	}
	if stored.Pair != validatedPair {
		return CreateQuoteUpdateResult{}, ErrIdempotencyKeyConflict
	}

	return CreateQuoteUpdateResult{Update: stored, Created: created}, nil
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
