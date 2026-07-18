package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	CityCommandTypeWorldRename   = "world.rename"
	CityCommandTypeWorldSetSpeed = "world.set_speed"
	CityCommandTypeWorldPause    = "world.pause"
	CityCommandTypeWorldResume   = "world.resume"

	CityCommandStatusPending  = "pending"
	CityCommandStatusApplied  = "applied"
	CityCommandStatusRejected = "rejected"

	CityWorldStatusRunning = "running"

	cityTickDuration       = time.Hour
	cityPendingCommandMax  = 1000
	cityDefaultEventLimit  = 100
	cityMaximumEventLimit  = 200
	cityCommandRejectedKey = "CITY_COMMAND_UNSUPPORTED"
)

var (
	ErrCityCommandNotFound         = infraerrors.NotFound("CITY_COMMAND_NOT_FOUND", "city command not found")
	ErrCityCommandConflict         = infraerrors.Conflict("CITY_COMMAND_CONFLICT", "city command idempotency key was reused with different intent")
	ErrCityExpectedTickConflict    = infraerrors.Conflict("CITY_EXPECTED_TICK_CONFLICT", "city world tick no longer matches the expected tick")
	ErrCityPermissionDenied        = infraerrors.Forbidden("CITY_PERMISSION_DENIED", "city operation requires the owner role")
	ErrCityWorldUnavailable        = infraerrors.Conflict("CITY_WORLD_UNAVAILABLE", "city world cannot accept simulation writes in its current state")
	ErrCitySimulationVersion       = infraerrors.Conflict("CITY_SIMULATION_VERSION_UNSUPPORTED", "city simulation version is not supported by this engine")
	ErrCityCommandVersion          = infraerrors.Conflict("CITY_COMMAND_VERSION_UNSUPPORTED", "city command is not supported by the world's engine version")
	ErrCityCommandQueueFull        = infraerrors.TooManyRequests("CITY_COMMAND_QUEUE_FULL", "city command queue is full")
	ErrCitySimulationInvariant     = infraerrors.InternalServer("CITY_SIMULATION_INVARIANT_FAILED", "city simulation invariant failed")
	ErrCityStepIdempotencyConflict = infraerrors.Conflict("CITY_STEP_CONFLICT", "city step idempotency key was reused with different intent")
	cityTickEpochTime              = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
)

type CityCommand struct {
	ID                 int64          `json:"id"`
	WorldID            int64          `json:"world_id"`
	UserID             int64          `json:"user_id"`
	Sequence           int64          `json:"sequence"`
	ClientRequestID    string         `json:"client_request_id"`
	RequestFingerprint string         `json:"-"`
	CommandType        string         `json:"command_type"`
	Payload            map[string]any `json:"payload"`
	ExpectedWorldTick  *int64         `json:"expected_world_tick,omitempty"`
	Status             string         `json:"status"`
	ProcessedTick      *int64         `json:"processed_tick,omitempty"`
	Result             map[string]any `json:"result"`
	ErrorCode          *string        `json:"error_code,omitempty"`
	SubmittedAt        time.Time      `json:"submitted_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
}

type CityTick struct {
	ID                   int64     `json:"id"`
	WorldID              int64     `json:"world_id"`
	Tick                 int64     `json:"tick"`
	StepRequestID        string    `json:"step_request_id"`
	RequestFingerprint   string    `json:"-"`
	InitiatedByUserID    int64     `json:"initiated_by_user_id"`
	SimulationVersion    string    `json:"simulation_version"`
	PreviousStateHash    *string   `json:"previous_state_hash,omitempty"`
	StateHash            string    `json:"state_hash"`
	PRNGProof            string    `json:"prng_proof"`
	SimulatedFrom        time.Time `json:"simulated_from"`
	SimulatedTo          time.Time `json:"simulated_to"`
	FirstCommandSequence *int64    `json:"first_command_sequence,omitempty"`
	LastCommandSequence  *int64    `json:"last_command_sequence,omitempty"`
	CommandCount         int       `json:"command_count"`
	AppliedCommandCount  int       `json:"applied_command_count"`
	RejectedCommandCount int       `json:"rejected_command_count"`
	EventCount           int       `json:"event_count"`
	DurationMS           int64     `json:"duration_ms"`
	StartedAt            time.Time `json:"started_at"`
	CompletedAt          time.Time `json:"completed_at"`
}

type CityEvent struct {
	ID            int64          `json:"id"`
	WorldID       int64          `json:"world_id"`
	Tick          int64          `json:"tick"`
	Sequence      int            `json:"sequence"`
	CommandID     *int64         `json:"command_id,omitempty"`
	EventType     string         `json:"event_type"`
	AggregateType string         `json:"aggregate_type"`
	AggregateCode string         `json:"aggregate_code"`
	Payload       map[string]any `json:"payload"`
	CreatedAt     time.Time      `json:"created_at"`
}

type CityEventCursor struct {
	Tick     int64 `json:"tick"`
	Sequence int   `json:"sequence"`
}

type CityEventPage struct {
	Items      []*CityEvent     `json:"items"`
	NextCursor *CityEventCursor `json:"next_cursor,omitempty"`
}

type CityStepResult struct {
	Tick                    *CityTick                    `json:"tick"`
	Commands                []*CityCommand               `json:"commands"`
	Journals                []*CityJournal               `json:"journals"`
	ResourceOperations      []*CityResourceOperation     `json:"resource_operations"`
	MarketSettlements       []*CityMarketSettlement      `json:"market_settlements"`
	PopulationMovements     []*CityPopulationMovement    `json:"population_movements"`
	PopulationMigrations    []*CityPopulationMigration   `json:"population_migrations"`
	HouseholdMovements      []*CityHouseholdMovement     `json:"household_movements"`
	SpatialMutations        []*CitySpatialMutation       `json:"spatial_mutations"`
	DevelopmentFacts        []CityDevelopmentFact        `json:"development_facts"`
	BuildingAdjustments     []CityBuildingAdjustment     `json:"building_adjustments"`
	EnterpriseLocationFacts []CityEnterpriseLocationFact `json:"enterprise_location_facts"`
	WorldRuntimeFacts       []WorldRuntimeFact           `json:"world_runtime_facts"`
	WorldEffectOperations   []WorldEffectOperation       `json:"world_effect_operations"`
	WorldRuleCases          []WorldRuleCase              `json:"world_rule_cases"`
	Events                  []*CityEvent                 `json:"events"`
}

type CityCommandSubmitInput struct {
	UserID            int64
	WorldID           int64
	IdempotencyKey    string
	CommandType       string
	Payload           json.RawMessage
	ExpectedWorldTick *int64
}

type CityStepInput struct {
	UserID            int64
	WorldID           int64
	IdempotencyKey    string
	ExpectedWorldTick *int64
}

type CityEventListInput struct {
	UserID        int64
	WorldID       int64
	AfterTick     int64
	AfterSequence int
	Limit         int
}

type normalizedCityCommand struct {
	commandType string
	payload     json.RawMessage
	fingerprint string
}

type lockedCityWorld struct {
	name              string
	status            string
	simulationVersion string
	seed              int64
	currentTick       int64
	simulatedAt       time.Time
	speedMilli        int64
	timezone          string
	stateHash         *string
	memberRole        string
}

type cityWorldCandidate struct {
	name       string
	status     string
	speedMilli int64
}

type cityPendingEvent struct {
	command   *CityCommand
	status    string
	eventType string
	payload   map[string]any
	result    map[string]any
	errorCode *string
}

func (s *CityEconomyService) SubmitCommand(ctx context.Context, input CityCommandSubmitInput) (*CityCommand, error) {
	if input.UserID <= 0 || input.WorldID <= 0 || (input.ExpectedWorldTick != nil && *input.ExpectedWorldTick < 0) {
		return nil, ErrCityInvalidInput
	}
	requestID, err := requireCityIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeCityCommand(input.CommandType, input.Payload, input.ExpectedWorldTick)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin city command transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	world, err := lockCityWorld(ctx, tx, input.UserID, input.WorldID)
	if err != nil {
		return nil, err
	}
	existing, err := getCityCommandByRequest(ctx, tx, input.WorldID, input.UserID, requestID)
	if err == nil {
		if existing.RequestFingerprint != normalized.fingerprint {
			return nil, ErrCityCommandConflict
		}
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit city command replay: %w", err)
		}
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("find city command replay: %w", err)
	}
	if err = ensureCityWorldWritable(world); err != nil {
		return nil, err
	}
	engine, err := cityEngineForVersion(world.simulationVersion)
	if err != nil {
		return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": world.simulationVersion})
	}
	if !engine.supportsCommand(normalized.commandType) {
		return nil, ErrCityCommandVersion.WithMetadata(map[string]string{
			"version": world.simulationVersion, "command_type": normalized.commandType,
		})
	}
	if input.ExpectedWorldTick != nil && *input.ExpectedWorldTick != world.currentTick {
		return nil, cityExpectedTickError(*input.ExpectedWorldTick, world.currentTick)
	}

	var pendingCount int
	if err = tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_commands WHERE world_id = $1 AND status = 'pending'`, input.WorldID).Scan(&pendingCount); err != nil {
		return nil, fmt.Errorf("count pending city commands: %w", err)
	}
	if pendingCount >= cityPendingCommandMax {
		return nil, ErrCityCommandQueueFull
	}

	var sequence int64
	if err = tx.QueryRowContext(ctx, `
UPDATE city_worlds
SET next_command_sequence = next_command_sequence + 1, updated_at = NOW()
WHERE id = $1
RETURNING next_command_sequence - 1`, input.WorldID).Scan(&sequence); err != nil {
		return nil, fmt.Errorf("allocate city command sequence: %w", err)
	}

	command, err := scanCityCommand(tx.QueryRowContext(ctx, `
INSERT INTO city_commands AS c
    (world_id, user_id, sequence, client_request_id, request_fingerprint,
     command_type, payload, expected_world_tick)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8)
RETURNING `+cityCommandColumns,
		input.WorldID, input.UserID, sequence, requestID, normalized.fingerprint,
		normalized.commandType, []byte(normalized.payload), input.ExpectedWorldTick))
	if err != nil {
		return nil, fmt.Errorf("insert city command: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city command: %w", err)
	}
	return command, nil
}

func (s *CityEconomyService) GetCommand(ctx context.Context, userID, worldID, commandID int64) (*CityCommand, error) {
	if userID <= 0 || worldID <= 0 || commandID <= 0 {
		return nil, ErrCityInvalidInput
	}
	command, err := scanCityCommand(s.db.QueryRowContext(ctx, `
SELECT `+cityCommandColumns+`
FROM city_commands c
JOIN city_members m ON m.world_id = c.world_id AND m.user_id = $1 AND m.status = 'active'
WHERE c.world_id = $2 AND c.id = $3`, userID, worldID, commandID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityCommandNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get city command: %w", err)
	}
	return command, nil
}

func (s *CityEconomyService) StepWorld(ctx context.Context, input CityStepInput) (*CityStepResult, error) {
	result, err := s.stepWorld(ctx, input)
	if err != nil {
		s.recordCityTickFailure(ctx, input, err)
	}
	return result, err
}

func (s *CityEconomyService) stepWorld(ctx context.Context, input CityStepInput) (*CityStepResult, error) {
	if input.UserID <= 0 || input.WorldID <= 0 || (input.ExpectedWorldTick != nil && *input.ExpectedWorldTick < 0) {
		return nil, ErrCityInvalidInput
	}
	requestID, err := requireCityIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	requestFingerprint, err := cityStepFingerprint(input.ExpectedWorldTick)
	if err != nil {
		return nil, err
	}
	startedAt := time.Now().UTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin city tick transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, cityWorldLockKey(input.WorldID)); err != nil {
		return nil, fmt.Errorf("lock city world tick: %w", err)
	}
	world, err := lockCityWorld(ctx, tx, input.UserID, input.WorldID)
	if err != nil {
		return nil, err
	}

	existingTick, err := getCityTickByRequest(ctx, tx, input.WorldID, requestID)
	if err == nil {
		if existingTick.RequestFingerprint != requestFingerprint {
			return nil, ErrCityStepIdempotencyConflict
		}
		result, loadErr := loadCityStepResult(ctx, tx, existingTick)
		if loadErr != nil {
			return nil, loadErr
		}
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit city tick replay: %w", err)
		}
		return result, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("find city tick replay: %w", err)
	}
	if err = ensureCityWorldWritable(world); err != nil {
		return nil, err
	}
	engine, err := cityEngineForVersion(world.simulationVersion)
	if err != nil {
		return nil, ErrCitySimulationVersion.WithMetadata(map[string]string{"version": world.simulationVersion})
	}
	if input.ExpectedWorldTick != nil && *input.ExpectedWorldTick != world.currentTick {
		return nil, cityExpectedTickError(*input.ExpectedWorldTick, world.currentTick)
	}
	world.stateHash, err = ensureCityBaselineSnapshot(ctx, tx, input.WorldID, world)
	if err != nil {
		return nil, err
	}
	if world.currentTick == int64(1<<63-1) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "current_tick"})
	}

	targetTick := world.currentTick + 1
	simulatedTo := world.simulatedAt.Add(cityTickDuration)
	if !simulatedTo.After(world.simulatedAt) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "simulated_at"})
	}
	commands, err := loadPendingCityCommands(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	candidate := &cityWorldCandidate{name: world.name, status: world.status, speedMilli: world.speedMilli}
	pendingEvents := make([]cityPendingEvent, 0, len(commands))
	bootstrapEvents := make([]cityLedgerBootstrapEvent, 0)
	resourceBootstrapEvents := make([]cityResourceBootstrapEvent, 0)
	journalSequence := int64(1)
	resourceOperationSequence := int64(1)
	populationMigrationSequence := int64(1)
	householdMovementSequence := int64(1)
	spatialMutationSequence := int64(1)
	developmentFactSequence := int64(1)
	enterpriseLocationFactSequence := int64(1)
	worldRuntimeFactSequence := int64(1)
	worldEffectSequence := int64(1)
	worldRuleCaseSequence := int64(1)
	var ledgerUnit *cityLedgerBaseUnit
	hasLedgerCommand := false
	needsLedgerBootstrap := false
	hasResourceCommand := false
	marketCycleDue := false
	for _, command := range commands {
		if !engine.supportsCommand(command.CommandType) {
			continue
		}
		if isCityLedgerCommand(command.CommandType) {
			hasLedgerCommand = true
		}
		if cityLedgerCommandNeedsBootstrap(command.CommandType) {
			needsLedgerBootstrap = true
		}
		if isCityResourceCommand(command.CommandType) {
			hasResourceCommand = true
		}
		if command.CommandType == CityCommandTypeDevelopmentStart {
			hasResourceCommand = true
		}
		if command.CommandType == CityCommandTypeEnterpriseRelocate {
			hasResourceCommand = true
		}
	}
	if world.status == CityWorldStatusRunning && engine.hasStage(cityEngineStageMarkets) {
		marketCycleDue, err = cityEconomyCycleDue(ctx, tx, input.WorldID, targetTick)
		if err != nil {
			return nil, err
		}
	}
	if hasLedgerCommand || marketCycleDue {
		ledgerUnit, err = loadCityLedgerBaseUnit(ctx, tx, input.WorldID)
		if err != nil {
			return nil, err
		}
	}
	if needsLedgerBootstrap || marketCycleDue {
		bootstrapEvents, journalSequence, err = s.ensureCityLedgerBootstrap(
			ctx, tx, input.WorldID, targetTick, journalSequence, ledgerUnit,
		)
		if err != nil {
			return nil, err
		}
	}
	if hasResourceCommand || marketCycleDue {
		resourceBootstrapEvents, resourceOperationSequence, err = s.ensureCityResourceBootstrap(
			ctx, tx, input.WorldID, targetTick, resourceOperationSequence,
		)
		if err != nil {
			return nil, err
		}
	}
	appliedCount := 0
	rejectedCount := 0
	populationMigrationCount := 0
	householdMovementCount := 0
	spatialMutationCount := 0
	developmentFactCount := 0
	buildingAdjustmentCount := 0
	enterpriseLocationFactCount := 0
	worldRuntimeFactCount := 0
	worldEffectOperationCount := 0
	worldRuleCaseCount := 0
	if engine.hasStage(cityEngineStageWorldRuntime) {
		automaticRuntime, runtimeErr := expireWorldRuntimeStatuses(
			ctx, tx, input.WorldID, targetTick, worldRuntimeFactSequence, worldEffectSequence,
		)
		if runtimeErr != nil {
			return nil, runtimeErr
		}
		worldRuntimeFactCount += len(automaticRuntime.facts)
		worldEffectOperationCount += len(automaticRuntime.effects)
		worldRuntimeFactSequence = automaticRuntime.nextFactSeq
		worldEffectSequence = automaticRuntime.nextEffectSeq
	}
	for _, command := range commands {
		var pending cityPendingEvent
		if !engine.supportsCommand(command.CommandType) {
			pending = rejectedCityCommand(command, "CITY_COMMAND_VERSION_UNSUPPORTED")
		} else if isCityLedgerCommand(command.CommandType) {
			var journal *CityJournal
			pending, journal, err = s.applyCityLedgerCommand(
				ctx, tx, input.WorldID, targetTick, journalSequence, ledgerUnit, command,
			)
			if err != nil {
				return nil, err
			}
			if journal != nil {
				journalSequence++
			}
		} else if isCityResourceCommand(command.CommandType) {
			var operation *CityResourceOperation
			pending, operation, err = s.applyCityResourceCommand(
				ctx, tx, input.WorldID, targetTick, resourceOperationSequence, command,
			)
			if err != nil {
				return nil, err
			}
			if operation != nil {
				resourceOperationSequence++
			}
		} else if isCityPopulationMigrationCommand(command.CommandType) {
			var migration *CityPopulationMigration
			pending, migration, err = s.applyCityPopulationMigrationCommand(
				ctx, tx, input.WorldID, targetTick, populationMigrationSequence, command,
			)
			if err != nil {
				return nil, err
			}
			if migration != nil {
				populationMigrationSequence++
				populationMigrationCount++
			}
		} else if isCityHouseholdMovementCommand(command.CommandType) {
			var movement *CityHouseholdMovement
			pending, movement, err = s.applyCityHouseholdMovementCommand(
				ctx, tx, input.WorldID, targetTick, householdMovementSequence, command,
			)
			if err != nil {
				return nil, err
			}
			if movement != nil {
				householdMovementSequence++
				householdMovementCount++
			}
		} else if isCitySpatialCommand(command.CommandType) {
			var mutation *CitySpatialMutation
			pending, mutation, err = s.applyCitySpatialCommand(
				ctx, tx, input.WorldID, targetTick, spatialMutationSequence, command,
			)
			if err != nil {
				return nil, err
			}
			if mutation != nil {
				spatialMutationSequence++
				spatialMutationCount++
			}
		} else if isCityDevelopmentCommand(command.CommandType) {
			var execution cityDevelopmentExecution
			execution, err = s.applyCityDevelopmentCommand(
				ctx, tx, input.WorldID, targetTick, developmentFactSequence,
				resourceOperationSequence, command,
			)
			if err != nil {
				return nil, err
			}
			pending = execution.pending
			if execution.fact != nil {
				developmentFactSequence++
				developmentFactCount++
			}
			resourceOperationSequence += int64(len(execution.resourceOperations))
		} else if isCityEnterpriseLocationCommand(command.CommandType) {
			var execution cityEnterpriseLocationExecution
			execution, err = s.applyCityEnterpriseLocationCommand(
				ctx, tx, input.WorldID, targetTick, enterpriseLocationFactSequence,
				resourceOperationSequence, command,
			)
			if err != nil {
				return nil, err
			}
			pending = execution.pending
			if execution.fact != nil {
				enterpriseLocationFactSequence++
				enterpriseLocationFactCount++
			}
			resourceOperationSequence += int64(len(execution.resourceOperations))
		} else if isWorldRuntimeCommand(command.CommandType) {
			var execution worldRuntimeExecution
			execution, err = s.applyWorldRuntimeCommand(
				ctx, tx, input.WorldID, targetTick, worldRuntimeFactSequence,
				worldEffectSequence, worldRuleCaseSequence, command,
			)
			if err != nil {
				return nil, err
			}
			pending = execution.pending
			worldRuntimeFactCount += len(execution.facts)
			worldEffectOperationCount += len(execution.effects)
			worldRuleCaseCount += len(execution.cases)
			worldRuntimeFactSequence = execution.nextFactSeq
			worldEffectSequence = execution.nextEffectSeq
			worldRuleCaseSequence = execution.nextCaseSeq
		} else {
			pending = applyCityControlCommand(candidate, command)
		}
		if pending.status == CityCommandStatusApplied {
			appliedCount++
		} else {
			rejectedCount++
		}
		pendingEvents = append(pendingEvents, pending)
	}
	demographyExecution := cityDemographyExecution{events: make([]cityDemographyEvent, 0)}
	if engine.hasStage(cityEngineStageCalendarDemography) {
		demographyExecution, err = s.advanceCityCalendarAndDemography(
			ctx, tx, input.WorldID, targetTick, world.simulatedAt, simulatedTo, world.timezone,
		)
		if err != nil {
			return nil, err
		}
	}
	if cityEngineSupportsHouseholdLifecycle(engine.version) {
		movements, events, nextSequence, reconcileErr := reconcileCityHouseholdsAfterDemography(
			ctx, tx, input.WorldID, targetTick, householdMovementSequence,
		)
		if reconcileErr != nil {
			return nil, reconcileErr
		}
		householdMovementSequence = nextSequence
		householdMovementCount += len(movements)
		demographyExecution.events = append(demographyExecution.events, events...)
	}
	developmentEvents := make([]cityPendingEvent, 0)
	if candidate.status == CityWorldStatusRunning && engine.hasStage(cityEngineStageDevelopment) {
		developmentExecution, developmentErr := s.advanceCityDevelopmentProjects(
			ctx, tx, input.WorldID, targetTick, developmentFactSequence,
			resourceOperationSequence,
		)
		if developmentErr != nil {
			return nil, developmentErr
		}
		developmentFactSequence = developmentExecution.nextFactSequence
		resourceOperationSequence = developmentExecution.nextResourceSequence
		developmentFactCount += len(developmentExecution.facts)
		buildingAdjustmentCount += developmentExecution.adjustmentCount
		developmentEvents = developmentExecution.events
	}
	marketEvents := make([]cityMarketCycleEvent, 0)
	marketSettlementCount := 0
	if marketCycleDue {
		marketExecution, marketErr := s.settleCityEconomicCycle(
			ctx, tx, input.WorldID, targetTick, journalSequence,
			resourceOperationSequence, ledgerUnit,
		)
		if marketErr != nil {
			return nil, marketErr
		}
		journalSequence = marketExecution.nextJournalSequence
		resourceOperationSequence = marketExecution.nextResourceSequence
		marketEvents = marketExecution.events
		marketSettlementCount = len(marketExecution.settlements)
	}
	journalCount := journalSequence - 1
	resourceOperationCount := resourceOperationSequence - 1
	if _, err = tx.ExecContext(ctx, `
UPDATE city_worlds
SET name = $2,
    status = $3,
    speed_multiplier = ($4::numeric / 1000),
    current_tick = $5,
    simulated_at = $6,
    next_tick_at = NULL,
    updated_at = NOW()
WHERE id = $1`, input.WorldID, candidate.name, candidate.status, candidate.speedMilli, targetTick, simulatedTo); err != nil {
		return nil, fmt.Errorf("update city world tick state: %w", err)
	}

	_, canonical, stateHash, err := canonicalCityWorldState(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	prngProof := deriveCityRandomHex(world.simulationVersion, world.seed, targetTick, "tick", 0)
	completedAt := time.Now().UTC()
	durationMS := completedAt.Sub(startedAt).Milliseconds()
	if durationMS < 0 {
		durationMS = 0
	}
	var firstSequence, lastSequence any
	if len(commands) > 0 {
		firstSequence = commands[0].Sequence
		lastSequence = commands[len(commands)-1].Sequence
	}

	tick, err := scanCityTick(tx.QueryRowContext(ctx, `
INSERT INTO city_ticks AS t
    (world_id, tick, step_request_id, request_fingerprint, initiated_by_user_id,
     simulation_version, previous_state_hash, state_hash, prng_proof,
     simulated_from, simulated_to, first_command_sequence, last_command_sequence,
     command_count, applied_command_count, rejected_command_count, event_count,
     duration_ms, started_at, completed_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17, $18, $19, $20)
RETURNING `+cityTickColumns,
		input.WorldID, targetTick, requestID, requestFingerprint, input.UserID,
		world.simulationVersion, world.stateHash, stateHash, prngProof,
		world.simulatedAt, simulatedTo, firstSequence, lastSequence,
		len(commands), appliedCount, rejectedCount,
		len(bootstrapEvents)+len(resourceBootstrapEvents)+len(commands)+
			len(demographyExecution.events)+len(developmentEvents)+len(marketEvents)+1,
		durationMS, startedAt, completedAt))
	if err != nil {
		return nil, fmt.Errorf("insert city tick: %w", err)
	}

	eventSequence := 1
	for _, bootstrap := range bootstrapEvents {
		if _, err = insertCityEvent(ctx, tx, input.WorldID, targetTick, eventSequence,
			nil, bootstrap.eventType, bootstrap.payload); err != nil {
			return nil, err
		}
		eventSequence++
	}
	for _, bootstrap := range resourceBootstrapEvents {
		if _, err = insertCityEvent(ctx, tx, input.WorldID, targetTick, eventSequence,
			nil, bootstrap.eventType, bootstrap.payload); err != nil {
			return nil, err
		}
		eventSequence++
	}
	for _, pending := range pendingEvents {
		resultJSON, marshalErr := json.Marshal(pending.result)
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal city command result: %w", marshalErr)
		}
		updateResult, updateErr := tx.ExecContext(ctx, `
UPDATE city_commands
SET status = $2, processed_tick = $3, result = $4::jsonb, error_code = $5, updated_at = NOW()
WHERE id = $1 AND status = 'pending'`, pending.command.ID, pending.status, targetTick, resultJSON, pending.errorCode)
		if updateErr != nil {
			return nil, fmt.Errorf("complete city command %d: %w", pending.command.ID, updateErr)
		}
		rowsAffected, rowsErr := updateResult.RowsAffected()
		if rowsErr != nil || rowsAffected != 1 {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"command_id": strconv.FormatInt(pending.command.ID, 10)})
		}
		if _, err = insertCityEvent(ctx, tx, input.WorldID, targetTick, eventSequence,
			&pending.command.ID, pending.eventType, pending.payload); err != nil {
			return nil, err
		}
		eventSequence++
	}
	for _, demographicEvent := range demographyExecution.events {
		if _, err = insertCityEvent(ctx, tx, input.WorldID, targetTick, eventSequence,
			nil, demographicEvent.eventType, demographicEvent.payload); err != nil {
			return nil, err
		}
		eventSequence++
	}
	for _, developmentEvent := range developmentEvents {
		if _, err = insertCityEvent(ctx, tx, input.WorldID, targetTick, eventSequence,
			nil, developmentEvent.eventType, developmentEvent.payload); err != nil {
			return nil, err
		}
		eventSequence++
	}
	for _, marketEvent := range marketEvents {
		if _, err = insertCityEvent(ctx, tx, input.WorldID, targetTick, eventSequence,
			nil, marketEvent.eventType, marketEvent.payload); err != nil {
			return nil, err
		}
		eventSequence++
	}
	completionPayload := map[string]any{
		"tick":                           targetTick,
		"simulated_at":                   simulatedTo.UTC().Format(time.RFC3339Nano),
		"state_hash":                     stateHash,
		"prng_proof":                     prngProof,
		"command_count":                  len(commands),
		"applied_command_count":          appliedCount,
		"rejected_command_count":         rejectedCount,
		"journal_count":                  journalCount,
		"resource_operation_count":       resourceOperationCount,
		"market_settlement_count":        marketSettlementCount,
		"population_movement_count":      demographyExecution.movementCount,
		"population_migration_count":     populationMigrationCount,
		"household_movement_count":       householdMovementCount,
		"spatial_mutation_count":         spatialMutationCount,
		"development_fact_count":         developmentFactCount,
		"building_adjustment_count":      buildingAdjustmentCount,
		"enterprise_location_fact_count": enterpriseLocationFactCount,
		"world_runtime_fact_count":       worldRuntimeFactCount,
		"world_effect_operation_count":   worldEffectOperationCount,
		"world_rule_case_count":          worldRuleCaseCount,
	}
	if _, err = insertCityEvent(ctx, tx, input.WorldID, targetTick, eventSequence,
		nil, "city.tick.completed", completionPayload); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE city_worlds SET state_hash = $2 WHERE id = $1`, input.WorldID, stateHash); err != nil {
		return nil, fmt.Errorf("store city world state hash: %w", err)
	}
	if _, err = captureCitySnapshot(ctx, tx, citySnapshotCapture{
		worldID: input.WorldID, tick: targetTick, sourceTickID: &tick.ID,
		simulationVersion: world.simulationVersion, reason: CitySnapshotReasonTick,
		canonical: canonical, stateHash: stateHash,
	}); err != nil {
		return nil, err
	}

	result, err := loadCityStepResult(ctx, tx, tick)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city tick: %w", err)
	}
	return result, nil
}

func (s *CityEconomyService) recordCityTickFailure(ctx context.Context, input CityStepInput, stepErr error) {
	if s == nil || s.db == nil || !shouldRecordCityTickFailure(stepErr) || input.UserID <= 0 || input.WorldID <= 0 {
		return
	}
	requestID, err := requireCityIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return
	}
	errorCode := infraerrors.Reason(stepErr)
	if errorCode == "" {
		errorCode = "CITY_TICK_FAILED"
	}
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_, _ = s.db.ExecContext(auditCtx, `
INSERT INTO city_tick_failures
    (world_id, requested_by_user_id, client_request_id, simulation_version,
     world_tick, expected_world_tick, error_code, error_detail)
SELECT world.id, $2, $3, world.simulation_version, world.current_tick, $4, $5, $6
FROM city_worlds world WHERE world.id = $1`, input.WorldID, input.UserID, requestID,
		cityNullableInt64(input.ExpectedWorldTick), errorCode, cityAuditDetail(stepErr.Error()))
}

func shouldRecordCityTickFailure(err error) bool {
	return err != nil && !errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded) && infraerrors.Code(err) >= 500
}

func (s *CityEconomyService) ListEvents(ctx context.Context, input CityEventListInput) (*CityEventPage, error) {
	if input.UserID <= 0 || input.WorldID <= 0 || input.AfterTick < 0 || input.AfterSequence < 0 {
		return nil, ErrCityInvalidInput
	}
	if input.Limit <= 0 {
		input.Limit = cityDefaultEventLimit
	}
	if input.Limit > cityMaximumEventLimit {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT `+cityEventColumns+`
FROM city_events e
WHERE e.world_id = $1
  AND (e.tick > $2 OR (e.tick = $2 AND e.sequence > $3))
ORDER BY e.tick ASC, e.sequence ASC
LIMIT $4`, input.WorldID, input.AfterTick, input.AfterSequence, input.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list city events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*CityEvent, 0, input.Limit)
	for rows.Next() {
		item, scanErr := scanCityEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city events: %w", err)
	}
	page := &CityEventPage{Items: items}
	if len(items) > input.Limit {
		items = items[:input.Limit]
		page.Items = items
		last := items[len(items)-1]
		page.NextCursor = &CityEventCursor{Tick: last.Tick, Sequence: last.Sequence}
	}
	return page, nil
}

const cityCommandColumns = `
c.id, c.world_id, c.user_id, c.sequence, c.client_request_id, c.request_fingerprint,
c.command_type, c.payload, c.expected_world_tick, c.status, c.processed_tick,
c.result, c.error_code, c.submitted_at, c.updated_at`

const cityTickColumns = `
t.id, t.world_id, t.tick, t.step_request_id, t.request_fingerprint,
t.initiated_by_user_id, t.simulation_version, t.previous_state_hash, t.state_hash,
t.prng_proof, t.simulated_from, t.simulated_to, t.first_command_sequence,
t.last_command_sequence, t.command_count, t.applied_command_count,
t.rejected_command_count, t.event_count, t.duration_ms, t.started_at, t.completed_at`

const cityEventColumns = `
e.id, e.world_id, e.tick, e.sequence, e.command_id, e.event_type,
e.aggregate_type, e.aggregate_code, e.payload, e.created_at`

func requireCityIdempotencyKey(raw string) (string, error) {
	key, err := NormalizeIdempotencyKey(raw)
	if err != nil {
		return "", err
	}
	if key == "" {
		return "", ErrIdempotencyKeyRequired
	}
	return key, nil
}

func normalizeCityCommand(commandType string, rawPayload json.RawMessage, expectedTick *int64) (*normalizedCityCommand, error) {
	commandType = strings.ToLower(strings.TrimSpace(commandType))
	if len(bytes.TrimSpace(rawPayload)) == 0 {
		rawPayload = json.RawMessage(`{}`)
	}
	var payload any
	switch commandType {
	case CityCommandTypeWorldRename:
		var value struct {
			Name string `json:"name"`
		}
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, ErrCityInvalidInput.WithCause(err)
		}
		value.Name = strings.TrimSpace(value.Name)
		if utf8.RuneCountInString(value.Name) < 1 || utf8.RuneCountInString(value.Name) > 80 {
			return nil, ErrCityInvalidInput
		}
		payload = value
	case CityCommandTypeWorldSetSpeed:
		var value struct {
			SpeedMilli int64 `json:"speed_milli"`
		}
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, ErrCityInvalidInput.WithCause(err)
		}
		if value.SpeedMilli < 1 || value.SpeedMilli > 1_000_000 {
			return nil, ErrCityInvalidInput
		}
		payload = value
	case CityCommandTypeWorldPause, CityCommandTypeWorldResume:
		var value struct{}
		if err := decodeStrictCityObject(rawPayload, &value); err != nil {
			return nil, ErrCityInvalidInput.WithCause(err)
		}
		payload = value
	default:
		ledgerPayload, handled, ledgerErr := normalizeCityLedgerCommand(commandType, rawPayload)
		if ledgerErr != nil {
			return nil, ledgerErr
		}
		if !handled {
			resourcePayload, resourceHandled, resourceErr := normalizeCityResourceCommand(commandType, rawPayload)
			if resourceErr != nil {
				return nil, resourceErr
			}
			if !resourceHandled {
				migrationPayload, migrationHandled, migrationErr := normalizeCityPopulationMigrationCommand(commandType, rawPayload)
				if migrationErr != nil {
					return nil, migrationErr
				}
				if migrationHandled {
					payload = migrationPayload
				} else {
					householdPayload, householdHandled, householdErr := normalizeCityHouseholdMovementCommand(commandType, rawPayload)
					if householdErr != nil {
						return nil, householdErr
					}
					if householdHandled {
						payload = householdPayload
					} else {
						spatialPayload, spatialHandled, spatialErr := normalizeCitySpatialCommand(commandType, rawPayload)
						if spatialErr != nil {
							return nil, spatialErr
						}
						if spatialHandled {
							payload = spatialPayload
						} else {
							developmentPayload, developmentHandled, developmentErr := normalizeCityDevelopmentCommand(commandType, rawPayload)
							if developmentErr != nil {
								return nil, developmentErr
							}
							if developmentHandled {
								payload = developmentPayload
							} else {
								enterprisePayload, enterpriseHandled, enterpriseErr := normalizeCityEnterpriseLocationCommand(commandType, rawPayload)
								if enterpriseErr != nil {
									return nil, enterpriseErr
								}
								if enterpriseHandled {
									payload = enterprisePayload
								} else {
									runtimePayload, runtimeHandled, runtimeErr := normalizeWorldRuntimeCommand(commandType, rawPayload)
									if runtimeErr != nil {
										return nil, runtimeErr
									}
									if !runtimeHandled {
										return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "command_type"})
									}
									payload = runtimePayload
								}
							}
						}
					}
				}
			} else {
				payload = resourcePayload
			}
		} else {
			payload = ledgerPayload
		}
	}
	canonicalPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, ErrCityInvalidInput.WithCause(err)
	}
	fingerprintPayload, err := json.Marshal(struct {
		CommandType       string          `json:"command_type"`
		Payload           json.RawMessage `json:"payload"`
		ExpectedWorldTick *int64          `json:"expected_world_tick"`
	}{commandType, canonicalPayload, expectedTick})
	if err != nil {
		return nil, ErrCityInvalidInput.WithCause(err)
	}
	sum := sha256.Sum256(fingerprintPayload)
	return &normalizedCityCommand{
		commandType: commandType,
		payload:     canonicalPayload,
		fingerprint: hex.EncodeToString(sum[:]),
	}, nil
}

func decodeStrictCityObject(raw json.RawMessage, destination any) error {
	validation := json.NewDecoder(bytes.NewReader(raw))
	token, err := validation.Token()
	if err != nil {
		return err
	}
	opening, ok := token.(json.Delim)
	if !ok || opening != '{' {
		return errors.New("payload must be a JSON object")
	}
	seen := make(map[string]struct{})
	for validation.More() {
		keyToken, tokenErr := validation.Token()
		if tokenErr != nil {
			return tokenErr
		}
		key, ok := keyToken.(string)
		if !ok {
			return errors.New("payload contains a non-string object key")
		}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("payload contains duplicate field %q", key)
		}
		seen[key] = struct{}{}
		var value json.RawMessage
		if tokenErr = validation.Decode(&value); tokenErr != nil {
			return tokenErr
		}
	}
	closing, err := validation.Token()
	if err != nil || closing != json.Delim('}') {
		if err == nil {
			err = errors.New("payload object is not closed")
		}
		return err
	}
	if err = validation.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("payload must contain one JSON object")
		}
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("payload must contain one JSON object")
		}
		return err
	}
	return nil
}

func cityStepFingerprint(expectedTick *int64) (string, error) {
	raw, err := json.Marshal(struct {
		Operation         string `json:"operation"`
		ExpectedWorldTick *int64 `json:"expected_world_tick"`
	}{Operation: "city.step.v1", ExpectedWorldTick: expectedTick})
	if err != nil {
		return "", ErrCityInvalidInput.WithCause(err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func cityExpectedTickError(expected, actual int64) error {
	return ErrCityExpectedTickConflict.WithMetadata(map[string]string{
		"expected_tick": strconv.FormatInt(expected, 10),
		"actual_tick":   strconv.FormatInt(actual, 10),
	})
}

func lockCityWorld(ctx context.Context, tx *sql.Tx, userID, worldID int64) (*lockedCityWorld, error) {
	item := &lockedCityWorld{}
	var stateHash sql.NullString
	err := tx.QueryRowContext(ctx, `
SELECT w.name, w.status, w.simulation_version, w.seed, w.current_tick,
       w.simulated_at, ROUND(w.speed_multiplier * 1000)::bigint,
       w.timezone, w.state_hash, m.role
FROM city_worlds w
JOIN city_members m ON m.world_id = w.id
WHERE w.id = $1 AND m.user_id = $2 AND m.status = 'active'
FOR UPDATE OF w`, worldID, userID).Scan(
		&item.name, &item.status, &item.simulationVersion, &item.seed, &item.currentTick,
		&item.simulatedAt, &item.speedMilli, &item.timezone, &stateHash, &item.memberRole,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityWorldNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lock city world: %w", err)
	}
	if stateHash.Valid {
		item.stateHash = &stateHash.String
	}
	return item, nil
}

func ensureCityWorldWritable(world *lockedCityWorld) error {
	if world.memberRole != CityMemberRoleOwner {
		return ErrCityPermissionDenied
	}
	if _, err := cityEngineForVersion(world.simulationVersion); err != nil {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": world.simulationVersion})
	}
	if world.status != CityWorldStatusPaused && world.status != CityWorldStatusRunning {
		return ErrCityWorldUnavailable.WithMetadata(map[string]string{"status": world.status})
	}
	return nil
}

func authorizeCityWorldRead(ctx context.Context, queryer citySQLQueryer, userID, worldID int64) error {
	var exists int
	err := queryer.QueryRowContext(ctx, `
SELECT 1 FROM city_members
WHERE world_id = $1 AND user_id = $2 AND status = 'active'`, worldID, userID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCityWorldNotFound
	}
	if err != nil {
		return fmt.Errorf("authorize city world read: %w", err)
	}
	return nil
}

func getCityCommandByRequest(ctx context.Context, queryer citySQLQueryer, worldID, userID int64, requestID string) (*CityCommand, error) {
	return scanCityCommand(queryer.QueryRowContext(ctx, `
SELECT `+cityCommandColumns+`
FROM city_commands c
WHERE c.world_id = $1 AND c.user_id = $2 AND c.client_request_id = $3`, worldID, userID, requestID))
}

func loadPendingCityCommands(ctx context.Context, tx *sql.Tx, worldID int64) ([]*CityCommand, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT `+cityCommandColumns+`
FROM city_commands c
WHERE c.world_id = $1 AND c.status = 'pending'
ORDER BY c.sequence ASC
FOR UPDATE`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load pending city commands: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*CityCommand, 0)
	for rows.Next() {
		item, scanErr := scanCityCommand(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending city commands: %w", err)
	}
	return items, nil
}

func applyCityControlCommand(candidate *cityWorldCandidate, command *CityCommand) cityPendingEvent {
	pending := cityPendingEvent{
		command: command,
		status:  CityCommandStatusApplied,
		result:  map[string]any{"applied": true},
	}
	switch command.CommandType {
	case CityCommandTypeWorldRename:
		name, ok := command.Payload["name"].(string)
		name = strings.TrimSpace(name)
		if !ok || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 80 {
			return rejectedCityCommand(command, "CITY_COMMAND_PAYLOAD_INVALID")
		}
		previous := candidate.name
		candidate.name = name
		pending.eventType = "city.world.renamed"
		pending.payload = map[string]any{"previous_name": previous, "name": name}
		pending.result["name"] = name
	case CityCommandTypeWorldSetSpeed:
		speed, ok := cityJSONInteger(command.Payload["speed_milli"])
		if !ok || speed < 1 || speed > 1_000_000 {
			return rejectedCityCommand(command, "CITY_COMMAND_PAYLOAD_INVALID")
		}
		previous := candidate.speedMilli
		candidate.speedMilli = speed
		pending.eventType = "city.world.speed_changed"
		pending.payload = map[string]any{"previous_speed_milli": previous, "speed_milli": speed}
		pending.result["speed_milli"] = speed
	case CityCommandTypeWorldPause:
		previous := candidate.status
		candidate.status = CityWorldStatusPaused
		pending.eventType = "city.world.paused"
		pending.payload = map[string]any{"previous_status": previous, "status": candidate.status}
		pending.result["status"] = candidate.status
	case CityCommandTypeWorldResume:
		previous := candidate.status
		candidate.status = CityWorldStatusRunning
		pending.eventType = "city.world.resumed"
		pending.payload = map[string]any{"previous_status": previous, "status": candidate.status}
		pending.result["status"] = candidate.status
	default:
		return rejectedCityCommand(command, cityCommandRejectedKey)
	}
	return pending
}

func rejectedCityCommand(command *CityCommand, code string) cityPendingEvent {
	return cityPendingEvent{
		command:   command,
		status:    CityCommandStatusRejected,
		eventType: "city.command.rejected",
		payload:   map[string]any{"command_type": command.CommandType, "error_code": code},
		result:    map[string]any{"applied": false, "error_code": code},
		errorCode: &code,
	}
}

func cityJSONInteger(value any) (int64, bool) {
	switch number := value.(type) {
	case float64:
		converted := int64(number)
		return converted, float64(converted) == number
	case json.Number:
		converted, err := number.Int64()
		return converted, err == nil
	case int64:
		return number, true
	case int:
		return int64(number), true
	default:
		return 0, false
	}
}

func insertCityEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID, tick int64,
	sequence int,
	commandID *int64,
	eventType string,
	payload map[string]any,
) (*CityEvent, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal city event %s: %w", eventType, err)
	}
	event, err := scanCityEvent(tx.QueryRowContext(ctx, `
INSERT INTO city_events AS e
    (world_id, tick, sequence, command_id, event_type, aggregate_type, aggregate_code, payload)
VALUES ($1, $2, $3, $4, $5, 'world', 'world', $6::jsonb)
RETURNING `+cityEventColumns,
		worldID, tick, sequence, commandID, eventType, raw))
	if err != nil {
		return nil, fmt.Errorf("insert city event %s: %w", eventType, err)
	}
	return event, nil
}

func getCityTickByRequest(ctx context.Context, queryer citySQLQueryer, worldID int64, requestID string) (*CityTick, error) {
	return scanCityTick(queryer.QueryRowContext(ctx, `
SELECT `+cityTickColumns+`
FROM city_ticks t
WHERE t.world_id = $1 AND t.step_request_id = $2`, worldID, requestID))
}

func loadCityStepResult(ctx context.Context, queryer citySQLQueryer, tick *CityTick) (*CityStepResult, error) {
	commandRows, err := queryer.QueryContext(ctx, `
SELECT `+cityCommandColumns+`
FROM city_commands c
WHERE c.world_id = $1 AND c.processed_tick = $2
ORDER BY c.sequence ASC`, tick.WorldID, tick.Tick)
	if err != nil {
		return nil, fmt.Errorf("load city tick commands: %w", err)
	}
	commands := make([]*CityCommand, 0, tick.CommandCount)
	for commandRows.Next() {
		command, scanErr := scanCityCommand(commandRows)
		if scanErr != nil {
			_ = commandRows.Close()
			return nil, scanErr
		}
		commands = append(commands, command)
	}
	if err = commandRows.Err(); err != nil {
		_ = commandRows.Close()
		return nil, fmt.Errorf("iterate city tick commands: %w", err)
	}
	_ = commandRows.Close()
	journals, err := loadCityJournalsForTick(ctx, queryer, tick.WorldID, tick.Tick)
	if err != nil {
		return nil, err
	}
	resourceOperations, err := loadCityResourceOperationsForTick(ctx, queryer, tick.WorldID, tick.Tick)
	if err != nil {
		return nil, err
	}
	marketSettlements, err := loadCityMarketSettlementsForTick(ctx, queryer, tick.WorldID, tick.Tick)
	if err != nil {
		return nil, err
	}
	populationMovements, err := loadCityPopulationMovementsForTick(ctx, queryer, tick.WorldID, tick.Tick)
	if err != nil {
		return nil, err
	}
	populationMigrations, err := loadCityPopulationMigrationsForTick(ctx, queryer, tick.WorldID, tick.Tick)
	if err != nil {
		return nil, err
	}
	householdMovements, err := loadCityHouseholdMovementsForTick(ctx, queryer, tick.WorldID, tick.Tick)
	if err != nil {
		return nil, err
	}
	spatialMutations, err := loadCitySpatialMutationsForTick(ctx, queryer, tick.WorldID, tick.Tick)
	if err != nil {
		return nil, err
	}
	developmentFacts, err := loadCityDevelopmentFactsForTick(ctx, queryer, tick.WorldID, tick.Tick)
	if err != nil {
		return nil, err
	}
	buildingAdjustments, err := loadCityBuildingAdjustmentsForTick(ctx, queryer, tick.WorldID, tick.Tick)
	if err != nil {
		return nil, err
	}
	enterpriseLocationFacts, err := loadCityEnterpriseLocationFactsForTick(ctx, queryer, tick.WorldID, tick.Tick)
	if err != nil {
		return nil, err
	}
	worldRuntimeFacts, err := loadWorldRuntimeFactsForTick(ctx, queryer, tick.WorldID, tick.Tick)
	if err != nil {
		return nil, err
	}
	worldEffectOperations, err := loadWorldEffectOperations(ctx, queryer, tick.WorldID, tick.Tick)
	if err != nil {
		return nil, err
	}
	worldRuleCases, err := loadWorldRuleCasesForTick(ctx, queryer, tick.WorldID, tick.Tick)
	if err != nil {
		return nil, err
	}

	eventRows, err := queryer.QueryContext(ctx, `
SELECT `+cityEventColumns+`
FROM city_events e
WHERE e.world_id = $1 AND e.tick = $2
ORDER BY e.sequence ASC`, tick.WorldID, tick.Tick)
	if err != nil {
		return nil, fmt.Errorf("load city tick events: %w", err)
	}
	events := make([]*CityEvent, 0, tick.EventCount)
	for eventRows.Next() {
		event, scanErr := scanCityEvent(eventRows)
		if scanErr != nil {
			_ = eventRows.Close()
			return nil, scanErr
		}
		events = append(events, event)
	}
	if err = eventRows.Err(); err != nil {
		_ = eventRows.Close()
		return nil, fmt.Errorf("iterate city tick events: %w", err)
	}
	_ = eventRows.Close()
	if len(commands) != tick.CommandCount || len(events) != tick.EventCount {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"tick": strconv.FormatInt(tick.Tick, 10)})
	}
	return &CityStepResult{
		Tick: tick, Commands: commands, Journals: journals,
		ResourceOperations: resourceOperations, MarketSettlements: marketSettlements,
		PopulationMovements: populationMovements, PopulationMigrations: populationMigrations,
		HouseholdMovements:      householdMovements,
		SpatialMutations:        spatialMutations,
		DevelopmentFacts:        developmentFacts,
		BuildingAdjustments:     buildingAdjustments,
		EnterpriseLocationFacts: enterpriseLocationFacts,
		WorldRuntimeFacts:       worldRuntimeFacts,
		WorldEffectOperations:   worldEffectOperations,
		WorldRuleCases:          worldRuleCases,
		Events:                  events,
	}, nil
}

func scanCityCommand(row cityScannable) (*CityCommand, error) {
	item := &CityCommand{}
	var payload, result []byte
	var expectedTick, processedTick sql.NullInt64
	var errorCode sql.NullString
	if err := row.Scan(
		&item.ID, &item.WorldID, &item.UserID, &item.Sequence, &item.ClientRequestID,
		&item.RequestFingerprint, &item.CommandType, &payload, &expectedTick, &item.Status,
		&processedTick, &result, &errorCode, &item.SubmittedAt, &item.UpdatedAt,
	); err != nil {
		return nil, err
	}
	item.ExpectedWorldTick = nullInt64Pointer(expectedTick)
	item.ProcessedTick = nullInt64Pointer(processedTick)
	if errorCode.Valid {
		item.ErrorCode = &errorCode.String
	}
	var err error
	item.Payload, err = decodeCityJSONMap(payload)
	if err != nil {
		return nil, fmt.Errorf("decode city command payload: %w", err)
	}
	item.Result, err = decodeCityJSONMap(result)
	if err != nil {
		return nil, fmt.Errorf("decode city command result: %w", err)
	}
	return item, nil
}

func scanCityTick(row cityScannable) (*CityTick, error) {
	item := &CityTick{}
	var previousHash sql.NullString
	var firstSequence, lastSequence sql.NullInt64
	if err := row.Scan(
		&item.ID, &item.WorldID, &item.Tick, &item.StepRequestID, &item.RequestFingerprint,
		&item.InitiatedByUserID, &item.SimulationVersion, &previousHash, &item.StateHash,
		&item.PRNGProof, &item.SimulatedFrom, &item.SimulatedTo, &firstSequence, &lastSequence,
		&item.CommandCount, &item.AppliedCommandCount, &item.RejectedCommandCount,
		&item.EventCount, &item.DurationMS, &item.StartedAt, &item.CompletedAt,
	); err != nil {
		return nil, err
	}
	if previousHash.Valid {
		item.PreviousStateHash = &previousHash.String
	}
	item.FirstCommandSequence = nullInt64Pointer(firstSequence)
	item.LastCommandSequence = nullInt64Pointer(lastSequence)
	return item, nil
}

func scanCityEvent(row cityScannable) (*CityEvent, error) {
	item := &CityEvent{}
	var commandID sql.NullInt64
	var payload []byte
	if err := row.Scan(
		&item.ID, &item.WorldID, &item.Tick, &item.Sequence, &commandID, &item.EventType,
		&item.AggregateType, &item.AggregateCode, &payload, &item.CreatedAt,
	); err != nil {
		return nil, err
	}
	item.CommandID = nullInt64Pointer(commandID)
	var err error
	item.Payload, err = decodeCityJSONMap(payload)
	if err != nil {
		return nil, fmt.Errorf("decode city event payload: %w", err)
	}
	return item, nil
}

func cityWorldLockKey(worldID int64) int64 {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte("city_world:"))
	_, _ = hasher.Write([]byte(strconv.FormatInt(worldID, 10)))
	return int64(hasher.Sum64())
}

func deriveCityRandomHex(version string, seed, tick int64, subsystem string, sequence int64) string {
	hasher := sha256.New()
	writeCityHashString(hasher, version)
	writeCityHashInt64(hasher, seed)
	writeCityHashInt64(hasher, tick)
	writeCityHashString(hasher, subsystem)
	writeCityHashInt64(hasher, sequence)
	return hex.EncodeToString(hasher.Sum(nil))
}

type cityHashWriter interface {
	Write([]byte) (int, error)
}

func writeCityHashString(writer cityHashWriter, value string) {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(value)))
	_, _ = writer.Write(size[:])
	_, _ = writer.Write([]byte(value))
}

func writeCityHashInt64(writer cityHashWriter, value int64) {
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], uint64(value))
	_, _ = writer.Write(raw[:])
}

type cityHashState struct {
	Name               string                           `json:"name"`
	Status             string                           `json:"status"`
	SimulationVersion  string                           `json:"simulation_version"`
	Seed               int64                            `json:"seed"`
	CurrentTick        int64                            `json:"current_tick"`
	SimulatedAt        string                           `json:"simulated_at"`
	SpeedMilli         int64                            `json:"speed_milli"`
	Timezone           string                           `json:"timezone"`
	Settings           json.RawMessage                  `json:"settings"`
	MonetaryUnits      []cityHashMonetaryUnit           `json:"monetary_units"`
	AccountTemplates   []cityHashAccountTemplate        `json:"account_templates"`
	Entities           []cityHashEntity                 `json:"entities"`
	Accounts           []cityHashAccount                `json:"accounts"`
	Physical           cityPhysicalHashState            `json:"physical"`
	Markets            cityMarketHashState              `json:"markets"`
	Demography         cityDemographyHashState          `json:"demography"`
	Spatial            *citySpatialHashState            `json:"spatial,omitempty"`
	Land               *cityLandHashState               `json:"land,omitempty"`
	Development        *cityDevelopmentHashState        `json:"development,omitempty"`
	EnterpriseLocation *cityEnterpriseLocationHashState `json:"enterprise_location,omitempty"`
	WorldRuntime       *worldRuntimeHashState           `json:"world_runtime,omitempty"`
}

type cityHashMonetaryUnit struct {
	Code     string          `json:"code"`
	Name     string          `json:"name"`
	Symbol   string          `json:"symbol"`
	Scale    int             `json:"scale"`
	Status   string          `json:"status"`
	IsBase   bool            `json:"is_base"`
	Metadata json.RawMessage `json:"metadata"`
}

type cityHashAccountTemplate struct {
	EntityType    string          `json:"entity_type"`
	Code          string          `json:"code"`
	Name          string          `json:"name"`
	AccountClass  string          `json:"account_class"`
	NormalSide    string          `json:"normal_side"`
	AllowNegative bool            `json:"allow_negative"`
	IsRequired    bool            `json:"is_required"`
	SortOrder     int             `json:"sort_order"`
	Metadata      json.RawMessage `json:"metadata"`
}

type cityHashEntity struct {
	EntityType string          `json:"entity_type"`
	Code       string          `json:"code"`
	Name       string          `json:"name"`
	Status     string          `json:"status"`
	Metadata   json.RawMessage `json:"metadata"`
}

type cityHashAccount struct {
	EntityType         string          `json:"entity_type"`
	EntityCode         string          `json:"entity_code"`
	MonetaryUnitCode   string          `json:"monetary_unit_code"`
	TemplateCode       string          `json:"template_code"`
	AllowNegative      bool            `json:"allow_negative"`
	CurrentBalanceUnit int64           `json:"current_balance_units"`
	Version            int64           `json:"version"`
	Status             string          `json:"status"`
	Metadata           json.RawMessage `json:"metadata"`
}

func loadCityHashState(ctx context.Context, queryer citySQLQueryer, worldID int64) (cityHashState, error) {
	state := cityHashState{
		MonetaryUnits:    make([]cityHashMonetaryUnit, 0),
		AccountTemplates: make([]cityHashAccountTemplate, 0),
		Entities:         make([]cityHashEntity, 0),
		Accounts:         make([]cityHashAccount, 0),
	}
	var simulatedAt time.Time
	if err := queryer.QueryRowContext(ctx, `
SELECT name, status, simulation_version, seed, current_tick, simulated_at,
       ROUND(speed_multiplier * 1000)::bigint, timezone, settings
FROM city_worlds WHERE id = $1`, worldID).Scan(
		&state.Name, &state.Status, &state.SimulationVersion, &state.Seed, &state.CurrentTick,
		&simulatedAt, &state.SpeedMilli, &state.Timezone, &state.Settings,
	); err != nil {
		return state, fmt.Errorf("load city world hash state: %w", err)
	}
	state.SimulatedAt = simulatedAt.UTC().Format(time.RFC3339Nano)

	unitRows, err := queryer.QueryContext(ctx, `
SELECT code, name, symbol, scale, status, is_base, metadata
FROM city_monetary_units WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return state, fmt.Errorf("load city monetary units for hash: %w", err)
	}
	for unitRows.Next() {
		var item cityHashMonetaryUnit
		if err = unitRows.Scan(&item.Code, &item.Name, &item.Symbol, &item.Scale, &item.Status, &item.IsBase, &item.Metadata); err != nil {
			_ = unitRows.Close()
			return state, err
		}
		state.MonetaryUnits = append(state.MonetaryUnits, item)
	}
	if err = unitRows.Err(); err != nil {
		_ = unitRows.Close()
		return state, err
	}
	_ = unitRows.Close()

	templateRows, err := queryer.QueryContext(ctx, `
SELECT entity_type, code, name, account_class, normal_side, allow_negative,
       is_required, sort_order, metadata
FROM city_account_templates WHERE world_id = $1
ORDER BY entity_type ASC, sort_order ASC, code ASC`, worldID)
	if err != nil {
		return state, fmt.Errorf("load city account templates for hash: %w", err)
	}
	for templateRows.Next() {
		var item cityHashAccountTemplate
		if err = templateRows.Scan(&item.EntityType, &item.Code, &item.Name, &item.AccountClass,
			&item.NormalSide, &item.AllowNegative, &item.IsRequired, &item.SortOrder, &item.Metadata); err != nil {
			_ = templateRows.Close()
			return state, err
		}
		state.AccountTemplates = append(state.AccountTemplates, item)
	}
	if err = templateRows.Err(); err != nil {
		_ = templateRows.Close()
		return state, err
	}
	_ = templateRows.Close()

	entityRows, err := queryer.QueryContext(ctx, `
SELECT entity_type, code, name, status, metadata
FROM city_economic_entities WHERE world_id = $1
ORDER BY entity_type ASC, code ASC`, worldID)
	if err != nil {
		return state, fmt.Errorf("load city entities for hash: %w", err)
	}
	for entityRows.Next() {
		var item cityHashEntity
		if err = entityRows.Scan(&item.EntityType, &item.Code, &item.Name, &item.Status, &item.Metadata); err != nil {
			_ = entityRows.Close()
			return state, err
		}
		state.Entities = append(state.Entities, item)
	}
	if err = entityRows.Err(); err != nil {
		_ = entityRows.Close()
		return state, err
	}
	_ = entityRows.Close()

	accountRows, err := queryer.QueryContext(ctx, `
SELECT e.entity_type, e.code, u.code, t.code, a.allow_negative,
       a.current_balance_units, a.version, a.status, a.metadata
FROM city_accounts a
JOIN city_economic_entities e ON e.id = a.entity_id
JOIN city_monetary_units u ON u.id = a.monetary_unit_id
JOIN city_account_templates t ON t.id = a.template_id
WHERE a.world_id = $1
ORDER BY e.entity_type ASC, e.code ASC, u.code ASC, t.code ASC`, worldID)
	if err != nil {
		return state, fmt.Errorf("load city accounts for hash: %w", err)
	}
	for accountRows.Next() {
		var item cityHashAccount
		if err = accountRows.Scan(&item.EntityType, &item.EntityCode, &item.MonetaryUnitCode,
			&item.TemplateCode, &item.AllowNegative, &item.CurrentBalanceUnit, &item.Version,
			&item.Status, &item.Metadata); err != nil {
			_ = accountRows.Close()
			return state, err
		}
		state.Accounts = append(state.Accounts, item)
	}
	if err = accountRows.Err(); err != nil {
		_ = accountRows.Close()
		return state, err
	}
	_ = accountRows.Close()

	state.Physical, err = loadCityPhysicalHashState(ctx, queryer, worldID)
	if err != nil {
		return state, err
	}
	state.Markets, err = loadCityMarketHashState(ctx, queryer, worldID)
	if err != nil {
		return state, err
	}
	state.Demography, err = loadCityDemographyHashState(ctx, queryer, worldID)
	if err != nil {
		return state, err
	}
	if cityEngineSupportsSpatial(state.SimulationVersion) {
		state.Spatial, err = loadCitySpatialHashState(
			ctx, queryer, worldID, state.SimulationVersion, state.Seed,
		)
		if err != nil {
			return state, err
		}
	}
	if cityEngineSupportsLand(state.SimulationVersion) {
		state.Land, err = loadCityLandHashState(
			ctx, queryer, worldID, state.SimulationVersion, state.Seed,
		)
		if err != nil {
			return state, err
		}
	}
	if cityEngineSupportsDevelopment(state.SimulationVersion) {
		state.Development, err = loadCityDevelopmentHashState(
			ctx, queryer, worldID, state.SimulationVersion,
		)
		if err != nil {
			return state, err
		}
	}
	if cityEngineSupportsEnterpriseLocation(state.SimulationVersion) {
		state.EnterpriseLocation, err = loadCityEnterpriseLocationHashState(
			ctx, queryer, worldID, state.SimulationVersion,
		)
		if err != nil {
			return state, err
		}
	}
	if cityEngineSupportsWorldRuntime(state.SimulationVersion) {
		state.WorldRuntime, err = loadWorldRuntimeHashState(ctx, queryer, worldID)
		if err != nil {
			return state, err
		}
	}
	return state, nil
}

func canonicalCityWorldState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (cityHashState, []byte, string, error) {
	state, err := loadCityHashState(ctx, queryer, worldID)
	if err != nil {
		return cityHashState{}, nil, "", err
	}
	raw, err := marshalCanonicalCityState(state)
	if err != nil {
		return cityHashState{}, nil, "", fmt.Errorf("marshal city hash state: %w", err)
	}
	sum := sha256.Sum256(raw)
	return state, raw, hex.EncodeToString(sum[:]), nil
}

func hashCityWorldState(ctx context.Context, queryer citySQLQueryer, worldID int64) (string, error) {
	_, _, hash, err := canonicalCityWorldState(ctx, queryer, worldID)
	return hash, err
}

func CityTickEpoch() time.Time {
	return cityTickEpochTime
}
