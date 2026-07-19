package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldRuleSetBindingConstraintRepairUsesSemanticVersionPattern(t *testing.T) {
	content, err := FS.ReadFile("217_city_open_world_ruleset_binding_constraint_repair.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	require.Contains(t, sql, "drop constraint if exists city_open_world_binding_rule_set_identity_check")
	require.Contains(t, sql, "rule_set_version ~ '^[0-9]+\\.[0-9]+\\.[0-9]+$'")
	require.NotContains(t, sql, "rule_set_version ~ '^[0-9]+\\\\.[0-9]+\\\\.[0-9]+$'")
}
