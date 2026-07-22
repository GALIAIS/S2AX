-- V14 hardening is intentionally a follow-up migration: 235 may already be
-- deployed, so do not rewrite its checksum.  The original deferred foundation
-- triggers are extended with successor-epoch integrity checks that make every
-- effective assignment, opening fact, source cadence and metric window
-- auditable at commit time.

CREATE OR REPLACE FUNCTION assert_city_open_world_commute_lifecycle_successor_integrity(target_world_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE world_version VARCHAR(32);
BEGIN
    SELECT simulation_version INTO world_version
    FROM city_worlds WHERE id = target_world_id;
    IF world_version <> 'city-openworld-v14' THEN RETURN; END IF;

    IF EXISTS (
        SELECT 1
        FROM city_open_world_commute_assignment_epochs epoch
        WHERE epoch.world_id = target_world_id
          AND (
              (epoch.origin_kind = 'v13_baseline'
               AND (epoch.epoch_number <> 1 OR epoch.opened_fact_id IS NOT NULL))
              OR (epoch.origin_kind = 'admin_rebind'
                  AND (epoch.epoch_number <= 1 OR epoch.opened_fact_id IS NULL))
          )
    ) THEN
        RAISE EXCEPTION 'city open-world V14 epoch origin does not match its opening evidence'
            USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM city_open_world_commute_assignment_epochs epoch
        WHERE epoch.world_id = target_world_id
        GROUP BY epoch.binding_code
        HAVING MIN(epoch.epoch_number) <> 1 OR MAX(epoch.epoch_number) <> COUNT(*)
            OR COUNT(DISTINCT epoch.actor_id) <> 1
    ) THEN
        RAISE EXCEPTION 'city open-world V14 assignment epochs are not a contiguous single-actor chain'
            USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM city_open_world_commute_assignment_epochs epoch
        JOIN LATERAL (
            SELECT transition.state, transition.reason_code, transition.transition_tick,
                   transition.transition_sequence, transition.source_fact_id
            FROM city_open_world_commute_assignment_transitions transition
            WHERE transition.world_id = epoch.world_id
              AND transition.assignment_code = epoch.code
            ORDER BY transition.transition_tick, transition.transition_sequence
            LIMIT 1
        ) opening ON TRUE
        WHERE epoch.world_id = target_world_id
          AND (
              (epoch.origin_kind = 'v13_baseline'
               AND (opening.state <> 'active'
                    OR opening.reason_code <> 'baseline_initialized'
                    OR opening.transition_tick <> epoch.opened_tick
                    OR opening.transition_sequence <> 0
                    OR opening.source_fact_id IS NOT NULL))
              OR (epoch.origin_kind = 'admin_rebind'
                  AND (opening.state <> 'active'
                       OR opening.transition_tick <> epoch.opened_tick
                       OR opening.source_fact_id IS DISTINCT FROM epoch.opened_fact_id))
          )
    ) THEN
        RAISE EXCEPTION 'city open-world V14 assignment opening transition is invalid'
            USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        WITH latest AS (
            SELECT DISTINCT ON (transition.assignment_code)
                   transition.assignment_code, transition.state
            FROM city_open_world_commute_assignment_transitions transition
            WHERE transition.world_id = target_world_id
            ORDER BY transition.assignment_code, transition.transition_tick DESC,
                     transition.transition_sequence DESC
        )
        SELECT 1
        FROM city_open_world_commute_assignment_epochs epoch
        JOIN latest ON latest.assignment_code = epoch.code
        WHERE epoch.world_id = target_world_id
          AND latest.state IN ('active', 'suspended')
        GROUP BY epoch.binding_code
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'city open-world V14 binding has more than one effective epoch'
            USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM city_open_world_commute_lifecycle_sources source
        JOIN city_open_world_commute_assignment_epochs epoch
          ON epoch.world_id = source.world_id AND epoch.code = source.assignment_code
        LEFT JOIN city_open_world_runtime_facts last_fact
          ON last_fact.id = source.last_fact_id AND last_fact.world_id = source.world_id
        WHERE source.world_id = target_world_id
          AND (
              (source.generated_count + source.suppressed_count = 0
               AND (
                   source.last_transition_tick <> epoch.opened_tick
                   OR source.next_due_tick <> epoch.opened_tick + 1 + source.phase_offset
                   OR (epoch.origin_kind = 'v13_baseline' AND source.last_fact_id IS NOT NULL)
                   OR (epoch.origin_kind = 'admin_rebind'
                       AND source.last_fact_id IS DISTINCT FROM epoch.opened_fact_id)
               ))
              OR (source.generated_count + source.suppressed_count > 0
                  AND (source.last_fact_id IS NULL
                       OR source.next_due_tick <> source.last_transition_tick + source.period_ticks))
              OR (source.last_fact_id IS NOT NULL
                  AND (last_fact.id IS NULL OR last_fact.tick <> source.last_transition_tick))
          )
    ) THEN
        RAISE EXCEPTION 'city open-world V14 lifecycle source cadence or fact chain is invalid'
            USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        WITH ordered_metrics AS (
            SELECT metric.cycle_start_tick, metric.cycle_end_tick, profile.baseline_tick,
                   ROW_NUMBER() OVER (ORDER BY metric.cycle_start_tick) AS ordinal,
                   LAG(metric.cycle_end_tick) OVER (ORDER BY metric.cycle_start_tick) AS previous_end
            FROM city_open_world_commute_lifecycle_cycle_metrics metric
            JOIN city_open_world_commute_lifecycle_profiles profile
              ON profile.world_id = metric.world_id
            WHERE metric.world_id = target_world_id
        )
        SELECT 1
        FROM ordered_metrics metric
        WHERE (metric.ordinal = 1 AND metric.cycle_start_tick <> metric.baseline_tick + 1)
           OR (metric.ordinal > 1 AND metric.cycle_start_tick <> metric.previous_end + 1)
    ) THEN
        RAISE EXCEPTION 'city open-world V14 lifecycle metric windows are not contiguous'
            USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_open_world_commute_lifecycle_foundation_commit()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    PERFORM assert_city_open_world_commute_lifecycle_foundation(target_world_id);
    PERFORM assert_city_open_world_commute_lifecycle_successor_integrity(target_world_id);
    RETURN NULL;
END;
$$;
