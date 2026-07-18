package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityLandBuildingFoundationDefinesImmutableConservedFacts(t *testing.T) {
	content, err := FS.ReadFile("198_city_land_building_foundation.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"create table if not exists city_land_profiles",
		"create table if not exists city_zoning_rules",
		"create table if not exists city_parcels",
		"create table if not exists city_buildings",
		"create table if not exists city_building_unit_pools",
		"create table if not exists city_housing_allocations",
		"create table if not exists city_building_portals",
		"create table if not exists city_land_baselines",
		"city_land_foundation_write_enabled",
		"guard_city_land_foundation_projection",
		"assert_city_land_foundation",
		"parcel area does not conserve district developable area",
		"building capacity does not match district aggregates",
		"housing allocations do not conserve household units",
		"where scoped_rule.world_id = target_world_id",
		"world_version in ('city-f7-v1', 'city-f7-v2')",
		"city-f7-v2",
		"f7_v1_to_f7_v2",
		"4275912ce56d967b3596c5449ef28097623b0d1a9b80ea5f60e1a882f79e60c2",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "update city_worlds\nset simulation_version")
}
