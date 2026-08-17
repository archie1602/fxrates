BEGIN;

ALTER TABLE quote_updates
    ADD COLUMN processing_version bigint NOT NULL DEFAULT 0;

ALTER TABLE quote_updates
    ADD CONSTRAINT quote_updates_processing_version_nonnegative_check
        CHECK (processing_version >= 0);

COMMIT;
