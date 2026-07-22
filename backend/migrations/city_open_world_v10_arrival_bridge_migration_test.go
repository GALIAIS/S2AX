package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV10ArrivalBridgeMigrationPinsCrossScaleContract(t *testing.T) {
	content, err := FS.ReadFile("231_city_open_world_v10_arrival_bridge.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"city-openworld-v10",
		"openworld_v9_to_v10",
		"create table if not exists city_open_world_mobility_arrival_profiles",
		"create table if not exists city_open_world_mobility_arrivals",
		"completed_route_next_tick_bridge_v1",
		"validated_surface_anchor_landing_v1",
		"city_open_world_arrival_bootstrap_write_enabled",
		"city_open_world_arrival_write_enabled",
		"guard_city_open_world_mobility_arrival",
		"assert_city_open_world_arrival_foundation",
		"sub2api-open-world-mobility-arrival-catalog",
		"city_recovery_write_enabled",
		"completion_fact.tick < new.created_tick",
		"fact.parent_fact_id = old.last_fact_id",
		"new.next_attempt_tick = new.updated_tick + 1",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "update city_open_world_mobility_routes set")
	require.NotContains(t, sql, "delete from city_open_world_actor_locations")
}
