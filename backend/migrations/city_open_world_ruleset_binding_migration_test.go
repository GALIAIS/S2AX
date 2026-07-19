package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldRuleSetBindingMigrationPinsExistingV2Worlds(t *testing.T) {
	content, err := FS.ReadFile("212_city_open_world_ruleset_binding.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"add column if not exists rule_set_id",
		"add column if not exists rule_set_version",
		"add column if not exists rule_set_hash",
		"disable trigger city_open_world_binding_guard",
		"sub2api-classic",
		"136ce6b71a6ebd0f9db4fdfe2662dc7530485330e565e0a7feebcec4399b5277",
	} {
		require.Contains(t, sql, required)
	}
}
