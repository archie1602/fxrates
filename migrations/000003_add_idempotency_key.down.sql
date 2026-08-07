BEGIN;

ALTER TABLE quote_updates
    DROP CONSTRAINT quote_updates_idempotency_key_unique;

ALTER TABLE quote_updates
    DROP COLUMN idempotency_key;

COMMIT;
