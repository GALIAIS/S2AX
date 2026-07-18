package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityHouseholdLifecycleDefinesVersionedImmutableFacts(t *testing.T) {
	content, err := FS.ReadFile("196_city_household_lifecycle.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"add column if not exists household_units",
		"create table if not exists city_household_movements",
		"create table if not exists city_household_movement_lines",
		"city_household_movement_shape_check",
		"city_f63_household_write_enabled",
		"guard_city_household_movement_write",
		"city_household_movement_line_immutable_guard",
		"assert_city_household_movement_committed",
		"assert_city_household_projection",
		"initialize_city_f63_foundation",
		"city household movement does not match its applied command",
		"city-f6-v3",
		"f6_v2_to_f6_v3",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "update city_worlds\nset simulation_version")
}
