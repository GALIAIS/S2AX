-- City-world creation builds an interdependent F7+ graph in one transaction.
-- These checks are constraint triggers and therefore run once for every changed
-- row at COMMIT.  During bootstrap each capability is explicitly validated by
-- CityEconomyService.CreateWorld, so repeated full-world checks only turn a
-- valid creation into an O(rows * validation) commit.

CREATE OR REPLACE FUNCTION city_world_bootstrap_checks_suppressed()
RETURNS BOOLEAN
LANGUAGE SQL
AS $$
    SELECT COALESCE(
        current_setting('sub2api.city_world_bootstrap', TRUE),
        'off'
    ) = 'on'
$$;

CREATE OR REPLACE FUNCTION check_city_spatial_foundation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    IF city_world_bootstrap_checks_suppressed() THEN
        RETURN NULL;
    END IF;
    IF TG_TABLE_NAME = 'city_worlds' THEN
        target_world_id := COALESCE(
            (to_jsonb(NEW) ->> 'id')::BIGINT,
            (to_jsonb(OLD) ->> 'id')::BIGINT
        );
    ELSE
        target_world_id := COALESCE(
            (to_jsonb(NEW) ->> 'world_id')::BIGINT,
            (to_jsonb(OLD) ->> 'world_id')::BIGINT
        );
    END IF;
    PERFORM assert_city_spatial_foundation(target_world_id);
    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_land_foundation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    IF city_world_bootstrap_checks_suppressed() THEN
        RETURN NULL;
    END IF;
    IF TG_TABLE_NAME = 'city_worlds' THEN
        target_world_id := COALESCE(
            (to_jsonb(NEW) ->> 'id')::BIGINT,
            (to_jsonb(OLD) ->> 'id')::BIGINT
        );
    ELSE
        target_world_id := COALESCE(
            (to_jsonb(NEW) ->> 'world_id')::BIGINT,
            (to_jsonb(OLD) ->> 'world_id')::BIGINT
        );
    END IF;
    PERFORM assert_city_land_foundation(target_world_id);
    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_development_foundation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    IF city_world_bootstrap_checks_suppressed() THEN
        RETURN NULL;
    END IF;
    target_world_id := CASE
        WHEN TG_TABLE_NAME = 'city_worlds' THEN COALESCE(
            (to_jsonb(NEW) ->> 'id')::BIGINT, (to_jsonb(OLD) ->> 'id')::BIGINT
        )
        ELSE COALESCE(
            (to_jsonb(NEW) ->> 'world_id')::BIGINT, (to_jsonb(OLD) ->> 'world_id')::BIGINT
        )
    END;
    PERFORM assert_city_development_foundation(target_world_id);
    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_enterprise_location_foundation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    IF city_world_bootstrap_checks_suppressed() THEN
        RETURN NULL;
    END IF;
    target_world_id := CASE
        WHEN TG_TABLE_NAME = 'city_worlds' THEN COALESCE(
            (to_jsonb(NEW) ->> 'id')::BIGINT, (to_jsonb(OLD) ->> 'id')::BIGINT
        )
        ELSE COALESCE(
            (to_jsonb(NEW) ->> 'world_id')::BIGINT, (to_jsonb(OLD) ->> 'world_id')::BIGINT
        )
    END;
    PERFORM assert_city_enterprise_location_foundation(target_world_id);
    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION check_world_actor_spatial_control_foundation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    IF city_world_bootstrap_checks_suppressed() THEN
        RETURN NULL;
    END IF;
    target_world_id := COALESCE(
        NULLIF(to_jsonb(NEW)->>'world_id', '')::BIGINT,
        NULLIF(to_jsonb(OLD)->>'world_id', '')::BIGINT,
        NULLIF(to_jsonb(NEW)->>'id', '')::BIGINT,
        NULLIF(to_jsonb(OLD)->>'id', '')::BIGINT
    );
    PERFORM assert_world_runtime_foundation(target_world_id);
    PERFORM assert_world_actor_spatial_control_foundation(target_world_id);
    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION check_world_portal_access_foundation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    IF city_world_bootstrap_checks_suppressed() THEN
        RETURN NULL;
    END IF;
    target_world_id := COALESCE(
        NULLIF(to_jsonb(NEW)->>'world_id', '')::BIGINT,
        NULLIF(to_jsonb(OLD)->>'world_id', '')::BIGINT,
        NULLIF(to_jsonb(NEW)->>'id', '')::BIGINT,
        NULLIF(to_jsonb(OLD)->>'id', '')::BIGINT
    );
    PERFORM assert_world_runtime_foundation(target_world_id);
    PERFORM assert_world_actor_spatial_control_foundation(target_world_id);
    PERFORM assert_world_portal_access_foundation(target_world_id);
    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION check_world_navigation_intent_foundation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    IF city_world_bootstrap_checks_suppressed() THEN
        RETURN NULL;
    END IF;
    target_world_id := COALESCE(
        NULLIF(to_jsonb(NEW)->>'world_id', '')::BIGINT,
        NULLIF(to_jsonb(OLD)->>'world_id', '')::BIGINT,
        NULLIF(to_jsonb(NEW)->>'id', '')::BIGINT,
        NULLIF(to_jsonb(OLD)->>'id', '')::BIGINT
    );
    PERFORM assert_world_runtime_foundation(target_world_id);
    PERFORM assert_world_actor_spatial_control_foundation(target_world_id);
    PERFORM assert_world_portal_access_foundation(target_world_id);
    PERFORM assert_world_navigation_intent_foundation(target_world_id);
    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_service_foundation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    IF city_world_bootstrap_checks_suppressed() THEN
        RETURN NULL;
    END IF;
    target_world_id := COALESCE(
        NULLIF(to_jsonb(NEW)->>'world_id', '')::BIGINT,
        NULLIF(to_jsonb(OLD)->>'world_id', '')::BIGINT,
        NULLIF(to_jsonb(NEW)->>'id', '')::BIGINT,
        NULLIF(to_jsonb(OLD)->>'id', '')::BIGINT
    );
    PERFORM assert_city_spatial_foundation(target_world_id);
    PERFORM assert_city_land_foundation(target_world_id);
    PERFORM assert_city_development_foundation(target_world_id);
    PERFORM assert_world_runtime_foundation(target_world_id);
    PERFORM assert_city_service_foundation(target_world_id);
    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_facility_lifecycle_foundation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF city_world_bootstrap_checks_suppressed() THEN
        RETURN NULL;
    END IF;
    PERFORM assert_city_facility_lifecycle_foundation(COALESCE(NEW.id, OLD.id));
    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_physical_network_foundation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF city_world_bootstrap_checks_suppressed() THEN
        RETURN NULL;
    END IF;
    PERFORM assert_city_physical_network_foundation(COALESCE(NEW.id, OLD.id));
    RETURN NULL;
END;
$$;
