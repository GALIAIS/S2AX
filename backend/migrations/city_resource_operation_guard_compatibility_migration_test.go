package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityResourceOperationGuardCompatibilityPreservesEnterpriseAndV22Dispatch(t *testing.T) {
	content, err := FS.ReadFile("271_city_resource_operation_guard_compatibility.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"create or replace function guard_city_resource_operation_write()",
		"new.market_settlement_id is not distinct from old.market_settlement_id",
		"new.metadata ? 'enterprise_location_fact_id'",
		"assert_city_enterprise_relocation_resource_operation_ready(old.id)",
		"assert_city_resource_operation_ready_v22(old.id)",
		"city resource operations permit only one draft-to-posted transition",
	} {
		require.Contains(t, sql, required)
	}

	require.NotContains(t, sql, "unsafe-eval")
}
