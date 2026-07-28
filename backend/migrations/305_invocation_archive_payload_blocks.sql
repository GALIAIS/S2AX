-- Store new archive payloads in independently encrypted blocks.  This keeps
-- administrator review bounded even when an archived request or response is
-- large: direct-view reads only decrypt the block containing the requested
-- range instead of the whole payload.

ALTER TABLE invocation_archive_records
    ADD COLUMN IF NOT EXISTS request_chunked BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS response_chunked BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS invocation_archive_payload_blocks (
    record_id                  BIGINT NOT NULL REFERENCES invocation_archive_records(id) ON DELETE CASCADE,
    slot                       VARCHAR(16) NOT NULL,
    block_index                INT NOT NULL,
    byte_offset                BIGINT NOT NULL,
    captured_bytes             BIGINT NOT NULL,
    ciphertext                 TEXT NOT NULL,
    compression                VARCHAR(16) NOT NULL DEFAULT 'none',
    stored_bytes               BIGINT NOT NULL,
    uncompressed_stored_bytes  BIGINT NOT NULL,
    compression_checked_at     TIMESTAMPTZ NULL,
    compressed_at              TIMESTAMPTZ NULL,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (record_id, slot, block_index),
    UNIQUE (record_id, slot, byte_offset),
    CONSTRAINT chk_invocation_archive_payload_blocks_slot
        CHECK (slot IN ('request', 'response')),
    CONSTRAINT chk_invocation_archive_payload_blocks_index
        CHECK (block_index >= 0),
    CONSTRAINT chk_invocation_archive_payload_blocks_range
        CHECK (byte_offset >= 0 AND captured_bytes > 0),
    CONSTRAINT chk_invocation_archive_payload_blocks_compression
        CHECK (compression IN ('none', 'gzip')),
    CONSTRAINT chk_invocation_archive_payload_blocks_storage
        CHECK (
            stored_bytes >= 0
            AND uncompressed_stored_bytes >= stored_bytes
        )
);

CREATE INDEX IF NOT EXISTS idx_invocation_archive_payload_blocks_lookup
    ON invocation_archive_payload_blocks(record_id, slot, byte_offset);
CREATE INDEX IF NOT EXISTS idx_invocation_archive_payload_blocks_compression_candidates
    ON invocation_archive_payload_blocks(compression_checked_at ASC NULLS FIRST, record_id ASC, slot ASC, block_index ASC)
    WHERE compression = 'none';

COMMENT ON TABLE invocation_archive_payload_blocks IS
    'Independently AES-GCM-encrypted archive payload blocks for bounded direct-view reads.';
COMMENT ON COLUMN invocation_archive_records.request_chunked IS
    'True when request_ciphertext is represented by invocation_archive_payload_blocks.';
COMMENT ON COLUMN invocation_archive_records.response_chunked IS
    'True when response_ciphertext is represented by invocation_archive_payload_blocks.';
COMMENT ON COLUMN invocation_archive_payload_blocks.ciphertext IS
    'AES-GCM ciphertext for one bounded payload block; compression happens before encryption.';
