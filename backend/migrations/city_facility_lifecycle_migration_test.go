package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityFacilityLifecycleMigrationInstallsAuditableConservedProjection(t *testing.T) {
	content, err := FS.ReadFile("208_city_facility_lifecycle.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"create table if not exists city_facility_lifecycle_profiles",
		"create table if not exists city_facility_lifecycle_policies",
		"create table if not exists city_facility_lifecycle_facts",
		"create table if not exists city_facility_lifecycle_states",
		"create table if not exists city_facility_operations",
		"create table if not exists city_facility_staff_assignments",
		"create table if not exists city_facility_incidents",
		"create table if not exists city_facility_budget_movements",
		"city_facility_lifecycle_facts_tick_sequence_unique",
		"idx_city_facility_operations_one_open",
		"idx_city_facility_staff_actor_active",
		"idx_city_facility_incidents_one_open",
		"city_facility_budget_movement_projection_check",
		"city_facility_lifecycle_state_factor_check",
		"city_facility_lifecycle_bootstrap_write_enabled",
		"city_facility_lifecycle_fact_write_enabled",
		"guard_city_facility_lifecycle_fact_insert",
		"guard_city_facility_lifecycle_fact_immutable",
		"guard_city_facility_lifecycle_projection",
		"guard_city_facility_resource_operation_insert",
		"guard_city_facility_budget_movement",
		"post_city_facility_budget_movement",
		"assert_city_facility_lifecycle_foundation",
		"assert_city_resource_operation_ready(bigint)",
		"idx_city_resource_operations_one_per_facility_command_resource",
		"facility_lifecycle_fact_id",
		"facility.operation.start",
		"deferrable initially deferred",
		"facility.initialized",
		"capacity.changed",
		"operation.scheduled",
		"operation.progressed",
		"incident.opened",
		"city-f8-v2",
		"f8_v1_to_f8_v2",
		"facility_lifecycle",
		"facility_staffing",
		"facility_incidents",
		"facility_budget",
	} {
		require.Contains(t, sql, required)
	}
	require.Contains(t, sql, "world.simulation_version in ('city-f8-v1', 'city-f8-v2')")
	require.Contains(t, sql, "world_version not in ('city-f8-v1', 'city-f8-v2')")
	require.Contains(t, sql, "city_recovery_write_enabled")
	require.NotContains(t, sql, "language plpython")
}
