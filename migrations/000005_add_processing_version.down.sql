BEGIN;

ALTER TABLE quote_updates
    DROP CONSTRAINT quote_updates_processing_version_nonnegative_check;

ALTER TABLE quote_updates
    DROP COLUMN processing_version;

COMMIT;
