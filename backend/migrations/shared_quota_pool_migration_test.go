package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSharedQuotaPoolMigrationsDefineIndependentWindows(t *testing.T) {
	base, err := FS.ReadFile("308_shared_quota_pool.sql")
	require.NoError(t, err)
	windows, err := FS.ReadFile("310_shared_quota_pool_windows.sql")
	require.NoError(t, err)
	official, err := FS.ReadFile("311_shared_quota_official_percent.sql")
	require.NoError(t, err)
	analytics, err := FS.ReadFile("312_shared_quota_official_analytics.sql")
	require.NoError(t, err)

	baseSQL := strings.ToLower(string(base))
	windowSQL := strings.ToLower(string(windows))
	officialSQL := strings.ToLower(string(official))
	analyticsSQL := strings.ToLower(string(analytics))
	for _, required := range []string{
		"create table if not exists shared_quota_pools",
		"create table if not exists shared_quota_pool_members",
		"group_id                    bigint primary key",
	} {
		require.Contains(t, baseSQL, required)
	}
	for _, required := range []string{
		"create table if not exists shared_quota_pool_windows",
		"primary key (group_id, window_key)",
		"window_key ~ '^[a-z][a-z0-9_-]{0,31}$'",
		"group_id, 'long', enabled",
		"group_id, 'short', false",
	} {
		require.Contains(t, windowSQL, required)
	}
	for _, required := range []string{
		"add column if not exists capacity_mode",
		"add column if not exists upstream_account_id",
		"create table if not exists shared_quota_pool_official_snapshots",
		"primary key (group_id, window_key)",
		"used_percent >= 0 and used_percent <= 100",
	} {
		require.Contains(t, officialSQL, required)
	}
	for _, required := range []string{
		"analytics_used_credits",
		"analytics_credits_per_usd",
		"baseline_used_credits",
		"baseline_captured_at",
	} {
		require.Contains(t, analyticsSQL, required)
	}
}
