package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeCharacterActivityCatalogV110IsAppendOnlyAndClosesLifeLoop(t *testing.T) {
	content, err := FS.ReadFile("261_city_realtime_character_activity_catalog_v110.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city-realtime-character-core', '1.1.0'",
		"minimum_energy_milli",
		"minimum_satiety_milli",
		"work.civic_shift",
		"item.food.ration",
		"item_quantity_delta\": 1",
		"on conflict (catalog_id, catalog_version) do nothing",
	} {
		require.Contains(t, sql, required)
	}

	// Version 1.0.0 is a sealed historical catalog and must never be mutated.
	require.NotContains(t, sql, "update city_realtime_character_activity_catalogs")
	require.NotContains(t, sql, "delete from city_realtime_character_activity_catalogs")
	require.NotContains(t, sql, "virtual_currency_wallet")
}
