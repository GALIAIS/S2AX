package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	CitySnapshotReasonGenesis  = "genesis"
	CitySnapshotReasonBaseline = "baseline"
	CitySnapshotReasonTick     = "tick"
	citySnapshotFormat         = "city-state-v1+gzip"
	citySnapshotMaximumBytes   = 32 << 20
	citySnapshotMaximumPayload = citySnapshotMaximumBytes + (1 << 20)
	citySnapshotDefaultLimit   = 50
	citySnapshotMaximumLimit   = 200
	cityAuditDefaultLimit      = 50
	cityAuditMaximumLimit      = 200
	cityReplayMaximumTicks     = 10000

	CityReplayStatusRunning  = "running"
	CityReplayStatusVerified = "verified"
	CityReplayStatusDiverged = "diverged"
	CityReplayStatusFailed   = "failed"

	CityRecoveryStatusRunning = "running"
	CityRecoveryStatusApplied = "applied"
	CityRecoveryStatusFailed  = "failed"
)

var (
	ErrCitySnapshotNotFound     = infraerrors.NotFound("CITY_SNAPSHOT_NOT_FOUND", "city snapshot not found")
	ErrCitySnapshotIntegrity    = infraerrors.Conflict("CITY_SNAPSHOT_INTEGRITY_FAILED", "city snapshot integrity verification failed")
	ErrCityReplayNotFound       = infraerrors.NotFound("CITY_REPLAY_NOT_FOUND", "city replay run not found")
	ErrCityReplayConflict       = infraerrors.Conflict("CITY_REPLAY_CONFLICT", "city replay idempotency key was reused with different intent")
	ErrCityReplayRange          = infraerrors.BadRequest("CITY_REPLAY_RANGE_INVALID", "city replay range is invalid")
	ErrCityRecoveryNotFound     = infraerrors.NotFound("CITY_RECOVERY_NOT_FOUND", "city recovery run not found")
	ErrCityRecoveryConflict     = infraerrors.Conflict("CITY_RECOVERY_CONFLICT", "city recovery idempotency key was reused with different intent")
	ErrCityRecoveryPrecondition = infraerrors.Conflict("CITY_RECOVERY_PRECONDITION_FAILED", "city recovery requires a verified replay at the current tick")
)

type CitySnapshot struct {
	ID                int64     `json:"id"`
	WorldID           int64     `json:"world_id"`
	Tick              int64     `json:"tick"`
	SourceTickID      *int64    `json:"source_tick_id,omitempty"`
	SimulationVersion string    `json:"simulation_version"`
	SnapshotFormat    string    `json:"snapshot_format"`
	Reason            string    `json:"reason"`
	StateHash         string    `json:"state_hash"`
	PayloadHash       string    `json:"payload_hash"`
	UncompressedSize  int64     `json:"uncompressed_size"`
	CompressedSize    int64     `json:"compressed_size"`
	IntegrityVerified bool      `json:"integrity_verified"`
	CreatedAt         time.Time `json:"created_at"`
	payload           []byte
}

type CitySnapshotPage struct {
	Items      []*CitySnapshot `json:"items"`
	NextCursor *int64          `json:"next_cursor,omitempty"`
}

type CitySnapshotListInput struct {
	UserID            int64
	WorldID           int64
	AfterTick         int64
	Limit             int
	SimulationVersion string
}

type citySnapshotCapture struct {
	worldID           int64
	tick              int64
	sourceTickID      *int64
	simulationVersion string
	reason            string
	canonical         []byte
	stateHash         string
}

const citySnapshotColumns = `
s.id, s.world_id, s.tick, s.source_tick_id, s.simulation_version,
s.snapshot_format, s.reason, s.state_hash, s.payload_hash, s.payload,
s.uncompressed_size, s.compressed_size, s.created_at`

const citySnapshotListColumns = `
s.id, s.world_id, s.tick, s.source_tick_id, s.simulation_version,
s.snapshot_format, s.reason, s.state_hash, s.payload_hash, NULL::bytea AS payload,
s.uncompressed_size, s.compressed_size, s.created_at`

func captureCitySnapshot(
	ctx context.Context,
	tx *sql.Tx,
	input citySnapshotCapture,
) (*CitySnapshot, error) {
	if input.worldID <= 0 || input.tick < 0 || len(input.canonical) == 0 ||
		len(input.canonical) > citySnapshotMaximumBytes ||
		input.simulationVersion == "" || input.stateHash == "" {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "snapshot_capture"})
	}
	canonicalSum := sha256.Sum256(input.canonical)
	if hex.EncodeToString(canonicalSum[:]) != input.stateHash {
		return nil, ErrCitySnapshotIntegrity.WithMetadata(map[string]string{"field": "state_hash"})
	}
	compressed, err := compressCitySnapshot(input.canonical)
	if err != nil {
		return nil, err
	}
	payloadSum := sha256.Sum256(compressed)
	payloadHash := hex.EncodeToString(payloadSum[:])

	existing, err := loadCitySnapshotByTick(ctx, tx, input.worldID, input.tick)
	if err == nil {
		if existing.SimulationVersion != input.simulationVersion || existing.StateHash != input.stateHash ||
			existing.PayloadHash != payloadHash || !bytes.Equal(existing.payload, compressed) {
			return nil, ErrCitySnapshotIntegrity.WithMetadata(map[string]string{
				"field": "snapshot_collision", "tick": strconv.FormatInt(input.tick, 10),
			})
		}
		if _, _, verifyErr := verifyCitySnapshot(existing); verifyErr != nil {
			return nil, verifyErr
		}
		existing.IntegrityVerified = true
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("find city snapshot: %w", err)
	}

	snapshot, err := scanCitySnapshot(tx.QueryRowContext(ctx, `
INSERT INTO city_snapshots AS s
    (world_id, tick, source_tick_id, simulation_version, snapshot_format,
     reason, state_hash, payload_hash, payload, uncompressed_size, compressed_size)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING `+citySnapshotColumns,
		input.worldID, input.tick, cityNullableInt64(input.sourceTickID),
		input.simulationVersion, citySnapshotFormat, input.reason,
		input.stateHash, payloadHash, compressed, len(input.canonical), len(compressed)))
	if err != nil {
		return nil, fmt.Errorf("insert city snapshot: %w", err)
	}
	snapshot.IntegrityVerified = true
	return snapshot, nil
}

func ensureCityBaselineSnapshot(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	world *lockedCityWorld,
) (*string, error) {
	if world == nil || worldID <= 0 || world.currentTick < 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "snapshot_baseline"})
	}
	_, canonical, currentHash, err := canonicalCityWorldState(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	existing, err := loadCitySnapshotByTick(ctx, tx, worldID, world.currentTick)
	if err == nil {
		_, snapshotCanonical, verifyErr := verifyCitySnapshot(existing)
		if verifyErr != nil {
			return nil, verifyErr
		}
		if existing.StateHash != currentHash || !bytes.Equal(snapshotCanonical, canonical) {
			return nil, ErrCitySnapshotIntegrity.WithMetadata(map[string]string{
				"field": "live_projection", "tick": strconv.FormatInt(world.currentTick, 10),
			})
		}
		if world.stateHash != nil && *world.stateHash != currentHash {
			return nil, ErrCitySnapshotIntegrity.WithMetadata(map[string]string{"field": "world_state_hash"})
		}
		if world.stateHash == nil {
			if _, err = tx.ExecContext(ctx, `UPDATE city_worlds SET state_hash = $2 WHERE id = $1`, worldID, currentHash); err != nil {
				return nil, fmt.Errorf("restore city world baseline hash: %w", err)
			}
		}
		return stringPointer(currentHash), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("load city baseline snapshot: %w", err)
	}
	if world.stateHash != nil && *world.stateHash != currentHash {
		return nil, ErrCitySnapshotIntegrity.WithMetadata(map[string]string{"field": "world_state_hash"})
	}
	var sourceTickID *int64
	if world.currentTick > 0 {
		var id int64
		if err = tx.QueryRowContext(ctx, `
SELECT id FROM city_ticks WHERE world_id = $1 AND tick = $2`, worldID, world.currentTick).Scan(&id); err != nil {
			return nil, fmt.Errorf("load city baseline source tick: %w", err)
		}
		sourceTickID = &id
	}
	if _, err = captureCitySnapshot(ctx, tx, citySnapshotCapture{
		worldID: worldID, tick: world.currentTick, sourceTickID: sourceTickID,
		simulationVersion: world.simulationVersion, reason: CitySnapshotReasonBaseline,
		canonical: canonical, stateHash: currentHash,
	}); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE city_worlds SET state_hash = $2 WHERE id = $1`, worldID, currentHash); err != nil {
		return nil, fmt.Errorf("store city baseline state hash: %w", err)
	}
	return stringPointer(currentHash), nil
}

func compressCitySnapshot(canonical []byte) ([]byte, error) {
	var buffer bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buffer, gzip.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("create city snapshot compressor: %w", err)
	}
	writer.Header.ModTime = time.Time{}
	writer.Header.OS = 255
	if _, err = writer.Write(canonical); err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("compress city snapshot: %w", err)
	}
	if err = writer.Close(); err != nil {
		return nil, fmt.Errorf("close city snapshot compressor: %w", err)
	}
	return buffer.Bytes(), nil
}

// cityHashStateF5 preserves the exact canonical field order used by F5 snapshots.
// F6 keeps those immutable snapshots verifiable even though current replay and
// recovery only operate on snapshots matching the world's active engine version.
type cityHashStateF5 struct {
	Name              string                    `json:"name"`
	Status            string                    `json:"status"`
	SimulationVersion string                    `json:"simulation_version"`
	Seed              int64                     `json:"seed"`
	CurrentTick       int64                     `json:"current_tick"`
	SimulatedAt       string                    `json:"simulated_at"`
	SpeedMilli        int64                     `json:"speed_milli"`
	Timezone          string                    `json:"timezone"`
	Settings          json.RawMessage           `json:"settings"`
	MonetaryUnits     []cityHashMonetaryUnit    `json:"monetary_units"`
	AccountTemplates  []cityHashAccountTemplate `json:"account_templates"`
	Entities          []cityHashEntity          `json:"entities"`
	Accounts          []cityHashAccount         `json:"accounts"`
	Physical          cityPhysicalHashState     `json:"physical"`
	Markets           cityMarketHashState       `json:"markets"`
}

func decodeCanonicalCitySnapshot(canonical []byte, simulationVersion string) (cityHashState, []byte, error) {
	decode := func(target any) error {
		decoder := json.NewDecoder(bytes.NewReader(canonical))
		decoder.DisallowUnknownFields()
		return decoder.Decode(target)
	}
	switch simulationVersion {
	case CitySimulationVersionF6, CitySimulationVersionF6V2, CitySimulationVersionF6V3,
		CitySimulationVersionF7, CitySimulationVersionF7V2, CitySimulationVersionF7V3,
		CitySimulationVersionF7V4, CitySimulationVersionF7V5:
		var state cityHashState
		if err := decode(&state); err != nil {
			return cityHashState{}, nil, err
		}
		if state.SimulationVersion != simulationVersion {
			return cityHashState{}, nil, fmt.Errorf("city snapshot simulation version mismatch")
		}
		if cityEngineSupportsSpatial(simulationVersion) && state.Spatial == nil {
			return cityHashState{}, nil, fmt.Errorf("city F7 snapshot requires spatial state")
		}
		if !cityEngineSupportsSpatial(simulationVersion) && state.Spatial != nil {
			return cityHashState{}, nil, fmt.Errorf("legacy city snapshot cannot contain spatial state")
		}
		if cityEngineSupportsLand(simulationVersion) && state.Land == nil {
			return cityHashState{}, nil, fmt.Errorf("city F7.3 snapshot requires land state")
		}
		if !cityEngineSupportsLand(simulationVersion) && state.Land != nil {
			return cityHashState{}, nil, fmt.Errorf("legacy city snapshot cannot contain land state")
		}
		if cityEngineSupportsDevelopment(simulationVersion) && state.Development == nil {
			return cityHashState{}, nil, fmt.Errorf("city F7.4 snapshot requires development state")
		}
		if !cityEngineSupportsDevelopment(simulationVersion) && state.Development != nil {
			return cityHashState{}, nil, fmt.Errorf("legacy city snapshot cannot contain development state")
		}
		if cityEngineSupportsEnterpriseLocation(simulationVersion) && state.EnterpriseLocation == nil {
			return cityHashState{}, nil, fmt.Errorf("city F7.5 snapshot requires enterprise location state")
		}
		if !cityEngineSupportsEnterpriseLocation(simulationVersion) && state.EnterpriseLocation != nil {
			return cityHashState{}, nil, fmt.Errorf("legacy city snapshot cannot contain enterprise location state")
		}
		if cityEngineSupportsWorldRuntime(simulationVersion) && state.WorldRuntime == nil {
			return cityHashState{}, nil, fmt.Errorf("city F7.6 snapshot requires world runtime state")
		}
		if !cityEngineSupportsWorldRuntime(simulationVersion) && state.WorldRuntime != nil {
			return cityHashState{}, nil, fmt.Errorf("legacy city snapshot cannot contain world runtime state")
		}
		reencoded, err := marshalCanonicalCityState(state)
		return state, reencoded, err
	case CitySimulationVersionF5:
		var legacy cityHashStateF5
		if err := decode(&legacy); err != nil {
			return cityHashState{}, nil, err
		}
		reencoded, err := json.Marshal(legacy)
		if err != nil {
			return cityHashState{}, nil, err
		}
		return cityHashState{
			Name: legacy.Name, Status: legacy.Status,
			SimulationVersion: legacy.SimulationVersion, Seed: legacy.Seed,
			CurrentTick: legacy.CurrentTick, SimulatedAt: legacy.SimulatedAt,
			SpeedMilli: legacy.SpeedMilli, Timezone: legacy.Timezone,
			Settings: legacy.Settings, MonetaryUnits: legacy.MonetaryUnits,
			AccountTemplates: legacy.AccountTemplates, Entities: legacy.Entities,
			Accounts: legacy.Accounts, Physical: legacy.Physical, Markets: legacy.Markets,
		}, reencoded, nil
	default:
		return cityHashState{}, nil, fmt.Errorf("unsupported city snapshot simulation version %q", simulationVersion)
	}
}

func verifyCitySnapshot(snapshot *CitySnapshot) (cityHashState, []byte, error) {
	if snapshot == nil || snapshot.SnapshotFormat != citySnapshotFormat || snapshot.UncompressedSize <= 0 ||
		snapshot.UncompressedSize > citySnapshotMaximumBytes || snapshot.CompressedSize <= 0 ||
		snapshot.CompressedSize > citySnapshotMaximumPayload || snapshot.CompressedSize != int64(len(snapshot.payload)) {
		return cityHashState{}, nil, ErrCitySnapshotIntegrity.WithMetadata(map[string]string{"field": "metadata"})
	}
	payloadSum := sha256.Sum256(snapshot.payload)
	if hex.EncodeToString(payloadSum[:]) != snapshot.PayloadHash {
		return cityHashState{}, nil, ErrCitySnapshotIntegrity.WithMetadata(map[string]string{"field": "payload_hash"})
	}
	reader, err := gzip.NewReader(bytes.NewReader(snapshot.payload))
	if err != nil {
		return cityHashState{}, nil, ErrCitySnapshotIntegrity.WithMetadata(map[string]string{"field": "compression"}).WithCause(err)
	}
	canonical, readErr := io.ReadAll(io.LimitReader(reader, snapshot.UncompressedSize+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || int64(len(canonical)) != snapshot.UncompressedSize {
		if readErr == nil {
			readErr = closeErr
		}
		return cityHashState{}, nil, ErrCitySnapshotIntegrity.WithMetadata(map[string]string{"field": "payload_size"}).WithCause(readErr)
	}
	stateSum := sha256.Sum256(canonical)
	if hex.EncodeToString(stateSum[:]) != snapshot.StateHash {
		return cityHashState{}, nil, ErrCitySnapshotIntegrity.WithMetadata(map[string]string{"field": "state_hash"})
	}
	state, reencoded, err := decodeCanonicalCitySnapshot(canonical, snapshot.SimulationVersion)
	if err != nil {
		return cityHashState{}, nil, ErrCitySnapshotIntegrity.WithMetadata(map[string]string{"field": "state_json"}).WithCause(err)
	}
	if !bytes.Equal(reencoded, canonical) || state.CurrentTick != snapshot.Tick ||
		state.SimulationVersion != snapshot.SimulationVersion {
		return cityHashState{}, nil, ErrCitySnapshotIntegrity.WithMetadata(map[string]string{"field": "canonical_state"}).WithCause(err)
	}
	return state, canonical, nil
}

func (s *CityEconomyService) ListSnapshots(ctx context.Context, input CitySnapshotListInput) (*CitySnapshotPage, error) {
	if input.UserID <= 0 || input.WorldID <= 0 || input.AfterTick < 0 {
		return nil, ErrCityInvalidInput
	}
	if input.Limit <= 0 {
		input.Limit = citySnapshotDefaultLimit
	}
	if input.Limit > citySnapshotMaximumLimit {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	version := strings.TrimSpace(input.SimulationVersion)
	if version != "" {
		if _, err := cityEngineForVersion(version); err != nil {
			return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": version})
		}
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT `+citySnapshotListColumns+`
FROM city_snapshots s
JOIN city_worlds w ON w.id = s.world_id
WHERE s.world_id = $1
  AND s.simulation_version = COALESCE(NULLIF($4, ''), w.simulation_version)
  AND s.tick >= $2
ORDER BY s.tick ASC
LIMIT $3`, input.WorldID, input.AfterTick, input.Limit+1, version)
	if err != nil {
		return nil, fmt.Errorf("list city snapshots: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*CitySnapshot, 0, input.Limit)
	for rows.Next() {
		item, scanErr := scanCitySnapshot(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		item.payload = nil
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city snapshots: %w", err)
	}
	page := &CitySnapshotPage{Items: items}
	if len(items) > input.Limit {
		items = items[:input.Limit]
		page.Items = items
		next := items[len(items)-1].Tick + 1
		page.NextCursor = &next
	}
	return page, nil
}

func (s *CityEconomyService) GetSnapshot(ctx context.Context, userID, worldID, tick int64) (*CitySnapshot, error) {
	return s.GetSnapshotVersion(ctx, userID, worldID, tick, "")
}

func (s *CityEconomyService) GetSnapshotVersion(
	ctx context.Context,
	userID, worldID, tick int64,
	simulationVersion string,
) (*CitySnapshot, error) {
	if userID <= 0 || worldID <= 0 || tick < 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	version := strings.TrimSpace(simulationVersion)
	if version != "" {
		if _, err := cityEngineForVersion(version); err != nil {
			return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": version})
		}
	}
	item, err := loadCitySnapshotByVersion(ctx, s.db, worldID, tick, version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySnapshotNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get city snapshot: %w", err)
	}
	if _, _, err = verifyCitySnapshot(item); err != nil {
		return nil, err
	}
	item.IntegrityVerified = true
	item.payload = nil
	return item, nil
}

func loadCitySnapshotByTick(ctx context.Context, queryer citySQLQueryer, worldID, tick int64) (*CitySnapshot, error) {
	return loadCitySnapshotByVersion(ctx, queryer, worldID, tick, "")
}

func loadCitySnapshotByVersion(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
	simulationVersion string,
) (*CitySnapshot, error) {
	return scanCitySnapshot(queryer.QueryRowContext(ctx, `
SELECT `+citySnapshotColumns+`
FROM city_snapshots s
JOIN city_worlds w ON w.id = s.world_id
WHERE s.world_id = $1 AND s.tick = $2
  AND s.simulation_version = COALESCE(NULLIF($3, ''), w.simulation_version)`,
		worldID, tick, simulationVersion))
}

func scanCitySnapshot(scanner cityScannable) (*CitySnapshot, error) {
	item := &CitySnapshot{}
	var sourceTickID sql.NullInt64
	if err := scanner.Scan(
		&item.ID, &item.WorldID, &item.Tick, &sourceTickID,
		&item.SimulationVersion, &item.SnapshotFormat, &item.Reason,
		&item.StateHash, &item.PayloadHash, &item.payload,
		&item.UncompressedSize, &item.CompressedSize, &item.CreatedAt,
	); err != nil {
		return nil, err
	}
	item.SourceTickID = nullInt64Pointer(sourceTickID)
	return item, nil
}

func stringPointer(value string) *string {
	return &value
}

type CityReplayRun struct {
	ID                 int64      `json:"id"`
	WorldID            int64      `json:"world_id"`
	RequestedByUserID  int64      `json:"requested_by_user_id"`
	ClientRequestID    string     `json:"client_request_id"`
	BaseSnapshotID     int64      `json:"base_snapshot_id"`
	FromTick           int64      `json:"from_tick"`
	TargetTick         int64      `json:"target_tick"`
	Status             string     `json:"status"`
	ExpectedStateHash  *string    `json:"expected_state_hash,omitempty"`
	ActualStateHash    *string    `json:"actual_state_hash,omitempty"`
	VerifiedTickCount  int64      `json:"verified_tick_count"`
	DivergenceTick     *int64     `json:"divergence_tick,omitempty"`
	DivergencePath     *string    `json:"divergence_path,omitempty"`
	ErrorCode          *string    `json:"error_code,omitempty"`
	ErrorDetail        *string    `json:"error_detail,omitempty"`
	StartedAt          time.Time  `json:"started_at"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
	requestFingerprint string
}

type CityReplayInput struct {
	UserID         int64
	WorldID        int64
	IdempotencyKey string
	FromTick       *int64
	TargetTick     *int64
}

type CityReplayRunPage struct {
	Items      []*CityReplayRun `json:"items"`
	NextCursor *int64           `json:"next_cursor,omitempty"`
}

type CityAuditRunListInput struct {
	UserID  int64
	WorldID int64
	AfterID int64
	Limit   int
}

type cityReplayOutcome struct {
	status            string
	expectedHash      *string
	actualHash        *string
	verifiedTickCount int64
	divergenceTick    *int64
	divergencePath    *string
	errorCode         *string
	errorDetail       *string
}

const cityReplayRunColumns = `
r.id, r.world_id, r.requested_by_user_id, r.client_request_id,
r.request_fingerprint, r.base_snapshot_id, r.from_tick, r.target_tick,
r.status, r.expected_state_hash, r.actual_state_hash, r.verified_tick_count,
r.divergence_tick, r.divergence_path, r.error_code, r.error_detail,
r.started_at, r.completed_at`

func (s *CityEconomyService) StartReplay(ctx context.Context, input CityReplayInput) (*CityReplayRun, error) {
	if input.UserID <= 0 || input.WorldID <= 0 ||
		(input.FromTick != nil && *input.FromTick < 0) ||
		(input.TargetTick != nil && *input.TargetTick < 0) {
		return nil, ErrCityInvalidInput
	}
	requestID, err := requireCityIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	fingerprint, err := cityReplayFingerprint(input.FromTick, input.TargetTick)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin city replay transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, cityWorldLockKey(input.WorldID)); err != nil {
		return nil, fmt.Errorf("lock city replay world: %w", err)
	}
	world, err := lockCityWorld(ctx, tx, input.UserID, input.WorldID)
	if err != nil {
		return nil, err
	}
	if world.memberRole != CityMemberRoleOwner {
		return nil, ErrCityPermissionDenied
	}
	if _, engineErr := cityEngineForVersion(world.simulationVersion); engineErr != nil {
		return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": world.simulationVersion})
	}
	existing, err := loadCityReplayByRequest(ctx, tx, input.WorldID, input.UserID, requestID)
	if err == nil {
		if existing.requestFingerprint != fingerprint {
			return nil, ErrCityReplayConflict
		}
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit city replay idempotent read: %w", err)
		}
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("find city replay request: %w", err)
	}
	targetTick := world.currentTick
	if input.TargetTick != nil {
		targetTick = *input.TargetTick
	}
	if targetTick > world.currentTick {
		return nil, ErrCityReplayRange.WithMetadata(map[string]string{"field": "target_tick"})
	}
	fromTick := int64(-1)
	if input.FromTick != nil {
		fromTick = *input.FromTick
	} else {
		var earliest sql.NullInt64
		if err = tx.QueryRowContext(ctx, `
SELECT MIN(tick) FROM city_snapshots
WHERE world_id = $1 AND tick <= $2 AND simulation_version = $3`,
			input.WorldID, targetTick, world.simulationVersion).Scan(&earliest); err != nil {
			return nil, fmt.Errorf("select city replay baseline: %w", err)
		}
		if earliest.Valid {
			fromTick = earliest.Int64
		}
	}
	if fromTick < 0 || fromTick > targetTick || targetTick-fromTick > cityReplayMaximumTicks {
		return nil, ErrCityReplayRange.WithMetadata(map[string]string{
			"from_tick": strconv.FormatInt(fromTick, 10), "target_tick": strconv.FormatInt(targetTick, 10),
		})
	}
	base, err := loadCitySnapshotByTick(ctx, tx, input.WorldID, fromTick)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySnapshotNotFound.WithMetadata(map[string]string{"tick": strconv.FormatInt(fromTick, 10)})
	}
	if err != nil {
		return nil, fmt.Errorf("load city replay base snapshot: %w", err)
	}
	run, err := scanCityReplayRun(tx.QueryRowContext(ctx, `
INSERT INTO city_replay_runs AS r
    (world_id, requested_by_user_id, client_request_id, request_fingerprint,
     base_snapshot_id, from_tick, target_tick)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING `+cityReplayRunColumns,
		input.WorldID, input.UserID, requestID, fingerprint, base.ID, fromTick, targetTick))
	if err != nil {
		return nil, fmt.Errorf("insert city replay run: %w", err)
	}
	outcome := replayCityWorld(ctx, tx, input.WorldID, base, targetTick)
	completedAt := time.Now().UTC()
	run, err = scanCityReplayRun(tx.QueryRowContext(ctx, `
UPDATE city_replay_runs AS r
SET status = $2, expected_state_hash = $3, actual_state_hash = $4,
    verified_tick_count = $5, divergence_tick = $6, divergence_path = $7,
    error_code = $8, error_detail = $9, completed_at = $10
WHERE r.id = $1 AND r.status = 'running'
RETURNING `+cityReplayRunColumns,
		run.ID, outcome.status, cityNullableString(outcome.expectedHash),
		cityNullableString(outcome.actualHash), outcome.verifiedTickCount,
		cityNullableInt64(outcome.divergenceTick), cityNullableString(outcome.divergencePath),
		cityNullableString(outcome.errorCode), cityNullableString(outcome.errorDetail), completedAt))
	if err != nil {
		return nil, fmt.Errorf("complete city replay run: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city replay run: %w", err)
	}
	return run, nil
}

func (s *CityEconomyService) GetReplay(ctx context.Context, userID, worldID, runID int64) (*CityReplayRun, error) {
	if userID <= 0 || worldID <= 0 || runID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	item, err := scanCityReplayRun(s.db.QueryRowContext(ctx, `
SELECT `+cityReplayRunColumns+` FROM city_replay_runs r
WHERE r.world_id = $1 AND r.id = $2`, worldID, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityReplayNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get city replay run: %w", err)
	}
	return item, nil
}

func (s *CityEconomyService) ListReplays(ctx context.Context, input CityAuditRunListInput) (*CityReplayRunPage, error) {
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
SELECT `+cityReplayRunColumns+` FROM city_replay_runs r
WHERE r.world_id = $1 AND r.id > $2
ORDER BY r.id ASC LIMIT $3`, input.WorldID, input.AfterID, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list city replay runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*CityReplayRun, 0, input.Limit)
	for rows.Next() {
		item, scanErr := scanCityReplayRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city replay runs: %w", err)
	}
	page := &CityReplayRunPage{Items: items}
	if len(items) > input.Limit {
		items = items[:input.Limit]
		page.Items = items
		next := items[len(items)-1].ID
		page.NextCursor = &next
	}
	return page, nil
}

func replayCityWorld(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	base *CitySnapshot,
	targetTick int64,
) cityReplayOutcome {
	fail := func(code, detail string, verified int64) cityReplayOutcome {
		return cityReplayOutcome{
			status: CityReplayStatusFailed, verifiedTickCount: verified,
			errorCode: stringPointer(code), errorDetail: stringPointer(cityAuditDetail(detail)),
		}
	}
	state, _, err := verifyCitySnapshot(base)
	if err != nil {
		return fail("SNAPSHOT_INTEGRITY", err.Error(), 0)
	}
	currentHash := base.StateHash
	if state.CurrentTick != base.Tick || targetTick < base.Tick {
		return fail("BASE_SNAPSHOT_INVALID", "base snapshot tick does not match replay range", 0)
	}
	verified := int64(0)
	for tickNumber := base.Tick + 1; tickNumber <= targetTick; tickNumber++ {
		tick, loadErr := loadCityTickByNumber(ctx, queryer, worldID, tickNumber)
		if loadErr != nil {
			return fail("TICK_FACT_MISSING", loadErr.Error(), verified)
		}
		if tick.PreviousStateHash == nil || *tick.PreviousStateHash != currentHash {
			path := "/previous_state_hash"
			actual := currentHash
			return cityReplayOutcome{
				status: CityReplayStatusDiverged, expectedHash: tick.PreviousStateHash,
				actualHash: &actual, verifiedTickCount: verified,
				divergenceTick: &tickNumber, divergencePath: &path,
			}
		}
		if tick.SimulationVersion != state.SimulationVersion ||
			tick.PRNGProof != deriveCityRandomHex(state.SimulationVersion, state.Seed, tickNumber, "tick", 0) {
			return fail("TICK_PROOF_INVALID", fmt.Sprintf("tick %d version or PRNG proof mismatch", tickNumber), verified)
		}
		if applyErr := applyCityTickFacts(ctx, queryer, worldID, tick, &state); applyErr != nil {
			return fail("FACT_REDUCER_FAILED", applyErr.Error(), verified)
		}
		actualCanonical, actualHash, hashErr := canonicalCityHashState(state)
		if hashErr != nil {
			return fail("STATE_ENCODING_FAILED", hashErr.Error(), verified)
		}
		tickSnapshot, snapshotErr := loadCitySnapshotByTick(ctx, queryer, worldID, tickNumber)
		if snapshotErr != nil {
			return fail("TICK_SNAPSHOT_MISSING", snapshotErr.Error(), verified)
		}
		_, expectedCanonical, verifyErr := verifyCitySnapshot(tickSnapshot)
		if verifyErr != nil {
			return fail("TICK_SNAPSHOT_INTEGRITY", verifyErr.Error(), verified)
		}
		if actualHash != tick.StateHash || actualHash != tickSnapshot.StateHash ||
			!bytes.Equal(actualCanonical, expectedCanonical) {
			path := cityFirstJSONDifference(expectedCanonical, actualCanonical)
			expected := tickSnapshot.StateHash
			return cityReplayOutcome{
				status: CityReplayStatusDiverged, expectedHash: &expected,
				actualHash: &actualHash, verifiedTickCount: verified,
				divergenceTick: &tickNumber, divergencePath: &path,
			}
		}
		verified++
		currentHash = actualHash
	}
	expected := currentHash
	actual := currentHash
	return cityReplayOutcome{
		status: CityReplayStatusVerified, expectedHash: &expected, actualHash: &actual,
		verifiedTickCount: verified,
	}
}

func applyCityTickFacts(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	tick *CityTick,
	state *cityHashState,
) error {
	if tick == nil || state == nil || tick.WorldID != worldID || tick.Tick != state.CurrentTick+1 {
		return fmt.Errorf("tick sequence is not contiguous")
	}
	engine, err := cityEngineForVersion(state.SimulationVersion)
	if err != nil || tick.SimulationVersion != state.SimulationVersion {
		return fmt.Errorf("tick simulation version is unsupported or inconsistent")
	}
	for _, stage := range engine.stages {
		var stageErr error
		switch stage {
		case cityEngineStageControl:
			stageErr = replayCityControlCommands(ctx, queryer, worldID, tick.Tick, state)
		case cityEngineStageLedger:
			stageErr = replayCityJournalEntries(ctx, queryer, worldID, tick.Tick, state)
		case cityEngineStageResources:
			stageErr = replayCityResourceEntries(ctx, queryer, worldID, tick.Tick, state)
		case cityEngineStageCalendarDemography:
			stageErr = replayCityCalendarAndPopulation(ctx, queryer, worldID, tick.Tick, state)
		case cityEngineStageSpatial:
			stageErr = replayCitySpatialMutations(ctx, queryer, worldID, tick.Tick, state)
		case cityEngineStageDevelopment:
			stageErr = replayCityDevelopmentFacts(ctx, queryer, worldID, tick.Tick, state)
		case cityEngineStageEnterpriseLocation:
			stageErr = replayCityEnterpriseLocationFacts(ctx, queryer, worldID, tick.Tick, state)
		case cityEngineStageWorldRuntime:
			stageErr = replayWorldRuntimeFacts(ctx, queryer, worldID, tick.Tick, state)
		case cityEngineStageMarkets:
			stageErr = replayCityMarketSettlements(ctx, queryer, worldID, tick.Tick, state)
		default:
			stageErr = fmt.Errorf("unknown city engine stage %q", stage)
		}
		if stageErr != nil {
			return fmt.Errorf("replay stage %s: %w", stage, stageErr)
		}
	}
	state.CurrentTick = tick.Tick
	state.SimulatedAt = tick.SimulatedTo.UTC().Format(time.RFC3339Nano)
	return nil
}

func replayCityControlCommands(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
	state *cityHashState,
) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT command_type, payload
FROM city_commands
WHERE world_id = $1 AND processed_tick = $2 AND status = 'applied'
ORDER BY sequence ASC`, worldID, tick)
	if err != nil {
		return fmt.Errorf("load replay control commands: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var commandType string
		var raw []byte
		if err = rows.Scan(&commandType, &raw); err != nil {
			return err
		}
		var payload map[string]any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err = decoder.Decode(&payload); err != nil {
			return fmt.Errorf("decode replay command: %w", err)
		}
		switch commandType {
		case CityCommandTypeWorldRename:
			name, ok := payload["name"].(string)
			if !ok {
				return fmt.Errorf("rename command is missing name")
			}
			state.Name = name
		case CityCommandTypeWorldSetSpeed:
			speed, ok := cityJSONInteger(payload["speed_milli"])
			if !ok {
				return fmt.Errorf("speed command is invalid")
			}
			state.SpeedMilli = speed
		case CityCommandTypeWorldPause:
			state.Status = CityWorldStatusPaused
		case CityCommandTypeWorldResume:
			state.Status = CityWorldStatusRunning
		}
	}
	return rows.Err()
}

func replayCityJournalEntries(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
	state *cityHashState,
) error {
	index := make(map[string]int, len(state.Accounts))
	for position, account := range state.Accounts {
		index[cityAccountHashKey(account.EntityType, account.EntityCode, account.MonetaryUnitCode, account.TemplateCode)] = position
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT entity.entity_type, entity.code, unit.code, template.code,
       entry.balance_before_units, entry.balance_after_units,
       entry.account_version_before, entry.account_version_after
FROM city_journal_entries entry
JOIN city_journals journal ON journal.id = entry.journal_id
JOIN city_accounts account ON account.id = entry.account_id
JOIN city_economic_entities entity ON entity.id = account.entity_id
JOIN city_monetary_units unit ON unit.id = account.monetary_unit_id
JOIN city_account_templates template ON template.id = account.template_id
WHERE journal.world_id = $1 AND journal.tick = $2 AND journal.posted_at IS NOT NULL
ORDER BY journal.sequence ASC, entry.line_no ASC`, worldID, tick)
	if err != nil {
		return fmt.Errorf("load replay journal entries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var entityType, entityCode, unitCode, templateCode string
		var before, after, versionBefore, versionAfter int64
		if err = rows.Scan(&entityType, &entityCode, &unitCode, &templateCode,
			&before, &after, &versionBefore, &versionAfter); err != nil {
			return err
		}
		position, ok := index[cityAccountHashKey(entityType, entityCode, unitCode, templateCode)]
		if !ok {
			return fmt.Errorf("journal entry references unknown account")
		}
		account := &state.Accounts[position]
		if account.CurrentBalanceUnit != before || account.Version != versionBefore || versionAfter != versionBefore+1 {
			return fmt.Errorf("journal entry account projection chain is broken")
		}
		account.CurrentBalanceUnit = after
		account.Version = versionAfter
	}
	return rows.Err()
}

func replayCityResourceEntries(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
	state *cityHashState,
) error {
	index := make(map[string]int, len(state.Physical.Inventories))
	for position, balance := range state.Physical.Inventories {
		index[cityInventoryHashKey(balance.EntityCode, balance.DistrictCode, balance.ResourceCode)] = position
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT entity.code, district.code, resource.code,
       entry.quantity_before_units, entry.quantity_after_units,
       entry.balance_version_before, entry.balance_version_after,
       balance.entity_type, balance.opening_quantity_units,
       balance.status, balance.metadata
FROM city_resource_entries entry
JOIN city_resource_operations operation ON operation.id = entry.operation_id
JOIN city_inventory_balances balance ON balance.id = entry.balance_id
JOIN city_economic_entities entity ON entity.id = balance.entity_id
JOIN city_districts district ON district.id = balance.district_id
JOIN city_resources resource ON resource.id = balance.resource_id
WHERE operation.world_id = $1 AND operation.tick = $2 AND operation.posted_at IS NOT NULL
ORDER BY operation.sequence ASC, entry.line_no ASC`, worldID, tick)
	if err != nil {
		return fmt.Errorf("load replay resource entries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var entityCode, districtCode, resourceCode, entityType, status string
		var before, after, versionBefore, versionAfter int64
		var openingQuantity int64
		var metadata json.RawMessage
		if err = rows.Scan(&entityCode, &districtCode, &resourceCode,
			&before, &after, &versionBefore, &versionAfter, &entityType,
			&openingQuantity, &status, &metadata); err != nil {
			return err
		}
		key := cityInventoryHashKey(entityCode, districtCode, resourceCode)
		position, ok := index[key]
		if !ok {
			if before != 0 || versionBefore != 0 || openingQuantity != 0 || status != "active" {
				return fmt.Errorf("resource entry references an invalid newly-created inventory")
			}
			state.Physical.Inventories = append(state.Physical.Inventories, cityHashInventory{
				EntityType: entityType, EntityCode: entityCode, DistrictCode: districtCode,
				ResourceCode: resourceCode, OpeningQuantityUnits: openingQuantity,
				QuantityUnits: before, Version: versionBefore, Status: status, Metadata: metadata,
			})
			position = len(state.Physical.Inventories) - 1
			index[key] = position
		}
		balance := &state.Physical.Inventories[position]
		if balance.QuantityUnits != before || balance.Version != versionBefore || versionAfter != versionBefore+1 {
			return fmt.Errorf("resource entry inventory projection chain is broken")
		}
		balance.QuantityUnits = after
		balance.Version = versionAfter
	}
	if err = rows.Err(); err != nil {
		return err
	}
	districtOrder := make(map[string]int, len(state.Physical.Districts))
	for _, district := range state.Physical.Districts {
		districtOrder[district.Code] = district.SortOrder
	}
	sort.Slice(state.Physical.Inventories, func(i, j int) bool {
		left, right := state.Physical.Inventories[i], state.Physical.Inventories[j]
		if left.EntityCode != right.EntityCode {
			return left.EntityCode < right.EntityCode
		}
		if districtOrder[left.DistrictCode] != districtOrder[right.DistrictCode] {
			return districtOrder[left.DistrictCode] < districtOrder[right.DistrictCode]
		}
		return left.ResourceCode < right.ResourceCode
	})
	return nil
}

func replayCityMarketSettlements(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
	state *cityHashState,
) error {
	rows, err := queryer.QueryContext(ctx, `
SELECT id, cycle_index, settlement_type, clearing_price_units,
       demand_units, supply_units, cleared_units, unmet_demand_units,
       excess_supply_units, metadata
FROM city_market_settlements
WHERE world_id = $1 AND tick = $2 AND posted_at IS NOT NULL
ORDER BY sequence ASC`, worldID, tick)
	if err != nil {
		return fmt.Errorf("load replay market settlements: %w", err)
	}
	type settlementFact struct {
		id, cycleIndex, price, demand, supply, cleared, unmet, excess int64
		settlementType                                                string
		metadata                                                      []byte
	}
	settlements := make([]settlementFact, 0, 4)
	for rows.Next() {
		var item settlementFact
		if err = rows.Scan(&item.id, &item.cycleIndex, &item.settlementType,
			&item.price, &item.demand, &item.supply, &item.cleared,
			&item.unmet, &item.excess, &item.metadata); err != nil {
			_ = rows.Close()
			return err
		}
		settlements = append(settlements, item)
	}
	if err = closeCityRows(rows, "iterate replay market settlements"); err != nil {
		return err
	}
	if len(settlements) == 0 {
		return nil
	}
	if len(settlements) != 4 {
		return fmt.Errorf("economic cycle does not contain four settlements")
	}
	cycleIndex := settlements[0].cycleIndex
	marketIndex := make(map[string]int, len(state.Markets.Markets))
	for position, market := range state.Markets.Markets {
		marketIndex[market.MarketCode] = position
	}
	for _, settlement := range settlements {
		if settlement.cycleIndex != cycleIndex {
			return fmt.Errorf("economic cycle index changes within one tick")
		}
		if settlement.settlementType != CitySettlementFiscal {
			position, ok := marketIndex[settlement.settlementType]
			if !ok {
				return fmt.Errorf("settlement references unknown market %s", settlement.settlementType)
			}
			var metadata struct {
				NextQuoteUnits int64 `json:"next_quote_units"`
			}
			if err = json.Unmarshal(settlement.metadata, &metadata); err != nil || metadata.NextQuoteUnits <= 0 {
				return fmt.Errorf("settlement %s has invalid next quote metadata", settlement.settlementType)
			}
			market := &state.Markets.Markets[position]
			if settlement.price != market.QuoteUnits {
				return fmt.Errorf("settlement %s clearing price breaks quote chain", settlement.settlementType)
			}
			tickValue, priceValue := tick, settlement.price
			market.QuoteUnits = metadata.NextQuoteUnits
			market.LastClearingTick = &tickValue
			market.LastClearingPriceUnits = &priceValue
			market.LastDemandUnits = settlement.demand
			market.LastSupplyUnits = settlement.supply
			market.LastClearedUnits = settlement.cleared
			market.LastUnmetDemandUnits = settlement.unmet
			market.LastExcessSupplyUnits = settlement.excess
			market.Version++
		}
		switch settlement.settlementType {
		case CityMarketLabor:
			if err = replayCityLaborProjection(ctx, queryer, worldID, settlement.id, settlement.cleared, state); err != nil {
				return err
			}
		case CityMarketHousing:
			if err = replayCityHousingProjection(ctx, queryer, worldID, settlement.id, tick, settlement.price, state); err != nil {
				return err
			}
		case CitySettlementFiscal:
			if err = replayCityBudgetMovements(ctx, queryer, worldID, settlement.id, state); err != nil {
				return err
			}
		}
	}
	cycle := &state.Markets.Cycle
	if cycle.CycleIndex == math.MaxInt64 || cycle.Version == math.MaxInt64 ||
		cycle.CadenceTicks <= 0 || tick > math.MaxInt64-int64(cycle.CadenceTicks) {
		return fmt.Errorf("economic cycle projection exceeds integer bounds")
	}
	if cycleIndex != cycle.CycleIndex+1 || tick < cycle.NextDueTick {
		return fmt.Errorf("economic cycle projection chain is broken")
	}
	tickValue := tick
	cycle.CycleIndex = cycleIndex
	cycle.LastSettledTick = &tickValue
	cycle.NextDueTick = tick + int64(cycle.CadenceTicks)
	cycle.Version++
	return nil
}

func replayCityLaborProjection(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, settlementID, cleared int64,
	state *cityHashState,
) error {
	allocations := make(map[string]int64)
	rows, err := queryer.QueryContext(ctx, `
SELECT district.code, cohort.income_band, allocation.quantity_units
FROM city_market_allocations allocation
JOIN city_household_cohorts cohort ON cohort.id = allocation.cohort_id
JOIN city_districts district ON district.id = cohort.district_id
WHERE allocation.world_id = $1 AND allocation.settlement_id = $2
  AND allocation.allocation_type = 'employment'
ORDER BY allocation.line_no ASC`, worldID, settlementID)
	if err != nil {
		return fmt.Errorf("load replay labor allocations: %w", err)
	}
	var total int64
	for rows.Next() {
		var district, band string
		var quantity int64
		if err = rows.Scan(&district, &band, &quantity); err != nil {
			_ = rows.Close()
			return err
		}
		key := district + "\x00" + band
		if _, duplicate := allocations[key]; duplicate {
			_ = rows.Close()
			return fmt.Errorf("duplicate labor allocation")
		}
		allocations[key] = quantity
		total, err = addCityLedgerUnits(total, quantity)
		if err != nil {
			_ = rows.Close()
			return fmt.Errorf("sum replay labor allocations: %w", err)
		}
	}
	if err = closeCityRows(rows, "iterate replay labor allocations"); err != nil {
		return err
	}
	if total != cleared || len(state.Physical.Firms) != 1 {
		return fmt.Errorf("labor allocation does not match settlement")
	}
	for position := range state.Physical.HouseholdCohorts {
		cohort := &state.Physical.HouseholdCohorts[position]
		cohort.EmployedUnits = allocations[cohort.DistrictCode+"\x00"+cohort.IncomeBand]
		if cohort.EmployedUnits > cohort.WorkingAgeUnits {
			return fmt.Errorf("replayed employment exceeds working-age population")
		}
		cohort.Version++
	}
	state.Physical.Firms[0].EmployeeUnits = cleared
	state.Physical.Firms[0].Version++
	return nil
}

func replayCityHousingProjection(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, settlementID, tick, price int64,
	state *cityHashState,
) error {
	allocations := make(map[string]int64)
	rows, err := queryer.QueryContext(ctx, `
SELECT district.code, cohort.income_band, allocation.quantity_units
FROM city_market_allocations allocation
JOIN city_household_cohorts cohort ON cohort.id = allocation.cohort_id
JOIN city_districts district ON district.id = cohort.district_id
WHERE allocation.world_id = $1 AND allocation.settlement_id = $2
  AND allocation.allocation_type = 'housing'
ORDER BY allocation.line_no ASC`, worldID, settlementID)
	if err != nil {
		return fmt.Errorf("load replay housing allocations: %w", err)
	}
	for rows.Next() {
		var district, band string
		var quantity int64
		if err = rows.Scan(&district, &band, &quantity); err != nil {
			_ = rows.Close()
			return err
		}
		key := district + "\x00" + band
		if _, duplicate := allocations[key]; duplicate {
			_ = rows.Close()
			return fmt.Errorf("duplicate housing allocation")
		}
		allocations[key] = quantity
	}
	if err = closeCityRows(rows, "iterate replay housing allocations"); err != nil {
		return err
	}
	demand := make(map[string]int64, len(state.Physical.HouseholdCohorts))
	for _, cohort := range state.Physical.HouseholdCohorts {
		demand[cohort.DistrictCode+"\x00"+cohort.IncomeBand] = cohort.HousingDemandUnits
	}
	for position := range state.Markets.Occupancies {
		occupancy := &state.Markets.Occupancies[position]
		key := occupancy.DistrictCode + "\x00" + occupancy.IncomeBand
		occupied := allocations[key]
		cohortDemand, ok := demand[key]
		if !ok || occupied > cohortDemand {
			return fmt.Errorf("housing allocation does not match a cohort")
		}
		tickValue := tick
		occupancy.OccupiedUnits = occupied
		occupancy.UnmetUnits = cohortDemand - occupied
		occupancy.RentPriceUnits = price
		occupancy.LastSettledTick = &tickValue
		occupancy.Version++
	}
	return nil
}

func replayCityBudgetMovements(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, settlementID int64,
	state *cityHashState,
) error {
	index := make(map[string]int, len(state.Physical.BudgetLines))
	for position, budget := range state.Physical.BudgetLines {
		index[budget.Code] = position
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT budget.code, movement.spent_before_units, movement.spent_after_units,
       movement.budget_version_before, movement.budget_version_after
FROM city_budget_movements movement
JOIN city_government_budget_lines budget ON budget.id = movement.budget_line_id
WHERE movement.world_id = $1 AND movement.settlement_id = $2
ORDER BY movement.line_no ASC`, worldID, settlementID)
	if err != nil {
		return fmt.Errorf("load replay budget movements: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var code string
		var before, after, versionBefore, versionAfter int64
		if err = rows.Scan(&code, &before, &after, &versionBefore, &versionAfter); err != nil {
			return err
		}
		position, ok := index[code]
		if !ok {
			return fmt.Errorf("budget movement references unknown budget")
		}
		budget := &state.Physical.BudgetLines[position]
		if budget.SpentUnits != before || budget.Version != versionBefore || versionAfter != versionBefore+1 {
			return fmt.Errorf("budget projection chain is broken")
		}
		budget.SpentUnits = after
		budget.Version = versionAfter
	}
	return rows.Err()
}

func canonicalCityHashState(state cityHashState) ([]byte, string, error) {
	raw, err := marshalCanonicalCityState(state)
	if err != nil {
		return nil, "", fmt.Errorf("marshal replayed city state: %w", err)
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), nil
}

func loadCityTickByNumber(ctx context.Context, queryer citySQLQueryer, worldID, tick int64) (*CityTick, error) {
	return scanCityTick(queryer.QueryRowContext(ctx, `
SELECT `+cityTickColumns+` FROM city_ticks t
WHERE t.world_id = $1 AND t.tick = $2`, worldID, tick))
}

func cityReplayFingerprint(fromTick, targetTick *int64) (string, error) {
	raw, err := json.Marshal(struct {
		Operation  string `json:"operation"`
		FromTick   *int64 `json:"from_tick"`
		TargetTick *int64 `json:"target_tick"`
	}{Operation: "city.replay.v1", FromTick: fromTick, TargetTick: targetTick})
	if err != nil {
		return "", ErrCityInvalidInput.WithCause(err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func loadCityReplayByRequest(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, userID int64,
	requestID string,
) (*CityReplayRun, error) {
	return scanCityReplayRun(queryer.QueryRowContext(ctx, `
SELECT `+cityReplayRunColumns+` FROM city_replay_runs r
WHERE r.world_id = $1 AND r.requested_by_user_id = $2 AND r.client_request_id = $3`,
		worldID, userID, requestID))
}

func scanCityReplayRun(scanner cityScannable) (*CityReplayRun, error) {
	item := &CityReplayRun{}
	var expected, actual, divergencePath, errorCode, errorDetail sql.NullString
	var divergenceTick sql.NullInt64
	var completedAt sql.NullTime
	if err := scanner.Scan(
		&item.ID, &item.WorldID, &item.RequestedByUserID, &item.ClientRequestID,
		&item.requestFingerprint, &item.BaseSnapshotID, &item.FromTick, &item.TargetTick,
		&item.Status, &expected, &actual, &item.VerifiedTickCount,
		&divergenceTick, &divergencePath, &errorCode, &errorDetail,
		&item.StartedAt, &completedAt,
	); err != nil {
		return nil, err
	}
	item.ExpectedStateHash = nullStringPointer(expected)
	item.ActualStateHash = nullStringPointer(actual)
	item.DivergenceTick = nullInt64Pointer(divergenceTick)
	item.DivergencePath = nullStringPointer(divergencePath)
	item.ErrorCode = nullStringPointer(errorCode)
	item.ErrorDetail = nullStringPointer(errorDetail)
	if completedAt.Valid {
		item.CompletedAt = &completedAt.Time
	}
	return item, nil
}

func cityAccountHashKey(entityType, entityCode, unitCode, templateCode string) string {
	return entityType + "\x00" + entityCode + "\x00" + unitCode + "\x00" + templateCode
}

func cityInventoryHashKey(entityCode, districtCode, resourceCode string) string {
	return entityCode + "\x00" + districtCode + "\x00" + resourceCode
}

func cityNullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func cityAuditDetail(detail string) string {
	detail = strings.Join(strings.Fields(detail), " ")
	runes := []rune(detail)
	if len(runes) <= 512 {
		return detail
	}
	return string(runes[:512])
}

func cityFirstJSONDifference(expected, actual []byte) string {
	decode := func(raw []byte) (any, error) {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		return value, nil
	}
	expectedValue, expectedErr := decode(expected)
	actualValue, actualErr := decode(actual)
	if expectedErr != nil || actualErr != nil {
		return "/"
	}
	if path, different := cityFirstValueDifference(expectedValue, actualValue, ""); different {
		if path == "" {
			return "/"
		}
		return path
	}
	return ""
}

func cityFirstValueDifference(expected, actual any, path string) (string, bool) {
	switch expectedValue := expected.(type) {
	case map[string]any:
		actualValue, ok := actual.(map[string]any)
		if !ok {
			return path, true
		}
		keys := make([]string, 0, len(expectedValue)+len(actualValue))
		seen := make(map[string]struct{}, len(expectedValue)+len(actualValue))
		for key := range expectedValue {
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
		for key := range actualValue {
			if _, exists := seen[key]; !exists {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			expectedChild, expectedOK := expectedValue[key]
			actualChild, actualOK := actualValue[key]
			childPath := path + "/" + cityJSONPointerEscape(key)
			if !expectedOK || !actualOK {
				return childPath, true
			}
			if difference, different := cityFirstValueDifference(expectedChild, actualChild, childPath); different {
				return difference, true
			}
		}
		return "", false
	case []any:
		actualValue, ok := actual.([]any)
		if !ok {
			return path, true
		}
		length := len(expectedValue)
		if len(actualValue) < length {
			length = len(actualValue)
		}
		for index := 0; index < length; index++ {
			childPath := path + "/" + strconv.Itoa(index)
			if difference, different := cityFirstValueDifference(expectedValue[index], actualValue[index], childPath); different {
				return difference, true
			}
		}
		if len(expectedValue) != len(actualValue) {
			return path + "/" + strconv.Itoa(length), true
		}
		return "", false
	case json.Number:
		actualValue, ok := actual.(json.Number)
		return path, !ok || expectedValue.String() != actualValue.String()
	case string:
		actualValue, ok := actual.(string)
		return path, !ok || expectedValue != actualValue
	case bool:
		actualValue, ok := actual.(bool)
		return path, !ok || expectedValue != actualValue
	case nil:
		return path, actual != nil
	default:
		return path, fmt.Sprint(expected) != fmt.Sprint(actual)
	}
}

func cityJSONPointerEscape(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

type CityRecoveryRun struct {
	ID                      int64      `json:"id"`
	WorldID                 int64      `json:"world_id"`
	RequestedByUserID       int64      `json:"requested_by_user_id"`
	ClientRequestID         string     `json:"client_request_id"`
	ReplayRunID             int64      `json:"replay_run_id"`
	TargetSnapshotID        int64      `json:"target_snapshot_id"`
	TargetTick              int64      `json:"target_tick"`
	Status                  string     `json:"status"`
	BeforeStateHash         *string    `json:"before_state_hash,omitempty"`
	TargetStateHash         string     `json:"target_state_hash"`
	AfterStateHash          *string    `json:"after_state_hash,omitempty"`
	RestoredProjectionCount int        `json:"restored_projection_count"`
	ErrorCode               *string    `json:"error_code,omitempty"`
	ErrorDetail             *string    `json:"error_detail,omitempty"`
	StartedAt               time.Time  `json:"started_at"`
	CompletedAt             *time.Time `json:"completed_at,omitempty"`
	requestFingerprint      string
}

type CityRecoveryInput struct {
	UserID         int64
	WorldID        int64
	IdempotencyKey string
	ReplayRunID    int64
}

type CityRecoveryRunPage struct {
	Items      []*CityRecoveryRun `json:"items"`
	NextCursor *int64             `json:"next_cursor,omitempty"`
}

const cityRecoveryRunColumns = `
r.id, r.world_id, r.requested_by_user_id, r.client_request_id,
r.request_fingerprint, r.replay_run_id, r.target_snapshot_id, r.target_tick,
r.status, r.before_state_hash, r.target_state_hash, r.after_state_hash,
r.restored_projection_count, r.error_code, r.error_detail,
r.started_at, r.completed_at`

func (s *CityEconomyService) StartRecovery(ctx context.Context, input CityRecoveryInput) (*CityRecoveryRun, error) {
	if input.UserID <= 0 || input.WorldID <= 0 || input.ReplayRunID <= 0 {
		return nil, ErrCityInvalidInput
	}
	requestID, err := requireCityIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	fingerprint, err := cityRecoveryFingerprint(input.ReplayRunID)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin city recovery transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, cityWorldLockKey(input.WorldID)); err != nil {
		return nil, fmt.Errorf("lock city recovery world: %w", err)
	}
	world, err := lockCityWorld(ctx, tx, input.UserID, input.WorldID)
	if err != nil {
		return nil, err
	}
	if world.memberRole != CityMemberRoleOwner {
		return nil, ErrCityPermissionDenied
	}
	existing, err := loadCityRecoveryByRequest(ctx, tx, input.WorldID, input.UserID, requestID)
	if err == nil {
		if existing.requestFingerprint != fingerprint {
			return nil, ErrCityRecoveryConflict
		}
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit city recovery idempotent read: %w", err)
		}
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("find city recovery request: %w", err)
	}
	replay, err := scanCityReplayRun(tx.QueryRowContext(ctx, `
SELECT `+cityReplayRunColumns+` FROM city_replay_runs r
WHERE r.world_id = $1 AND r.id = $2`, input.WorldID, input.ReplayRunID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityReplayNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load city recovery replay: %w", err)
	}
	if replay.Status != CityReplayStatusVerified || replay.TargetTick != world.currentTick ||
		replay.ExpectedStateHash == nil || replay.ActualStateHash == nil ||
		*replay.ExpectedStateHash != *replay.ActualStateHash {
		return nil, ErrCityRecoveryPrecondition
	}
	targetSnapshot, err := loadCitySnapshotByTick(ctx, tx, input.WorldID, world.currentTick)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySnapshotNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load city recovery snapshot: %w", err)
	}
	targetState, targetCanonical, err := verifyCitySnapshot(targetSnapshot)
	if err != nil {
		return nil, err
	}
	if targetSnapshot.StateHash != *replay.ExpectedStateHash ||
		targetState.CurrentTick != world.currentTick ||
		targetState.SimulationVersion != world.simulationVersion {
		return nil, ErrCityRecoveryPrecondition
	}
	var beforeHash *string
	if _, _, hash, hashErr := canonicalCityWorldState(ctx, tx, input.WorldID); hashErr == nil {
		beforeHash = stringPointer(hash)
	}
	run, err := scanCityRecoveryRun(tx.QueryRowContext(ctx, `
INSERT INTO city_recovery_runs AS r
    (world_id, requested_by_user_id, client_request_id, request_fingerprint,
     replay_run_id, target_snapshot_id, target_tick, before_state_hash, target_state_hash)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING `+cityRecoveryRunColumns,
		input.WorldID, input.UserID, requestID, fingerprint, replay.ID,
		targetSnapshot.ID, targetSnapshot.Tick, cityNullableString(beforeHash), targetSnapshot.StateHash))
	if err != nil {
		return nil, fmt.Errorf("insert city recovery run: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `SAVEPOINT city_projection_recovery`); err != nil {
		return nil, fmt.Errorf("create city recovery savepoint: %w", err)
	}
	restoredCount, restoreErr := restoreCityProjection(ctx, tx, input.WorldID, run.ID, targetState)
	var afterHash *string
	var failureCode, failureDetail *string
	if restoreErr == nil {
		_, actualCanonical, actualHash, hashErr := canonicalCityWorldState(ctx, tx, input.WorldID)
		if hashErr != nil {
			restoreErr = hashErr
		} else if actualHash != targetSnapshot.StateHash || !bytes.Equal(actualCanonical, targetCanonical) {
			path := cityFirstJSONDifference(targetCanonical, actualCanonical)
			restoreErr = fmt.Errorf("restored projection differs at %s", path)
		} else {
			afterHash = stringPointer(actualHash)
		}
	}
	if restoreErr == nil {
		if _, constraintErr := tx.ExecContext(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); constraintErr != nil {
			restoreErr = fmt.Errorf("validate restored projection constraints: %w", constraintErr)
		}
	}
	status := CityRecoveryStatusApplied
	if restoreErr != nil {
		if _, rollbackErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT city_projection_recovery`); rollbackErr != nil {
			return nil, fmt.Errorf("rollback failed city recovery: %w", rollbackErr)
		}
		status = CityRecoveryStatusFailed
		failureCode = stringPointer("PROJECTION_RESTORE_FAILED")
		failureDetail = stringPointer(cityAuditDetail(restoreErr.Error()))
		restoredCount = 0
	} else if _, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT city_projection_recovery`); err != nil {
		return nil, fmt.Errorf("release city recovery savepoint: %w", err)
	}
	completedAt := time.Now().UTC()
	run, err = scanCityRecoveryRun(tx.QueryRowContext(ctx, `
UPDATE city_recovery_runs AS r
SET status = $2, after_state_hash = $3, restored_projection_count = $4,
    error_code = $5, error_detail = $6, completed_at = $7
WHERE r.id = $1 AND r.status = 'running'
RETURNING `+cityRecoveryRunColumns,
		run.ID, status, cityNullableString(afterHash), restoredCount,
		cityNullableString(failureCode), cityNullableString(failureDetail), completedAt))
	if err != nil {
		return nil, fmt.Errorf("complete city recovery run: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city recovery run: %w", err)
	}
	return run, nil
}

func (s *CityEconomyService) GetRecovery(ctx context.Context, userID, worldID, runID int64) (*CityRecoveryRun, error) {
	if userID <= 0 || worldID <= 0 || runID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	item, err := scanCityRecoveryRun(s.db.QueryRowContext(ctx, `
SELECT `+cityRecoveryRunColumns+` FROM city_recovery_runs r
WHERE r.world_id = $1 AND r.id = $2`, worldID, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityRecoveryNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get city recovery run: %w", err)
	}
	return item, nil
}

func (s *CityEconomyService) ListRecoveries(ctx context.Context, input CityAuditRunListInput) (*CityRecoveryRunPage, error) {
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
SELECT `+cityRecoveryRunColumns+` FROM city_recovery_runs r
WHERE r.world_id = $1 AND r.id > $2
ORDER BY r.id ASC LIMIT $3`, input.WorldID, input.AfterID, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list city recovery runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*CityRecoveryRun, 0, input.Limit)
	for rows.Next() {
		item, scanErr := scanCityRecoveryRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city recovery runs: %w", err)
	}
	page := &CityRecoveryRunPage{Items: items}
	if len(items) > input.Limit {
		items = items[:input.Limit]
		page.Items = items
		next := items[len(items)-1].ID
		page.NextCursor = &next
	}
	return page, nil
}

func restoreCityProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID, recoveryRunID int64,
	state cityHashState,
) (int, error) {
	engine, engineErr := cityEngineForVersion(state.SimulationVersion)
	if state.CurrentTick < 0 || engineErr != nil {
		return 0, fmt.Errorf("recovery snapshot version is unsupported")
	}
	if _, err := tx.ExecContext(ctx, `SELECT set_config('sub2api.city_recovery_run_id', $1, TRUE)`, strconv.FormatInt(recoveryRunID, 10)); err != nil {
		return 0, fmt.Errorf("activate city recovery write gate: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `SELECT set_config('sub2api.city_f4_write', 'on', TRUE)`); err != nil {
		return 0, fmt.Errorf("activate city F4 recovery write gate: %w", err)
	}
	simulatedAt, err := time.Parse(time.RFC3339Nano, state.SimulatedAt)
	if err != nil {
		return 0, fmt.Errorf("parse recovery simulated time: %w", err)
	}
	_, targetHash, err := canonicalCityHashState(state)
	if err != nil {
		return 0, err
	}
	count := 0
	apply := func(label, query string, args ...any) error {
		result, execErr := tx.ExecContext(ctx, query, args...)
		if execErr != nil {
			return fmt.Errorf("restore %s: %w", label, execErr)
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil || rows != 1 {
			return fmt.Errorf("restore %s affected %d rows", label, rows)
		}
		count++
		return nil
	}
	if err = apply("world", `
UPDATE city_worlds
SET name = $2, status = $3, simulation_version = $4, seed = $5,
    current_tick = $6, simulated_at = $7, speed_multiplier = ($8::numeric / 1000),
    timezone = $9, settings = $10::jsonb, state_hash = $11, updated_at = NOW()
WHERE id = $1`, worldID, state.Name, state.Status, state.SimulationVersion, state.Seed,
		state.CurrentTick, simulatedAt, state.SpeedMilli, state.Timezone, []byte(state.Settings),
		targetHash); err != nil {
		return 0, err
	}
	for _, unit := range state.MonetaryUnits {
		if err = apply("monetary unit "+unit.Code, `
UPDATE city_monetary_units
SET name = $3, symbol = $4, scale = $5, status = $6, is_base = $7,
    metadata = $8::jsonb, updated_at = NOW()
WHERE world_id = $1 AND code = $2`, worldID, unit.Code, unit.Name, unit.Symbol,
			unit.Scale, unit.Status, unit.IsBase, []byte(unit.Metadata)); err != nil {
			return 0, err
		}
	}
	for _, template := range state.AccountTemplates {
		if err = apply("account template "+template.EntityType+"."+template.Code, `
UPDATE city_account_templates
SET name = $4, account_class = $5, normal_side = $6, allow_negative = $7,
    is_required = $8, sort_order = $9, metadata = $10::jsonb
WHERE world_id = $1 AND entity_type = $2 AND code = $3`,
			worldID, template.EntityType, template.Code, template.Name, template.AccountClass,
			template.NormalSide, template.AllowNegative, template.IsRequired,
			template.SortOrder, []byte(template.Metadata)); err != nil {
			return 0, err
		}
	}
	for _, entity := range state.Entities {
		if err = apply("entity "+entity.Code, `
UPDATE city_economic_entities
SET name = $4, status = $5, metadata = $6::jsonb, updated_at = NOW()
WHERE world_id = $1 AND entity_type = $2 AND code = $3`,
			worldID, entity.EntityType, entity.Code, entity.Name, entity.Status, []byte(entity.Metadata)); err != nil {
			return 0, err
		}
	}
	for _, account := range state.Accounts {
		if err = apply("account "+account.EntityCode+"."+account.TemplateCode, `
UPDATE city_accounts account
SET allow_negative = $6, current_balance_units = $7, version = $8,
    status = $9, metadata = $10::jsonb, updated_at = NOW()
FROM city_economic_entities entity, city_monetary_units unit, city_account_templates template
WHERE account.world_id = $1 AND entity.id = account.entity_id
  AND unit.id = account.monetary_unit_id AND template.id = account.template_id
  AND entity.entity_type = $2 AND entity.code = $3 AND unit.code = $4 AND template.code = $5`,
			worldID, account.EntityType, account.EntityCode, account.MonetaryUnitCode,
			account.TemplateCode, account.AllowNegative, account.CurrentBalanceUnit,
			account.Version, account.Status, []byte(account.Metadata)); err != nil {
			return 0, err
		}
	}
	if err = restoreCityPhysicalProjection(ctx, tx, worldID, state.SimulationVersion, state.Physical, apply); err != nil {
		return 0, err
	}
	if engine.hasStage(cityEngineStageCalendarDemography) {
		if err = restoreCityDemographyProjection(ctx, tx, worldID, state.Demography, apply); err != nil {
			return 0, err
		}
	}
	if err = restoreCityMarketProjection(ctx, tx, worldID, state.Markets, apply); err != nil {
		return 0, err
	}
	var worldRuntimeIDs worldRuntimeRecoveryIDs
	if engine.hasStage(cityEngineStageWorldRuntime) {
		worldRuntimeIDs, err = loadWorldRuntimeRecoveryIDs(ctx, tx, worldID)
		if err != nil {
			return 0, err
		}
		runtimeCount, runtimeErr := clearWorldRuntimeProjection(ctx, tx, worldID)
		if runtimeErr != nil {
			return 0, runtimeErr
		}
		count += runtimeCount
	}
	var enterpriseLocationFactIDs map[cityEnterpriseLocationRecoveryFactKey]int64
	if engine.hasStage(cityEngineStageEnterpriseLocation) {
		enterpriseLocationFactIDs, err = loadCityEnterpriseLocationRecoveryFactIDs(ctx, tx, worldID)
		if err != nil {
			return 0, err
		}
		enterpriseCount, enterpriseErr := clearCityEnterpriseLocationProjection(ctx, tx, worldID)
		if enterpriseErr != nil {
			return 0, enterpriseErr
		}
		count += enterpriseCount
	}
	var developmentFactIDs map[cityDevelopmentRecoveryFactKey]int64
	if engine.hasStage(cityEngineStageDevelopment) {
		developmentFactIDs, err = loadCityDevelopmentRecoveryFactIDs(ctx, tx, worldID)
		if err != nil {
			return 0, err
		}
		developmentCount, developmentErr := clearCityDevelopmentProjection(ctx, tx, worldID)
		if developmentErr != nil {
			return 0, developmentErr
		}
		count += developmentCount
	}
	if engine.hasStage(cityEngineStageSpatial) && cityEngineSupportsLand(state.SimulationVersion) {
		if err = validateCityLandSnapshot(state); err != nil {
			return 0, err
		}
		landCount, clearErr := clearCityLandProjection(ctx, tx, worldID)
		if clearErr != nil {
			return 0, clearErr
		}
		count += landCount
	}
	if engine.hasStage(cityEngineStageSpatial) {
		spatialCount, spatialErr := restoreCitySpatialProjection(ctx, tx, worldID, state)
		if spatialErr != nil {
			return 0, spatialErr
		}
		count += spatialCount
	}
	if cityEngineSupportsLand(state.SimulationVersion) {
		landCount, landErr := restoreCityLandProjection(ctx, tx, worldID, state)
		if landErr != nil {
			return 0, landErr
		}
		count += landCount
	}
	if engine.hasStage(cityEngineStageDevelopment) {
		developmentCount, developmentErr := restoreCityDevelopmentProjection(
			ctx, tx, worldID, &state, developmentFactIDs,
		)
		if developmentErr != nil {
			return 0, developmentErr
		}
		count += developmentCount
	}
	if engine.hasStage(cityEngineStageEnterpriseLocation) {
		enterpriseCount, enterpriseErr := restoreCityEnterpriseLocationProjection(
			ctx, tx, worldID, &state, enterpriseLocationFactIDs,
		)
		if enterpriseErr != nil {
			return 0, enterpriseErr
		}
		count += enterpriseCount
	}
	if engine.hasStage(cityEngineStageWorldRuntime) {
		runtimeCount, runtimeErr := restoreWorldRuntimeProjection(
			ctx, tx, worldID, &state, worldRuntimeIDs,
		)
		if runtimeErr != nil {
			return 0, runtimeErr
		}
		count += runtimeCount
	}
	return count, nil
}

func restoreCityPhysicalProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	simulationVersion string,
	state cityPhysicalHashState,
	apply func(string, string, ...any) error,
) error {
	for _, district := range state.Districts {
		if err := apply("district "+district.Code, `
UPDATE city_districts
SET name = $3, sort_order = $4, area_units = $5, developable_area_units = $6,
    residential_capacity_units = $7, commercial_capacity_units = $8,
    industrial_capacity_units = $9, metadata = $10::jsonb, updated_at = NOW()
WHERE world_id = $1 AND code = $2`, worldID, district.Code, district.Name,
			district.SortOrder, district.AreaUnits, district.DevelopableAreaUnits,
			district.ResidentialCapacity, district.CommercialCapacity,
			district.IndustrialCapacity, []byte(district.Metadata)); err != nil {
			return err
		}
	}
	for _, cohort := range state.HouseholdCohorts {
		if cityEngineSupportsHouseholdLifecycle(simulationVersion) {
			if err := apply("household cohort "+cohort.DistrictCode+"."+cohort.IncomeBand, `
UPDATE city_household_cohorts cohort_state
SET population_units = $5, working_age_units = $6, employed_units = $7,
    household_units = $8, housing_demand_units = $9, version = $10,
    metadata = $11::jsonb, updated_at = NOW()
FROM city_districts district, city_economic_entities entity
WHERE cohort_state.world_id = $1 AND district.id = cohort_state.district_id
  AND entity.id = cohort_state.entity_id AND district.code = $2
  AND entity.code = $3 AND cohort_state.income_band = $4`,
				worldID, cohort.DistrictCode, cohort.EntityCode, cohort.IncomeBand,
				cohort.PopulationUnits, cohort.WorkingAgeUnits, cohort.EmployedUnits,
				cohort.HouseholdUnits, cohort.HousingDemandUnits, cohort.Version,
				[]byte(cohort.Metadata)); err != nil {
				return err
			}
			continue
		}
		if err := apply("household cohort "+cohort.DistrictCode+"."+cohort.IncomeBand, `
UPDATE city_household_cohorts cohort_state
SET population_units = $5, working_age_units = $6, employed_units = $7,
    housing_demand_units = $8, version = $9, metadata = $10::jsonb, updated_at = NOW()
FROM city_districts district, city_economic_entities entity
WHERE cohort_state.world_id = $1 AND district.id = cohort_state.district_id
  AND entity.id = cohort_state.entity_id AND district.code = $2
  AND entity.code = $3 AND cohort_state.income_band = $4`,
			worldID, cohort.DistrictCode, cohort.EntityCode, cohort.IncomeBand,
			cohort.PopulationUnits, cohort.WorkingAgeUnits, cohort.EmployedUnits,
			cohort.HousingDemandUnits, cohort.Version, []byte(cohort.Metadata)); err != nil {
			return err
		}
	}
	for _, firm := range state.Firms {
		if err := apply("firm "+firm.EntityCode, `
UPDATE city_firm_states firm_state
SET industry_code = $4, employee_units = $5, capital_stock_units = $6,
    production_capacity_units = $7, productivity_milli = $8,
    version = $9, metadata = $10::jsonb, updated_at = NOW()
FROM city_economic_entities entity, city_districts district
WHERE firm_state.world_id = $1 AND entity.id = firm_state.entity_id
  AND district.id = firm_state.district_id AND entity.code = $2 AND district.code = $3`,
			worldID, firm.EntityCode, firm.DistrictCode, firm.IndustryCode,
			firm.EmployeeUnits, firm.CapitalStockUnits, firm.ProductionCapacityUnits,
			firm.ProductivityMilli, firm.Version, []byte(firm.Metadata)); err != nil {
			return err
		}
	}
	government := state.Government
	if err := apply("government "+government.EntityCode, `
UPDATE city_government_states government_state
SET administrative_capacity_units = $3, public_service_capacity_units = $4,
    version = $5, metadata = $6::jsonb, updated_at = NOW()
FROM city_economic_entities entity
WHERE government_state.world_id = $1 AND entity.id = government_state.entity_id
  AND entity.code = $2`, worldID, government.EntityCode,
		government.AdministrativeCapacityUnits, government.PublicServiceCapacityUnits,
		government.Version, []byte(government.Metadata)); err != nil {
		return err
	}
	for _, budget := range state.BudgetLines {
		if err := apply("budget "+budget.Code, `
UPDATE city_government_budget_lines budget_state
SET name = $5, appropriated_units = $6, committed_units = $7,
    spent_units = $8, version = $9, metadata = $10::jsonb, updated_at = NOW()
FROM city_economic_entities entity, city_monetary_units unit
WHERE budget_state.world_id = $1 AND entity.id = budget_state.government_entity_id
  AND unit.id = budget_state.monetary_unit_id AND entity.code = $2
  AND unit.code = $3 AND budget_state.code = $4`,
			worldID, budget.EntityCode, budget.MonetaryUnitCode, budget.Code, budget.Name,
			budget.AppropriatedUnits, budget.CommittedUnits, budget.SpentUnits,
			budget.Version, []byte(budget.Metadata)); err != nil {
			return err
		}
	}
	for _, resource := range state.Resources {
		if err := apply("resource "+resource.Code, `
UPDATE city_resources
SET name = $3, resource_kind = $4, unit_code = $5, unit_scale = $6,
    storable = $7, status = $8, metadata = $9::jsonb, updated_at = NOW()
WHERE world_id = $1 AND code = $2`, worldID, resource.Code, resource.Name,
			resource.ResourceKind, resource.UnitCode, resource.UnitScale,
			resource.Storable, resource.Status, []byte(resource.Metadata)); err != nil {
			return err
		}
	}
	for _, recipe := range state.Recipes {
		if err := apply("recipe "+recipe.Code, `
UPDATE city_production_recipes
SET name = $3, industry_code = $4, capacity_units_per_batch = $5,
    status = $6, metadata = $7::jsonb, updated_at = NOW()
WHERE world_id = $1 AND code = $2`, worldID, recipe.Code, recipe.Name,
			recipe.IndustryCode, recipe.CapacityUnitsPerBatch, recipe.Status,
			[]byte(recipe.Metadata)); err != nil {
			return err
		}
		for _, line := range recipe.Lines {
			if err := apply("recipe line "+recipe.Code+"."+line.ResourceCode+"."+line.Direction, `
UPDATE city_production_recipe_lines line
SET quantity_units = $5
FROM city_production_recipes recipe, city_resources resource
WHERE line.world_id = $1 AND recipe.id = line.recipe_id AND resource.id = line.resource_id
  AND recipe.code = $2 AND resource.code = $3 AND line.direction = $4`,
				worldID, recipe.Code, line.ResourceCode, line.Direction, line.QuantityUnits); err != nil {
				return err
			}
		}
	}
	for _, firmRecipe := range state.FirmRecipes {
		if err := apply("firm recipe "+firmRecipe.FirmEntityCode+"."+firmRecipe.RecipeCode, `
UPDATE city_firm_recipes firm_recipe
SET status = $4
FROM city_economic_entities entity, city_production_recipes recipe
WHERE firm_recipe.world_id = $1 AND entity.id = firm_recipe.firm_entity_id
  AND recipe.id = firm_recipe.recipe_id AND entity.code = $2 AND recipe.code = $3`,
			worldID, firmRecipe.FirmEntityCode, firmRecipe.RecipeCode, firmRecipe.Status); err != nil {
			return err
		}
	}
	for _, inventory := range state.Inventories {
		if err := apply("inventory "+inventory.EntityCode+"."+inventory.DistrictCode+"."+inventory.ResourceCode, `
UPDATE city_inventory_balances balance
SET opening_quantity_units = $6, quantity_units = $7, version = $8,
    status = $9, metadata = $10::jsonb, updated_at = NOW()
FROM city_economic_entities entity, city_districts district, city_resources resource
WHERE balance.world_id = $1 AND entity.id = balance.entity_id
  AND district.id = balance.district_id AND resource.id = balance.resource_id
  AND balance.entity_type = $2 AND entity.code = $3
  AND district.code = $4 AND resource.code = $5`,
			worldID, inventory.EntityType, inventory.EntityCode, inventory.DistrictCode,
			inventory.ResourceCode, inventory.OpeningQuantityUnits, inventory.QuantityUnits,
			inventory.Version, inventory.Status, []byte(inventory.Metadata)); err != nil {
			return err
		}
	}
	return nil
}

func restoreCityMarketProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	state cityMarketHashState,
	apply func(string, string, ...any) error,
) error {
	if err := apply("economic cycle", `
UPDATE city_economic_cycle_states
SET cycle_index = $2, cadence_ticks = $3, next_due_tick = $4,
    last_settled_tick = $5, version = $6, updated_at = NOW()
WHERE world_id = $1`, worldID, state.Cycle.CycleIndex, state.Cycle.CadenceTicks,
		state.Cycle.NextDueTick, cityNullableInt64(state.Cycle.LastSettledTick),
		state.Cycle.Version); err != nil {
		return err
	}
	if err := apply("economic policy", `
UPDATE city_economic_policies
SET labor_demand_capacity_milli = $2, goods_demand_population_divisor = $3,
    household_wage_tax_milli = $4, firm_sales_tax_milli = $5,
    procurement_share_milli = $6, social_support_share_milli = $7,
    version = $8, updated_at = NOW()
WHERE world_id = $1`, worldID, state.Policy.LaborDemandCapacityMilli,
		state.Policy.GoodsDemandPopulationDivisor, state.Policy.HouseholdWageTaxMilli,
		state.Policy.FirmSalesTaxMilli, state.Policy.ProcurementShareMilli,
		state.Policy.SocialSupportShareMilli, state.Policy.Version); err != nil {
		return err
	}
	for _, market := range state.Markets {
		if err := apply("market "+market.MarketCode, `
UPDATE city_market_states market_state
SET monetary_unit_id = unit.id, resource_id = resource.id,
    quote_units = $5, floor_units = $6, ceiling_units = $7,
    maximum_adjustment_milli = $8, last_clearing_tick = $9,
    last_clearing_price_units = $10, last_demand_units = $11,
    last_supply_units = $12, last_cleared_units = $13,
    last_unmet_demand_units = $14, last_excess_supply_units = $15,
    version = $16, updated_at = NOW()
FROM city_monetary_units unit
LEFT JOIN city_resources resource
  ON resource.world_id = $1 AND resource.code = $4
WHERE market_state.world_id = $1 AND market_state.market_code = $2
  AND unit.world_id = $1 AND unit.code = $3`,
			worldID, market.MarketCode, market.MonetaryUnitCode,
			cityNullableString(market.ResourceCode), market.QuoteUnits, market.FloorUnits,
			market.CeilingUnits, market.MaximumAdjustmentMilli,
			cityNullableInt64(market.LastClearingTick), cityNullableInt64(market.LastClearingPriceUnits),
			market.LastDemandUnits, market.LastSupplyUnits, market.LastClearedUnits,
			market.LastUnmetDemandUnits, market.LastExcessSupplyUnits, market.Version); err != nil {
			return err
		}
	}
	for _, occupancy := range state.Occupancies {
		if err := apply("occupancy "+occupancy.DistrictCode+"."+occupancy.IncomeBand, `
UPDATE city_housing_occupancies occupancy_state
SET occupied_units = $4, unmet_units = $5, rent_price_units = $6,
    last_settled_tick = $7, version = $8, updated_at = NOW()
FROM city_household_cohorts cohort, city_districts district
WHERE occupancy_state.world_id = $1 AND cohort.id = occupancy_state.cohort_id
  AND district.id = occupancy_state.district_id AND district.code = $2
  AND cohort.income_band = $3`, worldID, occupancy.DistrictCode, occupancy.IncomeBand,
			occupancy.OccupiedUnits, occupancy.UnmetUnits, occupancy.RentPriceUnits,
			cityNullableInt64(occupancy.LastSettledTick), occupancy.Version); err != nil {
			return err
		}
	}
	return nil
}

func cityRecoveryFingerprint(replayRunID int64) (string, error) {
	raw, err := json.Marshal(struct {
		Operation   string `json:"operation"`
		ReplayRunID int64  `json:"replay_run_id"`
	}{Operation: "city.recovery.v1", ReplayRunID: replayRunID})
	if err != nil {
		return "", ErrCityInvalidInput.WithCause(err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func loadCityRecoveryByRequest(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, userID int64,
	requestID string,
) (*CityRecoveryRun, error) {
	return scanCityRecoveryRun(queryer.QueryRowContext(ctx, `
SELECT `+cityRecoveryRunColumns+` FROM city_recovery_runs r
WHERE r.world_id = $1 AND r.requested_by_user_id = $2 AND r.client_request_id = $3`,
		worldID, userID, requestID))
}

func scanCityRecoveryRun(scanner cityScannable) (*CityRecoveryRun, error) {
	item := &CityRecoveryRun{}
	var beforeHash, afterHash, errorCode, errorDetail sql.NullString
	var completedAt sql.NullTime
	if err := scanner.Scan(
		&item.ID, &item.WorldID, &item.RequestedByUserID, &item.ClientRequestID,
		&item.requestFingerprint, &item.ReplayRunID, &item.TargetSnapshotID,
		&item.TargetTick, &item.Status, &beforeHash, &item.TargetStateHash,
		&afterHash, &item.RestoredProjectionCount, &errorCode, &errorDetail,
		&item.StartedAt, &completedAt,
	); err != nil {
		return nil, err
	}
	item.BeforeStateHash = nullStringPointer(beforeHash)
	item.AfterStateHash = nullStringPointer(afterHash)
	item.ErrorCode = nullStringPointer(errorCode)
	item.ErrorDetail = nullStringPointer(errorDetail)
	if completedAt.Valid {
		item.CompletedAt = &completedAt.Time
	}
	return item, nil
}
