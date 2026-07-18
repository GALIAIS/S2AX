package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityPopulationMigrationDefinesImmutableFactsAndVersionBoundary(t *testing.T) {
	content, err := FS.ReadFile("195_city_population_migration.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"create table if not exists city_population_migrations",
		"create table if not exists city_population_migration_lines",
		"city_population_migration_tick_fk",
		"city_population_migration_command_fk",
		"city_population_migration_shape_check",
		"city_population_migration_line_version_check",
		"city_f62_migration_write_enabled",
		"invalid city population migration projection",
		"guard_city_population_migration_write",
		"city_population_migration_line_immutable_guard",
		"assert_city_population_migration_committed",
		"city population migration does not match its applied command",
		"city-f6-v2",
		"f6_v1_to_f6_v2",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "update city_worlds\nset simulation_version")
}
