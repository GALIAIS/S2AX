-- 城市模拟 F7.3：确定性地块、分区、建筑、单位池、住房占用和跨层 Portal 基线。

CREATE TABLE IF NOT EXISTS city_land_profiles (
    world_id BIGINT PRIMARY KEY REFERENCES city_worlds(id) ON DELETE RESTRICT,
    rule_set_id VARCHAR(64) NOT NULL,
    rule_set_version VARCHAR(24) NOT NULL,
    rule_set_hash VARCHAR(64) NOT NULL,
    spatial_overmap_root_hash VARCHAR(64) NOT NULL,
    nominal_cell_area_sqm BIGINT NOT NULL CHECK (nominal_cell_area_sqm > 0),
    baseline_hash VARCHAR(64) NOT NULL,
    zoning_rule_count BIGINT NOT NULL CHECK (zoning_rule_count >= 0),
    parcel_count BIGINT NOT NULL CHECK (parcel_count >= 0),
    building_count BIGINT NOT NULL CHECK (building_count >= 0),
    unit_pool_count BIGINT NOT NULL CHECK (unit_pool_count >= 0),
    housing_allocation_count BIGINT NOT NULL CHECK (housing_allocation_count >= 0),
    portal_count BIGINT NOT NULL CHECK (portal_count >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision = 1),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_land_profile_rule_set_check CHECK (
        rule_set_id ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND rule_set_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND rule_set_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT city_land_profile_hash_check CHECK (
        spatial_overmap_root_hash ~ '^[0-9a-f]{64}$'
        AND baseline_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT city_land_profile_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_zoning_rules (
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    code VARCHAR(32) NOT NULL,
    name VARCHAR(64) NOT NULL,
    primary_use VARCHAR(16) NOT NULL,
    max_floor_area_ratio_milli BIGINT NOT NULL CHECK (max_floor_area_ratio_milli > 0),
    max_coverage_milli BIGINT NOT NULL CHECK (max_coverage_milli BETWEEN 1 AND 1000),
    max_floors SMALLINT NOT NULL CHECK (max_floors BETWEEN 1 AND 128),
    sqm_per_capacity_unit BIGINT NOT NULL CHECK (sqm_per_capacity_unit > 0),
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status = 'active'),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_zoning_rules_pk PRIMARY KEY (world_id, code),
    CONSTRAINT city_zoning_rule_code_check CHECK (
        code IN ('residential', 'commercial', 'industrial')
        AND primary_use = code
    ),
    CONSTRAINT city_zoning_rule_name_check CHECK (char_length(btrim(name)) BETWEEN 1 AND 64),
    CONSTRAINT city_zoning_rule_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS city_parcels (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    district_id BIGINT NOT NULL,
    code VARCHAR(160) NOT NULL,
    zone_code VARCHAR(32) NOT NULL,
    chunk_x BIGINT NOT NULL,
    chunk_y BIGINT NOT NULL,
    z SMALLINT NOT NULL DEFAULT 0,
    local_min_x SMALLINT NOT NULL,
    local_min_y SMALLINT NOT NULL,
    local_max_x SMALLINT NOT NULL,
    local_max_y SMALLINT NOT NULL,
    area_sqm BIGINT NOT NULL CHECK (area_sqm > 0),
    developable_area_sqm BIGINT NOT NULL CHECK (
        developable_area_sqm > 0 AND developable_area_sqm <= area_sqm
    ),
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status = 'active'),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version = 1),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_parcel_district_fk
        FOREIGN KEY (district_id, world_id)
        REFERENCES city_districts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_parcel_zone_fk
        FOREIGN KEY (world_id, zone_code)
        REFERENCES city_zoning_rules(world_id, code) ON DELETE RESTRICT,
    CONSTRAINT city_parcel_overmap_fk
        FOREIGN KEY (world_id, chunk_x, chunk_y, z)
        REFERENCES city_overmap_tiles(world_id, chunk_x, chunk_y, z) ON DELETE RESTRICT,
    CONSTRAINT city_parcel_code_check CHECK (code ~ '^[a-z][a-z0-9_]{1,159}$'),
    CONSTRAINT city_parcel_geometry_check CHECK (
        chunk_x BETWEEN -4 AND 4 AND chunk_y BETWEEN -4 AND 4 AND z = 0
        AND local_min_x BETWEEN 0 AND 31 AND local_min_y BETWEEN 0 AND 31
        AND local_max_x BETWEEN local_min_x AND 31
        AND local_max_y BETWEEN local_min_y AND 31
    ),
    CONSTRAINT city_parcel_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_parcels_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_parcels_id_world_unique UNIQUE (id, world_id),
    CONSTRAINT city_parcels_world_geometry_unique UNIQUE (
        world_id, chunk_x, chunk_y, z, local_min_x, local_min_y, local_max_x, local_max_y
    )
);

CREATE INDEX IF NOT EXISTS idx_city_parcels_world_bbox
    ON city_parcels (world_id, z, chunk_y, chunk_x, code);
CREATE INDEX IF NOT EXISTS idx_city_parcels_world_district_zone
    ON city_parcels (world_id, district_id, zone_code, code);

CREATE TABLE IF NOT EXISTS city_buildings (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    district_id BIGINT NOT NULL,
    parcel_id BIGINT NOT NULL,
    code VARCHAR(160) NOT NULL,
    primary_use VARCHAR(16) NOT NULL CHECK (
        primary_use IN ('residential', 'commercial', 'industrial')
    ),
    chunk_x BIGINT NOT NULL,
    chunk_y BIGINT NOT NULL,
    footprint_z SMALLINT NOT NULL DEFAULT 0,
    local_min_x SMALLINT NOT NULL,
    local_min_y SMALLINT NOT NULL,
    local_max_x SMALLINT NOT NULL,
    local_max_y SMALLINT NOT NULL,
    base_z SMALLINT NOT NULL,
    top_z SMALLINT NOT NULL,
    floor_count SMALLINT NOT NULL CHECK (floor_count > 0),
    footprint_area_sqm BIGINT NOT NULL CHECK (footprint_area_sqm > 0),
    floor_area_sqm BIGINT NOT NULL CHECK (floor_area_sqm > 0),
    capacity_units BIGINT NOT NULL CHECK (capacity_units > 0),
    occupied_units BIGINT NOT NULL DEFAULT 0 CHECK (
        occupied_units >= 0 AND occupied_units <= capacity_units
    ),
    quality_milli BIGINT NOT NULL DEFAULT 1000 CHECK (quality_milli BETWEEN 1 AND 1000000),
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status = 'active'),
    completed_tick BIGINT NOT NULL DEFAULT 0 CHECK (completed_tick >= 0),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version = 1),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_building_district_fk
        FOREIGN KEY (district_id, world_id)
        REFERENCES city_districts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_building_parcel_fk
        FOREIGN KEY (parcel_id, world_id)
        REFERENCES city_parcels(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_building_code_check CHECK (code ~ '^[a-z][a-z0-9_]{1,159}$'),
    CONSTRAINT city_building_geometry_check CHECK (
        chunk_x BETWEEN -4 AND 4 AND chunk_y BETWEEN -4 AND 4 AND footprint_z = 0
        AND local_min_x BETWEEN 0 AND 31 AND local_min_y BETWEEN 0 AND 31
        AND local_max_x BETWEEN local_min_x AND 31
        AND local_max_y BETWEEN local_min_y AND 31
        AND base_z BETWEEN -32 AND 127 AND top_z BETWEEN base_z AND 127
        AND top_z - base_z + 1 = floor_count
    ),
    CONSTRAINT city_building_area_check CHECK (floor_area_sqm >= footprint_area_sqm),
    CONSTRAINT city_building_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_buildings_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_buildings_world_parcel_unique UNIQUE (world_id, parcel_id),
    CONSTRAINT city_buildings_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_buildings_world_bbox
    ON city_buildings (world_id, footprint_z, chunk_y, chunk_x, code);
CREATE INDEX IF NOT EXISTS idx_city_buildings_world_district_use
    ON city_buildings (world_id, district_id, primary_use, code);

CREATE TABLE IF NOT EXISTS city_building_unit_pools (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    district_id BIGINT NOT NULL,
    building_id BIGINT NOT NULL,
    code VARCHAR(192) NOT NULL,
    use_type VARCHAR(16) NOT NULL CHECK (
        use_type IN ('residential', 'commercial', 'industrial')
    ),
    unit_count BIGINT NOT NULL CHECK (unit_count > 0),
    occupied_unit_count BIGINT NOT NULL DEFAULT 0 CHECK (
        occupied_unit_count >= 0 AND occupied_unit_count <= unit_count
    ),
    capacity_units_per_unit BIGINT NOT NULL DEFAULT 1 CHECK (capacity_units_per_unit > 0),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version = 1),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_building_unit_pool_district_fk
        FOREIGN KEY (district_id, world_id)
        REFERENCES city_districts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_building_unit_pool_building_fk
        FOREIGN KEY (building_id, world_id)
        REFERENCES city_buildings(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_building_unit_pool_code_check CHECK (code ~ '^[a-z][a-z0-9_]{1,191}$'),
    CONSTRAINT city_building_unit_pool_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_building_unit_pools_world_code_unique UNIQUE (world_id, code),
    CONSTRAINT city_building_unit_pools_world_building_unique UNIQUE (world_id, building_id),
    CONSTRAINT city_building_unit_pools_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_building_unit_pools_world_district
    ON city_building_unit_pools (world_id, district_id, use_type, code);

CREATE TABLE IF NOT EXISTS city_housing_allocations (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    district_id BIGINT NOT NULL,
    pool_id BIGINT NOT NULL,
    cohort_id BIGINT NOT NULL,
    cohort_key VARCHAR(192) NOT NULL,
    allocated_units BIGINT NOT NULL CHECK (allocated_units > 0),
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status = 'active'),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version = 1),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_housing_allocation_district_fk
        FOREIGN KEY (district_id, world_id)
        REFERENCES city_districts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_housing_allocation_pool_fk
        FOREIGN KEY (pool_id, world_id)
        REFERENCES city_building_unit_pools(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_housing_allocation_cohort_fk
        FOREIGN KEY (cohort_id, world_id)
        REFERENCES city_household_cohorts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_housing_allocation_key_check CHECK (
        cohort_key ~ '^[a-z][a-z0-9_]{1,31}/[a-z][a-z0-9_]{1,63}/(low|middle|high)$'
    ),
    CONSTRAINT city_housing_allocation_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_housing_allocations_world_pool_cohort_unique UNIQUE (world_id, pool_id, cohort_id),
    CONSTRAINT city_housing_allocations_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_housing_allocations_world_cohort
    ON city_housing_allocations (world_id, cohort_id, pool_id);

CREATE TABLE IF NOT EXISTS city_building_portals (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    district_id BIGINT NOT NULL,
    building_id BIGINT NOT NULL,
    code VARCHAR(64) NOT NULL,
    portal_type VARCHAR(16) NOT NULL CHECK (portal_type IN ('entrance', 'stair')),
    from_x BIGINT NOT NULL,
    from_y BIGINT NOT NULL,
    from_z SMALLINT NOT NULL CHECK (from_z BETWEEN -32 AND 127),
    to_x BIGINT NOT NULL,
    to_y BIGINT NOT NULL,
    to_z SMALLINT NOT NULL CHECK (to_z BETWEEN -32 AND 127),
    bidirectional BOOLEAN NOT NULL DEFAULT TRUE,
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status = 'active'),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version = 1),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_building_portal_district_fk
        FOREIGN KEY (district_id, world_id)
        REFERENCES city_districts(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_building_portal_building_fk
        FOREIGN KEY (building_id, world_id)
        REFERENCES city_buildings(id, world_id) ON DELETE RESTRICT,
    CONSTRAINT city_building_portal_code_check CHECK (code ~ '^[a-z][a-z0-9_]{1,63}$'),
    CONSTRAINT city_building_portal_shape_check CHECK (
        (portal_type = 'entrance' AND from_z = to_z)
        OR (portal_type = 'stair' AND from_x = to_x AND from_y = to_y AND to_z = from_z + 1)
    ),
    CONSTRAINT city_building_portal_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT city_building_portals_world_building_code_unique UNIQUE (world_id, building_id, code),
    CONSTRAINT city_building_portals_id_world_unique UNIQUE (id, world_id)
);

CREATE INDEX IF NOT EXISTS idx_city_building_portals_world_bbox
    ON city_building_portals (world_id, from_z, from_y, from_x, building_id);

CREATE TABLE IF NOT EXISTS city_land_baselines (
    world_id BIGINT PRIMARY KEY REFERENCES city_worlds(id) ON DELETE RESTRICT,
    tick BIGINT NOT NULL CHECK (tick >= 0),
    rule_set_hash VARCHAR(64) NOT NULL CHECK (rule_set_hash ~ '^[0-9a-f]{64}$'),
    baseline_hash VARCHAR(64) NOT NULL CHECK (baseline_hash ~ '^[0-9a-f]{64}$'),
    zoning_rule_count BIGINT NOT NULL CHECK (zoning_rule_count >= 0),
    parcel_count BIGINT NOT NULL CHECK (parcel_count >= 0),
    building_count BIGINT NOT NULL CHECK (building_count >= 0),
    unit_pool_count BIGINT NOT NULL CHECK (unit_pool_count >= 0),
    housing_allocation_count BIGINT NOT NULL CHECK (housing_allocation_count >= 0),
    portal_count BIGINT NOT NULL CHECK (portal_count >= 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    posted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_land_baseline_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE OR REPLACE FUNCTION city_land_foundation_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT city_f7_initialization_write_enabled(target_world_id)
        OR city_engine_upgrade_write_enabled(target_world_id)
        OR city_recovery_write_enabled(target_world_id)
$$;

CREATE OR REPLACE FUNCTION guard_city_land_foundation_projection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
    target_world_id := CASE
        WHEN TG_OP = 'DELETE' THEN (to_jsonb(OLD) ->> 'world_id')::BIGINT
        ELSE (to_jsonb(NEW) ->> 'world_id')::BIGINT
    END;
    IF NOT city_land_foundation_write_enabled(target_world_id) THEN
        RAISE EXCEPTION 'city land foundation is immutable outside genesis, audited upgrade, or verified recovery'
            USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

DO $$
DECLARE
    table_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'city_land_profiles', 'city_zoning_rules', 'city_parcels', 'city_buildings',
        'city_building_unit_pools', 'city_housing_allocations',
        'city_building_portals', 'city_land_baselines'
    ] LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I', table_name || '_projection_guard', table_name);
        EXECUTE format(
            'CREATE TRIGGER %I BEFORE INSERT OR UPDATE OR DELETE ON %I '
            || 'FOR EACH ROW EXECUTE FUNCTION guard_city_land_foundation_projection()',
            table_name || '_projection_guard', table_name
        );
    END LOOP;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_land_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    world_version VARCHAR(32);
    world_tick BIGINT;
    profile_row city_land_profiles%ROWTYPE;
    baseline_row city_land_baselines%ROWTYPE;
    actual_zoning BIGINT;
    actual_parcels BIGINT;
    actual_buildings BIGINT;
    actual_pools BIGINT;
    actual_allocations BIGINT;
    actual_portals BIGINT;
    invalid_count BIGINT;
BEGIN
    SELECT simulation_version, current_tick INTO world_version, world_tick
    FROM city_worlds WHERE id = target_world_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'city land world does not exist' USING ERRCODE = '23514';
    END IF;

    IF world_version <> 'city-f7-v2' THEN
        IF EXISTS (SELECT 1 FROM city_land_profiles WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_zoning_rules WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_parcels WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_buildings WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_building_unit_pools WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_housing_allocations WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_building_portals WHERE world_id = target_world_id)
           OR EXISTS (SELECT 1 FROM city_land_baselines WHERE world_id = target_world_id) THEN
            RAISE EXCEPTION 'legacy city engine cannot contain F7.3 land state' USING ERRCODE = '23514';
        END IF;
        RETURN;
    END IF;

    SELECT * INTO profile_row FROM city_land_profiles WHERE world_id = target_world_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'city F7.3 land profile is missing' USING ERRCODE = '23514';
    END IF;
    SELECT * INTO baseline_row FROM city_land_baselines WHERE world_id = target_world_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'city F7.3 posted land baseline is missing' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO actual_zoning FROM city_zoning_rules WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO actual_parcels FROM city_parcels WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO actual_buildings FROM city_buildings WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO actual_pools FROM city_building_unit_pools WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO actual_allocations FROM city_housing_allocations WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO actual_portals FROM city_building_portals WHERE world_id = target_world_id;

    IF profile_row.rule_set_id <> 'sub2api-land'
       OR profile_row.rule_set_version <> '1.0.0'
       OR profile_row.rule_set_hash <> '4275912ce56d967b3596c5449ef28097623b0d1a9b80ea5f60e1a882f79e60c2'
       OR profile_row.nominal_cell_area_sqm <> 1500
       OR profile_row.spatial_overmap_root_hash IS DISTINCT FROM (
            SELECT overmap_root_hash FROM city_spatial_profiles WHERE world_id = target_world_id
       )
       OR profile_row.baseline_hash <> baseline_row.baseline_hash
       OR profile_row.rule_set_hash <> baseline_row.rule_set_hash
       OR baseline_row.tick > world_tick
       OR (baseline_row.tick > 0 AND NOT EXISTS (
            SELECT 1 FROM city_ticks WHERE world_id = target_world_id AND tick = baseline_row.tick
       ))
       OR (profile_row.zoning_rule_count, profile_row.parcel_count, profile_row.building_count,
           profile_row.unit_pool_count, profile_row.housing_allocation_count, profile_row.portal_count)
          IS DISTINCT FROM
          (actual_zoning, actual_parcels, actual_buildings,
           actual_pools, actual_allocations, actual_portals)
       OR (baseline_row.zoning_rule_count, baseline_row.parcel_count, baseline_row.building_count,
           baseline_row.unit_pool_count, baseline_row.housing_allocation_count, baseline_row.portal_count)
          IS DISTINCT FROM
          (actual_zoning, actual_parcels, actual_buildings,
           actual_pools, actual_allocations, actual_portals) THEN
        RAISE EXCEPTION 'city F7.3 land profile or baseline is inconsistent' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM (VALUES
        ('commercial'::VARCHAR, 4000::BIGINT, 600::BIGINT, 16::SMALLINT, 25::BIGINT),
        ('industrial'::VARCHAR, 1500::BIGINT, 700::BIGINT, 4::SMALLINT, 40::BIGINT),
        ('residential'::VARCHAR, 3000::BIGINT, 450::BIGINT, 12::SMALLINT, 90::BIGINT)
    ) expected(code, far_milli, coverage_milli, max_floors, sqm_per_capacity)
    FULL JOIN (
        SELECT * FROM city_zoning_rules scoped_rule
        WHERE scoped_rule.world_id = target_world_id
    ) rule ON rule.code = expected.code
    WHERE expected.code IS NULL OR rule.code IS NULL
       OR rule.primary_use <> expected.code OR rule.max_floor_area_ratio_milli <> expected.far_milli
       OR rule.max_coverage_milli <> expected.coverage_milli OR rule.max_floors <> expected.max_floors
       OR rule.sqm_per_capacity_unit <> expected.sqm_per_capacity OR rule.status <> 'active';
    IF invalid_count <> 0 OR actual_zoning <> 3 THEN
        RAISE EXCEPTION 'city F7.3 zoning rules do not match the bound rule set' USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_districts district
    LEFT JOIN LATERAL (
        SELECT COALESCE(SUM(parcel.area_sqm), 0)::BIGINT AS area_sqm
        FROM city_parcels parcel
        WHERE parcel.world_id = district.world_id AND parcel.district_id = district.id
    ) parcel_sum ON TRUE
    WHERE district.world_id = target_world_id
      AND parcel_sum.area_sqm <> district.developable_area_units;
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.3 parcel area does not conserve district developable area'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_parcels parcel
    JOIN city_overmap_tiles tile
      ON tile.world_id = parcel.world_id AND tile.chunk_x = parcel.chunk_x
     AND tile.chunk_y = parcel.chunk_y AND tile.z = parcel.z
    WHERE parcel.world_id = target_world_id
      AND (tile.district_id <> parcel.district_id OR parcel.developable_area_sqm <> parcel.area_sqm);
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.3 parcel projection is inconsistent with immutable overmap'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_buildings building
    JOIN city_parcels parcel
      ON parcel.id = building.parcel_id AND parcel.world_id = building.world_id
    JOIN city_zoning_rules rule
      ON rule.world_id = building.world_id AND rule.code = parcel.zone_code
    WHERE building.world_id = target_world_id
      AND (building.district_id <> parcel.district_id OR building.primary_use <> parcel.zone_code
           OR building.chunk_x <> parcel.chunk_x OR building.chunk_y <> parcel.chunk_y
           OR building.footprint_z <> parcel.z
           OR building.local_min_x < parcel.local_min_x OR building.local_min_y < parcel.local_min_y
           OR building.local_max_x > parcel.local_max_x OR building.local_max_y > parcel.local_max_y
           OR building.floor_count > rule.max_floors
           OR building.footprint_area_sqm::NUMERIC
              > parcel.area_sqm::NUMERIC * rule.max_coverage_milli::NUMERIC / 1000
           OR building.floor_area_sqm::NUMERIC
              > parcel.area_sqm::NUMERIC * rule.max_floor_area_ratio_milli::NUMERIC / 1000
           OR building.completed_tick > world_tick);
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.3 building geometry or zoning envelope is invalid'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_districts district
    LEFT JOIN LATERAL (
        SELECT COALESCE(SUM(building.capacity_units) FILTER (WHERE building.primary_use = 'residential'), 0)::BIGINT AS residential,
               COALESCE(SUM(building.capacity_units) FILTER (WHERE building.primary_use = 'commercial'), 0)::BIGINT AS commercial,
               COALESCE(SUM(building.capacity_units) FILTER (WHERE building.primary_use = 'industrial'), 0)::BIGINT AS industrial
        FROM city_buildings building
        WHERE building.world_id = district.world_id AND building.district_id = district.id
    ) capacity ON TRUE
    WHERE district.world_id = target_world_id
      AND (capacity.residential <> district.residential_capacity_units
           OR capacity.commercial <> district.commercial_capacity_units
           OR capacity.industrial <> district.industrial_capacity_units);
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.3 building capacity does not match district aggregates'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_buildings building
    LEFT JOIN city_building_unit_pools pool
      ON pool.world_id = building.world_id AND pool.building_id = building.id
    WHERE building.world_id = target_world_id
      AND (pool.id IS NULL OR pool.district_id <> building.district_id
           OR pool.use_type <> building.primary_use OR pool.capacity_units_per_unit <> 1
           OR pool.unit_count <> building.capacity_units
           OR pool.occupied_unit_count <> building.occupied_units);
    IF invalid_count <> 0 OR actual_pools <> actual_buildings THEN
        RAISE EXCEPTION 'city F7.3 unit pool does not match building capacity'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_housing_allocations allocation
    JOIN city_building_unit_pools pool
      ON pool.id = allocation.pool_id AND pool.world_id = allocation.world_id
    JOIN city_household_cohorts cohort
      ON cohort.id = allocation.cohort_id AND cohort.world_id = allocation.world_id
    JOIN city_districts district
      ON district.id = allocation.district_id AND district.world_id = allocation.world_id
    JOIN city_economic_entities entity
      ON entity.id = cohort.entity_id AND entity.world_id = cohort.world_id
    WHERE allocation.world_id = target_world_id
      AND (pool.use_type <> 'residential' OR pool.district_id <> allocation.district_id
           OR cohort.district_id <> allocation.district_id
           OR allocation.cohort_key <> district.code || '/' || entity.code || '/' || cohort.income_band);
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.3 housing allocation identity is invalid'
            USING ERRCODE = '23514';
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_building_unit_pools pool
    LEFT JOIN LATERAL (
        SELECT COALESCE(SUM(allocation.allocated_units), 0)::BIGINT AS allocated_units
        FROM city_housing_allocations allocation
        WHERE allocation.world_id = pool.world_id AND allocation.pool_id = pool.id
    ) allocated ON TRUE
    WHERE pool.world_id = target_world_id
      AND allocated.allocated_units <> pool.occupied_unit_count;
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.3 housing allocations do not match pool occupancy'
            USING ERRCODE = '23514';
    END IF;

    -- F7.3 captures initial occupancy as an immutable baseline. Compare it to
    -- the live household projection only at that baseline tick; later household
    -- lifecycle changes remain outside this baseline until occupancy facts land.
    IF world_tick = baseline_row.tick THEN
        SELECT COUNT(*) INTO invalid_count
        FROM city_household_cohorts cohort
        LEFT JOIN LATERAL (
            SELECT COALESCE(SUM(allocation.allocated_units), 0)::BIGINT AS allocated_units
            FROM city_housing_allocations allocation
            WHERE allocation.world_id = cohort.world_id AND allocation.cohort_id = cohort.id
        ) allocated ON TRUE
        WHERE cohort.world_id = target_world_id
          AND allocated.allocated_units <> cohort.household_units;
        IF invalid_count <> 0 THEN
            RAISE EXCEPTION 'city F7.3 housing allocations do not conserve household units'
                USING ERRCODE = '23514';
        END IF;
    END IF;

    SELECT COUNT(*) INTO invalid_count
    FROM city_building_portals portal
    JOIN city_buildings building
      ON building.id = portal.building_id AND building.world_id = portal.world_id
    WHERE portal.world_id = target_world_id
      AND (portal.district_id <> building.district_id OR NOT portal.bidirectional
           OR portal.from_z < building.base_z OR portal.from_z > building.top_z
           OR portal.to_z < building.base_z OR portal.to_z > building.top_z
           OR portal.to_x < building.chunk_x * 32 + building.local_min_x
           OR portal.to_x > building.chunk_x * 32 + building.local_max_x
           OR portal.to_y < building.chunk_y * 32 + building.local_min_y
           OR portal.to_y > building.chunk_y * 32 + building.local_max_y);
    IF invalid_count <> 0 THEN
        RAISE EXCEPTION 'city F7.3 building portal projection is invalid'
            USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_city_land_foundation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_world_id BIGINT;
BEGIN
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

DROP TRIGGER IF EXISTS city_land_profile_commit_check ON city_land_profiles;
CREATE CONSTRAINT TRIGGER city_land_profile_commit_check
AFTER INSERT OR UPDATE OR DELETE ON city_land_profiles
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_land_foundation();

DROP TRIGGER IF EXISTS city_land_baseline_commit_check ON city_land_baselines;
CREATE CONSTRAINT TRIGGER city_land_baseline_commit_check
AFTER INSERT OR UPDATE OR DELETE ON city_land_baselines
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_land_foundation();

DROP TRIGGER IF EXISTS city_land_world_commit_check ON city_worlds;
CREATE CONSTRAINT TRIGGER city_land_world_commit_check
AFTER INSERT OR UPDATE ON city_worlds
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_city_land_foundation();

-- F7.2 继续使用 F7.1 的生成域；F7.3 只增加土地基线，不改变 Overmap/Chunk proof。
CREATE OR REPLACE FUNCTION guard_city_spatial_mutation_insert()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    command_type_value VARCHAR(64);
    command_status_value VARCHAR(16);
    world_tick BIGINT;
    world_version VARCHAR(32);
BEGIN
    SELECT command_type, status INTO command_type_value, command_status_value
    FROM city_commands WHERE id = NEW.source_command_id AND world_id = NEW.world_id;
    SELECT current_tick, simulation_version INTO world_tick, world_version
    FROM city_worlds WHERE id = NEW.world_id;
    IF command_type_value IS DISTINCT FROM 'spatial.generate_chunk'
       OR command_status_value IS DISTINCT FROM 'pending'
       OR world_version NOT IN ('city-f7-v1', 'city-f7-v2')
       OR NEW.tick IS DISTINCT FROM world_tick + 1 THEN
        RAISE EXCEPTION 'city spatial mutation does not match a pending spatial generation command'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION assert_city_spatial_foundation(target_world_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    world_version VARCHAR(32);
    profile_count BIGINT;
    tile_count BIGINT;
    invalid_count BIGINT;
BEGIN
    SELECT simulation_version INTO world_version FROM city_worlds WHERE id = target_world_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'city spatial world does not exist' USING ERRCODE = '23514';
    END IF;
    SELECT COUNT(*) INTO profile_count FROM city_spatial_profiles WHERE world_id = target_world_id;
    SELECT COUNT(*) INTO tile_count FROM city_overmap_tiles WHERE world_id = target_world_id;
    IF world_version IN ('city-f7-v1', 'city-f7-v2') THEN
        IF profile_count <> 1 OR tile_count <> 81 THEN
            RAISE EXCEPTION 'city spatial profile or overmap is incomplete' USING ERRCODE = '23514';
        END IF;
        SELECT COUNT(*) INTO invalid_count
        FROM city_overmap_tiles tile
        JOIN city_spatial_profiles profile ON profile.world_id = tile.world_id
        LEFT JOIN city_districts district
          ON district.id = tile.district_id AND district.world_id = tile.world_id
        WHERE tile.world_id = target_world_id
          AND (district.id IS NULL
               OR tile.chunk_x < profile.minimum_chunk_x OR tile.chunk_x > profile.maximum_chunk_x
               OR tile.chunk_y < profile.minimum_chunk_y OR tile.chunk_y > profile.maximum_chunk_y
               OR tile.z <> 0);
        IF invalid_count <> 0 THEN
            RAISE EXCEPTION 'city overmap contains invalid tiles' USING ERRCODE = '23514';
        END IF;
        SELECT COUNT(*) INTO invalid_count
        FROM city_map_chunks chunk
        JOIN city_spatial_profiles profile ON profile.world_id = chunk.world_id
        LEFT JOIN city_overmap_tiles tile
          ON tile.world_id = chunk.world_id
         AND tile.chunk_x = chunk.chunk_x AND tile.chunk_y = chunk.chunk_y AND tile.z = chunk.z
        LEFT JOIN city_spatial_mutations mutation
          ON mutation.id = chunk.source_mutation_id AND mutation.world_id = chunk.world_id
        WHERE chunk.world_id = target_world_id
          AND (tile.world_id IS NULL OR mutation.posted_at IS NULL
               OR chunk.generator_id <> profile.generator_id
               OR chunk.generator_version <> profile.generator_version
               OR chunk.generated_tick <> mutation.tick);
        IF invalid_count <> 0 THEN
            RAISE EXCEPTION 'city chunk projection is inconsistent' USING ERRCODE = '23514';
        END IF;
    ELSIF profile_count <> 0 OR tile_count <> 0
          OR EXISTS (SELECT 1 FROM city_map_chunks WHERE world_id = target_world_id)
          OR EXISTS (SELECT 1 FROM city_spatial_mutations WHERE world_id = target_world_id) THEN
        RAISE EXCEPTION 'legacy city engine cannot contain spatial state' USING ERRCODE = '23514';
    END IF;
END;
$$;

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES ('city-f7-v2', 'supported', 'city-state-v1+gzip',
        '["control","ledger","resources","calendar_demography","population_migration","household_lifecycle","spatial","land","markets","snapshot","replay","recovery"]'::jsonb)
ON CONFLICT (version) DO NOTHING;

INSERT INTO city_engine_upgrade_paths (from_version, to_version, upgrade_code)
VALUES ('city-f7-v1', 'city-f7-v2', 'f7_v1_to_f7_v2')
ON CONFLICT (from_version, to_version) DO NOTHING;
