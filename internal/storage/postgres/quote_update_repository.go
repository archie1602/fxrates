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

const expectedSchemaVersion = 7

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
	updateID uuid.UUID,
	pair domain.Pair,
	idempotencyKey *uuid.UUID,
) (domain.QuoteUpdate, bool, error) {
	if updateID == uuid.Nil {
		return domain.QuoteUpdate{}, false, errors.New("create quote update: update ID must not be zero")
	}
	validatedPair, err := domain.ParsePair(string(pair))
	if err != nil {
		return domain.QuoteUpdate{}, false, fmt.Errorf("create quote update: validate pair: %w", err)
	}
	if idempotencyKey != nil && *idempotencyKey == uuid.Nil {
		return domain.QuoteUpdate{}, false, errors.New("create quote update: idempotency key must not be zero")
	}

	ctx, cancel := r.queryContext(ctx)
	defer cancel()

	const query = `
		INSERT INTO quote_updates (
			id,
			pair,
			status,
			idempotency_key
		)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING
			id::text,
			pair,
			status,
			failure_code,
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
		updateID.String(),
		validatedPair,
		domain.UpdatePending,
		keyValue,
	))
	if err == nil {
		return stored, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.QuoteUpdate{}, false, fmt.Errorf("insert quote update: %w", err)
	}
	if idempotencyKey == nil {
		return domain.QuoteUpdate{}, false, errors.New("insert quote update: conflict without an idempotency key")
	}

	existing, found, err := r.getByIdempotencyKey(ctx, *idempotencyKey)
	if err != nil {
		return domain.QuoteUpdate{}, false, err
	}
	if !found {
		return domain.QuoteUpdate{}, false, errors.New("insert quote update: conflicting idempotency key was not found")
	}

	return existing, false, nil
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
			failure_code,
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
		failureCode  pgtype.Text
		errorMessage pgtype.Text
		createdAt    pgtype.Timestamptz
		updatedAt    pgtype.Timestamptz
	)

	err := row.Scan(
		&id,
		&pair,
		&status,
		&failureCode,
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
	parsedPair, err := domain.ParsePair(pair)
	if err != nil {
		return domain.QuoteUpdate{}, fmt.Errorf("parse stored quote update pair: %w", err)
	}
	parsedStatus, err := domain.ParseUpdateStatus(status)
	if err != nil {
		return domain.QuoteUpdate{}, fmt.Errorf("parse stored quote update status: %w", err)
	}

	update := domain.QuoteUpdate{
		ID:        parsedID,
		Pair:      parsedPair,
		Status:    parsedStatus,
		CreatedAt: createdAt.Time,
		UpdatedAt: updatedAt.Time,
	}
	if failureCode.Valid {
		parsedFailureCode, err := domain.ParseUpdateFailureCode(failureCode.String)
		if err != nil {
			return domain.QuoteUpdate{}, fmt.Errorf("parse stored quote update failure code: %w", err)
		}
		update.FailureCode = parsedFailureCode
	}
	if errorMessage.Valid {
		update.FailureMessage = errorMessage.String
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
			u.failure_code,
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
		failureCode  pgtype.Text
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
		&failureCode,
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
	parsedPair, err := domain.ParsePair(pair)
	if err != nil {
		return domain.QuoteUpdateResult{}, false, fmt.Errorf("parse stored quote update pair: %w", err)
	}
	parsedStatus, err := domain.ParseUpdateStatus(status)
	if err != nil {
		return domain.QuoteUpdateResult{}, false, fmt.Errorf("parse stored quote update status: %w", err)
	}

	update := domain.QuoteUpdate{
		ID:        parsedID,
		Pair:      parsedPair,
		Status:    parsedStatus,
		CreatedAt: createdAt.Time,
		UpdatedAt: updatedAt.Time,
	}
	if failureCode.Valid {
		parsedFailureCode, err := domain.ParseUpdateFailureCode(failureCode.String)
		if err != nil {
			return domain.QuoteUpdateResult{}, false, fmt.Errorf("parse stored quote update failure code: %w", err)
		}
		update.FailureCode = parsedFailureCode
	}
	if errorMessage.Valid {
		update.FailureMessage = errorMessage.String
	}

	result := domain.QuoteUpdateResult{Update: update}
	if rate.Valid && rateDate.Valid && fetchedAt.Valid {
		parsedRate, err := domain.ParseRate(rate.String)
		if err != nil {
			return domain.QuoteUpdateResult{}, false, fmt.Errorf("parse stored rate: %w", err)
		}
		result.Quote = &domain.Quote{
			UpdateID:  parsedID,
			Pair:      parsedPair,
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
	validatedPair, err := domain.ParsePair(string(pair))
	if err != nil {
		return domain.Quote{}, false, fmt.Errorf("select latest quote: validate pair: %w", err)
	}

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

	err = r.database.QueryRow(ctx, query, validatedPair).Scan(
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
	parsedPair, err := domain.ParsePair(storedPair)
	if err != nil {
		return domain.Quote{}, false, fmt.Errorf("parse stored quote pair: %w", err)
	}

	return domain.Quote{
		UpdateID:  parsedUpdateID,
		Pair:      parsedPair,
		Rate:      parsedRate,
		RateDate:  rateDate.Time,
		FetchedAt: fetchedAt.Time,
	}, true, nil
}

func (r *QuoteUpdateRepository) TakeNextPendingUpdate(
	ctx context.Context,
) (service.ClaimedQuoteUpdate, bool, error) {
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
			failure_code = NULL,
			error_message = NULL,
			updated_at = CURRENT_TIMESTAMP,
			processing_version = processing_version + 1
		FROM next_update
		WHERE u.id = next_update.id
		RETURNING
			u.id::text,
			u.pair,
			u.status,
			u.created_at,
			u.updated_at,
			u.processing_version
	`

	var (
		id         string
		pair       string
		status     string
		createdAt  pgtype.Timestamptz
		updatedAt  pgtype.Timestamptz
		leaseToken int64
	)

	err := r.database.QueryRow(
		ctx,
		query,
		domain.UpdateProcessing,
	).Scan(
		&id,
		&pair,
		&status,
		&createdAt,
		&updatedAt,
		&leaseToken,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return service.ClaimedQuoteUpdate{}, false, nil
	}
	if err != nil {
		return service.ClaimedQuoteUpdate{}, false, fmt.Errorf("take next pending quote update: %w", err)
	}

	parsedID, err := uuid.Parse(id)
	if err != nil {
		return service.ClaimedQuoteUpdate{}, false, fmt.Errorf("parse stored quote update id: %w", err)
	}
	if leaseToken <= 0 {
		return service.ClaimedQuoteUpdate{}, false, fmt.Errorf("take next pending quote update: invalid processing lease token %d", leaseToken)
	}
	parsedPair, err := domain.ParsePair(pair)
	if err != nil {
		return service.ClaimedQuoteUpdate{}, false, fmt.Errorf("parse claimed quote update pair: %w", err)
	}
	parsedStatus, err := domain.ParseUpdateStatus(status)
	if err != nil {
		return service.ClaimedQuoteUpdate{}, false, fmt.Errorf("parse claimed quote update status: %w", err)
	}

	return service.ClaimedQuoteUpdate{
		Update: domain.QuoteUpdate{
			ID:        parsedID,
			Pair:      parsedPair,
			Status:    parsedStatus,
			CreatedAt: createdAt.Time,
			UpdatedAt: updatedAt.Time,
		},
		LeaseToken: service.ProcessingLeaseToken(leaseToken),
	}, true, nil
}

func (r *QuoteUpdateRepository) CompleteUpdate(
	ctx context.Context,
	claim service.ClaimedQuoteUpdate,
	quote domain.Quote,
) (bool, error) {
	if quote.UpdateID != claim.Update.ID || quote.Pair != claim.Update.Pair {
		return false, errors.New("complete quote update: quote does not match processing claim")
	}

	ctx, cancel := r.queryContext(ctx)
	defer cancel()

	const query = `
		WITH completed_update AS (
			UPDATE quote_updates
			SET
				status = $2,
				failure_code = NULL,
				error_message = NULL,
				updated_at = CURRENT_TIMESTAMP
			WHERE id = $1
				AND status = $3
				AND pair = $4
				AND processing_version = $5
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
		claim.Update.ID.String(),
		domain.UpdateCompleted,
		domain.UpdateProcessing,
		claim.Update.Pair,
		int64(claim.LeaseToken),
		string(quote.Rate),
		quote.RateDate.Format(time.DateOnly),
		quote.FetchedAt,
	)
	if err != nil {
		return false, fmt.Errorf("complete quote update: %w", err)
	}

	return commandTag.RowsAffected() == 1, nil
}

func (r *QuoteUpdateRepository) FailUpdate(
	ctx context.Context,
	claim service.ClaimedQuoteUpdate,
	failure service.QuoteUpdateFailure,
) (bool, error) {
	if _, err := domain.ParseUpdateFailureCode(string(failure.Code)); err != nil {
		return false, fmt.Errorf("fail quote update: validate failure code: %w", err)
	}
	if failure.Message == "" {
		return false, errors.New("fail quote update: failure message is required")
	}

	ctx, cancel := r.queryContext(ctx)
	defer cancel()

	const query = `
		UPDATE quote_updates
		SET
			status = $2,
			failure_code = $3,
			error_message = $4,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
			AND status = $5
			AND processing_version = $6
	`

	commandTag, err := r.database.Exec(
		ctx,
		query,
		claim.Update.ID.String(),
		domain.UpdateFailed,
		failure.Code,
		failure.Message,
		domain.UpdateProcessing,
		int64(claim.LeaseToken),
	)
	if err != nil {
		return false, fmt.Errorf("fail quote update: %w", err)
	}

	return commandTag.RowsAffected() == 1, nil
}

func (r *QuoteUpdateRepository) RequeueStaleProcessingUpdates(
	ctx context.Context,
	processingTimeout time.Duration,
) (int64, error) {
	if processingTimeout <= 0 {
		return 0, errors.New("requeue stale processing quote updates: processing timeout must be positive")
	}

	ctx, cancel := r.queryContext(ctx)
	defer cancel()

	const query = `
		UPDATE quote_updates
		SET
			status = $1,
			failure_code = NULL,
			error_message = NULL,
			updated_at = CURRENT_TIMESTAMP
		WHERE status = 'processing'
			AND updated_at < CURRENT_TIMESTAMP - $2::interval
	`

	commandTag, err := r.database.Exec(
		ctx,
		query,
		domain.UpdatePending,
		pgtype.Interval{Microseconds: processingTimeout.Microseconds(), Valid: true},
	)
	if err != nil {
		return 0, fmt.Errorf("requeue stale processing quote updates: %w", err)
	}

	return commandTag.RowsAffected(), nil
}

func (r *QuoteUpdateRepository) Ready(ctx context.Context) error {
	ctx, cancel := r.queryContext(ctx)
	defer cancel()

	var (
		version int
		dirty   bool
	)
	if err := r.database.QueryRow(
		ctx,
		"SELECT version, dirty FROM schema_migrations LIMIT 1",
	).Scan(&version, &dirty); err != nil {
		return fmt.Errorf("check PostgreSQL schema version: %w", err)
	}
	if dirty {
		return errors.New("check PostgreSQL schema version: migration state is dirty")
	}
	if version != expectedSchemaVersion {
		return fmt.Errorf(
			"check PostgreSQL schema version: got %d, want %d",
			version,
			expectedSchemaVersion,
		)
	}

	return nil
}

func (r *QuoteUpdateRepository) queryContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, r.queryTimeout)
}

var _ service.QuoteUpdateProcessorRepository = (*QuoteUpdateRepository)(nil)
var _ service.QuoteUpdateRecoveryRepository = (*QuoteUpdateRepository)(nil)
