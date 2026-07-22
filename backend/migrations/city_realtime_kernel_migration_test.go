package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeKernelMigrationDefinesSeparateTemporalContract(t *testing.T) {
	content, err := FS.ReadFile("252_city_realtime_kernel.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city-openworld-realtime-v1",
		"create table if not exists city_clock_profiles",
		"create table if not exists city_clock_nodes",
		"create table if not exists city_world_time_states",
		"create table if not exists city_world_clock_segments",
		"create table if not exists city_temporal_frames",
		"create table if not exists city_due_events",
		"create table if not exists city_temporal_continuations",
		"create table if not exists city_realtime_schedule_states",
		"city_due_events_created_frame_fk",
		"city_due_events_resolved_frame_fk",
		"deferrable initially deferred",
		"quantum_us = 1000000",
		"city_temporal_frame_immutable_guard",
		"empty wall-clock seconds do not create rows",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "city-openworld-v24', 'supported', 'city-realtime")
}

func TestCityRealtimeProductionClockProfilesArePinnedToHostModes(t *testing.T) {
	content, err := FS.ReadFile("254_city_realtime_production_clock_profiles.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"realtime-system-ntp-v1",
		"realtime-system-nts-v1",
		"system_ntp",
		"system_nts",
		"production",
		"freeze_elapsed_time_v1",
		"timezone_elapsed_v1",
		"on conflict (id) do nothing",
	} {
		require.Contains(t, sql, required)
	}
}
