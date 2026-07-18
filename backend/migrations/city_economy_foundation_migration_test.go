package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityEconomyFoundationMigrationDefinesIsolationAndIntegrity(t *testing.T) {
	content, err := FS.ReadFile("187_city_economy_foundation.sql")
	require.NoError(t, err)

	sql := strings.ToLower(string(content))
	for _, fragment := range []string{
		"create table if not exists city_worlds",
		"create table if not exists city_members",
		"create table if not exists city_monetary_units",
		"create table if not exists city_economic_entities",
		"create table if not exists city_account_templates",
		"create table if not exists city_accounts",
		"idx_city_worlds_one_private_active_per_owner",
		"idx_city_monetary_units_one_base_per_world",
		"city_accounts_entity_fk",
		"city_accounts_monetary_unit_fk",
		"city_accounts_template_fk",
		"allow_negative or current_balance_units >= 0",
		"assert_city_world_foundation",
		"deferrable initially deferred",
		"on delete restrict",
	} {
		require.Contains(t, sql, fragment)
	}
	require.NotContains(t, sql, "references virtual_currencies")
	require.NotContains(t, sql, "references virtual_currency_wallets")
}
