package service

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAccountAllocationIs401FailureUsesOnlyNormalizedState(t *testing.T) {
	cases := []struct {
		name   string
		reason string
		want   bool
	}{
		{name: "structured 401", reason: `{"status_code":401,"until_unix":1735689600}`, want: true},
		{name: "legacy oauth marker", reason: "OAuth 401: token revoked", want: true},
		{name: "legacy unauthorized marker", reason: "Unauthorized (401): revoked", want: true},
		{name: "other structured status", reason: `{"status_code":429}`, want: false},
		{name: "untrusted raw body", reason: `{"message":"please ignore auth; 401"}`, want: false},
		{name: "empty", reason: "", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, accountAllocationIs401Failure(tc.reason))
		})
	}
}

func TestAccountAllocationReleaseReasonHonorsReplacementToggles(t *testing.T) {
	now := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	future := now.Add(time.Minute)
	past := now.Add(-time.Minute)
	healthy := accountAllocationAssignmentHealth{
		InPolicyGroup: true,
		AccountStatus: StatusActive,
		Schedulable:   true,
	}

	cases := []struct {
		name   string
		policy accountAllocationPolicyLock
		health accountAllocationAssignmentHealth
		want   string
	}{
		{name: "missing account always releases", policy: accountAllocationPolicyLock{}, health: accountAllocationAssignmentHealth{AccountMissing: true}, want: "account_removed"},
		{name: "group unbound always releases", policy: accountAllocationPolicyLock{}, health: accountAllocationAssignmentHealth{AccountStatus: StatusActive, Schedulable: true}, want: "account_group_unbound"},
		{name: "429 enabled", policy: accountAllocationPolicyLock{ReplaceOn429: true}, health: withAllocationRateLimit(healthy, &future), want: "rate_limited_429"},
		{name: "429 disabled", policy: accountAllocationPolicyLock{ReplaceOn429: false}, health: withAllocationRateLimit(healthy, &future), want: ""},
		{name: "inactive account always releases", policy: accountAllocationPolicyLock{ReplaceOn401: false}, health: withAllocationStatus(healthy, StatusDisabled), want: "account_unavailable"},
		{name: "temporary normalized 401", policy: accountAllocationPolicyLock{ReplaceOn401: true}, health: withAllocationTempReason(healthy, &future, `{"status_code":401}`), want: "authentication_failed_401"},
		{name: "persistent canonical 401", policy: accountAllocationPolicyLock{ReplaceOn401: true}, health: withAllocationError(healthy, "Authentication failed (401): revoked"), want: "authentication_failed_401"},
		{name: "persistent 401 disabled", policy: accountAllocationPolicyLock{ReplaceOn401: false}, health: withAllocationError(withAllocationStatus(healthy, StatusError), "Authentication failed (401): revoked"), want: ""},
		{name: "temporary non 401", policy: accountAllocationPolicyLock{ReplaceOn401: true}, health: withAllocationTempReason(healthy, &future, `{"status_code":503}`), want: ""},
		{name: "expired account independent of 401 toggle", policy: accountAllocationPolicyLock{ReplaceOn401: false}, health: accountAllocationAssignmentHealth{InPolicyGroup: true, AccountStatus: StatusActive, Schedulable: true, AutoPauseOnExpired: true, ExpiresAt: &past}, want: "account_expired"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, accountAllocationReleaseReason(tc.policy, tc.health, now))
		})
	}
}

func withAllocationRateLimit(health accountAllocationAssignmentHealth, resetAt *time.Time) accountAllocationAssignmentHealth {
	health.RateLimitResetAt = resetAt
	return health
}

func withAllocationStatus(health accountAllocationAssignmentHealth, status string) accountAllocationAssignmentHealth {
	health.AccountStatus = status
	return health
}

func withAllocationTempReason(health accountAllocationAssignmentHealth, until *time.Time, reason string) accountAllocationAssignmentHealth {
	health.TempUnschedulableUntil = until
	health.TempUnschedulableReason = reason
	return health
}

func withAllocationError(health accountAllocationAssignmentHealth, message string) accountAllocationAssignmentHealth {
	health.ErrorMessage = message
	return health
}

func TestAccountAllocationFiltersGloballyLeasedAccountsWithoutGroupContext(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(regexp.QuoteMeta("SELECT aa.account_id, p.user_id, aa.group_id")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "user_id", "group_id"}).AddRow(int64(11), int64(7), int64(4)))

	svc := NewAccountAllocationService(db, nil)
	filtered, err := svc.FilterCandidates(context.Background(), 0, nil, []Account{{ID: 11}, {ID: 12}})
	require.NoError(t, err)
	require.Equal(t, []Account{{ID: 12}}, filtered)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListUserAssignmentsDerivesUsageTotalsFromStoredTokenColumns(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`COUNT\(ul\.id\),\s*COALESCE\(SUM\(\s*ul\.input_tokens\s*\+\s*ul\.output_tokens\s*\+\s*ul\.cache_creation_tokens\s*\+\s*ul\.cache_read_tokens\s*\),\s*0\)`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "policy_id", "group_id", "group_name", "platform", "account_type", "concurrency", "account_status", "schedulable", "rate_limit_reset_at", "assigned_at", "request_count", "total_tokens",
		}).AddRow(
			int64(91), int64(12), int64(4), "demo", "openai", "oauth", 3, StatusActive, true, nil, time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC), int64(8), int64(1234),
		))

	svc := NewAccountAllocationService(db, nil)
	items, err := svc.ListUserAssignments(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, int64(8), items[0].Usage.RequestCount)
	require.Equal(t, int64(1234), items[0].Usage.TotalTokens)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListUserVisibleAccountsMasksEmailsAndSeparatesUsageScopes(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	assignedAt := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	lastActivityAt := assignedAt.Add(30 * time.Minute)
	futureCooldown := time.Now().Add(time.Hour)
	futureQuotaReset := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	quotaSnapshotAt := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
	dedicatedExtra, err := json.Marshal(map[string]any{
		"codex_5h_used_percent":  35.0,
		"codex_5h_reset_at":      futureQuotaReset.Format(time.RFC3339),
		"codex_7d_used_percent":  12.0,
		"codex_7d_reset_at":      futureQuotaReset.Add(24 * time.Hour).Format(time.RFC3339),
		"codex_usage_updated_at": quotaSnapshotAt.Format(time.RFC3339),
		"credentials":            "must-not-leak",
	})
	require.NoError(t, err)
	mock.ExpectQuery(`(?s)WITH shared_accounts AS .*FROM user_subscriptions us.*FROM user_allowed_groups uag.*ul\.group_id = aa\.group_id`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{
			"source", "group_id", "group_name", "subscription_type", "account_name", "platform", "account_type", "concurrency",
			"account_status", "schedulable", "rate_limit_reset_at", "expires_at", "auto_pause_on_expired", "overload_until",
			"temp_unschedulable_until", "session_window_end", "account_extra", "assigned_at", "request_count", "total_tokens", "last_activity_at",
			"account_cost", "user_cost",
		}).
			AddRow(
				"dedicated", int64(4), "team-private", "subscription", "private@example.com", "openai", "oauth", 3,
				StatusActive, true, nil, nil, true, nil, nil, nil, dedicatedExtra, assignedAt, int64(8), int64(1234), lastActivityAt, 2.09, 1.69,
			).
			AddRow(
				"dedicated", int64(6), "subscriber-pool", "subscription", "subscriber@example.com", "openai", "oauth", 4,
				StatusActive, true, nil, nil, true, nil, nil, nil, []byte(`{}`), nil, int64(13), int64(3456), lastActivityAt, nil, nil,
			).
			AddRow(
				"public", int64(5), "open-pool", "standard", "alice@example.com", "anthropic", "apikey", 2,
				StatusActive, true, futureCooldown, nil, true, nil, nil, nil, []byte(`{}`), nil, int64(21), int64(5678), nil, nil, nil,
			))

	svc := NewAccountAllocationService(db, nil)
	overview, err := svc.ListUserVisibleAccounts(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, overview.Items, 3)

	dedicated := overview.Items[0]
	require.Equal(t, AccountAllocationVisibleSourceDedicated, dedicated.Source)
	require.Equal(t, "p***e@example.com", dedicated.AccountName)
	require.True(t, dedicated.AccountNameMasked)
	require.Equal(t, AccountAllocationVisibleUsageScopePersonalLease, dedicated.Usage.Scope)
	require.NotNil(t, dedicated.AssignedAt)
	require.NotNil(t, dedicated.LastActivityAt)
	require.Equal(t, lastActivityAt, *dedicated.LastActivityAt)
	require.NotNil(t, dedicated.Usage.AccountCost)
	require.InDelta(t, 2.09, *dedicated.Usage.AccountCost, 0.00001)
	require.NotNil(t, dedicated.Usage.UserCost)
	require.InDelta(t, 1.69, *dedicated.Usage.UserCost, 0.00001)
	require.NotNil(t, dedicated.UpstreamQuota)
	require.NotNil(t, dedicated.UpstreamQuota.FiveHour)
	require.InDelta(t, 35, dedicated.UpstreamQuota.FiveHour.Utilization, 0.00001)
	require.NotNil(t, dedicated.UpstreamQuota.SevenDay)
	require.Equal(t, quotaSnapshotAt, *dedicated.UpstreamQuota.UpdatedAt)
	require.Equal(t, "ready", dedicated.Status)
	require.NotContains(t, dedicated.ViewKey, "4")

	subscribed := overview.Items[1]
	require.Equal(t, AccountAllocationVisibleSourceDedicated, subscribed.Source)
	require.Equal(t, "s***r@example.com", subscribed.AccountName)
	require.True(t, subscribed.AccountNameMasked)
	require.Equal(t, AccountAllocationVisibleUsageScopeRolling24h, subscribed.Usage.Scope)
	require.Nil(t, subscribed.AssignedAt)
	require.Nil(t, subscribed.Usage.AccountCost)
	require.Nil(t, subscribed.Usage.UserCost)
	require.Nil(t, subscribed.UpstreamQuota)

	public := overview.Items[2]
	require.Equal(t, AccountAllocationVisibleSourcePublic, public.Source)
	require.Equal(t, "a***e@example.com", public.AccountName)
	require.True(t, public.AccountNameMasked)
	require.Equal(t, AccountAllocationVisibleUsageScopeRolling24h, public.Usage.Scope)
	require.Nil(t, public.AssignedAt)
	require.Nil(t, public.LastActivityAt)
	require.Nil(t, public.Usage.AccountCost)
	require.Nil(t, public.Usage.UserCost)
	require.Nil(t, public.UpstreamQuota)
	require.Equal(t, "cooling", public.Status)
	require.NotNil(t, public.RateLimitResetAt)

	require.Equal(t, 2, overview.Summary.DedicatedGroupCount)
	require.Equal(t, 1, overview.Summary.PublicGroupCount)
	require.Equal(t, 2, overview.Summary.DedicatedAccountCount)
	require.Equal(t, 1, overview.Summary.PublicAccountCount)
	require.Equal(t, 2, overview.Summary.ReadyAccountCount)
	payload, err := json.Marshal(overview)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "private@example.com")
	require.NotContains(t, string(payload), "\\\"account_id\\\"")
	require.NotContains(t, string(payload), "codex_5h_used_percent")
	require.NotContains(t, string(payload), "must-not-leak")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountAllocationVisibleUpstreamQuotaUsesCachedSnapshotsOnly(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	fiveHourReset := now.Add(2 * time.Hour)
	sevenDayReset := now.Add(5 * 24 * time.Hour)

	openAIExtra, err := json.Marshal(map[string]any{
		"codex_5h_used_percent":  48.0,
		"codex_5h_reset_at":      fiveHourReset.Format(time.RFC3339),
		"codex_7d_used_percent":  18.0,
		"codex_7d_reset_at":      sevenDayReset.Format(time.RFC3339),
		"codex_usage_updated_at": now.Add(-time.Minute).Format(time.RFC3339),
	})
	require.NoError(t, err)
	openAIQuota := accountAllocationVisibleUpstreamQuota(AccountAllocationVisibleSourceDedicated, PlatformOpenAI, AccountTypeOAuth, nil, openAIExtra, now)
	require.NotNil(t, openAIQuota)
	require.NotNil(t, openAIQuota.FiveHour)
	require.NotNil(t, openAIQuota.SevenDay)
	require.InDelta(t, 48, openAIQuota.FiveHour.Utilization, 0.00001)

	anthropicExtra, err := json.Marshal(map[string]any{
		"session_window_utilization":   0.42,
		"passive_usage_7d_utilization": 0.17,
		"passive_usage_7d_reset":       sevenDayReset.Unix(),
		"passive_usage_sampled_at":     now.Add(-2 * time.Minute).Format(time.RFC3339),
	})
	require.NoError(t, err)
	anthropicQuota := accountAllocationVisibleUpstreamQuota(AccountAllocationVisibleSourceDedicated, PlatformAnthropic, AccountTypeOAuth, &fiveHourReset, anthropicExtra, now)
	require.NotNil(t, anthropicQuota)
	require.NotNil(t, anthropicQuota.FiveHour)
	require.NotNil(t, anthropicQuota.SevenDay)
	require.InDelta(t, 42, anthropicQuota.FiveHour.Utilization, 0.00001)
	require.InDelta(t, 17, anthropicQuota.SevenDay.Utilization, 0.00001)

	require.Nil(t, accountAllocationVisibleUpstreamQuota(AccountAllocationVisibleSourceDedicated, PlatformGemini, AccountTypeOAuth, nil, openAIExtra, now))
	require.Nil(t, accountAllocationVisibleUpstreamQuota(AccountAllocationVisibleSourcePublic, PlatformOpenAI, AccountTypeOAuth, nil, openAIExtra, now))
}

func TestMaskAccountAllocationDisplayName(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		want       string
		wantMasked bool
	}{
		{name: "email", input: "alice@example.com", want: "a***e@example.com", wantMasked: true},
		{name: "short local part", input: "ab@example.com", want: "a***@example.com", wantMasked: true},
		{name: "plain label", input: "OpenAI Team 01", want: "OpenAI Team 01", wantMasked: false},
		{name: "multiple at markers", input: "name@one@two", want: "name@one@two", wantMasked: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, masked := maskAccountAllocationDisplayName(tc.input)
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.wantMasked, masked)
		})
	}
}

func TestAccountAllocationVisibleStatusPrioritizesUnavailableState(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Minute)
	past := now.Add(-time.Minute)

	require.Equal(t, "ready", accountAllocationVisibleStatus(StatusActive, true, nil, nil, true, nil, nil, now))
	require.Equal(t, "cooling", accountAllocationVisibleStatus(StatusActive, true, &future, nil, true, nil, nil, now))
	require.Equal(t, "unavailable", accountAllocationVisibleStatus(StatusDisabled, true, &future, nil, true, nil, nil, now))
	require.Equal(t, "unavailable", accountAllocationVisibleStatus(StatusActive, true, nil, &past, true, nil, nil, now))
	require.Equal(t, "unavailable", accountAllocationVisibleStatus(StatusActive, true, nil, nil, true, &future, nil, now))
}

func TestAccountAllocationAssignmentScannerSupportsDeletedAccountHistory(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	assignedAt := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	releasedAt := assignedAt.Add(time.Minute)
	mock.ExpectQuery("SELECT").
		WithArgs(int64(91)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "policy_id", "user_id", "group_id", "account_id", "account_name", "platform", "account_type", "concurrency",
			"account_status", "schedulable", "rate_limit_reset_at", "status", "assigned_by", "assigned_at", "released_at", "release_reason", "last_reconciled_at",
		}).AddRow(
			int64(91), int64(12), int64(7), int64(4), nil, "", "", "", 0,
			"removed", false, nil, "released", nil, assignedAt, releasedAt, "account_removed", nil,
		))

	row := db.QueryRowContext(context.Background(), "SELECT", int64(91))
	assignment, err := scanAccountAllocationAssignment(row)
	require.NoError(t, err)
	require.Zero(t, assignment.AccountID)
	require.Equal(t, "removed", assignment.AccountStatus)
	require.Equal(t, "released", assignment.Status)
	require.NotNil(t, assignment.ReleasedAt)
	require.WithinDuration(t, releasedAt, *assignment.ReleasedAt, time.Second)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEnsureActiveAccountAllocationReferencesRejectsDeletedUser(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND deleted_at IS NULL AND status = 'active')")).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	err = ensureActiveAccountAllocationReferences(context.Background(), db, 7, 4)
	require.Error(t, err)
	require.Contains(t, err.Error(), "active target user not found")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEnsureActiveAccountAllocationReferencesRejectsInactiveGroup(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND deleted_at IS NULL AND status = 'active')")).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM groups WHERE id = $1 AND deleted_at IS NULL AND status = 'active')")).
		WithArgs(int64(4)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	err = ensureActiveAccountAllocationReferences(context.Background(), db, 7, 4)
	require.Error(t, err)
	require.Contains(t, err.Error(), "active target group not found")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountAllocationEnableRejectsSoftDeletedReferenceBeforePolicyUpdate(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, user_id, group_id, desired_count, auto_replenish, replace_on_401, replace_on_429, status FROM account_allocation_policies WHERE id = $1 AND deleted_at IS NULL FOR UPDATE")).
		WithArgs(int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "group_id", "desired_count", "auto_replenish", "replace_on_401", "replace_on_429", "status",
		}).AddRow(int64(9), int64(7), int64(4), 2, true, true, true, accountAllocationPolicyDisabled))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND deleted_at IS NULL AND status = 'active')")).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	svc := NewAccountAllocationService(db, nil)
	_, err = svc.SetPolicyStatus(context.Background(), 9, true, 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "active target user not found")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountAllocationDeletePolicyReleasesLeasesAndKeepsAuditHistory(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, user_id, group_id, desired_count").
		WithArgs(int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "group_id", "desired_count", "auto_replenish", "replace_on_401", "replace_on_429", "status",
		}).AddRow(int64(9), int64(7), int64(4), 2, true, true, true, accountAllocationPolicyActive))
	mock.ExpectQuery("UPDATE account_allocation_assignments").
		WithArgs(int64(9), accountAllocationAssignmentGone, "policy_deleted", accountAllocationAssignmentLive).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(31)))
	mock.ExpectExec("INSERT INTO account_allocation_events").
		WithArgs(int64(9), int64(31), "assignment_released", int64(1), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE account_allocation_policies").
		WithArgs(int64(9), accountAllocationPolicyDisabled).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO account_allocation_events").
		WithArgs(int64(9), nil, "policy_deleted", int64(1), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(2, 1))
	mock.ExpectCommit()

	svc := NewAccountAllocationService(db, nil)
	require.NoError(t, svc.DeletePolicy(context.Background(), 9, 1))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountAllocationCreatePolicyValidatesReferencesInsideWriteTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND deleted_at IS NULL AND status = 'active')")).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	svc := NewAccountAllocationService(db, nil)
	_, err = svc.CreatePolicy(context.Background(), AccountAllocationPolicyInput{
		UserID:  7,
		GroupID: 4,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "active target user not found")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountAllocationCapabilitiesReflectServerConfiguration(t *testing.T) {
	svc := NewAccountAllocationService(nil, &config.Config{AccountAllocation: config.AccountAllocationConfig{
		MaxDesiredCount:          17,
		ReconcileIntervalSeconds: 23,
	}})

	require.Equal(t, AccountAllocationCapabilities{
		MaxDesiredCount:          17,
		ReconcileIntervalSeconds: 23,
	}, svc.Capabilities())
}

func TestAccountAllocationExplicitReconcileAllIsNotLimitedToWorkerBatchSize(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`(?s)SELECT id.*ORDER BY last_reconciled_at NULLS FIRST, id ASC\s*$`).
		WithArgs(accountAllocationPolicyActive, accountAllocationAssignmentLive).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	svc := NewAccountAllocationService(db, &config.Config{AccountAllocation: config.AccountAllocationConfig{
		PolicyBatchSize: 1,
	}})
	results, err := svc.ReconcileAll(context.Background())
	require.NoError(t, err)
	require.Empty(t, results)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountAllocationBackgroundReconcileUsesConfiguredBatchSize(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`(?s)SELECT id.*ORDER BY last_reconciled_at NULLS FIRST, id ASC LIMIT \$3`).
		WithArgs(accountAllocationPolicyActive, accountAllocationAssignmentLive, 7).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	svc := NewAccountAllocationService(db, &config.Config{AccountAllocation: config.AccountAllocationConfig{
		PolicyBatchSize: 7,
	}})
	results, err := svc.reconcileBatch(context.Background())
	require.NoError(t, err)
	require.Empty(t, results)
	require.NoError(t, mock.ExpectationsWereMet())
}
