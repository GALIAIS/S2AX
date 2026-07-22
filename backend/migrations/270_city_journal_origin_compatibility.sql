-- Restore the narrow V15 automatic-expiry reversal origin after V22 and V24
-- extended the journal shape.  The expiry reconciliation is a system-created
-- accounting fact (not a user command), so its signed provenance tag remains
-- the only source-free reversal that may be posted.
ALTER TABLE city_journals
    DROP CONSTRAINT IF EXISTS city_journal_origin_check;
ALTER TABLE city_journals
    ADD CONSTRAINT city_journal_origin_check CHECK (
        (journal_type = 'opening'
            AND source_command_id IS NULL AND market_settlement_id IS NULL
            AND reversal_of_journal_id IS NULL)
        OR (journal_type = 'freight_fee'
            AND source_command_id IS NULL AND market_settlement_id IS NULL
            AND reversal_of_journal_id IS NULL)
        OR (journal_type = 'reversal'
            AND market_settlement_id IS NULL
            AND reversal_of_journal_id IS NOT NULL
            AND (
                source_command_id IS NOT NULL
                OR (
                    source_command_id IS NULL
                    AND metadata->>'system_origin' = 'open_world_supply_chain.auto_expiry.v1'
                )
            ))
        OR (journal_type NOT IN ('opening', 'reversal', 'freight_fee')
            AND reversal_of_journal_id IS NULL
            AND ((source_command_id IS NOT NULL)::INTEGER
                 + (market_settlement_id IS NOT NULL)::INTEGER) = 1)
    );
