-- V23 validates its reserve and claim evidence while the command that created
-- it is still pending in the enclosing tick transaction. The generic command
-- finalizer changes that row to applied only after every command has been
-- reduced and the snapshot is canonicalized. Permit this one transient state
-- only inside V23's audited write context; all durable reads and later writes
-- still require an applied source command.
--
-- V249 also replaces the V22 foundation function. Keep its V23/V24 successor
-- scope live for existing databases that may have applied an earlier V249
-- revision before the broadened version guard was introduced.

DO $$
DECLARE
    definition TEXT;
BEGIN
    definition := pg_get_functiondef('assert_city_open_world_freight_settlement_foundation(bigint)'::REGPROCEDURE);
    definition := replace(
        definition,
        $old$world_version <> 'city-openworld-v22'$old$,
        $new$world_version NOT IN ('city-openworld-v22','city-openworld-v23','city-openworld-v24')$new$
    );
    IF position($needle$world_version NOT IN ('city-openworld-v22','city-openworld-v23','city-openworld-v24')$needle$ IN definition) = 0 THEN
        RAISE EXCEPTION 'cannot preserve V22 freight-settlement assertion scope through V24' USING ERRCODE = '23514';
    END IF;
    EXECUTE definition;

    definition := pg_get_functiondef('assert_city_open_world_carrier_recovery_foundation(bigint)'::REGPROCEDURE);
    definition := replace(
        definition,
        $old$OR command.status <> 'applied'
               OR journal.journal_type <> 'subsidy'$old$,
        $new$OR (command.status <> 'applied' AND NOT (
                    command.status = 'pending'
                    AND city_open_world_carrier_recovery_write_enabled(funding.world_id)
               ))
               OR journal.journal_type <> 'subsidy'$new$
    );
    definition := replace(
        definition,
        $old$OR command.status <> 'applied'
               OR journal.journal_type <> 'cash_transfer'$old$,
        $new$OR (command.status <> 'applied' AND NOT (
                    command.status = 'pending'
                    AND city_open_world_carrier_recovery_write_enabled(recovery.world_id)
               ))
               OR journal.journal_type <> 'cash_transfer'$new$
    );
    IF position($needle$city_open_world_carrier_recovery_write_enabled(funding.world_id)$needle$ IN definition) = 0
       OR position($needle$city_open_world_carrier_recovery_write_enabled(recovery.world_id)$needle$ IN definition) = 0 THEN
        RAISE EXCEPTION 'cannot permit V23 pending source commands only in their audited write context' USING ERRCODE = '23514';
    END IF;
    EXECUTE definition;
END;
$$;
