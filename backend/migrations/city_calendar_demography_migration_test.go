package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityCalendarDemographyMigrationDefinesF61Invariants(t *testing.T) {
	content, err := FS.ReadFile("193_city_calendar_demography_foundation.sql")
	require.NoError(t, err)

	sql := strings.ToLower(string(content))
	for _, fragment := range []string{
		"city_snapshots_world_tick_version_unique",
		"create table if not exists city_calendar_states",
		"create table if not exists city_calendar_boundaries",
		"create table if not exists city_demographic_policies",
		"create table if not exists city_demographic_cohort_states",
		"create table if not exists city_population_movements",
		"create table if not exists city_population_movement_lines",
		"quarter_index bigint not null",
		"last_quarterly_tick bigint",
		"'quarter'",
		"city_f6_boundary_write_enabled",
		"city_f6_movement_write_enabled",
		"city_calendar_boundary_sequence_type_check",
		"city_calendar_boundary_period_date_check",
		"assert_city_calendar_projection",
		"city calendar boundaries are immutable facts",
		"city population movement lines are immutable facts",
		"city population movement must be posted before commit",
		"city population movement summary does not match immutable lines",
		"city population movement does not match its monthly boundary and cohort set",
		"city demographic projection does not match household cohorts",
		"initialize_city_f6_foundation",
		"city-f6-v1",
		"deferrable initially deferred",
		"on delete restrict",
	} {
		require.Contains(t, sql, fragment)
	}
	require.NotContains(t, sql, "interest_rate")
	require.NotContains(t, sql, "loan_contract")
	require.NotContains(t, sql, "stock_order")
	require.NotContains(t, sql, "shareholding")
}
