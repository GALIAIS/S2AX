package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityOpenWorldV14CommuteLifecycleHardeningKeepsEpochChainsAuditable(t *testing.T) {
	content, err := FS.ReadFile("236_city_open_world_v14_commute_lifecycle_hardening.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"assert_city_open_world_commute_lifecycle_successor_integrity",
		"epoch.epoch_number <> 1",
		"max(epoch.epoch_number) <> count(*)",
		"count(distinct epoch.actor_id) <> 1",
		"opening transition is invalid",
		"more than one effective epoch",
		"source cadence or fact chain is invalid",
		"lifecycle metric windows are not contiguous",
		"check_city_open_world_commute_lifecycle_foundation_commit",
	} {
		require.Contains(t, sql, required)
	}
}
