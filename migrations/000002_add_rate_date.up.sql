BEGIN;

ALTER TABLE exchange_rates
    ADD COLUMN rate_date date;

UPDATE exchange_rates
SET rate_date = (fetched_at AT TIME ZONE 'UTC')::date
WHERE rate_date IS NULL;

ALTER TABLE exchange_rates
    ALTER COLUMN rate_date SET NOT NULL;

COMMIT;
