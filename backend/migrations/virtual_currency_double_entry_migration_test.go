package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVirtualCurrencyDoubleEntryMigrationDefinesConservationAndImmutability(t *testing.T) {
	content, err := FS.ReadFile("186_virtual_currency_double_entry.sql")
	require.NoError(t, err)

	sql := strings.ToLower(string(content))
	for _, fragment := range []string{
		"create table if not exists virtual_currency_journals",
		"create table if not exists virtual_currency_postings",
		"alter column journal_id set not null",
		"deferrable initially deferred",
		"assert_virtual_currency_journal_balanced",
		"guard_virtual_currency_journal_mutation",
		"guard_virtual_currency_child_mutation",
		"posted_at timestamptz",
		"on delete restrict",
	} {
		require.Contains(t, sql, fragment)
	}
	require.Contains(t, sql, "having count(p.id) < 2 or coalesce(sum(p.amount_units), 0) <> 0")
	require.Contains(t, sql, "virtual currency journals are immutable")
	require.Contains(t, sql, "virtual currency % rows are immutable")
}
