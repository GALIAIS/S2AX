package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecurityAuditExceptionSemanticsMigrationRemovesUnsupportedSkipDetector(t *testing.T) {
	content, err := FS.ReadFile("294_security_audit_exception_semantics.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	require.Contains(t, sql, "where effect='skip_detector'")
	require.Contains(t, sql, "set effect='warn_only'")
	require.Contains(t, sql, "check (effect in ('allow_and_record', 'warn_only'))")
}
