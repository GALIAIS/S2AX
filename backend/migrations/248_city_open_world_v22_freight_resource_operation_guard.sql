-- V22 receipt application posts a conserved-or-lost freight operation before
-- it writes the receipt evidence that seals the exact line outcomes. Keep the
-- older resource validator authoritative for every pre-V22 operation type and
-- add only this one command-bound successor branch.

ALTER TABLE city_open_world_supply_chain_reservation_releases
    DROP CONSTRAINT IF EXISTS city_open_world_supply_chain_release_identity_check;
ALTER TABLE city_open_world_supply_chain_reservation_releases
    ADD CONSTRAINT city_open_world_supply_chain_release_identity_check CHECK (
        reason_code IN ('delivered', 'settled', 'cancelled', 'expired', 'failed')
    );

CREATE OR REPLACE FUNCTION assert_city_resource_operation_ready_v22(target_operation_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
    target_type VARCHAR(24);
    target_actor BIGINT;
    target_district BIGINT;
    target_source_command BIGINT;
    expected_command_type VARCHAR(64);
    entry_count BIGINT;
    incoming_units NUMERIC;
    outgoing_units NUMERIC;
BEGIN
    SELECT world_id, operation_type, actor_entity_id, district_id, source_command_id
    INTO target_world_id, target_type, target_actor, target_district, target_source_command
    FROM city_resource_operations
    WHERE id = target_operation_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'city resource operation % does not exist', target_operation_id
            USING ERRCODE = '23514';
    END IF;

    IF target_type <> 'freight_settlement' THEN
        PERFORM assert_city_resource_operation_ready(target_operation_id);
        RETURN;
    END IF;

    IF target_source_command IS NULL THEN
        RAISE EXCEPTION 'freight settlement resource operation requires a source command'
            USING ERRCODE = '23514';
    END IF;

    SELECT command_type INTO expected_command_type
    FROM city_commands
    WHERE id = target_source_command AND world_id = target_world_id;
    IF expected_command_type IS DISTINCT FROM 'open_world.freight_settlement.receipt' THEN
        RAISE EXCEPTION 'freight settlement resource operation does not match its source command'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*),
           COALESCE(SUM(quantity_units) FILTER (WHERE direction = 'in'), 0),
           COALESCE(SUM(quantity_units) FILTER (WHERE direction = 'out'), 0)
    INTO entry_count, incoming_units, outgoing_units
    FROM city_resource_entries
    WHERE operation_id = target_operation_id;

    IF entry_count = 0 OR outgoing_units <= 0 OR incoming_units < 0 OR incoming_units > outgoing_units
       OR EXISTS (
           SELECT 1
           FROM city_resource_entries entry
           JOIN city_inventory_balances balance ON balance.id = entry.balance_id
           WHERE entry.operation_id = target_operation_id
             AND entry.direction = 'out'
             AND (balance.entity_id <> target_actor OR balance.district_id <> target_district)
       )
       OR EXISTS (
           SELECT 1
           FROM city_resource_entries entry
           WHERE entry.operation_id = target_operation_id
           GROUP BY entry.resource_id
           HAVING COALESCE(SUM(entry.quantity_units) FILTER (WHERE entry.direction = 'in'), 0)
                > COALESCE(SUM(entry.quantity_units) FILTER (WHERE entry.direction = 'out'), 0)
       ) THEN
        RAISE EXCEPTION 'freight settlement resource operation is not an authorized outbound settlement'
            USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_resource_operation_write()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.posted_at IS NOT NULL THEN
            RAISE EXCEPTION 'city resource operations must be inserted as drafts'
                USING ERRCODE = '55000';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'city resource operations are immutable facts'
            USING ERRCODE = '55000';
    END IF;
    IF OLD.posted_at IS NULL AND NEW.posted_at IS NOT NULL
       AND NEW.id IS NOT DISTINCT FROM OLD.id
       AND NEW.world_id IS NOT DISTINCT FROM OLD.world_id
       AND NEW.tick IS NOT DISTINCT FROM OLD.tick
       AND NEW.sequence IS NOT DISTINCT FROM OLD.sequence
       AND NEW.operation_key IS NOT DISTINCT FROM OLD.operation_key
       AND NEW.operation_type IS NOT DISTINCT FROM OLD.operation_type
       AND NEW.source_command_id IS NOT DISTINCT FROM OLD.source_command_id
       AND NEW.actor_entity_id IS NOT DISTINCT FROM OLD.actor_entity_id
       AND NEW.district_id IS NOT DISTINCT FROM OLD.district_id
       AND NEW.recipe_id IS NOT DISTINCT FROM OLD.recipe_id
       AND NEW.batch_count IS NOT DISTINCT FROM OLD.batch_count
       AND NEW.description IS NOT DISTINCT FROM OLD.description
       AND NEW.metadata IS NOT DISTINCT FROM OLD.metadata
       AND NEW.created_at IS NOT DISTINCT FROM OLD.created_at THEN
        PERFORM assert_city_resource_operation_ready_v22(OLD.id);
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city resource operations permit only one draft-to-posted transition'
        USING ERRCODE = '55000';
END;
$$;

-- V22 completes the V15 order with an order.settled fact, then emits the
-- V17 settlement.confirmed custody receipt at the same fact cursor.  The V17
-- trigger predates that terminal state and therefore only accepted
-- order.delivered.  Preserve the original cursor check for every legacy
-- receipt while allowing precisely this V22 successor evidence pair.
CREATE OR REPLACE FUNCTION guard_city_open_world_enterprise_freight_receipt_projection()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    target_world_id BIGINT; source_code_value VARCHAR(160); source_order_value VARCHAR(160);
    row_data JSONB;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'DELETE' OR NOT city_open_world_enterprise_freight_receipt_write_enabled(target_world_id) THEN
        RAISE EXCEPTION 'open-world V17 freight-receipt projections require their audited write context'
            USING ERRCODE = '55000';
    END IF;
    -- One trigger function serves six projection tables with different row
    -- records. Access table-specific columns through JSONB so PostgreSQL
    -- does not resolve columns that are absent from a given trigger row.
    row_data := to_jsonb(NEW);
    IF TG_TABLE_NAME = 'city_open_world_enterprise_freight_shipments' THEN
        SELECT fact.source_code, source.order_code
          INTO source_code_value, source_order_value
        FROM city_open_world_enterprise_freight_facts fact
        JOIN city_open_world_enterprise_freight_sources source
          ON source.world_id = fact.world_id AND source.code = fact.source_code
        WHERE fact.id = (row_data->>'source_freight_fact_id')::BIGINT
          AND fact.world_id = target_world_id
          AND fact.fact_type = 'source.created';
        IF source_code_value IS DISTINCT FROM row_data->>'freight_source_code'
           OR source_order_value IS DISTINCT FROM row_data->>'order_code' THEN
            RAISE EXCEPTION 'open-world V17 shipment must root in its V16 source.created fact'
                USING ERRCODE = '23514';
        END IF;
    ELSIF TG_TABLE_NAME = 'city_open_world_enterprise_freight_receipt_facts' THEN
        IF row_data->>'evidence_kind' = 'enterprise_freight' AND NOT EXISTS (
            SELECT 1 FROM city_open_world_enterprise_freight_facts fact
            WHERE fact.id = (row_data->>'freight_fact_id')::BIGINT AND fact.world_id = target_world_id
              AND fact.tick = (row_data->>'tick')::BIGINT AND fact.sequence = (row_data->>'sequence')::BIGINT
        ) THEN
            RAISE EXCEPTION 'open-world V17 freight evidence must match its V16 fact cursor' USING ERRCODE = '23514';
        END IF;
        IF row_data->>'evidence_kind' = 'supply_chain' AND NOT EXISTS (
            SELECT 1 FROM city_open_world_supply_chain_facts fact
            WHERE fact.id = (row_data->>'supply_chain_fact_id')::BIGINT AND fact.world_id = target_world_id
              AND fact.tick = (row_data->>'tick')::BIGINT AND fact.sequence = (row_data->>'sequence')::BIGINT
              AND (
                  fact.fact_type = 'order.delivered'
                  OR (fact.fact_type = 'order.settled' AND row_data->>'fact_type' = 'settlement.confirmed')
              )
        ) THEN
            RAISE EXCEPTION 'open-world V17 receipt evidence must match its V15 delivery or V22 settlement fact cursor'
                USING ERRCODE = '23514';
        END IF;
    ELSIF TG_TABLE_NAME = 'city_open_world_enterprise_freight_shipment_transitions' AND NOT EXISTS (
        SELECT 1 FROM city_open_world_enterprise_freight_receipt_facts fact
        WHERE fact.id = (row_data->>'source_fact_id')::BIGINT AND fact.world_id = target_world_id
          AND fact.shipment_code = row_data->>'shipment_code'
          AND fact.tick = (row_data->>'transition_tick')::BIGINT
          AND fact.sequence = (row_data->>'transition_sequence')::BIGINT
    ) THEN
        RAISE EXCEPTION 'open-world V17 shipment transition must reference its receipt fact cursor'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
