//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type cityLedgerFixture struct {
	worldID      int64
	unitID       int64
	householdID  int64
	firmID       int64
	governmentID int64
}

func TestCityDoubleEntryLedgerPostsBalancesReversesAndProtectsFacts(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	ownerA := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-ledger-a-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	ownerB := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-ledger-b-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	outsider := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("city-ledger-outsider-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	createFixture := func(ownerID int64) cityLedgerFixture {
		scale := 2
		seed := int64(771122)
		foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
			OwnerUserID: ownerID, Name: "Balanced Ledger City", Timezone: "Asia/Shanghai", Seed: &seed,
			MonetaryUnit: service.CityMonetaryUnitCreateInput{
				Code: "city_credit", Name: "City Credit", Symbol: "CC", Scale: &scale,
			},
		})
		require.NoError(t, err)
		fixture := cityLedgerFixture{worldID: foundation.World.ID, unitID: foundation.MonetaryUnits[0].ID}
		for _, entity := range foundation.Entities {
			switch entity.EntityType {
			case service.CityEntityTypeHousehold:
				fixture.householdID = entity.ID
			case service.CityEntityTypeFirm:
				fixture.firmID = entity.ID
			case service.CityEntityTypeGovernment:
				fixture.governmentID = entity.ID
			}
		}
		require.Positive(t, fixture.householdID)
		require.Positive(t, fixture.firmID)
		require.Positive(t, fixture.governmentID)
		return fixture
	}
	worldA := createFixture(ownerA.ID)
	worldB := createFixture(ownerB.ID)

	marshalPayload := func(value any) json.RawMessage {
		raw, err := json.Marshal(value)
		require.NoError(t, err)
		return raw
	}
	submitBatch := func(ownerID int64, fixture cityLedgerFixture) []*service.CityCommand {
		expectedTick := int64(0)
		inputs := []service.CityCommandSubmitInput{
			{UserID: ownerID, WorldID: fixture.worldID, IdempotencyKey: "ledger-transfer-0", CommandType: service.CityCommandTypeLedgerCashTransfer,
				Payload: marshalPayload(map[string]any{"from_entity_id": fixture.householdID, "to_entity_id": fixture.firmID, "amount_units": 100, "memo": "gift"}), ExpectedWorldTick: &expectedTick},
			{UserID: ownerID, WorldID: fixture.worldID, IdempotencyKey: "ledger-wage-0", CommandType: service.CityCommandTypeLedgerWage,
				Payload: marshalPayload(map[string]any{"firm_entity_id": fixture.firmID, "household_entity_id": fixture.householdID, "amount_units": 200}), ExpectedWorldTick: &expectedTick},
			{UserID: ownerID, WorldID: fixture.worldID, IdempotencyKey: "ledger-purchase-0", CommandType: service.CityCommandTypeLedgerPurchase,
				Payload: marshalPayload(map[string]any{"household_entity_id": fixture.householdID, "firm_entity_id": fixture.firmID, "amount_units": 300}), ExpectedWorldTick: &expectedTick},
			{UserID: ownerID, WorldID: fixture.worldID, IdempotencyKey: "ledger-tax-0", CommandType: service.CityCommandTypeLedgerTax,
				Payload: marshalPayload(map[string]any{"payer_entity_id": fixture.householdID, "amount_units": 50}), ExpectedWorldTick: &expectedTick},
			{UserID: ownerID, WorldID: fixture.worldID, IdempotencyKey: "ledger-subsidy-0", CommandType: service.CityCommandTypeLedgerSubsidy,
				Payload: marshalPayload(map[string]any{"recipient_entity_id": fixture.householdID, "amount_units": 70}), ExpectedWorldTick: &expectedTick},
		}
		commands := make([]*service.CityCommand, 0, len(inputs))
		for _, input := range inputs {
			command, err := cityService.SubmitCommand(ctx, input)
			require.NoError(t, err)
			commands = append(commands, command)
		}
		return commands
	}
	commandsA := submitBatch(ownerA.ID, worldA)
	_ = submitBatch(ownerB.ID, worldB)
	expectedZero := int64(0)
	stepA, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerA.ID, WorldID: worldA.worldID, IdempotencyKey: "ledger-step-0", ExpectedWorldTick: &expectedZero,
	})
	require.NoError(t, err)
	stepB, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerB.ID, WorldID: worldB.worldID, IdempotencyKey: "ledger-step-0", ExpectedWorldTick: &expectedZero,
	})
	require.NoError(t, err)
	require.Equal(t, stepA.Tick.StateHash, stepB.Tick.StateHash)
	require.Equal(t, 5, stepA.Tick.CommandCount)
	require.Equal(t, 5, stepA.Tick.AppliedCommandCount)
	require.Zero(t, stepA.Tick.RejectedCommandCount)
	require.Len(t, stepA.Journals, 8)
	require.Len(t, stepA.Events, 9)
	for _, journal := range stepA.Journals {
		require.Equal(t, journal.DebitTotalUnits, journal.CreditTotalUnits)
		require.Positive(t, journal.DebitTotalUnits)
		require.GreaterOrEqual(t, journal.EntryCount, 2)
	}
	for _, command := range stepA.Commands {
		require.Equal(t, service.CityCommandStatusApplied, command.Status)
	}

	var purchaseJournal *service.CityJournal
	for _, journal := range stepA.Journals {
		if journal.JournalType == "purchase" {
			purchaseJournal = journal
		}
	}
	require.NotNil(t, purchaseJournal)
	loadedPurchase, err := cityService.GetJournal(ctx, ownerA.ID, worldA.worldID, purchaseJournal.Tick, purchaseJournal.Sequence)
	require.NoError(t, err)
	require.Len(t, loadedPurchase.Entries, 4)
	entityTotals := make(map[int64][2]int64)
	for _, entry := range loadedPurchase.Entries {
		totals := entityTotals[entry.EntityID]
		totals[0] += entry.DebitUnits
		totals[1] += entry.CreditUnits
		entityTotals[entry.EntityID] = totals
	}
	for _, totals := range entityTotals {
		require.Equal(t, totals[0], totals[1])
	}

	trial, err := cityService.GetTrialBalance(ctx, ownerA.ID, worldA.worldID)
	require.NoError(t, err)
	require.True(t, trial.Balanced)
	require.Equal(t, int64(1), trial.AsOfTick)
	require.Len(t, trial.Units, 1)
	require.Zero(t, trial.Units[0].ProjectionMismatchCount)
	require.Zero(t, trial.Units[0].EntityImbalanceCount)
	require.Equal(t, trial.Units[0].TotalDebitUnits, trial.Units[0].TotalCreditUnits)

	page, err := cityService.ListJournals(ctx, service.CityJournalListInput{
		UserID: ownerA.ID, WorldID: worldA.worldID, Limit: 3,
	})
	require.NoError(t, err)
	require.Len(t, page.Items, 3)
	require.NotNil(t, page.NextCursor)
	nextPage, err := cityService.ListJournals(ctx, service.CityJournalListInput{
		UserID: ownerA.ID, WorldID: worldA.worldID, Limit: 10,
		AfterTick: page.NextCursor.Tick, AfterSequence: page.NextCursor.Sequence,
	})
	require.NoError(t, err)
	require.Len(t, nextPage.Items, 5)
	_, err = cityService.ListJournals(ctx, service.CityJournalListInput{UserID: outsider.ID, WorldID: worldA.worldID})
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)
	_, err = cityService.GetTrialBalance(ctx, outsider.ID, worldA.worldID)
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)

	replayed, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerA.ID, WorldID: worldA.worldID, IdempotencyKey: "ledger-step-0", ExpectedWorldTick: &expectedZero,
	})
	require.NoError(t, err)
	require.Equal(t, stepA.Tick.ID, replayed.Tick.ID)
	require.Len(t, replayed.Journals, 8)
	var journalCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM city_journals WHERE world_id = $1`, worldA.worldID).Scan(&journalCount))
	require.Equal(t, 8, journalCount)

	expectedOne := int64(1)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: ownerA.ID, WorldID: worldA.worldID, IdempotencyKey: "ledger-insufficient-1",
		CommandType:       service.CityCommandTypeLedgerPurchase,
		Payload:           marshalPayload(map[string]any{"household_entity_id": worldA.householdID, "firm_entity_id": worldA.firmID, "amount_units": 2_000_000}),
		ExpectedWorldTick: &expectedOne,
	})
	require.NoError(t, err)
	insufficientStep, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerA.ID, WorldID: worldA.worldID, IdempotencyKey: "ledger-step-1", ExpectedWorldTick: &expectedOne,
	})
	require.NoError(t, err)
	require.Len(t, insufficientStep.Journals, 0)
	require.Equal(t, 1, insufficientStep.Tick.RejectedCommandCount)
	require.Equal(t, service.CityCommandStatusRejected, insufficientStep.Commands[0].Status)
	require.Equal(t, "CITY_LEDGER_INSUFFICIENT_BALANCE", *insufficientStep.Commands[0].ErrorCode)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM city_journals WHERE world_id = $1`, worldA.worldID).Scan(&journalCount))
	require.Equal(t, 8, journalCount)

	expectedTwo := int64(2)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: ownerA.ID, WorldID: worldA.worldID, IdempotencyKey: "ledger-reverse-2",
		CommandType: service.CityCommandTypeLedgerReverse,
		Payload: marshalPayload(map[string]any{
			"journal_tick": purchaseJournal.Tick, "journal_sequence": purchaseJournal.Sequence, "reason": "customer refund",
		}), ExpectedWorldTick: &expectedTwo,
	})
	require.NoError(t, err)
	reversalStep, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerA.ID, WorldID: worldA.worldID, IdempotencyKey: "ledger-step-2", ExpectedWorldTick: &expectedTwo,
	})
	require.NoError(t, err)
	require.Len(t, reversalStep.Journals, 1)
	reversal := reversalStep.Journals[0]
	require.Equal(t, "reversal", reversal.JournalType)
	require.Equal(t, purchaseJournal.ID, *reversal.ReversalOfJournalID)
	require.Equal(t, purchaseJournal.Tick, *reversal.ReversalOfTick)
	require.Equal(t, purchaseJournal.Sequence, *reversal.ReversalOfSequence)

	accountBalance := func(entityID int64, accountCode string) int64 {
		var balance int64
		err := integrationDB.QueryRowContext(ctx, `
SELECT account.current_balance_units
FROM city_accounts account
JOIN city_account_templates template ON template.id = account.template_id
WHERE account.world_id = $1 AND account.entity_id = $2 AND template.code = $3`,
			worldA.worldID, entityID, accountCode).Scan(&balance)
		require.NoError(t, err)
		return balance
	}
	require.Equal(t, int64(1_000_120), accountBalance(worldA.householdID, "cash"))
	require.Zero(t, accountBalance(worldA.householdID, "consumption_expense"))
	require.Equal(t, int64(4_999_900), accountBalance(worldA.firmID, "cash"))
	require.Zero(t, accountBalance(worldA.firmID, "revenue"))
	require.Equal(t, int64(9_999_980), accountBalance(worldA.governmentID, "cash"))
	trial, err = cityService.GetTrialBalance(ctx, ownerA.ID, worldA.worldID)
	require.NoError(t, err)
	require.True(t, trial.Balanced)

	expectedThree := int64(3)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: ownerA.ID, WorldID: worldA.worldID, IdempotencyKey: "ledger-reverse-again-3",
		CommandType: service.CityCommandTypeLedgerReverse,
		Payload: marshalPayload(map[string]any{
			"journal_tick": purchaseJournal.Tick, "journal_sequence": purchaseJournal.Sequence, "reason": "duplicate refund",
		}), ExpectedWorldTick: &expectedThree,
	})
	require.NoError(t, err)
	duplicateReversalStep, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerA.ID, WorldID: worldA.worldID, IdempotencyKey: "ledger-step-3", ExpectedWorldTick: &expectedThree,
	})
	require.NoError(t, err)
	require.Len(t, duplicateReversalStep.Journals, 0)
	require.Equal(t, "CITY_LEDGER_ALREADY_REVERSED", *duplicateReversalStep.Commands[0].ErrorCode)

	_, err = integrationDB.ExecContext(ctx, `UPDATE city_journals SET description = 'tampered' WHERE id = $1`, purchaseJournal.ID)
	require.ErrorContains(t, err, "draft-to-posted transition")
	var entryID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT id FROM city_journal_entries WHERE journal_id = $1 ORDER BY line_no LIMIT 1`, purchaseJournal.ID).Scan(&entryID))
	_, err = integrationDB.ExecContext(ctx, `DELETE FROM city_journal_entries WHERE id = $1`, entryID)
	require.ErrorContains(t, err, "immutable facts")
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_accounts SET current_balance_units = current_balance_units + 1
WHERE world_id = $1 AND entity_id = $2`, worldA.worldID, worldA.householdID)
	require.ErrorContains(t, err, "only change through a draft journal")
	_, err = integrationDB.ExecContext(ctx, `
INSERT INTO city_journals
    (world_id, monetary_unit_id, tick, sequence, operation_key, journal_type, posted_at)
VALUES ($1, $2, 3, 900, 'invalid:preposted', 'opening', NOW())`, worldA.worldID, worldA.unitID)
	require.ErrorContains(t, err, "must be inserted as drafts")

	beforeUnbalanced := accountBalance(worldA.householdID, "cash")
	unbalancedTx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	var invalidJournalID, invalidEntryID int64
	require.NoError(t, unbalancedTx.QueryRowContext(ctx, `
INSERT INTO city_journals
    (world_id, monetary_unit_id, tick, sequence, operation_key, journal_type)
VALUES ($1, $2, 3, 901, 'invalid:unbalanced', 'opening')
RETURNING id`, worldA.worldID, worldA.unitID).Scan(&invalidJournalID))
	var householdCashID int64
	require.NoError(t, unbalancedTx.QueryRowContext(ctx, `
SELECT account.id
FROM city_accounts account
JOIN city_account_templates template ON template.id = account.template_id
WHERE account.world_id = $1 AND account.entity_id = $2 AND template.code = 'cash'`,
		worldA.worldID, worldA.householdID).Scan(&householdCashID))
	require.NoError(t, unbalancedTx.QueryRowContext(ctx, `
SELECT post_city_journal_entry($1, $2, 1, 1, 0, 'invalid')`, invalidJournalID, householdCashID).Scan(&invalidEntryID))
	_, err = unbalancedTx.ExecContext(ctx, `UPDATE city_journals SET posted_at = NOW() WHERE id = $1`, invalidJournalID)
	require.ErrorContains(t, err, "not balanced")
	require.NoError(t, unbalancedTx.Rollback())
	require.Equal(t, beforeUnbalanced, accountBalance(worldA.householdID, "cash"))

	for index, command := range commandsA {
		require.Equal(t, int64(index+1), command.Sequence)
	}
}
