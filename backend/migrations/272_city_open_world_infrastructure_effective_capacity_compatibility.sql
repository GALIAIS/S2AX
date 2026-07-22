-- V21 introduced an explicit next-tick capacity effect on the V20
-- infrastructure transition fact.  Later versions kept the V20 foundation
-- assertion active, but its original fact predicate only accepted the V20
-- no-scheduler marker.  Preserve immutable V20 history while allowing the
-- precisely versioned V21+ successor marker and its exact visibility cursor.
DO $$
DECLARE
    definition TEXT;
BEGIN
    definition := pg_get_functiondef('assert_city_open_world_infrastructure_foundation(bigint)'::REGPROCEDURE);
    definition := replace(
        definition,
        $old$OR fact.payload->>'v9_scheduler_effect' <> 'none'$old$,
        $new$OR NOT (
                    (fact.payload->>'v9_scheduler_effect' = 'none'
                     AND COALESCE(fact.payload->>'v9_scheduler_effective_from_tick', '0') = '0')
                    OR (
                        fact.payload->>'v9_scheduler_effect' = 'next_tick_effective_capacity_v1'
                        AND fact.payload->>'v9_scheduler_effective_from_tick' = (transition.transition_tick + 1)::TEXT
                        AND world_version IN (
                            'city-openworld-v21','city-openworld-v22','city-openworld-v23','city-openworld-v24'
                        )
                    )
                )$new$
    );
    IF position($needle$next_tick_effective_capacity_v1$needle$ IN definition) = 0
       OR position($needle$city-openworld-v24$needle$ IN definition) = 0 THEN
        RAISE EXCEPTION 'cannot extend V20 infrastructure assertion for V21 effective capacity'
            USING ERRCODE = '23514';
    END IF;
    EXECUTE definition;
END;
$$;

-- The transition trigger was introduced with V21 and remained pinned to the
-- exact V21 world string even after V22-V24 inherited the effective-capacity
-- runtime. Re-declare it with the same append-only state machine and expand
-- only the successor world set that is allowed to emit the V21 marker.
CREATE OR REPLACE FUNCTION guard_city_open_world_infrastructure_transition()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    target_world_id BIGINT;
    previous_state VARCHAR(24);
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_open_world_infrastructure_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_infrastructure_bootstrap_write_enabled(target_world_id)
       AND NEW.from_state = '' AND NEW.to_state = 'operational'
       AND NEW.capacity_milli = 1000 AND NEW.reason_code = 'baseline_initialized'
       AND NEW.source_fact_id IS NULL AND NEW.transition_sequence = 0
       AND NEW.metadata->>'schema_version' = '1'
       AND NEW.metadata->>'origin' = 'baseline'
       AND NEW.metadata->>'scheduler' = 'not_consumed_by_v9'
       AND EXISTS (SELECT 1 FROM city_open_world_infrastructure_profiles profile
                   WHERE profile.world_id = target_world_id AND profile.baseline_tick = NEW.transition_tick) THEN
        RETURN NEW;
    END IF;
    IF TG_OP <> 'INSERT' OR NOT city_open_world_infrastructure_write_enabled(target_world_id) THEN
        RAISE EXCEPTION 'open-world V20 infrastructure transitions are append-only audited facts'
            USING ERRCODE = '55000';
    END IF;
    SELECT transition.to_state INTO previous_state
    FROM city_open_world_infrastructure_asset_transitions transition
    WHERE transition.world_id = target_world_id AND transition.asset_code = NEW.asset_code
      AND (transition.transition_tick < NEW.transition_tick
           OR (transition.transition_tick = NEW.transition_tick AND transition.transition_sequence < NEW.transition_sequence))
    ORDER BY transition.transition_tick DESC, transition.transition_sequence DESC
    LIMIT 1;
    IF previous_state IS NULL OR NEW.from_state <> previous_state
       OR NEW.reason_code = 'baseline_initialized'
       OR NEW.metadata->>'schema_version' <> '1'
       OR NEW.metadata->>'origin' <> 'command'
       OR NEW.metadata->>'previous_state' <> NEW.from_state
       OR NEW.metadata->>'scheduler' <> 'not_consumed_by_v9'
       OR NOT ((NEW.from_state = 'operational' AND NEW.to_state IN ('restricted', 'maintenance', 'closed'))
               OR (NEW.from_state = 'restricted' AND NEW.to_state IN ('operational', 'maintenance', 'closed'))
               OR (NEW.from_state = 'maintenance' AND NEW.to_state IN ('operational', 'closed'))
               OR (NEW.from_state = 'closed' AND NEW.to_state = 'construction')
               OR (NEW.from_state = 'construction' AND NEW.to_state IN ('operational', 'closed'))) THEN
        RAISE EXCEPTION 'open-world V20 infrastructure transition violates the lifecycle state machine'
            USING ERRCODE = '23514';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM city_open_world_runtime_facts fact
        WHERE fact.id = NEW.source_fact_id AND fact.world_id = target_world_id
          AND fact.tick = NEW.transition_tick AND fact.sequence = NEW.transition_sequence
          AND fact.fact_type = 'infrastructure.asset.transitioned'
          AND fact.payload->>'asset_code' = NEW.asset_code
          AND fact.payload->>'from_state' = NEW.from_state
          AND fact.payload->>'to_state' = NEW.to_state
          AND fact.payload->>'capacity_milli' = NEW.capacity_milli::TEXT
          AND fact.payload->>'reason_code' = NEW.reason_code
          AND ((fact.payload->>'v9_scheduler_effect' = 'none'
                AND COALESCE(fact.payload->>'v9_scheduler_effective_from_tick', '0') = '0')
               OR (fact.payload->>'v9_scheduler_effect' = 'next_tick_effective_capacity_v1'
                   AND fact.payload->>'v9_scheduler_effective_from_tick' = (NEW.transition_tick + 1)::TEXT
                   AND EXISTS (
                       SELECT 1 FROM city_worlds world
                       WHERE world.id = target_world_id
                         AND world.simulation_version IN (
                             'city-openworld-v21','city-openworld-v22',
                             'city-openworld-v23','city-openworld-v24'
                         )
                   )))
    ) THEN
        RAISE EXCEPTION 'open-world V20 infrastructure transition must match its runtime fact cursor'
            USING ERRCODE = '23514';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM city_open_world_infrastructure_asset_states state
        WHERE state.world_id = target_world_id AND state.asset_code = NEW.asset_code
          AND state.state = NEW.to_state AND state.capacity_milli = NEW.capacity_milli
          AND state.effective_tick = NEW.transition_tick AND state.source_fact_id = NEW.source_fact_id
          AND state.metadata->>'previous_state' = NEW.from_state
    ) THEN
        RAISE EXCEPTION 'open-world V20 infrastructure transition must match the current state projection'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
