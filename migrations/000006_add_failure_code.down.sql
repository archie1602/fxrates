BEGIN;

ALTER TABLE quote_updates
    DROP CONSTRAINT quote_updates_failure_state_check,
    DROP CONSTRAINT quote_updates_failure_code_check,
    DROP COLUMN failure_code;

COMMIT;
