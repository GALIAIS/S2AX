package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV14CommuteLifecycleMigrationPinsSuccessorEpochEvidence(t *testing.T) {
	content, err := FS.ReadFile("235_city_open_world_v14_commute_lifecycle.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"city-openworld-v14",
		"openworld_v13_to_v14",
		"create table if not exists city_open_world_commute_lifecycle_profiles",
		"create table if not exists city_open_world_commute_assignment_epochs",
		"create table if not exists city_open_world_commute_assignment_transitions",
		"create table if not exists city_open_world_commute_lifecycle_sources",
		"create table if not exists city_open_world_commute_lifecycle_cycle_metrics",
		"immutable_assignment_epoch_lifecycle_v1",
		"active_epoch_verified_facility_presence_od_v1",
		"city_open_world_commute_lifecycle_bootstrap_write_enabled",
		"guard_city_open_world_commute_assignment_epoch",
		"assert_city_open_world_commute_lifecycle_foundation",
		"sub2api-open-world-commute-lifecycle-catalog",
		"city_recovery_write_enabled",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "update city_open_world_commute_bindings set")
	require.NotContains(t, sql, "delete from city_open_world_commute_sources")
}
