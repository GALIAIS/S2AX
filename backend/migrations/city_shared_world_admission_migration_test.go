package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCitySharedWorldAdmissionMigrationDropsPrivateOwnerConstraint(t *testing.T) {
	content, err := FS.ReadFile("251_city_shared_world_admission.sql")
	require.NoError(t, err)
	require.Contains(t, strings.ToLower(string(content)), "drop index if exists idx_city_worlds_one_private_active_per_owner")
}
