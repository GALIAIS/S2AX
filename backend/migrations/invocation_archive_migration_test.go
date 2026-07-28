package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInvocationArchiveMigrationProtectsPayloadsAndRetention(t *testing.T) {
	raw, err := FS.ReadFile("303_invocation_archive.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(raw))
	for _, fragment := range []string{
		"invocation_archive_records",
		"request_ciphertext",
		"response_ciphertext",
		"expires_at",
		"invocation_archive_access_logs",
		"on delete set null",
		"append-only",
		"mode in ('request_only', 'full')",
	} {
		require.Contains(t, sql, fragment)
	}
}

func TestInvocationArchiveCompressionMigrationTracksEncryptedStorage(t *testing.T) {
	raw, err := FS.ReadFile("304_invocation_archive_payload_compression.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(raw))
	for _, fragment := range []string{
		"request_compression",
		"response_compression",
		"request_stored_bytes",
		"response_stored_bytes",
		"compression_checked_at",
		"compressed_at",
		"idx_invocation_archive_records_compression_candidates",
		"gzip",
	} {
		require.Contains(t, sql, fragment)
	}
}

func TestInvocationArchivePayloadBlockMigrationBoundsDirectViewReads(t *testing.T) {
	raw, err := FS.ReadFile("305_invocation_archive_payload_blocks.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(raw))
	for _, fragment := range []string{
		"invocation_archive_payload_blocks",
		"on delete cascade",
		"request_chunked",
		"response_chunked",
		"byte_offset",
		"idx_invocation_archive_payload_blocks_lookup",
		"idx_invocation_archive_payload_blocks_compression_candidates",
	} {
		require.Contains(t, sql, fragment)
	}
}
