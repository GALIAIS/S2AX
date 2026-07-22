package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeCharacterPortalTraversalMigrationKeepsTopologyAndReceiptsSealed(t *testing.T) {
	content, err := FS.ReadFile("263_city_realtime_character_portal_traversal.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"realtime_character_portals",
		"realtime_character_interiors",
		"add column if not exists portal_code",
		"event_kind in ('spawn', 'move', 'despawn', 'teleport', 'portal')",
		"event_kind = 'portal'",
		"character.portal",
		"idx_city_realtime_spatial_interiors_cells",
		"using gin (cells jsonb_path_ops)",
	} {
		require.Contains(t, sql, required)
	}

	// Traversal operates on world-local static topology only. It must never
	// mint a platform balance or rewrite the immutable world generator.
	require.NotContains(t, sql, "virtual_currency_wallet")
	require.NotContains(t, sql, "update city_realtime_spatial_portals")
	require.NotContains(t, sql, "delete from city_realtime_spatial_portals")
}
