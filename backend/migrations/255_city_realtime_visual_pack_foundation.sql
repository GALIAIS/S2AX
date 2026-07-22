-- Realtime V2 keeps visual content in a separately versioned content plane.
-- A visual pack can change how a sealed semantic Cell is painted, but never
-- changes collision, world generation, temporal frames, or the physical
-- canonical state hash. World bindings are immutable once genesis completes.

-- Visual manifests are public, member-safe renderer data. Keep their format
-- deliberately small and data-only: URLs, scripts, SVG and arbitrary CSS are
-- never a transport for a visual pack. A later atlas endpoint resolves only
-- approved asset IDs from this immutable manifest.
CREATE OR REPLACE FUNCTION city_visual_manifest_is_safe(
    candidate JSONB,
    expected_render_contract VARCHAR(96)
)
RETURNS BOOLEAN
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
    profile_key TEXT;
    profile_palette JSONB;
    color_key TEXT;
    color_value JSONB;
BEGIN
    IF jsonb_typeof(candidate) <> 'object'
       OR candidate ->> 'schema_version' <> '1'
       OR candidate ->> 'render_mode' <> expected_render_contract
       OR candidate ->> 'logical_tile_px' <> '16'
       OR jsonb_typeof(candidate -> 'profile_palettes') <> 'object'
       OR jsonb_typeof(candidate -> 'assets') <> 'array'
       OR NOT (candidate -> 'profile_palettes' ? 'default')
       OR candidate::TEXT ~* '(https?://|data:|javascript:|<svg|<script)' THEN
        RETURN FALSE;
    END IF;

    FOR profile_key, profile_palette IN
        SELECT key, value FROM jsonb_each(candidate -> 'profile_palettes')
    LOOP
        IF (profile_key <> 'default'
            AND profile_key !~ '^[a-z][a-z0-9_.-]{1,63}$')
           OR jsonb_typeof(profile_palette) <> 'object' THEN
            RETURN FALSE;
        END IF;
        FOR color_key, color_value IN SELECT key, value FROM jsonb_each(profile_palette)
        LOOP
            IF color_key NOT IN (
                'map_background', 'ground', 'soil', 'road', 'water',
                'building_residential', 'building_commercial', 'building_industrial',
                'structure', 'portal', 'furniture', 'item', 'entity', 'overlay', 'window'
            )
               OR jsonb_typeof(color_value) <> 'string'
               OR (color_value #>> '{}') !~* '^#[0-9a-f]{6}$' THEN
                RETURN FALSE;
            END IF;
        END LOOP;
    END LOOP;
    RETURN TRUE;
END;
$$;

CREATE TABLE IF NOT EXISTS city_visual_packs (
    pack_id VARCHAR(96) NOT NULL,
    pack_version VARCHAR(24) NOT NULL,
    status VARCHAR(16) NOT NULL,
    semantic_projection_version VARCHAR(96) NOT NULL,
    render_contract_version VARCHAR(96) NOT NULL,
    compatibility JSONB NOT NULL,
    manifest JSONB NOT NULL,
    manifest_hash VARCHAR(64) NOT NULL,
    asset_set_hash VARCHAR(64) NOT NULL,
    provenance JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ,
    PRIMARY KEY (pack_id, pack_version),
    CONSTRAINT city_visual_packs_identity_check CHECK (
        pack_id ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND pack_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND status IN ('staging', 'published', 'retired', 'revoked')
        AND semantic_projection_version ~ '^city-[a-z0-9_.-]{3,95}$'
        AND render_contract_version IN ('procedural_pixel_v1', 'atlas_pixel_v1')
        AND jsonb_typeof(compatibility) = 'object'
        AND jsonb_typeof(manifest) = 'object'
        AND jsonb_typeof(provenance) = 'object'
        AND jsonb_typeof(compatibility -> 'spatial_profile_ids') = 'array'
        AND jsonb_typeof(compatibility -> 'semantic_projection_versions') = 'array'
        AND city_visual_manifest_is_safe(manifest, render_contract_version)
        AND manifest_hash ~ '^[0-9a-f]{64}$'
        AND asset_set_hash ~ '^[0-9a-f]{64}$'
        AND manifest_hash = encode(sha256(convert_to(manifest::text, 'UTF8')), 'hex')
        AND ((status = 'published' AND published_at IS NOT NULL)
             OR (status <> 'published'))
    )
);

CREATE TABLE IF NOT EXISTS city_visual_assets (
    pack_id VARCHAR(96) NOT NULL,
    pack_version VARCHAR(24) NOT NULL,
    asset_id VARCHAR(128) NOT NULL,
    asset_kind VARCHAR(32) NOT NULL,
    status VARCHAR(16) NOT NULL,
    file_format VARCHAR(8) NOT NULL,
    storage_key VARCHAR(256) NOT NULL,
    pixel_width INTEGER NOT NULL,
    pixel_height INTEGER NOT NULL,
    logical_tile_px SMALLINT NOT NULL DEFAULT 16,
    content_hash VARCHAR(64) NOT NULL,
    provenance JSONB NOT NULL DEFAULT '{}'::jsonb,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (pack_id, pack_version, asset_id),
    CONSTRAINT city_visual_assets_pack_fk
        FOREIGN KEY (pack_id, pack_version)
        REFERENCES city_visual_packs(pack_id, pack_version) ON DELETE RESTRICT,
    CONSTRAINT city_visual_assets_identity_check CHECK (
        asset_id ~ '^[a-z][a-z0-9_.-]{1,127}$'
        AND asset_kind IN ('terrain', 'infrastructure', 'building_exterior', 'interior', 'furniture', 'item', 'vehicle', 'character_base', 'character_wear', 'effect', 'marker', 'atlas')
        AND status IN ('staging', 'approved', 'published', 'retired', 'rejected')
        AND file_format IN ('png', 'webp')
        AND storage_key ~ '^[a-z0-9][a-z0-9/_.-]{1,255}$'
        AND storage_key !~ '(^|/)\.\.(/|$)'
        AND pixel_width BETWEEN 1 AND 8192
        AND pixel_height BETWEEN 1 AND 8192
        AND logical_tile_px IN (8, 16, 24, 32)
        AND content_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(provenance) = 'object'
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE TABLE IF NOT EXISTS city_visual_asset_variants (
    pack_id VARCHAR(96) NOT NULL,
    pack_version VARCHAR(24) NOT NULL,
    semantic_namespace VARCHAR(32) NOT NULL,
    semantic_key VARCHAR(128) NOT NULL,
    variant_code VARCHAR(64) NOT NULL,
    asset_id VARCHAR(128) NOT NULL,
    frame_x INTEGER NOT NULL DEFAULT 0,
    frame_y INTEGER NOT NULL DEFAULT 0,
    frame_width INTEGER NOT NULL,
    frame_height INTEGER NOT NULL,
    anchor_x SMALLINT NOT NULL DEFAULT 0,
    anchor_y SMALLINT NOT NULL DEFAULT 0,
    draw_layer SMALLINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (pack_id, pack_version, semantic_namespace, semantic_key, variant_code),
    CONSTRAINT city_visual_asset_variants_asset_fk
        FOREIGN KEY (pack_id, pack_version, asset_id)
        REFERENCES city_visual_assets(pack_id, pack_version, asset_id) ON DELETE RESTRICT,
    CONSTRAINT city_visual_asset_variants_identity_check CHECK (
        semantic_namespace ~ '^[a-z][a-z0-9_.-]{1,31}$'
        AND semantic_key ~ '^[a-z][a-z0-9_.-]{1,127}$'
        AND variant_code ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND frame_x >= 0 AND frame_y >= 0
        AND frame_width BETWEEN 1 AND 8192
        AND frame_height BETWEEN 1 AND 8192
        AND draw_layer BETWEEN 0 AND 127
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE TABLE IF NOT EXISTS city_visual_generation_jobs (
    id BIGSERIAL PRIMARY KEY,
    pack_id VARCHAR(96) NOT NULL,
    pack_version VARCHAR(24) NOT NULL,
    asset_class VARCHAR(32) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'draft',
    request_spec JSONB NOT NULL,
    candidate_hash VARCHAR(64),
    review JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by_user_id BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    reviewed_by_user_id BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reviewed_at TIMESTAMPTZ,
    CONSTRAINT city_visual_generation_jobs_pack_fk
        FOREIGN KEY (pack_id, pack_version)
        REFERENCES city_visual_packs(pack_id, pack_version) ON DELETE RESTRICT,
    CONSTRAINT city_visual_generation_jobs_identity_check CHECK (
        asset_class IN ('terrain', 'infrastructure', 'building_exterior', 'interior', 'furniture', 'item', 'vehicle', 'character_base', 'character_wear', 'effect', 'marker')
        AND status IN ('draft', 'queued', 'generated', 'reviewing', 'approved', 'rejected', 'cancelled', 'failed')
        AND jsonb_typeof(request_spec) = 'object'
        AND jsonb_typeof(review) = 'object'
        AND (candidate_hash IS NULL OR candidate_hash ~ '^[0-9a-f]{64}$')
    )
);

CREATE INDEX IF NOT EXISTS idx_city_visual_generation_jobs_queue
    ON city_visual_generation_jobs (status, created_at ASC);

CREATE TABLE IF NOT EXISTS city_world_visual_bindings (
    world_id BIGINT PRIMARY KEY REFERENCES city_realtime_spatial_bindings(world_id) ON DELETE RESTRICT,
    pack_id VARCHAR(96) NOT NULL,
    pack_version VARCHAR(24) NOT NULL,
    spatial_profile_id VARCHAR(64) NOT NULL,
    semantic_projection_version VARCHAR(96) NOT NULL,
    render_contract_version VARCHAR(96) NOT NULL,
    manifest_hash VARCHAR(64) NOT NULL,
    asset_set_hash VARCHAR(64) NOT NULL,
    binding_hash VARCHAR(64) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_world_visual_bindings_pack_fk
        FOREIGN KEY (pack_id, pack_version)
        REFERENCES city_visual_packs(pack_id, pack_version) ON DELETE RESTRICT,
    CONSTRAINT city_world_visual_bindings_identity_check CHECK (
        spatial_profile_id ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND semantic_projection_version ~ '^city-[a-z0-9_.-]{3,95}$'
        AND render_contract_version ~ '^[a-z][a-z0-9_.-]{3,95}$'
        AND manifest_hash ~ '^[0-9a-f]{64}$'
        AND asset_set_hash ~ '^[0-9a-f]{64}$'
        AND binding_hash ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(metadata) = 'object'
    )
);

CREATE INDEX IF NOT EXISTS idx_city_world_visual_bindings_pack
    ON city_world_visual_bindings (pack_id, pack_version);

-- Bind a pack to the exact renderer-relevant asset and frame inventory. The
-- hash is recomputed during publication; storage credentials and generation
-- prompts are intentionally absent from it.
CREATE OR REPLACE FUNCTION city_visual_pack_asset_set_hash(
    target_pack_id VARCHAR(96),
    target_pack_version VARCHAR(24)
)
RETURNS VARCHAR(64)
LANGUAGE SQL
STABLE
AS $$
    SELECT encode(sha256(convert_to(
        jsonb_build_object(
            'assets', COALESCE((
                SELECT jsonb_agg(jsonb_build_object(
                    'asset_id', asset.asset_id,
                    'asset_kind', asset.asset_kind,
                    'status', asset.status,
                    'file_format', asset.file_format,
                    'storage_key', asset.storage_key,
                    'pixel_width', asset.pixel_width,
                    'pixel_height', asset.pixel_height,
                    'logical_tile_px', asset.logical_tile_px,
                    'content_hash', asset.content_hash,
                    'metadata', asset.metadata
                ) ORDER BY asset.asset_id)
                FROM city_visual_assets asset
                WHERE asset.pack_id = target_pack_id
                  AND asset.pack_version = target_pack_version
            ), '[]'::jsonb),
            'variants', COALESCE((
                SELECT jsonb_agg(jsonb_build_object(
                    'semantic_namespace', variant.semantic_namespace,
                    'semantic_key', variant.semantic_key,
                    'variant_code', variant.variant_code,
                    'asset_id', variant.asset_id,
                    'frame_x', variant.frame_x,
                    'frame_y', variant.frame_y,
                    'frame_width', variant.frame_width,
                    'frame_height', variant.frame_height,
                    'anchor_x', variant.anchor_x,
                    'anchor_y', variant.anchor_y,
                    'draw_layer', variant.draw_layer,
                    'metadata', variant.metadata
                ) ORDER BY variant.semantic_namespace, variant.semantic_key, variant.variant_code)
                FROM city_visual_asset_variants variant
                WHERE variant.pack_id = target_pack_id
                  AND variant.pack_version = target_pack_version
            ), '[]'::jsonb)
        )::TEXT,
        'UTF8'
    )), 'hex');
$$;

-- The first pack is intentionally procedural and contains no image assets.
-- It proves the immutable manifest/binding contract while the audited ImageGen
-- atlas pipeline is built. The renderer has a strict finite fallback palette;
-- this pack cannot supply an arbitrary URL, script, SVG, or user-controlled
-- image to a client.
WITH default_pack AS (
    SELECT $manifest$
{
  "schema_version": 1,
  "render_mode": "procedural_pixel_v1",
  "logical_tile_px": 16,
  "profile_palettes": {
    "default": {
      "ground": "#5f8259", "soil": "#a57a50", "road": "#77736b", "water": "#3b6f97",
      "building_residential": "#b66f69", "building_commercial": "#d29a55", "building_industrial": "#8393a4",
      "structure": "#343332", "portal": "#e1bd66", "furniture": "#aa704a", "overlay": "#70b8aa"
    },
    "jp.metropolitan": {
      "ground": "#6b9468", "soil": "#b78c61", "road": "#6d7370", "water": "#4a83ad",
      "building_residential": "#bd7770", "building_commercial": "#d8a458", "building_industrial": "#8998aa",
      "structure": "#3a3835", "portal": "#eccb76", "furniture": "#b47a52", "overlay": "#75c3b4"
    },
    "cn.metropolitan": {
      "ground": "#6f8d61", "soil": "#a87c4e", "road": "#74716a", "water": "#437aa0",
      "building_residential": "#b76d62", "building_commercial": "#ce9250", "building_industrial": "#788e9f",
      "structure": "#393632", "portal": "#e0ba63", "furniture": "#a66e47", "overlay": "#70b5a1"
    }
  },
  "semantic_rules": {
    "terrain": ["deep_water", "water", "road", "floor", "soil", "sand", "grass"],
    "building_uses": ["residential", "commercial", "industrial"],
    "layers": ["structure", "portal", "furniture", "item", "entity", "field", "overlay"]
  },
  "assets": []
}
$manifest$::jsonb AS manifest
)
INSERT INTO city_visual_packs
    (pack_id, pack_version, status, semantic_projection_version, render_contract_version,
     compatibility, manifest, manifest_hash, asset_set_hash, provenance, published_at)
SELECT
    'city-pixel-core', '1.0.0', 'published',
    'city-realtime-semantic-pixel-v1', 'procedural_pixel_v1',
    '{"spatial_profile_ids":["*"],"semantic_projection_versions":["city-realtime-semantic-pixel-v1"]}'::jsonb,
    manifest,
    encode(sha256(convert_to(manifest::text, 'UTF8')), 'hex'),
    city_visual_pack_asset_set_hash('city-pixel-core', '1.0.0'),
    '{"source_kind":"procedural_bootstrap","rights":"project_original","generation_required":true}'::jsonb,
    NOW()
FROM default_pack
ON CONFLICT (pack_id, pack_version) DO NOTHING;

-- Existing V2 worlds retain their physical state hash. Their binding is a
-- content-plane backfill, not a hidden physics/timeline rewrite.
WITH default_pack AS (
    SELECT pack_id, pack_version, semantic_projection_version, render_contract_version,
           manifest_hash, asset_set_hash
    FROM city_visual_packs
    WHERE pack_id = 'city-pixel-core' AND pack_version = '1.0.0' AND status = 'published'
)
INSERT INTO city_world_visual_bindings
    (world_id, pack_id, pack_version, spatial_profile_id, semantic_projection_version,
     render_contract_version, manifest_hash, asset_set_hash, binding_hash, metadata)
SELECT
    spatial.world_id, pack.pack_id, pack.pack_version, spatial.profile_id,
    pack.semantic_projection_version, pack.render_contract_version,
    pack.manifest_hash, pack.asset_set_hash,
    encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-visual-binding-v1', pack.pack_id, pack.pack_version,
        spatial.profile_id, pack.semantic_projection_version, pack.render_contract_version,
        pack.manifest_hash, pack.asset_set_hash), 'UTF8')), 'hex'),
    '{"binding_source":"migration_backfill"}'::jsonb
FROM city_realtime_spatial_bindings spatial
JOIN city_world_time_states time_state ON time_state.world_id = spatial.world_id
CROSS JOIN default_pack pack
WHERE time_state.temporal_engine_version = 'city-openworld-realtime-v2'
ON CONFLICT (world_id) DO NOTHING;

CREATE OR REPLACE FUNCTION guard_city_visual_pack_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' AND NEW.status = 'published' THEN
        IF NEW.asset_set_hash <> city_visual_pack_asset_set_hash(NEW.pack_id, NEW.pack_version) THEN
            RAISE EXCEPTION 'published city visual pack asset set hash mismatch'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    IF TG_OP = 'DELETE' AND OLD.status IN ('published', 'retired', 'revoked') THEN
        RAISE EXCEPTION 'published city visual pack is immutable'
            USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'UPDATE' AND OLD.status IN ('retired', 'revoked') THEN
        RAISE EXCEPTION 'published city visual pack is immutable'
            USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'UPDATE' AND OLD.status = 'published' AND (
        NEW.status <> 'retired'
        OR NEW.pack_id IS DISTINCT FROM OLD.pack_id
        OR NEW.pack_version IS DISTINCT FROM OLD.pack_version
        OR NEW.semantic_projection_version IS DISTINCT FROM OLD.semantic_projection_version
        OR NEW.render_contract_version IS DISTINCT FROM OLD.render_contract_version
        OR NEW.compatibility IS DISTINCT FROM OLD.compatibility
        OR NEW.manifest IS DISTINCT FROM OLD.manifest
        OR NEW.manifest_hash IS DISTINCT FROM OLD.manifest_hash
        OR NEW.asset_set_hash IS DISTINCT FROM OLD.asset_set_hash
        OR NEW.provenance IS DISTINCT FROM OLD.provenance
        OR NEW.published_at IS DISTINCT FROM OLD.published_at
    ) THEN
        RAISE EXCEPTION 'published city visual pack is immutable'
            USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'UPDATE' AND OLD.status = 'staging' AND NEW.status = 'published' THEN
        IF NEW.asset_set_hash <> city_visual_pack_asset_set_hash(NEW.pack_id, NEW.pack_version) THEN
            RAISE EXCEPTION 'published city visual pack asset set hash mismatch'
                USING ERRCODE = '23514';
        END IF;
        IF EXISTS (
            SELECT 1
            FROM city_visual_assets asset
            WHERE asset.pack_id = NEW.pack_id
              AND asset.pack_version = NEW.pack_version
              AND asset.status <> 'published'
        ) THEN
            RAISE EXCEPTION 'published city visual pack contains non-published assets'
                USING ERRCODE = '23514';
        END IF;
        IF NEW.render_contract_version = 'atlas_pixel_v1' AND NOT EXISTS (
            SELECT 1
            FROM city_visual_assets asset
            WHERE asset.pack_id = NEW.pack_id
              AND asset.pack_version = NEW.pack_version
              AND asset.asset_kind = 'atlas'
              AND asset.status = 'published'
        ) THEN
            RAISE EXCEPTION 'atlas visual pack requires a published atlas asset'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_visual_packs_immutable_guard ON city_visual_packs;
CREATE TRIGGER city_visual_packs_immutable_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_visual_packs
FOR EACH ROW EXECUTE FUNCTION guard_city_visual_pack_mutation();

CREATE OR REPLACE FUNCTION guard_city_visual_pack_children()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_pack_id VARCHAR(96);
    target_pack_version VARCHAR(24);
    target_status VARCHAR(16);
BEGIN
    IF TG_OP = 'DELETE' THEN
        target_pack_id := OLD.pack_id;
        target_pack_version := OLD.pack_version;
    ELSE
        target_pack_id := NEW.pack_id;
        target_pack_version := NEW.pack_version;
    END IF;
    IF TG_OP = 'UPDATE' AND (
        NEW.pack_id IS DISTINCT FROM OLD.pack_id
        OR NEW.pack_version IS DISTINCT FROM OLD.pack_version
    ) THEN
        RAISE EXCEPTION 'city visual asset records cannot move between packs'
            USING ERRCODE = '55000';
    END IF;
    SELECT status INTO target_status
    FROM city_visual_packs
    WHERE pack_id = target_pack_id AND pack_version = target_pack_version;
    IF NOT FOUND OR target_status <> 'staging' THEN
        RAISE EXCEPTION 'published city visual assets are immutable'
            USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_visual_assets_pack_guard ON city_visual_assets;
CREATE TRIGGER city_visual_assets_pack_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_visual_assets
FOR EACH ROW EXECUTE FUNCTION guard_city_visual_pack_children();

DROP TRIGGER IF EXISTS city_visual_asset_variants_pack_guard ON city_visual_asset_variants;
CREATE TRIGGER city_visual_asset_variants_pack_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_visual_asset_variants
FOR EACH ROW EXECUTE FUNCTION guard_city_visual_pack_children();

DROP TRIGGER IF EXISTS city_visual_generation_jobs_pack_guard ON city_visual_generation_jobs;
CREATE TRIGGER city_visual_generation_jobs_pack_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_visual_generation_jobs
FOR EACH ROW EXECUTE FUNCTION guard_city_visual_pack_children();

CREATE OR REPLACE FUNCTION city_realtime_visual_binding_write_enabled(target_world_id BIGINT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT COALESCE(current_setting('sub2api.city_realtime_visual_binding_initialize_world_id', TRUE), '') = target_world_id::TEXT
       AND EXISTS (
            SELECT 1
            FROM city_worlds world
            JOIN city_world_time_states state ON state.world_id = world.id
            WHERE world.id = target_world_id
              AND world.simulation_version = 'city-openworld-realtime-v2'
              AND state.temporal_engine_version = 'city-openworld-realtime-v2'
              AND world.current_tick = 0
              AND world.state_hash IS NULL
       )
$$;

CREATE OR REPLACE FUNCTION guard_city_realtime_visual_binding()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    expected_manifest_hash VARCHAR(64);
    expected_asset_set_hash VARCHAR(64);
    expected_semantic_version VARCHAR(96);
    expected_render_contract VARCHAR(96);
    compatibility JSONB;
    expected_profile_id VARCHAR(64);
    expected_binding_hash VARCHAR(64);
BEGIN
    IF TG_OP <> 'INSERT' OR NOT city_realtime_visual_binding_write_enabled(NEW.world_id) THEN
        RAISE EXCEPTION 'city realtime visual binding is immutable outside genesis initialization'
            USING ERRCODE = '55000';
    END IF;

    SELECT pack.manifest_hash, pack.asset_set_hash, pack.semantic_projection_version,
           pack.render_contract_version, pack.compatibility
    INTO expected_manifest_hash, expected_asset_set_hash, expected_semantic_version,
         expected_render_contract, compatibility
    FROM city_visual_packs pack
    WHERE pack.pack_id = NEW.pack_id
      AND pack.pack_version = NEW.pack_version
      AND pack.status = 'published';
    IF NOT FOUND THEN
        RAISE EXCEPTION 'city realtime visual binding references a non-published pack'
            USING ERRCODE = '23503';
    END IF;

    SELECT profile_id INTO expected_profile_id
    FROM city_realtime_spatial_bindings
    WHERE world_id = NEW.world_id;
    IF NOT FOUND OR NEW.spatial_profile_id <> expected_profile_id THEN
        RAISE EXCEPTION 'city realtime visual binding profile does not match sealed spatial profile'
            USING ERRCODE = '23514';
    END IF;
    IF NOT ((compatibility -> 'spatial_profile_ids') ? expected_profile_id
            OR (compatibility -> 'spatial_profile_ids') ? '*')
       OR NOT ((compatibility -> 'semantic_projection_versions') ? NEW.semantic_projection_version) THEN
        RAISE EXCEPTION 'city realtime visual pack is not compatible with the sealed world projection'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.manifest_hash <> expected_manifest_hash
       OR NEW.asset_set_hash <> expected_asset_set_hash
       OR NEW.semantic_projection_version <> expected_semantic_version
       OR NEW.render_contract_version <> expected_render_contract THEN
        RAISE EXCEPTION 'city realtime visual binding hash or contract mismatch'
            USING ERRCODE = '23514';
    END IF;

    expected_binding_hash := encode(sha256(convert_to(concat_ws(E'\x1f',
        'city-realtime-visual-binding-v1', NEW.pack_id, NEW.pack_version,
        NEW.spatial_profile_id, NEW.semantic_projection_version, NEW.render_contract_version,
        NEW.manifest_hash, NEW.asset_set_hash), 'UTF8')), 'hex');
    IF NEW.binding_hash <> expected_binding_hash THEN
        RAISE EXCEPTION 'city realtime visual binding hash mismatch'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_world_visual_bindings_guard ON city_world_visual_bindings;
CREATE TRIGGER city_world_visual_bindings_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_world_visual_bindings
FOR EACH ROW EXECUTE FUNCTION guard_city_realtime_visual_binding();
