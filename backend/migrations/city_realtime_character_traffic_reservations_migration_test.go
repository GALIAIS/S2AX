package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeCharacterTrafficReservationsMigrationIsBoundedAndPaired(t *testing.T) {
	content, err := FS.ReadFile("281_city_realtime_character_traffic_reservations.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"city_realtime_character_traffic_capacity_policies",
		"city_realtime_character_traffic_reservation_world_bindings",
		"city_realtime_character_traffic_reservation_heads",
		"city_realtime_character_traffic_reservation_events",
		"city-realtime-pedestrian-capacity",
		"stable_due_event_order",
		"system.realtime.character_traffic_reservation",
		"traffic_reservation_granted",
		"traffic_reservation_denied",
		"traffic_reservation_consumed",
		"uq_city_realtime_character_traffic_active_slot",
		"guard_city_realtime_character_traffic_reservation_head",
		"assert_city_realtime_character_navigation_traffic_boundary",
		"assert_city_realtime_character_navigation_traffic_consumption",
		"realtime_character_traffic_reservations",
	} {
		require.Contains(t, sql, required)
	}

	for _, forbidden := range []string{
		"raw_model_response",
		"provider_secret",
		"payment_order",
		"virtual_currency_wallet",
		"reward_amount",
		"reward_currency",
		"route_cache",
		"prompt_text",
		"free_text",
	} {
		require.NotContains(t, sql, forbidden)
	}
}
