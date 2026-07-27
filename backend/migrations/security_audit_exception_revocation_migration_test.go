package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecurityAuditExceptionRevocationMigrationIsAttributedAndIdempotent(t *testing.T) {
	content, err := FS.ReadFile("299_security_audit_exception_revocation_audit.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	require.Contains(t, sql, "add column if not exists revoked_by")
	require.Contains(t, sql, "add column if not exists revoked_reason")
	require.Contains(t, sql, "foreign key (revoked_by) references users(id)")
	require.Contains(t, sql, "status <> 'revoked'")
	require.Contains(t, sql, "revoked_reason <> ''")
}
