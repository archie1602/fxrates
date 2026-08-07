BEGIN;

ALTER TABLE exchange_rates
    DROP COLUMN rate_date;

COMMIT;
