-- Realtime worlds are a separate temporal engine.  These tables deliberately
-- do not alter the V24 tick pipeline: a realtime world owns one shared time
-- state, append-only clock segments, and immutable temporal frames.

ALTER TABLE city_engine_versions
    DROP CONSTRAINT IF EXISTS city_engine_version_code_check;

ALTER TABLE city_engine_versions
    ADD CONSTRAINT city_engine_version_code_check
    CHECK (version ~ '^city-(f[0-9]+|openworld|openworld-realtime)-v[0-9]+$');

INSERT INTO city_engine_versions (version, status, canonical_format, capabilities)
VALUES (
    'city-openworld-realtime-v1',
    'supported',
    'city-realtime-state-v1+gzip',
    '["control","ledger","resources","calendar_demography","realtime_clock","temporal_frames","due_events","shared_world","snapshot","replay","recovery_verification"]'::jsonb
)
ON CONFLICT (version) DO NOTHING;

CREATE TABLE IF NOT EXISTS city_clock_profiles (
    id VARCHAR(96) PRIMARY KEY,
    version VARCHAR(24) NOT NULL,
    profile_hash VARCHAR(64) NOT NULL,
    source_clock_mode VARCHAR(32) NOT NULL,
    deployment_scope VARCHAR(16) NOT NULL,
    quantum_us BIGINT NOT NULL,
    maximum_uncertainty_us BIGINT NOT NULL,
    maximum_database_skew_us BIGINT NOT NULL,
    pause_policy VARCHAR(48) NOT NULL,
    calendar_policy VARCHAR(48) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'published',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_clock_profiles_identity_check CHECK (
        id ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
        AND profile_hash ~ '^[0-9a-f]{64}$'
        AND source_clock_mode IN (
            'system_ntp', 'system_nts', 'private_time_service',
            'frozen_test_clock', 'manual_admin_clock'
        )
        AND deployment_scope IN ('production', 'test', 'development')
        AND quantum_us = 1000000
        AND maximum_uncertainty_us BETWEEN 0 AND 60000000
        AND maximum_database_skew_us BETWEEN 0 AND 60000000
        AND pause_policy = 'freeze_elapsed_time_v1'
        AND calendar_policy = 'timezone_elapsed_v1'
        AND status = 'published'
    ),
    CONSTRAINT city_clock_profiles_scope_check CHECK (
        (deployment_scope = 'production' AND source_clock_mode IN ('system_ntp', 'system_nts', 'private_time_service'))
        OR (deployment_scope <> 'production' AND source_clock_mode IN ('frozen_test_clock', 'manual_admin_clock'))
    ),
    CONSTRAINT city_clock_profiles_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

INSERT INTO city_clock_profiles (
    id, version, profile_hash, source_clock_mode, deployment_scope, quantum_us,
    maximum_uncertainty_us, maximum_database_skew_us, pause_policy, calendar_policy, metadata
)
VALUES (
    'realtime-diagnostic-v1', '1.0.0',
    'e88e41a95fad3a148b3ffa6926d0eee9783b1473ab0ab8dd6c18f058fd8bc40a',
    'frozen_test_clock', 'test', 1000000, 5000000, 30000000,
    'freeze_elapsed_time_v1', 'timezone_elapsed_v1',
    '{"schema_version":1,"purpose":"realtime_kernel_diagnostic_only"}'::jsonb
)
ON CONFLICT (id) DO NOTHING;

DROP TRIGGER IF EXISTS city_clock_profile_immutable_guard ON city_clock_profiles;
CREATE TRIGGER city_clock_profile_immutable_guard
BEFORE UPDATE OR DELETE ON city_clock_profiles
FOR EACH ROW EXECUTE FUNCTION reject_city_immutable_fact_mutation();

CREATE TABLE IF NOT EXISTS city_clock_nodes (
    node_id VARCHAR(128) PRIMARY KEY,
    source_clock_mode VARCHAR(32) NOT NULL,
    health_state VARCHAR(16) NOT NULL,
    offset_estimate_us BIGINT,
    uncertainty_us BIGINT,
    last_sync_at TIMESTAMPTZ,
    observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT city_clock_nodes_identity_check CHECK (
        node_id ~ '^[a-z][a-z0-9_.-]{1,127}$'
        AND source_clock_mode IN (
            'system_ntp', 'system_nts', 'private_time_service',
            'frozen_test_clock', 'manual_admin_clock'
        )
        AND health_state IN ('initializing', 'healthy', 'degraded', 'unsafe', 'recovering')
        AND (uncertainty_us IS NULL OR uncertainty_us >= 0)
    ),
    CONSTRAINT city_clock_nodes_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_city_clock_nodes_health_observed
    ON city_clock_nodes (health_state, observed_at DESC);

CREATE TABLE IF NOT EXISTS city_world_time_states (
    world_id BIGINT PRIMARY KEY REFERENCES city_worlds(id) ON DELETE RESTRICT,
    temporal_engine_version VARCHAR(32) NOT NULL REFERENCES city_engine_versions(version) ON DELETE RESTRICT,
    clock_profile_id VARCHAR(96) NOT NULL REFERENCES city_clock_profiles(id) ON DELETE RESTRICT,
    clock_profile_hash VARCHAR(64) NOT NULL,
    lifecycle_status VARCHAR(16) NOT NULL,
    clock_state VARCHAR(16) NOT NULL DEFAULT 'initializing',
    current_world_time_us BIGINT NOT NULL DEFAULT 0,
    last_committed_effective_utc TIMESTAMPTZ NOT NULL,
    current_clock_segment_id BIGINT,
    timeline_frame_sequence BIGINT NOT NULL DEFAULT 0,
    timeline_cursor VARCHAR(32) NOT NULL,
    next_due_at_world_time_us BIGINT,
    catchup_target_world_time_us BIGINT,
    recovery_state VARCHAR(24) NOT NULL DEFAULT 'idle',
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_world_time_states_identity_check CHECK (
        temporal_engine_version = 'city-openworld-realtime-v1'
        AND clock_profile_hash ~ '^[0-9a-f]{64}$'
        AND lifecycle_status IN ('running', 'paused', 'archived', 'recovering')
        AND clock_state IN ('initializing', 'healthy', 'degraded', 'unsafe', 'recovering')
        AND current_world_time_us >= 0
        AND timeline_frame_sequence >= 0
        AND timeline_cursor ~ '^twf_[0-9]{12}$'
        AND (next_due_at_world_time_us IS NULL OR next_due_at_world_time_us >= current_world_time_us)
        AND (catchup_target_world_time_us IS NULL OR catchup_target_world_time_us >= current_world_time_us)
        AND recovery_state IN ('idle', 'catching_up', 'failed', 'held')
        AND version > 0
    )
);

CREATE TABLE IF NOT EXISTS city_world_clock_segments (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_worlds(id) ON DELETE RESTRICT,
    segment_sequence BIGINT NOT NULL,
    clock_profile_id VARCHAR(96) NOT NULL REFERENCES city_clock_profiles(id) ON DELETE RESTRICT,
    clock_profile_hash VARCHAR(64) NOT NULL,
    source_clock_mode VARCHAR(32) NOT NULL,
    effective_utc_anchor TIMESTAMPTZ NOT NULL,
    world_elapsed_anchor_us BIGINT NOT NULL,
    uncertainty_us BIGINT NOT NULL,
    reason VARCHAR(24) NOT NULL,
    monotonic_anchor_proof VARCHAR(64),
    closed_at TIMESTAMPTZ,
    close_reason VARCHAR(24),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_world_clock_segments_identity_check CHECK (
        segment_sequence >= 0
        AND clock_profile_hash ~ '^[0-9a-f]{64}$'
        AND source_clock_mode IN (
            'system_ntp', 'system_nts', 'private_time_service',
            'frozen_test_clock', 'manual_admin_clock'
        )
        AND world_elapsed_anchor_us >= 0
        AND uncertainty_us >= 0
        AND reason IN ('create', 'resume', 'recover', 'source_switch', 'upgrade', 'test')
        AND (monotonic_anchor_proof IS NULL OR monotonic_anchor_proof ~ '^[0-9a-f]{64}$')
        AND ((closed_at IS NULL AND close_reason IS NULL)
             OR (closed_at IS NOT NULL AND close_reason IN ('pause', 'recover', 'source_switch', 'archive')))
    ),
    CONSTRAINT city_world_clock_segments_world_sequence_unique UNIQUE (world_id, segment_sequence),
    CONSTRAINT city_world_clock_segments_world_id_unique UNIQUE (world_id, id)
);

CREATE INDEX IF NOT EXISTS idx_city_world_clock_segments_active
    ON city_world_clock_segments (world_id, closed_at, segment_sequence DESC);

ALTER TABLE city_world_time_states
    DROP CONSTRAINT IF EXISTS city_world_time_states_current_segment_fk;

ALTER TABLE city_world_time_states
    ADD CONSTRAINT city_world_time_states_current_segment_fk
    FOREIGN KEY (world_id, current_clock_segment_id)
    REFERENCES city_world_clock_segments(world_id, id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE IF NOT EXISTS city_temporal_frames (
    world_id BIGINT NOT NULL REFERENCES city_world_time_states(world_id) ON DELETE RESTRICT,
    frame_sequence BIGINT NOT NULL,
    timeline_cursor VARCHAR(32) NOT NULL,
    world_time_from_us BIGINT NOT NULL,
    world_time_to_us BIGINT NOT NULL,
    clock_segment_id BIGINT NOT NULL,
    temporal_engine_version VARCHAR(32) NOT NULL REFERENCES city_engine_versions(version) ON DELETE RESTRICT,
    clock_profile_hash VARCHAR(64) NOT NULL,
    frame_kind VARCHAR(24) NOT NULL,
    effective_utc_from TIMESTAMPTZ NOT NULL,
    effective_utc_to TIMESTAMPTZ NOT NULL,
    previous_state_hash VARCHAR(64),
    state_hash VARCHAR(64) NOT NULL,
    due_event_digest VARCHAR(64) NOT NULL,
    phase_summary JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (world_id, frame_sequence),
    CONSTRAINT city_temporal_frames_identity_check CHECK (
        frame_sequence >= 0
        AND timeline_cursor ~ '^twf_[0-9]{12}$'
        AND world_time_from_us >= 0
        AND world_time_to_us >= world_time_from_us
        AND temporal_engine_version = 'city-openworld-realtime-v1'
        AND clock_profile_hash ~ '^[0-9a-f]{64}$'
        AND frame_kind IN ('genesis', 'command', 'due_event', 'settlement', 'lifecycle', 'recovery', 'diagnostic')
        AND effective_utc_to >= effective_utc_from
        AND state_hash ~ '^[0-9a-f]{64}$'
        AND (previous_state_hash IS NULL OR previous_state_hash ~ '^[0-9a-f]{64}$')
        AND due_event_digest ~ '^[0-9a-f]{64}$'
        AND jsonb_typeof(phase_summary) = 'object'
    ),
    CONSTRAINT city_temporal_frames_segment_fk
        FOREIGN KEY (world_id, clock_segment_id)
        REFERENCES city_world_clock_segments(world_id, id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_city_temporal_frames_world_cursor
    ON city_temporal_frames (world_id, frame_sequence DESC);

DROP TRIGGER IF EXISTS city_temporal_frame_immutable_guard ON city_temporal_frames;
CREATE TRIGGER city_temporal_frame_immutable_guard
BEFORE UPDATE OR DELETE ON city_temporal_frames
FOR EACH ROW EXECUTE FUNCTION reject_city_immutable_fact_mutation();

CREATE TABLE IF NOT EXISTS city_due_events (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_world_time_states(world_id) ON DELETE RESTRICT,
    event_type VARCHAR(96) NOT NULL,
    schema_version SMALLINT NOT NULL,
    due_world_time_us BIGINT NOT NULL,
    temporal_phase VARCHAR(24) NOT NULL,
    priority INTEGER NOT NULL,
    aggregate_type VARCHAR(64) NOT NULL,
    aggregate_key VARCHAR(160) NOT NULL,
    dedup_key VARCHAR(160) NOT NULL,
    source_kind VARCHAR(24) NOT NULL,
    source_reference VARCHAR(160) NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    payload_hash VARCHAR(64) NOT NULL,
    expected_version BIGINT,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    lease_token VARCHAR(64),
    lease_expires_at TIMESTAMPTZ,
    created_frame_sequence BIGINT,
    resolved_frame_sequence BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_due_events_identity_check CHECK (
        event_type ~ '^[a-z][a-z0-9_.-]{1,95}$'
        AND schema_version > 0
        AND due_world_time_us >= 0
        AND temporal_phase IN ('pre_clock', 'pre_command', 'pre_lifecycle', 'movement', 'activity', 'city_settlement', 'rule_effect', 'post_schedule')
        AND aggregate_type ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND aggregate_key ~ '^[a-z][a-z0-9_.:-]{1,159}$'
        AND dedup_key ~ '^[a-z][a-z0-9_.:-]{1,159}$'
        AND source_kind IN ('command', 'fact', 'system', 'agent')
        AND source_reference ~ '^[a-z][a-z0-9_.:-]{1,159}$'
        AND jsonb_typeof(payload) = 'object'
        AND payload_hash ~ '^[0-9a-f]{64}$'
        AND (expected_version IS NULL OR expected_version >= 0)
        AND status IN ('pending', 'leased', 'applied', 'cancelled', 'rejected', 'dead_letter')
        AND ((lease_token IS NULL) = (lease_expires_at IS NULL))
        AND (created_frame_sequence IS NULL OR created_frame_sequence >= 0)
        AND (resolved_frame_sequence IS NULL OR resolved_frame_sequence >= 0)
    ),
    CONSTRAINT city_due_events_world_dedup_unique UNIQUE (world_id, dedup_key),
    CONSTRAINT city_due_events_created_frame_fk
        FOREIGN KEY (world_id, created_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_due_events_resolved_frame_fk
        FOREIGN KEY (world_id, resolved_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX IF NOT EXISTS idx_city_due_events_dispatch
    ON city_due_events (world_id, status, due_world_time_us, temporal_phase, priority, id);

CREATE TABLE IF NOT EXISTS city_temporal_continuations (
    id BIGSERIAL PRIMARY KEY,
    world_id BIGINT NOT NULL REFERENCES city_world_time_states(world_id) ON DELETE RESTRICT,
    continuation_key VARCHAR(160) NOT NULL,
    continuation_type VARCHAR(64) NOT NULL,
    from_world_time_us BIGINT NOT NULL,
    target_world_time_us BIGINT NOT NULL,
    checkpoint JSONB NOT NULL DEFAULT '{}'::jsonb,
    checkpoint_hash VARCHAR(64) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    created_frame_sequence BIGINT,
    resolved_frame_sequence BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_temporal_continuations_identity_check CHECK (
        continuation_key ~ '^[a-z][a-z0-9_.:-]{1,159}$'
        AND continuation_type ~ '^[a-z][a-z0-9_.-]{1,63}$'
        AND from_world_time_us >= 0
        AND target_world_time_us >= from_world_time_us
        AND jsonb_typeof(checkpoint) = 'object'
        AND checkpoint_hash ~ '^[0-9a-f]{64}$'
        AND status IN ('pending', 'running', 'completed', 'failed', 'cancelled')
        AND (created_frame_sequence IS NULL OR created_frame_sequence >= 0)
        AND (resolved_frame_sequence IS NULL OR resolved_frame_sequence >= 0)
    ),
    CONSTRAINT city_temporal_continuations_world_key_unique UNIQUE (world_id, continuation_key),
    CONSTRAINT city_temporal_continuations_created_frame_fk
        FOREIGN KEY (world_id, created_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT city_temporal_continuations_resolved_frame_fk
        FOREIGN KEY (world_id, resolved_frame_sequence)
        REFERENCES city_temporal_frames(world_id, frame_sequence) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX IF NOT EXISTS idx_city_temporal_continuations_pending
    ON city_temporal_continuations (world_id, status, target_world_time_us, id);

CREATE TABLE IF NOT EXISTS city_realtime_schedule_states (
    world_id BIGINT PRIMARY KEY REFERENCES city_world_time_states(world_id) ON DELETE CASCADE,
    lease_token VARCHAR(64),
    lease_expires_at TIMESTAMPTZ,
    node_id VARCHAR(128) REFERENCES city_clock_nodes(node_id) ON DELETE SET NULL,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    retry_not_before TIMESTAMPTZ,
    last_attempt_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    last_error_code VARCHAR(160),
    last_error_detail VARCHAR(1024),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT city_realtime_schedule_states_check CHECK (
        consecutive_failures BETWEEN 0 AND 1000000
        AND ((lease_token IS NULL) = (lease_expires_at IS NULL))
    )
);

CREATE INDEX IF NOT EXISTS idx_city_realtime_schedule_states_lease
    ON city_realtime_schedule_states (lease_expires_at, world_id)
    WHERE lease_expires_at IS NOT NULL;

COMMENT ON TABLE city_world_time_states IS
    'Canonical realtime world clock state; browser time and legacy city ticks are never inputs.';
COMMENT ON TABLE city_temporal_frames IS
    'Append-only realtime frames. Empty wall-clock seconds do not create rows.';
COMMENT ON TABLE city_realtime_schedule_states IS
    'Operational realtime worker lease/retry state; not part of canonical world state.';
