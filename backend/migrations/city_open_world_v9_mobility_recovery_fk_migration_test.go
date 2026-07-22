package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV9MobilityRecoveryMigrationDefersRouteDemandCycle(t *testing.T) {
	content, err := FS.ReadFile("230_city_open_world_v9_mobility_recovery_fk.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	require.Contains(t, sql, "drop constraint if exists city_open_world_mobility_route_demand_fk")
	require.Contains(t, sql, "drop constraint if exists city_open_world_mobility_demand_route_fk")
	require.Contains(t, sql, "on delete no action")
	require.Contains(t, sql, "deferrable initially deferred")
	require.NotContains(t, sql, "on delete cascade")
}
