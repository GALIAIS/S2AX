-- city-openworld-v8 adds a sealed, delayed bridge from a V7 service response
-- to a target-domain metric. Its delivery contract is next_tick_only: a
-- response created in tick T can only become an impact application in a later
-- tick; no same-tick cross-domain mutation is permitted by either the schema
-- or the write guards.

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES (
    'city-openworld-v8', 'supported', 'city-state-v1+gzip',
    '["control","ledger","resources","calendar_demography","open_world_regions","open_world_materialization","vertical_interiors","portal_topology","open_world_runtime","actor_location","actor_occupancy","dynamic_portal_state","open_world_navigation","scenario_binding","version_vector","facilities","npc_lod","social_service_catalog","social_service_access","social_service_queue","social_service_response","cross_domain_impact_bridge","delayed_effects","domain_metrics","activities","rules","case_lifecycle","rewards","snapshot","replay","recovery_verification"]'::jsonb
)
ON CONFLICT (version) DO NOTHING;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-openworld-v7', 'city-openworld-v8', 'openworld_v7_to_v8')
ON CONFLICT (from_version, to_version) DO NOTHING;

CREATE UNIQUE INDEX IF NOT EXISTS idx_city_open_world_service_responses_id_world
    ON city_open_world_service_responses (id, world_id);

CREATE TABLE IF NOT EXISTS city_open_world_impact_profiles (
    world_id BIGINT PRIMARY KEY
        REFERENCES city_open_world_service_profiles(world_id) ON DELETE RESTRICT,
    profile_id VARCHAR(96) NOT NULL,
    profile_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    baseline_tick BIGINT NOT NULL CHECK (baseline_tick >= 0),
    source_contract_version VARCHAR(32) NOT NULL,
    delivery_contract_version VARCHAR(32) NOT NULL,
    maximum_schedules_per_tick INTEGER NOT NULL CHECK (maximum_schedules_per_tick BETWEEN 1 AND 100000),
    effect_count BIGINT NOT NULL DEFAULT 0 CHECK (effect_count >= 0),
    applied_count BIGINT NOT NULL DEFAULT 0 CHECK (applied_count >= 0),
    metric_count BIGINT NOT NULL DEFAULT 0 CHECK (metric_count >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_impact_profile_identity_check CHECK (
        profile_id ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND profile_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND content_hash ~ '^[0-9a-f]{64}$'
        AND source_contract_version ~ '^[a-z][a-z0-9_.-]{1,31}$'
        AND delivery_contract_version ~ '^[a-z][a-z0-9_.-]{1,31}$'
        AND applied_count <= effect_count
    ),
    CONSTRAINT city_open_world_impact_profile_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_open_world_impact_catalog (
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_impact_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    source_kind VARCHAR(64) NOT NULL,
    service_code VARCHAR(64) NOT NULL,
    outcome VARCHAR(16) NOT NULL,
    target_domain VARCHAR(32) NOT NULL,
    metric_code VARCHAR(128) NOT NULL,
    delta_units_per_source_unit BIGINT NOT NULL,
    definition_version VARCHAR(24) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, code),
    CONSTRAINT city_open_world_impact_catalog_service_fk
        FOREIGN KEY (world_id, service_code)
        REFERENCES city_open_world_service_catalog(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_impact_catalog_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND source_kind = 'service.response'
        AND outcome IN ('served', 'expired')
        AND target_domain = 'actor'
        AND metric_code ~ '^[a-z][a-z0-9_.-]{1,127}$'
        AND delta_units_per_source_unit <> 0
        AND definition_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND content_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT city_open_world_impact_catalog_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_impact_catalog_service_outcome_unique
        UNIQUE (world_id, source_kind, service_code, outcome)
);

CREATE TABLE IF NOT EXISTS city_open_world_impact_effects (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_impact_profiles(world_id) ON DELETE RESTRICT,
    code VARCHAR(160) NOT NULL,
    source_response_id BIGINT NOT NULL,
    source_fact_id BIGINT NOT NULL,
    catalog_code VARCHAR(160) NOT NULL,
    target_actor_id BIGINT NOT NULL,
    target_domain VARCHAR(32) NOT NULL,
    target_code VARCHAR(128) NOT NULL,
    metric_code VARCHAR(128) NOT NULL,
    source_units BIGINT NOT NULL CHECK (source_units > 0),
    delta_units BIGINT NOT NULL CHECK (delta_units <> 0),
    scheduled_tick BIGINT NOT NULL CHECK (scheduled_tick > 0 AND scheduled_tick < 9223372036854775807),
    effective_tick BIGINT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'scheduled',
    applied_tick BIGINT,
    application_fact_id BIGINT,
    before_units BIGINT,
    after_units BIGINT,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_open_world_impact_effect_response_fk
        FOREIGN KEY (source_response_id, world_id)
        REFERENCES city_open_world_service_responses(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_impact_effect_source_fact_fk
        FOREIGN KEY (source_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_impact_effect_catalog_fk
        FOREIGN KEY (world_id, catalog_code)
        REFERENCES city_open_world_impact_catalog(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_impact_effect_actor_fk
        FOREIGN KEY (target_actor_id, world_id)
        REFERENCES city_open_world_actors(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_open_world_impact_effect_application_fact_fk
        FOREIGN KEY (application_fact_id, world_id)
        REFERENCES city_open_world_runtime_facts(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_impact_effect_identity_check CHECK (
        code ~ '^[a-z][a-z0-9_.-]{1,159}$'
        AND target_domain = 'actor'
        AND target_code ~ '^[a-z][a-z0-9_.-]{1,127}$'
        AND metric_code ~ '^[a-z][a-z0-9_.-]{1,127}$'
        AND effective_tick = scheduled_tick + 1
        AND status IN ('scheduled', 'applied')
    ),
    CONSTRAINT city_open_world_impact_effect_application_check CHECK (
        (status = 'scheduled'
            AND applied_tick IS NULL AND application_fact_id IS NULL
            AND before_units IS NULL AND after_units IS NULL)
        OR (status = 'applied'
            AND applied_tick IS NOT NULL AND applied_tick >= effective_tick
            AND application_fact_id IS NOT NULL
            AND before_units IS NOT NULL AND after_units IS NOT NULL)
    ),
    CONSTRAINT city_open_world_impact_effect_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_open_world_impact_effects_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_open_world_impact_effects_source_catalog_unique UNIQUE (world_id, source_response_id, catalog_code),
    CONSTRAINT city_open_world_impact_effects_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_impact_effects_due
    ON city_open_world_impact_effects (world_id, effective_tick, code)
    WHERE status = 'scheduled';
CREATE INDEX IF NOT EXISTS idx_city_open_world_impact_effects_source
    ON city_open_world_impact_effects (world_id, source_response_id, catalog_code);

CREATE TABLE IF NOT EXISTS city_open_world_impact_metrics (
    world_id BIGINT NOT NULL
        REFERENCES city_open_world_impact_profiles(world_id) ON DELETE RESTRICT,
    target_domain VARCHAR(32) NOT NULL,
    target_code VARCHAR(128) NOT NULL,
    metric_code VARCHAR(128) NOT NULL,
    value_units BIGINT NOT NULL,
    last_applied_tick BIGINT NOT NULL CHECK (last_applied_tick > 0),
    last_effect_id BIGINT NOT NULL,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, target_domain, target_code, metric_code),
    CONSTRAINT city_open_world_impact_metric_last_effect_fk
        FOREIGN KEY (last_effect_id, world_id)
        REFERENCES city_open_world_impact_effects(id, world_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_open_world_impact_metric_identity_check CHECK (
        target_domain = 'actor'
        AND target_code ~ '^[a-z][a-z0-9_.-]{1,127}$'
        AND metric_code ~ '^[a-z][a-z0-9_.-]{1,127}$'
    ),
    CONSTRAINT city_open_world_impact_metric_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_city_open_world_impact_metrics_target
    ON city_open_world_impact_metrics (world_id, target_domain, target_code, metric_code);

CREATE OR REPLACE FUNCTION city_open_world_impact_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_impact_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1
            FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v8'
              AND world.state_hash IS NULL
              AND (
                  world.current_tick = 0
                  OR EXISTS (
                      SELECT 1 FROM city_world_upgrade_runs upgrade
                      WHERE upgrade.id = CASE
                          WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                          THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT
                          ELSE NULL
                      END
                        AND upgrade.world_id = target_world_id
                        AND upgrade.to_version = 'city-openworld-v8'
                        AND upgrade.status = 'running'
                  )
              )
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_impact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-v8'
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_impact_baseline()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND city_open_world_impact_bootstrap_write_enabled(target_world_id) THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND TG_TABLE_NAME = 'city_open_world_impact_profiles'
       AND city_open_world_impact_write_enabled(target_world_id)
       AND NEW.revision = OLD.revision + 1
       AND NEW.effect_count >= OLD.effect_count
       AND NEW.applied_count >= OLD.applied_count
       AND NEW.metric_count >= OLD.metric_count
       AND NEW.applied_count <= NEW.effect_count
       AND (OLD.world_id, OLD.profile_id, OLD.profile_version, OLD.content_hash,
            OLD.baseline_tick, OLD.source_contract_version, OLD.delivery_contract_version,
            OLD.maximum_schedules_per_tick, OLD.metadata, OLD.created_at)
           IS NOT DISTINCT FROM
           (NEW.world_id, NEW.profile_id, NEW.profile_version, NEW.content_hash,
            NEW.baseline_tick, NEW.source_contract_version, NEW.delivery_contract_version,
            NEW.maximum_schedules_per_tick, NEW.metadata, NEW.created_at) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'open-world impact baseline is immutable outside genesis, audited upgrade, or recovery'
        USING ERRCODE = '55000';
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_impact_effect()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'DELETE' OR NOT city_open_world_impact_write_enabled(target_world_id) THEN
        RAISE EXCEPTION 'open-world impact effects require a runtime fact or recovery context'
            USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'INSERT' THEN
        IF NEW.status <> 'scheduled'
           OR NEW.applied_tick IS NOT NULL OR NEW.application_fact_id IS NOT NULL
           OR NEW.before_units IS NOT NULL OR NEW.after_units IS NOT NULL
           OR NEW.version <> 1
           OR NOT EXISTS (
                SELECT 1
                FROM city_open_world_service_responses response
                JOIN city_open_world_service_requests request_value
                  ON request_value.id = response.request_id AND request_value.world_id = response.world_id
                JOIN city_open_world_impact_catalog catalog
                  ON catalog.world_id = NEW.world_id AND catalog.code = NEW.catalog_code
                JOIN city_open_world_runtime_facts source_fact
                  ON source_fact.id = NEW.source_fact_id AND source_fact.world_id = NEW.world_id
                JOIN city_open_world_actors actor
                  ON actor.id = NEW.target_actor_id AND actor.world_id = NEW.world_id
                WHERE response.id = NEW.source_response_id
                  AND response.world_id = NEW.world_id
                  AND source_fact.posted_at IS NOT NULL
                  AND response.source_fact_id = NEW.source_fact_id
                  AND response.actor_id = NEW.target_actor_id
                  AND actor.code = NEW.target_code
                  AND catalog.source_kind = 'service.response'
                  AND catalog.service_code = response.service_code
                  AND catalog.outcome = response.outcome
                  AND catalog.target_domain = NEW.target_domain
                  AND catalog.metric_code = NEW.metric_code
                  AND NEW.scheduled_tick = response.resolved_tick
                  AND NEW.effective_tick = response.resolved_tick + 1
                  AND NEW.source_units = CASE
                        WHEN response.outcome = 'served' THEN response.delivered_units
                        ELSE request_value.requested_units
                      END
                  AND NEW.delta_units = NEW.source_units * catalog.delta_units_per_source_unit
           ) THEN
            RAISE EXCEPTION 'open-world impact schedule does not match its immutable service response'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    IF OLD.status <> 'scheduled' OR NEW.status <> 'applied'
       OR NEW.applied_tick IS NULL OR NEW.applied_tick < OLD.effective_tick
       OR NEW.application_fact_id IS NULL OR NEW.before_units IS NULL OR NEW.after_units IS NULL
       OR NEW.version <> OLD.version + 1
       OR (OLD.id, OLD.world_id, OLD.code, OLD.source_response_id, OLD.source_fact_id,
           OLD.catalog_code, OLD.target_actor_id, OLD.target_domain, OLD.target_code,
           OLD.metric_code, OLD.source_units, OLD.delta_units, OLD.scheduled_tick,
           OLD.effective_tick, OLD.metadata, OLD.created_at)
          IS DISTINCT FROM
          (NEW.id, NEW.world_id, NEW.code, NEW.source_response_id, NEW.source_fact_id,
           NEW.catalog_code, NEW.target_actor_id, NEW.target_domain, NEW.target_code,
           NEW.metric_code, NEW.source_units, NEW.delta_units, NEW.scheduled_tick,
           NEW.effective_tick, NEW.metadata, NEW.created_at)
       OR NOT EXISTS (
            SELECT 1
            FROM city_open_world_runtime_facts application_fact
            WHERE application_fact.id = NEW.application_fact_id
              AND application_fact.world_id = NEW.world_id
              AND application_fact.tick = NEW.applied_tick
              AND application_fact.parent_fact_id = OLD.source_fact_id
              AND application_fact.actor_id = OLD.target_actor_id
              AND application_fact.fact_type = 'impact.applied'
       ) THEN
        RAISE EXCEPTION 'open-world impact effects permit only one valid scheduled-to-applied transition'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_impact_metric()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := COALESCE(NEW.world_id, OLD.world_id);
    IF city_recovery_write_enabled(target_world_id) THEN
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'DELETE' OR NOT city_open_world_impact_write_enabled(target_world_id) THEN
        RAISE EXCEPTION 'open-world impact metrics require an impact application or recovery context'
            USING ERRCODE = '55000';
    END IF;
    IF (TG_OP = 'INSERT' AND NEW.version <> 1)
       OR (TG_OP = 'UPDATE' AND (
            NEW.version <> OLD.version + 1
            OR (OLD.world_id, OLD.target_domain, OLD.target_code, OLD.metric_code, OLD.created_at)
               IS DISTINCT FROM
               (NEW.world_id, NEW.target_domain, NEW.target_code, NEW.metric_code, NEW.created_at)
       ))
       OR NOT EXISTS (
            SELECT 1
            FROM city_open_world_impact_effects effect_value
            WHERE effect_value.id = NEW.last_effect_id
              AND effect_value.world_id = NEW.world_id
              AND effect_value.target_domain = NEW.target_domain
              AND effect_value.target_code = NEW.target_code
              AND effect_value.metric_code = NEW.metric_code
              AND NEW.last_applied_tick >= effect_value.effective_tick
       ) THEN
        RAISE EXCEPTION 'open-world impact metric is not anchored to its application effect'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_open_world_impact_profile_guard ON city_open_world_impact_profiles;
CREATE TRIGGER city_open_world_impact_profile_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_impact_profiles
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_impact_baseline();

DROP TRIGGER IF EXISTS city_open_world_impact_catalog_guard ON city_open_world_impact_catalog;
CREATE TRIGGER city_open_world_impact_catalog_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_impact_catalog
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_impact_baseline();

DROP TRIGGER IF EXISTS city_open_world_impact_effect_guard ON city_open_world_impact_effects;
CREATE TRIGGER city_open_world_impact_effect_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_impact_effects
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_impact_effect();

DROP TRIGGER IF EXISTS city_open_world_impact_metric_guard ON city_open_world_impact_metrics;
CREATE TRIGGER city_open_world_impact_metric_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_open_world_impact_metrics
FOR EACH ROW EXECUTE FUNCTION guard_city_open_world_impact_metric();

-- V8 widens only the successor-version gates. Earlier generated/static rows
-- keep their prior contracts and are never migrated in place.
CREATE OR REPLACE FUNCTION city_open_world_service_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_service_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1
            FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v7', 'city-openworld-v8')
              AND world.state_hash IS NULL
              AND (
                  world.current_tick = 0
                  OR EXISTS (
                      SELECT 1 FROM city_world_upgrade_runs upgrade
                      WHERE upgrade.id = CASE
                          WHEN COALESCE(current_setting('sub2api.city_upgrade_run_id', TRUE), '') ~ '^[1-9][0-9]*$'
                          THEN current_setting('sub2api.city_upgrade_run_id', TRUE)::BIGINT
                          ELSE NULL
                      END
                        AND upgrade.world_id = target_world_id
                        AND upgrade.to_version = world.simulation_version
                        AND upgrade.status = 'running'
                  )
              )
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_service_fact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT city_open_world_runtime_fact_write_enabled(target_world_id)
       AND EXISTS (
            SELECT 1 FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v7', 'city-openworld-v8')
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_initialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_initialize_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1
            FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN (
                  'city-openworld-v1', 'city-openworld-v2', 'city-openworld-v3',
                  'city-openworld-v4', 'city-openworld-v5', 'city-openworld-v6',
                  'city-openworld-v7', 'city-openworld-v8'
              )
              AND world.current_tick = 0
              AND world.state_hash IS NULL
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_materialization_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_materialization_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1
            FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN (
                  'city-openworld-v2', 'city-openworld-v3', 'city-openworld-v4',
                  'city-openworld-v5', 'city-openworld-v6', 'city-openworld-v7',
                  'city-openworld-v8'
              )
              AND world.current_tick >= 0
              AND world.state_hash IS NOT NULL
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_runtime_bootstrap_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_runtime_bootstrap_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1
            FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v4', 'city-openworld-v5', 'city-openworld-v6', 'city-openworld-v7', 'city-openworld-v8')
              AND world.current_tick = 0
              AND world.state_hash IS NULL
       )
$$;

CREATE OR REPLACE FUNCTION city_open_world_runtime_fact_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_open_world_runtime_fact_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1
            FROM city_worlds world
            WHERE world.id = target_world_id
              AND world.simulation_version IN ('city-openworld-v4', 'city-openworld-v5', 'city-openworld-v6', 'city-openworld-v7', 'city-openworld-v8')
              AND world.current_tick >= 0
              AND world.state_hash IS NOT NULL
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_open_world_runtime_fact_insert()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    world_tick BIGINT;
    world_version VARCHAR(32);
    command_status_value VARCHAR(16);
BEGIN
    IF city_recovery_write_enabled(NEW.world_id) THEN
        RETURN NEW;
    END IF;
    SELECT current_tick, simulation_version INTO world_tick, world_version
    FROM city_worlds WHERE id = NEW.world_id;
    IF world_version NOT IN ('city-openworld-v4', 'city-openworld-v5', 'city-openworld-v6', 'city-openworld-v7', 'city-openworld-v8')
       OR NEW.tick IS DISTINCT FROM world_tick + 1
       OR NEW.posted_at IS NOT NULL THEN
        RAISE EXCEPTION 'open-world runtime fact must be a draft for the next V4/V5/V6/V7/V8 tick'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.source_command_id IS NOT NULL THEN
        SELECT status INTO command_status_value
        FROM city_commands
        WHERE id = NEW.source_command_id AND world_id = NEW.world_id;
        IF command_status_value IS DISTINCT FROM 'pending' THEN
            RAISE EXCEPTION 'open-world runtime fact requires a pending source command'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    IF NEW.source_command_id IS NULL AND NEW.parent_fact_id IS NULL
       AND NOT (world_version IN ('city-openworld-v5', 'city-openworld-v6', 'city-openworld-v7', 'city-openworld-v8') AND NEW.fact_type LIKE 'system.%') THEN
        RAISE EXCEPTION 'derived open-world runtime fact requires a parent fact'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_open_world_service_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    world_version VARCHAR(32);
    profile_tick BIGINT;
BEGIN
    SELECT simulation_version INTO world_version FROM city_worlds WHERE id = target_world_id;
    IF world_version NOT IN ('city-openworld-v7', 'city-openworld-v8') THEN
        RETURN;
    END IF;
    SELECT baseline_tick INTO profile_tick
    FROM city_open_world_service_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL THEN
        RAISE EXCEPTION 'city open-world V7/V8 service profile is missing' USING ERRCODE = '23514';
    END IF;
    IF (SELECT COUNT(*) FROM city_open_world_service_catalog WHERE world_id = target_world_id) <> 4 THEN
        RAISE EXCEPTION 'city open-world V7/V8 service catalog is incomplete' USING ERRCODE = '23514';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM city_open_world_service_providers WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'city open-world V7/V8 service providers are missing' USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_open_world_impact_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    world_version VARCHAR(32);
    profile_tick BIGINT;
    world_tick BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF world_version <> 'city-openworld-v8' THEN
        RETURN;
    END IF;
    SELECT baseline_tick INTO profile_tick
    FROM city_open_world_impact_profiles WHERE world_id = target_world_id;
    IF profile_tick IS NULL OR profile_tick > world_tick THEN
        RAISE EXCEPTION 'city open-world V8 impact profile is missing or has an invalid baseline' USING ERRCODE = '23514';
    END IF;
    IF (SELECT COUNT(*) FROM city_open_world_impact_catalog WHERE world_id = target_world_id) <> 8 THEN
        RAISE EXCEPTION 'city open-world V8 impact catalog is incomplete' USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_world_version_vector(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    world_version VARCHAR(32);
    vector_generation SMALLINT;
    vector_version VARCHAR(32);
BEGIN
    SELECT simulation_version, version_vector_generation INTO world_version, vector_generation
    FROM city_worlds WHERE id = target_world_id;
    IF world_version !~ '^city-openworld-v[0-9]+$' OR vector_generation < 1 THEN
        RAISE EXCEPTION 'city world version vector only applies to versioned open worlds'
            USING ERRCODE = '23514';
    END IF;
    SELECT engine_version INTO vector_version
    FROM city_world_version_vectors
    WHERE world_id = target_world_id AND generation = vector_generation;
    IF vector_version IS DISTINCT FROM world_version THEN
        RAISE EXCEPTION 'city world active version vector header is missing or stale'
            USING ERRCODE = '23514';
    END IF;
    IF (SELECT COUNT(*) FROM city_world_version_bindings
        WHERE world_id = target_world_id AND generation = vector_generation) <> 7 THEN
        RAISE EXCEPTION 'city open-world version vector requires exactly seven bindings'
            USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM (VALUES
            ('content_catalog'), ('economic_policy'), ('engine'), ('rule_bundle'),
            ('scenario'), ('spatial_profile'), ('worldgen_plan')
        ) AS required(component_code)
        WHERE NOT EXISTS (
            SELECT 1 FROM city_world_version_bindings binding
            WHERE binding.world_id = target_world_id
              AND binding.generation = vector_generation
              AND binding.component_code = required.component_code
        )
    ) THEN
        RAISE EXCEPTION 'city open-world version vector is incomplete'
            USING ERRCODE = '23514';
    END IF;
    IF world_version = 'city-openworld-v8' AND NOT EXISTS (
        SELECT 1
        FROM city_world_version_bindings binding
        WHERE binding.world_id = target_world_id
          AND binding.generation = vector_generation
          AND binding.component_code = 'content_catalog'
          AND binding.bundle_id = 'sub2api-open-world-impact-catalog'
          AND binding.bundle_version = '1.0.0'
    ) THEN
        RAISE EXCEPTION 'city open-world V8 impact version vector is incomplete'
            USING ERRCODE = '23514';
    END IF;
END;
$$;
