BEGIN;

ALTER TABLE quote_updates
    ADD COLUMN failure_code text;

UPDATE quote_updates
SET
    failure_code = 'rate_provider_error',
    error_message = 'failed to fetch exchange rate'
WHERE status = 'failed';

ALTER TABLE quote_updates
    ADD CONSTRAINT quote_updates_failure_code_check
        CHECK (failure_code IS NULL OR failure_code IN ('rate_provider_error')),
    ADD CONSTRAINT quote_updates_failure_state_check
        CHECK (
            (
                status = 'failed'
                AND failure_code IS NOT NULL
                AND error_message IS NOT NULL
            )
            OR
            (
                status <> 'failed'
                AND failure_code IS NULL
                AND error_message IS NULL
            )
        );

COMMIT;
