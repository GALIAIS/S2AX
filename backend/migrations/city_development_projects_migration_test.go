package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityDevelopmentProjectsDefinesVersionedImmutableConstructionFacts(t *testing.T) {
	content, err := FS.ReadFile("199_city_development_projects.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"create table if not exists city_development_profiles",
		"create table if not exists city_development_projects",
		"create table if not exists city_development_facts",
		"create table if not exists city_building_adjustments",
		"create table if not exists city_development_baselines",
		"vertical_expansion",
		"renovation",
		"city_development_fact_write_enabled",
		"guard_city_development_fact_insert",
		"guard_city_development_project_projection",
		"guard_city_building_adjustment_immutable",
		"guard_city_development_resource_operation_insert",
		"assert_city_development_foundation",
		"construction labor reservations exceed firm capacity",
		"effective building capacity does not match district aggregates",
		"where scoped_rule.world_id = target_world_id",
		"city-f7-v3",
		"f7_v2_to_f7_v3",
		"b1bbc919b39020a5bc4760fb0ee80468d286a4d74b97d4bbae8f8601c5bb9f3f",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "update city_buildings set")
	require.NotContains(t, sql, "delete from city_development_facts")
}
