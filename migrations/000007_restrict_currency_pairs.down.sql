BEGIN;

ALTER TABLE quote_updates
    DROP CONSTRAINT quote_updates_supported_pair_check,
    ADD CONSTRAINT quote_updates_pair_format_check
        CHECK (pair ~ '^[A-Z]{3}/[A-Z]{3}$'),
    ADD CONSTRAINT quote_updates_different_currencies_check
        CHECK (left(pair, 3) <> right(pair, 3));

COMMIT;
