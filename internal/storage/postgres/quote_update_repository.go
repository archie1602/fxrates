package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"fxrates/internal/domain"
	"fxrates/internal/service"
)

type QuoteUpdateRepository struct {
	database     *pgxpool.Pool
	queryTimeout time.Duration
}

func NewQuoteUpdateRepository(
	database *pgxpool.Pool,
	queryTimeout time.Duration,
) (*QuoteUpdateRepository, error) {
	if database == nil {
		return nil, errors.New("create quote update repository: database is required")
	}
	if queryTimeout <= 0 {
		return nil, errors.New("create quote update repository: query timeout must be positive")
	}

	return &QuoteUpdateRepository{
		database:     database,
		queryTimeout: queryTimeout,
	}, nil
}

func (r *QuoteUpdateRepository) CreateOrGet(
	ctx context.Context,
	update domain.QuoteUpdate,
	idempotencyKey *uuid.UUID,
) (domain.QuoteUpdate, error) {
	ctx, cancel := r.queryContext(ctx)
	defer cancel()

	const query = `
		INSERT INTO quote_updates (
			id,
			pair,
			status,
			idempotency_key,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING
			id::text,
			pair,
			status,
			error_message,
			created_at,
			updated_at
	`

	var keyValue any
	if idempotencyKey != nil {
		keyValue = idempotencyKey.String()
	}

	stored, err := scanQuoteUpdate(r.database.QueryRow(
		ctx,
		query,
		update.ID.String(),
		update.Pair,
		update.Status,
		keyValue,
		update.CreatedAt,
		update.UpdatedAt,
	))
	if err == nil {
		return stored, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.QuoteUpdate{}, fmt.Errorf("insert quote update: %w", err)
	}
	if idempotencyKey == nil {
		return domain.QuoteUpdate{}, errors.New("insert quote update: conflict without an idempotency key")
	}

	existing, found, err := r.getByIdempotencyKey(ctx, *idempotencyKey)
	if err != nil {
		return domain.QuoteUpdate{}, err
	}
	if !found {
		return domain.QuoteUpdate{}, errors.New("insert quote update: conflicting idempotency key was not found")
	}

	return existing, nil
}

func (r *QuoteUpdateRepository) getByIdempotencyKey(
	ctx context.Context,
	idempotencyKey uuid.UUID,
) (domain.QuoteUpdate, bool, error) {
	const query = `
		SELECT
			id::text,
			pair,
			status,
			error_message,
			created_at,
			updated_at
		FROM quote_updates
		WHERE idempotency_key = $1
	`

	update, err := scanQuoteUpdate(r.database.QueryRow(ctx, query, idempotencyKey.String()))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.QuoteUpdate{}, false, nil
	}
	if err != nil {
		return domain.QuoteUpdate{}, false, fmt.Errorf("select quote update by idempotency key: %w", err)
	}

	return update, true, nil
}

func scanQuoteUpdate(row pgx.Row) (domain.QuoteUpdate, error) {
	var (
		id           string
		pair         string
		status       string
		errorMessage pgtype.Text
		createdAt    pgtype.Timestamptz
		updatedAt    pgtype.Timestamptz
	)

	err := row.Scan(
		&id,
		&pair,
		&status,
		&errorMessage,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return domain.QuoteUpdate{}, err
	}

	parsedID, err := uuid.Parse(id)
	if err != nil {
		return domain.QuoteUpdate{}, fmt.Errorf("parse stored quote update id: %w", err)
	}

	update := domain.QuoteUpdate{
		ID:        parsedID,
		Pair:      domain.Pair(pair),
		Status:    domain.UpdateStatus(status),
		CreatedAt: createdAt.Time,
		UpdatedAt: updatedAt.Time,
	}
	if errorMessage.Valid {
		update.Error = errorMessage.String
	}

	return update, nil
}

func (r *QuoteUpdateRepository) GetByID(
	ctx context.Context,
	updateID uuid.UUID,
) (domain.QuoteUpdateResult, bool, error) {
	ctx, cancel := r.queryContext(ctx)
	defer cancel()

	const query = `
		SELECT
			u.id::text,
			u.pair,
			u.status,
			u.error_message,
			u.created_at,
			u.updated_at,
			r.rate::text,
			r.rate_date,
			r.fetched_at
		FROM quote_updates AS u
		LEFT JOIN exchange_rates AS r ON r.update_id = u.id
		WHERE u.id = $1
	`

	var (
		id           string
		pair         string
		status       string
		errorMessage pgtype.Text
		createdAt    pgtype.Timestamptz
		updatedAt    pgtype.Timestamptz
		rate         pgtype.Text
		rateDate     pgtype.Date
		fetchedAt    pgtype.Timestamptz
	)

	err := r.database.QueryRow(ctx, query, updateID.String()).Scan(
		&id,
		&pair,
		&status,
		&errorMessage,
		&createdAt,
		&updatedAt,
		&rate,
		&rateDate,
		&fetchedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.QuoteUpdateResult{}, false, nil
	}
	if err != nil {
		return domain.QuoteUpdateResult{}, false, fmt.Errorf("select quote update: %w", err)
	}

	parsedID, err := uuid.Parse(id)
	if err != nil {
		return domain.QuoteUpdateResult{}, false, fmt.Errorf("parse stored quote update id: %w", err)
	}

	update := domain.QuoteUpdate{
		ID:        parsedID,
		Pair:      domain.Pair(pair),
		Status:    domain.UpdateStatus(status),
		CreatedAt: createdAt.Time,
		UpdatedAt: updatedAt.Time,
	}
	if errorMessage.Valid {
		update.Error = errorMessage.String
	}

	result := domain.QuoteUpdateResult{Update: update}
	if rate.Valid && rateDate.Valid && fetchedAt.Valid {
		parsedRate, err := domain.ParseRate(rate.String)
		if err != nil {
			return domain.QuoteUpdateResult{}, false, fmt.Errorf("parse stored rate: %w", err)
		}
		result.Quote = &domain.Quote{
			UpdateID:  parsedID,
			Pair:      domain.Pair(pair),
			Rate:      parsedRate,
			RateDate:  rateDate.Time,
			FetchedAt: fetchedAt.Time,
		}
	}

	return result, true, nil
}

func (r *QuoteUpdateRepository) GetLatest(
	ctx context.Context,
	pair domain.Pair,
) (domain.Quote, bool, error) {
	ctx, cancel := r.queryContext(ctx)
	defer cancel()

	const query = `
		SELECT
			u.id::text,
			u.pair,
			r.rate::text,
			r.rate_date,
			r.fetched_at
		FROM quote_updates AS u
		JOIN exchange_rates AS r ON r.update_id = u.id
		WHERE u.pair = $1
			AND u.status = 'completed'
		ORDER BY u.updated_at DESC, u.id DESC
		LIMIT 1
	`

	var (
		updateID   string
		storedPair string
		rate       string
		rateDate   pgtype.Date
		fetchedAt  pgtype.Timestamptz
	)

	err := r.database.QueryRow(ctx, query, pair).Scan(
		&updateID,
		&storedPair,
		&rate,
		&rateDate,
		&fetchedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Quote{}, false, nil
	}
	if err != nil {
		return domain.Quote{}, false, fmt.Errorf("select latest quote: %w", err)
	}

	parsedUpdateID, err := uuid.Parse(updateID)
	if err != nil {
		return domain.Quote{}, false, fmt.Errorf("parse stored quote update id: %w", err)
	}
	parsedRate, err := domain.ParseRate(rate)
	if err != nil {
		return domain.Quote{}, false, fmt.Errorf("parse stored rate: %w", err)
	}

	return domain.Quote{
		UpdateID:  parsedUpdateID,
		Pair:      domain.Pair(storedPair),
		Rate:      parsedRate,
		RateDate:  rateDate.Time,
		FetchedAt: fetchedAt.Time,
	}, true, nil
}

func (r *QuoteUpdateRepository) TakeNextPendingUpdate(
	ctx context.Context,
	startedAt time.Time,
) (domain.QuoteUpdate, bool, error) {
	ctx, cancel := r.queryContext(ctx)
	defer cancel()

	const query = `
		WITH next_update AS (
			SELECT id
			FROM quote_updates
			WHERE status = 'pending'
			ORDER BY created_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE quote_updates AS u
		SET
			status = $1,
			error_message = NULL,
			updated_at = $2
		FROM next_update
		WHERE u.id = next_update.id
		RETURNING
			u.id::text,
			u.pair,
			u.status,
			u.created_at,
			u.updated_at
	`

	var (
		id        string
		pair      string
		status    string
		createdAt pgtype.Timestamptz
		updatedAt pgtype.Timestamptz
	)

	err := r.database.QueryRow(
		ctx,
		query,
		domain.UpdateProcessing,
		startedAt,
	).Scan(
		&id,
		&pair,
		&status,
		&createdAt,
		&updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.QuoteUpdate{}, false, nil
	}
	if err != nil {
		return domain.QuoteUpdate{}, false, fmt.Errorf("take next pending quote update: %w", err)
	}

	parsedID, err := uuid.Parse(id)
	if err != nil {
		return domain.QuoteUpdate{}, false, fmt.Errorf("parse stored quote update id: %w", err)
	}

	return domain.QuoteUpdate{
		ID:        parsedID,
		Pair:      domain.Pair(pair),
		Status:    domain.UpdateStatus(status),
		CreatedAt: createdAt.Time,
		UpdatedAt: updatedAt.Time,
	}, true, nil
}

func (r *QuoteUpdateRepository) CompleteUpdate(
	ctx context.Context,
	quote domain.Quote,
	completedAt time.Time,
) error {
	ctx, cancel := r.queryContext(ctx)
	defer cancel()

	const query = `
		WITH completed_update AS (
			UPDATE quote_updates
			SET
				status = $2,
				error_message = NULL,
				updated_at = $3
			WHERE id = $1
				AND status = $4
				AND pair = $5
			RETURNING id
		)
		INSERT INTO exchange_rates (
			update_id,
			rate,
			rate_date,
			fetched_at
		)
		SELECT
			id,
			$6::numeric,
			$7::date,
			$8
		FROM completed_update
	`

	commandTag, err := r.database.Exec(
		ctx,
		query,
		quote.UpdateID.String(),
		domain.UpdateCompleted,
		completedAt,
		domain.UpdateProcessing,
		quote.Pair,
		string(quote.Rate),
		quote.RateDate.Format(time.DateOnly),
		quote.FetchedAt,
	)
	if err != nil {
		return fmt.Errorf("complete quote update: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("complete quote update %s: update is not processing or pair does not match", quote.UpdateID)
	}

	return nil
}

func (r *QuoteUpdateRepository) FailUpdate(
	ctx context.Context,
	updateID uuid.UUID,
	message string,
	failedAt time.Time,
) error {
	ctx, cancel := r.queryContext(ctx)
	defer cancel()

	const query = `
		UPDATE quote_updates
		SET
			status = $2,
			error_message = $3,
			updated_at = $4
		WHERE id = $1
			AND status = $5
	`

	commandTag, err := r.database.Exec(
		ctx,
		query,
		updateID.String(),
		domain.UpdateFailed,
		message,
		failedAt,
		domain.UpdateProcessing,
	)
	if err != nil {
		return fmt.Errorf("fail quote update: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("fail quote update %s: update is not processing", updateID)
	}

	return nil
}

func (r *QuoteUpdateRepository) RequeueStaleProcessingUpdates(
	ctx context.Context,
	staleBefore time.Time,
	requeuedAt time.Time,
) (int64, error) {
	ctx, cancel := r.queryContext(ctx)
	defer cancel()

	const query = `
		UPDATE quote_updates
		SET
			status = $1,
			error_message = NULL,
			updated_at = $2
		WHERE status = 'processing'
			AND updated_at < $3
	`

	commandTag, err := r.database.Exec(
		ctx,
		query,
		domain.UpdatePending,
		requeuedAt,
		staleBefore,
	)
	if err != nil {
		return 0, fmt.Errorf("requeue stale processing quote updates: %w", err)
	}

	return commandTag.RowsAffected(), nil
}

func (r *QuoteUpdateRepository) Ready(ctx context.Context) error {
	ctx, cancel := r.queryContext(ctx)
	defer cancel()

	if err := r.database.Ping(ctx); err != nil {
		return fmt.Errorf("check PostgreSQL readiness: %w", err)
	}

	return nil
}

func (r *QuoteUpdateRepository) queryContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, r.queryTimeout)
}

var _ service.QuoteUpdateProcessorRepository = (*QuoteUpdateRepository)(nil)
var _ service.QuoteUpdateRecoveryRepository = (*QuoteUpdateRepository)(nil)
