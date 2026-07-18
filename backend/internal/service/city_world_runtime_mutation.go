package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
)

const (
	worldRuntimeRejectionActorLimit    = "WORLD_ACTOR_LIMIT_REACHED"
	worldRuntimeRejectionActorNotFound = "WORLD_ACTOR_NOT_FOUND"
	worldRuntimeRejectionDefinition    = "WORLD_RUNTIME_DEFINITION_UNAVAILABLE"
	worldRuntimeRejectionRequirement   = "WORLD_REQUIREMENT_NOT_SATISFIED"
	worldRuntimeRejectionActivityLimit = "WORLD_ACTIVITY_TICK_LIMIT_REACHED"
	worldRuntimeRejectionRoleActive    = "WORLD_ROLE_ALREADY_ACTIVE"
	worldRuntimeRejectionRoleCooldown  = "WORLD_ROLE_TRANSITION_COOLDOWN"
	worldRuntimeRejectionEffectInvalid = "WORLD_EFFECT_INVALID"
	worldRuntimeRejectionEffectLimit   = "WORLD_EFFECT_LIMIT_EXCEEDED"
)

type worldRuntimeBusinessError struct{ code string }

func (err *worldRuntimeBusinessError) Error() string { return err.code }

func worldRuntimeReject(code string) error { return &worldRuntimeBusinessError{code: code} }

func worldRuntimeBusinessRejectionCode(err error) string {
	var businessErr *worldRuntimeBusinessError
	if errors.As(err, &businessErr) {
		return businessErr.code
	}
	if errors.Is(err, ErrWorldRuntimeDefinitionNotFound) {
		return worldRuntimeRejectionDefinition
	}
	return ""
}

type worldRuntimeActorRef struct {
	id    int64
	actor WorldActor
}

type worldRuntimeFactRecord struct {
	id   int64
	fact WorldRuntimeFact
}

type worldRuntimeExecution struct {
	pending       cityPendingEvent
	facts         []WorldRuntimeFact
	effects       []WorldEffectOperation
	cases         []WorldRuleCase
	nextFactSeq   int64
	nextEffectSeq int64
	nextCaseSeq   int64
}

func (s *CityEconomyService) applyWorldRuntimeCommand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
) (worldRuntimeExecution, error) {
	if _, err := tx.ExecContext(ctx, `SAVEPOINT world_runtime_command`); err != nil {
		return worldRuntimeExecution{}, fmt.Errorf("create world runtime command savepoint: %w", err)
	}
	execution, err := s.postWorldRuntimeCommand(
		ctx, tx, worldID, targetTick, factSequence, effectSequence, caseSequence, command,
	)
	if err != nil {
		if _, rollbackErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT world_runtime_command`); rollbackErr != nil {
			return worldRuntimeExecution{}, fmt.Errorf("rollback world runtime command after %v: %w", err, rollbackErr)
		}
		if _, releaseErr := tx.ExecContext(ctx, `RELEASE SAVEPOINT world_runtime_command`); releaseErr != nil {
			return worldRuntimeExecution{}, fmt.Errorf("release rejected world runtime command: %w", releaseErr)
		}
		if code := worldRuntimeBusinessRejectionCode(err); code != "" {
			return worldRuntimeExecution{
				pending:     rejectedCityCommand(command, code),
				nextFactSeq: factSequence, nextEffectSeq: effectSequence, nextCaseSeq: caseSequence,
			}, nil
		}
		return worldRuntimeExecution{}, err
	}
	if _, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT world_runtime_command`); err != nil {
		return worldRuntimeExecution{}, fmt.Errorf("release world runtime command savepoint: %w", err)
	}
	return execution, nil
}

func (s *CityEconomyService) postWorldRuntimeCommand(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
) (worldRuntimeExecution, error) {
	switch command.CommandType {
	case CityCommandTypeActorCreate:
		payload, err := decodeStoredCityCommandPayload[worldActorCreatePayload](command)
		if err != nil {
			return worldRuntimeExecution{}, err
		}
		return s.createWorldActor(ctx, tx, worldID, targetTick, factSequence, effectSequence, caseSequence, command, payload)
	case CityCommandTypeActorActivityPerform:
		payload, err := decodeStoredCityCommandPayload[worldActorActivityPayload](command)
		if err != nil {
			return worldRuntimeExecution{}, err
		}
		return s.performWorldActorActivity(ctx, tx, worldID, targetTick, factSequence, effectSequence, caseSequence, command, payload)
	case CityCommandTypeActorRoleTransition:
		payload, err := decodeStoredCityCommandPayload[worldActorRoleTransitionPayload](command)
		if err != nil {
			return worldRuntimeExecution{}, err
		}
		return s.transitionWorldActorRole(ctx, tx, worldID, targetTick, factSequence, effectSequence, caseSequence, command, payload)
	default:
		return worldRuntimeExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"command_type": command.CommandType})
	}
}

func (s *CityEconomyService) createWorldActor(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	payload worldActorCreatePayload,
) (worldRuntimeExecution, error) {
	var maximumActors int
	if err := tx.QueryRowContext(ctx, `
SELECT maximum_player_actors_per_member
FROM world_runtime_profiles WHERE world_id = $1 FOR UPDATE`, worldID).Scan(&maximumActors); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return worldRuntimeExecution{}, worldRuntimeReject(worldRuntimeRejectionDefinition)
		}
		return worldRuntimeExecution{}, fmt.Errorf("load world runtime profile for actor creation: %w", err)
	}
	var activeActors int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM world_actors
WHERE world_id = $1 AND owner_user_id = $2 AND status = 'active'`, worldID, command.UserID).Scan(&activeActors); err != nil {
		return worldRuntimeExecution{}, fmt.Errorf("count controlled world actors: %w", err)
	}
	if activeActors >= maximumActors {
		return worldRuntimeExecution{}, worldRuntimeReject(worldRuntimeRejectionActorLimit)
	}
	archetypeDefinition, err := loadWorldRuntimeDefinition(ctx, tx, worldID, WorldRuntimeDefinitionArchetype, payload.ArchetypeCode)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	archetype, err := decodeWorldRuntimeDefinition[worldRuntimeArchetypeDefinition](archetypeDefinition)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	if _, err = loadWorldRuntimeDefinition(ctx, tx, worldID, WorldRuntimeDefinitionActorType, archetype.ActorTypeCode); err != nil {
		return worldRuntimeExecution{}, err
	}
	actorCode := fmt.Sprintf("actor_%08d", command.Sequence)
	archetypeCode, archetypeVersion := archetypeDefinition.Code, archetypeDefinition.Version
	ownerID := command.UserID
	actor := WorldActor{
		Code: actorCode, OwnerUserID: &ownerID, ActorTypeCode: archetype.ActorTypeCode,
		Name: payload.Name, Status: "active", ArchetypeCode: &archetypeCode,
		ArchetypeVersion: &archetypeVersion, CreatedTick: targetTick, UpdatedTick: targetTick,
		Version: 1, Metadata: json.RawMessage(`{"schema_version":1,"control_mode":"owner"}`),
	}
	factPayload, err := json.Marshal(map[string]any{
		"schema_version": 1, "actor": actor, "archetype_code": archetypeDefinition.Code,
	})
	if err != nil {
		return worldRuntimeExecution{}, fmt.Errorf("marshal actor creation fact: %w", err)
	}
	root, err := insertWorldRuntimeFact(ctx, tx, worldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		sourceCommandID: &command.ID, factType: WorldRuntimeFactActorCreated,
		definition: archetypeDefinition, payload: factPayload,
	})
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = enableWorldRuntimeFactWrite(ctx, tx, root.id); err != nil {
		return worldRuntimeExecution{}, err
	}
	var actorID int64
	if err = tx.QueryRowContext(ctx, `
INSERT INTO world_actors
    (world_id, code, owner_user_id, actor_type_code, name, status,
     archetype_code, archetype_version, created_tick, updated_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, 'active', $6, $7, $8, $8, 1, $9::jsonb)
RETURNING id`, worldID, actor.Code, command.UserID, actor.ActorTypeCode, actor.Name,
		archetypeDefinition.Code, archetypeDefinition.Version, targetTick, []byte(actor.Metadata)).Scan(&actorID); err != nil {
		return worldRuntimeExecution{}, fmt.Errorf("insert world actor: %w", err)
	}
	actorRef := &worldRuntimeActorRef{id: actorID, actor: actor}
	effectSpecs := make([]worldRuntimeEffectSpec, 0, len(archetype.InitialAttributes)+len(archetype.InitialRoles))
	attributeCodes := make([]string, 0, len(archetype.InitialAttributes))
	for code := range archetype.InitialAttributes {
		attributeCodes = append(attributeCodes, code)
	}
	sort.Strings(attributeCodes)
	for _, code := range attributeCodes {
		effectSpecs = append(effectSpecs, worldRuntimeEffectSpec{
			Type: WorldRuntimeEffectAttributeSet, Key: code, ValueUnits: archetype.InitialAttributes[code],
		})
	}
	for _, roleCode := range archetype.InitialRoles {
		effectSpecs = append(effectSpecs, worldRuntimeEffectSpec{Type: WorldRuntimeEffectRoleGrant, Key: roleCode})
	}
	if len(effectSpecs) > worldRuntimeMaximumEffects {
		return worldRuntimeExecution{}, worldRuntimeReject(worldRuntimeRejectionEffectLimit)
	}
	effects, nextEffectSequence, err := applyWorldRuntimeEffects(
		ctx, tx, worldID, targetTick, effectSequence, actorRef, root, effectSpecs,
	)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = updateWorldRuntimeProfile(ctx, tx, worldID, 1, 1, int64(len(effects)), 0); err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = postWorldRuntimeFact(ctx, tx, root.id); err != nil {
		return worldRuntimeExecution{}, err
	}
	root.fact.ActorCode = &actorCode
	root.fact.Payload = factPayload
	pending := cityPendingEvent{
		command: command, status: CityCommandStatusApplied, eventType: "world.actor.created",
		payload: map[string]any{"actor_code": actorCode, "archetype_code": archetypeDefinition.Code},
		result:  map[string]any{"applied": true, "actor_code": actorCode},
	}
	return worldRuntimeExecution{
		pending: pending, facts: []WorldRuntimeFact{root.fact}, effects: effects, cases: []WorldRuleCase{},
		nextFactSeq: factSequence + 1, nextEffectSeq: nextEffectSequence, nextCaseSeq: caseSequence,
	}, nil
}

func (s *CityEconomyService) performWorldActorActivity(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	payload worldActorActivityPayload,
) (worldRuntimeExecution, error) {
	actor, err := loadControlledWorldActor(ctx, tx, worldID, command.UserID, payload.ActorCode)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	definition, err := loadWorldRuntimeDefinition(ctx, tx, worldID, WorldRuntimeDefinitionActivity, payload.ActivityCode)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	activity, err := decodeWorldRuntimeDefinition[worldRuntimeActivityDefinition](definition)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	evaluation, err := evaluateWorldRequirement(ctx, tx, worldID, actor.id, targetTick-1, activity.Requirements)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	if !evaluation.Satisfied {
		return worldRuntimeExecution{}, worldRuntimeReject(worldRuntimeRejectionRequirement)
	}
	var performed int
	if err = tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM world_runtime_facts
WHERE world_id = $1 AND tick = $2 AND actor_id = $3
  AND fact_type = $4 AND definition_kind = 'activity' AND definition_code = $5`,
		worldID, targetTick, actor.id, WorldRuntimeFactActivityPerformed, definition.Code).Scan(&performed); err != nil {
		return worldRuntimeExecution{}, fmt.Errorf("count world activity executions: %w", err)
	}
	if performed >= activity.MaximumPerTick {
		return worldRuntimeExecution{}, worldRuntimeReject(worldRuntimeRejectionActivityLimit)
	}
	factPayload, err := json.Marshal(map[string]any{
		"schema_version": 1, "activity_code": definition.Code,
		"trigger_tags": activity.TriggerTags, "requirements": evaluation,
	})
	if err != nil {
		return worldRuntimeExecution{}, fmt.Errorf("marshal world activity fact: %w", err)
	}
	root, err := insertWorldRuntimeFact(ctx, tx, worldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		sourceCommandID: &command.ID, actorID: &actor.id,
		factType: WorldRuntimeFactActivityPerformed, definition: definition, payload: factPayload,
	})
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = enableWorldRuntimeFactWrite(ctx, tx, root.id); err != nil {
		return worldRuntimeExecution{}, err
	}
	effects, nextEffectSequence, err := applyWorldRuntimeEffects(
		ctx, tx, worldID, targetTick, effectSequence, actor, root, activity.Effects,
	)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	facts := []WorldRuntimeFact{root.fact}
	cases := make([]WorldRuleCase, 0)
	nextFactSequence := factSequence + 1
	nextCaseSequence := caseSequence
	ruleEffects := 0
	for _, trigger := range activity.TriggerTags {
		ruleExecution, ruleErr := applyTriggeredWorldRules(
			ctx, tx, worldID, targetTick, nextFactSequence, nextEffectSequence,
			nextCaseSequence, actor, root, trigger,
		)
		if ruleErr != nil {
			return worldRuntimeExecution{}, ruleErr
		}
		facts = append(facts, ruleExecution.facts...)
		effects = append(effects, ruleExecution.effects...)
		cases = append(cases, ruleExecution.cases...)
		ruleEffects += len(ruleExecution.effects)
		nextFactSequence = ruleExecution.nextFactSeq
		nextEffectSequence = ruleExecution.nextEffectSeq
		nextCaseSequence = ruleExecution.nextCaseSeq
		if len(cases) > worldRuntimeMaximumRuleCases || len(effects) > worldRuntimeMaximumEffects {
			return worldRuntimeExecution{}, worldRuntimeReject(worldRuntimeRejectionEffectLimit)
		}
	}
	if err = enableWorldRuntimeFactWrite(ctx, tx, root.id); err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = touchWorldActor(ctx, tx, worldID, actor.id, targetTick); err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = updateWorldRuntimeProfile(ctx, tx, worldID, 0, int64(len(facts)), int64(len(effects)), int64(len(cases))); err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = postWorldRuntimeFact(ctx, tx, root.id); err != nil {
		return worldRuntimeExecution{}, err
	}
	pending := cityPendingEvent{
		command: command, status: CityCommandStatusApplied, eventType: "world.actor.activity_performed",
		payload: map[string]any{
			"actor_code": actor.actor.Code, "activity_code": definition.Code,
			"effect_count": len(effects) - ruleEffects, "rule_case_count": len(cases),
		},
		result: map[string]any{
			"applied": true, "actor_code": actor.actor.Code, "activity_code": definition.Code,
			"effect_count": len(effects), "rule_case_count": len(cases),
		},
	}
	return worldRuntimeExecution{
		pending: pending, facts: facts, effects: effects, cases: cases,
		nextFactSeq: nextFactSequence, nextEffectSeq: nextEffectSequence, nextCaseSeq: nextCaseSequence,
	}, nil
}

func (s *CityEconomyService) transitionWorldActorRole(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	payload worldActorRoleTransitionPayload,
) (worldRuntimeExecution, error) {
	actor, err := loadControlledWorldActor(ctx, tx, worldID, command.UserID, payload.ActorCode)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	definition, err := loadWorldRuntimeDefinition(ctx, tx, worldID, WorldRuntimeDefinitionRole, payload.RoleCode)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	roleDefinition, err := decodeWorldRuntimeDefinition[worldRuntimeRoleDefinition](definition)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	evaluation, err := evaluateWorldRequirement(ctx, tx, worldID, actor.id, targetTick-1, roleDefinition.Requirements)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	if !evaluation.Satisfied {
		return worldRuntimeExecution{}, worldRuntimeReject(worldRuntimeRejectionRequirement)
	}
	var currentRole string
	var currentGrantedTick int64
	err = tx.QueryRowContext(ctx, `
SELECT role_code, granted_tick
FROM world_actor_roles
WHERE world_id = $1 AND actor_id = $2 AND category_code = $3 AND status = 'active'
FOR UPDATE`, worldID, actor.id, roleDefinition.CategoryCode).Scan(&currentRole, &currentGrantedTick)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return worldRuntimeExecution{}, fmt.Errorf("load current world actor role: %w", err)
	}
	if currentRole == definition.Code {
		return worldRuntimeExecution{}, worldRuntimeReject(worldRuntimeRejectionRoleActive)
	}
	if err == nil && targetTick-currentGrantedTick < roleDefinition.CooldownTicks {
		return worldRuntimeExecution{}, worldRuntimeReject(worldRuntimeRejectionRoleCooldown)
	}
	currentRoleRevokeEffects := make([]worldRuntimeEffectSpec, 0)
	if currentRole != "" {
		currentDefinition, loadErr := loadWorldRuntimeDefinition(
			ctx, tx, worldID, WorldRuntimeDefinitionRole, currentRole,
		)
		if loadErr != nil {
			return worldRuntimeExecution{}, loadErr
		}
		currentDefinitionPayload, decodeErr := decodeWorldRuntimeDefinition[worldRuntimeRoleDefinition](currentDefinition)
		if decodeErr != nil {
			return worldRuntimeExecution{}, decodeErr
		}
		if currentDefinitionPayload.CategoryCode != roleDefinition.CategoryCode {
			return worldRuntimeExecution{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{
				"field": "world_runtime_role_category",
			})
		}
		currentRoleRevokeEffects = append(currentRoleRevokeEffects, currentDefinitionPayload.OnRevokeEffects...)
	}
	factPayload, err := json.Marshal(map[string]any{
		"schema_version": 1, "category_code": roleDefinition.CategoryCode,
		"from_role_code": nullableWorldRuntimeString(currentRole), "to_role_code": definition.Code,
		"requirements": evaluation,
	})
	if err != nil {
		return worldRuntimeExecution{}, fmt.Errorf("marshal world role transition fact: %w", err)
	}
	root, err := insertWorldRuntimeFact(ctx, tx, worldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence,
		sourceCommandID: &command.ID, actorID: &actor.id,
		factType: WorldRuntimeFactRoleTransitioned, definition: definition, payload: factPayload,
	})
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = enableWorldRuntimeFactWrite(ctx, tx, root.id); err != nil {
		return worldRuntimeExecution{}, err
	}
	specs := make([]worldRuntimeEffectSpec, 0, 2+len(currentRoleRevokeEffects)+len(roleDefinition.OnGrantEffects))
	if currentRole != "" {
		specs = append(specs, worldRuntimeEffectSpec{Type: WorldRuntimeEffectRoleRevoke, Key: currentRole})
		specs = append(specs, currentRoleRevokeEffects...)
	}
	specs = append(specs, worldRuntimeEffectSpec{Type: WorldRuntimeEffectRoleGrant, Key: definition.Code})
	specs = append(specs, roleDefinition.OnGrantEffects...)
	if len(specs) > worldRuntimeMaximumEffects {
		return worldRuntimeExecution{}, worldRuntimeReject(worldRuntimeRejectionEffectLimit)
	}
	effects, nextEffectSequence, err := applyWorldRuntimeEffects(
		ctx, tx, worldID, targetTick, effectSequence, actor, root, specs,
	)
	if err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = touchWorldActor(ctx, tx, worldID, actor.id, targetTick); err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = updateWorldRuntimeProfile(ctx, tx, worldID, 0, 1, int64(len(effects)), 0); err != nil {
		return worldRuntimeExecution{}, err
	}
	if err = postWorldRuntimeFact(ctx, tx, root.id); err != nil {
		return worldRuntimeExecution{}, err
	}
	pending := cityPendingEvent{
		command: command, status: CityCommandStatusApplied, eventType: "world.actor.role_transitioned",
		payload: map[string]any{
			"actor_code": actor.actor.Code, "category_code": roleDefinition.CategoryCode,
			"from_role_code": nullableWorldRuntimeString(currentRole), "to_role_code": definition.Code,
		},
		result: map[string]any{"applied": true, "actor_code": actor.actor.Code, "role_code": definition.Code},
	}
	return worldRuntimeExecution{
		pending: pending, facts: []WorldRuntimeFact{root.fact}, effects: effects, cases: []WorldRuleCase{},
		nextFactSeq: factSequence + 1, nextEffectSeq: nextEffectSequence, nextCaseSeq: caseSequence,
	}, nil
}

type worldRuntimeFactInsert struct {
	worldID         int64
	tick            int64
	sequence        int64
	sourceCommandID *int64
	parentFactID    *int64
	actorID         *int64
	factType        string
	definition      *WorldRuntimeDefinition
	payload         json.RawMessage
}

func insertWorldRuntimeFact(
	ctx context.Context,
	tx *sql.Tx,
	input worldRuntimeFactInsert,
) (*worldRuntimeFactRecord, error) {
	var definitionKind, definitionCode, definitionVersion, definitionHash any
	if input.definition != nil {
		definitionKind, definitionCode = input.definition.Kind, input.definition.Code
		definitionVersion, definitionHash = input.definition.Version, input.definition.Hash
	}
	record := &worldRuntimeFactRecord{}
	if err := tx.QueryRowContext(ctx, `
INSERT INTO world_runtime_facts
    (world_id, tick, sequence, source_command_id, parent_fact_id, actor_id,
     fact_type, definition_kind, definition_code, definition_version, definition_hash, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb)
RETURNING id`, input.worldID, input.tick, input.sequence, cityNullableInt64(input.sourceCommandID),
		cityNullableInt64(input.parentFactID), cityNullableInt64(input.actorID), input.factType,
		definitionKind, definitionCode, definitionVersion, definitionHash, []byte(input.payload)).Scan(&record.id); err != nil {
		return nil, fmt.Errorf("insert world runtime fact %s: %w", input.factType, err)
	}
	record.fact = WorldRuntimeFact{
		Tick: input.tick, Sequence: input.sequence, FactType: input.factType, Payload: input.payload,
	}
	if input.sourceCommandID != nil {
		var commandSequence int64
		if err := tx.QueryRowContext(ctx, `SELECT sequence FROM city_commands WHERE id = $1`, *input.sourceCommandID).Scan(&commandSequence); err != nil {
			return nil, fmt.Errorf("load world runtime fact source command: %w", err)
		}
		record.fact.SourceCommandSequence = &commandSequence
	}
	if input.parentFactID != nil {
		var parent WorldRuntimeFactRef
		if err := tx.QueryRowContext(ctx, `SELECT tick, sequence FROM world_runtime_facts WHERE id = $1`, *input.parentFactID).Scan(
			&parent.Tick, &parent.Sequence,
		); err != nil {
			return nil, fmt.Errorf("load world runtime parent fact: %w", err)
		}
		record.fact.Parent = &parent
	}
	if input.actorID != nil {
		var code string
		if err := tx.QueryRowContext(ctx, `SELECT code FROM world_actors WHERE id = $1`, *input.actorID).Scan(&code); err != nil {
			return nil, fmt.Errorf("load world runtime fact actor: %w", err)
		}
		record.fact.ActorCode = &code
	}
	if input.definition != nil {
		record.fact.DefinitionKind = stringPointer(input.definition.Kind)
		record.fact.DefinitionCode = stringPointer(input.definition.Code)
		record.fact.DefinitionVersion = stringPointer(input.definition.Version)
		record.fact.DefinitionHash = stringPointer(input.definition.Hash)
	}
	return record, nil
}

func enableWorldRuntimeFactWrite(ctx context.Context, tx *sql.Tx, factID int64) error {
	if _, err := tx.ExecContext(ctx, `SELECT set_config('sub2api.world_runtime_fact_id', $1, TRUE)`, strconv.FormatInt(factID, 10)); err != nil {
		return fmt.Errorf("enable world runtime fact write: %w", err)
	}
	return nil
}

func postWorldRuntimeFact(ctx context.Context, tx *sql.Tx, factID int64) error {
	result, err := tx.ExecContext(ctx, `
UPDATE world_runtime_facts SET posted_at = NOW()
WHERE id = $1 AND posted_at IS NULL`, factID)
	if err != nil {
		return fmt.Errorf("post world runtime fact: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_runtime_fact_post"})
	}
	return nil
}

func updateWorldRuntimeProfile(
	ctx context.Context,
	tx *sql.Tx,
	worldID, actorDelta, factDelta, effectDelta, caseDelta int64,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE world_runtime_profiles
SET actor_count = actor_count + $2,
    fact_count = fact_count + $3,
    effect_count = effect_count + $4,
    case_count = case_count + $5,
    revision = revision + $3,
    updated_at = NOW()
WHERE world_id = $1`, worldID, actorDelta, factDelta, effectDelta, caseDelta)
	if err != nil {
		return fmt.Errorf("update world runtime profile: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_runtime_profile"})
	}
	return nil
}

func loadControlledWorldActor(
	ctx context.Context,
	tx *sql.Tx,
	worldID, userID int64,
	actorCode string,
) (*worldRuntimeActorRef, error) {
	item := &worldRuntimeActorRef{}
	var ownerID sql.NullInt64
	var archetypeCode, archetypeVersion sql.NullString
	err := tx.QueryRowContext(ctx, `
SELECT id, code, owner_user_id, actor_type_code, name, status,
       archetype_code, archetype_version, created_tick, updated_tick, version, metadata
FROM world_actors
WHERE world_id = $1 AND code = $2 AND owner_user_id = $3 AND status = 'active'
FOR UPDATE`, worldID, actorCode, userID).Scan(
		&item.id, &item.actor.Code, &ownerID, &item.actor.ActorTypeCode, &item.actor.Name,
		&item.actor.Status, &archetypeCode, &archetypeVersion, &item.actor.CreatedTick,
		&item.actor.UpdatedTick, &item.actor.Version, &item.actor.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, worldRuntimeReject(worldRuntimeRejectionActorNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("load controlled world actor: %w", err)
	}
	item.actor.OwnerUserID = nullInt64Pointer(ownerID)
	if archetypeCode.Valid {
		item.actor.ArchetypeCode = stringPointer(archetypeCode.String)
		item.actor.ArchetypeVersion = stringPointer(archetypeVersion.String)
	}
	return item, nil
}

func touchWorldActor(ctx context.Context, tx *sql.Tx, worldID, actorID, targetTick int64) error {
	if _, err := tx.ExecContext(ctx, `
UPDATE world_actors
SET updated_tick = $3, version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND id = $2`, worldID, actorID, targetTick); err != nil {
		return fmt.Errorf("touch world actor projection: %w", err)
	}
	return nil
}

type worldRuntimeAutomaticExecution struct {
	facts         []WorldRuntimeFact
	effects       []WorldEffectOperation
	nextFactSeq   int64
	nextEffectSeq int64
}

type expiringWorldRuntimeStatus struct {
	id             int64
	actorID        int64
	actorCode      string
	instanceCode   string
	statusCode     string
	intensityUnits int64
	stacks         int
	grantedTick    int64
	expiresTick    int64
	sourceFactID   int64
	sourceFactTick int64
	sourceFactSeq  int64
	version        int64
	metadata       json.RawMessage
}

func expireWorldRuntimeStatuses(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence int64,
) (worldRuntimeAutomaticExecution, error) {
	execution := worldRuntimeAutomaticExecution{
		facts: make([]WorldRuntimeFact, 0), effects: make([]WorldEffectOperation, 0),
		nextFactSeq: factSequence, nextEffectSeq: effectSequence,
	}
	rows, err := tx.QueryContext(ctx, `
SELECT status.id, status.actor_id, actor.code, status.instance_code, status.status_code,
       status.intensity_units, status.stacks, status.granted_tick, status.expires_tick,
       status.source_fact_id, source.tick, source.sequence, status.version, status.metadata
FROM world_actor_statuses status
JOIN world_actors actor
  ON actor.id = status.actor_id AND actor.world_id = status.world_id
JOIN world_runtime_facts source
  ON source.id = status.source_fact_id AND source.world_id = status.world_id
WHERE status.world_id = $1 AND status.lifecycle_status = 'active'
  AND status.expires_tick IS NOT NULL AND status.expires_tick <= $2
ORDER BY actor.code ASC, status.status_code ASC, status.instance_code ASC
FOR UPDATE OF status`, worldID, targetTick)
	if err != nil {
		return execution, fmt.Errorf("load expiring world actor statuses: %w", err)
	}
	items := make([]expiringWorldRuntimeStatus, 0)
	for rows.Next() {
		var item expiringWorldRuntimeStatus
		if err = rows.Scan(
			&item.id, &item.actorID, &item.actorCode, &item.instanceCode, &item.statusCode,
			&item.intensityUnits, &item.stacks, &item.grantedTick, &item.expiresTick,
			&item.sourceFactID, &item.sourceFactTick, &item.sourceFactSeq,
			&item.version, &item.metadata,
		); err != nil {
			_ = rows.Close()
			return execution, fmt.Errorf("scan expiring world actor status: %w", err)
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate expiring world actor statuses"); err != nil {
		return execution, err
	}
	for _, item := range items {
		definition, loadErr := loadWorldRuntimeDefinition(
			ctx, tx, worldID, WorldRuntimeDefinitionStatus, item.statusCode,
		)
		if loadErr != nil {
			return execution, loadErr
		}
		statusBefore := WorldActorStatus{
			ActorCode: item.actorCode, InstanceCode: item.instanceCode, StatusCode: item.statusCode,
			Lifecycle: "active", IntensityUnits: item.intensityUnits, Stacks: item.stacks,
			GrantedTick: item.grantedTick, ExpiresTick: int64Pointer(item.expiresTick),
			SourceFactTick: item.sourceFactTick, SourceFactSeq: item.sourceFactSeq,
			Version: item.version, Metadata: item.metadata,
		}
		factPayload, marshalErr := json.Marshal(map[string]any{
			"schema_version": 1, "status_before": statusBefore,
			"expiration_tick": targetTick, "reason": "duration_elapsed",
		})
		if marshalErr != nil {
			return execution, fmt.Errorf("marshal world status expiration fact: %w", marshalErr)
		}
		fact, insertErr := insertWorldRuntimeFact(ctx, tx, worldRuntimeFactInsert{
			worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq,
			parentFactID: &item.sourceFactID, actorID: &item.actorID,
			factType: WorldRuntimeFactStatusExpired, definition: definition, payload: factPayload,
		})
		if insertErr != nil {
			return execution, insertErr
		}
		execution.nextFactSeq++
		actor := &worldRuntimeActorRef{id: item.actorID, actor: WorldActor{Code: item.actorCode}}
		operations, nextEffectSequence, effectErr := applyWorldRuntimeEffects(
			ctx, tx, worldID, targetTick, execution.nextEffectSeq, actor, fact,
			[]worldRuntimeEffectSpec{{Type: WorldRuntimeEffectStatusExpire, Key: item.statusCode}},
		)
		if effectErr != nil {
			return execution, effectErr
		}
		if len(operations) != 1 {
			return execution, ErrCitySimulationInvariant.WithMetadata(map[string]string{
				"field": "world_runtime_status_expiration_effect",
			})
		}
		execution.nextEffectSeq = nextEffectSequence
		if err = touchWorldActor(ctx, tx, worldID, item.actorID, targetTick); err != nil {
			return execution, err
		}
		if err = updateWorldRuntimeProfile(ctx, tx, worldID, 0, 1, 1, 0); err != nil {
			return execution, err
		}
		if err = postWorldRuntimeFact(ctx, tx, fact.id); err != nil {
			return execution, err
		}
		execution.facts = append(execution.facts, fact.fact)
		execution.effects = append(execution.effects, operations...)
	}
	return execution, nil
}

func applyWorldRuntimeEffects(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, effectSequence int64,
	actor *worldRuntimeActorRef,
	fact *worldRuntimeFactRecord,
	specs []worldRuntimeEffectSpec,
) ([]WorldEffectOperation, int64, error) {
	if actor == nil || fact == nil || len(specs) > worldRuntimeMaximumEffects {
		return nil, effectSequence, worldRuntimeReject(worldRuntimeRejectionEffectLimit)
	}
	if !worldRuntimeEffectSpecsValid(specs, false) {
		return nil, effectSequence, worldRuntimeReject(worldRuntimeRejectionEffectInvalid)
	}
	if err := enableWorldRuntimeFactWrite(ctx, tx, fact.id); err != nil {
		return nil, effectSequence, err
	}
	operations := make([]WorldEffectOperation, 0, len(specs))
	for index, spec := range specs {
		operation, err := applyWorldRuntimeEffect(
			ctx, tx, worldID, targetTick, effectSequence, index+1, actor, fact, spec,
		)
		if err != nil {
			return nil, effectSequence, err
		}
		operations = append(operations, *operation)
		effectSequence++
	}
	return operations, effectSequence, nil
}

func applyWorldRuntimeEffect(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, effectSequence int64,
	operationIndex int,
	actor *worldRuntimeActorRef,
	fact *worldRuntimeFactRecord,
	spec worldRuntimeEffectSpec,
) (*WorldEffectOperation, error) {
	if !worldRuntimeCodeValid(spec.Key, 160) {
		return nil, worldRuntimeReject(worldRuntimeRejectionEffectInvalid)
	}
	operation := &WorldEffectOperation{
		Tick: targetTick, Sequence: effectSequence,
		SourceFact:     WorldRuntimeFactRef{Tick: fact.fact.Tick, Sequence: fact.fact.Sequence},
		OperationIndex: operationIndex, EffectType: spec.Type, ExecutorVersion: worldRuntimeExecutorVersion,
		TargetActorCode: stringPointer(actor.actor.Code), TargetKey: stringPointer(spec.Key),
	}
	var payload any
	switch spec.Type {
	case WorldRuntimeEffectAttributeSet, WorldRuntimeEffectAttributeAdd, WorldRuntimeEffectExperienceAdd:
		attribute, before, after, err := applyWorldRuntimeAttributeEffect(
			ctx, tx, worldID, targetTick, actor.id, actor.actor.Code, spec,
		)
		if err != nil {
			return nil, err
		}
		operation.BeforeUnits = &before
		delta := after - before
		operation.DeltaUnits = &delta
		operation.AfterUnits = &after
		payload = map[string]any{"schema_version": 1, "attribute_after": attribute}
	case WorldRuntimeEffectRoleGrant, WorldRuntimeEffectRoleRevoke:
		role, before, after, err := applyWorldRuntimeRoleEffect(
			ctx, tx, worldID, targetTick, actor.id, actor.actor.Code, spec,
		)
		if err != nil {
			return nil, err
		}
		operation.BeforeUnits = &before
		delta := after - before
		operation.DeltaUnits = &delta
		operation.AfterUnits = &after
		payload = map[string]any{"schema_version": 1, "role_after": role}
	case WorldRuntimeEffectStatusGrant, WorldRuntimeEffectStatusRevoke, WorldRuntimeEffectStatusExpire:
		status, before, after, err := applyWorldRuntimeStatusEffect(
			ctx, tx, worldID, targetTick, effectSequence, actor.id, actor.actor.Code,
			fact.id, fact.fact.Sequence, spec,
		)
		if err != nil {
			return nil, err
		}
		operation.BeforeUnits = &before
		delta := after - before
		operation.DeltaUnits = &delta
		operation.AfterUnits = &after
		payload = map[string]any{"schema_version": 1, "status_after": status}
	default:
		return nil, worldRuntimeReject(worldRuntimeRejectionEffectInvalid)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal world runtime effect payload: %w", err)
	}
	operation.Payload = raw
	if _, err = tx.ExecContext(ctx, `
INSERT INTO world_effect_operations
    (world_id, tick, sequence, source_fact_id, operation_index, effect_type,
     executor_version, target_actor_id, target_key, before_units, delta_units,
     after_units, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb)`,
		worldID, targetTick, effectSequence, fact.id, operationIndex, operation.EffectType,
		operation.ExecutorVersion, actor.id, spec.Key, cityNullableInt64(operation.BeforeUnits),
		cityNullableInt64(operation.DeltaUnits), cityNullableInt64(operation.AfterUnits), []byte(raw)); err != nil {
		return nil, fmt.Errorf("insert world effect operation %s: %w", spec.Type, err)
	}
	return operation, nil
}

func applyWorldRuntimeAttributeEffect(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, actorID int64,
	actorCode string,
	spec worldRuntimeEffectSpec,
) (WorldActorAttribute, int64, int64, error) {
	definition, err := loadWorldRuntimeDefinition(ctx, tx, worldID, WorldRuntimeDefinitionAttribute, spec.Key)
	if err != nil {
		return WorldActorAttribute{}, 0, 0, err
	}
	attributeDefinition, err := decodeWorldRuntimeDefinition[worldRuntimeAttributeDefinition](definition)
	if err != nil {
		return WorldActorAttribute{}, 0, 0, err
	}
	var value, experience, version int64
	err = tx.QueryRowContext(ctx, `
SELECT value_units, experience_units, version
FROM world_actor_attributes
WHERE world_id = $1 AND actor_id = $2 AND attribute_code = $3
FOR UPDATE`, worldID, actorID, spec.Key).Scan(&value, &experience, &version)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return WorldActorAttribute{}, 0, 0, fmt.Errorf("load world actor attribute: %w", err)
	}
	if !exists {
		value = attributeDefinition.DefaultUnits
		if spec.Type == WorldRuntimeEffectAttributeSet {
			value = 0
		}
		version = 0
	}
	before := value
	after := value
	switch spec.Type {
	case WorldRuntimeEffectAttributeSet:
		after = spec.ValueUnits
	case WorldRuntimeEffectAttributeAdd:
		after = saturatingWorldRuntimeAdd(value, spec.ValueUnits)
	case WorldRuntimeEffectExperienceAdd:
		before = experience
		after = saturatingWorldRuntimeAdd(experience, spec.ValueUnits)
		if after < 0 {
			after = 0
		}
		experience = after
	default:
		return WorldActorAttribute{}, 0, 0, worldRuntimeReject(worldRuntimeRejectionEffectInvalid)
	}
	if spec.Type != WorldRuntimeEffectExperienceAdd {
		if after < attributeDefinition.MinimumUnits {
			after = attributeDefinition.MinimumUnits
		}
		if after > attributeDefinition.MaximumUnits {
			after = attributeDefinition.MaximumUnits
		}
		value = after
	}
	metadata := json.RawMessage(`{"schema_version":1}`)
	if exists {
		version++
		if _, err = tx.ExecContext(ctx, `
UPDATE world_actor_attributes
SET value_units = $4, experience_units = $5, last_changed_tick = $6,
    version = $7, metadata = $8::jsonb, updated_at = NOW()
WHERE world_id = $1 AND actor_id = $2 AND attribute_code = $3`, worldID, actorID,
			spec.Key, value, experience, targetTick, version, []byte(metadata)); err != nil {
			return WorldActorAttribute{}, 0, 0, fmt.Errorf("update world actor attribute: %w", err)
		}
	} else {
		version = 1
		if _, err = tx.ExecContext(ctx, `
INSERT INTO world_actor_attributes
    (world_id, actor_id, attribute_code, value_units, experience_units,
     last_changed_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, 1, $7::jsonb)`, worldID, actorID, spec.Key,
			value, experience, targetTick, []byte(metadata)); err != nil {
			return WorldActorAttribute{}, 0, 0, fmt.Errorf("insert world actor attribute: %w", err)
		}
	}
	return WorldActorAttribute{
		ActorCode: actorCode, AttributeCode: spec.Key, ValueUnits: value,
		ExperienceUnits: experience, LastChangedTick: targetTick, Version: version, Metadata: metadata,
	}, before, after, nil
}

func applyWorldRuntimeRoleEffect(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, actorID int64,
	actorCode string,
	spec worldRuntimeEffectSpec,
) (WorldActorRole, int64, int64, error) {
	definition, err := loadWorldRuntimeDefinition(ctx, tx, worldID, WorldRuntimeDefinitionRole, spec.Key)
	if err != nil {
		return WorldActorRole{}, 0, 0, err
	}
	roleDefinition, err := decodeWorldRuntimeDefinition[worldRuntimeRoleDefinition](definition)
	if err != nil {
		return WorldActorRole{}, 0, 0, err
	}
	metadata := json.RawMessage(`{"schema_version":1}`)
	if spec.Type == WorldRuntimeEffectRoleGrant {
		var existing int
		if err = tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM world_actor_roles
WHERE world_id = $1 AND actor_id = $2 AND category_code = $3 AND status = 'active'`,
			worldID, actorID, roleDefinition.CategoryCode).Scan(&existing); err != nil {
			return WorldActorRole{}, 0, 0, fmt.Errorf("inspect active world role category: %w", err)
		}
		if existing != 0 {
			return WorldActorRole{}, 0, 0, worldRuntimeReject(worldRuntimeRejectionRoleActive)
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO world_actor_roles
    (world_id, actor_id, role_code, category_code, status, granted_tick, version, metadata)
VALUES ($1, $2, $3, $4, 'active', $5, 1, $6::jsonb)`, worldID, actorID,
			spec.Key, roleDefinition.CategoryCode, targetTick, []byte(metadata)); err != nil {
			return WorldActorRole{}, 0, 0, fmt.Errorf("grant world actor role: %w", err)
		}
		return WorldActorRole{
			ActorCode: actorCode, RoleCode: spec.Key, CategoryCode: roleDefinition.CategoryCode,
			Status: "active", GrantedTick: targetTick, Version: 1, Metadata: metadata,
		}, 0, 1, nil
	}
	if spec.Type != WorldRuntimeEffectRoleRevoke {
		return WorldActorRole{}, 0, 0, worldRuntimeReject(worldRuntimeRejectionEffectInvalid)
	}
	var grantedTick, version int64
	err = tx.QueryRowContext(ctx, `
SELECT granted_tick, version FROM world_actor_roles
WHERE world_id = $1 AND actor_id = $2 AND role_code = $3 AND status = 'active'
FOR UPDATE`, worldID, actorID, spec.Key).Scan(&grantedTick, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return WorldActorRole{}, 0, 0, worldRuntimeReject(worldRuntimeRejectionEffectInvalid)
	}
	if err != nil {
		return WorldActorRole{}, 0, 0, fmt.Errorf("load active world actor role: %w", err)
	}
	version++
	if _, err = tx.ExecContext(ctx, `
UPDATE world_actor_roles
SET status = 'revoked', revoked_tick = $4, version = $5, updated_at = NOW()
WHERE world_id = $1 AND actor_id = $2 AND role_code = $3 AND status = 'active'`,
		worldID, actorID, spec.Key, targetTick, version); err != nil {
		return WorldActorRole{}, 0, 0, fmt.Errorf("revoke world actor role: %w", err)
	}
	return WorldActorRole{
		ActorCode: actorCode, RoleCode: spec.Key, CategoryCode: roleDefinition.CategoryCode,
		Status: "revoked", GrantedTick: grantedTick, RevokedTick: int64Pointer(targetTick),
		Version: version, Metadata: metadata,
	}, 1, 0, nil
}

func applyWorldRuntimeStatusEffect(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, effectSequence, actorID int64,
	actorCode string,
	factID, factSequence int64,
	spec worldRuntimeEffectSpec,
) (WorldActorStatus, int64, int64, error) {
	definition, err := loadWorldRuntimeDefinition(ctx, tx, worldID, WorldRuntimeDefinitionStatus, spec.Key)
	if err != nil {
		return WorldActorStatus{}, 0, 0, err
	}
	statusDefinition, err := decodeWorldRuntimeDefinition[worldRuntimeStatusDefinition](definition)
	if err != nil {
		return WorldActorStatus{}, 0, 0, err
	}
	var id, intensity int64
	var stacks int
	var grantedTick, version int64
	var instanceCode string
	var expiresTick sql.NullInt64
	err = tx.QueryRowContext(ctx, `
SELECT id, instance_code, intensity_units, stacks, granted_tick, expires_tick, version
FROM world_actor_statuses
WHERE world_id = $1 AND actor_id = $2 AND status_code = $3 AND lifecycle_status = 'active'
FOR UPDATE`, worldID, actorID, spec.Key).Scan(
		&id, &instanceCode, &intensity, &stacks, &grantedTick, &expiresTick, &version,
	)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return WorldActorStatus{}, 0, 0, fmt.Errorf("load active world actor status: %w", err)
	}
	metadata := json.RawMessage(`{"schema_version":1}`)
	if spec.Type == WorldRuntimeEffectStatusGrant {
		before := int64(stacks)
		if !exists {
			stacks = 0
			grantedTick = targetTick
			version = 0
			instanceCode = fmt.Sprintf("status.%s.%d.%d", actorCode, targetTick, effectSequence)
		}
		addStacks := spec.Stacks
		if addStacks <= 0 {
			addStacks = 1
		}
		stacks += addStacks
		if stacks > statusDefinition.MaximumStacks {
			stacks = statusDefinition.MaximumStacks
		}
		if spec.IntensityUnits > intensity {
			intensity = spec.IntensityUnits
		}
		var nextExpires *int64
		if expiresTick.Valid {
			nextExpires = int64Pointer(expiresTick.Int64)
		}
		if spec.DurationTicks > 0 {
			candidate := targetTick + spec.DurationTicks
			if nextExpires == nil || candidate > *nextExpires {
				nextExpires = int64Pointer(candidate)
			}
		}
		version++
		if exists {
			if _, err = tx.ExecContext(ctx, `
UPDATE world_actor_statuses
SET intensity_units = $4, stacks = $5, expires_tick = $6,
    version = $7, source_fact_id = $8, updated_at = NOW()
WHERE world_id = $1 AND actor_id = $2 AND id = $3`, worldID, actorID, id,
				intensity, stacks, cityNullableInt64(nextExpires), version, factID); err != nil {
				return WorldActorStatus{}, 0, 0, fmt.Errorf("stack world actor status: %w", err)
			}
		} else {
			if _, err = tx.ExecContext(ctx, `
INSERT INTO world_actor_statuses
    (world_id, actor_id, instance_code, status_code, lifecycle_status,
     intensity_units, stacks, granted_tick, expires_tick, source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, 'active', $5, $6, $7, $8, $9, 1, $10::jsonb)`,
				worldID, actorID, instanceCode, spec.Key, intensity, stacks, targetTick,
				cityNullableInt64(nextExpires), factID, []byte(metadata)); err != nil {
				return WorldActorStatus{}, 0, 0, fmt.Errorf("grant world actor status: %w", err)
			}
		}
		return WorldActorStatus{
			ActorCode: actorCode, InstanceCode: instanceCode, StatusCode: spec.Key,
			Lifecycle: "active", IntensityUnits: intensity, Stacks: stacks,
			GrantedTick: grantedTick, ExpiresTick: nextExpires,
			SourceFactTick: targetTick, SourceFactSeq: factSequence, Version: version, Metadata: metadata,
		}, before, int64(stacks), nil
	}
	if (spec.Type != WorldRuntimeEffectStatusRevoke && spec.Type != WorldRuntimeEffectStatusExpire) || !exists {
		return WorldActorStatus{}, 0, 0, worldRuntimeReject(worldRuntimeRejectionEffectInvalid)
	}
	before := int64(stacks)
	version++
	lifecycle := "revoked"
	if spec.Type == WorldRuntimeEffectStatusExpire {
		lifecycle = "expired"
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE world_actor_statuses
SET lifecycle_status = $4, ended_tick = $5, source_fact_id = $6,
    version = $7, updated_at = NOW()
WHERE world_id = $1 AND actor_id = $2 AND id = $3`,
		worldID, actorID, id, lifecycle, targetTick, factID, version); err != nil {
		return WorldActorStatus{}, 0, 0, fmt.Errorf("end world actor status: %w", err)
	}
	return WorldActorStatus{
		ActorCode: actorCode, InstanceCode: instanceCode, StatusCode: spec.Key,
		Lifecycle: lifecycle, IntensityUnits: intensity, Stacks: stacks,
		GrantedTick: grantedTick, ExpiresTick: nullInt64Pointer(expiresTick), EndedTick: int64Pointer(targetTick),
		SourceFactTick: targetTick, SourceFactSeq: factSequence, Version: version, Metadata: metadata,
	}, before, 0, nil
}

type worldTriggeredRuleExecution struct {
	facts         []WorldRuntimeFact
	effects       []WorldEffectOperation
	cases         []WorldRuleCase
	nextFactSeq   int64
	nextEffectSeq int64
	nextCaseSeq   int64
}

func applyTriggeredWorldRules(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	actor *worldRuntimeActorRef,
	sourceFact *worldRuntimeFactRecord,
	trigger string,
) (worldTriggeredRuleExecution, error) {
	execution := worldTriggeredRuleExecution{
		facts: make([]WorldRuntimeFact, 0), effects: make([]WorldEffectOperation, 0), cases: make([]WorldRuleCase, 0),
		nextFactSeq: factSequence, nextEffectSeq: effectSequence, nextCaseSeq: caseSequence,
	}
	rows, err := tx.QueryContext(ctx, `
SELECT definition_kind, code, definition_version, content_hash, visibility, payload
FROM world_runtime_definitions
WHERE world_id = $1 AND definition_kind = 'rule'
  AND payload->'triggers' ? $2
ORDER BY code ASC`, worldID, trigger)
	if err != nil {
		return execution, fmt.Errorf("load triggered world rules: %w", err)
	}
	definitions := make([]WorldRuntimeDefinition, 0)
	for rows.Next() {
		var definition WorldRuntimeDefinition
		if err = rows.Scan(&definition.Kind, &definition.Code, &definition.Version,
			&definition.Hash, &definition.Visibility, &definition.Payload); err != nil {
			_ = rows.Close()
			return execution, fmt.Errorf("scan triggered world rule: %w", err)
		}
		definitions = append(definitions, definition)
	}
	if err = closeCityRows(rows, "iterate triggered world rules"); err != nil {
		return execution, err
	}
	for _, definition := range definitions {
		rule, decodeErr := decodeWorldRuntimeDefinition[worldRuntimeRuleDefinition](&definition)
		if decodeErr != nil {
			return execution, decodeErr
		}
		evaluation, evaluateErr := evaluateWorldRequirement(
			ctx, tx, worldID, actor.id, targetTick, rule.Requirements,
		)
		if evaluateErr != nil {
			return execution, evaluateErr
		}
		if !evaluation.Satisfied {
			continue
		}
		fromTick := targetTick - rule.OccurrenceWindowTicks + 1
		if fromTick < 1 {
			fromTick = 1
		}
		var previousOccurrences int
		if err = tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM world_rule_cases
WHERE world_id = $1 AND subject_actor_id = $2 AND rule_code = $3
  AND tick BETWEEN $4 AND $5 AND status IN ('decided', 'closed')`,
			worldID, actor.id, definition.Code, fromTick, targetTick).Scan(&previousOccurrences); err != nil {
			return execution, fmt.Errorf("count world rule occurrences: %w", err)
		}
		occurrences := previousOccurrences + 1
		tier := rule.Tiers[0]
		for _, candidate := range rule.Tiers {
			if candidate.MinimumOccurrences <= occurrences {
				tier = candidate
			}
		}
		consequencePayload, marshalErr := json.Marshal(map[string]any{
			"schema_version": 1, "rule_code": definition.Code,
			"occurrences_in_window": occurrences, "severity_units": tier.SeverityUnits,
		})
		if marshalErr != nil {
			return execution, fmt.Errorf("marshal world rule consequence: %w", marshalErr)
		}
		consequence, insertErr := insertWorldRuntimeFact(ctx, tx, worldRuntimeFactInsert{
			worldID: worldID, tick: targetTick, sequence: execution.nextFactSeq,
			parentFactID: &sourceFact.id, actorID: &actor.id,
			factType: WorldRuntimeFactRuleConsequent, definition: &definition, payload: consequencePayload,
		})
		if insertErr != nil {
			return execution, insertErr
		}
		execution.nextFactSeq++
		if err = enableWorldRuntimeFactWrite(ctx, tx, consequence.id); err != nil {
			return execution, err
		}
		consequenceEffects, nextEffect, effectErr := applyWorldRuntimeEffects(
			ctx, tx, worldID, targetTick, execution.nextEffectSeq, actor, consequence, tier.Effects,
		)
		if effectErr != nil {
			return execution, effectErr
		}
		execution.nextEffectSeq = nextEffect
		decisionCode := "violation"
		decidedTick := targetTick
		caseCode := fmt.Sprintf("case.%d.%d", sourceFact.fact.SourceCommandSequenceValue(), execution.nextCaseSeq)
		casePayload, marshalErr := json.Marshal(map[string]any{
			"schema_version": 1, "trigger": trigger, "requirements": evaluation,
			"occurrences_in_window": occurrences,
		})
		if marshalErr != nil {
			return execution, fmt.Errorf("marshal world rule case: %w", marshalErr)
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO world_rule_cases
    (world_id, code, tick, sequence, source_fact_id, consequence_fact_id,
     subject_actor_id, rule_code, rule_version, rule_hash, category_code,
     scope_kind, scope_code, status, severity_units, decision_code,
     created_tick, decided_tick, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        'decided', $14, $15, $3, $3, $16::jsonb)`, worldID, caseCode, targetTick,
			execution.nextCaseSeq, sourceFact.id, consequence.id, actor.id,
			definition.Code, definition.Version, definition.Hash, rule.CategoryCode,
			rule.ScopeKind, rule.ScopeCode, tier.SeverityUnits, decisionCode, []byte(casePayload)); err != nil {
			return execution, fmt.Errorf("insert world rule case: %w", err)
		}
		worldCase := WorldRuleCase{
			Code: caseCode, Tick: targetTick, Sequence: execution.nextCaseSeq,
			SourceFact: sourceFact.fact.Ref(), ConsequenceFact: worldRuntimeFactRefPointer(consequence.fact.Ref()),
			SubjectActorCode: actor.actor.Code, RuleCode: definition.Code,
			RuleVersion: definition.Version, RuleHash: definition.Hash, CategoryCode: rule.CategoryCode,
			ScopeKind: rule.ScopeKind, ScopeCode: rule.ScopeCode, Status: "decided",
			SeverityUnits: tier.SeverityUnits, DecisionCode: &decisionCode,
			CreatedTick: targetTick, DecidedTick: &decidedTick, Payload: casePayload,
		}
		execution.nextCaseSeq++
		if err = postWorldRuntimeFact(ctx, tx, consequence.id); err != nil {
			return execution, err
		}
		execution.facts = append(execution.facts, consequence.fact)
		execution.effects = append(execution.effects, consequenceEffects...)
		execution.cases = append(execution.cases, worldCase)
	}
	return execution, nil
}

func (fact WorldRuntimeFact) Ref() WorldRuntimeFactRef {
	return WorldRuntimeFactRef{Tick: fact.Tick, Sequence: fact.Sequence}
}

func (fact WorldRuntimeFact) SourceCommandSequenceValue() int64 {
	if fact.SourceCommandSequence == nil {
		return fact.Sequence
	}
	return *fact.SourceCommandSequence
}

func worldRuntimeFactRefPointer(value WorldRuntimeFactRef) *WorldRuntimeFactRef { return &value }

func nullableWorldRuntimeString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func saturatingWorldRuntimeAdd(left, right int64) int64 {
	if right > 0 && left > math.MaxInt64-right {
		return math.MaxInt64
	}
	if right < 0 && left < math.MinInt64-right {
		return math.MinInt64
	}
	return left + right
}
