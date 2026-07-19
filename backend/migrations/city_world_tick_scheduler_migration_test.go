package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityWorldTickSchedulerMigrationDefinesDurableOperationalCoordination(t *testing.T) {
	content, err := FS.ReadFile("203_city_world_tick_scheduler.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"create table if not exists city_world_schedule_states",
		"lease_token",
		"lease_expires_at",
		"consecutive_failures",
		"retry_not_before",
		"last_success_at",
		"idx_city_world_schedule_states_retry",
		"idx_city_world_schedule_states_lease",
		"on delete cascade",
	} {
		require.Contains(t, sql, required)
	}
	require.Contains(t, sql, "never part of canonical simulation state")
}
