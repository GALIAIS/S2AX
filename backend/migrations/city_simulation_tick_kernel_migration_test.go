package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCitySimulationTickKernelMigrationDefinesDeterministicFacts(t *testing.T) {
	content, err := FS.ReadFile("188_city_simulation_tick_kernel.sql")
	require.NoError(t, err)

	sql := strings.ToLower(string(content))
	for _, fragment := range []string{
		"add column if not exists next_command_sequence",
		"2000-01-01 00:00:00+00",
		"create table if not exists city_commands",
		"create table if not exists city_ticks",
		"create table if not exists city_events",
		"city_commands_world_user_request_unique",
		"city_ticks_world_request_unique",
		"city_events_world_tick_sequence_unique",
		"city_commands_processed_tick_fk",
		"city_events_command_tick_fk",
		"city command identity and intent are immutable",
		"city_tick_fact_guard",
		"city_event_fact_guard",
		"assert_city_tick_fact_summary",
		"deferrable initially deferred",
		"on delete restrict",
	} {
		require.Contains(t, sql, fragment)
	}
	require.NotContains(t, sql, "interest_rate")
	require.NotContains(t, sql, "loan")
	require.NotContains(t, sql, "stock")
}
