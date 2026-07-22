-- Migrations 257 and 258 extend the already-published realtime-v2 engine
-- with sealed actor and Agent runtime tables. Engine definitions stay
-- immutable for all normal writes; this narrowly permits only the two
-- declarative capability additions required by those immediately following
-- migrations. Without this bridge a fresh database reaches migration 257
-- after the immutable-definition trigger from migration 194 and cannot boot.

CREATE OR REPLACE FUNCTION guard_city_engine_definition_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.version = 'city-openworld-realtime-v2'
       AND NEW.version = OLD.version
       AND NEW.status = OLD.status
       AND NEW.canonical_format = OLD.canonical_format
       AND (
           NEW.capabilities = OLD.capabilities
           OR NEW.capabilities = OLD.capabilities ||
              '["realtime_actors","actor_position_events","member_safe_actor_projection"]'::jsonb
           OR NEW.capabilities = OLD.capabilities ||
              '["realtime_agents","agent_policy_binding","agent_lifecycle"]'::jsonb
       ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'city engine definitions are immutable' USING ERRCODE = '55000';
END;
$$;

COMMENT ON FUNCTION guard_city_engine_definition_immutable() IS
    'Engine definitions are immutable except for the one-time realtime-v2 capability bridge consumed by migrations 257 and 258.';
