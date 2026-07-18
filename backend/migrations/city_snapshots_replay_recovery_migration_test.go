package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCitySnapshotsReplayRecoveryMigrationDefinesF5Invariants(t *testing.T) {
	content, err := FS.ReadFile("192_city_snapshots_replay_recovery.sql")
	require.NoError(t, err)

	sql := strings.ToLower(string(content))
	for _, fragment := range []string{
		"create table if not exists city_snapshots",
		"create table if not exists city_replay_runs",
		"create table if not exists city_recovery_runs",
		"city-state-v1+gzip",
		"city_snapshots_world_tick_unique",
		"city snapshot source tick does not match world and tick",
		"city tick snapshot does not match its immutable tick fact",
		"city replay runs are immutable audit records",
		"city recovery runs are immutable audit records",
		"city_recovery_write_enabled",
		"sub2api.city_recovery_run_id",
		"city-f5-v1",
		"on delete restrict",
	} {
		require.Contains(t, sql, fragment)
	}
	require.NotContains(t, sql, "interest_rate")
	require.NotContains(t, sql, "loan_contract")
	require.NotContains(t, sql, "stock_order")
	require.NotContains(t, sql, "shareholding")
}
