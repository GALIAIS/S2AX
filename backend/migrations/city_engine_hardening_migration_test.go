package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityEngineHardeningMigrationDefinesVersionAndUpgradeInvariants(t *testing.T) {
	content, err := FS.ReadFile("194_city_engine_hardening.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"create table if not exists city_engine_versions",
		"city_worlds_engine_version_fk",
		"create table if not exists city_engine_upgrade_paths",
		"guard_city_engine_upgrade_path_acyclic",
		"city_engine_upgrade_path_acyclic_guard",
		"create table if not exists city_world_upgrade_runs",
		"create table if not exists city_tick_failures",
		"simulation_version varchar(32) not null references city_engine_versions",
		"world_tick bigint not null",
		"city_tick_failure_immutable_guard",
		"city_world_upgrade_run_terminal_check",
		"guard_city_world_upgrade_run_insert",
		"guard_city_world_upgrade_run_evidence",
		"city_world_upgrade_run_evidence_guard",
		"target_hash is distinct from new.after_state_hash",
		"guard_city_world_upgrade_run_write",
		"city_engine_upgrade_write_enabled",
		"guard_city_world_engine_version",
		"city_world_engine_version_guard",
		"and not city_engine_upgrade_write_enabled(new.world_id)",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "update city_worlds\nset simulation_version")
}
