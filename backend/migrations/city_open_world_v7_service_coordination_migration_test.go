package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV7ServiceCoordinationMigrationPinsFactBackedQueues(t *testing.T) {
	content, err := FS.ReadFile("227_city_open_world_v7_service_coordination.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"city-openworld-v7",
		"openworld_v6_to_v7",
		"create table if not exists city_open_world_service_profiles",
		"create table if not exists city_open_world_service_catalog",
		"create table if not exists city_open_world_service_providers",
		"create table if not exists city_open_world_service_requests",
		"create table if not exists city_open_world_service_responses",
		"city_open_world_service_fact_write_enabled",
		"guard_city_open_world_service_request",
		"guard_city_open_world_service_response",
		"assert_city_open_world_service_foundation",
		"city-openworld-v6', 'city-openworld-v7",
		"city_recovery_write_enabled",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "create table if not exists city_service_profiles")
	require.NotContains(t, sql, "create table if not exists world_service_requests")
}
