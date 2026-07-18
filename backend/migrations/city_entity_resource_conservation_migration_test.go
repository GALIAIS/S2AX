package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityEntityResourceConservationMigrationDefinesF3Invariants(t *testing.T) {
	content, err := FS.ReadFile("190_city_entity_resource_conservation.sql")
	require.NoError(t, err)

	sql := strings.ToLower(string(content))
	for _, fragment := range []string{
		"create table if not exists city_districts",
		"create table if not exists city_household_cohorts",
		"create table if not exists city_firm_states",
		"create table if not exists city_government_states",
		"create table if not exists city_government_budget_lines",
		"create table if not exists city_resources",
		"create table if not exists city_inventory_balances",
		"create table if not exists city_production_recipes",
		"create table if not exists city_resource_operations",
		"create table if not exists city_resource_entries",
		"city inventory balances can only change through a draft resource operation",
		"post_city_resource_entry",
		"city resource transfer is not conserved",
		"idx_city_resource_operations_one_per_command",
		"idx_city_resource_operations_one_opening_per_scope",
		"must exactly post configured opening inventory",
		"does not match its source command",
		"city production entries do not match the fixed recipe",
		"not granted to the firm and district",
		"city production capacity exceeded",
		"city resource operations permit only one draft-to-posted transition",
		"city resource entries are immutable facts",
		"city resource operation must be posted before commit",
		"initialize_city_f3_foundation",
		"assert_city_f3_foundation",
		"city-f3-v1",
		"deferrable initially deferred",
		"on delete restrict",
	} {
		require.Contains(t, sql, fragment)
	}
	require.NotContains(t, sql, "interest_rate")
	require.NotContains(t, sql, "loan")
	require.NotContains(t, sql, "stock_order")
	require.NotContains(t, sql, "shareholding")
}
