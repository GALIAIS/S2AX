-- V22 extended the resource-operation validator for freight settlement, but
-- replaced the operation-write guard and accidentally dropped the V7
-- enterprise-relocation branch.  Keep both versioned validators in the final
-- dispatch path: relocation operations retain their fact-bound validator,
-- while every other operation receives the V22 validator (which delegates all
-- legacy operation types to the original guard).
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
       AND NEW.market_settlement_id IS NOT DISTINCT FROM OLD.market_settlement_id
       AND NEW.actor_entity_id IS NOT DISTINCT FROM OLD.actor_entity_id
       AND NEW.district_id IS NOT DISTINCT FROM OLD.district_id
       AND NEW.recipe_id IS NOT DISTINCT FROM OLD.recipe_id
       AND NEW.batch_count IS NOT DISTINCT FROM OLD.batch_count
       AND NEW.description IS NOT DISTINCT FROM OLD.description
       AND NEW.metadata IS NOT DISTINCT FROM OLD.metadata
       AND NEW.created_at IS NOT DISTINCT FROM OLD.created_at THEN
        IF NEW.metadata ? 'enterprise_location_fact_id' THEN
            PERFORM assert_city_enterprise_relocation_resource_operation_ready(OLD.id);
        ELSE
            PERFORM assert_city_resource_operation_ready_v22(OLD.id);
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city resource operations permit only one draft-to-posted transition'
        USING ERRCODE = '55000';
END;
$$;
