package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityDoubleEntryLedgerMigrationEnforcesPostedImmutableFacts(t *testing.T) {
	content, err := FS.ReadFile("189_city_double_entry_ledger.sql")
	require.NoError(t, err)

	sql := strings.ToLower(string(content))
	for _, fragment := range []string{
		"create table if not exists city_journals",
		"create table if not exists city_journal_entries",
		"city_journals_world_operation_unique",
		"city_journals_world_tick_sequence_unique",
		"city_journal_entry_side_check",
		"post_city_journal_entry",
		"city account balances can only change through a draft journal",
		"city account has insufficient balance",
		"assert_city_journal_ready",
		"not balanced by economic entity",
		"city journals must be inserted as drafts",
		"city journals permit only one draft-to-posted transition",
		"city journal entries are immutable facts",
		"city journal entry must match its locked account projection",
		"city_journal_commit_check",
		"deferrable initially deferred",
		"on delete restrict",
	} {
		require.Contains(t, sql, fragment)
	}
	require.NotContains(t, sql, "interest_rate")
	require.NotContains(t, sql, "loan")
	require.NotContains(t, sql, "stock")
}
