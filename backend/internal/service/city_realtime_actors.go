package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
)

const (
	cityRealtimeActorRuntimeSchemaVersion = 1
	cityRealtimeActorBootstrapCount       = 6
	cityRealtimeActorInitialPatrolDelayUS = int64(5 * cityRealtimeTimeQuantumUS)
	cityRealtimeActorPatrolSpacingUS      = int64(2 * cityRealtimeTimeQuantumUS)
	cityRealtimeActorPatrolIntervalUS     = int64(12 * cityRealtimeTimeQuantumUS)
	cityRealtimeActorMaximumChunkSpan     = int64(8)
	cityRealtimeActorMaximumChunkAbs      = int64(1_000_000)
	cityRealtimeActorDefaultProjectionCap = 128
	cityRealtimeActorMaximumProjectionCap = 256

	cityRealtimeDueEventTypeActorPatrol = "system.realtime.actor_patrol"
)

var cityRealtimeActorAppearanceVariants = []string{
	"resident.ochre",
	"resident.teal",
	"resident.indigo",
	"resident.rose",
	"resident.slate",
	"resident.olive",
}

// CityRealtimeActorSnapshotInput selects a bounded, member-safe actor window.
// It intentionally has no user, ownership, control, personality, model, or
// agent-memory filters: those belong to later private control planes, never to
// the shared map projection.
type CityRealtimeActorSnapshotInput struct {
	UserID        int64
	WorldID       int64
	MinimumChunkX int64
	MaximumChunkX int64
	MinimumChunkY int64
	MaximumChunkY int64
	Z             int32
	Limit         int
}

// CityRealtimePublicActor contains only facts safe for every active member of
// a shared world to see. ActorCode and PublicLabel are simulation identifiers,
// not user account names or agent identities.
type CityRealtimePublicActor struct {
	ActorCode         string `json:"actor_code"`
	ActorKind         string `json:"actor_kind"`
	PublicLabel       string `json:"public_label"`
	AppearanceVariant string `json:"appearance_variant"`
	LifecycleStatus   string `json:"lifecycle_status"`
	X                 int64  `json:"x"`
	Y                 int64  `json:"y"`
	Z                 int32  `json:"z"`
	MotionState       string `json:"motion_state"`
	PositionRevision  int64  `json:"position_revision"`
	LastFrameSequence int64  `json:"last_frame_sequence"`
}

// CityRealtimeActorSnapshot is the shared dynamic-overlay contract for the
// pixel renderer. It deliberately reuses the world's temporal cursor instead
// of inventing an independent client-side actor clock.
type CityRealtimeActorSnapshot struct {
	WorldID               int64                     `json:"world_id"`
	TimelineFrameSequence int64                     `json:"timeline_frame_sequence"`
	TimelineCursor        string                    `json:"timeline_cursor"`
	StaticProjectionHash  string                    `json:"static_projection_hash"`
	ProjectionScopeEpoch  int64                     `json:"projection_scope_epoch"`
	MinimumChunkX         int64                     `json:"minimum_chunk_x"`
	MaximumChunkX         int64                     `json:"maximum_chunk_x"`
	MinimumChunkY         int64                     `json:"minimum_chunk_y"`
	MaximumChunkY         int64                     `json:"maximum_chunk_y"`
	Z                     int32                     `json:"z"`
	ActorProjectionHash   string                    `json:"actor_projection_hash"`
	Actors                []CityRealtimePublicActor `json:"actors"`
}

type cityRealtimeActorIdentity struct {
	ActorCode          string
	ActorKind          string
	PublicLabel        string
	AppearanceVariant  string
	LifecycleStatus    string
	SpawnX             int64
	SpawnY             int64
	SpawnZ             int32
	SpawnFrameSequence int64
	IdentityHash       string
}

type cityRealtimeActorState struct {
	ActorCode         string
	X                 int64
	Y                 int64
	Z                 int32
	MotionState       string
	PositionRevision  int64
	LastFrameSequence int64
	StateHash         string
	EventChainHash    string
}

type cityRealtimeActorHash struct {
	ActorCode          string `json:"actor_code"`
	ActorKind          string `json:"actor_kind"`
	PublicLabel        string `json:"public_label"`
	AppearanceVariant  string `json:"appearance_variant"`
	LifecycleStatus    string `json:"lifecycle_status"`
	SpawnX             int64  `json:"spawn_x"`
	SpawnY             int64  `json:"spawn_y"`
	SpawnZ             int32  `json:"spawn_z"`
	SpawnFrameSequence int64  `json:"spawn_frame_sequence"`
	IdentityHash       string `json:"identity_hash"`
	X                  int64  `json:"x"`
	Y                  int64  `json:"y"`
	Z                  int32  `json:"z"`
	MotionState        string `json:"motion_state"`
	PositionRevision   int64  `json:"position_revision"`
	LastFrameSequence  int64  `json:"last_frame_sequence"`
	StateHash          string `json:"state_hash"`
	EventChainHash     string `json:"event_chain_hash"`
}

// cityRealtimeActorHashState stays bounded even after long-running worlds:
// each actor state carries the head of its append-only event hash chain, while
// history is kept in city_realtime_actor_position_events for audit/replay.
type cityRealtimeActorHashState struct {
	SchemaVersion int                     `json:"schema_version"`
	Actors        []cityRealtimeActorHash `json:"actors"`
}

type cityRealtimeActorSpawnCandidate struct {
	X int64
	Y int64
	Z int32
}

type cityRealtimeActorPatrolPayload struct {
	ActorCode string `json:"actor_code"`
	Step      int64  `json:"step"`
}

type cityRealtimeActorPositionEventInput struct {
	ActorCode         string
	EventSequence     int64
	FrameSequence     int64
	EventKind         string
	PortalCode        string
	From              *cityRealtimeActorSpawnCandidate
	To                cityRealtimeActorSpawnCandidate
	MotionState       string
	PreviousEventHash *string
}

// initializeCityRealtimeActorFoundation seeds a small, deterministic NPC
// cohort on server-authored traversable cells. It runs only while the new
// world has no genesis hash; the database trigger closes the same tables as
// soon as that genesis transaction commits.
func initializeCityRealtimeActorFoundation(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	simulationVersion string,
) error {
	if worldID <= 0 || !cityEngineSupportsRealtimeStaticWorldgen(simulationVersion) {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	binding, err := loadCityRealtimeSpatialBinding(ctx, tx, worldID)
	if err != nil {
		return err
	}
	candidates, err := loadCityRealtimeActorSpawnCandidates(ctx, tx, worldID)
	if err != nil {
		return err
	}
	if len(candidates) < cityRealtimeActorBootstrapCount {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_actor_spawn_candidates"})
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_realtime_actor_initialize_world_id', $1, TRUE)`, strconv.FormatInt(worldID, 10)); err != nil {
		return fmt.Errorf("activate realtime actor initialization gate: %w", err)
	}

	identityStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_realtime_actor_identities
    (world_id, actor_code, actor_kind, public_label, appearance_variant,
     lifecycle_status, spawn_x, spawn_y, spawn_z, spawn_frame_sequence,
     identity_hash, metadata)
VALUES ($1, $2, $3, $4, $5, 'active', $6, $7, $8, 0, $9, '{}'::jsonb)`)
	if err != nil {
		return fmt.Errorf("prepare realtime actor identity insert: %w", err)
	}
	defer func() { _ = identityStatement.Close() }()
	stateStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_realtime_actor_states
    (world_id, actor_code, x, y, z, motion_state, position_revision,
     last_frame_sequence, state_hash, event_chain_hash, metadata)
VALUES ($1, $2, $3, $4, $5, 'idle', 1, 0, $6, $7, '{}'::jsonb)`)
	if err != nil {
		return fmt.Errorf("prepare realtime actor state insert: %w", err)
	}
	defer func() { _ = stateStatement.Close() }()
	eventStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_realtime_actor_position_events
    (world_id, actor_code, event_sequence, frame_sequence, event_kind,
     from_x, from_y, from_z, to_x, to_y, to_z, motion_state,
     public_visibility, previous_event_hash, event_hash, metadata)
VALUES ($1, $2, 0, 0, 'spawn', NULL, NULL, NULL, $3, $4, $5, 'idle',
        TRUE, NULL, $6, '{}'::jsonb)`)
	if err != nil {
		return fmt.Errorf("prepare realtime actor spawn event insert: %w", err)
	}
	defer func() { _ = eventStatement.Close() }()

	for index := 0; index < cityRealtimeActorBootstrapCount; index++ {
		candidate := cityRealtimeActorBootstrapCandidate(binding.Seed, index, candidates)
		identity := cityRealtimeActorIdentity{
			ActorCode:          fmt.Sprintf("npc.resident.%02d", index+1),
			ActorKind:          "npc",
			PublicLabel:        fmt.Sprintf("Resident %02d", index+1),
			AppearanceVariant:  cityRealtimeActorAppearanceVariants[index%len(cityRealtimeActorAppearanceVariants)],
			LifecycleStatus:    "active",
			SpawnX:             candidate.X,
			SpawnY:             candidate.Y,
			SpawnZ:             candidate.Z,
			SpawnFrameSequence: 0,
		}
		identityHash, hashErr := cityRealtimeActorIdentityHash(identity)
		if hashErr != nil {
			return hashErr
		}
		identity.IdentityHash = identityHash
		spawnEventHash, hashErr := cityRealtimeActorPositionEventHash(cityRealtimeActorPositionEventInput{
			ActorCode: identity.ActorCode, EventSequence: 0, FrameSequence: 0,
			EventKind: "spawn", To: candidate, MotionState: "idle",
		})
		if hashErr != nil {
			return hashErr
		}
		state := cityRealtimeActorState{
			ActorCode: identity.ActorCode, X: candidate.X, Y: candidate.Y, Z: candidate.Z,
			MotionState: "idle", PositionRevision: 1, LastFrameSequence: 0,
			EventChainHash: spawnEventHash,
		}
		stateHash, hashErr := cityRealtimeActorStateHash(state)
		if hashErr != nil {
			return hashErr
		}
		state.StateHash = stateHash
		if _, err = identityStatement.ExecContext(ctx, worldID, identity.ActorCode, identity.ActorKind,
			identity.PublicLabel, identity.AppearanceVariant, identity.SpawnX, identity.SpawnY,
			identity.SpawnZ, identity.IdentityHash); err != nil {
			return fmt.Errorf("insert realtime actor identity %s: %w", identity.ActorCode, err)
		}
		if _, err = stateStatement.ExecContext(ctx, worldID, state.ActorCode, state.X, state.Y, state.Z,
			state.StateHash, state.EventChainHash); err != nil {
			return fmt.Errorf("insert realtime actor state %s: %w", identity.ActorCode, err)
		}
		if _, err = eventStatement.ExecContext(ctx, worldID, identity.ActorCode, candidate.X, candidate.Y,
			candidate.Z, spawnEventHash); err != nil {
			return fmt.Errorf("insert realtime actor spawn event %s: %w", identity.ActorCode, err)
		}
		dueWorldTimeUS := cityRealtimeActorInitialPatrolDelayUS + int64(index)*cityRealtimeActorPatrolSpacingUS
		if err = scheduleCityRealtimeActorPatrolDueEvent(ctx, tx, worldID, identity.ActorCode,
			dueWorldTimeUS, 1, state.PositionRevision, 0); err != nil {
			return err
		}
	}
	nextDueAtWorldTimeUS, err := cityRealtimeNextPendingDue(ctx, tx, worldID)
	if err != nil {
		return err
	}
	if nextDueAtWorldTimeUS == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_actor_initial_due"})
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE city_world_time_states
SET next_due_at_world_time_us = $2, updated_at = NOW()
WHERE world_id = $1`, worldID, *nextDueAtWorldTimeUS); err != nil {
		return fmt.Errorf("attach realtime actor initial due time: %w", err)
	}
	return nil
}

func loadCityRealtimeActorSpawnCandidates(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) ([]cityRealtimeActorSpawnCandidate, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT chunk_x, chunk_y, payload
FROM city_realtime_spatial_chunks
WHERE world_id = $1 AND z = $2
ORDER BY chunk_y ASC, chunk_x ASC`, worldID, cityspatial.SurfaceZ)
	if err != nil {
		return nil, fmt.Errorf("load realtime actor spawn chunks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	primary := make([]cityRealtimeActorSpawnCandidate, 0)
	fallback := make([]cityRealtimeActorSpawnCandidate, 0)
	for rows.Next() {
		var chunkX, chunkY int64
		var rawPayload []byte
		if err = rows.Scan(&chunkX, &chunkY, &rawPayload); err != nil {
			return nil, err
		}
		var payload cityspatial.OpenWorldChunkPayload
		if err = json.Unmarshal(rawPayload, &payload); err != nil {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_actor_spawn_payload"}).WithCause(err)
		}
		if err = cityspatial.ValidateOpenWorldChunkPayload(payload); err != nil {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_actor_spawn_payload"}).WithCause(err)
		}
		blocked := make(map[int]struct{})
		for _, layer := range payload.Layers {
			if layer.Kind == cityspatial.RuleKindStructure {
				blocked[int(layer.Y)*payload.Width+int(layer.X)] = struct{}{}
			}
		}
		cellIndex := 0
		for _, run := range payload.TerrainRuns {
			for offset := 0; offset < run.Length; offset++ {
				index := cellIndex + offset
				if _, isBlocked := blocked[index]; isBlocked {
					continue
				}
				candidate := cityRealtimeActorSpawnCandidate{
					X: chunkX*int64(payload.Width) + int64(index%payload.Width),
					Y: chunkY*int64(payload.Height) + int64(index/payload.Width),
					Z: cityspatial.SurfaceZ,
				}
				switch run.DefinitionID {
				case "terrain.road", "terrain.sidewalk":
					primary = append(primary, candidate)
				case "terrain.grass", "terrain.ground", "terrain.soil":
					fallback = append(fallback, candidate)
				}
			}
			cellIndex += run.Length
		}
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime actor spawn chunks: %w", err)
	}
	if len(primary) >= cityRealtimeActorBootstrapCount {
		return primary, nil
	}
	primary = append(primary, fallback...)
	if len(primary) == 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_actor_spawn_candidates"})
	}
	return primary, nil
}

func cityRealtimeActorBootstrapCandidate(seed int64, actorIndex int, candidates []cityRealtimeActorSpawnCandidate) cityRealtimeActorSpawnCandidate {
	if len(candidates) == 0 {
		return cityRealtimeActorSpawnCandidate{}
	}
	start := cityRealtimeActorStableIndex(seed, "bootstrap", len(candidates))
	stride := 1
	if len(candidates) > 1 {
		stride += cityRealtimeActorStableIndex(seed, "bootstrap-stride", len(candidates)-1)
		for cityRealtimeActorGreatestCommonDivisor(stride, len(candidates)) != 1 {
			stride++
		}
	}
	return candidates[(start+actorIndex*stride)%len(candidates)]
}

func cityRealtimeActorGreatestCommonDivisor(left, right int) int {
	for right != 0 {
		left, right = right, left%right
	}
	if left < 0 {
		return -left
	}
	return left
}

func cityRealtimeActorStableIndex(seed int64, discriminator string, modulus int) int {
	if modulus <= 0 {
		return 0
	}
	hasher := sha256.New()
	writeCityHashString(hasher, "city-realtime-actor-v1")
	writeCityHashInt64(hasher, seed)
	writeCityHashString(hasher, discriminator)
	sum := hasher.Sum(nil)
	return int(binary.BigEndian.Uint64(sum[:8]) % uint64(modulus))
}

func cityRealtimeActorIdentityHash(identity cityRealtimeActorIdentity) (string, error) {
	_, hash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version":       cityRealtimeActorRuntimeSchemaVersion,
		"actor_code":           identity.ActorCode,
		"actor_kind":           identity.ActorKind,
		"public_label":         identity.PublicLabel,
		"appearance_variant":   identity.AppearanceVariant,
		"lifecycle_status":     identity.LifecycleStatus,
		"spawn_x":              identity.SpawnX,
		"spawn_y":              identity.SpawnY,
		"spawn_z":              identity.SpawnZ,
		"spawn_frame_sequence": identity.SpawnFrameSequence,
	})
	if err != nil {
		return "", fmt.Errorf("canonicalize realtime actor identity: %w", err)
	}
	return hash, nil
}

func cityRealtimeActorStateHash(state cityRealtimeActorState) (string, error) {
	_, hash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version":      cityRealtimeActorRuntimeSchemaVersion,
		"actor_code":          state.ActorCode,
		"x":                   state.X,
		"y":                   state.Y,
		"z":                   state.Z,
		"motion_state":        state.MotionState,
		"position_revision":   state.PositionRevision,
		"last_frame_sequence": state.LastFrameSequence,
		"event_chain_hash":    state.EventChainHash,
	})
	if err != nil {
		return "", fmt.Errorf("canonicalize realtime actor state: %w", err)
	}
	return hash, nil
}

func cityRealtimeActorPositionEventHash(input cityRealtimeActorPositionEventInput) (string, error) {
	value := map[string]any{
		"schema_version": cityRealtimeActorRuntimeSchemaVersion,
		"actor_code":     input.ActorCode,
		"event_sequence": input.EventSequence,
		"frame_sequence": input.FrameSequence,
		"event_kind":     input.EventKind,
		"to_x":           input.To.X,
		"to_y":           input.To.Y,
		"to_z":           input.To.Z,
		"motion_state":   input.MotionState,
	}
	if input.PortalCode != "" {
		value["portal_code"] = input.PortalCode
	}
	if input.From != nil {
		value["from_x"] = input.From.X
		value["from_y"] = input.From.Y
		value["from_z"] = input.From.Z
	}
	if input.PreviousEventHash != nil {
		value["previous_event_hash"] = *input.PreviousEventHash
	}
	_, hash, err := cityRealtimeCanonicalJSONObject(value)
	if err != nil {
		return "", fmt.Errorf("canonicalize realtime actor position event: %w", err)
	}
	return hash, nil
}

func loadCityRealtimeActorHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeActorHashState, error) {
	state := &cityRealtimeActorHashState{
		SchemaVersion: cityRealtimeActorRuntimeSchemaVersion,
		Actors:        make([]cityRealtimeActorHash, 0),
	}
	var missing int
	if err := queryer.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_realtime_actor_identities identity
LEFT JOIN city_realtime_actor_states state
  ON state.world_id = identity.world_id AND state.actor_code = identity.actor_code
WHERE identity.world_id = $1 AND state.actor_code IS NULL`, worldID).Scan(&missing); err != nil {
		return nil, fmt.Errorf("check realtime actor state completeness: %w", err)
	}
	if missing != 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_actor_state"})
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT identity.actor_code, identity.actor_kind, identity.public_label,
       identity.appearance_variant, identity.lifecycle_status,
       identity.spawn_x, identity.spawn_y, identity.spawn_z,
       identity.spawn_frame_sequence, identity.identity_hash,
       state.x, state.y, state.z, state.motion_state, state.position_revision,
       state.last_frame_sequence, state.state_hash, state.event_chain_hash
FROM city_realtime_actor_identities identity
JOIN city_realtime_actor_states state
  ON state.world_id = identity.world_id AND state.actor_code = identity.actor_code
WHERE identity.world_id = $1
ORDER BY identity.actor_code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load realtime actor hash state: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var item cityRealtimeActorHash
		if err = rows.Scan(
			&item.ActorCode, &item.ActorKind, &item.PublicLabel, &item.AppearanceVariant,
			&item.LifecycleStatus, &item.SpawnX, &item.SpawnY, &item.SpawnZ,
			&item.SpawnFrameSequence, &item.IdentityHash, &item.X, &item.Y, &item.Z,
			&item.MotionState, &item.PositionRevision, &item.LastFrameSequence,
			&item.StateHash, &item.EventChainHash,
		); err != nil {
			return nil, err
		}
		if err = validateCityRealtimeActorHash(item); err != nil {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_actor_hash"}).WithCause(err)
		}
		state.Actors = append(state.Actors, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime actor hash state: %w", err)
	}
	if err = validateCityRealtimeActorHashState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_actor_hash_state"}).WithCause(err)
	}
	return state, nil
}

func validateCityRealtimeActorHashState(state *cityRealtimeActorHashState) error {
	if state == nil || state.SchemaVersion != cityRealtimeActorRuntimeSchemaVersion || state.Actors == nil {
		return fmt.Errorf("invalid realtime actor hash state")
	}
	for index := range state.Actors {
		if err := validateCityRealtimeActorHash(state.Actors[index]); err != nil {
			return err
		}
		if index > 0 && state.Actors[index-1].ActorCode >= state.Actors[index].ActorCode {
			return fmt.Errorf("realtime actors are not in stable canonical order")
		}
	}
	return nil
}

func validateCityRealtimeActorHash(item cityRealtimeActorHash) error {
	identity := cityRealtimeActorIdentity{
		ActorCode: item.ActorCode, ActorKind: item.ActorKind, PublicLabel: item.PublicLabel,
		AppearanceVariant: item.AppearanceVariant, LifecycleStatus: item.LifecycleStatus,
		SpawnX: item.SpawnX, SpawnY: item.SpawnY, SpawnZ: item.SpawnZ,
		SpawnFrameSequence: item.SpawnFrameSequence, IdentityHash: item.IdentityHash,
	}
	if !cityRealtimeActorIdentityValid(identity) || !cityRealtimeSHA256Hex(item.IdentityHash) {
		return fmt.Errorf("invalid realtime actor identity")
	}
	expectedIdentityHash, err := cityRealtimeActorIdentityHash(identity)
	if err != nil || expectedIdentityHash != item.IdentityHash {
		return fmt.Errorf("realtime actor identity hash mismatch")
	}
	state := cityRealtimeActorState{
		ActorCode: item.ActorCode, X: item.X, Y: item.Y, Z: item.Z,
		MotionState: item.MotionState, PositionRevision: item.PositionRevision,
		LastFrameSequence: item.LastFrameSequence, StateHash: item.StateHash,
		EventChainHash: item.EventChainHash,
	}
	if !cityRealtimeActorStateValid(state) || !cityRealtimeSHA256Hex(item.StateHash) || !cityRealtimeSHA256Hex(item.EventChainHash) {
		return fmt.Errorf("invalid realtime actor state")
	}
	expectedStateHash, err := cityRealtimeActorStateHash(state)
	if err != nil || expectedStateHash != item.StateHash {
		return fmt.Errorf("realtime actor state hash mismatch")
	}
	return nil
}

func cityRealtimeActorIdentityValid(identity cityRealtimeActorIdentity) bool {
	if !cityRealtimeDueEventIdentifierValid(identity.ActorCode, 96) ||
		!cityRealtimeDueEventIdentifierValid(identity.AppearanceVariant, 64) ||
		!cityRealtimeActorPublicLabelValid(identity.PublicLabel) ||
		identity.SpawnFrameSequence < 0 {
		return false
	}
	switch identity.ActorKind {
	case "npc", "character", "service":
	default:
		return false
	}
	switch identity.LifecycleStatus {
	case "active", "inactive", "retired":
	default:
		return false
	}
	return true
}

// cityRealtimeActorPublicLabelValid deliberately accepts a small Unicode
// name alphabet so Chinese, Japanese, and other ordinary player names render
// correctly, while still refusing delimiters that can turn a public map label
// into an account identifier, URL, path, or markup-like payload. The SQL
// migration keeps an independent coarse fence; this is the canonical service
// rule used before a label enters the actor hash chain.
func cityRealtimeActorPublicLabelValid(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || utf8.RuneCountInString(value) > 64 {
		return false
	}
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsNumber(character) {
			continue
		}
		switch character {
		case ' ', '.', '_', '-', '\'', '·', '・':
			continue
		default:
			return false
		}
	}
	return true
}

func cityRealtimeActorStateValid(state cityRealtimeActorState) bool {
	if !cityRealtimeDueEventIdentifierValid(state.ActorCode, 96) || state.PositionRevision <= 0 ||
		state.LastFrameSequence < 0 || !cityRealtimeSHA256Hex(state.EventChainHash) {
		return false
	}
	switch state.MotionState {
	case "idle", "walking", "inside", "unavailable":
		return true
	default:
		return false
	}
}

func scheduleCityRealtimeActorPatrolDueEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	actorCode string,
	dueWorldTimeUS, step, expectedRevision, createdFrameSequence int64,
) error {
	if worldID <= 0 || !cityRealtimeDueEventIdentifierValid(actorCode, 96) ||
		dueWorldTimeUS <= 0 || dueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 ||
		step <= 0 || expectedRevision <= 0 || createdFrameSequence < 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_actor_patrol_schedule"})
	}
	payload, payloadHash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"actor_code": actorCode,
		"step":       step,
	})
	if err != nil {
		return fmt.Errorf("canonicalize realtime actor patrol payload: %w", err)
	}
	dedupKey := fmt.Sprintf("actor.patrol.%s.%012d", actorCode, step)
	if !cityRealtimeDueEventIdentifierValid(dedupKey, 160) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_actor_patrol_dedup"})
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_due_events
    (world_id, event_type, schema_version, due_world_time_us, temporal_phase,
     priority, aggregate_type, aggregate_key, dedup_key, source_kind,
     source_reference, payload, payload_hash, expected_version, status,
     created_frame_sequence)
VALUES ($1, $2, 1, $3, 'movement', 100, 'realtime_actor', $4, $5, 'system',
        'realtime_actor_runtime', $6::jsonb, $7, $8, 'pending', $9)`,
		worldID, cityRealtimeDueEventTypeActorPatrol, dueWorldTimeUS,
		"actor:"+actorCode, dedupKey, []byte(payload), payloadHash,
		expectedRevision, createdFrameSequence,
	); err != nil {
		return fmt.Errorf("schedule realtime actor patrol %s: %w", actorCode, err)
	}
	return nil
}

// applyCityRealtimeActorPatrolDueEvent is deliberately internal to the
// server-owned temporal reducer. A browser cannot select a route, actor,
// target, time, or source; it can only observe the committed shared state.
func applyCityRealtimeActorPatrolDueEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID, frameSequence int64,
	event cityRealtimeDueEventRecord,
) (bool, error) {
	if event.EventType != cityRealtimeDueEventTypeActorPatrol || event.SchemaVersion != 1 ||
		event.SourceKind != cityRealtimeDueEventSourceKindSystem ||
		event.TemporalPhase != "movement" || event.AggregateType != "realtime_actor" ||
		event.SourceReference != "realtime_actor_runtime" || event.ExpectedVersion == nil {
		return false, nil
	}
	payload := cityRealtimeActorPatrolPayload{}
	if err := json.Unmarshal(event.Payload, &payload); err != nil ||
		!cityRealtimeDueEventIdentifierValid(payload.ActorCode, 96) || payload.Step <= 0 ||
		event.AggregateKey != "actor:"+payload.ActorCode {
		return false, nil
	}

	identity := cityRealtimeActorIdentity{}
	state := cityRealtimeActorState{}
	err := tx.QueryRowContext(ctx, `
SELECT identity.actor_code, identity.actor_kind, identity.public_label,
       identity.appearance_variant, identity.lifecycle_status,
       identity.spawn_x, identity.spawn_y, identity.spawn_z,
       identity.spawn_frame_sequence, identity.identity_hash,
       state.x, state.y, state.z, state.motion_state, state.position_revision,
       state.last_frame_sequence, state.state_hash, state.event_chain_hash
FROM city_realtime_actor_identities identity
JOIN city_realtime_actor_states state
  ON state.world_id = identity.world_id AND state.actor_code = identity.actor_code
WHERE identity.world_id = $1 AND identity.actor_code = $2
FOR UPDATE OF state`, worldID, payload.ActorCode).Scan(
		&identity.ActorCode, &identity.ActorKind, &identity.PublicLabel, &identity.AppearanceVariant,
		&identity.LifecycleStatus, &identity.SpawnX, &identity.SpawnY, &identity.SpawnZ,
		&identity.SpawnFrameSequence, &identity.IdentityHash, &state.X, &state.Y, &state.Z,
		&state.MotionState, &state.PositionRevision, &state.LastFrameSequence,
		&state.StateHash, &state.EventChainHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock realtime actor patrol state: %w", err)
	}
	state.ActorCode = identity.ActorCode
	if identity.LifecycleStatus != "active" || !cityRealtimeActorIdentityValid(identity) ||
		!cityRealtimeActorStateValid(state) || *event.ExpectedVersion != state.PositionRevision {
		return false, nil
	}
	if expectedStateHash, hashErr := cityRealtimeActorStateHash(state); hashErr != nil || expectedStateHash != state.StateHash {
		return false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_actor_state_hash"})
	}
	canPatrol, err := cityRealtimeNPCAgentCanPatrol(ctx, tx, worldID, state.ActorCode)
	if err != nil {
		return false, err
	}
	next := cityRealtimeActorSpawnCandidate{X: state.X, Y: state.Y, Z: state.Z}
	moved := false
	if canPatrol {
		candidates, candidateErr := loadCityRealtimeActorSpawnCandidates(ctx, tx, worldID)
		if candidateErr != nil {
			return false, candidateErr
		}
		next, moved, err = selectCityRealtimeActorPatrolTarget(ctx, tx, worldID, state, payload.Step, candidates)
		if err != nil {
			return false, err
		}
	}
	if !moved {
		// A full world never silently invents a conflicting move. Treating the
		// event as applied but retaining the actor at rest keeps the queue live
		// and lets a later patrol retry after other actors have moved.
		next = cityRealtimeActorSpawnCandidate{X: state.X, Y: state.Y, Z: state.Z}
	}
	nextFrameSequence := frameSequence
	nextRevision := state.PositionRevision + 1
	nextMotionState := "walking"
	if !moved {
		nextMotionState = "idle"
	}
	previousEventHash := state.EventChainHash
	eventHash, err := cityRealtimeActorPositionEventHash(cityRealtimeActorPositionEventInput{
		ActorCode: state.ActorCode, EventSequence: nextRevision - 1, FrameSequence: nextFrameSequence,
		EventKind: "move", From: &cityRealtimeActorSpawnCandidate{X: state.X, Y: state.Y, Z: state.Z},
		To: next, MotionState: nextMotionState, PreviousEventHash: &previousEventHash,
	})
	if err != nil {
		return false, err
	}
	nextState := cityRealtimeActorState{
		ActorCode: state.ActorCode, X: next.X, Y: next.Y, Z: next.Z,
		MotionState: nextMotionState, PositionRevision: nextRevision,
		LastFrameSequence: nextFrameSequence, EventChainHash: eventHash,
	}
	nextState.StateHash, err = cityRealtimeActorStateHash(nextState)
	if err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_realtime_actor_mutation_world_id', $1, TRUE)`, strconv.FormatInt(worldID, 10)); err != nil {
		return false, fmt.Errorf("activate realtime actor mutation gate: %w", err)
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_realtime_actor_mutation_frame_sequence', $1, TRUE)`, strconv.FormatInt(frameSequence, 10)); err != nil {
		return false, fmt.Errorf("activate realtime actor mutation frame gate: %w", err)
	}
	updateResult, err := tx.ExecContext(ctx, `
UPDATE city_realtime_actor_states
SET x = $3, y = $4, z = $5, motion_state = $6,
    position_revision = $7, last_frame_sequence = $8,
    state_hash = $9, event_chain_hash = $10, updated_at = NOW()
WHERE world_id = $1 AND actor_code = $2
  AND position_revision = $11 AND last_frame_sequence = $12`,
		worldID, state.ActorCode, nextState.X, nextState.Y, nextState.Z,
		nextState.MotionState, nextState.PositionRevision, nextState.LastFrameSequence,
		nextState.StateHash, nextState.EventChainHash,
		state.PositionRevision, state.LastFrameSequence,
	)
	if err != nil {
		return false, fmt.Errorf("advance realtime actor patrol state: %w", err)
	}
	if affected, affectedErr := updateResult.RowsAffected(); affectedErr != nil {
		return false, fmt.Errorf("check realtime actor patrol state advance: %w", affectedErr)
	} else if affected != 1 {
		return false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_actor_state_version"})
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_actor_position_events
    (world_id, actor_code, event_sequence, frame_sequence, event_kind,
     from_x, from_y, from_z, to_x, to_y, to_z, motion_state,
     public_visibility, previous_event_hash, event_hash, metadata)
VALUES ($1, $2, $3, $4, 'move', $5, $6, $7, $8, $9, $10, $11,
        TRUE, $12, $13, '{}'::jsonb)`,
		worldID, state.ActorCode, nextRevision-1, frameSequence,
		state.X, state.Y, state.Z, next.X, next.Y, next.Z, nextMotionState,
		previousEventHash, eventHash,
	); err != nil {
		return false, fmt.Errorf("append realtime actor patrol event: %w", err)
	}
	if event.DueWorldTimeUS > cityRealtimeMaximumWorldTimeUS-cityRealtimeActorPatrolIntervalUS {
		return false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_actor_patrol_time"})
	}
	if err = scheduleCityRealtimeActorPatrolDueEvent(ctx, tx, worldID, state.ActorCode,
		event.DueWorldTimeUS+cityRealtimeActorPatrolIntervalUS,
		payload.Step+1, nextRevision, frameSequence); err != nil {
		return false, err
	}
	return true, nil
}

func selectCityRealtimeActorPatrolTarget(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	state cityRealtimeActorState,
	step int64,
	candidates []cityRealtimeActorSpawnCandidate,
) (cityRealtimeActorSpawnCandidate, bool, error) {
	if len(candidates) == 0 {
		return cityRealtimeActorSpawnCandidate{}, false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_actor_patrol_candidates"})
	}
	currentIndex := -1
	for index, candidate := range candidates {
		if candidate.X == state.X && candidate.Y == state.Y && candidate.Z == state.Z {
			currentIndex = index
			break
		}
	}
	start := cityRealtimeActorStableIndex(step, state.ActorCode, len(candidates))
	if currentIndex >= 0 && len(candidates) > 1 {
		stride := 1 + cityRealtimeActorStableIndex(step, state.ActorCode+"-stride", len(candidates)-1)
		start = (currentIndex + stride) % len(candidates)
	}
	for offset := 0; offset < len(candidates); offset++ {
		candidate := candidates[(start+offset)%len(candidates)]
		if candidate.X == state.X && candidate.Y == state.Y && candidate.Z == state.Z {
			continue
		}
		var occupied bool
		if err := queryer.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1
    FROM city_realtime_actor_states state
    JOIN city_realtime_actor_identities identity
      ON identity.world_id = state.world_id AND identity.actor_code = state.actor_code
    WHERE state.world_id = $1
      AND state.actor_code <> $2
      AND identity.lifecycle_status = 'active'
      AND state.x = $3 AND state.y = $4 AND state.z = $5
)`, worldID, state.ActorCode, candidate.X, candidate.Y, candidate.Z).Scan(&occupied); err != nil {
			return cityRealtimeActorSpawnCandidate{}, false, fmt.Errorf("check realtime actor patrol occupancy: %w", err)
		}
		if !occupied {
			return candidate, true, nil
		}
	}
	return cityRealtimeActorSpawnCandidate{X: state.X, Y: state.Y, Z: state.Z}, false, nil
}

// GetRealtimeActors returns one bounded actor overlay under the same
// repeatable-read snapshot as the world handshake. Two members looking at the
// same window therefore receive identical public actors and cursor data.
func (s *CityEconomyService) GetRealtimeActors(
	ctx context.Context,
	input CityRealtimeActorSnapshotInput,
) (*CityRealtimeActorSnapshot, error) {
	if err := validateCityRealtimeActorSnapshotInput(input); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin city realtime actor snapshot transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	world, err := loadCityRealtimeWorldProjection(ctx, tx, input.UserID, input.WorldID)
	if err != nil {
		return nil, err
	}
	item, err := loadCityRealtimeActorSnapshot(ctx, tx, world, input)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city realtime actor snapshot transaction: %w", err)
	}
	return item, nil
}

func validateCityRealtimeActorSnapshotInput(input CityRealtimeActorSnapshotInput) error {
	if input.UserID <= 0 || input.WorldID <= 0 || input.Z != cityspatial.SurfaceZ ||
		input.MinimumChunkX > input.MaximumChunkX || input.MinimumChunkY > input.MaximumChunkY ||
		input.MinimumChunkX < -cityRealtimeActorMaximumChunkAbs || input.MaximumChunkX > cityRealtimeActorMaximumChunkAbs ||
		input.MinimumChunkY < -cityRealtimeActorMaximumChunkAbs || input.MaximumChunkY > cityRealtimeActorMaximumChunkAbs ||
		input.MaximumChunkX-input.MinimumChunkX+1 > cityRealtimeActorMaximumChunkSpan ||
		input.MaximumChunkY-input.MinimumChunkY+1 > cityRealtimeActorMaximumChunkSpan {
		return ErrCityInvalidInput
	}
	if input.Limit < 0 || input.Limit > cityRealtimeActorMaximumProjectionCap {
		return ErrCityInvalidInput
	}
	return nil
}

func loadCityRealtimeActorSnapshot(
	ctx context.Context,
	queryer citySQLQueryer,
	world *CityRealtimeWorldProjection,
	input CityRealtimeActorSnapshotInput,
) (*CityRealtimeActorSnapshot, error) {
	if world == nil || world.WorldID != input.WorldID || !cityEngineSupportsRealtimeStaticWorldgen(world.TemporalEngineVersion) {
		return nil, ErrCityRealtimeStaticWorldRequired
	}
	limit := input.Limit
	if limit == 0 {
		limit = cityRealtimeActorDefaultProjectionCap
	}
	chunkSize := world.Spatial.ChunkSize
	if chunkSize != cityspatial.DefaultChunkSize {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_actor_chunk_size"})
	}
	minimumX := input.MinimumChunkX * chunkSize
	maximumX := (input.MaximumChunkX+1)*chunkSize - 1
	minimumY := input.MinimumChunkY * chunkSize
	maximumY := (input.MaximumChunkY+1)*chunkSize - 1
	item := &CityRealtimeActorSnapshot{
		WorldID:               input.WorldID,
		TimelineFrameSequence: world.TimelineFrameSequence,
		TimelineCursor:        world.TimelineCursor,
		StaticProjectionHash:  world.StaticProjectionHash,
		ProjectionScopeEpoch:  world.Viewer.ProjectionScopeEpoch,
		MinimumChunkX:         input.MinimumChunkX,
		MaximumChunkX:         input.MaximumChunkX,
		MinimumChunkY:         input.MinimumChunkY,
		MaximumChunkY:         input.MaximumChunkY,
		Z:                     input.Z,
		Actors:                make([]CityRealtimePublicActor, 0),
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT identity.actor_code, identity.actor_kind, identity.public_label,
       identity.appearance_variant, identity.lifecycle_status,
       state.x, state.y, state.z, state.motion_state, state.position_revision,
       state.last_frame_sequence, state.state_hash, state.event_chain_hash
FROM city_realtime_actor_identities identity
JOIN city_realtime_actor_states state
  ON state.world_id = identity.world_id AND state.actor_code = identity.actor_code
WHERE identity.world_id = $1
  AND identity.lifecycle_status <> 'retired'
  AND state.z = $2
  AND state.x BETWEEN $3 AND $4
  AND state.y BETWEEN $5 AND $6
ORDER BY state.y ASC, state.x ASC, identity.actor_code ASC
LIMIT $7`, input.WorldID, input.Z, minimumX, maximumX, minimumY, maximumY, limit)
	if err != nil {
		return nil, fmt.Errorf("load realtime actor snapshot: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		actor := CityRealtimePublicActor{}
		state := cityRealtimeActorState{}
		if err = rows.Scan(
			&actor.ActorCode, &actor.ActorKind, &actor.PublicLabel, &actor.AppearanceVariant,
			&actor.LifecycleStatus, &actor.X, &actor.Y, &actor.Z, &actor.MotionState,
			&actor.PositionRevision, &actor.LastFrameSequence, &state.StateHash, &state.EventChainHash,
		); err != nil {
			return nil, err
		}
		state.ActorCode = actor.ActorCode
		state.X, state.Y, state.Z = actor.X, actor.Y, actor.Z
		state.MotionState, state.PositionRevision, state.LastFrameSequence = actor.MotionState, actor.PositionRevision, actor.LastFrameSequence
		if !cityRealtimeActorStateValid(state) {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_actor_projection_state"})
		}
		expectedStateHash, hashErr := cityRealtimeActorStateHash(state)
		if hashErr != nil || expectedStateHash != state.StateHash {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_actor_projection_hash"})
		}
		item.Actors = append(item.Actors, actor)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime actor snapshot: %w", err)
	}
	projectionHash, err := cityRealtimeActorProjectionHash(item)
	if err != nil {
		return nil, err
	}
	item.ActorProjectionHash = projectionHash
	return item, nil
}

func cityRealtimeActorProjectionHash(snapshot *CityRealtimeActorSnapshot) (string, error) {
	if snapshot == nil || snapshot.Actors == nil {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_actor_projection"})
	}
	actors := append([]CityRealtimePublicActor(nil), snapshot.Actors...)
	sort.Slice(actors, func(left, right int) bool {
		if actors[left].Y != actors[right].Y {
			return actors[left].Y < actors[right].Y
		}
		if actors[left].X != actors[right].X {
			return actors[left].X < actors[right].X
		}
		return actors[left].ActorCode < actors[right].ActorCode
	})
	raw, err := json.Marshal(struct {
		SchemaVersion         int                       `json:"schema_version"`
		WorldID               int64                     `json:"world_id"`
		TimelineFrameSequence int64                     `json:"timeline_frame_sequence"`
		TimelineCursor        string                    `json:"timeline_cursor"`
		StaticProjectionHash  string                    `json:"static_projection_hash"`
		MinimumChunkX         int64                     `json:"minimum_chunk_x"`
		MaximumChunkX         int64                     `json:"maximum_chunk_x"`
		MinimumChunkY         int64                     `json:"minimum_chunk_y"`
		MaximumChunkY         int64                     `json:"maximum_chunk_y"`
		Z                     int32                     `json:"z"`
		Actors                []CityRealtimePublicActor `json:"actors"`
	}{
		SchemaVersion: cityRealtimeActorRuntimeSchemaVersion,
		WorldID:       snapshot.WorldID, TimelineFrameSequence: snapshot.TimelineFrameSequence,
		TimelineCursor: snapshot.TimelineCursor, StaticProjectionHash: snapshot.StaticProjectionHash,
		MinimumChunkX: snapshot.MinimumChunkX, MaximumChunkX: snapshot.MaximumChunkX,
		MinimumChunkY: snapshot.MinimumChunkY, MaximumChunkY: snapshot.MaximumChunkY,
		Z: snapshot.Z, Actors: actors,
	})
	if err != nil {
		return "", fmt.Errorf("marshal realtime actor projection: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
