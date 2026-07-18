package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityEnterpriseLocationsDefinesVersionedConservedPlacementFacts(t *testing.T) {
	content, err := FS.ReadFile("200_city_enterprise_locations.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"create table if not exists city_enterprise_location_profiles",
		"create table if not exists city_enterprise_sites",
		"create table if not exists city_enterprise_location_facts",
		"create table if not exists city_enterprise_location_baselines",
		"city_enterprise_location_fact_write_enabled",
		"guard_city_enterprise_location_fact_insert",
		"guard_city_enterprise_site_projection",
		"guard_city_enterprise_firm_location_projection",
		"assert_city_enterprise_location_foundation",
		"enterprise occupancy exceeds effective building pool supply",
		"required enterprise sites or primary district are inconsistent",
		"city-f7-v4",
		"f7_v3_to_f7_v4",
		"b5ec620c0b3bbe81b564a59fe0c372bce97932b31d7d5af341fe62a2b362f39d",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "update city_building_unit_pools set")
	require.NotContains(t, sql, "delete from city_enterprise_location_facts")
}
