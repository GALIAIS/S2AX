package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type WorldRuntimeCatalog struct {
	Profile     WorldRuntimeProfile      `json:"profile"`
	Definitions []WorldRuntimeDefinition `json:"definitions"`
}

type WorldActorRoleOption struct {
	Definition             WorldRuntimeDefinition     `json:"definition"`
	Active                 bool                       `json:"active"`
	Eligible               bool                       `json:"eligible"`
	CurrentCategoryRole    *string                    `json:"current_category_role,omitempty"`
	CooldownRemainingTicks int64                      `json:"cooldown_remaining_ticks"`
	BlockedReasonCodes     []string                   `json:"blocked_reason_codes"`
	Evaluation             WorldRequirementEvaluation `json:"evaluation"`
}

const (
	worldRuleCaseDefaultPageSize = 100
	worldRuleCaseMaximumPageSize = 500
)

type WorldRuleCaseCursor struct {
	Tick     int64 `json:"tick"`
	Sequence int64 `json:"sequence"`
}

type WorldRuleCasePage struct {
	Items      []WorldRuleCase      `json:"items"`
	NextCursor *WorldRuleCaseCursor `json:"next_cursor,omitempty"`
}

type WorldRuleCaseQueryInput struct {
	UserID        int64
	WorldID       int64
	ActorCode     string
	CategoryCode  string
	Status        string
	AfterTick     int64
	AfterSequence int64
	Limit         int
}

func (s *CityEconomyService) GetWorldRuntimeCatalog(
	ctx context.Context,
	userID, worldID int64,
) (*WorldRuntimeCatalog, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	state, err := loadWorldRuntimeHashState(ctx, s.db, worldID)
	if err != nil {
		return nil, err
	}
	visible := make([]WorldRuntimeDefinition, 0, len(state.Definitions))
	for _, definition := range state.Definitions {
		if definition.Visibility != "hidden" {
			visible = append(visible, definition)
		}
	}
	return &WorldRuntimeCatalog{Profile: state.Profile, Definitions: visible}, nil
}

func (s *CityEconomyService) ListWorldActors(
	ctx context.Context,
	userID, worldID int64,
) ([]WorldActor, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT code, owner_user_id, actor_type_code, name, status, archetype_code,
       archetype_version, created_tick, updated_tick, version, metadata
FROM world_actors
WHERE world_id = $1 AND owner_user_id = $2
ORDER BY status ASC, code ASC`, worldID, userID)
	if err != nil {
		return nil, fmt.Errorf("list controlled world actors: %w", err)
	}
	items := make([]WorldActor, 0)
	for rows.Next() {
		item, scanErr := scanWorldActor(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		items = append(items, *item)
	}
	if err = closeCityRows(rows, "iterate controlled world actors"); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *CityEconomyService) GetWorldActorState(
	ctx context.Context,
	userID, worldID int64,
	actorCode string,
) (*WorldActorState, error) {
	if userID <= 0 || worldID <= 0 || !worldRuntimeCodeValid(actorCode, 128) {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	var actorID int64
	actor, err := scanWorldActor(s.db.QueryRowContext(ctx, `
SELECT code, owner_user_id, actor_type_code, name, status, archetype_code,
       archetype_version, created_tick, updated_tick, version, metadata
FROM world_actors
WHERE world_id = $1 AND owner_user_id = $2 AND code = $3`, worldID, userID, actorCode))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWorldActorNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get controlled world actor: %w", err)
	}
	if err = s.db.QueryRowContext(ctx, `
SELECT id FROM world_actors WHERE world_id = $1 AND owner_user_id = $2 AND code = $3`,
		worldID, userID, actorCode).Scan(&actorID); err != nil {
		return nil, fmt.Errorf("resolve controlled world actor identity: %w", err)
	}
	attributes, err := loadWorldActorAttributes(ctx, s.db, worldID, actorID, actorCode)
	if err != nil {
		return nil, err
	}
	roles, err := loadWorldActorRoles(ctx, s.db, worldID, actorID, actorCode)
	if err != nil {
		return nil, err
	}
	statuses, err := loadWorldActorStatuses(ctx, s.db, worldID, actorID, actorCode)
	if err != nil {
		return nil, err
	}
	facts, err := loadWorldRuntimeFacts(ctx, s.db, worldID, &actorID, 0, 50)
	if err != nil {
		return nil, err
	}
	return &WorldActorState{
		Actor: *actor, Attributes: attributes, Roles: roles, Statuses: statuses, RecentFacts: facts,
	}, nil
}

func (s *CityEconomyService) GetWorldActorRoleOptions(
	ctx context.Context,
	userID, worldID int64,
	actorCode string,
) ([]WorldActorRoleOption, error) {
	if userID <= 0 || worldID <= 0 || !worldRuntimeCodeValid(actorCode, 128) {
		return nil, ErrCityInvalidInput
	}
	var actorID, currentTick int64
	if err := s.db.QueryRowContext(ctx, `
SELECT actor.id, world.current_tick
FROM world_actors actor
JOIN city_worlds world ON world.id = actor.world_id
JOIN city_members member ON member.world_id = actor.world_id
WHERE actor.world_id = $1 AND actor.code = $2 AND actor.owner_user_id = $3
  AND member.user_id = $3 AND member.status = 'active'`, worldID, actorCode, userID).Scan(
		&actorID, &currentTick,
	); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWorldActorNotFound
	} else if err != nil {
		return nil, fmt.Errorf("load actor for role options: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT definition_kind, code, definition_version, content_hash, visibility, payload
FROM world_runtime_definitions
WHERE world_id = $1 AND definition_kind = 'role' AND visibility <> 'hidden'
ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("list world role definitions: %w", err)
	}
	definitions := make([]WorldRuntimeDefinition, 0)
	for rows.Next() {
		var definition WorldRuntimeDefinition
		if err = rows.Scan(&definition.Kind, &definition.Code, &definition.Version,
			&definition.Hash, &definition.Visibility, &definition.Payload); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan world role definition: %w", err)
		}
		definitions = append(definitions, definition)
	}
	if err = closeCityRows(rows, "iterate world role definitions"); err != nil {
		return nil, err
	}
	type activeRoleState struct {
		code        string
		grantedTick int64
	}
	activeRoles := make(map[string]activeRoleState)
	roleRows, err := s.db.QueryContext(ctx, `
SELECT role_code, category_code, granted_tick
FROM world_actor_roles
WHERE world_id = $1 AND actor_id = $2 AND status = 'active'
ORDER BY category_code ASC`, worldID, actorID)
	if err != nil {
		return nil, fmt.Errorf("load active actor roles for options: %w", err)
	}
	for roleRows.Next() {
		var code, category string
		var grantedTick int64
		if err = roleRows.Scan(&code, &category, &grantedTick); err != nil {
			_ = roleRows.Close()
			return nil, fmt.Errorf("scan active actor role for options: %w", err)
		}
		activeRoles[category] = activeRoleState{code: code, grantedTick: grantedTick}
	}
	if err = closeCityRows(roleRows, "iterate active actor roles for options"); err != nil {
		return nil, err
	}
	items := make([]WorldActorRoleOption, 0, len(definitions))
	for _, definition := range definitions {
		role, decodeErr := decodeWorldRuntimeDefinition[worldRuntimeRoleDefinition](&definition)
		if decodeErr != nil {
			return nil, decodeErr
		}
		evaluation, evaluateErr := evaluateWorldRequirement(ctx, s.db, worldID, actorID, currentTick, role.Requirements)
		if evaluateErr != nil {
			return nil, evaluateErr
		}
		categoryRole, categoryActive := activeRoles[role.CategoryCode]
		active := categoryActive && categoryRole.code == definition.Code
		blockedReasons := make([]string, 0, 3)
		if !evaluation.Satisfied {
			blockedReasons = append(blockedReasons, "requirements_not_satisfied")
		}
		if active {
			blockedReasons = append(blockedReasons, "already_active")
		}
		cooldownRemaining := int64(0)
		if categoryActive && !active {
			elapsedAtNextTick := currentTick + 1 - categoryRole.grantedTick
			cooldownRemaining = role.CooldownTicks - elapsedAtNextTick
			if cooldownRemaining < 0 {
				cooldownRemaining = 0
			}
			if cooldownRemaining > 0 {
				blockedReasons = append(blockedReasons, "transition_cooldown")
			}
		}
		var currentCategoryRole *string
		if categoryActive {
			currentCategoryRole = stringPointer(categoryRole.code)
		}
		items = append(items, WorldActorRoleOption{
			Definition: definition, Active: active,
			Eligible:            evaluation.Satisfied && !active && cooldownRemaining == 0,
			CurrentCategoryRole: currentCategoryRole, CooldownRemainingTicks: cooldownRemaining,
			BlockedReasonCodes: blockedReasons, Evaluation: evaluation,
		})
	}
	return items, nil
}

func (s *CityEconomyService) ListWorldRules(
	ctx context.Context,
	userID, worldID int64,
) ([]WorldRuntimeDefinition, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT definition_kind, code, definition_version, content_hash, visibility, payload
FROM world_runtime_definitions
WHERE world_id = $1 AND definition_kind = 'rule' AND visibility <> 'hidden'
ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("list visible world rules: %w", err)
	}
	items := make([]WorldRuntimeDefinition, 0)
	for rows.Next() {
		var item WorldRuntimeDefinition
		if err = rows.Scan(&item.Kind, &item.Code, &item.Version, &item.Hash, &item.Visibility, &item.Payload); err != nil {
			_ = rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate visible world rules"); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *CityEconomyService) ListWorldRuleCases(
	ctx context.Context,
	userID, worldID int64,
) ([]WorldRuleCase, error) {
	page, err := s.QueryWorldRuleCases(ctx, WorldRuleCaseQueryInput{
		UserID: userID, WorldID: worldID, Limit: worldRuleCaseMaximumPageSize,
	})
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

func (s *CityEconomyService) QueryWorldRuleCases(
	ctx context.Context,
	input WorldRuleCaseQueryInput,
) (*WorldRuleCasePage, error) {
	if input.UserID <= 0 || input.WorldID <= 0 || input.AfterTick < 0 || input.AfterSequence < 0 ||
		input.Limit < 0 ||
		(input.AfterTick == 0) != (input.AfterSequence == 0) ||
		(input.ActorCode != "" && !worldRuntimeCodeValid(input.ActorCode, 128)) ||
		(input.CategoryCode != "" && !worldRuntimeCodeValid(input.CategoryCode, 128)) ||
		(input.Status != "" && !worldRuntimeCodeValid(input.Status, 24)) {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "world_rule_case_query"})
	}
	if err := authorizeCityWorldRead(ctx, s.db, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	limit := input.Limit
	if limit <= 0 {
		limit = worldRuleCaseDefaultPageSize
	}
	if limit > worldRuleCaseMaximumPageSize {
		limit = worldRuleCaseMaximumPageSize
	}
	query := `
SELECT value.code, value.tick, value.sequence, source.tick, source.sequence,
       consequence.tick, consequence.sequence, actor.code, value.rule_code,
       value.rule_version, value.rule_hash, value.category_code, value.scope_kind,
       value.scope_code, value.status, value.severity_units, value.decision_code,
       value.created_tick, value.decided_tick, value.closed_tick, value.payload
FROM world_rule_cases value
JOIN world_runtime_facts source ON source.id = value.source_fact_id AND source.world_id = value.world_id
LEFT JOIN world_runtime_facts consequence ON consequence.id = value.consequence_fact_id AND consequence.world_id = value.world_id
JOIN world_actors actor ON actor.id = value.subject_actor_id AND actor.world_id = value.world_id
WHERE value.world_id = $1 AND actor.owner_user_id = $2`
	args := []any{input.WorldID, input.UserID}
	if input.ActorCode != "" {
		query += fmt.Sprintf(` AND actor.code = $%d`, len(args)+1)
		args = append(args, input.ActorCode)
	}
	if input.CategoryCode != "" {
		query += fmt.Sprintf(` AND value.category_code = $%d`, len(args)+1)
		args = append(args, input.CategoryCode)
	}
	if input.Status != "" {
		query += fmt.Sprintf(` AND value.status = $%d`, len(args)+1)
		args = append(args, input.Status)
	}
	if input.AfterTick > 0 {
		query += fmt.Sprintf(` AND (value.tick, value.sequence) < ($%d, $%d)`, len(args)+1, len(args)+2)
		args = append(args, input.AfterTick, input.AfterSequence)
	}
	query += fmt.Sprintf(` ORDER BY value.tick DESC, value.sequence DESC LIMIT $%d`, len(args)+1)
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query controlled world rule cases: %w", err)
	}
	items, err := scanWorldRuleCaseRows(rows)
	if err != nil {
		return nil, err
	}
	page := &WorldRuleCasePage{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = &WorldRuleCaseCursor{Tick: last.Tick, Sequence: last.Sequence}
	}
	return page, nil
}

func loadWorldRuntimeHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*worldRuntimeHashState, error) {
	state := &WorldRuntimeState{
		Definitions: make([]WorldRuntimeDefinition, 0), Actors: make([]WorldActor, 0),
		Attributes: make([]WorldActorAttribute, 0), Roles: make([]WorldActorRole, 0),
		Statuses: make([]WorldActorStatus, 0), Facts: make([]WorldRuntimeFact, 0),
		Effects: make([]WorldEffectOperation, 0), RuleCases: make([]WorldRuleCase, 0),
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT runtime_id, runtime_version, catalog_version, catalog_hash, baseline_tick,
       maximum_player_actors_per_member, actor_count, fact_count, effect_count,
       case_count, revision, metadata
FROM world_runtime_profiles WHERE world_id = $1`, worldID).Scan(
		&state.Profile.RuntimeID, &state.Profile.RuntimeVersion, &state.Profile.CatalogVersion,
		&state.Profile.CatalogHash, &state.Profile.BaselineTick,
		&state.Profile.MaximumPlayerActorsPerMember, &state.Profile.ActorCount,
		&state.Profile.FactCount, &state.Profile.EffectCount, &state.Profile.CaseCount,
		&state.Profile.Revision, &state.Profile.Metadata,
	); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWorldRuntimeStateNotFound
	} else if err != nil {
		return nil, fmt.Errorf("load world runtime profile: %w", err)
	}
	definitionRows, err := queryer.QueryContext(ctx, `
SELECT definition_kind, code, definition_version, content_hash, visibility, payload
FROM world_runtime_definitions WHERE world_id = $1
ORDER BY definition_kind ASC, code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load world runtime definitions: %w", err)
	}
	for definitionRows.Next() {
		var item WorldRuntimeDefinition
		if err = definitionRows.Scan(&item.Kind, &item.Code, &item.Version, &item.Hash,
			&item.Visibility, &item.Payload); err != nil {
			_ = definitionRows.Close()
			return nil, err
		}
		state.Definitions = append(state.Definitions, item)
	}
	if err = closeCityRows(definitionRows, "iterate world runtime definitions"); err != nil {
		return nil, err
	}
	actorRows, err := queryer.QueryContext(ctx, `
SELECT code, owner_user_id, actor_type_code, name, status, archetype_code,
       archetype_version, created_tick, updated_tick, version, metadata
FROM world_actors WHERE world_id = $1 ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load world actors: %w", err)
	}
	for actorRows.Next() {
		item, scanErr := scanWorldActor(actorRows)
		if scanErr != nil {
			_ = actorRows.Close()
			return nil, scanErr
		}
		state.Actors = append(state.Actors, *item)
	}
	if err = closeCityRows(actorRows, "iterate world actors"); err != nil {
		return nil, err
	}
	attributeRows, err := queryer.QueryContext(ctx, `
SELECT actor.code, value.attribute_code, value.value_units, value.experience_units,
       value.last_changed_tick, value.version, value.metadata
FROM world_actor_attributes value
JOIN world_actors actor ON actor.id = value.actor_id AND actor.world_id = value.world_id
WHERE value.world_id = $1 ORDER BY actor.code ASC, value.attribute_code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load world actor attributes: %w", err)
	}
	for attributeRows.Next() {
		var item WorldActorAttribute
		if err = attributeRows.Scan(&item.ActorCode, &item.AttributeCode, &item.ValueUnits,
			&item.ExperienceUnits, &item.LastChangedTick, &item.Version, &item.Metadata); err != nil {
			_ = attributeRows.Close()
			return nil, err
		}
		state.Attributes = append(state.Attributes, item)
	}
	if err = closeCityRows(attributeRows, "iterate world actor attributes"); err != nil {
		return nil, err
	}
	roleRows, err := queryer.QueryContext(ctx, `
SELECT actor.code, role.role_code, role.category_code, role.status,
       role.granted_tick, role.revoked_tick, role.version, role.metadata
FROM world_actor_roles role
JOIN world_actors actor ON actor.id = role.actor_id AND actor.world_id = role.world_id
WHERE role.world_id = $1
ORDER BY actor.code ASC, role.category_code ASC, role.granted_tick ASC, role.role_code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load world actor roles: %w", err)
	}
	for roleRows.Next() {
		var item WorldActorRole
		var revoked sql.NullInt64
		if err = roleRows.Scan(&item.ActorCode, &item.RoleCode, &item.CategoryCode,
			&item.Status, &item.GrantedTick, &revoked, &item.Version, &item.Metadata); err != nil {
			_ = roleRows.Close()
			return nil, err
		}
		item.RevokedTick = nullInt64Pointer(revoked)
		state.Roles = append(state.Roles, item)
	}
	if err = closeCityRows(roleRows, "iterate world actor roles"); err != nil {
		return nil, err
	}
	statusRows, err := queryer.QueryContext(ctx, `
SELECT actor.code, status.instance_code, status.status_code, status.lifecycle_status,
       status.intensity_units, status.stacks, status.granted_tick, status.expires_tick,
       status.ended_tick, fact.tick, fact.sequence, status.version, status.metadata
FROM world_actor_statuses status
JOIN world_actors actor ON actor.id = status.actor_id AND actor.world_id = status.world_id
LEFT JOIN world_runtime_facts fact ON fact.id = status.source_fact_id AND fact.world_id = status.world_id
WHERE status.world_id = $1
ORDER BY actor.code ASC, status.status_code ASC, status.instance_code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load world actor statuses: %w", err)
	}
	for statusRows.Next() {
		var item WorldActorStatus
		var expires, ended, factTick, factSequence sql.NullInt64
		if err = statusRows.Scan(&item.ActorCode, &item.InstanceCode, &item.StatusCode,
			&item.Lifecycle, &item.IntensityUnits, &item.Stacks, &item.GrantedTick,
			&expires, &ended, &factTick, &factSequence, &item.Version, &item.Metadata); err != nil {
			_ = statusRows.Close()
			return nil, err
		}
		item.ExpiresTick = nullInt64Pointer(expires)
		item.EndedTick = nullInt64Pointer(ended)
		if factTick.Valid {
			item.SourceFactTick, item.SourceFactSeq = factTick.Int64, factSequence.Int64
		}
		state.Statuses = append(state.Statuses, item)
	}
	if err = closeCityRows(statusRows, "iterate world actor statuses"); err != nil {
		return nil, err
	}
	state.Facts, err = loadWorldRuntimeFacts(ctx, queryer, worldID, nil, 0, 0)
	if err != nil {
		return nil, err
	}
	state.Effects, err = loadWorldEffectOperations(ctx, queryer, worldID, 0)
	if err != nil {
		return nil, err
	}
	state.RuleCases, err = loadWorldRuleCases(ctx, queryer, worldID, nil, 0)
	if err != nil {
		return nil, err
	}
	return state, nil
}

func scanWorldActor(row cityScannable) (*WorldActor, error) {
	item := &WorldActor{}
	var owner sql.NullInt64
	var archetypeCode, archetypeVersion sql.NullString
	if err := row.Scan(&item.Code, &owner, &item.ActorTypeCode, &item.Name, &item.Status,
		&archetypeCode, &archetypeVersion, &item.CreatedTick, &item.UpdatedTick,
		&item.Version, &item.Metadata); err != nil {
		return nil, err
	}
	item.OwnerUserID = nullInt64Pointer(owner)
	if archetypeCode.Valid {
		item.ArchetypeCode = stringPointer(archetypeCode.String)
	}
	if archetypeVersion.Valid {
		item.ArchetypeVersion = stringPointer(archetypeVersion.String)
	}
	return item, nil
}

func loadWorldActorAttributes(
	ctx context.Context, queryer citySQLQueryer, worldID, actorID int64, actorCode string,
) ([]WorldActorAttribute, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT attribute_code, value_units, experience_units, last_changed_tick, version, metadata
FROM world_actor_attributes
WHERE world_id = $1 AND actor_id = $2 ORDER BY attribute_code ASC`, worldID, actorID)
	if err != nil {
		return nil, fmt.Errorf("load world actor attributes: %w", err)
	}
	items := make([]WorldActorAttribute, 0)
	for rows.Next() {
		item := WorldActorAttribute{ActorCode: actorCode}
		if err = rows.Scan(&item.AttributeCode, &item.ValueUnits, &item.ExperienceUnits,
			&item.LastChangedTick, &item.Version, &item.Metadata); err != nil {
			_ = rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate world actor attributes"); err != nil {
		return nil, err
	}
	return items, nil
}

func loadWorldActorRoles(
	ctx context.Context, queryer citySQLQueryer, worldID, actorID int64, actorCode string,
) ([]WorldActorRole, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT role_code, category_code, status, granted_tick, revoked_tick, version, metadata
FROM world_actor_roles
WHERE world_id = $1 AND actor_id = $2
ORDER BY category_code ASC, granted_tick ASC, role_code ASC`, worldID, actorID)
	if err != nil {
		return nil, fmt.Errorf("load world actor roles: %w", err)
	}
	items := make([]WorldActorRole, 0)
	for rows.Next() {
		item := WorldActorRole{ActorCode: actorCode}
		var revoked sql.NullInt64
		if err = rows.Scan(&item.RoleCode, &item.CategoryCode, &item.Status,
			&item.GrantedTick, &revoked, &item.Version, &item.Metadata); err != nil {
			_ = rows.Close()
			return nil, err
		}
		item.RevokedTick = nullInt64Pointer(revoked)
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate world actor roles"); err != nil {
		return nil, err
	}
	return items, nil
}

func loadWorldActorStatuses(
	ctx context.Context, queryer citySQLQueryer, worldID, actorID int64, actorCode string,
) ([]WorldActorStatus, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT status.instance_code, status.status_code, status.lifecycle_status,
       status.intensity_units, status.stacks, status.granted_tick, status.expires_tick,
       status.ended_tick, fact.tick, fact.sequence, status.version, status.metadata
FROM world_actor_statuses status
LEFT JOIN world_runtime_facts fact ON fact.id = status.source_fact_id AND fact.world_id = status.world_id
WHERE status.world_id = $1 AND status.actor_id = $2
ORDER BY status.status_code ASC, status.instance_code ASC`, worldID, actorID)
	if err != nil {
		return nil, fmt.Errorf("load world actor statuses: %w", err)
	}
	items := make([]WorldActorStatus, 0)
	for rows.Next() {
		item := WorldActorStatus{ActorCode: actorCode}
		var expires, ended, factTick, factSequence sql.NullInt64
		if err = rows.Scan(&item.InstanceCode, &item.StatusCode, &item.Lifecycle,
			&item.IntensityUnits, &item.Stacks, &item.GrantedTick, &expires, &ended,
			&factTick, &factSequence, &item.Version, &item.Metadata); err != nil {
			_ = rows.Close()
			return nil, err
		}
		item.ExpiresTick, item.EndedTick = nullInt64Pointer(expires), nullInt64Pointer(ended)
		if factTick.Valid {
			item.SourceFactTick, item.SourceFactSeq = factTick.Int64, factSequence.Int64
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate world actor statuses"); err != nil {
		return nil, err
	}
	return items, nil
}

func loadWorldRuntimeFacts(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorID *int64,
	tick int64,
	limit int,
) ([]WorldRuntimeFact, error) {
	query := `
SELECT fact.tick, fact.sequence, command.sequence, parent.tick, parent.sequence,
       actor.code, fact.fact_type, fact.definition_kind, fact.definition_code,
       fact.definition_version, fact.definition_hash, fact.payload
FROM world_runtime_facts fact
LEFT JOIN city_commands command ON command.id = fact.source_command_id AND command.world_id = fact.world_id
LEFT JOIN world_runtime_facts parent ON parent.id = fact.parent_fact_id AND parent.world_id = fact.world_id
LEFT JOIN world_actors actor ON actor.id = fact.actor_id AND actor.world_id = fact.world_id
WHERE fact.world_id = $1 AND fact.posted_at IS NOT NULL`
	args := []any{worldID}
	if actorID != nil {
		query += fmt.Sprintf(` AND fact.actor_id = $%d`, len(args)+1)
		args = append(args, *actorID)
	}
	if tick > 0 {
		query += fmt.Sprintf(` AND fact.tick = $%d`, len(args)+1)
		args = append(args, tick)
	}
	if limit > 0 {
		query += ` ORDER BY fact.tick DESC, fact.sequence DESC LIMIT ` + fmt.Sprintf("%d", limit)
	} else {
		query += ` ORDER BY fact.tick ASC, fact.sequence ASC`
	}
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load world runtime facts: %w", err)
	}
	items := make([]WorldRuntimeFact, 0)
	for rows.Next() {
		var item WorldRuntimeFact
		var commandSequence, parentTick, parentSequence sql.NullInt64
		var actorCode, kind, code, version, hash sql.NullString
		if err = rows.Scan(&item.Tick, &item.Sequence, &commandSequence, &parentTick,
			&parentSequence, &actorCode, &item.FactType, &kind, &code, &version,
			&hash, &item.Payload); err != nil {
			_ = rows.Close()
			return nil, err
		}
		item.SourceCommandSequence = nullInt64Pointer(commandSequence)
		if parentTick.Valid {
			item.Parent = &WorldRuntimeFactRef{Tick: parentTick.Int64, Sequence: parentSequence.Int64}
		}
		if actorCode.Valid {
			item.ActorCode = stringPointer(actorCode.String)
		}
		if kind.Valid {
			item.DefinitionKind, item.DefinitionCode = stringPointer(kind.String), stringPointer(code.String)
			item.DefinitionVersion, item.DefinitionHash = stringPointer(version.String), stringPointer(hash.String)
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate world runtime facts"); err != nil {
		return nil, err
	}
	return items, nil
}

func loadWorldEffectOperations(
	ctx context.Context, queryer citySQLQueryer, worldID, tick int64,
) ([]WorldEffectOperation, error) {
	query := `
SELECT effect.tick, effect.sequence, fact.tick, fact.sequence, effect.operation_index,
       effect.effect_type, effect.executor_version, actor.code, effect.target_key,
       effect.before_units, effect.delta_units, effect.after_units, effect.payload
FROM world_effect_operations effect
JOIN world_runtime_facts fact ON fact.id = effect.source_fact_id AND fact.world_id = effect.world_id
LEFT JOIN world_actors actor ON actor.id = effect.target_actor_id AND actor.world_id = effect.world_id
WHERE effect.world_id = $1`
	args := []any{worldID}
	if tick > 0 {
		query += ` AND effect.tick = $2`
		args = append(args, tick)
	}
	query += ` ORDER BY effect.tick ASC, effect.sequence ASC`
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load world effect operations: %w", err)
	}
	items := make([]WorldEffectOperation, 0)
	for rows.Next() {
		var item WorldEffectOperation
		var actorCode, targetKey sql.NullString
		var before, delta, after sql.NullInt64
		if err = rows.Scan(&item.Tick, &item.Sequence, &item.SourceFact.Tick,
			&item.SourceFact.Sequence, &item.OperationIndex, &item.EffectType,
			&item.ExecutorVersion, &actorCode, &targetKey, &before, &delta, &after,
			&item.Payload); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if actorCode.Valid {
			item.TargetActorCode = stringPointer(actorCode.String)
		}
		if targetKey.Valid {
			item.TargetKey = stringPointer(targetKey.String)
		}
		item.BeforeUnits, item.DeltaUnits, item.AfterUnits = nullInt64Pointer(before), nullInt64Pointer(delta), nullInt64Pointer(after)
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate world effect operations"); err != nil {
		return nil, err
	}
	return items, nil
}

func loadWorldRuleCases(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	ownerUserID *int64,
	tick int64,
) ([]WorldRuleCase, error) {
	query := `
SELECT value.code, value.tick, value.sequence, source.tick, source.sequence,
       consequence.tick, consequence.sequence, actor.code, value.rule_code,
       value.rule_version, value.rule_hash, value.category_code, value.scope_kind,
       value.scope_code, value.status, value.severity_units, value.decision_code,
       value.created_tick, value.decided_tick, value.closed_tick, value.payload
FROM world_rule_cases value
JOIN world_runtime_facts source ON source.id = value.source_fact_id AND source.world_id = value.world_id
LEFT JOIN world_runtime_facts consequence ON consequence.id = value.consequence_fact_id AND consequence.world_id = value.world_id
JOIN world_actors actor ON actor.id = value.subject_actor_id AND actor.world_id = value.world_id
WHERE value.world_id = $1`
	args := []any{worldID}
	if ownerUserID != nil {
		query += fmt.Sprintf(` AND actor.owner_user_id = $%d`, len(args)+1)
		args = append(args, *ownerUserID)
	}
	if tick > 0 {
		query += fmt.Sprintf(` AND value.tick = $%d`, len(args)+1)
		args = append(args, tick)
	}
	query += ` ORDER BY value.tick ASC, value.sequence ASC`
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load world rule cases: %w", err)
	}
	return scanWorldRuleCaseRows(rows)
}

func scanWorldRuleCaseRows(rows *sql.Rows) ([]WorldRuleCase, error) {
	items := make([]WorldRuleCase, 0)
	for rows.Next() {
		var item WorldRuleCase
		var consequenceTick, consequenceSequence, decided, closed sql.NullInt64
		var decision sql.NullString
		if err := rows.Scan(&item.Code, &item.Tick, &item.Sequence,
			&item.SourceFact.Tick, &item.SourceFact.Sequence, &consequenceTick,
			&consequenceSequence, &item.SubjectActorCode, &item.RuleCode,
			&item.RuleVersion, &item.RuleHash, &item.CategoryCode, &item.ScopeKind,
			&item.ScopeCode, &item.Status, &item.SeverityUnits, &decision,
			&item.CreatedTick, &decided, &closed, &item.Payload); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if consequenceTick.Valid {
			item.ConsequenceFact = &WorldRuntimeFactRef{Tick: consequenceTick.Int64, Sequence: consequenceSequence.Int64}
		}
		if decision.Valid {
			item.DecisionCode = stringPointer(decision.String)
		}
		item.DecidedTick, item.ClosedTick = nullInt64Pointer(decided), nullInt64Pointer(closed)
		items = append(items, item)
	}
	if err := closeCityRows(rows, "iterate world rule cases"); err != nil {
		return nil, err
	}
	return items, nil
}

func loadWorldRuntimeFactsForTick(
	ctx context.Context, queryer citySQLQueryer, worldID, tick int64,
) ([]WorldRuntimeFact, error) {
	return loadWorldRuntimeFacts(ctx, queryer, worldID, nil, tick, 0)
}

func loadWorldRuleCasesForTick(
	ctx context.Context, queryer citySQLQueryer, worldID, tick int64,
) ([]WorldRuleCase, error) {
	return loadWorldRuleCases(ctx, queryer, worldID, nil, tick)
}
