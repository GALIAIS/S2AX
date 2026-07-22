package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
)

const (
	cityRealtimeCharacterActionCreate = "character.create"
	cityRealtimeCharacterActionMove   = "character.move"

	cityRealtimePlayerActorCodePrefix    = "character.player."
	cityRealtimePlayerAppearanceVariant  = "player.cobalt"
	cityRealtimeCharacterReceiptKeyMin   = 8
	cityRealtimeCharacterReceiptKeyMax   = 128
	cityRealtimeCharacterMutationSummary = 1
)

// CityRealtimeCharacter is the only character contract returned to its owner.
// It deliberately excludes the private Agent code, owner user ID, provider
// choices, personality seed, memory, and model-related state. The same Actor
// can be observed by other members through CityRealtimePublicActor.
type CityRealtimeCharacter struct {
	ActorCode         string `json:"actor_code"`
	PublicLabel       string `json:"public_label"`
	AppearanceVariant string `json:"appearance_variant"`
	LifecycleStatus   string `json:"lifecycle_status"`
	ControlMode       string `json:"control_mode"`
	X                 int64  `json:"x"`
	Y                 int64  `json:"y"`
	Z                 int32  `json:"z"`
	MotionState       string `json:"motion_state"`
	PositionRevision  int64  `json:"position_revision"`
	LastFrameSequence int64  `json:"last_frame_sequence"`
}

// CityRealtimeMyCharacterProjection is member-private only because it tells
// the requester which shared Actor belongs to them. It never appears in the
// common map projection or in another member's response.
type CityRealtimeMyCharacterProjection struct {
	WorldID               int64  `json:"world_id"`
	TimelineFrameSequence int64  `json:"timeline_frame_sequence"`
	TimelineCursor        string `json:"timeline_cursor"`
	// RuntimeReady distinguishes a contemporary V2 world with the sealed
	// Agent foundation from historic V2 worlds whose canonical state must stay
	// unchanged. Historic worlds remain readable but cannot create a player
	// Character Agent.
	RuntimeReady        bool                                        `json:"runtime_ready"`
	Exists              bool                                        `json:"exists"`
	Character           *CityRealtimeCharacter                      `json:"character,omitempty"`
	Life                *CityRealtimeCharacterLife                  `json:"life,omitempty"`
	Agent               *CityRealtimeCharacterAgentConfiguration    `json:"agent,omitempty"`
	AvailableArchetypes []CityRealtimeCharacterArchetypeOption      `json:"available_archetypes,omitempty"`
	AvailableActivities []CityRealtimeCharacterActivityAvailability `json:"available_activities,omitempty"`
	AvailablePortals    []CityRealtimeCharacterPortalTransition     `json:"available_portals,omitempty"`
	CurrentInterior     *CityRealtimeCharacterInteriorProjection    `json:"current_interior,omitempty"`
}

type CityRealtimeCharacterCreateInput struct {
	UserID         int64
	WorldID        int64
	PublicLabel    string
	ArchetypeCode  string
	IdempotencyKey string
}

type CityRealtimeCharacterMoveInput struct {
	UserID         int64
	WorldID        int64
	X              int64
	Y              int64
	Z              int32
	IdempotencyKey string
}

// CityRealtimeCharacterMutationResult is safe to retain in the durable
// idempotency receipt. The result contains only the owner's own public Actor
// state and the public Temporal Frame that sealed it.
type CityRealtimeCharacterMutationResult struct {
	Character  CityRealtimeCharacter                    `json:"character"`
	Life       *CityRealtimeCharacterLife               `json:"life,omitempty"`
	Activity   *CityRealtimeCharacterActivityResult     `json:"activity,omitempty"`
	RoleChange *CityRealtimeCharacterRoleChangeResult   `json:"role_change,omitempty"`
	Agent      *CityRealtimeCharacterAgentConfiguration `json:"agent,omitempty"`
	Frame      *CityTemporalFrame                       `json:"frame"`
}

type cityRealtimeCharacterRecord struct {
	agent    cityRealtimeAgentInstance
	identity cityRealtimeActorIdentity
	state    cityRealtimeActorState
}

func (record cityRealtimeCharacterRecord) projection() CityRealtimeCharacter {
	return CityRealtimeCharacter{
		ActorCode:         record.identity.ActorCode,
		PublicLabel:       record.identity.PublicLabel,
		AppearanceVariant: record.identity.AppearanceVariant,
		LifecycleStatus:   record.agent.LifecycleStatus,
		ControlMode:       record.agent.ControlMode,
		X:                 record.state.X,
		Y:                 record.state.Y,
		Z:                 record.state.Z,
		MotionState:       record.state.MotionState,
		PositionRevision:  record.state.PositionRevision,
		LastFrameSequence: record.state.LastFrameSequence,
	}
}

type cityRealtimeCharacterActionReceipt struct {
	ActionType  string
	RequestHash string
	Result      CityRealtimeCharacterMutationResult
}

func (s *CityEconomyService) GetRealtimeMyCharacter(
	ctx context.Context,
	userID, worldID int64,
) (*CityRealtimeMyCharacterProjection, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin realtime character projection transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = requireCityRealtimeWorldRead(ctx, tx, userID, worldID); err != nil {
		return nil, err
	}
	var simulationVersion string
	var timelineFrameSequence int64
	var timelineCursor string
	var currentWorldTimeUS int64
	if err = tx.QueryRowContext(ctx, `
SELECT world.simulation_version, time_state.timeline_frame_sequence, time_state.timeline_cursor,
       time_state.current_world_time_us
FROM city_worlds world
JOIN city_world_time_states time_state ON time_state.world_id = world.id
WHERE world.id = $1`, worldID).Scan(&simulationVersion, &timelineFrameSequence, &timelineCursor, &currentWorldTimeUS); err != nil {
		return nil, fmt.Errorf("load realtime character projection world: %w", err)
	}
	if !cityEngineSupportsRealtimeStaticWorldgen(simulationVersion) {
		return nil, ErrCityRealtimeStaticWorldRequired.WithMetadata(map[string]string{"version": simulationVersion})
	}
	item := &CityRealtimeMyCharacterProjection{
		WorldID:               worldID,
		TimelineFrameSequence: timelineFrameSequence,
		TimelineCursor:        timelineCursor,
	}
	agents, err := loadCityRealtimeAgentHashState(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	lifeRuntime, err := loadCityRealtimeCharacterLifeRuntime(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	// Worlds created before either immutable foundation intentionally retain
	// their historic hash and cannot offer a partial player runtime.
	if agents == nil || lifeRuntime == nil {
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit historical realtime character projection: %w", err)
		}
		return item, nil
	}
	item.RuntimeReady = true
	item.AvailableArchetypes = cityRealtimeCharacterArchetypeOptions(lifeRuntime)
	record, found, err := loadCityRealtimeOwnedCharacter(ctx, tx, worldID, userID, false)
	if err != nil {
		return nil, err
	}
	if found {
		profile, profileFound, profileErr := loadCityRealtimeCharacterProfile(ctx, tx, worldID, record.identity.ActorCode, false)
		if profileErr != nil {
			return nil, profileErr
		}
		if !profileFound {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_profile_missing"})
		}
		if !cityRealtimeCharacterProfileMatchesRuntime(profile, lifeRuntime) {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_profile_runtime"})
		}
		availability, availabilityErr := cityRealtimeCharacterActivityAvailability(
			ctx, tx, worldID, currentWorldTimeUS, record.state, profile, lifeRuntime,
		)
		if availabilityErr != nil {
			return nil, availabilityErr
		}
		position := cityRealtimeActorSpawnCandidate{X: record.state.X, Y: record.state.Y, Z: record.state.Z}
		interior, interiorFound, interiorErr := loadCityRealtimeCharacterInteriorAt(ctx, tx, worldID, position)
		if interiorErr != nil {
			return nil, interiorErr
		}
		portals, portalErr := cityRealtimeCharacterAvailablePortals(ctx, tx, worldID, record.identity.ActorCode, position)
		if portalErr != nil {
			return nil, portalErr
		}
		item.Exists = true
		character := record.projection()
		item.Character = &character
		life, lifeErr := cityRealtimeCharacterLifeProjection(profile, lifeRuntime)
		if lifeErr != nil {
			return nil, lifeErr
		}
		item.Life = &life
		agentProjection, agentProjectionErr := cityRealtimeCharacterAgentProjection(
			ctx, tx, worldID, *agents.Binding, record.agent,
		)
		if agentProjectionErr != nil {
			return nil, agentProjectionErr
		}
		item.Agent = agentProjection
		item.AvailableActivities = availability
		item.AvailablePortals = portals
		if interiorFound {
			projection := interior.projection()
			item.CurrentInterior = &projection
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime character projection: %w", err)
	}
	return item, nil
}

func (s *CityEconomyService) CreateRealtimeCharacter(
	ctx context.Context,
	input CityRealtimeCharacterCreateInput,
) (*CityRealtimeCharacterMutationResult, error) {
	label, err := normalizeCityRealtimeCharacterLabel(input.PublicLabel)
	if err != nil {
		return nil, err
	}
	archetypeCode := strings.TrimSpace(input.ArchetypeCode)
	if archetypeCode != "" && !cityRealtimeCharacterProgressionCodeValid(archetypeCode, 96) {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "archetype_code"})
	}
	idempotencyKey, err := normalizeCityRealtimeCharacterIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if input.UserID <= 0 || input.WorldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	requestHash, err := cityRealtimeCharacterRequestHash(cityRealtimeCharacterActionCreate, map[string]any{
		"world_id":       input.WorldID,
		"user_id":        input.UserID,
		"label":          label,
		"archetype_code": archetypeCode,
	})
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin realtime character create transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockCityRealtimeCharacterWorld(ctx, tx, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	world, err := lockCityWorld(ctx, tx, input.UserID, input.WorldID)
	if err != nil {
		return nil, err
	}
	state, err := lockCityRealtimeState(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	if err = validateCityRealtimeCharacterCommandWindow(world, state); err != nil {
		return nil, err
	}
	if receipt, found, receiptErr := loadCityRealtimeCharacterActionReceipt(
		ctx, tx, input.WorldID, input.UserID, idempotencyKey,
	); receiptErr != nil {
		return nil, receiptErr
	} else if found {
		return completeCityRealtimeCharacterReceipt(tx, receipt, cityRealtimeCharacterActionCreate, requestHash)
	}
	agentState, err := loadCityRealtimeAgentHashState(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	lifeRuntime, err := loadCityRealtimeCharacterLifeRuntime(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	if agentState == nil || agentState.Binding == nil || lifeRuntime == nil {
		return nil, ErrCityRealtimeCharacterRuntimeUnavailable
	}
	if _, err = cityRealtimeCharacterResolveArchetype(lifeRuntime, archetypeCode); err != nil {
		return nil, err
	}
	if existing, exists, loadErr := loadCityRealtimeOwnedCharacter(ctx, tx, input.WorldID, input.UserID, true); loadErr != nil {
		return nil, loadErr
	} else if exists {
		_ = existing
		return nil, ErrCityRealtimeCharacterExists
	}
	if err = cityRealtimeRejectPendingDueAtCurrentTime(ctx, tx, input.WorldID, state.currentWorldTimeUS); err != nil {
		return nil, err
	}
	frameSequence, cursor, err := cityRealtimeCharacterNextFrame(state)
	if err != nil {
		return nil, err
	}
	actorCode, err := newCityRealtimePlayerActorCode()
	if err != nil {
		return nil, err
	}
	agentCode, err := cityRealtimeAgentCodeForUserCharacter(actorCode)
	if err != nil {
		return nil, err
	}
	spawn, err := selectCityRealtimeCharacterSpawn(ctx, tx, input.WorldID, world.seed, actorCode)
	if err != nil {
		return nil, err
	}
	actorCodePointer := actorCode
	identity := cityRealtimeActorIdentity{
		ActorCode:          actorCode,
		ActorKind:          "character",
		PublicLabel:        label,
		AppearanceVariant:  cityRealtimePlayerAppearanceVariant,
		LifecycleStatus:    "active",
		SpawnX:             spawn.X,
		SpawnY:             spawn.Y,
		SpawnZ:             spawn.Z,
		SpawnFrameSequence: frameSequence,
	}
	identity.IdentityHash, err = cityRealtimeActorIdentityHash(identity)
	if err != nil {
		return nil, err
	}
	spawnEventHash, err := cityRealtimeActorPositionEventHash(cityRealtimeActorPositionEventInput{
		ActorCode: actorCode, EventSequence: 0, FrameSequence: frameSequence,
		EventKind: "spawn", To: spawn, MotionState: "idle",
	})
	if err != nil {
		return nil, err
	}
	actorState := cityRealtimeActorState{
		ActorCode: actorCode, X: spawn.X, Y: spawn.Y, Z: spawn.Z,
		MotionState: "idle", PositionRevision: 1, LastFrameSequence: frameSequence,
		EventChainHash: spawnEventHash,
	}
	actorState.StateHash, err = cityRealtimeActorStateHash(actorState)
	if err != nil {
		return nil, err
	}
	ownerUserID := input.UserID
	agent, err := newCityRealtimeAgentSpawnInstance(
		*agentState.Binding, agentCode, "character", "character.user", nil,
		&actorCodePointer, &ownerUserID, "manual", frameSequence, cityRealtimeCharacterActionCreate,
	)
	if err != nil {
		return nil, err
	}
	if err = enableCityRealtimeCharacterMutationGates(ctx, tx, input.WorldID, frameSequence, true); err != nil {
		return nil, err
	}
	if err = enableCityRealtimeCharacterActivityMutationGates(ctx, tx, input.WorldID, frameSequence); err != nil {
		return nil, err
	}
	if err = insertCityRealtimeCharacterActorSpawn(ctx, tx, input.WorldID, identity, actorState, spawnEventHash); err != nil {
		return nil, err
	}
	if err = insertCityRealtimeCharacterAgentSpawn(ctx, tx, input.WorldID, agent, cityRealtimeCharacterActionCreate); err != nil {
		return nil, err
	}
	life, err := seedCityRealtimeCharacterLife(
		ctx, tx, input.WorldID, actorCode, frameSequence, state.currentWorldTimeUS, lifeRuntime, archetypeCode,
	)
	if err != nil {
		return nil, err
	}
	if lifeRuntime.Metabolism != nil {
		if err = scheduleCityRealtimeCharacterMetabolismDueEvent(
			ctx, tx, input.WorldID, lifeRuntime, life, frameSequence,
		); err != nil {
			return nil, err
		}
		state.nextDueAtWorldTimeUS, err = cityRealtimeNextPendingDue(ctx, tx, input.WorldID)
		if err != nil || state.nextDueAtWorldTimeUS == nil {
			if err == nil {
				err = ErrCitySimulationInvariant.WithMetadata(map[string]string{
					"field": "realtime_character_metabolism_initial_due",
				})
			}
			return nil, err
		}
	}
	lifeProjection, lifeProjectionErr := cityRealtimeCharacterLifeProjection(life, lifeRuntime)
	if lifeProjectionErr != nil {
		return nil, lifeProjectionErr
	}
	result := &CityRealtimeCharacterMutationResult{Character: cityRealtimeCharacterRecord{
		agent: agent, identity: identity, state: actorState,
	}.projection(), Life: cityRealtimeCharacterLifePointer(lifeProjection)}
	if result.Frame, err = sealCityRealtimeCharacterFrame(
		ctx, tx, input.WorldID, world, state, frameSequence, cursor, cityRealtimeCharacterActionCreate,
		map[string]any{
			"character_created": 1, "character_moved": 0, "character_life_seeded": 1,
			"character_metabolism_scheduled": boolToCityRealtimeCount(lifeRuntime.Metabolism != nil),
			"character_progression_seeded":   boolToCityRealtimeCount(lifeRuntime.Progression != nil),
		},
	); err != nil {
		return nil, err
	}
	if err = canonicalizeCityRealtimeCharacterMutationResult(result); err != nil {
		return nil, err
	}
	if err = storeCityRealtimeCharacterActionReceipt(
		ctx, tx, input.WorldID, input.UserID, idempotencyKey, actorCode,
		cityRealtimeCharacterActionCreate, requestHash, frameSequence, *result,
	); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime character create: %w", err)
	}
	return result, nil
}

func (s *CityEconomyService) MoveRealtimeCharacter(
	ctx context.Context,
	input CityRealtimeCharacterMoveInput,
) (*CityRealtimeCharacterMutationResult, error) {
	idempotencyKey, err := normalizeCityRealtimeCharacterIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if input.UserID <= 0 || input.WorldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	requestHash, err := cityRealtimeCharacterRequestHash(cityRealtimeCharacterActionMove, map[string]any{
		"world_id": input.WorldID,
		"user_id":  input.UserID,
		"x":        input.X,
		"y":        input.Y,
		"z":        input.Z,
	})
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin realtime character move transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockCityRealtimeCharacterWorld(ctx, tx, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	world, err := lockCityWorld(ctx, tx, input.UserID, input.WorldID)
	if err != nil {
		return nil, err
	}
	state, err := lockCityRealtimeState(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	if err = validateCityRealtimeCharacterCommandWindow(world, state); err != nil {
		return nil, err
	}
	if receipt, found, receiptErr := loadCityRealtimeCharacterActionReceipt(
		ctx, tx, input.WorldID, input.UserID, idempotencyKey,
	); receiptErr != nil {
		return nil, receiptErr
	} else if found {
		return completeCityRealtimeCharacterReceipt(tx, receipt, cityRealtimeCharacterActionMove, requestHash)
	}
	agentState, err := loadCityRealtimeAgentHashState(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	lifeRuntime, err := loadCityRealtimeCharacterLifeRuntime(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	if agentState == nil || agentState.Binding == nil || lifeRuntime == nil {
		return nil, ErrCityRealtimeCharacterRuntimeUnavailable
	}
	record, found, err := loadCityRealtimeOwnedCharacter(ctx, tx, input.WorldID, input.UserID, true)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrCityRealtimeCharacterNotFound
	}
	if record.agent.LifecycleStatus != "active" || record.identity.LifecycleStatus != "active" ||
		record.agent.ControlMode != "manual" {
		return nil, ErrCityRealtimeCharacterControlUnavailable
	}
	profile, profileFound, profileErr := loadCityRealtimeCharacterProfile(ctx, tx, input.WorldID, record.identity.ActorCode, false)
	if profileErr != nil {
		return nil, profileErr
	}
	if !profileFound {
		return nil, ErrCityRealtimeCharacterRuntimeUnavailable
	}
	if !cityRealtimeCharacterProfileMatchesRuntime(profile, lifeRuntime) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_profile_runtime"})
	}
	if err = cityRealtimeRejectPendingDueAtCurrentTime(ctx, tx, input.WorldID, state.currentWorldTimeUS); err != nil {
		return nil, err
	}
	frameSequence, cursor, err := cityRealtimeCharacterNextFrame(state)
	if err != nil {
		return nil, err
	}
	target := cityRealtimeActorSpawnCandidate{X: input.X, Y: input.Y, Z: input.Z}
	if !cityRealtimeCharacterAdjacentStep(record.state, target) {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "destination"})
	}
	motionState, traversable, err := cityRealtimeCharacterWalkMotionState(ctx, tx, input.WorldID, record.state, target)
	if err != nil {
		return nil, err
	}
	if !traversable {
		return nil, ErrCityRealtimeCharacterControlUnavailable.WithMetadata(map[string]string{"field": "destination"})
	}
	occupied, err := cityRealtimeActorPositionOccupied(ctx, tx, input.WorldID, record.identity.ActorCode, target)
	if err != nil {
		return nil, err
	}
	if occupied {
		return nil, ErrCityRealtimeCharacterControlUnavailable.WithMetadata(map[string]string{"field": "destination_occupied"})
	}
	if err = advanceCityRealtimeCharacterPosition(
		ctx, tx, input.WorldID, frameSequence, &record, target, "move", motionState, "",
	); err != nil {
		return nil, err
	}
	life, lifeErr := cityRealtimeCharacterLifeProjection(profile, lifeRuntime)
	if lifeErr != nil {
		return nil, lifeErr
	}
	result := &CityRealtimeCharacterMutationResult{
		Character: record.projection(), Life: cityRealtimeCharacterLifePointer(life),
	}
	if result.Frame, err = sealCityRealtimeCharacterFrame(
		ctx, tx, input.WorldID, world, state, frameSequence, cursor, cityRealtimeCharacterActionMove,
		map[string]any{"character_created": 0, "character_moved": 1},
	); err != nil {
		return nil, err
	}
	if err = canonicalizeCityRealtimeCharacterMutationResult(result); err != nil {
		return nil, err
	}
	if err = storeCityRealtimeCharacterActionReceipt(
		ctx, tx, input.WorldID, input.UserID, idempotencyKey, record.identity.ActorCode,
		cityRealtimeCharacterActionMove, requestHash, frameSequence, *result,
	); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime character move: %w", err)
	}
	return result, nil
}

func lockCityRealtimeCharacterWorld(ctx context.Context, tx *sql.Tx, userID, worldID int64) error {
	if tx == nil || userID <= 0 || worldID <= 0 {
		return ErrCityInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, cityWorldLockKey(worldID)); err != nil {
		return fmt.Errorf("lock realtime character world: %w", err)
	}
	return nil
}

func validateCityRealtimeCharacterCommandWindow(world *lockedCityWorld, state *lockedCityRealtimeState) error {
	if world == nil || state == nil || !cityEngineSupportsRealtimeStaticWorldgen(world.simulationVersion) {
		if world == nil {
			return ErrCityRealtimeStaticWorldRequired
		}
		return ErrCityRealtimeStaticWorldRequired.WithMetadata(map[string]string{"version": world.simulationVersion})
	}
	if world.status != CityWorldStatusRunning || world.stateHash == nil ||
		state.lifecycleStatus != CityWorldStatusRunning {
		return ErrCityWorldUnavailable.WithMetadata(map[string]string{"status": world.status})
	}
	if state.clockState != cityRealtimeClockStateInitializing && state.clockState != cityRealtimeClockStateHealthy ||
		state.recoveryState != cityRealtimeRecoveryStateIdle {
		return ErrCityRealtimeClockUnsafe
	}
	if state.timelineFrameSequence >= cityRealtimeMaximumTimelineSequence {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_timeline"})
	}
	return nil
}

func cityRealtimeRejectPendingDueAtCurrentTime(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, currentWorldTimeUS int64,
) error {
	pending, err := cityRealtimeFirstPendingDueAtOrBefore(ctx, queryer, worldID, currentWorldTimeUS)
	if err != nil {
		return err
	}
	if pending != nil {
		return ErrCityRealtimeDueEventPending.WithMetadata(map[string]string{
			"next_due_at_world_time_us": strconv.FormatInt(*pending, 10),
		})
	}
	return nil
}

func cityRealtimeCharacterNextFrame(state *lockedCityRealtimeState) (int64, string, error) {
	if state == nil || state.timelineFrameSequence < 0 || state.timelineFrameSequence >= cityRealtimeMaximumTimelineSequence {
		return 0, "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_timeline"})
	}
	frameSequence := state.timelineFrameSequence + 1
	cursor, err := cityRealtimeTimelineCursor(frameSequence)
	if err != nil {
		return 0, "", err
	}
	return frameSequence, cursor, nil
}

func enableCityRealtimeCharacterMutationGates(
	ctx context.Context,
	tx *sql.Tx,
	worldID, frameSequence int64,
	includeAgent bool,
) error {
	settings := []struct {
		name  string
		value int64
	}{
		{name: "sub2api.city_realtime_actor_mutation_world_id", value: worldID},
		{name: "sub2api.city_realtime_actor_mutation_frame_sequence", value: frameSequence},
	}
	if includeAgent {
		settings = append(settings,
			struct {
				name  string
				value int64
			}{name: "sub2api.city_realtime_agent_mutation_world_id", value: worldID},
			struct {
				name  string
				value int64
			}{name: "sub2api.city_realtime_agent_mutation_frame_sequence", value: frameSequence},
		)
	}
	for _, setting := range settings {
		if _, err := tx.ExecContext(ctx, `SELECT set_config($1, $2, TRUE)`, setting.name, strconv.FormatInt(setting.value, 10)); err != nil {
			return fmt.Errorf("activate realtime character mutation gate %s: %w", setting.name, err)
		}
	}
	return nil
}

func insertCityRealtimeCharacterActorSpawn(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	identity cityRealtimeActorIdentity,
	state cityRealtimeActorState,
	spawnEventHash string,
) error {
	if !cityRealtimeActorIdentityValid(identity) || !cityRealtimeActorStateValid(state) ||
		!cityRealtimeSHA256Hex(spawnEventHash) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_spawn"})
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_actor_identities
    (world_id, actor_code, actor_kind, public_label, appearance_variant,
     lifecycle_status, spawn_x, spawn_y, spawn_z, spawn_frame_sequence,
     identity_hash, metadata)
VALUES ($1, $2, 'character', $3, $4, 'active', $5, $6, $7, $8, $9, '{}'::jsonb)`,
		worldID, identity.ActorCode, identity.PublicLabel, identity.AppearanceVariant,
		identity.SpawnX, identity.SpawnY, identity.SpawnZ, identity.SpawnFrameSequence,
		identity.IdentityHash,
	); err != nil {
		return fmt.Errorf("insert realtime player character identity: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_actor_states
    (world_id, actor_code, x, y, z, motion_state, position_revision,
     last_frame_sequence, state_hash, event_chain_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, '{}'::jsonb)`,
		worldID, state.ActorCode, state.X, state.Y, state.Z, state.MotionState,
		state.PositionRevision, state.LastFrameSequence, state.StateHash, state.EventChainHash,
	); err != nil {
		return fmt.Errorf("insert realtime player character state: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_actor_position_events
    (world_id, actor_code, event_sequence, frame_sequence, event_kind,
     to_x, to_y, to_z, motion_state, public_visibility, event_hash, metadata)
VALUES ($1, $2, 0, $3, 'spawn', $4, $5, $6, 'idle', TRUE, $7, '{}'::jsonb)`,
		worldID, identity.ActorCode, identity.SpawnFrameSequence,
		identity.SpawnX, identity.SpawnY, identity.SpawnZ, spawnEventHash,
	); err != nil {
		return fmt.Errorf("append realtime player character spawn event: %w", err)
	}
	return nil
}

func insertCityRealtimeCharacterAgentSpawn(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	agent cityRealtimeAgentInstance,
	reasonCode string,
) error {
	if agent.AgentSubtype != "character.user" || !cityRealtimeAgentIdentifierValid(reasonCode, 64) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_agent_spawn"})
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_agent_instances
    (world_id, agent_code, agent_kind, agent_subtype, parent_agent_code,
     actor_code, owner_user_id, lifecycle_status, control_mode,
     definition_code, definition_version, definition_hash, authorization_hash,
     lifecycle_revision, spawned_frame_sequence, last_frame_sequence,
     instance_hash, event_chain_hash, metadata)
VALUES ($1, $2, $3, $4, NULL, $5, $6, $7, $8, $9, $10, $11, $12,
        $13, $14, $15, $16, $17, '{}'::jsonb)`,
		worldID, agent.AgentCode, agent.AgentKind, agent.AgentSubtype, agent.ActorCode,
		agent.OwnerUserID, agent.LifecycleStatus, agent.ControlMode, agent.DefinitionCode,
		agent.DefinitionVersion, agent.DefinitionHash, agent.AuthorizationHash,
		agent.LifecycleRevision, agent.SpawnedFrameSequence, agent.LastFrameSequence,
		agent.InstanceHash, agent.EventChainHash,
	); err != nil {
		return fmt.Errorf("insert realtime player character agent: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_agent_lifecycle_events
    (world_id, agent_code, event_sequence, frame_sequence, event_type,
     from_status, to_status, control_mode, reason_code, previous_event_hash,
     event_hash, metadata)
VALUES ($1, $2, 0, $3, 'spawn', NULL, $4, $5, $6, NULL, $7, '{}'::jsonb)`,
		worldID, agent.AgentCode, agent.SpawnedFrameSequence, agent.LifecycleStatus,
		agent.ControlMode, reasonCode, agent.EventChainHash,
	); err != nil {
		return fmt.Errorf("append realtime player character agent spawn event: %w", err)
	}
	return nil
}

func sealCityRealtimeCharacterFrame(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	world *lockedCityWorld,
	state *lockedCityRealtimeState,
	frameSequence int64,
	cursor string,
	action string,
	counts map[string]any,
) (*CityTemporalFrame, error) {
	if world == nil || state == nil || !cityRealtimeAgentIdentifierValid(action, 64) || frameSequence <= 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_frame"})
	}
	if err := updateCityRealtimeTimeStateForFrame(
		ctx, tx, worldID, state.currentWorldTimeUS, state.lastCommittedEffectiveUTC,
		frameSequence, cursor, state.nextDueAtWorldTimeUS, state.catchupTargetWorldTimeUS,
		state.clockState, state.recoveryState,
	); err != nil {
		return nil, err
	}
	_, _, stateHash, err := canonicalCityWorldState(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE city_worlds SET state_hash = $2, updated_at = NOW() WHERE id = $1`, worldID, stateHash); err != nil {
		return nil, fmt.Errorf("store realtime character frame state hash: %w", err)
	}
	phaseSummary := map[string]any{
		"schema_version":  cityRealtimeCharacterMutationSummary,
		"frame_kind":      "command",
		"command_count":   1,
		"due_event_count": 0,
		"command":         action,
	}
	for key, value := range counts {
		phaseSummary[key] = value
	}
	return insertCityRealtimeFrame(ctx, tx, cityRealtimeFrameInsertInput{
		WorldID:               worldID,
		TemporalEngineVersion: state.temporalEngineVersion,
		FrameSequence:         frameSequence,
		TimelineCursor:        cursor,
		WorldTimeFromUS:       state.currentWorldTimeUS,
		WorldTimeToUS:         state.currentWorldTimeUS,
		ClockSegmentID:        state.clockSegmentID,
		ClockSegmentSequence:  state.clockSegmentSequence,
		ClockProfileHash:      state.clockProfileHash,
		FrameKind:             "command",
		EffectiveUTCFrom:      state.lastCommittedEffectiveUTC,
		EffectiveUTCTo:        state.lastCommittedEffectiveUTC,
		PreviousStateHash:     world.stateHash,
		StateHash:             stateHash,
		DueEventDigest:        cityRealtimeEmptyDueEventDigest,
		PhaseSummary:          phaseSummary,
	})
}

// canonicalizeCityRealtimeCharacterMutationResult normalizes the only
// schemaless field exposed by a character mutation. JSONB returns untyped
// numbers as float64 when a durable idempotency receipt is replayed; applying
// the same JSON representation before the first response makes retries
// structurally identical to the original response.
func canonicalizeCityRealtimeCharacterMutationResult(result *CityRealtimeCharacterMutationResult) error {
	if result == nil || result.Frame == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_mutation_result"})
	}
	rawSummary, err := json.Marshal(result.Frame.PhaseSummary)
	if err != nil {
		return fmt.Errorf("marshal realtime character frame summary: %w", err)
	}
	phaseSummary, err := decodeCityJSONMap(rawSummary)
	if err != nil {
		return fmt.Errorf("canonicalize realtime character frame summary: %w", err)
	}
	result.Frame.PhaseSummary = phaseSummary
	return nil
}

func loadCityRealtimeOwnedCharacter(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, userID int64,
	forUpdate bool,
) (cityRealtimeCharacterRecord, bool, error) {
	if worldID <= 0 || userID <= 0 {
		return cityRealtimeCharacterRecord{}, false, ErrCityInvalidInput
	}
	query := `
SELECT agent.agent_code, agent.agent_kind, agent.agent_subtype, agent.parent_agent_code,
       agent.actor_code, agent.owner_user_id, agent.lifecycle_status, agent.control_mode,
       agent.definition_code, agent.definition_version, agent.definition_hash,
       agent.authorization_hash, agent.lifecycle_revision, agent.spawned_frame_sequence,
       agent.last_frame_sequence, agent.instance_hash, agent.event_chain_hash,
       identity.actor_code, identity.actor_kind, identity.public_label,
       identity.appearance_variant, identity.lifecycle_status, identity.spawn_x,
       identity.spawn_y, identity.spawn_z, identity.spawn_frame_sequence, identity.identity_hash,
       state.actor_code, state.x, state.y, state.z, state.motion_state,
       state.position_revision, state.last_frame_sequence, state.state_hash, state.event_chain_hash
FROM city_realtime_agent_instances agent
JOIN city_realtime_actor_identities identity
  ON identity.world_id = agent.world_id AND identity.actor_code = agent.actor_code
JOIN city_realtime_actor_states state
  ON state.world_id = identity.world_id AND state.actor_code = identity.actor_code
WHERE agent.world_id = $1
  AND agent.owner_user_id = $2
  AND agent.agent_subtype = 'character.user'
  AND agent.lifecycle_status NOT IN ('retiring', 'terminated')
ORDER BY agent.spawned_frame_sequence DESC, agent.agent_code DESC
LIMIT 1`
	if forUpdate {
		query += " FOR UPDATE OF agent, state"
	}
	record := cityRealtimeCharacterRecord{}
	var parentAgentCode, actorCode sql.NullString
	var ownerUserID sql.NullInt64
	err := queryer.QueryRowContext(ctx, query, worldID, userID).Scan(
		&record.agent.AgentCode, &record.agent.AgentKind, &record.agent.AgentSubtype,
		&parentAgentCode, &actorCode, &ownerUserID, &record.agent.LifecycleStatus,
		&record.agent.ControlMode, &record.agent.DefinitionCode, &record.agent.DefinitionVersion,
		&record.agent.DefinitionHash, &record.agent.AuthorizationHash, &record.agent.LifecycleRevision,
		&record.agent.SpawnedFrameSequence, &record.agent.LastFrameSequence,
		&record.agent.InstanceHash, &record.agent.EventChainHash,
		&record.identity.ActorCode, &record.identity.ActorKind, &record.identity.PublicLabel,
		&record.identity.AppearanceVariant, &record.identity.LifecycleStatus, &record.identity.SpawnX,
		&record.identity.SpawnY, &record.identity.SpawnZ, &record.identity.SpawnFrameSequence,
		&record.identity.IdentityHash,
		&record.state.ActorCode, &record.state.X, &record.state.Y, &record.state.Z,
		&record.state.MotionState, &record.state.PositionRevision, &record.state.LastFrameSequence,
		&record.state.StateHash, &record.state.EventChainHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterRecord{}, false, nil
	}
	if err != nil {
		return cityRealtimeCharacterRecord{}, false, fmt.Errorf("load realtime owned character: %w", err)
	}
	record.agent.ParentAgentCode = cityRealtimeAgentNullStringPointer(parentAgentCode)
	record.agent.ActorCode = cityRealtimeAgentNullStringPointer(actorCode)
	record.agent.OwnerUserID = cityRealtimeAgentNullInt64Pointer(ownerUserID)
	if record.agent.ActorCode == nil || record.agent.OwnerUserID == nil || *record.agent.OwnerUserID != userID ||
		record.identity.ActorKind != "character" || record.identity.ActorCode != *record.agent.ActorCode ||
		record.state.ActorCode != *record.agent.ActorCode || !cityRealtimeActorIdentityValid(record.identity) ||
		!cityRealtimeActorStateValid(record.state) {
		return cityRealtimeCharacterRecord{}, false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_owned_character"})
	}
	return record, true, nil
}

func selectCityRealtimeCharacterSpawn(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, seed int64,
	actorCode string,
) (cityRealtimeActorSpawnCandidate, error) {
	candidates, err := loadCityRealtimeActorSpawnCandidates(ctx, queryer, worldID)
	if err != nil {
		return cityRealtimeActorSpawnCandidate{}, err
	}
	if len(candidates) == 0 {
		return cityRealtimeActorSpawnCandidate{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_spawn_candidates"})
	}
	start := cityRealtimeActorStableIndex(seed, actorCode, len(candidates))
	for offset := 0; offset < len(candidates); offset++ {
		candidate := candidates[(start+offset)%len(candidates)]
		occupied, occupancyErr := cityRealtimeActorPositionOccupied(ctx, queryer, worldID, "", candidate)
		if occupancyErr != nil {
			return cityRealtimeActorSpawnCandidate{}, occupancyErr
		}
		if !occupied {
			return candidate, nil
		}
	}
	return cityRealtimeActorSpawnCandidate{}, ErrCityRealtimeCharacterControlUnavailable.WithMetadata(map[string]string{"field": "spawn_capacity"})
}

func cityRealtimeActorPositionOccupied(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	excludedActorCode string,
	position cityRealtimeActorSpawnCandidate,
) (bool, error) {
	var occupied bool
	if err := queryer.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1
    FROM city_realtime_actor_states state
    JOIN city_realtime_actor_identities identity
      ON identity.world_id = state.world_id AND identity.actor_code = state.actor_code
    WHERE state.world_id = $1
      AND ($2 = '' OR state.actor_code <> $2)
      AND identity.lifecycle_status = 'active'
      AND state.x = $3 AND state.y = $4 AND state.z = $5
)`, worldID, excludedActorCode, position.X, position.Y, position.Z).Scan(&occupied); err != nil {
		return false, fmt.Errorf("check realtime actor position occupancy: %w", err)
	}
	return occupied, nil
}

func cityRealtimeCharacterAdjacentStep(state cityRealtimeActorState, target cityRealtimeActorSpawnCandidate) bool {
	if state.Z != target.Z {
		return false
	}
	// Never derive adjacency through subtraction/absolute value: MinInt64
	// would overflow and could turn an arbitrary world coordinate into a
	// one-cell move. Explicit successor/predecessor checks are total across
	// the entire signed coordinate domain.
	if target.X == state.X {
		return (state.Y > math.MinInt64 && target.Y == state.Y-1) ||
			(state.Y < math.MaxInt64 && target.Y == state.Y+1)
	}
	if target.Y == state.Y {
		return (state.X > math.MinInt64 && target.X == state.X-1) ||
			(state.X < math.MaxInt64 && target.X == state.X+1)
	}
	return false
}

func cityRealtimeCharacterSurfaceCellTraversable(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	position cityRealtimeActorSpawnCandidate,
) (bool, error) {
	if position.Z != cityspatial.SurfaceZ {
		return false, nil
	}
	address, err := cityspatial.SplitWorldCoordinate(cityspatial.WorldCoordinate{
		X: position.X, Y: position.Y, Z: position.Z,
	}, cityspatial.DefaultChunkSize)
	if err != nil {
		return false, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "destination"}).WithCause(err)
	}
	var rawPayload []byte
	err = queryer.QueryRowContext(ctx, `
SELECT payload
FROM city_realtime_spatial_chunks
WHERE world_id = $1 AND chunk_x = $2 AND chunk_y = $3 AND z = $4`,
		worldID, address.Chunk.X, address.Chunk.Y, address.Chunk.Z,
	).Scan(&rawPayload)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load realtime character destination chunk: %w", err)
	}
	payload := cityspatial.OpenWorldChunkPayload{}
	if err = json.Unmarshal(rawPayload, &payload); err != nil {
		return false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_destination_payload"}).WithCause(err)
	}
	if err = cityspatial.ValidateOpenWorldChunkPayload(payload); err != nil {
		return false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_destination_payload"}).WithCause(err)
	}
	cellIndex := int(address.Local.Y)*payload.Width + int(address.Local.X)
	if cellIndex < 0 || cellIndex >= payload.Width*payload.Height {
		return false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_destination_cell"})
	}
	for _, layer := range payload.Layers {
		if int(layer.X) == int(address.Local.X) && int(layer.Y) == int(address.Local.Y) && layer.Kind == cityspatial.RuleKindStructure {
			return false, nil
		}
	}
	terrainID, ok := cityRealtimeTerrainDefinitionAt(payload.TerrainRuns, cellIndex)
	if !ok {
		return false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_destination_terrain"})
	}
	switch terrainID {
	case "terrain.road", "terrain.sidewalk", "terrain.grass", "terrain.ground", "terrain.soil":
		return true, nil
	default:
		return false, nil
	}
}

func cityRealtimeTerrainDefinitionAt(runs []cityspatial.TerrainRun, cellIndex int) (string, bool) {
	if cellIndex < 0 {
		return "", false
	}
	covered := 0
	for _, run := range runs {
		if run.Length <= 0 {
			return "", false
		}
		if cellIndex < covered+run.Length {
			return run.DefinitionID, true
		}
		covered += run.Length
	}
	return "", false
}

func normalizeCityRealtimeCharacterLabel(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !cityRealtimeActorPublicLabelValid(value) {
		return "", ErrCityInvalidInput.WithMetadata(map[string]string{"field": "public_label"})
	}
	return value, nil
}

func normalizeCityRealtimeCharacterIdempotencyKey(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) < cityRealtimeCharacterReceiptKeyMin || len(value) > cityRealtimeCharacterReceiptKeyMax {
		return "", ErrCityInvalidInput.WithMetadata(map[string]string{"field": "idempotency_key"})
	}
	for index, character := range value {
		if (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') || (index > 0 && (character == '.' || character == '_' || character == ':' || character == '-')) {
			continue
		}
		return "", ErrCityInvalidInput.WithMetadata(map[string]string{"field": "idempotency_key"})
	}
	return value, nil
}

func cityRealtimeCharacterRequestHash(action string, payload map[string]any) (string, error) {
	if !cityRealtimeAgentIdentifierValid(action, 64) || payload == nil {
		return "", ErrCityInvalidInput
	}
	payload["action"] = action
	_, hash, err := cityRealtimeCanonicalJSONObject(payload)
	if err != nil {
		return "", ErrCityInvalidInput.WithCause(err)
	}
	return hash, nil
}

func newCityRealtimePlayerActorCode() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate realtime player actor code: %w", err)
	}
	code := cityRealtimePlayerActorCodePrefix + hex.EncodeToString(raw)
	if !cityRealtimePlayerActorCodeValid(code) {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_actor_code"})
	}
	if _, err := cityRealtimeAgentCodeForUserCharacter(code); err != nil {
		return "", err
	}
	return code, nil
}

func cityRealtimePlayerActorCodeValid(value string) bool {
	if !strings.HasPrefix(value, cityRealtimePlayerActorCodePrefix) {
		return false
	}
	suffix := strings.TrimPrefix(value, cityRealtimePlayerActorCodePrefix)
	if len(suffix) != 32 {
		return false
	}
	for _, character := range suffix {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	_, err := hex.DecodeString(suffix)
	return err == nil
}

func loadCityRealtimeCharacterActionReceipt(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, userID int64,
	idempotencyKey string,
) (cityRealtimeCharacterActionReceipt, bool, error) {
	receipt := cityRealtimeCharacterActionReceipt{}
	var rawResult []byte
	err := queryer.QueryRowContext(ctx, `
SELECT action_type, request_hash, result_payload
FROM city_realtime_character_action_receipts
WHERE world_id = $1 AND user_id = $2 AND idempotency_key = $3`,
		worldID, userID, idempotencyKey,
	).Scan(&receipt.ActionType, &receipt.RequestHash, &rawResult)
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterActionReceipt{}, false, nil
	}
	if err != nil {
		return cityRealtimeCharacterActionReceipt{}, false, fmt.Errorf("load realtime character action receipt: %w", err)
	}
	if err = json.Unmarshal(rawResult, &receipt.Result); err != nil || receipt.Result.Frame == nil ||
		receipt.Result.Character.ActorCode == "" || !cityRealtimeActorPublicLabelValid(receipt.Result.Character.PublicLabel) ||
		!cityRealtimeCharacterMutationResultProjectionValid(receipt.Result) {
		if err == nil {
			err = errors.New("invalid character receipt payload")
		}
		return cityRealtimeCharacterActionReceipt{}, false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_receipt"}).WithCause(err)
	}
	return receipt, true, nil
}

func cityRealtimeCharacterMutationResultProjectionValid(result CityRealtimeCharacterMutationResult) bool {
	if result.Life != nil && !cityRealtimeCharacterLifeProjectionValid(*result.Life) {
		return false
	}
	if result.Activity != nil && !cityRealtimeCharacterActivityResultValid(*result.Activity) {
		return false
	}
	if result.Agent != nil && !cityRealtimeCharacterAgentConfigurationValid(*result.Agent) {
		return false
	}
	return true
}

func cityRealtimeCharacterLifeProjectionValid(life CityRealtimeCharacterLife) bool {
	if life.EnergyMilli < 0 || life.EnergyMilli > 1000 || life.SatietyMilli < 0 || life.SatietyMilli > 1000 ||
		life.MoraleMilli < 0 || life.MoraleMilli > 1000 || life.CivicStandingMilli < 0 || life.CivicStandingMilli > 1000 ||
		life.CityCreditUnits < cityRealtimeCharacterLifeMinimumCreditUnits || life.CityCreditUnits > cityRealtimeCharacterLifeMaximumCreditUnits ||
		life.Revision <= 0 || life.ActivityRevision < 0 || life.LawRevision < 0 || life.LastFrameSequence <= 0 ||
		life.LastActivityWorldTimeUS < 0 || life.Inventory == nil {
		return false
	}
	for index, stack := range life.Inventory {
		if !strings.HasPrefix(stack.ItemCode, "item.") || stack.Quantity < 0 || stack.Revision <= 0 ||
			stack.LastFrameSequence <= 0 || (index > 0 && life.Inventory[index-1].ItemCode >= stack.ItemCode) {
			return false
		}
	}
	return true
}

func cityRealtimeCharacterActivityResultValid(activity CityRealtimeCharacterActivityResult) bool {
	return cityRealtimeAgentIdentifierValid(activity.Code, 64) && cityRealtimeAgentIdentifierValid(activity.CategoryCode, 32) &&
		(activity.Outcome == "completed" || activity.Outcome == "penalized") &&
		activity.EnergyDeltaMilli >= -1000 && activity.EnergyDeltaMilli <= 1000 &&
		activity.SatietyDeltaMilli >= -1000 && activity.SatietyDeltaMilli <= 1000 &&
		activity.MoraleDeltaMilli >= -1000 && activity.MoraleDeltaMilli <= 1000 &&
		activity.CivicStandingDeltaMilli >= -1000 && activity.CivicStandingDeltaMilli <= 1000 &&
		activity.CityCreditDeltaUnits >= -1000000 && activity.CityCreditDeltaUnits <= 1000000 &&
		(activity.ItemCode == "" || strings.HasPrefix(activity.ItemCode, "item.")) &&
		(activity.LawCaseCode == "" || strings.HasPrefix(activity.LawCaseCode, "law."))
}

func completeCityRealtimeCharacterReceipt(
	tx *sql.Tx,
	receipt cityRealtimeCharacterActionReceipt,
	action, requestHash string,
) (*CityRealtimeCharacterMutationResult, error) {
	if receipt.ActionType != action || receipt.RequestHash != requestHash {
		return nil, ErrCityRealtimeCharacterIdempotencyConflict
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit replayed realtime character receipt: %w", err)
	}
	return &receipt.Result, nil
}

func storeCityRealtimeCharacterActionReceipt(
	ctx context.Context,
	tx *sql.Tx,
	worldID, userID int64,
	idempotencyKey, actorCode, action, requestHash string,
	frameSequence int64,
	result CityRealtimeCharacterMutationResult,
) error {
	payload, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal realtime character action receipt: %w", err)
	}
	for _, setting := range []struct {
		name  string
		value int64
	}{
		{name: "sub2api.city_realtime_character_receipt_world_id", value: worldID},
		{name: "sub2api.city_realtime_character_receipt_frame_sequence", value: frameSequence},
	} {
		if _, err = tx.ExecContext(ctx, `SELECT set_config($1, $2, TRUE)`, setting.name, strconv.FormatInt(setting.value, 10)); err != nil {
			return fmt.Errorf("activate realtime character receipt gate %s: %w", setting.name, err)
		}
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_action_receipts
    (world_id, user_id, idempotency_key, actor_code, action_type,
     request_hash, frame_sequence, result_payload, result_hash)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb,
        encode(sha256(convert_to(($8::jsonb)::text, 'UTF8')), 'hex'))`,
		worldID, userID, idempotencyKey, actorCode, action,
		requestHash, frameSequence, string(payload),
	); err != nil {
		return fmt.Errorf("store realtime character action receipt: %w", err)
	}
	return nil
}
