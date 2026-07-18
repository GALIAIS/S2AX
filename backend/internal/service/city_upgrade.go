package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	CityUpgradeStatusRunning = "running"
	CityUpgradeStatusPlanned = "planned"
	CityUpgradeStatusApplied = "applied"
	CityUpgradeStatusFailed  = "failed"
)

var (
	ErrCityUpgradeNotFound = infraerrors.NotFound("CITY_ENGINE_UPGRADE_NOT_FOUND", "city engine upgrade run not found")
	ErrCityUpgradeConflict = infraerrors.Conflict("CITY_ENGINE_UPGRADE_CONFLICT", "city engine upgrade idempotency key was reused with different intent")
	ErrCityUpgradePath     = infraerrors.Conflict("CITY_ENGINE_UPGRADE_PATH_UNAVAILABLE", "city engine upgrade path is unavailable")
	ErrCityUpgradeState    = infraerrors.Conflict("CITY_ENGINE_UPGRADE_STATE_INVALID", "city engine upgrade requires a paused and valid source world")
)

type CityEngineInfo struct {
	Version             string     `json:"version"`
	CurrentVersion      string     `json:"current_version"`
	WorldStatus         string     `json:"world_status"`
	CurrentTick         int64      `json:"current_tick"`
	StateHash           *string    `json:"state_hash,omitempty"`
	Writable            bool       `json:"writable"`
	Stages              []string   `json:"stages"`
	UpgradeTargets      []string   `json:"upgrade_targets"`
	PendingCommandCount int64      `json:"pending_command_count"`
	SnapshotCount       int64      `json:"snapshot_count"`
	SnapshotBytes       int64      `json:"snapshot_bytes"`
	LastTickDurationMS  *int64     `json:"last_tick_duration_ms,omitempty"`
	LastTickCompletedAt *time.Time `json:"last_tick_completed_at,omitempty"`
	FailedTickCount     int64      `json:"failed_tick_count"`
	LastFailureCode     *string    `json:"last_failure_code,omitempty"`
	LastFailureAt       *time.Time `json:"last_failure_at,omitempty"`
}

type CityUpgradeRun struct {
	ID                 int64      `json:"id"`
	WorldID            int64      `json:"world_id"`
	RequestedByUserID  int64      `json:"requested_by_user_id"`
	ClientRequestID    string     `json:"client_request_id"`
	FromVersion        string     `json:"from_version"`
	ToVersion          string     `json:"to_version"`
	FromTick           int64      `json:"from_tick"`
	DryRun             bool       `json:"dry_run"`
	Status             string     `json:"status"`
	SourceSnapshotID   int64      `json:"source_snapshot_id"`
	TargetSnapshotID   *int64     `json:"target_snapshot_id,omitempty"`
	BeforeStateHash    string     `json:"before_state_hash"`
	AfterStateHash     *string    `json:"after_state_hash,omitempty"`
	ErrorCode          *string    `json:"error_code,omitempty"`
	ErrorDetail        *string    `json:"error_detail,omitempty"`
	StartedAt          time.Time  `json:"started_at"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
	requestFingerprint string
}

type CityUpgradeInput struct {
	UserID         int64
	WorldID        int64
	IdempotencyKey string
	TargetVersion  string
	DryRun         bool
}

type CityUpgradeRunPage struct {
	Items      []*CityUpgradeRun `json:"items"`
	NextCursor *int64            `json:"next_cursor,omitempty"`
}

const cityUpgradeRunColumns = `
u.id, u.world_id, u.requested_by_user_id, u.client_request_id,
u.request_fingerprint, u.from_version, u.to_version, u.from_tick,
u.dry_run, u.status, u.source_snapshot_id, u.target_snapshot_id,
u.before_state_hash, u.after_state_hash, u.error_code, u.error_detail,
u.started_at, u.completed_at`

func (s *CityEconomyService) GetEngineInfo(ctx context.Context, userID, worldID int64) (*CityEngineInfo, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	var version, status string
	var tick, pendingCommands, snapshotCount, snapshotBytes, failedTickCount int64
	var stateHash, lastFailureCode sql.NullString
	var lastTickDuration sql.NullInt64
	var lastTickCompleted, lastFailureAt sql.NullTime
	if err := s.db.QueryRowContext(ctx, `
SELECT world.simulation_version, world.status, world.current_tick, world.state_hash,
       (SELECT COUNT(*) FROM city_commands command
        WHERE command.world_id = world.id AND command.status = 'pending'),
       (SELECT COUNT(*) FROM city_snapshots snapshot
        WHERE snapshot.world_id = world.id AND snapshot.simulation_version = world.simulation_version),
       (SELECT COALESCE(SUM(snapshot.compressed_size), 0) FROM city_snapshots snapshot
        WHERE snapshot.world_id = world.id AND snapshot.simulation_version = world.simulation_version),
       latest.duration_ms, latest.completed_at,
       (SELECT COUNT(*) FROM city_tick_failures failure WHERE failure.world_id = world.id),
       latest_failure.error_code, latest_failure.failed_at
FROM city_worlds world
LEFT JOIN LATERAL (
    SELECT tick.duration_ms, tick.completed_at
    FROM city_ticks tick WHERE tick.world_id = world.id
    ORDER BY tick.tick DESC LIMIT 1
) latest ON TRUE
LEFT JOIN LATERAL (
    SELECT failure.error_code, failure.failed_at
    FROM city_tick_failures failure WHERE failure.world_id = world.id
    ORDER BY failure.id DESC LIMIT 1
) latest_failure ON TRUE
WHERE world.id = $1`, worldID).
		Scan(&version, &status, &tick, &stateHash, &pendingCommands,
			&snapshotCount, &snapshotBytes, &lastTickDuration, &lastTickCompleted,
			&failedTickCount, &lastFailureCode, &lastFailureAt); err != nil {
		return nil, fmt.Errorf("load city engine info: %w", err)
	}
	engine, err := cityEngineForVersion(version)
	if err != nil {
		return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": version})
	}
	stages := make([]string, len(engine.stages))
	for index, stage := range engine.stages {
		stages[index] = string(stage)
	}
	return &CityEngineInfo{
		Version: version, CurrentVersion: CurrentCitySimulationVersion,
		WorldStatus: status, CurrentTick: tick,
		Writable: status == CityWorldStatusPaused || status == CityWorldStatusRunning,
		Stages:   stages, UpgradeTargets: cityEngineUpgradeTargets(version),
		StateHash: nullStringPointer(stateHash), PendingCommandCount: pendingCommands,
		SnapshotCount: snapshotCount, SnapshotBytes: snapshotBytes,
		LastTickDurationMS: nullInt64Pointer(lastTickDuration), LastTickCompletedAt: nullTimePointer(lastTickCompleted),
		FailedTickCount: failedTickCount, LastFailureCode: nullStringPointer(lastFailureCode),
		LastFailureAt: nullTimePointer(lastFailureAt),
	}, nil
}

func (s *CityEconomyService) StartUpgrade(ctx context.Context, input CityUpgradeInput) (*CityUpgradeRun, error) {
	targetVersion := strings.TrimSpace(input.TargetVersion)
	if input.UserID <= 0 || input.WorldID <= 0 || targetVersion == "" {
		return nil, ErrCityInvalidInput
	}
	if _, err := cityEngineForVersion(targetVersion); err != nil {
		return nil, ErrCityUpgradePath.WithMetadata(map[string]string{"target_version": targetVersion})
	}
	requestID, err := requireCityIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	fingerprint, err := cityUpgradeFingerprint(targetVersion, input.DryRun)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin city engine upgrade transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, cityWorldLockKey(input.WorldID)); err != nil {
		return nil, fmt.Errorf("lock city engine upgrade world: %w", err)
	}
	world, err := lockCityWorld(ctx, tx, input.UserID, input.WorldID)
	if err != nil {
		return nil, err
	}
	if world.memberRole != CityMemberRoleOwner {
		return nil, ErrCityPermissionDenied
	}
	existing, err := loadCityUpgradeByRequest(ctx, tx, input.WorldID, input.UserID, requestID)
	if err == nil {
		if existing.requestFingerprint != fingerprint {
			return nil, ErrCityUpgradeConflict
		}
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit city engine upgrade idempotent read: %w", err)
		}
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("find city engine upgrade request: %w", err)
	}
	if world.status != CityWorldStatusPaused || !cityEngineCanUpgrade(world.simulationVersion, targetVersion) {
		return nil, ErrCityUpgradeState.WithMetadata(map[string]string{
			"status": world.status, "from_version": world.simulationVersion, "to_version": targetVersion,
		})
	}
	var pendingCommandCount int64
	if err = tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_commands WHERE world_id = $1 AND status = 'pending'`, input.WorldID).
		Scan(&pendingCommandCount); err != nil {
		return nil, fmt.Errorf("count pending commands before city engine upgrade: %w", err)
	}
	if pendingCommandCount != 0 {
		return nil, ErrCityUpgradeState.WithMetadata(map[string]string{
			"pending_command_count": strconv.FormatInt(pendingCommandCount, 10),
		})
	}
	world.stateHash, err = ensureCityBaselineSnapshot(ctx, tx, input.WorldID, world)
	if err != nil {
		return nil, err
	}
	sourceSnapshot, err := loadCitySnapshotByTick(ctx, tx, input.WorldID, world.currentTick)
	if err != nil {
		return nil, fmt.Errorf("load city engine upgrade source snapshot: %w", err)
	}
	if world.stateHash == nil || sourceSnapshot.StateHash != *world.stateHash {
		return nil, ErrCitySnapshotIntegrity.WithMetadata(map[string]string{"field": "upgrade_source"})
	}
	run, err := scanCityUpgradeRun(tx.QueryRowContext(ctx, `
INSERT INTO city_world_upgrade_runs AS u
    (world_id, requested_by_user_id, client_request_id, request_fingerprint,
     from_version, to_version, from_tick, dry_run, source_snapshot_id, before_state_hash)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING `+cityUpgradeRunColumns,
		input.WorldID, input.UserID, requestID, fingerprint,
		world.simulationVersion, targetVersion, world.currentTick, input.DryRun,
		sourceSnapshot.ID, sourceSnapshot.StateHash))
	if err != nil {
		return nil, fmt.Errorf("insert city engine upgrade run: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `SAVEPOINT city_engine_upgrade`); err != nil {
		return nil, fmt.Errorf("create city engine upgrade savepoint: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `SELECT set_config('sub2api.city_upgrade_run_id', $1, TRUE)`, strconv.FormatInt(run.ID, 10)); err != nil {
		return nil, fmt.Errorf("activate city engine upgrade write gate: %w", err)
	}

	targetCanonical, targetHash, upgradeErr := applyCityEngineUpgrade(
		ctx, tx, input.WorldID, world.simulationVersion, targetVersion,
	)
	var targetSnapshot *CitySnapshot
	if upgradeErr == nil && !input.DryRun {
		targetSnapshot, upgradeErr = captureCitySnapshot(ctx, tx, citySnapshotCapture{
			worldID: input.WorldID, tick: world.currentTick,
			sourceTickID: sourceSnapshot.SourceTickID, simulationVersion: targetVersion,
			reason: CitySnapshotReasonBaseline, canonical: targetCanonical, stateHash: targetHash,
		})
	}
	if upgradeErr == nil {
		_, upgradeErr = tx.ExecContext(ctx, `SET CONSTRAINTS ALL IMMEDIATE`)
	}

	status := CityUpgradeStatusApplied
	var afterHash, errorCode, errorDetail *string
	var targetSnapshotID *int64
	if upgradeErr != nil {
		if _, rollbackErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT city_engine_upgrade`); rollbackErr != nil {
			return nil, fmt.Errorf("rollback failed city engine upgrade: %w", rollbackErr)
		}
		_, _ = tx.ExecContext(ctx, `SET CONSTRAINTS ALL DEFERRED`)
		status = CityUpgradeStatusFailed
		errorCode = stringPointer("CITY_ENGINE_UPGRADE_FAILED")
		errorDetail = stringPointer(cityAuditDetail(upgradeErr.Error()))
	} else if input.DryRun {
		if _, err = tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT city_engine_upgrade`); err != nil {
			return nil, fmt.Errorf("rollback city engine upgrade dry run: %w", err)
		}
		_, _ = tx.ExecContext(ctx, `SET CONSTRAINTS ALL DEFERRED`)
		status = CityUpgradeStatusPlanned
		afterHash = stringPointer(targetHash)
	} else {
		if _, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT city_engine_upgrade`); err != nil {
			return nil, fmt.Errorf("release city engine upgrade savepoint: %w", err)
		}
		afterHash = stringPointer(targetHash)
		targetSnapshotID = &targetSnapshot.ID
	}
	completedAt := time.Now().UTC()
	run, err = scanCityUpgradeRun(tx.QueryRowContext(ctx, `
UPDATE city_world_upgrade_runs AS u
SET status = $2, target_snapshot_id = $3, after_state_hash = $4,
    error_code = $5, error_detail = $6, completed_at = $7
WHERE u.id = $1 AND u.status = 'running'
RETURNING `+cityUpgradeRunColumns,
		run.ID, status, cityNullableInt64(targetSnapshotID), cityNullableString(afterHash),
		cityNullableString(errorCode), cityNullableString(errorDetail), completedAt))
	if err != nil {
		return nil, fmt.Errorf("complete city engine upgrade run: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city engine upgrade run: %w", err)
	}
	return run, nil
}

func applyCityEngineUpgrade(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	fromVersion, toVersion string,
) ([]byte, string, error) {
	if !cityEngineCanUpgrade(fromVersion, toVersion) {
		return nil, "", ErrCityUpgradePath
	}
	if fromVersion == CitySimulationVersionF5 && toVersion == CitySimulationVersionF6 {
		if _, err := tx.ExecContext(ctx, `SELECT initialize_city_f6_foundation($1)`, worldID); err != nil {
			return nil, "", fmt.Errorf("initialize target city engine foundation: %w", err)
		}
		var dayIndex, monthIndex, quarterIndex, yearIndex, calendarVersion int64
		if err := tx.QueryRowContext(ctx, `
SELECT day_index, month_index, quarter_index, year_index, version
FROM city_calendar_states WHERE world_id = $1 FOR UPDATE`, worldID).
			Scan(&dayIndex, &monthIndex, &quarterIndex, &yearIndex, &calendarVersion); err != nil {
			return nil, "", fmt.Errorf("lock legacy city calendar projection: %w", err)
		}
		if dayIndex != 0 || monthIndex != 0 || quarterIndex != 0 || yearIndex != 0 {
			return nil, "", fmt.Errorf("legacy engine source already contains target-version calendar facts")
		}
		var calendarNeedsRebase bool
		if err := tx.QueryRowContext(ctx, `
SELECT calendar.local_date IS DISTINCT FROM (world.simulated_at AT TIME ZONE world.timezone)::date
FROM city_calendar_states calendar
JOIN city_worlds world ON world.id = calendar.world_id
WHERE calendar.world_id = $1`, worldID).Scan(&calendarNeedsRebase); err != nil {
			return nil, "", fmt.Errorf("compare legacy city calendar projection: %w", err)
		}
		if calendarNeedsRebase {
			if calendarVersion == int64(^uint64(0)>>1) {
				return nil, "", fmt.Errorf("city calendar version overflow")
			}
			if _, err := tx.ExecContext(ctx, `
UPDATE city_calendar_states calendar
SET local_date = (world.simulated_at AT TIME ZONE world.timezone)::date,
    version = calendar.version + 1, updated_at = NOW()
FROM city_worlds world
WHERE calendar.world_id = $1 AND world.id = calendar.world_id`, worldID); err != nil {
				return nil, "", fmt.Errorf("rebase target city calendar projection: %w", err)
			}
		}
	} else if fromVersion == CitySimulationVersionF6V2 && toVersion == CitySimulationVersionF6V3 {
		if _, err := tx.ExecContext(ctx, `SELECT initialize_city_f63_foundation($1)`, worldID); err != nil {
			return nil, "", fmt.Errorf("initialize target city household foundation: %w", err)
		}
	} else if fromVersion == CitySimulationVersionF6V3 && toVersion == CitySimulationVersionF7 {
		var seed int64
		if err := tx.QueryRowContext(ctx, `SELECT seed FROM city_worlds WHERE id = $1`, worldID).Scan(&seed); err != nil {
			return nil, "", fmt.Errorf("load target city spatial seed: %w", err)
		}
		if err := initializeCityF7SpatialFoundation(ctx, tx, worldID, seed, toVersion); err != nil {
			return nil, "", err
		}
	} else if fromVersion == CitySimulationVersionF7 && toVersion == CitySimulationVersionF7V2 {
		var seed int64
		if err := tx.QueryRowContext(ctx, `SELECT seed FROM city_worlds WHERE id = $1`, worldID).Scan(&seed); err != nil {
			return nil, "", fmt.Errorf("load target city land seed: %w", err)
		}
		if err := initializeCityLandFoundation(ctx, tx, worldID, seed, toVersion); err != nil {
			return nil, "", err
		}
	} else if fromVersion == CitySimulationVersionF7V2 && toVersion == CitySimulationVersionF7V3 {
		if err := initializeCityDevelopmentFoundation(ctx, tx, worldID); err != nil {
			return nil, "", err
		}
	} else if fromVersion == CitySimulationVersionF7V3 && toVersion == CitySimulationVersionF7V4 {
		if err := initializeCityEnterpriseLocationFoundation(ctx, tx, worldID); err != nil {
			return nil, "", err
		}
	} else if fromVersion == CitySimulationVersionF7V4 && toVersion == CitySimulationVersionF7V5 {
		if err := initializeWorldRuntimeFoundation(ctx, tx, worldID); err != nil {
			return nil, "", err
		}
	} else if fromVersion != CitySimulationVersionF6 || toVersion != CitySimulationVersionF6V2 {
		return nil, "", ErrCityUpgradePath
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE city_worlds SET simulation_version = $2, state_hash = NULL, updated_at = NOW()
WHERE id = $1`, worldID, toVersion); err != nil {
		return nil, "", fmt.Errorf("switch city engine version: %w", err)
	}
	for _, assertion := range []string{
		"assert_city_world_foundation",
		"assert_city_f3_foundation",
		"assert_city_f4_foundation",
		"assert_city_calendar_projection",
		"assert_city_demography_projection",
		"assert_city_household_projection",
		"assert_city_spatial_foundation",
		"assert_city_land_foundation",
		"assert_city_development_foundation",
		"assert_city_enterprise_location_foundation",
		"assert_world_runtime_foundation",
	} {
		if _, err := tx.ExecContext(ctx, `SELECT `+assertion+`($1)`, worldID); err != nil {
			return nil, "", fmt.Errorf("validate target city engine with %s: %w", assertion, err)
		}
	}
	state, canonical, stateHash, err := canonicalCityWorldState(ctx, tx, worldID)
	if err != nil {
		return nil, "", err
	}
	if state.SimulationVersion != toVersion {
		return nil, "", fmt.Errorf("target city engine canonical version mismatch")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE city_worlds SET state_hash = $2 WHERE id = $1`, worldID, stateHash); err != nil {
		return nil, "", fmt.Errorf("store target city engine state hash: %w", err)
	}
	return canonical, stateHash, nil
}

func (s *CityEconomyService) GetUpgrade(ctx context.Context, userID, worldID, runID int64) (*CityUpgradeRun, error) {
	if userID <= 0 || worldID <= 0 || runID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	item, err := scanCityUpgradeRun(s.db.QueryRowContext(ctx, `
SELECT `+cityUpgradeRunColumns+` FROM city_world_upgrade_runs u
WHERE u.world_id = $1 AND u.id = $2`, worldID, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityUpgradeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get city engine upgrade run: %w", err)
	}
	return item, nil
}

func (s *CityEconomyService) ListUpgrades(ctx context.Context, input CityAuditRunListInput) (*CityUpgradeRunPage, error) {
	if input.UserID <= 0 || input.WorldID <= 0 || input.AfterID < 0 {
		return nil, ErrCityInvalidInput
	}
	if input.Limit <= 0 {
		input.Limit = cityAuditDefaultLimit
	}
	if input.Limit > cityAuditMaximumLimit {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT `+cityUpgradeRunColumns+` FROM city_world_upgrade_runs u
WHERE u.world_id = $1 AND u.id > $2
ORDER BY u.id ASC LIMIT $3`, input.WorldID, input.AfterID, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list city engine upgrade runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*CityUpgradeRun, 0, input.Limit)
	for rows.Next() {
		item, scanErr := scanCityUpgradeRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city engine upgrade runs: %w", err)
	}
	page := &CityUpgradeRunPage{Items: items}
	if len(items) > input.Limit {
		items = items[:input.Limit]
		page.Items = items
		next := items[len(items)-1].ID
		page.NextCursor = &next
	}
	return page, nil
}

func cityUpgradeFingerprint(targetVersion string, dryRun bool) (string, error) {
	raw, err := json.Marshal(struct {
		Operation     string `json:"operation"`
		TargetVersion string `json:"target_version"`
		DryRun        bool   `json:"dry_run"`
	}{Operation: "city.engine.upgrade", TargetVersion: targetVersion, DryRun: dryRun})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func loadCityUpgradeByRequest(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, userID int64,
	requestID string,
) (*CityUpgradeRun, error) {
	return scanCityUpgradeRun(queryer.QueryRowContext(ctx, `
SELECT `+cityUpgradeRunColumns+` FROM city_world_upgrade_runs u
WHERE u.world_id = $1 AND u.requested_by_user_id = $2 AND u.client_request_id = $3`,
		worldID, userID, requestID))
}

func scanCityUpgradeRun(scanner cityScannable) (*CityUpgradeRun, error) {
	item := &CityUpgradeRun{}
	var targetSnapshotID sql.NullInt64
	var afterHash, errorCode, errorDetail sql.NullString
	var completedAt sql.NullTime
	if err := scanner.Scan(
		&item.ID, &item.WorldID, &item.RequestedByUserID, &item.ClientRequestID,
		&item.requestFingerprint, &item.FromVersion, &item.ToVersion, &item.FromTick,
		&item.DryRun, &item.Status, &item.SourceSnapshotID, &targetSnapshotID,
		&item.BeforeStateHash, &afterHash, &errorCode, &errorDetail,
		&item.StartedAt, &completedAt,
	); err != nil {
		return nil, err
	}
	item.TargetSnapshotID = nullInt64Pointer(targetSnapshotID)
	item.AfterStateHash = nullStringPointer(afterHash)
	item.ErrorCode = nullStringPointer(errorCode)
	item.ErrorDetail = nullStringPointer(errorDetail)
	item.CompletedAt = nullTimePointer(completedAt)
	return item, nil
}
