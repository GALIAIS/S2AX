//go:build integration

package repository

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountAllocationControlPlaneLifecycle(t *testing.T) {
	isolateIntegrationData(t)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	adminID := insertAccountAllocationTestUser(t, ctx, "allocation-admin@example.test", "admin")
	targetID := insertAccountAllocationTestUser(t, ctx, "allocation-target@example.test", "user")
	observerID := insertAccountAllocationTestUser(t, ctx, "allocation-observer@example.test", "user")
	groupID := insertAccountAllocationTestGroup(t, ctx, "allocation-exclusive", true)
	_, err := integrationDB.ExecContext(ctx, `
		INSERT INTO user_allowed_groups (user_id, group_id)
		VALUES ($1, $2)`, targetID, groupID)
	require.NoError(t, err)

	accountIDs := []int64{
		insertAccountAllocationTestAccount(t, ctx, groupID, "alpha.account@example.test", 10),
		insertAccountAllocationTestAccount(t, ctx, groupID, "bravo.account@example.test", 20),
		insertAccountAllocationTestAccount(t, ctx, groupID, "charlie.account@example.test", 30),
	}

	svc := service.NewAccountAllocationService(integrationDB, &config.Config{
		AccountAllocation: config.AccountAllocationConfig{
			ReconcileIntervalSeconds: 15,
			PolicyBatchSize:          100,
			MaxDesiredCount:          50,
		},
	})

	policy, err := svc.CreatePolicy(ctx, service.AccountAllocationPolicyInput{
		UserID:        targetID,
		GroupID:       groupID,
		DesiredCount:  2,
		AutoReplenish: true,
		ReplaceOn401:  true,
		ReplaceOn429:  true,
		ActorUserID:   adminID,
	})
	require.NoError(t, err)
	require.Equal(t, service.AccountAllocationAccessReady, policy.AccessStatus)
	require.Equal(t, 2, policy.ActiveAssignmentCount)
	require.Zero(t, policy.Shortage)

	active := activeAccountAllocationAssignments(t, ctx, svc, policy.ID)
	require.Len(t, active, 2)
	require.Equal(t, accountIDs[:2], sortedAccountAllocationIDs(active))

	targetCandidates, err := svc.FilterCandidates(ctx, targetID, &groupID, accountAllocationCandidates(accountIDs))
	require.NoError(t, err)
	require.Equal(t, accountIDs[:2], sortedServiceAccountIDs(targetCandidates))
	observerCandidates, err := svc.FilterCandidates(ctx, observerID, &groupID, accountAllocationCandidates(accountIDs))
	require.NoError(t, err)
	require.Equal(t, accountIDs[2:], sortedServiceAccountIDs(observerCandidates))

	targetDirectory, err := svc.ListUserVisibleAccounts(ctx, targetID)
	require.NoError(t, err)
	targetDedicatedCount := 0
	for _, item := range targetDirectory.Items {
		if item.GroupID != groupID {
			continue
		}
		targetDedicatedCount++
		require.Equal(t, service.AccountAllocationVisibleSourceDedicated, item.Source)
		require.True(t, item.AccountNameMasked)
		require.NotContains(t, item.AccountName, "alpha.account")
		require.NotContains(t, item.AccountName, "bravo.account")
	}
	require.Equal(t, 2, targetDedicatedCount)
	observerDirectory, err := svc.ListUserVisibleAccounts(ctx, observerID)
	require.NoError(t, err)
	for _, item := range observerDirectory.Items {
		require.NotEqual(t, groupID, item.GroupID)
	}

	overview, err := svc.GetOverview(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, overview.ActivePolicyCount)
	require.Equal(t, 2, overview.DesiredAccountCount)
	require.Equal(t, 2, overview.ActiveAssignmentCount)
	require.Zero(t, overview.ShortageCount)
	require.Zero(t, overview.BlockedPolicyCount)

	// A persisted 429 cooldown releases the leased account and fills the gap
	// with a different healthy idle account.
	rateLimitedAccountID := active[0].AccountID
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE accounts
		SET rate_limit_reset_at = NOW() + INTERVAL '30 minutes'
		WHERE id = $1`, rateLimitedAccountID)
	require.NoError(t, err)
	reconciled429, err := svc.ReconcilePolicy(ctx, policy.ID)
	require.NoError(t, err)
	require.Equal(t, 1, reconciled429.ReleasedCount)
	require.Equal(t, 1, reconciled429.AssignedCount)
	require.Zero(t, reconciled429.Shortage)
	require.False(t, assignmentIsActiveForAccount(t, ctx, policy.ID, rateLimitedAccountID))
	require.Equal(t, "rate_limited_429", latestAccountAllocationReleaseReason(t, ctx, policy.ID, rateLimitedAccountID))

	_, err = integrationDB.ExecContext(ctx, `
		UPDATE accounts
		SET rate_limit_reset_at = NULL
		WHERE id = $1`, rateLimitedAccountID)
	require.NoError(t, err)

	// A permanent local 401 marker follows the independent 401 toggle and is
	// replaced with the recovered idle account.
	active = activeAccountAllocationAssignments(t, ctx, svc, policy.ID)
	authFailedAccountID := active[0].AccountID
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE accounts
		SET status = 'error',
			schedulable = FALSE,
			error_message = 'Authentication failed (401): integration fixture'
		WHERE id = $1`, authFailedAccountID)
	require.NoError(t, err)
	reconciled401, err := svc.ReconcilePolicy(ctx, policy.ID)
	require.NoError(t, err)
	require.Equal(t, 1, reconciled401.ReleasedCount)
	require.Equal(t, 1, reconciled401.AssignedCount)
	require.Zero(t, reconciled401.Shortage)
	require.False(t, assignmentIsActiveForAccount(t, ctx, policy.ID, authFailedAccountID))
	require.Equal(t, "authentication_failed_401", latestAccountAllocationReleaseReason(t, ctx, policy.ID, authFailedAccountID))

	// Revoking exclusive-group access fails closed: every lease is released and
	// the policy remains active but explicitly blocked, ready to recover later.
	_, err = integrationDB.ExecContext(ctx, `
		DELETE FROM user_allowed_groups
		WHERE user_id = $1 AND group_id = $2`, targetID, groupID)
	require.NoError(t, err)
	require.Empty(t, activeAccountAllocationAssignments(t, ctx, svc, policy.ID))
	blocked, err := svc.ReconcilePolicy(ctx, policy.ID)
	require.NoError(t, err)
	require.Equal(t, service.AccountAllocationAccessGroupPermission, blocked.AccessStatus)
	require.Zero(t, blocked.ReleasedCount)
	require.Zero(t, blocked.ActiveAfter)
	require.Equal(t, 2, blocked.Shortage)
	policy, err = svc.GetPolicy(ctx, policy.ID)
	require.NoError(t, err)
	require.Equal(t, service.AccountAllocationAccessGroupPermission, policy.AccessStatus)
	overview, err = svc.GetOverview(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, overview.BlockedPolicyCount)

	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO user_allowed_groups (user_id, group_id)
		VALUES ($1, $2)`, targetID, groupID)
	require.NoError(t, err)
	recovered, err := svc.ReconcilePolicy(ctx, policy.ID)
	require.NoError(t, err)
	require.Equal(t, service.AccountAllocationAccessReady, recovered.AccessStatus)
	require.Equal(t, 2, recovered.AssignedCount)
	require.Zero(t, recovered.Shortage)

	// Manual release does not immediately assign the same account back. With
	// only one alternative healthy account already leased, the policy exposes a
	// real shortage until the next explicit/background reconciliation.
	active = activeAccountAllocationAssignments(t, ctx, svc, policy.ID)
	manualReleasedAccountID := active[0].AccountID
	require.NoError(t, svc.ReleaseAssignment(ctx, policy.ID, active[0].ID, adminID))
	require.False(t, assignmentIsActiveForAccount(t, ctx, policy.ID, manualReleasedAccountID))
	policy, err = svc.GetPolicy(ctx, policy.ID)
	require.NoError(t, err)
	require.Equal(t, 1, policy.ActiveAssignmentCount)
	require.Equal(t, 1, policy.Shortage)

	refilled, err := svc.ReconcilePolicy(ctx, policy.ID)
	require.NoError(t, err)
	require.Equal(t, 1, refilled.AssignedCount)
	require.Zero(t, refilled.Shortage)

	// Manual-only policies are still health-reconciled by ReconcileAll; they
	// release unusable leases but deliberately do not fill the resulting gap.
	policy, err = svc.UpdatePolicy(ctx, policy.ID, service.AccountAllocationPolicyUpdate{
		DesiredCount:  2,
		AutoReplenish: false,
		ReplaceOn401:  true,
		ReplaceOn429:  true,
		ActorUserID:   adminID,
	})
	require.NoError(t, err)
	active = activeAccountAllocationAssignments(t, ctx, svc, policy.ID)
	unavailableAccountID := active[0].AccountID
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE accounts
		SET status = 'disabled',
			schedulable = FALSE,
			error_message = NULL
		WHERE id = $1`, unavailableAccountID)
	require.NoError(t, err)
	allResults, err := svc.ReconcileAll(ctx)
	require.NoError(t, err)
	require.Len(t, allResults, 1)
	require.Equal(t, 1, allResults[0].ReleasedCount)
	require.Zero(t, allResults[0].AssignedCount)
	require.Equal(t, 1, allResults[0].Shortage)

	policy, err = svc.SetPolicyStatus(ctx, policy.ID, false, adminID)
	require.NoError(t, err)
	require.Equal(t, "disabled", policy.Status)
	require.Zero(t, policy.ActiveAssignmentCount)

	var eventCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM account_allocation_events
		WHERE policy_id = $1`, policy.ID).Scan(&eventCount))
	require.GreaterOrEqual(t, eventCount, 12)
}

func TestAccountDirectoryIncludesSubscribedExclusiveGroupWithoutAllocationPolicy(t *testing.T) {
	isolateIntegrationData(t)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	adminID := insertAccountAllocationTestUser(t, ctx, "directory-admin@example.test", "admin")
	subscriberID := insertAccountAllocationTestUser(t, ctx, "directory-subscriber@example.test", "user")
	observerID := insertAccountAllocationTestUser(t, ctx, "directory-observer@example.test", "user")
	groupID := insertAccountAllocationTestSubscriptionGroup(t, ctx, "directory-subscription")
	insertAccountAllocationTestAccount(t, ctx, groupID, "subscribed.account@example.test", 10)

	_, err := integrationDB.ExecContext(ctx, `
		INSERT INTO user_subscriptions (
			user_id, group_id, starts_at, expires_at, status, assigned_by
		)
		VALUES ($1, $2, NOW(), NOW() + INTERVAL '30 days', 'active', $3)`,
		subscriberID, groupID, adminID)
	require.NoError(t, err)

	svc := service.NewAccountAllocationService(integrationDB, nil)
	subscriberDirectory, err := svc.ListUserVisibleAccounts(ctx, subscriberID)
	require.NoError(t, err)
	require.Equal(t, 1, subscriberDirectory.Summary.DedicatedGroupCount)
	require.Equal(t, 1, subscriberDirectory.Summary.DedicatedAccountCount)
	require.Len(t, subscriberDirectory.Items, 1)
	item := subscriberDirectory.Items[0]
	require.Equal(t, groupID, item.GroupID)
	require.Equal(t, service.AccountAllocationVisibleSourceDedicated, item.Source)
	require.Equal(t, service.AccountAllocationVisibleUsageScopeRolling24h, item.Usage.Scope)
	require.Nil(t, item.AssignedAt)
	require.True(t, item.AccountNameMasked)
	require.NotContains(t, item.AccountName, "subscribed.account")

	observerDirectory, err := svc.ListUserVisibleAccounts(ctx, observerID)
	require.NoError(t, err)
	for _, observerItem := range observerDirectory.Items {
		require.NotEqual(t, groupID, observerItem.GroupID)
	}
}

func insertAccountAllocationTestUser(t *testing.T, ctx context.Context, email, role string) int64 {
	t.Helper()
	var id int64
	err := integrationDB.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, role, status)
		VALUES ($1, 'integration-test-hash', $2, 'active')
		RETURNING id`, email, role).Scan(&id)
	require.NoError(t, err)
	return id
}

func insertAccountAllocationTestSubscriptionGroup(t *testing.T, ctx context.Context, name string) int64 {
	t.Helper()
	var id int64
	err := integrationDB.QueryRowContext(ctx, `
		INSERT INTO groups (name, description, rate_multiplier, is_exclusive, status, platform, subscription_type)
		VALUES ($1, 'account directory subscription fixture', 1, TRUE, 'active', 'openai', 'subscription')
		RETURNING id`, name).Scan(&id)
	require.NoError(t, err)
	return id
}

func insertAccountAllocationTestGroup(t *testing.T, ctx context.Context, name string, exclusive bool) int64 {
	t.Helper()
	var id int64
	err := integrationDB.QueryRowContext(ctx, `
		INSERT INTO groups (name, description, rate_multiplier, is_exclusive, status, platform, subscription_type)
		VALUES ($1, 'account allocation integration fixture', 1, $2, 'active', 'openai', 'standard')
		RETURNING id`, name, exclusive).Scan(&id)
	require.NoError(t, err)
	return id
}

func insertAccountAllocationTestAccount(t *testing.T, ctx context.Context, groupID int64, name string, priority int) int64 {
	t.Helper()
	var id int64
	err := integrationDB.QueryRowContext(ctx, `
		INSERT INTO accounts (
			name, platform, type, credentials, extra, concurrency, priority, status, schedulable
		)
		VALUES ($1, 'openai', 'apikey', '{}'::jsonb, '{}'::jsonb, 2, $2, 'active', TRUE)
		RETURNING id`, name, priority).Scan(&id)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO account_groups (account_id, group_id, priority)
		VALUES ($1, $2, $3)`, id, groupID, priority)
	require.NoError(t, err)
	return id
}

func activeAccountAllocationAssignments(t *testing.T, ctx context.Context, svc *service.AccountAllocationService, policyID int64) []service.AccountAllocationAssignment {
	t.Helper()
	items, err := svc.ListAssignments(ctx, policyID)
	require.NoError(t, err)
	active := make([]service.AccountAllocationAssignment, 0, len(items))
	for _, item := range items {
		if item.Status == "active" {
			active = append(active, item)
		}
	}
	return active
}

func sortedAccountAllocationIDs(items []service.AccountAllocationAssignment) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.AccountID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func accountAllocationCandidates(ids []int64) []service.Account {
	items := make([]service.Account, 0, len(ids))
	for _, id := range ids {
		items = append(items, service.Account{ID: id})
	}
	return items
}

func sortedServiceAccountIDs(items []service.Account) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func assignmentIsActiveForAccount(t *testing.T, ctx context.Context, policyID, accountID int64) bool {
	t.Helper()
	var active bool
	err := integrationDB.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM account_allocation_assignments
			WHERE policy_id = $1
				AND account_id = $2
				AND status = 'active'
		)`, policyID, accountID).Scan(&active)
	require.NoError(t, err)
	return active
}

func latestAccountAllocationReleaseReason(t *testing.T, ctx context.Context, policyID, accountID int64) string {
	t.Helper()
	var reason string
	err := integrationDB.QueryRowContext(ctx, `
		SELECT release_reason
		FROM account_allocation_assignments
		WHERE policy_id = $1
			AND account_id = $2
			AND status = 'released'
		ORDER BY released_at DESC, id DESC
		LIMIT 1`, policyID, accountID).Scan(&reason)
	require.NoError(t, err, fmt.Sprintf("release reason for account %d", accountID))
	return reason
}
