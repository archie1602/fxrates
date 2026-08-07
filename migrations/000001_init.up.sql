BEGIN;

CREATE TABLE quote_updates (
    id uuid PRIMARY KEY,
    pair text NOT NULL,
    status text NOT NULL,
    error_message text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT quote_updates_pair_format_check
        CHECK (pair ~ '^[A-Z]{3}/[A-Z]{3}$'),
    CONSTRAINT quote_updates_different_currencies_check
        CHECK (left(pair, 3) <> right(pair, 3)),
    CONSTRAINT quote_updates_status_check
        CHECK (status IN ('pending', 'processing', 'completed', 'failed'))
);

CREATE TABLE exchange_rates (
    update_id uuid PRIMARY KEY
        REFERENCES quote_updates(id) ON DELETE CASCADE,
    rate numeric(30, 12) NOT NULL,
    fetched_at timestamptz NOT NULL,

    CONSTRAINT exchange_rates_positive_check CHECK (rate > 0)
);

CREATE INDEX quote_updates_pending_created_at_idx
    ON quote_updates (created_at)
    WHERE status = 'pending';

CREATE INDEX quote_updates_pair_completed_updated_at_idx
    ON quote_updates (pair, updated_at DESC)
    WHERE status = 'completed';

COMMIT;
