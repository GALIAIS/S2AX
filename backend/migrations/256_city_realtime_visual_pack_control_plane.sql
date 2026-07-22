-- The visual content plane is deliberately released through a small, audited
-- policy layer. A pack is never selected by a browser or by a world-creation
-- payload: a server-owned release policy picks one published, compatible pack
-- for each semantic projection/profile pair. Existing world bindings remain
-- immutable and therefore never change when this policy changes.

CREATE TABLE IF NOT EXISTS city_visual_pack_release_policies (
    semantic_projection_version VARCHAR(96) NOT NULL,
    spatial_profile_id VARCHAR(64) NOT NULL,
    pack_id VARCHAR(96) NOT NULL,
    pack_version VARCHAR(24) NOT NULL,
    created_by_user_id BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    updated_by_user_id BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (semantic_projection_version, spatial_profile_id),
    CONSTRAINT city_visual_pack_release_policies_identity_check CHECK (
        semantic_projection_version ~ '^city-[a-z0-9_.-]{3,95}$'
        AND (spatial_profile_id = '*' OR spatial_profile_id ~ '^[a-z][a-z0-9_.-]{1,63}$')
    ),
    CONSTRAINT city_visual_pack_release_policies_pack_fk
        FOREIGN KEY (pack_id, pack_version)
        REFERENCES city_visual_packs(pack_id, pack_version) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_city_visual_pack_release_policies_pack
    ON city_visual_pack_release_policies (pack_id, pack_version);

-- The currently shipped Canvas renderer is procedural-only. Atlas packs stay
-- reviewable in the content schema, but cannot be released until the bounded
-- atlas loader, storage isolation and client-side integrity checks exist.
CREATE OR REPLACE FUNCTION guard_city_visual_pack_release_policy()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_status VARCHAR(16);
    target_semantic_projection_version VARCHAR(96);
    target_render_contract_version VARCHAR(96);
    target_compatibility JSONB;
BEGIN
    SELECT pack.status,
           pack.semantic_projection_version,
           pack.render_contract_version,
           pack.compatibility
    INTO target_status,
         target_semantic_projection_version,
         target_render_contract_version,
         target_compatibility
    FROM city_visual_packs pack
    WHERE pack.pack_id = NEW.pack_id
      AND pack.pack_version = NEW.pack_version;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'city visual release policy references an unknown pack'
            USING ERRCODE = '23503';
    END IF;
    IF target_status <> 'published' THEN
        RAISE EXCEPTION 'city visual release policy requires a published pack'
            USING ERRCODE = '23514';
    END IF;
    IF target_semantic_projection_version <> NEW.semantic_projection_version THEN
        RAISE EXCEPTION 'city visual release policy semantic projection mismatch'
            USING ERRCODE = '23514';
    END IF;
    IF target_render_contract_version <> 'procedural_pixel_v1' THEN
        RAISE EXCEPTION 'city visual release policy renderer contract is not deployable'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.spatial_profile_id = '*' THEN
        IF NOT ((target_compatibility -> 'spatial_profile_ids') ? '*') THEN
            RAISE EXCEPTION 'wildcard city visual release policy requires wildcard-compatible pack'
                USING ERRCODE = '23514';
        END IF;
    ELSIF NOT ((target_compatibility -> 'spatial_profile_ids') ? NEW.spatial_profile_id
                OR (target_compatibility -> 'spatial_profile_ids') ? '*') THEN
        RAISE EXCEPTION 'city visual release policy profile is not compatible with pack'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_visual_pack_release_policies_guard ON city_visual_pack_release_policies;
CREATE TRIGGER city_visual_pack_release_policies_guard
BEFORE INSERT OR UPDATE ON city_visual_pack_release_policies
FOR EACH ROW EXECUTE FUNCTION guard_city_visual_pack_release_policy();

-- Bootstrap only the policy, not a mutable per-world default. New worlds look
-- up this policy during genesis, whereas worlds created before or after a
-- policy change keep their already-sealed visual binding.
INSERT INTO city_visual_pack_release_policies
    (semantic_projection_version, spatial_profile_id, pack_id, pack_version)
SELECT 'city-realtime-semantic-pixel-v1', '*', 'city-pixel-core', '1.0.0'
WHERE EXISTS (
    SELECT 1
    FROM city_visual_packs pack
    WHERE pack.pack_id = 'city-pixel-core'
      AND pack.pack_version = '1.0.0'
      AND pack.status = 'published'
)
ON CONFLICT (semantic_projection_version, spatial_profile_id) DO NOTHING;

-- Review events are append-only administrative evidence. They intentionally
-- store only finite status/identifier metadata, never prompts, provider URLs,
-- storage paths, credentials, generated pixels, or player supplied content.
CREATE TABLE IF NOT EXISTS city_visual_pack_review_events (
    id BIGSERIAL PRIMARY KEY,
    pack_id VARCHAR(96) NOT NULL,
    pack_version VARCHAR(24) NOT NULL,
    generation_job_id BIGINT REFERENCES city_visual_generation_jobs(id) ON DELETE RESTRICT,
    event_type VARCHAR(40) NOT NULL,
    actor_user_id BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_visual_pack_review_events_pack_fk
        FOREIGN KEY (pack_id, pack_version)
        REFERENCES city_visual_packs(pack_id, pack_version) ON DELETE RESTRICT,
    CONSTRAINT city_visual_pack_review_events_identity_check CHECK (
        event_type IN (
            'staging_created', 'manifest_updated', 'generation_requested',
            'generation_reviewed', 'published', 'retired',
            'release_policy_assigned'
        )
        AND jsonb_typeof(metadata) = 'object'
        AND length(metadata::TEXT) <= 4096
        AND metadata::TEXT !~* '(https?://|data:|javascript:|<svg|<script)'
        AND metadata::TEXT !~* '"(prompt|source_url|source_uri|storage_key|asset_url)"[[:space:]]*:'
    )
);

CREATE INDEX IF NOT EXISTS idx_city_visual_pack_review_events_pack
    ON city_visual_pack_review_events (pack_id, pack_version, id DESC);

CREATE OR REPLACE FUNCTION guard_city_visual_pack_review_event()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'city visual pack review events are append-only'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_visual_pack_review_events_immutable_guard ON city_visual_pack_review_events;
CREATE TRIGGER city_visual_pack_review_events_immutable_guard
BEFORE INSERT OR UPDATE OR DELETE ON city_visual_pack_review_events
FOR EACH ROW EXECUTE FUNCTION guard_city_visual_pack_review_event();

-- A procedural pack can be released without asset files, but any active or
-- accepted generation job must first be materialised by the future secure
-- atlas ingestion worker. This prevents a review record from being mistaken
-- for a deployed visual asset.
CREATE OR REPLACE FUNCTION guard_city_visual_pack_publication_review_state()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.status = 'staging'
       AND NEW.status = 'published'
       AND EXISTS (
            SELECT 1
            FROM city_visual_generation_jobs job
            WHERE job.pack_id = NEW.pack_id
              AND job.pack_version = NEW.pack_version
              AND job.status NOT IN ('rejected', 'cancelled', 'failed')
       ) THEN
        RAISE EXCEPTION 'city visual pack has generation jobs awaiting secure asset materialisation'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS city_visual_packs_publication_review_guard ON city_visual_packs;
CREATE TRIGGER city_visual_packs_publication_review_guard
BEFORE UPDATE ON city_visual_packs
FOR EACH ROW EXECUTE FUNCTION guard_city_visual_pack_publication_review_state();
