package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityPublicServiceFoundationMigrationInstallsVersionedFactBackedSettlement(t *testing.T) {
	content, err := FS.ReadFile("207_city_public_service_foundation.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"create table if not exists city_service_profiles",
		"create table if not exists city_service_definitions",
		"create table if not exists city_facility_type_definitions",
		"create table if not exists city_service_facts",
		"create table if not exists city_facilities",
		"create table if not exists city_facility_service_capacities",
		"create table if not exists city_service_demands",
		"create table if not exists city_service_connections",
		"create table if not exists city_service_allocations",
		"create table if not exists city_service_settlements",
		"city_service_facts_world_tick_sequence_unique",
		"city_service_settlements_demand_tick_unique",
		"city_service_profile_revision_check",
		"city_service_fact_write_enabled",
		"guard_city_service_fact_insert",
		"guard_city_service_fact_immutable",
		"guard_city_service_versioned_projection",
		"guard_city_service_settlement_projection",
		"assert_city_service_foundation",
		"deferrable initially deferred",
		"floor(",
		"city-f8-v1",
		"f7_v9_to_f8_v1",
		"public_services",
	} {
		require.Contains(t, sql, required)
	}
	require.Contains(t, sql, "city-f7-v9', 'city-f8-v1")
	require.Contains(t, sql, "to_jsonb(new)->'facility_id'")
	require.Contains(t, sql, "select 1 from city_household_cohorts cohort")
	require.Contains(t, sql, "select 1 from city_firm_states firm")
	require.Contains(t, sql, "select 1 from world_actor_locations location")
	require.Contains(t, sql, "building.status <> 'active'")
	require.NotContains(t, sql, "(new.facility_id, new.service_definition_id)")
	require.NotContains(t, sql, "language plpython")
}
