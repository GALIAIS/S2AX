package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// The public city runtime routes predate the open-world runtime split.  Keep
// their stable wire contract, but select the authoritative runtime storage for
// the world's engine version.  This lets the same player workspace work with
// both F7 worlds and city-openworld-v4+ worlds without exposing a second,
// partially wired API surface.
func cityWorldUsesOpenWorldRuntime(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (bool, string, error) {
	var version string
	if err := queryer.QueryRowContext(ctx, `
SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, "", ErrCityWorldNotFound
		}
		return false, "", fmt.Errorf("load playable runtime world version: %w", err)
	}
	return cityEngineSupportsOpenWorldRuntime(version), version, nil
}

func (s *CityEconomyService) GetPlayableWorldRuntimeCatalog(
	ctx context.Context,
	userID, worldID int64,
) (*WorldRuntimeCatalog, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	openWorld, _, err := cityWorldUsesOpenWorldRuntime(ctx, s.db, worldID)
	if err != nil {
		return nil, err
	}
	if !openWorld {
		return s.GetWorldRuntimeCatalog(ctx, userID, worldID)
	}
	catalog, err := s.GetCityOpenWorldRuntimeCatalog(ctx, userID, worldID)
	if err != nil {
		return nil, err
	}
	return &WorldRuntimeCatalog{
		Profile: WorldRuntimeProfile{
			RuntimeID:                    catalog.Profile.RuntimeID,
			RuntimeVersion:               catalog.Profile.RuntimeVersion,
			CatalogVersion:               catalog.Profile.CatalogVersion,
			CatalogHash:                  catalog.Profile.CatalogHash,
			BaselineTick:                 catalog.Profile.BaselineTick,
			MaximumPlayerActorsPerMember: catalog.Profile.MaximumPlayerActorsPerMember,
			ActorCount:                   catalog.Profile.ActorCount,
			FactCount:                    catalog.Profile.FactCount,
			EffectCount:                  catalog.Profile.EffectCount,
			CaseCount:                    catalog.Profile.CaseCount,
			Revision:                     catalog.Profile.Revision,
			Metadata:                     catalog.Profile.Metadata,
		},
		Definitions: catalog.Definitions,
	}, nil
}

func (s *CityEconomyService) ListPlayableWorldActors(
	ctx context.Context,
	userID, worldID int64,
) ([]WorldActor, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	openWorld, _, err := cityWorldUsesOpenWorldRuntime(ctx, s.db, worldID)
	if err != nil {
		return nil, err
	}
	if !openWorld {
		return s.ListWorldActors(ctx, userID, worldID)
	}
	actors, err := s.ListCityOpenWorldActors(ctx, userID, worldID)
	if err != nil {
		return nil, err
	}
	items := make([]WorldActor, len(actors))
	for index := range actors {
		items[index] = cityOpenWorldActorToWorldActor(actors[index])
	}
	return items, nil
}

func (s *CityEconomyService) GetPlayableWorldActorState(
	ctx context.Context,
	userID, worldID int64,
	actorCode string,
) (*WorldActorState, error) {
	actorCode = strings.ToLower(strings.TrimSpace(actorCode))
	if userID <= 0 || worldID <= 0 || !worldRuntimeCodeValid(actorCode, 128) {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	openWorld, _, err := cityWorldUsesOpenWorldRuntime(ctx, s.db, worldID)
	if err != nil {
		return nil, err
	}
	if !openWorld {
		return s.GetWorldActorState(ctx, userID, worldID, actorCode)
	}
	state, err := s.GetCityOpenWorldActorState(ctx, userID, worldID, actorCode)
	if err != nil {
		return nil, err
	}
	return cityOpenWorldActorStateToWorldActorState(state), nil
}

func (s *CityEconomyService) GetPlayableWorldActorRoleOptions(
	ctx context.Context,
	userID, worldID int64,
	actorCode string,
) ([]WorldActorRoleOption, error) {
	actorCode = strings.ToLower(strings.TrimSpace(actorCode))
	if userID <= 0 || worldID <= 0 || !worldRuntimeCodeValid(actorCode, 128) {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	openWorld, _, err := cityWorldUsesOpenWorldRuntime(ctx, s.db, worldID)
	if err != nil {
		return nil, err
	}
	if !openWorld {
		return s.GetWorldActorRoleOptions(ctx, userID, worldID, actorCode)
	}
	return s.getCityOpenWorldRuntimeRoleOptions(ctx, userID, worldID, actorCode)
}

func (s *CityEconomyService) ListPlayableWorldRules(
	ctx context.Context,
	userID, worldID int64,
) ([]WorldRuntimeDefinition, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	openWorld, _, err := cityWorldUsesOpenWorldRuntime(ctx, s.db, worldID)
	if err != nil {
		return nil, err
	}
	if !openWorld {
		return s.ListWorldRules(ctx, userID, worldID)
	}
	catalog, err := s.GetCityOpenWorldRuntimeCatalog(ctx, userID, worldID)
	if err != nil {
		return nil, err
	}
	items := make([]WorldRuntimeDefinition, 0)
	for _, definition := range catalog.Definitions {
		if definition.Kind == WorldRuntimeDefinitionRule && definition.Visibility != "hidden" {
			items = append(items, definition)
		}
	}
	return items, nil
}

func (s *CityEconomyService) QueryPlayableWorldRuleCases(
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
	openWorld, _, err := cityWorldUsesOpenWorldRuntime(ctx, s.db, input.WorldID)
	if err != nil {
		return nil, err
	}
	if !openWorld {
		return s.QueryWorldRuleCases(ctx, input)
	}
	return s.queryCityOpenWorldRuntimeRuleCases(ctx, input)
}

func (s *CityEconomyService) ListPlayableWorldPortalStates(
	ctx context.Context,
	input WorldPortalAccessQueryInput,
) ([]WorldPortalAccessView, error) {
	input.ActorCode = strings.ToLower(strings.TrimSpace(input.ActorCode))
	if input.UserID <= 0 || input.WorldID <= 0 ||
		(input.ActorCode != "" && !worldRuntimeCodeValid(input.ActorCode, 128)) {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	openWorld, _, err := cityWorldUsesOpenWorldRuntime(ctx, s.db, input.WorldID)
	if err != nil {
		return nil, err
	}
	if !openWorld {
		return s.ListWorldPortalStates(ctx, input)
	}
	return s.listCityOpenWorldRuntimePortalStates(ctx, input)
}

func (s *CityEconomyService) ListPlayableWorldNavigationIntents(
	ctx context.Context,
	input WorldNavigationIntentQueryInput,
) ([]WorldActorNavigationIntent, error) {
	if input.UserID <= 0 || input.WorldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	openWorld, version, err := cityWorldUsesOpenWorldRuntime(ctx, s.db, input.WorldID)
	if err != nil {
		return nil, err
	}
	if !openWorld {
		return s.ListWorldNavigationIntents(ctx, input)
	}
	if !cityEngineSupportsOpenWorldSocialRuntime(version) {
		return nil, ErrWorldNavigationIntentUnavailable
	}
	intents, err := s.ListCityOpenWorldNavigationIntents(ctx, input.UserID, input.WorldID)
	if err != nil {
		return nil, err
	}
	items := make([]WorldActorNavigationIntent, len(intents))
	for index := range intents {
		items[index] = cityOpenWorldNavigationIntentToWorldNavigationIntent(intents[index])
	}
	return items, nil
}

func (s *CityEconomyService) GetPlayableWorldNavigationIntent(
	ctx context.Context,
	input WorldNavigationIntentQueryInput,
) (*WorldActorNavigationIntent, error) {
	input.ActorCode = strings.ToLower(strings.TrimSpace(input.ActorCode))
	if input.UserID <= 0 || input.WorldID <= 0 || !worldRuntimeCodeValid(input.ActorCode, 128) {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	openWorld, _, err := cityWorldUsesOpenWorldRuntime(ctx, s.db, input.WorldID)
	if err != nil {
		return nil, err
	}
	if !openWorld {
		return s.GetWorldNavigationIntent(ctx, input)
	}
	items, err := s.ListPlayableWorldNavigationIntents(ctx, input)
	if err != nil {
		return nil, err
	}
	for index := range items {
		if items[index].ActorCode == input.ActorCode {
			return &items[index], nil
		}
	}
	return nil, ErrWorldNavigationIntentUnavailable
}

func (s *CityEconomyService) ListPlayableWorldNavigationReservations(
	ctx context.Context,
	input WorldNavigationReservationQueryInput,
) ([]WorldNavigationReservation, error) {
	if input.UserID <= 0 || input.WorldID <= 0 || input.Tick != nil && *input.Tick < 1 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	openWorld, version, err := cityWorldUsesOpenWorldRuntime(ctx, s.db, input.WorldID)
	if err != nil {
		return nil, err
	}
	if !openWorld {
		return s.ListWorldNavigationReservations(ctx, input)
	}
	if !cityEngineSupportsOpenWorldSocialRuntime(version) {
		return nil, ErrWorldNavigationIntentUnavailable
	}
	// V5 navigation keeps a fact-backed intent and deterministic move facts;
	// unlike the F7 protocol, it deliberately has no separate reservation
	// ledger.  An empty, available list accurately represents that contract.
	return []WorldNavigationReservation{}, nil
}

func (s *CityEconomyService) getCityOpenWorldRuntimeRoleOptions(
	ctx context.Context,
	userID, worldID int64,
	actorCode string,
) ([]WorldActorRoleOption, error) {
	var actorID, currentTick int64
	query := `
SELECT actor.id, world.current_tick
FROM city_open_world_actors actor
JOIN city_worlds world ON world.id = actor.world_id
JOIN city_members member ON member.world_id = actor.world_id
WHERE actor.world_id = $1 AND actor.code = $2
  AND member.user_id = $3 AND member.status = 'active'
  AND (actor.owner_user_id = $3 OR EXISTS (
      SELECT 1 FROM city_open_world_actor_controls grant_value
      WHERE grant_value.world_id = actor.world_id AND grant_value.actor_id = actor.id
        AND grant_value.user_id = $3 AND grant_value.status = 'active'
		AND grant_value.capability IN ('actor.command', 'actor.control.manage')
	  ))`
	args := []any{worldID, actorCode, userID}
	if IsCitySystemAdministrator(ctx) {
		query = `
SELECT actor.id, world.current_tick
FROM city_open_world_actors actor
JOIN city_worlds world ON world.id = actor.world_id
WHERE actor.world_id = $1 AND actor.code = $2`
		args = []any{worldID, actorCode}
	}
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&actorID, &currentTick); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityOpenWorldActorNotFound
	} else if err != nil {
		return nil, fmt.Errorf("load open-world actor for role options: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT definition_kind, code, definition_version, content_hash, visibility, payload
FROM city_open_world_runtime_definitions
WHERE world_id = $1 AND definition_kind = 'role' AND visibility <> 'hidden'
ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("list open-world role definitions: %w", err)
	}
	definitions := make([]WorldRuntimeDefinition, 0)
	for rows.Next() {
		var definition WorldRuntimeDefinition
		if err = rows.Scan(&definition.Kind, &definition.Code, &definition.Version,
			&definition.Hash, &definition.Visibility, &definition.Payload); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan open-world role definition: %w", err)
		}
		definitions = append(definitions, definition)
	}
	if err = closeCityRows(rows, "iterate open-world role definitions"); err != nil {
		return nil, err
	}
	type activeRoleState struct {
		code        string
		grantedTick int64
	}
	activeRoles := make(map[string]activeRoleState)
	roleRows, err := s.db.QueryContext(ctx, `
SELECT role_code, category_code, granted_tick
FROM city_open_world_actor_roles
WHERE world_id = $1 AND actor_id = $2 AND status = 'active'
ORDER BY category_code ASC`, worldID, actorID)
	if err != nil {
		return nil, fmt.Errorf("load open-world active roles for options: %w", err)
	}
	for roleRows.Next() {
		var code, category string
		var grantedTick int64
		if err = roleRows.Scan(&code, &category, &grantedTick); err != nil {
			_ = roleRows.Close()
			return nil, fmt.Errorf("scan open-world active actor role: %w", err)
		}
		activeRoles[category] = activeRoleState{code: code, grantedTick: grantedTick}
	}
	if err = closeCityRows(roleRows, "iterate open-world active roles for options"); err != nil {
		return nil, err
	}
	items := make([]WorldActorRoleOption, 0, len(definitions))
	for _, definition := range definitions {
		role, decodeErr := decodeWorldRuntimeDefinition[worldRuntimeRoleDefinition](&definition)
		if decodeErr != nil {
			return nil, decodeErr
		}
		evaluation, evaluateErr := evaluateCityOpenWorldRuntimeRequirement(
			ctx, s.db, worldID, actorID, currentTick, role.Requirements,
		)
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

func (s *CityEconomyService) queryCityOpenWorldRuntimeRuleCases(
	ctx context.Context,
	input WorldRuleCaseQueryInput,
) (*WorldRuleCasePage, error) {
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
FROM city_open_world_rule_cases value
JOIN city_open_world_runtime_facts source ON source.id = value.source_fact_id AND source.world_id = value.world_id
LEFT JOIN city_open_world_runtime_facts consequence ON consequence.id = value.consequence_fact_id AND consequence.world_id = value.world_id
JOIN city_open_world_actors actor ON actor.id = value.subject_actor_id AND actor.world_id = value.world_id
WHERE value.world_id = $1`
	args := []any{input.WorldID}
	if !IsCitySystemAdministrator(ctx) {
		query += ` AND (
    actor.owner_user_id = $2 OR EXISTS (
        SELECT 1
        FROM city_open_world_actor_controls grant_value
        JOIN city_members member
          ON member.world_id = grant_value.world_id AND member.user_id = grant_value.user_id
         AND member.status = 'active'
        WHERE grant_value.world_id = actor.world_id AND grant_value.actor_id = actor.id
          AND grant_value.user_id = $2 AND grant_value.status = 'active'
          AND grant_value.capability IN ('actor.command', 'actor.control.manage')
    )
)`
		args = append(args, input.UserID)
	}
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
		return nil, fmt.Errorf("query controlled open-world rule cases: %w", err)
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

func (s *CityEconomyService) listCityOpenWorldRuntimePortalStates(
	ctx context.Context,
	input WorldPortalAccessQueryInput,
) ([]WorldPortalAccessView, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin open-world portal state snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = authorizeCityWorldRead(ctx, tx, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	var worldTick int64
	if err = tx.QueryRowContext(ctx, `
SELECT current_tick FROM city_worlds WHERE id = $1`, input.WorldID).Scan(&worldTick); err != nil {
		return nil, fmt.Errorf("load open-world portal world tick: %w", err)
	}
	actorID := int64(0)
	if input.ActorCode != "" {
		actorID, err = loadCityOpenWorldRuntimeActorIDForRead(
			ctx, tx, input.WorldID, input.UserID, input.ActorCode,
		)
		if err != nil {
			return nil, err
		}
	}
	portals, err := loadCityOpenWorldPortals(ctx, tx, input.WorldID, "")
	if err != nil {
		return nil, err
	}
	states, err := loadCityOpenWorldPortalStates(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	stateByCode := make(map[string]CityOpenWorldPortalState, len(states))
	for _, state := range states {
		stateByCode[state.PortalCode] = state
	}
	defaultRequirement, _, defaultPolicyHash, err := canonicalWorldPortalAccessRequirement(publicWorldPortalAccessRequirement())
	if err != nil {
		return nil, ErrCitySimulationInvariant.WithCause(err)
	}
	items := make([]WorldPortalAccessView, 0, len(portals))
	for _, portal := range portals {
		state, exists := stateByCode[portal.Code]
		if !exists {
			// Open-world portal policy is sparse by design: an absent row means
			// the immutable worldgen portal remains open to the public. Expose
			// that baseline state so a player can discover and use every
			// generated building entrance before an administrator changes it.
			state = CityOpenWorldPortalState{
				PortalCode:        portal.Code,
				BuildingCode:      portal.BuildingCode,
				PortalType:        portal.PortalType,
				StateCode:         WorldPortalStateOpen,
				AccessRequirement: defaultRequirement,
				AccessPolicyHash:  defaultPolicyHash,
				ChangedTick:       0,
				Version:           1,
				Metadata:          json.RawMessage(`{"schema_version":1,"source":"implicit_baseline"}`),
			}
		}
		item := WorldPortalAccessView{
			State: WorldPortalState{
				BuildingCode:      state.BuildingCode,
				PortalCode:        state.PortalCode,
				PortalType:        state.PortalType,
				StateCode:         state.StateCode,
				AccessRequirement: state.AccessRequirement,
				AccessPolicyHash:  state.AccessPolicyHash,
				ChangedTick:       state.ChangedTick,
				SourceFact:        cityOpenWorldFactRefToWorldFactRef(state.SourceFact),
				Version:           state.Version,
				Metadata:          state.Metadata,
			},
			From:          CityNavigationCoordinate{X: portal.From.X, Y: portal.From.Y, Z: portal.From.Z},
			To:            CityNavigationCoordinate{X: portal.To.X, Y: portal.To.Y, Z: portal.To.Z},
			Bidirectional: portal.Bidirectional,
		}
		if actorID > 0 {
			evaluation, evaluateErr := evaluateCityOpenWorldRuntimeRequirement(
				ctx, tx, input.WorldID, actorID, worldTick, state.AccessRequirement,
			)
			if evaluateErr != nil {
				return nil, evaluateErr
			}
			accessible := state.StateCode == WorldPortalStateOpen && evaluation.Satisfied
			item.Accessible = &accessible
			item.AccessEvaluation = &evaluation
		}
		items = append(items, item)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit open-world portal state snapshot: %w", err)
	}
	return items, nil
}

func loadCityOpenWorldRuntimeActorIDForRead(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, userID int64,
	actorCode string,
) (int64, error) {
	query := `
SELECT actor.id
FROM city_open_world_actors actor
WHERE actor.world_id = $1 AND actor.code = $2 AND actor.status = 'active'
  AND (actor.owner_user_id = $3 OR EXISTS (
      SELECT 1
      FROM city_open_world_actor_controls grant_value
      JOIN city_members member
        ON member.world_id = grant_value.world_id AND member.user_id = grant_value.user_id
       AND member.status = 'active'
      WHERE grant_value.world_id = actor.world_id AND grant_value.actor_id = actor.id
        AND grant_value.user_id = $3 AND grant_value.status = 'active'
        AND grant_value.capability IN ('actor.command', 'actor.control.manage')
  ))`
	args := []any{worldID, actorCode, userID}
	if IsCitySystemAdministrator(ctx) {
		query = `
SELECT actor.id
FROM city_open_world_actors actor
WHERE actor.world_id = $1 AND actor.code = $2 AND actor.status = 'active'`
		args = []any{worldID, actorCode}
	}
	var actorID int64
	if err := queryer.QueryRowContext(ctx, query, args...).Scan(&actorID); errors.Is(err, sql.ErrNoRows) {
		return 0, ErrCityOpenWorldActorNotFound
	} else if err != nil {
		return 0, fmt.Errorf("load readable open-world actor: %w", err)
	}
	return actorID, nil
}

func cityOpenWorldActorToWorldActor(source CityOpenWorldActor) WorldActor {
	return WorldActor{
		Code:             source.Code,
		OwnerUserID:      source.OwnerUserID,
		ActorTypeCode:    source.ActorTypeCode,
		Name:             source.Name,
		Status:           source.Status,
		ArchetypeCode:    source.ArchetypeCode,
		ArchetypeVersion: source.ArchetypeVersion,
		CreatedTick:      source.CreatedTick,
		UpdatedTick:      source.UpdatedTick,
		Version:          source.Version,
		Metadata:         source.Metadata,
		Location:         cityOpenWorldLocationToWorldActorLocation(source.Location),
	}
}

func cityOpenWorldLocationToWorldActorLocation(source *CityOpenWorldActorLocation) *WorldActorLocation {
	if source == nil {
		return nil
	}
	spaceCode := source.LocationScope
	var anchorKind, anchorCode *string
	if source.BuildingCode != nil && *source.BuildingCode != "" {
		spaceCode = *source.BuildingCode
		anchorKind = stringPointer("building")
		anchorCode = stringPointer(*source.BuildingCode)
	} else {
		chunkCode := fmt.Sprintf("chunk.%d.%d", source.ChunkX, source.ChunkY)
		anchorKind = stringPointer("chunk")
		anchorCode = stringPointer(chunkCode)
	}
	return &WorldActorLocation{
		ActorCode:        source.ActorCode,
		SpaceKind:        source.SpaceKind,
		SpaceCode:        spaceCode,
		X:                source.X,
		Y:                source.Y,
		Z:                source.Z,
		ChunkX:           source.ChunkX,
		ChunkY:           source.ChunkY,
		LocalX:           source.LocalX,
		LocalY:           source.LocalY,
		AnchorKind:       anchorKind,
		AnchorCode:       anchorCode,
		JurisdictionCode: source.LocationScope,
		MovedTick:        source.MovedTick,
		SourceFact:       cityOpenWorldFactRefToWorldFactRef(source.SourceFact),
		Version:          source.Version,
		Metadata:         source.Metadata,
	}
}

func cityOpenWorldActorStateToWorldActorState(source *CityOpenWorldActorState) *WorldActorState {
	if source == nil {
		return nil
	}
	attributes := make([]WorldActorAttribute, len(source.Attributes))
	for index, value := range source.Attributes {
		attributes[index] = WorldActorAttribute{
			ActorCode: value.ActorCode, AttributeCode: value.AttributeCode,
			ValueUnits: value.ValueUnits, ExperienceUnits: value.ExperienceUnits,
			LastChangedTick: value.LastChangedTick, Version: value.Version, Metadata: value.Metadata,
		}
	}
	roles := make([]WorldActorRole, len(source.Roles))
	for index, value := range source.Roles {
		roles[index] = WorldActorRole{
			ActorCode: value.ActorCode, RoleCode: value.RoleCode, CategoryCode: value.CategoryCode,
			Status: value.Status, GrantedTick: value.GrantedTick, RevokedTick: value.RevokedTick,
			Version: value.Version, Metadata: value.Metadata,
		}
	}
	statuses := make([]WorldActorStatus, len(source.Statuses))
	for index, value := range source.Statuses {
		statuses[index] = WorldActorStatus{
			ActorCode: value.ActorCode, InstanceCode: value.InstanceCode, StatusCode: value.StatusCode,
			Lifecycle: value.Lifecycle, IntensityUnits: value.IntensityUnits, Stacks: value.Stacks,
			GrantedTick: value.GrantedTick, ExpiresTick: value.ExpiresTick, EndedTick: value.EndedTick,
			SourceFactTick: value.SourceFact.Tick, SourceFactSeq: value.SourceFact.Sequence,
			Version: value.Version, Metadata: value.Metadata,
		}
	}
	facts := make([]WorldRuntimeFact, len(source.RecentFacts))
	for index, value := range source.RecentFacts {
		facts[index] = WorldRuntimeFact{
			Tick: value.Tick, Sequence: value.Sequence, SourceCommandSequence: value.SourceCommandSequence,
			Parent: cityOpenWorldFactRefToWorldFactRef(value.Parent), ActorCode: value.ActorCode,
			FactType: value.FactType, DefinitionKind: value.DefinitionKind,
			DefinitionCode: value.DefinitionCode, DefinitionVersion: value.DefinitionVersion,
			DefinitionHash: value.DefinitionHash, Payload: value.Payload,
		}
	}
	controls := make([]WorldActorControlGrant, len(source.ControlGrants))
	for index, value := range source.ControlGrants {
		controls[index] = WorldActorControlGrant{
			Code: value.Code, ActorCode: value.ActorCode, UserID: value.UserID,
			Capability: value.Capability, Status: value.Status, GrantedByUserID: value.GrantedByUserID,
			GrantedTick: value.GrantedTick, RevokedTick: value.RevokedTick,
			GrantSourceFact:  cityOpenWorldFactRefToWorldFactRef(value.GrantSourceFact),
			RevokeSourceFact: cityOpenWorldFactRefToWorldFactRef(value.RevokeSourceFact),
			Version:          value.Version, Metadata: value.Metadata,
		}
	}
	actor := cityOpenWorldActorToWorldActor(source.Actor)
	return &WorldActorState{
		Actor: actor, Attributes: attributes, Roles: roles, Statuses: statuses,
		RecentFacts: facts, Location: actor.Location, ControlGrants: controls,
		Capabilities: source.Capabilities,
	}
}

func cityOpenWorldFactRefToWorldFactRef(source *CityOpenWorldRuntimeFactRef) *WorldRuntimeFactRef {
	if source == nil {
		return nil
	}
	return &WorldRuntimeFactRef{Tick: source.Tick, Sequence: source.Sequence}
}

func cityOpenWorldNavigationIntentToWorldNavigationIntent(source CityOpenWorldNavigationIntent) WorldActorNavigationIntent {
	return WorldActorNavigationIntent{
		ActorCode: source.ActorCode, IntentCode: source.IntentCode,
		Destination: CityNavigationCoordinate{X: source.TargetX, Y: source.TargetY, Z: source.TargetZ},
		Status:      source.Status, OnBlocked: WorldNavigationOnBlockedRetry,
		Priority: source.Priority, MaxSteps: source.MaximumSteps,
		BudgetUnits: 0, BudgetGainUnits: 0, BudgetCapUnits: 0,
		BlockedAttempts: source.BlockedAttempts, NextAttemptTick: source.NextAttemptTick,
		CreatedTick: source.CreatedTick, UpdatedTick: source.UpdatedTick,
		SourceFact: WorldRuntimeFactRef{Tick: source.SourceFact.Tick, Sequence: source.SourceFact.Sequence},
		Version:    source.Version, Metadata: source.Metadata,
	}
}
