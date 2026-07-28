-- Archive payloads are encrypted at rest. These fields let the maintenance
-- worker compress old, already-encrypted records before re-encrypting them,
-- while keeping an exact accounting of the physical payload footprint.

ALTER TABLE invocation_archive_records
    ADD COLUMN IF NOT EXISTS request_compression VARCHAR(16) NOT NULL DEFAULT 'none',
    ADD COLUMN IF NOT EXISTS response_compression VARCHAR(16) NOT NULL DEFAULT 'none',
    ADD COLUMN IF NOT EXISTS request_stored_bytes BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS response_stored_bytes BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS request_uncompressed_stored_bytes BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS response_uncompressed_stored_bytes BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS compression_checked_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS compressed_at TIMESTAMPTZ NULL;

UPDATE invocation_archive_records
SET request_stored_bytes = octet_length(request_ciphertext),
    response_stored_bytes = octet_length(response_ciphertext),
    request_uncompressed_stored_bytes = octet_length(request_ciphertext),
    response_uncompressed_stored_bytes = octet_length(response_ciphertext)
WHERE request_stored_bytes = 0
   OR response_stored_bytes = 0
   OR request_uncompressed_stored_bytes = 0
   OR response_uncompressed_stored_bytes = 0;

ALTER TABLE invocation_archive_records
    DROP CONSTRAINT IF EXISTS chk_invocation_archive_request_compression;
ALTER TABLE invocation_archive_records
    ADD CONSTRAINT chk_invocation_archive_request_compression
        CHECK (request_compression IN ('none', 'gzip'));

ALTER TABLE invocation_archive_records
    DROP CONSTRAINT IF EXISTS chk_invocation_archive_response_compression;
ALTER TABLE invocation_archive_records
    ADD CONSTRAINT chk_invocation_archive_response_compression
        CHECK (response_compression IN ('none', 'gzip'));

ALTER TABLE invocation_archive_records
    DROP CONSTRAINT IF EXISTS chk_invocation_archive_payload_storage_bytes;
ALTER TABLE invocation_archive_records
    ADD CONSTRAINT chk_invocation_archive_payload_storage_bytes
        CHECK (
            request_stored_bytes >= 0
            AND response_stored_bytes >= 0
            AND request_uncompressed_stored_bytes >= request_stored_bytes
            AND response_uncompressed_stored_bytes >= response_stored_bytes
        );

CREATE INDEX IF NOT EXISTS idx_invocation_archive_records_compression_candidates
    ON invocation_archive_records(compression_checked_at ASC NULLS FIRST, created_at ASC, id ASC)
    WHERE request_compression = 'none' OR response_compression = 'none';

COMMENT ON COLUMN invocation_archive_records.request_compression IS
    'Payload-envelope compression applied before AES-GCM encryption; none or gzip.';
COMMENT ON COLUMN invocation_archive_records.response_compression IS
    'Payload-envelope compression applied before AES-GCM encryption; none or gzip.';
COMMENT ON COLUMN invocation_archive_records.compression_checked_at IS
    'Last background compaction eligibility check; prevents hot-looping incompressible records.';
