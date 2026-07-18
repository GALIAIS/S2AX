package service

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeCityLedgerCommandCanonicalizesAndRejectsUnsafeAmounts(t *testing.T) {
	expectedTick := int64(3)
	first, err := normalizeCityCommand(
		" LEDGER.WAGE ",
		json.RawMessage(`{"firm_entity_id":2,"household_entity_id":1,"amount_units":125,"memo":"  payroll  "}`),
		&expectedTick,
	)
	require.NoError(t, err)
	second, err := normalizeCityCommand(
		CityCommandTypeLedgerWage,
		json.RawMessage(`{"memo":"payroll","amount_units":125,"household_entity_id":1,"firm_entity_id":2}`),
		&expectedTick,
	)
	require.NoError(t, err)
	require.JSONEq(t, `{"firm_entity_id":2,"household_entity_id":1,"amount_units":125,"memo":"payroll"}`, string(first.payload))
	require.Equal(t, first.fingerprint, second.fingerprint)

	for _, payload := range []string{
		`{"firm_entity_id":2,"household_entity_id":1,"amount_units":0}`,
		`{"firm_entity_id":2,"household_entity_id":1,"amount_units":1.5}`,
		`{"firm_entity_id":2,"household_entity_id":1,"amount_units":9223372036854775807}`,
		`{"firm_entity_id":2,"household_entity_id":1,"amount_units":1,"unexpected":true}`,
		`{"firm_entity_id":2,"firm_entity_id":3,"household_entity_id":1,"amount_units":1}`,
	} {
		_, err = normalizeCityCommand(CityCommandTypeLedgerWage, json.RawMessage(payload), &expectedTick)
		require.ErrorIs(t, err, ErrCityInvalidInput, payload)
	}

	_, err = normalizeCityCommand(
		CityCommandTypeLedgerReverse,
		json.RawMessage(`{"journal_tick":1,"journal_sequence":2,"reason":"   "}`),
		&expectedTick,
	)
	require.ErrorIs(t, err, ErrCityInvalidInput)
}

func TestCityLedgerPostingValidationRequiresGlobalAndEntityBalance(t *testing.T) {
	household := &cityLedgerAccountRef{id: 1, entityID: 10, normalSide: "debit", balanceUnits: 100}
	firm := &cityLedgerAccountRef{id: 2, entityID: 20, normalSide: "debit", balanceUnits: 100}
	require.Error(t, validateCityLedgerPostingLines([]cityLedgerPostingLine{
		{account: household, debitUnits: 10},
		{account: firm, creditUnits: 10},
	}))

	householdCredit := &cityLedgerAccountRef{id: 3, entityID: 10, normalSide: "credit"}
	firmCredit := &cityLedgerAccountRef{id: 4, entityID: 20, normalSide: "credit"}
	require.NoError(t, validateCityLedgerPostingLines([]cityLedgerPostingLine{
		{account: household, debitUnits: 10},
		{account: householdCredit, creditUnits: 10},
		{account: firm, debitUnits: 10},
		{account: firmCredit, creditUnits: 10},
	}))
}

func TestCityOpeningAmountUsesIntegerMinorUnits(t *testing.T) {
	amount, err := cityOpeningAmountUnits(10_000, 3)
	require.NoError(t, err)
	require.Equal(t, int64(10_000_000), amount)

	_, err = cityOpeningAmountUnits(math.MaxInt64, 1)
	require.ErrorIs(t, err, ErrCitySimulationInvariant)
}

func TestSyncCityLedgerAccountRefsCarriesBalancesAcrossJournals(t *testing.T) {
	account := &cityLedgerAccountRef{id: 7, balanceUnits: 100, version: 3}
	err := syncCityLedgerAccountRefs(
		[]cityLedgerPostingLine{{account: account, creditUnits: 40}},
		[]*CityJournalEntry{{
			AccountID: 7, BalanceBeforeUnits: 100, BalanceAfterUnits: 60,
			AccountVersionBefore: 3, AccountVersionAfter: 4,
		}},
	)
	require.NoError(t, err)
	require.Equal(t, int64(60), account.balanceUnits)
	require.Equal(t, int64(4), account.version)

	err = syncCityLedgerAccountRefs(
		[]cityLedgerPostingLine{{account: account, creditUnits: 10}},
		[]*CityJournalEntry{{
			AccountID: 7, BalanceBeforeUnits: 100, BalanceAfterUnits: 90,
			AccountVersionBefore: 3, AccountVersionAfter: 4,
		}},
	)
	require.ErrorIs(t, err, ErrCitySimulationInvariant)
}
