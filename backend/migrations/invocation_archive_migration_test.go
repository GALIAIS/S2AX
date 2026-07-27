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
