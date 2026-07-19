package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

func (s *CityEconomyService) performCityOpenWorldActorActivity(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	payload cityOpenWorldActorActivityPayload,
) (cityOpenWorldRuntimeExecution, error) {
	actor, err := loadControlledCityOpenWorldActor(ctx, tx, worldID, command.UserID, payload.ActorCode, WorldActorCapabilityCommand)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	definition, err := loadCityOpenWorldRuntimeDefinition(ctx, tx, worldID, WorldRuntimeDefinitionActivity, payload.ActivityCode)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	activity, err := decodeWorldRuntimeDefinition[worldRuntimeActivityDefinition](definition)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, ErrCitySimulationInvariant.WithCause(err)
	}
	evaluation, err := evaluateCityOpenWorldRuntimeRequirement(ctx, tx, worldID, actor.id, targetTick, activity.Requirements)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if !evaluation.Satisfied {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionRequirement)
	}
	var alreadyPerformed int
	if err = tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_open_world_runtime_facts
WHERE world_id = $1 AND actor_id = $2 AND tick = $3
  AND fact_type = $4 AND definition_kind = $5 AND definition_code = $6`,
		worldID, actor.id, targetTick, CityOpenWorldRuntimeFactActivityPerformed,
		WorldRuntimeDefinitionActivity, definition.Code).Scan(&alreadyPerformed); err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("count open-world activity executions: %w", err)
	}
	if alreadyPerformed >= activity.MaximumPerTick {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionActivityLimit)
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	rootPayload, err := json.Marshal(map[string]any{
		"activity_code": definition.Code, "trigger_tags": activity.TriggerTags, "location": actor.location,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("marshal open-world activity fact: %w", err)
	}
	root, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence, sourceCommandID: &command.ID,
		actorID: &actor.id, factType: CityOpenWorldRuntimeFactActivityPerformed, definition: definition, payload: rootPayload,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	facts := []CityOpenWorldRuntimeFact{root.fact}
	toPost := []*cityOpenWorldRuntimeFactRecord{root}
	effects, nextEffectSeq, err := applyCityOpenWorldRuntimeEffectSpecs(
		ctx, tx, worldID, targetTick, effectSequence, root, actor, activity.Effects,
	)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	nextFactSeq := factSequence + 1
	nextCaseSeq := caseSequence
	cases, ruleFacts, ruleEffects, nextFactSeq, nextEffectSeq, nextCaseSeq, err := applyCityOpenWorldRuntimeRules(
		ctx, tx, worldID, targetTick, nextFactSeq, nextEffectSeq, nextCaseSeq, actor, root, activity.TriggerTags,
	)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	for _, fact := range ruleFacts {
		facts = append(facts, fact.fact)
		toPost = append(toPost, fact)
	}
	effects = append(effects, ruleEffects...)
	for _, fact := range toPost {
		if err = postCityOpenWorldRuntimeFact(ctx, tx, fact.id); err != nil {
			return cityOpenWorldRuntimeExecution{}, err
		}
	}
	if err = touchCityOpenWorldActor(ctx, tx, worldID, actor.id, targetTick); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = updateCityOpenWorldRuntimeProfile(ctx, tx, worldID, 0, int64(len(facts)), int64(len(effects)), int64(len(cases))); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	return cityOpenWorldRuntimeExecution{
		pending: cityOpenWorldRuntimePending(command, "city.open_world.actor.activity_performed", map[string]any{
			"actor_code": actor.actor.Code, "activity_code": definition.Code, "rule_case_count": len(cases),
		}),
		facts: facts, effects: effects, cases: cases,
		nextFactSeq: nextFactSeq, nextEffectSeq: nextEffectSeq, nextCaseSeq: nextCaseSeq,
	}, nil
}

func (s *CityEconomyService) transitionCityOpenWorldActorRole(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	command *CityCommand,
	payload cityOpenWorldActorRoleTransitionPayload,
) (cityOpenWorldRuntimeExecution, error) {
	actor, err := loadControlledCityOpenWorldActor(ctx, tx, worldID, command.UserID, payload.ActorCode, WorldActorCapabilityCommand)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	definition, err := loadCityOpenWorldRuntimeDefinition(ctx, tx, worldID, WorldRuntimeDefinitionRole, payload.RoleCode)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	role, err := decodeWorldRuntimeDefinition[worldRuntimeRoleDefinition](definition)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, ErrCitySimulationInvariant.WithCause(err)
	}
	evaluation, err := evaluateCityOpenWorldRuntimeRequirement(ctx, tx, worldID, actor.id, targetTick, role.Requirements)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if !evaluation.Satisfied {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionRequirement)
	}
	// Lock the active category row directly. PostgreSQL does not allow a
	// row-level lock on an aggregate query, and serialising the mutable row is
	// what prevents two same-tick role transitions from both becoming active.
	var activeRoleCode sql.NullString
	err = tx.QueryRowContext(ctx, `
SELECT role_code
FROM city_open_world_actor_roles
WHERE world_id = $1 AND actor_id = $2 AND category_code = $3 AND status = 'active'
ORDER BY granted_tick DESC, role_code ASC
LIMIT 1
FOR UPDATE`, worldID, actor.id, role.CategoryCode).Scan(&activeRoleCode)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("lock open-world active role: %w", err)
	}
	if errors.Is(err, sql.ErrNoRows) {
		activeRoleCode = sql.NullString{}
	}
	var latestTransition sql.NullInt64
	if err = tx.QueryRowContext(ctx, `
SELECT MAX(COALESCE(revoked_tick, granted_tick))
FROM city_open_world_actor_roles
WHERE world_id = $1 AND actor_id = $2 AND category_code = $3`,
		worldID, actor.id, role.CategoryCode,
	).Scan(&latestTransition); err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("load open-world role transition cooldown: %w", err)
	}
	if activeRoleCode.Valid && activeRoleCode.String == definition.Code {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionRoleActive)
	}
	if role.CooldownTicks > 0 && latestTransition.Valid && targetTick-latestTransition.Int64 < role.CooldownTicks {
		return cityOpenWorldRuntimeExecution{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionRoleCooldown)
	}
	if err = activateCityOpenWorldRuntimeFactWrite(ctx, tx, worldID); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	raw, err := json.Marshal(map[string]any{
		"actor_code": actor.actor.Code, "previous_role_code": nullableCityOpenWorldString(activeRoleCode), "role_code": definition.Code,
		"category_code": role.CategoryCode,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("marshal open-world role transition fact: %w", err)
	}
	root, err := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
		worldID: worldID, tick: targetTick, sequence: factSequence, sourceCommandID: &command.ID,
		actorID: &actor.id, factType: CityOpenWorldRuntimeFactRoleTransitioned, definition: definition, payload: raw,
	})
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if activeRoleCode.Valid {
		if _, err = tx.ExecContext(ctx, `
UPDATE city_open_world_actor_roles
SET status = 'revoked', revoked_tick = $4, version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND actor_id = $2 AND category_code = $3 AND status = 'active'`, worldID, actor.id, role.CategoryCode, targetTick); err != nil {
			return cityOpenWorldRuntimeExecution{}, fmt.Errorf("revoke previous open-world role: %w", err)
		}
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_actor_roles
    (world_id, actor_id, role_code, category_code, status, granted_tick, version, metadata)
VALUES ($1, $2, $3, $4, 'active', $5, 1, '{}'::jsonb)`, worldID, actor.id, definition.Code, role.CategoryCode, targetTick); err != nil {
		return cityOpenWorldRuntimeExecution{}, fmt.Errorf("grant open-world role: %w", err)
	}
	effects, nextEffectSeq, err := applyCityOpenWorldRuntimeEffectSpecs(
		ctx, tx, worldID, targetTick, effectSequence, root, actor, role.OnGrantEffects,
	)
	if err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = touchCityOpenWorldActor(ctx, tx, worldID, actor.id, targetTick); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = postCityOpenWorldRuntimeFact(ctx, tx, root.id); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	if err = updateCityOpenWorldRuntimeProfile(ctx, tx, worldID, 0, 1, int64(len(effects)), 0); err != nil {
		return cityOpenWorldRuntimeExecution{}, err
	}
	return cityOpenWorldRuntimeExecution{
		pending: cityOpenWorldRuntimePending(command, "city.open_world.actor.role_transitioned", map[string]any{"actor_code": actor.actor.Code, "role_code": definition.Code}),
		facts:   []CityOpenWorldRuntimeFact{root.fact}, effects: effects, cases: []CityOpenWorldRuleCase{},
		nextFactSeq: factSequence + 1, nextEffectSeq: nextEffectSeq, nextCaseSeq: caseSequence,
	}, nil
}

func applyCityOpenWorldRuntimeRules(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, factSequence, effectSequence, caseSequence int64,
	actor *cityOpenWorldRuntimeActorRef,
	root *cityOpenWorldRuntimeFactRecord,
	triggerTags []string,
) ([]CityOpenWorldRuleCase, []*cityOpenWorldRuntimeFactRecord, []CityOpenWorldRuntimeEffect, int64, int64, int64, error) {
	definitions, err := loadCityOpenWorldRuntimeDefinitions(ctx, tx, worldID)
	if err != nil {
		return nil, nil, nil, factSequence, effectSequence, caseSequence, err
	}
	ruleDefinitions := make([]CityOpenWorldRuntimeDefinition, 0)
	for _, definition := range definitions {
		if definition.Kind == WorldRuntimeDefinitionRule {
			ruleDefinitions = append(ruleDefinitions, definition)
		}
	}
	cases := make([]CityOpenWorldRuleCase, 0)
	facts := make([]*cityOpenWorldRuntimeFactRecord, 0)
	effects := make([]CityOpenWorldRuntimeEffect, 0)
	for _, definition := range ruleDefinitions {
		rule, decodeErr := decodeWorldRuntimeDefinition[worldRuntimeRuleDefinition](&definition)
		if decodeErr != nil {
			return nil, nil, nil, factSequence, effectSequence, caseSequence, ErrCitySimulationInvariant.WithCause(decodeErr)
		}
		if !cityOpenWorldRuntimeTagsIntersect(rule.Triggers, triggerTags) {
			continue
		}
		evaluation, evaluateErr := evaluateCityOpenWorldRuntimeRequirement(ctx, tx, worldID, actor.id, targetTick, rule.Requirements)
		if evaluateErr != nil {
			return nil, nil, nil, factSequence, effectSequence, caseSequence, evaluateErr
		}
		if !evaluation.Satisfied {
			continue
		}
		occurrences, countErr := countCityOpenWorldRuntimeTriggeredActivities(
			ctx, tx, worldID, actor.id, targetTick, rule.OccurrenceWindowTicks, rule.Triggers,
		)
		if countErr != nil {
			return nil, nil, nil, factSequence, effectSequence, caseSequence, countErr
		}
		tier, found := cityOpenWorldRuntimeRuleTierForOccurrences(rule.Tiers, occurrences)
		if !found {
			continue
		}
		casePayload, marshalErr := json.Marshal(map[string]any{
			"occurrences": occurrences, "trigger_tags": triggerTags, "rule_code": definition.Code,
			"evaluation": evaluation,
		})
		if marshalErr != nil {
			return nil, nil, nil, factSequence, effectSequence, caseSequence, marshalErr
		}
		caseFact, factErr := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
			worldID: worldID, tick: targetTick, sequence: factSequence, parentFactID: &root.id,
			actorID: &actor.id, factType: CityOpenWorldRuntimeFactRuleCaseOpened, definition: &definition, payload: casePayload,
		})
		if factErr != nil {
			return nil, nil, nil, factSequence, effectSequence, caseSequence, factErr
		}
		factSequence++
		facts = append(facts, caseFact)
		consequencePayload, marshalErr := json.Marshal(map[string]any{
			"rule_code": definition.Code, "severity_units": tier.SeverityUnits, "occurrences": occurrences,
		})
		if marshalErr != nil {
			return nil, nil, nil, factSequence, effectSequence, caseSequence, marshalErr
		}
		consequenceFact, factErr := insertCityOpenWorldRuntimeFact(ctx, tx, cityOpenWorldRuntimeFactInsert{
			worldID: worldID, tick: targetTick, sequence: factSequence, parentFactID: &caseFact.id,
			actorID: &actor.id, factType: CityOpenWorldRuntimeFactRuleConsequence, definition: &definition, payload: consequencePayload,
		})
		if factErr != nil {
			return nil, nil, nil, factSequence, effectSequence, caseSequence, factErr
		}
		factSequence++
		facts = append(facts, consequenceFact)
		caseCode := "case." + strconv.FormatInt(actor.id, 10) + "." + strconv.FormatInt(targetTick, 10) + "." + strconv.FormatInt(caseSequence, 10)
		item, caseErr := insertCityOpenWorldRuntimeRuleCase(ctx, tx, cityOpenWorldRuntimeRuleCaseInsert{
			worldID: worldID, code: caseCode, tick: targetTick, sequence: caseSequence, sourceFact: caseFact,
			consequenceFact: consequenceFact, subjectActor: actor, rule: &definition, categoryCode: rule.CategoryCode,
			scopeKind: rule.ScopeKind, scopeCode: rule.ScopeCode, severityUnits: tier.SeverityUnits, payload: casePayload,
		})
		if caseErr != nil {
			return nil, nil, nil, factSequence, effectSequence, caseSequence, caseErr
		}
		caseSequence++
		cases = append(cases, item)
		ruleEffects, nextSequence, effectsErr := applyCityOpenWorldRuntimeEffectSpecs(
			ctx, tx, worldID, targetTick, effectSequence, consequenceFact, actor, tier.Effects,
		)
		if effectsErr != nil {
			return nil, nil, nil, factSequence, effectSequence, caseSequence, effectsErr
		}
		effectSequence = nextSequence
		effects = append(effects, ruleEffects...)
	}
	return cases, facts, effects, factSequence, effectSequence, caseSequence, nil
}

func cityOpenWorldRuntimeTagsIntersect(left, right []string) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(left))
	for _, value := range left {
		seen[value] = struct{}{}
	}
	for _, value := range right {
		if _, found := seen[value]; found {
			return true
		}
	}
	return false
}

func cityOpenWorldRuntimeRuleTierForOccurrences(tiers []worldRuntimeRuleTier, occurrences int) (worldRuntimeRuleTier, bool) {
	best := worldRuntimeRuleTier{}
	found := false
	for _, tier := range tiers {
		if occurrences >= tier.MinimumOccurrences && (!found || tier.MinimumOccurrences > best.MinimumOccurrences) {
			best, found = tier, true
		}
	}
	return best, found
}

func countCityOpenWorldRuntimeTriggeredActivities(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, actorID, targetTick, windowTicks int64,
	triggerTags []string,
) (int, error) {
	fromTick := targetTick - windowTicks + 1
	if fromTick < 1 {
		fromTick = 1
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT payload FROM city_open_world_runtime_facts
WHERE world_id = $1 AND actor_id = $2 AND fact_type = $3
  AND tick BETWEEN $4 AND $5`, worldID, actorID, CityOpenWorldRuntimeFactActivityPerformed, fromTick, targetTick)
	if err != nil {
		return 0, fmt.Errorf("load open-world triggered activities: %w", err)
	}
	defer func() { _ = rows.Close() }()
	count := 0
	for rows.Next() {
		var payload struct {
			TriggerTags []string `json:"trigger_tags"`
		}
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return 0, err
		}
		if err = json.Unmarshal(raw, &payload); err != nil {
			return 0, fmt.Errorf("decode open-world triggered activity: %w", err)
		}
		if cityOpenWorldRuntimeTagsIntersect(payload.TriggerTags, triggerTags) {
			count++
		}
	}
	if err = rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate open-world triggered activities: %w", err)
	}
	return count, nil
}

type cityOpenWorldRuntimeRuleCaseInsert struct {
	worldID         int64
	code            string
	tick            int64
	sequence        int64
	sourceFact      *cityOpenWorldRuntimeFactRecord
	consequenceFact *cityOpenWorldRuntimeFactRecord
	subjectActor    *cityOpenWorldRuntimeActorRef
	rule            *CityOpenWorldRuntimeDefinition
	categoryCode    string
	scopeKind       string
	scopeCode       string
	severityUnits   int64
	payload         json.RawMessage
}

func insertCityOpenWorldRuntimeRuleCase(
	ctx context.Context,
	tx *sql.Tx,
	input cityOpenWorldRuntimeRuleCaseInsert,
) (CityOpenWorldRuleCase, error) {
	if input.sourceFact == nil || input.consequenceFact == nil || input.subjectActor == nil || input.rule == nil {
		return CityOpenWorldRuleCase{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_rule_case"})
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_rule_cases
    (world_id, code, tick, sequence, source_fact_id, consequence_fact_id, subject_actor_id,
     rule_code, rule_version, rule_hash, category_code, scope_kind, scope_code, status,
     severity_units, created_tick, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, 'open', $14, $3, $15::jsonb)`,
		input.worldID, input.code, input.tick, input.sequence, input.sourceFact.id, input.consequenceFact.id,
		input.subjectActor.id, input.rule.Code, input.rule.Version, input.rule.Hash, input.categoryCode,
		input.scopeKind, input.scopeCode, input.severityUnits, []byte(input.payload)); err != nil {
		return CityOpenWorldRuleCase{}, fmt.Errorf("insert open-world rule case: %w", err)
	}
	return CityOpenWorldRuleCase{
		Code: input.code, Tick: input.tick, Sequence: input.sequence,
		SourceFact:       CityOpenWorldRuntimeFactRef{Tick: input.sourceFact.fact.Tick, Sequence: input.sourceFact.fact.Sequence},
		ConsequenceFact:  &CityOpenWorldRuntimeFactRef{Tick: input.consequenceFact.fact.Tick, Sequence: input.consequenceFact.fact.Sequence},
		SubjectActorCode: input.subjectActor.actor.Code, RuleCode: input.rule.Code, RuleVersion: input.rule.Version,
		RuleHash: input.rule.Hash, CategoryCode: input.categoryCode, ScopeKind: input.scopeKind, ScopeCode: input.scopeCode,
		Status: "open", SeverityUnits: input.severityUnits, CreatedTick: input.tick, Payload: input.payload,
	}, nil
}

func applyCityOpenWorldRuntimeEffectSpecs(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, effectSequence int64,
	sourceFact *cityOpenWorldRuntimeFactRecord,
	actor *cityOpenWorldRuntimeActorRef,
	specifications []worldRuntimeEffectSpec,
) ([]CityOpenWorldRuntimeEffect, int64, error) {
	if len(specifications) > cityOpenWorldRuntimeMaximumEffects || sourceFact == nil || actor == nil {
		return nil, effectSequence, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionDefinition)
	}
	effects := make([]CityOpenWorldRuntimeEffect, 0, len(specifications))
	for index, specification := range specifications {
		effect, err := applyCityOpenWorldRuntimeEffectSpec(
			ctx, tx, worldID, targetTick, effectSequence+int64(len(effects)), sourceFact, actor, index+1, specification,
		)
		if err != nil {
			return nil, effectSequence, err
		}
		effects = append(effects, effect)
	}
	return effects, effectSequence + int64(len(effects)), nil
}

func applyCityOpenWorldRuntimeEffectSpec(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, effectSequence int64,
	sourceFact *cityOpenWorldRuntimeFactRecord,
	actor *cityOpenWorldRuntimeActorRef,
	operationIndex int,
	specification worldRuntimeEffectSpec,
) (CityOpenWorldRuntimeEffect, error) {
	switch specification.Type {
	case WorldRuntimeEffectAttributeSet, WorldRuntimeEffectAttributeAdd, WorldRuntimeEffectExperienceAdd:
		return applyCityOpenWorldRuntimeAttributeEffect(ctx, tx, worldID, targetTick, effectSequence, sourceFact, actor, operationIndex, specification)
	case WorldRuntimeEffectStatusGrant, WorldRuntimeEffectStatusRevoke:
		return applyCityOpenWorldRuntimeStatusEffect(ctx, tx, worldID, targetTick, effectSequence, sourceFact, actor, operationIndex, specification)
	case WorldRuntimeEffectRoleGrant, WorldRuntimeEffectRoleRevoke:
		return applyCityOpenWorldRuntimeRoleEffect(ctx, tx, worldID, targetTick, effectSequence, sourceFact, actor, operationIndex, specification)
	default:
		return CityOpenWorldRuntimeEffect{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionDefinition)
	}
}

func applyCityOpenWorldRuntimeAttributeEffect(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, effectSequence int64,
	sourceFact *cityOpenWorldRuntimeFactRecord,
	actor *cityOpenWorldRuntimeActorRef,
	operationIndex int,
	specification worldRuntimeEffectSpec,
) (CityOpenWorldRuntimeEffect, error) {
	definition, err := loadCityOpenWorldRuntimeDefinition(ctx, tx, worldID, WorldRuntimeDefinitionAttribute, specification.Key)
	if err != nil {
		return CityOpenWorldRuntimeEffect{}, err
	}
	attributeDefinition, err := decodeWorldRuntimeDefinition[worldRuntimeAttributeDefinition](definition)
	if err != nil {
		return CityOpenWorldRuntimeEffect{}, ErrCitySimulationInvariant.WithCause(err)
	}
	var value, experience int64
	err = tx.QueryRowContext(ctx, `
SELECT value_units, experience_units
FROM city_open_world_actor_attributes
WHERE world_id = $1 AND actor_id = $2 AND attribute_code = $3
FOR UPDATE`, worldID, actor.id, specification.Key).Scan(&value, &experience)
	if errors.Is(err, sql.ErrNoRows) {
		return CityOpenWorldRuntimeEffect{}, cityOpenWorldRuntimeReject(cityOpenWorldRuntimeRejectionDefinition)
	}
	if err != nil {
		return CityOpenWorldRuntimeEffect{}, fmt.Errorf("lock open-world actor attribute: %w", err)
	}
	before, after, delta := value, value, int64(0)
	field := "value"
	if specification.Type == WorldRuntimeEffectExperienceAdd {
		field, before = "experience", experience
		after = cityOpenWorldRuntimeSaturatingAdd(experience, specification.ValueUnits)
		if after < 0 {
			after = 0
		}
		delta = after - before
		if _, err = tx.ExecContext(ctx, `
UPDATE city_open_world_actor_attributes
SET experience_units = $4, last_changed_tick = $5, version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND actor_id = $2 AND attribute_code = $3`, worldID, actor.id, specification.Key, after, targetTick); err != nil {
			return CityOpenWorldRuntimeEffect{}, fmt.Errorf("update open-world actor experience: %w", err)
		}
	} else {
		if specification.Type == WorldRuntimeEffectAttributeSet {
			after = specification.ValueUnits
		} else {
			after = cityOpenWorldRuntimeSaturatingAdd(value, specification.ValueUnits)
		}
		if attributeDefinition.OverflowPolicy == "clamp" {
			if after < attributeDefinition.MinimumUnits {
				after = attributeDefinition.MinimumUnits
			}
			if after > attributeDefinition.MaximumUnits {
				after = attributeDefinition.MaximumUnits
			}
		}
		delta = after - before
		if _, err = tx.ExecContext(ctx, `
UPDATE city_open_world_actor_attributes
SET value_units = $4, last_changed_tick = $5, version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND actor_id = $2 AND attribute_code = $3`, worldID, actor.id, specification.Key, after, targetTick); err != nil {
			return CityOpenWorldRuntimeEffect{}, fmt.Errorf("update open-world actor attribute: %w", err)
		}
	}
	payload, err := json.Marshal(map[string]any{"attribute_code": specification.Key, "field": field})
	if err != nil {
		return CityOpenWorldRuntimeEffect{}, err
	}
	return insertCityOpenWorldRuntimeEffect(ctx, tx, cityOpenWorldRuntimeEffectInsert{
		worldID: worldID, tick: targetTick, sequence: effectSequence, sourceFact: sourceFact,
		operationIndex: operationIndex, effectType: specification.Type, targetActorID: &actor.id,
		targetActorCode: &actor.actor.Code, targetKey: cityOpenWorldStringPointer(specification.Key),
		beforeUnits: &before, deltaUnits: &delta, afterUnits: &after, payload: payload,
	})
}

func applyCityOpenWorldRuntimeStatusEffect(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, effectSequence int64,
	sourceFact *cityOpenWorldRuntimeFactRecord,
	actor *cityOpenWorldRuntimeActorRef,
	operationIndex int,
	specification worldRuntimeEffectSpec,
) (CityOpenWorldRuntimeEffect, error) {
	definition, err := loadCityOpenWorldRuntimeDefinition(ctx, tx, worldID, WorldRuntimeDefinitionStatus, specification.Key)
	if err != nil {
		return CityOpenWorldRuntimeEffect{}, err
	}
	statusDefinition, err := decodeWorldRuntimeDefinition[worldRuntimeStatusDefinition](definition)
	if err != nil {
		return CityOpenWorldRuntimeEffect{}, ErrCitySimulationInvariant.WithCause(err)
	}
	var statusID int64
	var beforeStacks int64
	var currentExpires sql.NullInt64
	err = tx.QueryRowContext(ctx, `
SELECT id, stacks, expires_tick
FROM city_open_world_actor_statuses
WHERE world_id = $1 AND actor_id = $2 AND status_code = $3 AND lifecycle_status = 'active'
FOR UPDATE`, worldID, actor.id, specification.Key).Scan(&statusID, &beforeStacks, &currentExpires)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return CityOpenWorldRuntimeEffect{}, fmt.Errorf("lock open-world actor status: %w", err)
	}
	if specification.Type == WorldRuntimeEffectStatusRevoke {
		if errors.Is(err, sql.ErrNoRows) {
			zero := int64(0)
			payload, marshalErr := json.Marshal(map[string]any{"status_code": specification.Key, "changed": false})
			if marshalErr != nil {
				return CityOpenWorldRuntimeEffect{}, marshalErr
			}
			return insertCityOpenWorldRuntimeEffect(ctx, tx, cityOpenWorldRuntimeEffectInsert{
				worldID: worldID, tick: targetTick, sequence: effectSequence, sourceFact: sourceFact,
				operationIndex: operationIndex, effectType: specification.Type, targetActorID: &actor.id,
				targetActorCode: &actor.actor.Code, targetKey: cityOpenWorldStringPointer(specification.Key),
				beforeUnits: &zero, deltaUnits: &zero, afterUnits: &zero, payload: payload,
			})
		}
		if _, err = tx.ExecContext(ctx, `
UPDATE city_open_world_actor_statuses
SET lifecycle_status = 'revoked', ended_tick = $3, source_fact_id = $4,
    version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND lifecycle_status = 'active'`, worldID, statusID, targetTick, sourceFact.id); err != nil {
			return CityOpenWorldRuntimeEffect{}, fmt.Errorf("revoke open-world actor status: %w", err)
		}
		zero := int64(0)
		delta := -beforeStacks
		payload, marshalErr := json.Marshal(map[string]any{"status_code": specification.Key, "changed": true})
		if marshalErr != nil {
			return CityOpenWorldRuntimeEffect{}, marshalErr
		}
		return insertCityOpenWorldRuntimeEffect(ctx, tx, cityOpenWorldRuntimeEffectInsert{
			worldID: worldID, tick: targetTick, sequence: effectSequence, sourceFact: sourceFact,
			operationIndex: operationIndex, effectType: specification.Type, targetActorID: &actor.id,
			targetActorCode: &actor.actor.Code, targetKey: cityOpenWorldStringPointer(specification.Key),
			beforeUnits: &beforeStacks, deltaUnits: &delta, afterUnits: &zero, payload: payload,
		})
	}
	stacks := specification.Stacks
	if stacks < 1 {
		stacks = 1
	}
	afterStacks := beforeStacks + int64(stacks)
	if afterStacks > int64(statusDefinition.MaximumStacks) {
		afterStacks = int64(statusDefinition.MaximumStacks)
	}
	if afterStacks < 1 {
		afterStacks = 1
	}
	var expires any
	if specification.DurationTicks > 0 {
		expiresAt := targetTick + specification.DurationTicks
		if currentExpires.Valid && currentExpires.Int64 > expiresAt {
			expiresAt = currentExpires.Int64
		}
		expires = expiresAt
	}
	if errors.Is(err, sql.ErrNoRows) {
		instanceCode := "status." + strconv.FormatInt(actor.id, 10) + "." + specification.Key
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_actor_statuses
    (world_id, actor_id, instance_code, status_code, lifecycle_status, intensity_units,
     stacks, granted_tick, expires_tick, source_fact_id, version, metadata)
VALUES ($1, $2, $3, $4, 'active', $5, $6, $7, $8, $9, 1, '{}'::jsonb)`,
			worldID, actor.id, instanceCode, specification.Key, specification.IntensityUnits, afterStacks,
			targetTick, expires, sourceFact.id); err != nil {
			return CityOpenWorldRuntimeEffect{}, fmt.Errorf("grant open-world actor status: %w", err)
		}
	} else {
		if _, err = tx.ExecContext(ctx, `
UPDATE city_open_world_actor_statuses
SET intensity_units = $3, stacks = $4, expires_tick = $5, source_fact_id = $6,
    version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND id = $2 AND lifecycle_status = 'active'`,
			worldID, statusID, specification.IntensityUnits, afterStacks, expires, sourceFact.id); err != nil {
			return CityOpenWorldRuntimeEffect{}, fmt.Errorf("update open-world actor status: %w", err)
		}
	}
	delta := afterStacks - beforeStacks
	payload, err := json.Marshal(map[string]any{"status_code": specification.Key, "intensity_units": specification.IntensityUnits})
	if err != nil {
		return CityOpenWorldRuntimeEffect{}, err
	}
	return insertCityOpenWorldRuntimeEffect(ctx, tx, cityOpenWorldRuntimeEffectInsert{
		worldID: worldID, tick: targetTick, sequence: effectSequence, sourceFact: sourceFact,
		operationIndex: operationIndex, effectType: specification.Type, targetActorID: &actor.id,
		targetActorCode: &actor.actor.Code, targetKey: cityOpenWorldStringPointer(specification.Key),
		beforeUnits: &beforeStacks, deltaUnits: &delta, afterUnits: &afterStacks, payload: payload,
	})
}

func applyCityOpenWorldRuntimeRoleEffect(
	ctx context.Context,
	tx *sql.Tx,
	worldID, targetTick, effectSequence int64,
	sourceFact *cityOpenWorldRuntimeFactRecord,
	actor *cityOpenWorldRuntimeActorRef,
	operationIndex int,
	specification worldRuntimeEffectSpec,
) (CityOpenWorldRuntimeEffect, error) {
	definition, err := loadCityOpenWorldRuntimeDefinition(ctx, tx, worldID, WorldRuntimeDefinitionRole, specification.Key)
	if err != nil {
		return CityOpenWorldRuntimeEffect{}, err
	}
	role, err := decodeWorldRuntimeDefinition[worldRuntimeRoleDefinition](definition)
	if err != nil {
		return CityOpenWorldRuntimeEffect{}, ErrCitySimulationInvariant.WithCause(err)
	}
	var activeRoleCode sql.NullString
	err = tx.QueryRowContext(ctx, `
SELECT role_code FROM city_open_world_actor_roles
WHERE world_id = $1 AND actor_id = $2 AND category_code = $3 AND status = 'active'
FOR UPDATE`, worldID, actor.id, role.CategoryCode).Scan(&activeRoleCode)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return CityOpenWorldRuntimeEffect{}, fmt.Errorf("lock open-world effect role: %w", err)
	}
	changed := false
	if specification.Type == WorldRuntimeEffectRoleGrant {
		if !activeRoleCode.Valid || activeRoleCode.String != definition.Code {
			if activeRoleCode.Valid {
				if _, err = tx.ExecContext(ctx, `
UPDATE city_open_world_actor_roles
SET status = 'revoked', revoked_tick = $4, version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND actor_id = $2 AND category_code = $3 AND status = 'active'`, worldID, actor.id, role.CategoryCode, targetTick); err != nil {
					return CityOpenWorldRuntimeEffect{}, fmt.Errorf("revoke open-world effect role: %w", err)
				}
			}
			if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_actor_roles
    (world_id, actor_id, role_code, category_code, status, granted_tick, version, metadata)
VALUES ($1, $2, $3, $4, 'active', $5, 1, '{}'::jsonb)`, worldID, actor.id, definition.Code, role.CategoryCode, targetTick); err != nil {
				return CityOpenWorldRuntimeEffect{}, fmt.Errorf("grant open-world effect role: %w", err)
			}
			changed = true
		}
	} else if activeRoleCode.Valid && activeRoleCode.String == definition.Code {
		if _, err = tx.ExecContext(ctx, `
UPDATE city_open_world_actor_roles
SET status = 'revoked', revoked_tick = $4, version = version + 1, updated_at = NOW()
WHERE world_id = $1 AND actor_id = $2 AND role_code = $3 AND status = 'active'`, worldID, actor.id, definition.Code, targetTick); err != nil {
			return CityOpenWorldRuntimeEffect{}, fmt.Errorf("revoke open-world effect role: %w", err)
		}
		changed = true
	}
	payload, err := json.Marshal(map[string]any{"role_code": definition.Code, "category_code": role.CategoryCode, "changed": changed})
	if err != nil {
		return CityOpenWorldRuntimeEffect{}, err
	}
	return insertCityOpenWorldRuntimeEffect(ctx, tx, cityOpenWorldRuntimeEffectInsert{
		worldID: worldID, tick: targetTick, sequence: effectSequence, sourceFact: sourceFact,
		operationIndex: operationIndex, effectType: specification.Type, targetActorID: &actor.id,
		targetActorCode: &actor.actor.Code, targetKey: cityOpenWorldStringPointer(definition.Code), payload: payload,
	})
}

func nullableCityOpenWorldString(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}
