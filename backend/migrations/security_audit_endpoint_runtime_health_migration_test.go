package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecurityAuditEndpointRuntimeHealthMigrationDefinesDurableCircuitMetrics(t *testing.T) {
	content, err := FS.ReadFile("293_security_audit_endpoint_runtime_health.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"alter table security_audit_endpoint_health",
		"request_count bigint not null default 0",
		"success_count bigint not null default 0",
		"timeout_count bigint not null default 0",
		"rate_limited_count bigint not null default 0",
		"server_error_count bigint not null default 0",
		"invalid_response_count bigint not null default 0",
		"latency_sum_ms bigint not null default 0",
		"idx_security_audit_endpoint_breaker",
	} {
		require.Contains(t, sql, required)
	}
}
