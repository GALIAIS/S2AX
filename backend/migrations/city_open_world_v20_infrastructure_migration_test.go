package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV20InfrastructureMigrationPinsMutableAssetLifecycle(t *testing.T) {
	content, err := FS.ReadFile("242_city_open_world_v20_infrastructure.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city-openworld-v20",
		"openworld_v19_to_v20",
		"mutable_infrastructure_assets",
		"infrastructure_lifecycle_facts",
		"create table if not exists city_open_world_infrastructure_profiles",
		"create table if not exists city_open_world_infrastructure_assets",
		"create table if not exists city_open_world_infrastructure_asset_states",
		"create table if not exists city_open_world_infrastructure_asset_transitions",
		"v19_node_corridor_asset_seed_v1",
		"append_only_asset_transition_state_v1",
		"city_open_world_infrastructure_bootstrap_write_enabled",
		"city_open_world_infrastructure_write_enabled",
		"guard_city_open_world_infrastructure_transition",
		"assert_city_open_world_infrastructure_foundation",
		"sub2api-open-world-infrastructure-catalog",
		"city-openworld-v19','city-openworld-v20",
	} {
		require.Contains(t, sql, required)
	}

	require.Contains(t, sql, "v9_scheduler_effect")
	require.Contains(t, sql, "not_consumed_by_v9")
	require.Contains(t, sql, "city_recovery_write_enabled")
	require.NotContains(t, sql, "unsafe-eval")
}
