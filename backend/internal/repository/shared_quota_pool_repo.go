package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type sharedQuotaPoolRepository struct {
	db *sql.DB
}

func NewSharedQuotaPoolRepository(db *sql.DB) service.SharedQuotaPoolRepository {
	return &sharedQuotaPoolRepository{db: db}
}

func (r *sharedQuotaPoolRepository) GetConfig(ctx context.Context, groupID int64) (*service.SharedQuotaPoolConfig, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	if err := r.advanceWindows(ctx, groupID, time.Now()); err != nil {
		return nil, err
	}
	const query = `
		SELECT group_id, enabled, window_seconds, capacity_usd,
		       reserve_ratio, soft_stop_ratio, hard_stop_ratio,
		       borrow_enabled, borrow_multiplier, upstream_capacity_usd,
		       upstream_utilization_percent, window_start, window_end, updated_at,
		       capacity_mode, upstream_account_id
		FROM shared_quota_pools
		WHERE group_id = $1`

	var config service.SharedQuotaPoolConfig
	var capacity, upstreamCapacity, upstreamUtilization sql.NullFloat64
	var capacityMode sql.NullString
	var upstreamAccountID sql.NullInt64
	if err := r.db.QueryRowContext(ctx, query, groupID).Scan(
		&config.GroupID,
		&config.Enabled,
		&config.WindowSeconds,
		&capacity,
		&config.ReserveRatio,
		&config.SoftStopRatio,
		&config.HardStopRatio,
		&config.BorrowEnabled,
		&config.BorrowMultiplier,
		&upstreamCapacity,
		&upstreamUtilization,
		&config.WindowStart,
		&config.WindowEnd,
		&config.UpdatedAt,
		&capacityMode,
		&upstreamAccountID,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if capacity.Valid {
		config.CapacityUSD = &capacity.Float64
	}
	if upstreamCapacity.Valid {
		config.UpstreamCapacityUSD = &upstreamCapacity.Float64
	}
	if upstreamUtilization.Valid {
		config.UpstreamUtilizationPercent = &upstreamUtilization.Float64
	}
	if capacityMode.Valid {
		config.CapacityMode = capacityMode.String
	}
	if upstreamAccountID.Valid {
		config.UpstreamAccountID = &upstreamAccountID.Int64
	}
	windows, err := r.getWindows(ctx, groupID)
	if err != nil {
		return nil, err
	}
	config.Windows = windows
	if len(config.Windows) == 0 && config.Enabled {
		config.Windows = []service.SharedQuotaPoolWindowConfig{legacyWindowFromConfig(config)}
	}
	for _, window := range config.Windows {
		if window.Key != "long" {
			continue
		}
		config.WindowSeconds = window.WindowSeconds
		config.CapacityUSD = window.CapacityUSD
		config.ReserveRatio = window.ReserveRatio
		config.SoftStopRatio = window.SoftStopRatio
		config.HardStopRatio = window.HardStopRatio
		config.UpstreamCapacityUSD = window.UpstreamCapacityUSD
		config.UpstreamUtilizationPercent = window.UpstreamUtilizationPercent
		config.CapacityMode = window.CapacityMode
		config.UpstreamAccountID = window.UpstreamAccountID
		config.WindowStart = window.WindowStart
		config.WindowEnd = window.WindowEnd
		break
	}
	return &config, nil
}

func (r *sharedQuotaPoolRepository) SaveConfigAndWindowsAndMembers(ctx context.Context, config *service.SharedQuotaPoolConfig, windows []service.SharedQuotaPoolWindowConfig, members []service.SharedQuotaPoolMemberInput) error {
	if r == nil || r.db == nil {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	const upsertConfig = `
		INSERT INTO shared_quota_pools (
			group_id, enabled, window_seconds, capacity_usd,
			reserve_ratio, soft_stop_ratio, hard_stop_ratio,
			borrow_enabled, borrow_multiplier, upstream_capacity_usd,
			upstream_utilization_percent, capacity_mode, upstream_account_id
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (group_id) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			window_seconds = EXCLUDED.window_seconds,
			capacity_usd = EXCLUDED.capacity_usd,
			reserve_ratio = EXCLUDED.reserve_ratio,
			soft_stop_ratio = EXCLUDED.soft_stop_ratio,
			hard_stop_ratio = EXCLUDED.hard_stop_ratio,
			borrow_enabled = EXCLUDED.borrow_enabled,
			borrow_multiplier = EXCLUDED.borrow_multiplier,
			upstream_capacity_usd = EXCLUDED.upstream_capacity_usd,
			upstream_utilization_percent = EXCLUDED.upstream_utilization_percent,
			capacity_mode = EXCLUDED.capacity_mode,
			upstream_account_id = EXCLUDED.upstream_account_id,
			updated_at = NOW()`
	legacyWindow := legacyWindowFromConfig(*config)
	if len(windows) > 0 {
		for _, window := range windows {
			if window.Key == "long" {
				legacyWindow = window
				break
			}
		}
	}
	if _, err := tx.ExecContext(ctx, upsertConfig,
		config.GroupID,
		config.Enabled,
		legacyWindow.WindowSeconds,
		nilFloat(legacyWindow.CapacityUSD),
		legacyWindow.ReserveRatio,
		legacyWindow.SoftStopRatio,
		legacyWindow.HardStopRatio,
		config.BorrowEnabled,
		config.BorrowMultiplier,
		nilFloat(legacyWindow.UpstreamCapacityUSD),
		nilFloat(legacyWindow.UpstreamUtilizationPercent),
		capacityModeOrDefault(legacyWindow.CapacityMode),
		nilInt64(legacyWindow.UpstreamAccountID),
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM shared_quota_pool_windows WHERE group_id = $1`, config.GroupID); err != nil {
		return err
	}
	for _, window := range windows {
		windowStart := window.WindowStart
		if windowStart.IsZero() {
			windowStart = time.Now()
		}
		windowEnd := window.WindowEnd
		if windowEnd.IsZero() {
			windowEnd = windowStart.Add(time.Duration(window.WindowSeconds) * time.Second)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO shared_quota_pool_windows (
				group_id, window_key, enabled, window_seconds, capacity_usd,
				reserve_ratio, soft_stop_ratio, hard_stop_ratio,
				upstream_capacity_usd, upstream_utilization_percent,
				window_start, window_end, capacity_mode, upstream_account_id
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
			ON CONFLICT (group_id, window_key) DO UPDATE SET
				enabled = EXCLUDED.enabled,
				window_seconds = EXCLUDED.window_seconds,
				capacity_usd = EXCLUDED.capacity_usd,
				reserve_ratio = EXCLUDED.reserve_ratio,
				soft_stop_ratio = EXCLUDED.soft_stop_ratio,
				hard_stop_ratio = EXCLUDED.hard_stop_ratio,
				upstream_capacity_usd = EXCLUDED.upstream_capacity_usd,
				upstream_utilization_percent = EXCLUDED.upstream_utilization_percent,
				window_start = EXCLUDED.window_start,
				window_end = EXCLUDED.window_end,
				capacity_mode = EXCLUDED.capacity_mode,
				upstream_account_id = EXCLUDED.upstream_account_id,
				updated_at = NOW()`,
			config.GroupID, window.Key, window.Enabled, window.WindowSeconds,
			nilFloat(window.CapacityUSD), window.ReserveRatio, window.SoftStopRatio,
			window.HardStopRatio, nilFloat(window.UpstreamCapacityUSD),
			nilFloat(window.UpstreamUtilizationPercent), windowStart, windowEnd,
			capacityModeOrDefault(window.CapacityMode),
			nilInt64(window.UpstreamAccountID)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM shared_quota_pool_members WHERE group_id = $1`, config.GroupID); err != nil {
		return err
	}

	for _, member := range members {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO shared_quota_pool_members (group_id, user_id, weight, enabled)
			VALUES ($1,$2,$3,$4)
			ON CONFLICT (group_id, user_id) DO UPDATE SET
				weight = EXCLUDED.weight,
				enabled = EXCLUDED.enabled,
				updated_at = NOW()`,
			config.GroupID, member.UserID, member.Weight, member.Enabled); err != nil {
			return err
		}
	}
	for _, window := range windows {
		if err := advanceWindowTx(ctx, tx, config.GroupID, window.Key, time.Now()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *sharedQuotaPoolRepository) UpsertMember(ctx context.Context, groupID, userID int64, weight float64, enabled bool) error {
	if r == nil || r.db == nil {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO shared_quota_pool_members (group_id, user_id, weight, enabled)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (group_id, user_id) DO UPDATE SET
			weight = EXCLUDED.weight,
			enabled = EXCLUDED.enabled,
			updated_at = NOW()`, groupID, userID, weight, enabled)
	return err
}

func (r *sharedQuotaPoolRepository) DeleteMember(ctx context.Context, groupID, userID int64) error {
	if r == nil || r.db == nil {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM shared_quota_pool_members WHERE group_id = $1 AND user_id = $2`, groupID, userID)
	return err
}

func (r *sharedQuotaPoolRepository) ListActiveMembers(ctx context.Context, groupID int64, now time.Time) ([]service.SharedQuotaPoolMember, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	const query = `
		SELECT DISTINCT ON (us.user_id)
			us.user_id, us.id, COALESCE(u.email, ''), COALESCE(u.username, ''),
			COALESCE(m.weight, 1), COALESCE(m.enabled, TRUE), (m.user_id IS NOT NULL)
		FROM user_subscriptions us
		JOIN users u ON u.id = us.user_id AND u.deleted_at IS NULL
		LEFT JOIN shared_quota_pool_members m
			ON m.group_id = us.group_id AND m.user_id = us.user_id
		WHERE us.group_id = $1
		  AND us.status = 'active'
		  AND us.expires_at > $2
		  AND us.deleted_at IS NULL
		ORDER BY us.user_id, us.expires_at DESC, us.id DESC`
	rows, err := r.db.QueryContext(ctx, query, groupID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := make([]service.SharedQuotaPoolMember, 0)
	for rows.Next() {
		var member service.SharedQuotaPoolMember
		if err := rows.Scan(&member.UserID, &member.SubscriptionID, &member.Email, &member.Username, &member.Weight, &member.Enabled, &member.Configured); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func (r *sharedQuotaPoolRepository) GetUsage(ctx context.Context, groupID int64, windowStart, windowEnd time.Time) (float64, map[int64]float64, error) {
	if r == nil || r.db == nil {
		return 0, map[int64]float64{}, nil
	}
	const query = `
		SELECT user_id, COALESCE(SUM(actual_cost), 0)
		FROM usage_logs
		WHERE group_id = $1
		  AND subscription_id IS NOT NULL
		  AND created_at >= $2
		  AND created_at < $3
		GROUP BY user_id`
	rows, err := r.db.QueryContext(ctx, query, groupID, windowStart, windowEnd)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	usageByUser := make(map[int64]float64)
	var total float64
	for rows.Next() {
		var userID int64
		var usage float64
		if err := rows.Scan(&userID, &usage); err != nil {
			return 0, nil, err
		}
		usageByUser[userID] = usage
		total += usage
	}
	return total, usageByUser, rows.Err()
}

func (r *sharedQuotaPoolRepository) GetOfficialQuotaSnapshot(ctx context.Context, groupID int64, windowKey string) (*service.SharedQuotaOfficialSnapshot, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var snapshot service.SharedQuotaOfficialSnapshot
	var resetAt sql.NullTime
	err := r.db.QueryRowContext(ctx, `
		SELECT account_id, used_percent, limit_window_seconds, reset_at, fetched_at
		FROM shared_quota_pool_official_snapshots
		WHERE group_id = $1 AND window_key = $2`, groupID, windowKey).Scan(
		&snapshot.AccountID, &snapshot.UsedPercent, &snapshot.LimitWindowSeconds, &resetAt, &snapshot.FetchedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if resetAt.Valid {
		snapshot.ResetAt = resetAt.Time
	}
	return &snapshot, nil
}

func (r *sharedQuotaPoolRepository) SaveOfficialQuotaSnapshot(ctx context.Context, groupID int64, windowKey string, snapshot *service.SharedQuotaOfficialSnapshot) error {
	if r == nil || r.db == nil || snapshot == nil {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO shared_quota_pool_official_snapshots (
			group_id, window_key, account_id, used_percent, limit_window_seconds,
			reset_at, fetched_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (group_id, window_key) DO UPDATE SET
			account_id = EXCLUDED.account_id,
			used_percent = EXCLUDED.used_percent,
			limit_window_seconds = EXCLUDED.limit_window_seconds,
			reset_at = EXCLUDED.reset_at,
			fetched_at = EXCLUDED.fetched_at,
			updated_at = NOW()`, groupID, windowKey, snapshot.AccountID,
		snapshot.UsedPercent, snapshot.LimitWindowSeconds, timeOrNil(snapshot.ResetAt), snapshot.FetchedAt)
	return err
}

func (r *sharedQuotaPoolRepository) getWindows(ctx context.Context, groupID int64) ([]service.SharedQuotaPoolWindowConfig, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT window_key, enabled, window_seconds, capacity_usd,
		       reserve_ratio, soft_stop_ratio, hard_stop_ratio,
		       upstream_capacity_usd, upstream_utilization_percent,
		       window_start, window_end, updated_at,
		       capacity_mode, upstream_account_id
		FROM shared_quota_pool_windows
		WHERE group_id = $1
		ORDER BY CASE window_key WHEN 'short' THEN 0 WHEN 'long' THEN 1 ELSE 2 END, window_key`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	windows := make([]service.SharedQuotaPoolWindowConfig, 0)
	for rows.Next() {
		var window service.SharedQuotaPoolWindowConfig
		var capacity, upstreamCapacity, upstreamUtilization sql.NullFloat64
		var capacityMode sql.NullString
		var upstreamAccountID sql.NullInt64
		if err := rows.Scan(&window.Key, &window.Enabled, &window.WindowSeconds, &capacity,
			&window.ReserveRatio, &window.SoftStopRatio, &window.HardStopRatio,
			&upstreamCapacity, &upstreamUtilization, &window.WindowStart,
			&window.WindowEnd, &window.UpdatedAt, &capacityMode, &upstreamAccountID); err != nil {
			return nil, err
		}
		if capacity.Valid {
			window.CapacityUSD = &capacity.Float64
		}
		if upstreamCapacity.Valid {
			window.UpstreamCapacityUSD = &upstreamCapacity.Float64
		}
		if upstreamUtilization.Valid {
			window.UpstreamUtilizationPercent = &upstreamUtilization.Float64
		}
		if capacityMode.Valid {
			window.CapacityMode = capacityMode.String
		}
		if upstreamAccountID.Valid {
			window.UpstreamAccountID = &upstreamAccountID.Int64
		}
		windows = append(windows, window)
	}
	return windows, rows.Err()
}

func legacyWindowFromConfig(config service.SharedQuotaPoolConfig) service.SharedQuotaPoolWindowConfig {
	return service.SharedQuotaPoolWindowConfig{
		Key: "long", Enabled: config.Enabled, WindowSeconds: config.WindowSeconds,
		CapacityUSD: config.CapacityUSD, ReserveRatio: config.ReserveRatio,
		SoftStopRatio: config.SoftStopRatio, HardStopRatio: config.HardStopRatio,
		UpstreamCapacityUSD:        config.UpstreamCapacityUSD,
		UpstreamUtilizationPercent: config.UpstreamUtilizationPercent,
		WindowStart:                config.WindowStart, WindowEnd: config.WindowEnd,
	}
}

func (r *sharedQuotaPoolRepository) advanceWindows(ctx context.Context, groupID int64, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE shared_quota_pool_windows
		SET window_start = window_start + make_interval(secs => (
				(FLOOR(EXTRACT(EPOCH FROM ($2 - window_end)) / window_seconds) + 1)
				* window_seconds
			)::double precision),
			window_end = window_end + make_interval(secs => (
				(FLOOR(EXTRACT(EPOCH FROM ($2 - window_end)) / window_seconds) + 1)
				* window_seconds
			)::double precision),
			updated_at = NOW()
		WHERE group_id = $1 AND window_end <= $2`, groupID, now)
	return err
}

func advanceWindowTx(ctx context.Context, tx *sql.Tx, groupID int64, windowKey string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE shared_quota_pool_windows
		SET window_start = window_start + make_interval(secs => (
				(FLOOR(EXTRACT(EPOCH FROM ($3 - window_end)) / window_seconds) + 1)
				* window_seconds
			)::double precision),
			window_end = window_end + make_interval(secs => (
				(FLOOR(EXTRACT(EPOCH FROM ($3 - window_end)) / window_seconds) + 1)
				* window_seconds
			)::double precision),
			updated_at = NOW()
		WHERE group_id = $1 AND window_key = $2 AND window_end <= $3`, groupID, windowKey, now)
	return err
}

func nilFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nilInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func capacityModeOrDefault(value string) string {
	if value == "" {
		return service.SharedQuotaCapacityModeUSD
	}
	return value
}
