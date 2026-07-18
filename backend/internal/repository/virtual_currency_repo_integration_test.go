//go:build integration

package repository

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestVirtualCurrencyRepositoryLifecycleAndMaintenance(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	group := mustCreateGroup(t, client, &service.Group{
		Name: fmt.Sprintf("virtual-currency-group-%s", suffix),
	})
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("virtual-currency-user-%s@example.com", suffix),
		PasswordHash: "integration-test-password",
	})
	repo := NewVirtualCurrencyRepository(integrationDB)
	currencyService := service.NewVirtualCurrencyService(repo)
	currency, err := currencyService.CreateCurrency(ctx, service.VirtualCurrencyCreateInput{
		Code:   "itgold" + suffix,
		Name:   "Integration Gold",
		Symbol: "G",
		Scale:  2,
	})
	require.NoError(t, err)
	require.NotNil(t, currency)

	_, err = currencyService.UpsertGroupPolicy(ctx, service.VirtualCurrencyPolicyInput{
		CurrencyID: currency.ID,
		GroupID:    group.ID,
		Enabled:    true,
		CanEarn:    true,
		CanSpend:   true,
	})
	require.NoError(t, err)

	grant := service.VirtualCurrencyGrantInput{
		CurrencyCode:      currency.Code,
		UserID:            user.ID,
		GroupID:           group.ID,
		AmountUnits:       100,
		SourceType:        service.VirtualCurrencySourceMission,
		SourceID:          "mission:" + suffix,
		IdempotencyKey:    "grant:" + suffix,
		Reason:            "integration reward",
		RequireUserAccess: true,
	}
	grantEntry, err := currencyService.Grant(ctx, grant)
	require.NoError(t, err)
	require.Positive(t, grantEntry.JournalID)
	require.Equal(t, int64(100), grantEntry.AvailableAfterUnits)

	replayedGrant, err := currencyService.Grant(ctx, grant)
	require.NoError(t, err)
	require.Equal(t, grantEntry.ID, replayedGrant.ID, "same idempotency key must replay the original ledger entry")
	require.Equal(t, grantEntry.JournalID, replayedGrant.JournalID)

	spendEntry, err := currencyService.Spend(ctx, service.VirtualCurrencySpendInput{
		CurrencyCode:   currency.Code,
		UserID:         user.ID,
		GroupID:        group.ID,
		AmountUnits:    25,
		SourceType:     service.VirtualCurrencySourceGame,
		SourceID:       "purchase:" + suffix,
		IdempotencyKey: "spend:" + suffix,
		Reason:         "integration purchase",
	})
	require.NoError(t, err)
	require.Equal(t, int64(75), spendEntry.AvailableAfterUnits)

	releasedHold, err := currencyService.ReserveHold(ctx, service.VirtualCurrencyReserveInput{
		CurrencyCode:   currency.Code,
		UserID:         user.ID,
		GroupID:        group.ID,
		AmountUnits:    20,
		ExpiresAt:      time.Now().UTC().Add(time.Hour),
		SourceType:     service.VirtualCurrencySourceGame,
		SourceID:       "order-release:" + suffix,
		IdempotencyKey: "reserve-release:" + suffix,
		Reason:         "release path",
	})
	require.NoError(t, err)
	require.Equal(t, int64(55), releasedHold.Ledger.AvailableAfterUnits)
	require.Equal(t, int64(20), releasedHold.Ledger.ReservedAfterUnits)

	settled, err := currencyService.ReleaseHold(ctx, service.VirtualCurrencyHoldSettlementInput{
		HoldID:         releasedHold.Hold.ID,
		UserID:         user.ID,
		SourceType:     service.VirtualCurrencySourceGame,
		SourceID:       "order-release:" + suffix,
		IdempotencyKey: "release:" + suffix,
		Reason:         "release path",
	})
	require.NoError(t, err)
	require.Equal(t, service.VirtualCurrencyHoldStatusReleased, settled.Hold.Status)
	require.Equal(t, int64(75), settled.Ledger.AvailableAfterUnits, "released funds must return to available balance")
	require.Zero(t, settled.Ledger.ReservedAfterUnits)

	expiringHold, err := currencyService.ReserveHold(ctx, service.VirtualCurrencyReserveInput{
		CurrencyCode:   currency.Code,
		UserID:         user.ID,
		GroupID:        group.ID,
		AmountUnits:    15,
		ExpiresAt:      time.Now().UTC().Add(time.Hour),
		SourceType:     service.VirtualCurrencySourceGame,
		SourceID:       "order-expire:" + suffix,
		IdempotencyKey: "reserve-expire:" + suffix,
		Reason:         "expiry path",
	})
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, "UPDATE virtual_currency_holds SET expires_at = NOW() - INTERVAL '1 second' WHERE id = $1", expiringHold.Hold.ID)
	require.NoError(t, err)

	expired, err := currencyService.ExpireExpiredHolds(ctx, currency.ID, 100)
	require.NoError(t, err)
	require.Equal(t, int64(1), expired)
	wallets, err := currencyService.ListUserWallets(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, wallets, 1)
	require.Equal(t, int64(75), wallets[0].AvailableUnits, "expired funds must return to available balance")
	require.Zero(t, wallets[0].ReservedUnits)

	report, err := currencyService.ReconcileCurrency(ctx, currency.ID, 20)
	require.NoError(t, err)
	require.Equal(t, int64(1), report.WalletCount)
	require.Equal(t, int64(1), report.LedgerUserCount)
	require.Zero(t, report.MismatchCount)
	require.GreaterOrEqual(t, report.Accounting.JournalCount, int64(6))
	require.GreaterOrEqual(t, report.Accounting.PostingCount, int64(12))
	require.Zero(t, report.Accounting.InvalidJournalCount)
	require.Equal(t, "75", report.Accounting.WalletAvailableUnits)
	require.Equal(t, "0", report.Accounting.WalletReservedUnits)
	require.Equal(t, "100", report.Accounting.GrossIssuedUnits)
	require.Equal(t, "25", report.Accounting.NetSinkUnits)
	require.Equal(t, "0", report.Accounting.ProjectionDeltaUnits)
	require.Equal(t, "0", report.Accounting.ConservationDeltaUnits)

	_, err = integrationDB.ExecContext(ctx, "UPDATE virtual_currency_wallets SET available_units = available_units - 1 WHERE user_id = $1 AND currency_id = $2", user.ID, currency.ID)
	require.NoError(t, err)
	driftReport, err := currencyService.ReconcileCurrency(ctx, currency.ID, 20)
	require.NoError(t, err)
	require.Equal(t, int64(1), driftReport.MismatchCount)
	require.Len(t, driftReport.Mismatches, 1)
	require.Equal(t, int64(74), driftReport.Mismatches[0].WalletAvailable)
	require.Equal(t, int64(75), driftReport.Mismatches[0].LedgerAvailable)
	require.Equal(t, "-1", driftReport.Accounting.ProjectionDeltaUnits)
	_, err = integrationDB.ExecContext(ctx, "UPDATE virtual_currency_wallets SET available_units = available_units + 1 WHERE user_id = $1 AND currency_id = $2", user.ID, currency.ID)
	require.NoError(t, err)

	ledger, page, err := currencyService.ListLedger(ctx, service.VirtualCurrencyLedgerQuery{UserID: user.ID, Page: 1, PageSize: 100})
	require.NoError(t, err)
	require.GreaterOrEqual(t, page.Total, int64(5))
	require.GreaterOrEqual(t, len(ledger), 5)

	var invalidJournals int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM (
    SELECT j.id
    FROM virtual_currency_journals j
    LEFT JOIN virtual_currency_postings p ON p.journal_id = j.id
    WHERE j.currency_id = $1
    GROUP BY j.id, j.posted_at
    HAVING j.posted_at IS NULL OR COUNT(p.id) < 2 OR COALESCE(SUM(p.amount_units), 0) <> 0
) invalid`, currency.ID).Scan(&invalidJournals))
	require.Zero(t, invalidJournals)

	_, err = integrationDB.ExecContext(ctx, "UPDATE virtual_currency_ledger_entries SET reason = reason WHERE id = $1", grantEntry.ID)
	require.ErrorContains(t, err, "immutable")
	_, err = integrationDB.ExecContext(ctx, `
INSERT INTO virtual_currency_postings (journal_id, currency_id, user_id, account_kind, amount_units)
VALUES ($1, $2, $3, 'user_available', 1)`, grantEntry.JournalID, currency.ID, user.ID)
	require.ErrorContains(t, err, "sealed")

	unbalancedTx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	var unbalancedJournalID int64
	err = unbalancedTx.QueryRowContext(ctx, `
INSERT INTO virtual_currency_journals
    (currency_id, initiator_user_id, group_id, entry_type, source_type, source_id,
     idempotency_key, request_fingerprint, reason)
VALUES ($1, $2, $3, 'adjustment', 'admin', $4, $5, $6, 'constraint test')
RETURNING id`, currency.ID, user.ID, group.ID, "unbalanced:"+suffix, "unbalanced:"+suffix, fmt.Sprintf("%064x", 1)).Scan(&unbalancedJournalID)
	require.NoError(t, err)
	_, err = unbalancedTx.ExecContext(ctx, `
INSERT INTO virtual_currency_postings (journal_id, currency_id, user_id, account_kind, amount_units)
VALUES ($1, $2, $3, 'user_available', 1)`, unbalancedJournalID, currency.ID, user.ID)
	require.NoError(t, err)
	_, err = unbalancedTx.ExecContext(ctx, "UPDATE virtual_currency_journals SET posted_at = NOW() WHERE id = $1", unbalancedJournalID)
	require.NoError(t, err)
	require.ErrorContains(t, unbalancedTx.Commit(), "not balanced")
}

func TestVirtualCurrencyRepositoryEnableForAllUsersUsesPublicGroupsWithoutCreatingWallets(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	publicGroup := mustCreateGroup(t, client, &service.Group{Name: "currency-public-" + suffix})
	exclusiveGroup := mustCreateGroup(t, client, &service.Group{Name: "currency-exclusive-" + suffix, IsExclusive: true})
	subscriptionGroup := mustCreateGroup(t, client, &service.Group{Name: "currency-subscription-" + suffix, SubscriptionType: service.SubscriptionTypeSubscription})
	disabledGroup := mustCreateGroup(t, client, &service.Group{Name: "currency-disabled-" + suffix, Status: service.StatusDisabled})
	user := mustCreateUser(t, client, &service.User{
		Email:        "currency-sync-user-" + suffix + "@example.com",
		PasswordHash: "integration-test-password",
	})
	repo := NewVirtualCurrencyRepository(integrationDB)
	currencyService := service.NewVirtualCurrencyService(repo)
	currency, err := currencyService.CreateCurrency(ctx, service.VirtualCurrencyCreateInput{
		Code:   "syncgold" + suffix,
		Name:   "Synchronized Gold",
		Symbol: "G",
		Scale:  2,
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM virtual_currency_wallets WHERE currency_id = $1", currency.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM virtual_currency_group_policies WHERE currency_id = $1", currency.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM virtual_currencies WHERE id = $1", currency.ID)
		_ = client.User.DeleteOneID(user.ID).Exec(ctx)
		for _, group := range []*service.Group{publicGroup, exclusiveGroup, subscriptionGroup, disabledGroup} {
			_ = client.Group.DeleteOneID(group.ID).Exec(ctx)
		}
	})

	maxBalance := int64(12345)
	_, err = currencyService.UpsertGroupPolicy(ctx, service.VirtualCurrencyPolicyInput{
		CurrencyID:      currency.ID,
		GroupID:         publicGroup.ID,
		Enabled:         false,
		CanEarn:         false,
		CanSpend:        false,
		MaxBalanceUnits: &maxBalance,
		Metadata:        map[string]any{"source": "manual"},
	})
	require.NoError(t, err)
	eligibleRows, err := integrationDB.QueryContext(ctx, `
SELECT id
FROM groups
WHERE status = 'active'
  AND deleted_at IS NULL
  AND subscription_type = 'standard'
  AND is_exclusive = FALSE
ORDER BY id ASC`)
	require.NoError(t, err)
	eligibleGroupIDs := make([]int64, 0)
	for eligibleRows.Next() {
		var groupID int64
		require.NoError(t, eligibleRows.Scan(&groupID))
		eligibleGroupIDs = append(eligibleGroupIDs, groupID)
	}
	require.NoError(t, eligibleRows.Err())
	require.NoError(t, eligibleRows.Close())

	policies, err := currencyService.EnableForAllUsers(ctx, currency.ID)
	require.NoError(t, err)
	require.Len(t, policies, len(eligibleGroupIDs))
	actualGroupIDs := make([]int64, 0, len(policies))
	var publicPolicy *service.VirtualCurrencyGroupPolicy
	for _, policy := range policies {
		actualGroupIDs = append(actualGroupIDs, policy.GroupID)
		require.True(t, policy.Enabled)
		require.True(t, policy.CanEarn)
		require.True(t, policy.CanSpend)
		if policy.GroupID == publicGroup.ID {
			publicPolicy = policy
		}
	}
	require.Equal(t, eligibleGroupIDs, actualGroupIDs)
	require.NotNil(t, publicPolicy)
	require.Equal(t, maxBalance, *publicPolicy.MaxBalanceUnits, "bulk enable must preserve an existing balance limit")
	require.Equal(t, "manual", publicPolicy.Metadata["source"], "bulk enable must preserve policy metadata")
	for _, excludedID := range []int64{exclusiveGroup.ID, subscriptionGroup.ID, disabledGroup.ID} {
		require.NotContains(t, actualGroupIDs, excludedID)
	}

	wallets, err := currencyService.ListUserWallets(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, wallets, 1)
	require.Equal(t, currency.ID, wallets[0].CurrencyID)
	require.Equal(t, eligibleGroupIDs, wallets[0].GroupIDs)
	require.Zero(t, wallets[0].AvailableUnits)
	require.Zero(t, wallets[0].ReservedUnits)

	var walletRows int
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM virtual_currency_wallets WHERE currency_id = $1 AND user_id = $2",
		currency.ID, user.ID,
	).Scan(&walletRows))
	require.Zero(t, walletRows, "enabling for all users should not create empty wallet rows")
}
