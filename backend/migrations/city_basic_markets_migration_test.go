package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityBasicMarketsMigrationDefinesF4Invariants(t *testing.T) {
	content, err := FS.ReadFile("191_city_basic_markets_and_fiscal_cycle.sql")
	require.NoError(t, err)

	sql := strings.ToLower(string(content))
	for _, fragment := range []string{
		"create table if not exists city_economic_cycle_states",
		"create table if not exists city_economic_policies",
		"create table if not exists city_market_states",
		"create table if not exists city_market_settlements",
		"create table if not exists city_market_allocations",
		"create table if not exists city_housing_occupancies",
		"create table if not exists city_budget_movements",
		"market_settlement_id",
		"post_city_market_state",
		"post_city_household_employment",
		"post_city_firm_employment",
		"post_city_housing_occupancy",
		"post_city_budget_spend",
		"advance_city_economic_cycle",
		"city labor projection is not conserved",
		"city housing occupancy exceeds conserved supply or demand",
		"city market settlement summary does not match posted facts",
		"city market settlements permit only one draft-to-posted transition",
		"city market allocations and budget movements are immutable facts",
		"city market settlement must be posted before commit",
		"initialize_city_f4_foundation",
		"assert_city_f4_foundation",
		"city-f4-v1",
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
