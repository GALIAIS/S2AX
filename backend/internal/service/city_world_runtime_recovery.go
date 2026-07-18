package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

type worldRuntimeRecoveryIdentity struct {
	tick     int64
	sequence int64
}

type worldRuntimeRecoveryIDs struct {
	facts   map[worldRuntimeRecoveryIdentity]int64
	effects map[worldRuntimeRecoveryIdentity]int64
	cases   map[worldRuntimeRecoveryIdentity]int64
}

func replayWorldRuntimeFacts(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
	state *cityHashState,
) error {
	if state == nil || state.WorldRuntime == nil || !cityEngineSupportsWorldRuntime(state.SimulationVersion) {
		return fmt.Errorf("world runtime replay state is unavailable")
	}
	facts, err := loadWorldRuntimeFactsForTick(ctx, queryer, worldID, tick)
	if err != nil {
		return err
	}
	effects, err := loadWorldEffectOperations(ctx, queryer, worldID, tick)
	if err != nil {
		return err
	}
	cases, err := loadWorldRuleCasesForTick(ctx, queryer, worldID, tick)
	if err != nil {
		return err
	}
	runtime := state.WorldRuntime
	for _, fact := range facts {
		if fact.DefinitionCode != nil {
			definition := findWorldRuntimeDefinition(runtime.Definitions, *fact.DefinitionKind, *fact.DefinitionCode)
			if definition == nil || definition.Version != *fact.DefinitionVersion || definition.Hash != *fact.DefinitionHash {
				return fmt.Errorf("world runtime fact %d/%d definition proof mismatch", fact.Tick, fact.Sequence)
			}
		}
		if fact.FactType == WorldRuntimeFactActorCreated {
			var payload struct {
				SchemaVersion int        `json:"schema_version"`
				Actor         WorldActor `json:"actor"`
				ArchetypeCode string     `json:"archetype_code"`
			}
			if err = json.Unmarshal(fact.Payload, &payload); err != nil || payload.SchemaVersion != 1 {
				return fmt.Errorf("decode replay actor creation fact: %w", err)
			}
			if findWorldRuntimeActor(runtime.Actors, payload.Actor.Code) >= 0 {
				return fmt.Errorf("replay actor code %s already exists", payload.Actor.Code)
			}
			runtime.Actors = append(runtime.Actors, payload.Actor)
		}
		runtime.Facts = append(runtime.Facts, fact)
	}
	for _, effect := range effects {
		if err = replayWorldRuntimeEffect(runtime, effect); err != nil {
			return fmt.Errorf("replay world effect %d/%d: %w", effect.Tick, effect.Sequence, err)
		}
		runtime.Effects = append(runtime.Effects, effect)
	}
	for _, fact := range facts {
		if fact.ActorCode == nil || (fact.FactType != WorldRuntimeFactActivityPerformed &&
			fact.FactType != WorldRuntimeFactRoleTransitioned && fact.FactType != WorldRuntimeFactStatusExpired) {
			continue
		}
		index := findWorldRuntimeActor(runtime.Actors, *fact.ActorCode)
		if index < 0 {
			return fmt.Errorf("replay world fact actor %s does not exist", *fact.ActorCode)
		}
		runtime.Actors[index].UpdatedTick = tick
		runtime.Actors[index].Version++
	}
	runtime.RuleCases = append(runtime.RuleCases, cases...)
	runtime.Profile.ActorCount = int64(len(runtime.Actors))
	runtime.Profile.FactCount = int64(len(runtime.Facts))
	runtime.Profile.EffectCount = int64(len(runtime.Effects))
	runtime.Profile.CaseCount = int64(len(runtime.RuleCases))
	runtime.Profile.Revision = runtime.Profile.FactCount + 1
	return nil
}

func replayWorldRuntimeEffect(runtime *worldRuntimeHashState, effect WorldEffectOperation) error {
	if runtime == nil || effect.TargetActorCode == nil || effect.TargetKey == nil ||
		effect.BeforeUnits == nil || effect.DeltaUnits == nil || effect.AfterUnits == nil ||
		*effect.BeforeUnits+*effect.DeltaUnits != *effect.AfterUnits {
		return fmt.Errorf("effect envelope is incomplete")
	}
	if findWorldRuntimeActor(runtime.Actors, *effect.TargetActorCode) < 0 {
		return fmt.Errorf("effect target actor does not exist")
	}
	var payload struct {
		SchemaVersion  int                  `json:"schema_version"`
		AttributeAfter *WorldActorAttribute `json:"attribute_after,omitempty"`
		RoleAfter      *WorldActorRole      `json:"role_after,omitempty"`
		StatusAfter    *WorldActorStatus    `json:"status_after,omitempty"`
	}
	if err := json.Unmarshal(effect.Payload, &payload); err != nil || payload.SchemaVersion != 1 {
		return fmt.Errorf("invalid effect payload")
	}
	switch effect.EffectType {
	case WorldRuntimeEffectAttributeSet, WorldRuntimeEffectAttributeAdd, WorldRuntimeEffectExperienceAdd:
		if payload.AttributeAfter == nil || payload.AttributeAfter.ActorCode != *effect.TargetActorCode ||
			payload.AttributeAfter.AttributeCode != *effect.TargetKey {
			return fmt.Errorf("attribute effect payload mismatch")
		}
		index := findWorldRuntimeAttribute(runtime.Attributes, *effect.TargetActorCode, *effect.TargetKey)
		before := int64(0)
		if index >= 0 {
			if effect.EffectType == WorldRuntimeEffectExperienceAdd {
				before = runtime.Attributes[index].ExperienceUnits
			} else {
				before = runtime.Attributes[index].ValueUnits
			}
		} else if effect.EffectType == WorldRuntimeEffectAttributeAdd {
			definition := findWorldRuntimeDefinition(
				runtime.Definitions, WorldRuntimeDefinitionAttribute, *effect.TargetKey,
			)
			if definition == nil {
				return fmt.Errorf("attribute effect definition is unavailable")
			}
			attributeDefinition, err := decodeWorldRuntimeDefinition[worldRuntimeAttributeDefinition](definition)
			if err != nil {
				return fmt.Errorf("decode attribute effect definition: %w", err)
			}
			before = attributeDefinition.DefaultUnits
		}
		if before != *effect.BeforeUnits {
			return fmt.Errorf("attribute effect before value mismatch")
		}
		payloadAfter := payload.AttributeAfter.ValueUnits
		if effect.EffectType == WorldRuntimeEffectExperienceAdd {
			payloadAfter = payload.AttributeAfter.ExperienceUnits
		}
		if payloadAfter != *effect.AfterUnits {
			return fmt.Errorf("attribute effect after value mismatch")
		}
		if index < 0 {
			runtime.Attributes = append(runtime.Attributes, *payload.AttributeAfter)
		} else {
			runtime.Attributes[index] = *payload.AttributeAfter
		}
	case WorldRuntimeEffectRoleGrant, WorldRuntimeEffectRoleRevoke:
		if payload.RoleAfter == nil || payload.RoleAfter.ActorCode != *effect.TargetActorCode ||
			payload.RoleAfter.RoleCode != *effect.TargetKey {
			return fmt.Errorf("role effect payload mismatch")
		}
		index := findActiveWorldRuntimeRole(runtime.Roles, *effect.TargetActorCode, *effect.TargetKey)
		if effect.EffectType == WorldRuntimeEffectRoleGrant {
			if *effect.BeforeUnits != 0 || *effect.AfterUnits != 1 || index >= 0 || payload.RoleAfter.Status != "active" {
				return fmt.Errorf("role grant before state mismatch")
			}
			runtime.Roles = append(runtime.Roles, *payload.RoleAfter)
		} else {
			if *effect.BeforeUnits != 1 || *effect.AfterUnits != 0 || index < 0 || payload.RoleAfter.Status != "revoked" {
				return fmt.Errorf("role revoke before state mismatch")
			}
			runtime.Roles[index] = *payload.RoleAfter
		}
	case WorldRuntimeEffectStatusGrant, WorldRuntimeEffectStatusRevoke, WorldRuntimeEffectStatusExpire:
		if payload.StatusAfter == nil || payload.StatusAfter.ActorCode != *effect.TargetActorCode ||
			payload.StatusAfter.StatusCode != *effect.TargetKey {
			return fmt.Errorf("status effect payload mismatch")
		}
		index := findWorldRuntimeStatus(runtime.Statuses, payload.StatusAfter.InstanceCode)
		before := int64(0)
		if index >= 0 && runtime.Statuses[index].Lifecycle == "active" {
			before = int64(runtime.Statuses[index].Stacks)
		}
		if before != *effect.BeforeUnits {
			return fmt.Errorf("status effect before state mismatch")
		}
		if effect.EffectType == WorldRuntimeEffectStatusGrant {
			if payload.StatusAfter.Lifecycle != "active" || int64(payload.StatusAfter.Stacks) != *effect.AfterUnits {
				return fmt.Errorf("status grant after state mismatch")
			}
		} else if *effect.AfterUnits != 0 || payload.StatusAfter.Lifecycle == "active" {
			return fmt.Errorf("status end after state mismatch")
		}
		if index < 0 {
			runtime.Statuses = append(runtime.Statuses, *payload.StatusAfter)
		} else {
			runtime.Statuses[index] = *payload.StatusAfter
		}
	default:
		return fmt.Errorf("unknown effect type %s", effect.EffectType)
	}
	return nil
}

func findWorldRuntimeDefinition(items []WorldRuntimeDefinition, kind, code string) *WorldRuntimeDefinition {
	for index := range items {
		if items[index].Kind == kind && items[index].Code == code {
			return &items[index]
		}
	}
	return nil
}

func findWorldRuntimeActor(items []WorldActor, code string) int {
	for index := range items {
		if items[index].Code == code {
			return index
		}
	}
	return -1
}

func findWorldRuntimeAttribute(items []WorldActorAttribute, actorCode, attributeCode string) int {
	for index := range items {
		if items[index].ActorCode == actorCode && items[index].AttributeCode == attributeCode {
			return index
		}
	}
	return -1
}

func findActiveWorldRuntimeRole(items []WorldActorRole, actorCode, roleCode string) int {
	for index := range items {
		if items[index].ActorCode == actorCode && items[index].RoleCode == roleCode && items[index].Status == "active" {
			return index
		}
	}
	return -1
}

func findWorldRuntimeStatus(items []WorldActorStatus, instanceCode string) int {
	for index := range items {
		if items[index].InstanceCode == instanceCode {
			return index
		}
	}
	return -1
}

func loadWorldRuntimeRecoveryIDs(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (worldRuntimeRecoveryIDs, error) {
	ids := worldRuntimeRecoveryIDs{
		facts:   make(map[worldRuntimeRecoveryIdentity]int64),
		effects: make(map[worldRuntimeRecoveryIdentity]int64),
		cases:   make(map[worldRuntimeRecoveryIdentity]int64),
	}
	load := func(query string, target map[worldRuntimeRecoveryIdentity]int64) error {
		rows, err := queryer.QueryContext(ctx, query, worldID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, tick, sequence int64
			if err = rows.Scan(&id, &tick, &sequence); err != nil {
				_ = rows.Close()
				return err
			}
			target[worldRuntimeRecoveryIdentity{tick: tick, sequence: sequence}] = id
		}
		return closeCityRows(rows, "iterate world runtime recovery identities")
	}
	if err := load(`SELECT id, tick, sequence FROM world_runtime_facts WHERE world_id = $1 ORDER BY tick, sequence`, ids.facts); err != nil {
		return ids, fmt.Errorf("load world runtime fact identities: %w", err)
	}
	if err := load(`SELECT id, tick, sequence FROM world_effect_operations WHERE world_id = $1 ORDER BY tick, sequence`, ids.effects); err != nil {
		return ids, fmt.Errorf("load world runtime effect identities: %w", err)
	}
	if err := load(`SELECT id, tick, sequence FROM world_rule_cases WHERE world_id = $1 ORDER BY tick, sequence`, ids.cases); err != nil {
		return ids, fmt.Errorf("load world runtime case identities: %w", err)
	}
	return ids, nil
}

func clearWorldRuntimeProjection(ctx context.Context, tx *sql.Tx, worldID int64) (int, error) {
	count := 0
	for _, statement := range []string{
		`DELETE FROM world_rule_cases WHERE world_id = $1`,
		`DELETE FROM world_effect_operations WHERE world_id = $1`,
		`DELETE FROM world_actor_statuses WHERE world_id = $1`,
		`DELETE FROM world_actor_roles WHERE world_id = $1`,
		`DELETE FROM world_actor_attributes WHERE world_id = $1`,
		`DELETE FROM world_runtime_facts WHERE world_id = $1`,
		`DELETE FROM world_actors WHERE world_id = $1`,
		`DELETE FROM world_runtime_definitions WHERE world_id = $1`,
		`DELETE FROM world_runtime_profiles WHERE world_id = $1`,
	} {
		result, err := tx.ExecContext(ctx, statement, worldID)
		if err != nil {
			return count, fmt.Errorf("clear world runtime projection: %w", err)
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return count, rowsErr
		}
		count += int(rows)
	}
	return count, nil
}

func restoreWorldRuntimeProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	state *cityHashState,
	preserved worldRuntimeRecoveryIDs,
) (int, error) {
	if state == nil || state.WorldRuntime == nil || !cityEngineSupportsWorldRuntime(state.SimulationVersion) {
		return 0, fmt.Errorf("recovery world runtime state is unavailable")
	}
	runtime := state.WorldRuntime
	if runtime.Profile.RuntimeID != worldRuntimeID || runtime.Profile.RuntimeVersion != worldRuntimeVersion ||
		runtime.Profile.CatalogVersion != worldRuntimeCatalogVersion ||
		runtime.Profile.ActorCount != int64(len(runtime.Actors)) ||
		runtime.Profile.FactCount != int64(len(runtime.Facts)) ||
		runtime.Profile.EffectCount != int64(len(runtime.Effects)) ||
		runtime.Profile.CaseCount != int64(len(runtime.RuleCases)) ||
		runtime.Profile.Revision != int64(len(runtime.Facts))+1 {
		return 0, fmt.Errorf("recovery world runtime profile is inconsistent")
	}
	count, err := clearWorldRuntimeProjection(ctx, tx, worldID)
	if err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO world_runtime_profiles
    (world_id, runtime_id, runtime_version, catalog_version, catalog_hash,
     baseline_tick, maximum_player_actors_per_member, actor_count, fact_count,
     effect_count, case_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb)`,
		worldID, runtime.Profile.RuntimeID, runtime.Profile.RuntimeVersion,
		runtime.Profile.CatalogVersion, runtime.Profile.CatalogHash, runtime.Profile.BaselineTick,
		runtime.Profile.MaximumPlayerActorsPerMember, runtime.Profile.ActorCount,
		runtime.Profile.FactCount, runtime.Profile.EffectCount, runtime.Profile.CaseCount,
		runtime.Profile.Revision, []byte(runtime.Profile.Metadata)); err != nil {
		return count, fmt.Errorf("restore world runtime profile: %w", err)
	}
	count++
	for _, definition := range runtime.Definitions {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO world_runtime_definitions
    (world_id, definition_kind, code, definition_version, content_hash, visibility, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)`, worldID, definition.Kind,
			definition.Code, definition.Version, definition.Hash, definition.Visibility,
			[]byte(definition.Payload)); err != nil {
			return count, fmt.Errorf("restore world runtime definition %s/%s: %w", definition.Kind, definition.Code, err)
		}
		count++
	}
	actorIDs := make(map[string]int64, len(runtime.Actors))
	for _, actor := range runtime.Actors {
		var actorID int64
		if err = tx.QueryRowContext(ctx, `
INSERT INTO world_actors
    (world_id, code, owner_user_id, actor_type_code, name, status,
     archetype_code, archetype_version, created_tick, updated_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb)
RETURNING id`, worldID, actor.Code, cityNullableInt64(actor.OwnerUserID), actor.ActorTypeCode,
			actor.Name, actor.Status, nullableStringValue(actor.ArchetypeCode),
			nullableStringValue(actor.ArchetypeVersion), actor.CreatedTick, actor.UpdatedTick,
			actor.Version, []byte(actor.Metadata)).Scan(&actorID); err != nil {
			return count, fmt.Errorf("restore world actor %s: %w", actor.Code, err)
		}
		actorIDs[actor.Code] = actorID
		count++
	}
	for _, attribute := range runtime.Attributes {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO world_actor_attributes
    (world_id, actor_id, attribute_code, value_units, experience_units,
     last_changed_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`, worldID, actorIDs[attribute.ActorCode],
			attribute.AttributeCode, attribute.ValueUnits, attribute.ExperienceUnits,
			attribute.LastChangedTick, attribute.Version, []byte(attribute.Metadata)); err != nil {
			return count, fmt.Errorf("restore world actor attribute: %w", err)
		}
		count++
	}
	for _, role := range runtime.Roles {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO world_actor_roles
    (world_id, actor_id, role_code, category_code, status, granted_tick,
     revoked_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)`, worldID, actorIDs[role.ActorCode],
			role.RoleCode, role.CategoryCode, role.Status, role.GrantedTick,
			cityNullableInt64(role.RevokedTick), role.Version, []byte(role.Metadata)); err != nil {
			return count, fmt.Errorf("restore world actor role: %w", err)
		}
		count++
	}
	factIDs := make(map[worldRuntimeRecoveryIdentity]int64, len(runtime.Facts))
	for _, fact := range runtime.Facts {
		identity := worldRuntimeRecoveryIdentity{tick: fact.Tick, sequence: fact.Sequence}
		var sourceCommandID, parentFactID, actorID any
		if fact.SourceCommandSequence != nil {
			var id int64
			if err = tx.QueryRowContext(ctx, `SELECT id FROM city_commands WHERE world_id = $1 AND sequence = $2`,
				worldID, *fact.SourceCommandSequence).Scan(&id); err != nil {
				return count, fmt.Errorf("resolve recovery world runtime command: %w", err)
			}
			sourceCommandID = id
		}
		if fact.Parent != nil {
			parentFactID = factIDs[worldRuntimeRecoveryIdentity{tick: fact.Parent.Tick, sequence: fact.Parent.Sequence}]
		}
		if fact.ActorCode != nil {
			actorID = actorIDs[*fact.ActorCode]
		}
		preservedID := preserved.facts[identity]
		query := `
INSERT INTO world_runtime_facts
    (world_id, tick, sequence, source_command_id, parent_fact_id, actor_id,
     fact_type, definition_kind, definition_code, definition_version,
     definition_hash, payload, posted_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, NOW())
RETURNING id`
		args := []any{worldID, fact.Tick, fact.Sequence, sourceCommandID, parentFactID, actorID,
			fact.FactType, nullableStringValue(fact.DefinitionKind), nullableStringValue(fact.DefinitionCode),
			nullableStringValue(fact.DefinitionVersion), nullableStringValue(fact.DefinitionHash), []byte(fact.Payload)}
		if preservedID > 0 {
			query = `
INSERT INTO world_runtime_facts
    (id, world_id, tick, sequence, source_command_id, parent_fact_id, actor_id,
     fact_type, definition_kind, definition_code, definition_version,
     definition_hash, payload, posted_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb, NOW())
RETURNING id`
			args = append([]any{preservedID}, args...)
		}
		var id int64
		if err = tx.QueryRowContext(ctx, query, args...).Scan(&id); err != nil {
			return count, fmt.Errorf("restore world runtime fact %d/%d: %w", fact.Tick, fact.Sequence, err)
		}
		factIDs[identity] = id
		count++
	}
	for _, status := range runtime.Statuses {
		sourceFactID := factIDs[worldRuntimeRecoveryIdentity{tick: status.SourceFactTick, sequence: status.SourceFactSeq}]
		if _, err = tx.ExecContext(ctx, `
INSERT INTO world_actor_statuses
    (world_id, actor_id, instance_code, status_code, lifecycle_status,
     intensity_units, stacks, granted_tick, expires_tick, ended_tick,
     source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb)`,
			worldID, actorIDs[status.ActorCode], status.InstanceCode, status.StatusCode,
			status.Lifecycle, status.IntensityUnits, status.Stacks, status.GrantedTick,
			cityNullableInt64(status.ExpiresTick), cityNullableInt64(status.EndedTick),
			sourceFactID, status.Version, []byte(status.Metadata)); err != nil {
			return count, fmt.Errorf("restore world actor status: %w", err)
		}
		count++
	}
	for _, effect := range runtime.Effects {
		identity := worldRuntimeRecoveryIdentity{tick: effect.Tick, sequence: effect.Sequence}
		preservedID := preserved.effects[identity]
		query := `
INSERT INTO world_effect_operations
    (world_id, tick, sequence, source_fact_id, operation_index, effect_type,
     executor_version, target_actor_id, target_key, before_units, delta_units,
     after_units, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb)`
		args := []any{worldID, effect.Tick, effect.Sequence,
			factIDs[worldRuntimeRecoveryIdentity{tick: effect.SourceFact.Tick, sequence: effect.SourceFact.Sequence}],
			effect.OperationIndex, effect.EffectType, effect.ExecutorVersion,
			nullableWorldRuntimeActorID(actorIDs, effect.TargetActorCode), nullableStringValue(effect.TargetKey),
			cityNullableInt64(effect.BeforeUnits), cityNullableInt64(effect.DeltaUnits),
			cityNullableInt64(effect.AfterUnits), []byte(effect.Payload)}
		if preservedID > 0 {
			query = `
INSERT INTO world_effect_operations
    (id, world_id, tick, sequence, source_fact_id, operation_index, effect_type,
     executor_version, target_actor_id, target_key, before_units, delta_units,
     after_units, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb)`
			args = append([]any{preservedID}, args...)
		}
		if _, err = tx.ExecContext(ctx, query, args...); err != nil {
			return count, fmt.Errorf("restore world effect %d/%d: %w", effect.Tick, effect.Sequence, err)
		}
		count++
	}
	for _, worldCase := range runtime.RuleCases {
		identity := worldRuntimeRecoveryIdentity{tick: worldCase.Tick, sequence: worldCase.Sequence}
		preservedID := preserved.cases[identity]
		query := `
INSERT INTO world_rule_cases
    (world_id, code, tick, sequence, source_fact_id, consequence_fact_id,
     subject_actor_id, rule_code, rule_version, rule_hash, category_code,
     scope_kind, scope_code, status, severity_units, decision_code,
     created_tick, decided_tick, closed_tick, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17, $18, $19, $20::jsonb)`
		consequenceID := any(nil)
		if worldCase.ConsequenceFact != nil {
			consequenceID = factIDs[worldRuntimeRecoveryIdentity{tick: worldCase.ConsequenceFact.Tick, sequence: worldCase.ConsequenceFact.Sequence}]
		}
		args := []any{worldID, worldCase.Code, worldCase.Tick, worldCase.Sequence,
			factIDs[worldRuntimeRecoveryIdentity{tick: worldCase.SourceFact.Tick, sequence: worldCase.SourceFact.Sequence}],
			consequenceID, actorIDs[worldCase.SubjectActorCode], worldCase.RuleCode,
			worldCase.RuleVersion, worldCase.RuleHash, worldCase.CategoryCode,
			worldCase.ScopeKind, worldCase.ScopeCode, worldCase.Status, worldCase.SeverityUnits,
			nullableStringValue(worldCase.DecisionCode), worldCase.CreatedTick,
			cityNullableInt64(worldCase.DecidedTick), cityNullableInt64(worldCase.ClosedTick), []byte(worldCase.Payload)}
		if preservedID > 0 {
			query = `
INSERT INTO world_rule_cases
    (id, world_id, code, tick, sequence, source_fact_id, consequence_fact_id,
     subject_actor_id, rule_code, rule_version, rule_hash, category_code,
     scope_kind, scope_code, status, severity_units, decision_code,
     created_tick, decided_tick, closed_tick, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17, $18, $19, $20, $21::jsonb)`
			args = append([]any{preservedID}, args...)
		}
		if _, err = tx.ExecContext(ctx, query, args...); err != nil {
			return count, fmt.Errorf("restore world rule case %s: %w", worldCase.Code, err)
		}
		count++
	}
	return count, nil
}

func nullableStringValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableWorldRuntimeActorID(actorIDs map[string]int64, actorCode *string) any {
	if actorCode == nil {
		return nil
	}
	actorID, exists := actorIDs[*actorCode]
	if !exists {
		return nil
	}
	return actorID
}
