package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityWorldBootstrapDeferredChecksMigrationKeepsNormalCommitValidation(t *testing.T) {
	content, err := FS.ReadFile("210_city_world_bootstrap_deferred_checks.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	require.Contains(t, sql, "city_world_bootstrap_checks_suppressed")
	require.Contains(t, sql, "sub2api.city_world_bootstrap")
	for _, functionName := range []string{
		"check_city_spatial_foundation",
		"check_city_land_foundation",
		"check_city_development_foundation",
		"check_city_enterprise_location_foundation",
		"check_world_actor_spatial_control_foundation",
		"check_world_portal_access_foundation",
		"check_world_navigation_intent_foundation",
		"check_city_service_foundation",
		"check_city_facility_lifecycle_foundation",
		"check_city_physical_network_foundation",
	} {
		require.Contains(t, sql, functionName)
	}
	require.Equal(t, 10, strings.Count(sql, "if city_world_bootstrap_checks_suppressed() then"))
}
