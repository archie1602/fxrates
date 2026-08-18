BEGIN;

ALTER TABLE quote_updates
    DROP CONSTRAINT quote_updates_pair_format_check,
    DROP CONSTRAINT quote_updates_different_currencies_check,
    ADD CONSTRAINT quote_updates_supported_pair_check
        CHECK (
            pair IN (
                'USD/EUR',
                'USD/MXN',
                'EUR/USD',
                'EUR/MXN',
                'MXN/USD',
                'MXN/EUR'
            )
        );

COMMIT;
