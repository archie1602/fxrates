BEGIN;

ALTER TABLE quote_updates
    ADD COLUMN idempotency_key uuid;

ALTER TABLE quote_updates
    ADD CONSTRAINT quote_updates_idempotency_key_unique
        UNIQUE (idempotency_key);

COMMIT;
