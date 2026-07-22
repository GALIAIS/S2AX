-- The only built-in production profiles for the host-clock authority. Their
-- IDs are selected in-process from the attested source mode; no browser or
-- administrator request can inject a profile ID, source URL, or clock bound.
--
-- profile_hash is the SHA-256 of the stable profile tuple documented beside
-- each row. The immutable trigger installed by migration 252 prevents later
-- edits from silently changing a world's pinned time contract.

INSERT INTO city_clock_profiles (
    id, version, profile_hash, source_clock_mode, deployment_scope, quantum_us,
    maximum_uncertainty_us, maximum_database_skew_us, pause_policy, calendar_policy,
    metadata
)
VALUES
    (
        'realtime-system-ntp-v1', '1.0.0',
        'c7859ce9bef9e70ecb7c948f7004cae9ea0831f685c56ee4145f8858bf08cd18',
        'system_ntp', 'production', 1000000, 5000000, 30000000,
        'freeze_elapsed_time_v1', 'timezone_elapsed_v1',
        '{"schema_version":1,"purpose":"shared_realtime_production","authority":"host_clock_v1"}'::jsonb
    ),
    (
        'realtime-system-nts-v1', '1.0.0',
        '202daa7921ad70c4fcb6f987974789986b87c9639a5010fd7afddff2ff7db7fe',
        'system_nts', 'production', 1000000, 5000000, 30000000,
        'freeze_elapsed_time_v1', 'timezone_elapsed_v1',
        '{"schema_version":1,"purpose":"shared_realtime_production","authority":"host_clock_v1"}'::jsonb
    )
ON CONFLICT (id) DO NOTHING;
