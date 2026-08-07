BEGIN;

CREATE INDEX quote_updates_processing_updated_at_idx
    ON quote_updates (updated_at)
    WHERE status = 'processing';

COMMIT;
