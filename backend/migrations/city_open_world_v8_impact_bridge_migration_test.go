package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV8ImpactBridgeMigrationPinsDelayedCrossDomainEffects(t *testing.T) {
	content, err := FS.ReadFile("228_city_open_world_v8_impact_bridge.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"city-openworld-v8",
		"openworld_v7_to_v8",
		"create table if not exists city_open_world_impact_profiles",
		"create table if not exists city_open_world_impact_catalog",
		"create table if not exists city_open_world_impact_effects",
		"create table if not exists city_open_world_impact_metrics",
		"effective_tick = scheduled_tick + 1",
		"city_open_world_impact_write_enabled",
		"guard_city_open_world_impact_effect",
		"guard_city_open_world_impact_metric",
		"assert_city_open_world_impact_foundation",
		"city_recovery_write_enabled",
		"next_tick_only",
		"city-openworld-v7', 'city-openworld-v8",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "create table if not exists city_impact_effects")
	require.NotContains(t, sql, "create table if not exists world_impact_metrics")
}
