package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

func (s *CityEconomyService) GetCityOpenWorldRuntimeCatalog(
	ctx context.Context,
	userID, worldID int64,
) (*CityOpenWorldRuntimeCatalog, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	state, err := loadCityOpenWorldRuntimeHashState(ctx, s.db, worldID)
	if err != nil {
		return nil, err
	}
	definitions := make([]CityOpenWorldRuntimeDefinition, 0, len(state.Definitions))
	for _, definition := range state.Definitions {
		if definition.Visibility != "hidden" {
			definitions = append(definitions, definition)
		}
	}
	return &CityOpenWorldRuntimeCatalog{Profile: state.Profile, Definitions: definitions}, nil
}

func (s *CityEconomyService) ListCityOpenWorldActors(
	ctx context.Context,
	userID, worldID int64,
) ([]CityOpenWorldActor, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT actor.code, actor.owner_user_id, actor.actor_type_code, actor.name, actor.status,
       actor.archetype_code, actor.archetype_version, actor.created_tick, actor.updated_tick,
       actor.version, actor.metadata
FROM city_open_world_actors actor
WHERE actor.world_id = $1 AND (
    actor.owner_user_id = $2 OR EXISTS (
        SELECT 1
        FROM city_open_world_actor_controls grant_value
        JOIN city_members member
          ON member.world_id = grant_value.world_id AND member.user_id = grant_value.user_id
         AND member.status = 'active'
        WHERE grant_value.world_id = actor.world_id
          AND grant_value.actor_id = actor.id
          AND grant_value.user_id = $2
          AND grant_value.status = 'active'
          AND grant_value.capability IN ('actor.command', 'actor.control.manage')
    )
)
ORDER BY actor.status ASC, actor.code ASC`, worldID, userID)
	if err != nil {
		return nil, fmt.Errorf("list visible open-world actors: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldActor, 0)
	for rows.Next() {
		item, scanErr := scanCityOpenWorldActor(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		location, locationErr := loadCityOpenWorldActorLocationByCode(ctx, s.db, worldID, item.Code)
		if locationErr != nil {
			return nil, locationErr
		}
		item.Location = location
		items = append(items, *item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate visible open-world actors: %w", err)
	}
	return items, nil
}

func (s *CityEconomyService) GetCityOpenWorldActorState(
	ctx context.Context,
	userID, worldID int64,
	actorCode string,
) (*CityOpenWorldActorState, error) {
	actorCode = strings.ToLower(strings.TrimSpace(actorCode))
	if userID <= 0 || worldID <= 0 || !worldRuntimeCodeValid(actorCode, 128) {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	var actorID int64
	actor, err := scanCityOpenWorldActor(s.db.QueryRowContext(ctx, `
SELECT actor.code, actor.owner_user_id, actor.actor_type_code, actor.name, actor.status,
       actor.archetype_code, actor.archetype_version, actor.created_tick, actor.updated_tick,
       actor.version, actor.metadata
FROM city_open_world_actors actor
WHERE actor.world_id = $1 AND actor.code = $3 AND (
    actor.owner_user_id = $2 OR EXISTS (
        SELECT 1
        FROM city_open_world_actor_controls grant_value
        JOIN city_members member
          ON member.world_id = grant_value.world_id AND member.user_id = grant_value.user_id
         AND member.status = 'active'
        WHERE grant_value.world_id = actor.world_id
          AND grant_value.actor_id = actor.id
          AND grant_value.user_id = $2
          AND grant_value.status = 'active'
          AND grant_value.capability IN ('actor.command', 'actor.control.manage')
    )
)
FOR SHARE`, worldID, userID, actorCode))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityOpenWorldActorNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get visible open-world actor: %w", err)
	}
	if err = s.db.QueryRowContext(ctx, `
SELECT id FROM city_open_world_actors WHERE world_id = $1 AND code = $2`, worldID, actorCode).Scan(&actorID); err != nil {
		return nil, fmt.Errorf("resolve open-world actor identity: %w", err)
	}
	attributes, err := loadCityOpenWorldActorAttributes(ctx, s.db, worldID, actorID, actorCode)
	if err != nil {
		return nil, err
	}
	roles, err := loadCityOpenWorldActorRoles(ctx, s.db, worldID, actorID, actorCode)
	if err != nil {
		return nil, err
	}
	statuses, err := loadCityOpenWorldActorStatuses(ctx, s.db, worldID, actorID, actorCode)
	if err != nil {
		return nil, err
	}
	location, err := loadCityOpenWorldActorLocationByCode(ctx, s.db, worldID, actorCode)
	if err != nil {
		return nil, err
	}
	actor.Location = location
	controls, err := loadCityOpenWorldActorControls(ctx, s.db, worldID, actorID, actorCode)
	if err != nil {
		return nil, err
	}
	facts, err := loadCityOpenWorldRuntimeFacts(ctx, s.db, worldID, nil)
	if err != nil {
		return nil, err
	}
	recentFacts := make([]CityOpenWorldRuntimeFact, 0, 64)
	for index := len(facts) - 1; index >= 0 && len(recentFacts) < 64; index-- {
		if facts[index].ActorCode != nil && *facts[index].ActorCode == actorCode {
			recentFacts = append(recentFacts, facts[index])
		}
	}
	for left, right := 0, len(recentFacts)-1; left < right; left, right = left+1, right-1 {
		recentFacts[left], recentFacts[right] = recentFacts[right], recentFacts[left]
	}
	capabilities := make([]string, 0, 2)
	for _, grant := range controls {
		if grant.UserID == userID && grant.Status == "active" {
			capabilities = append(capabilities, grant.Capability)
		}
	}
	if actor.OwnerUserID != nil && *actor.OwnerUserID == userID {
		capabilities = append(capabilities, WorldActorCapabilityCommand, WorldActorCapabilityManageControl)
	}
	capabilities = cityOpenWorldUniqueSortedStrings(capabilities)
	return &CityOpenWorldActorState{
		Actor: *actor, Attributes: attributes, Roles: roles, Statuses: statuses,
		RecentFacts: recentFacts, ControlGrants: controls, Capabilities: capabilities,
	}, nil
}

func (s *CityEconomyService) ListCityOpenWorldPortalStates(
	ctx context.Context,
	userID, worldID int64,
) ([]CityOpenWorldPortalState, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	return loadCityOpenWorldPortalStates(ctx, s.db, worldID)
}

func loadCityOpenWorldRuntimeHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityOpenWorldRuntimeHashState, error) {
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	state := &CityOpenWorldRuntimeState{
		Definitions: make([]CityOpenWorldRuntimeDefinition, 0), Actors: make([]CityOpenWorldActor, 0),
		Attributes: make([]CityOpenWorldActorAttribute, 0), Roles: make([]CityOpenWorldActorRole, 0),
		Statuses: make([]CityOpenWorldActorStatus, 0), Locations: make([]CityOpenWorldActorLocation, 0),
		ControlGrants: make([]CityOpenWorldActorControlGrant, 0), PortalStates: make([]CityOpenWorldPortalState, 0),
		Facts: make([]CityOpenWorldRuntimeFact, 0), Effects: make([]CityOpenWorldRuntimeEffect, 0),
		RuleCases: make([]CityOpenWorldRuleCase, 0),
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT runtime_id, runtime_version, catalog_version, catalog_hash, baseline_tick,
       maximum_player_actors_per_member, actor_count, fact_count, effect_count,
       case_count, revision, metadata
FROM city_open_world_runtime_profiles WHERE world_id = $1`, worldID).Scan(
		&state.Profile.RuntimeID, &state.Profile.RuntimeVersion, &state.Profile.CatalogVersion,
		&state.Profile.CatalogHash, &state.Profile.BaselineTick,
		&state.Profile.MaximumPlayerActorsPerMember, &state.Profile.ActorCount,
		&state.Profile.FactCount, &state.Profile.EffectCount, &state.Profile.CaseCount,
		&state.Profile.Revision, &state.Profile.Metadata,
	); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityOpenWorldRuntimeNotFound
	} else if err != nil {
		return nil, fmt.Errorf("load open-world runtime profile: %w", err)
	}
	definitions, err := loadCityOpenWorldRuntimeDefinitions(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	state.Definitions = definitions
	actors, err := loadCityOpenWorldActors(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	state.Actors = actors
	attributes, err := loadCityOpenWorldAllActorAttributes(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	state.Attributes = attributes
	roles, err := loadCityOpenWorldAllActorRoles(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	state.Roles = roles
	statuses, err := loadCityOpenWorldAllActorStatuses(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	state.Statuses = statuses
	locations, err := loadCityOpenWorldActorLocations(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	state.Locations = locations
	controls, err := loadCityOpenWorldAllActorControls(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	state.ControlGrants = controls
	portalStates, err := loadCityOpenWorldPortalStates(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	state.PortalStates = portalStates
	facts, err := loadCityOpenWorldRuntimeFacts(ctx, queryer, worldID, nil)
	if err != nil {
		return nil, err
	}
	state.Facts = facts
	effects, err := loadCityOpenWorldRuntimeEffects(ctx, queryer, worldID, nil)
	if err != nil {
		return nil, err
	}
	state.Effects = effects
	cases, err := loadCityOpenWorldRuleCases(ctx, queryer, worldID, nil)
	if err != nil {
		return nil, err
	}
	state.RuleCases = cases
	if state.Profile.RuntimeID == cityOpenWorldSocialRuntimeID {
		social, socialErr := loadCityOpenWorldV5SocialRuntimeState(ctx, queryer, worldID)
		if socialErr != nil {
			return nil, socialErr
		}
		state.Social = social
	}
	var simulationVersion string
	if err := queryer.QueryRowContext(ctx, `
SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&simulationVersion); err != nil {
		return nil, fmt.Errorf("load open-world runtime simulation version: %w", err)
	}
	if cityEngineSupportsOpenWorldServiceCoordination(simulationVersion) {
		services, serviceErr := loadCityOpenWorldServiceState(ctx, queryer, worldID)
		if serviceErr != nil {
			return nil, serviceErr
		}
		state.Services = services
	}
	if cityEngineSupportsOpenWorldImpactBridge(simulationVersion) {
		impacts, impactErr := loadCityOpenWorldImpactState(ctx, queryer, worldID)
		if impactErr != nil {
			return nil, impactErr
		}
		state.Impacts = impacts
	}
	if cityEngineSupportsOpenWorldMobility(simulationVersion) {
		mobility, mobilityErr := loadCityOpenWorldMobilityState(ctx, queryer, worldID)
		if mobilityErr != nil {
			return nil, mobilityErr
		}
		state.Mobility = mobility
	}
	if cityEngineSupportsOpenWorldArrivalBridge(simulationVersion) {
		arrivals, arrivalErr := loadCityOpenWorldMobilityArrivalState(ctx, queryer, worldID)
		if arrivalErr != nil {
			return nil, arrivalErr
		}
		state.Arrivals = arrivals
	}
	if cityEngineSupportsOpenWorldMobilityOD(simulationVersion) {
		od, odErr := loadCityOpenWorldMobilityODState(ctx, queryer, worldID)
		if odErr != nil {
			return nil, odErr
		}
		state.OD = od
	}
	if cityEngineSupportsOpenWorldCommuteBindings(simulationVersion) {
		commutes, commuteErr := loadCityOpenWorldCommuteState(ctx, queryer, worldID)
		if commuteErr != nil {
			return nil, commuteErr
		}
		state.Commutes = commutes
	}
	if cityEngineSupportsOpenWorldCommuteSources(simulationVersion) {
		commuteSources, sourceErr := loadCityOpenWorldCommuteSourceState(ctx, queryer, worldID)
		if sourceErr != nil {
			return nil, sourceErr
		}
		state.CommuteSources = commuteSources
	}
	if cityEngineSupportsOpenWorldCommuteLifecycle(simulationVersion) {
		lifecycle, lifecycleErr := loadCityOpenWorldCommuteLifecycleState(ctx, queryer, worldID)
		if lifecycleErr != nil {
			return nil, lifecycleErr
		}
		state.CommuteLifecycle = lifecycle
	}
	if cityEngineSupportsOpenWorldSupplyChain(simulationVersion) {
		supplyChain, supplyChainErr := loadCityOpenWorldSupplyChainState(ctx, queryer, worldID)
		if supplyChainErr != nil {
			return nil, supplyChainErr
		}
		state.SupplyChain = supplyChain
	}
	if cityEngineSupportsOpenWorldEnterpriseFreight(simulationVersion) {
		freight, freightErr := loadCityOpenWorldEnterpriseFreightState(ctx, queryer, worldID)
		if freightErr != nil {
			return nil, freightErr
		}
		state.EnterpriseFreight = freight
	}
	if cityEngineSupportsOpenWorldEnterpriseFreightReceipts(simulationVersion) {
		receipts, receiptErr := loadCityOpenWorldEnterpriseFreightReceiptState(ctx, queryer, worldID)
		if receiptErr != nil {
			return nil, receiptErr
		}
		state.EnterpriseFreightReceipts = receipts
	}
	if cityEngineSupportsOpenWorldFreightBatches(simulationVersion) {
		batches, batchErr := loadCityOpenWorldFreightBatchState(ctx, queryer, worldID)
		if batchErr != nil {
			return nil, batchErr
		}
		state.EnterpriseFreightBatches = batches
	}
	if cityEngineSupportsOpenWorldSpatialNetwork(simulationVersion) {
		network, networkErr := loadCityOpenWorldSpatialNetworkState(ctx, queryer, worldID)
		if networkErr != nil {
			return nil, networkErr
		}
		state.SpatialNetwork = network
	}
	if cityEngineSupportsOpenWorldInfrastructure(simulationVersion) {
		infrastructure, infrastructureErr := loadCityOpenWorldInfrastructureState(ctx, queryer, worldID)
		if infrastructureErr != nil {
			return nil, infrastructureErr
		}
		state.Infrastructure = infrastructure
	}
	if cityEngineSupportsOpenWorldEffectiveCapacity(simulationVersion) {
		effectiveCapacity, effectiveCapacityErr := loadCityOpenWorldEffectiveCapacityState(ctx, queryer, worldID)
		if effectiveCapacityErr != nil {
			return nil, effectiveCapacityErr
		}
		state.EffectiveCapacity = effectiveCapacity
	}
	if cityEngineSupportsOpenWorldFreightSettlements(simulationVersion) {
		freightSettlements, settlementErr := loadCityOpenWorldFreightSettlementState(ctx, queryer, worldID)
		if settlementErr != nil {
			return nil, settlementErr
		}
		state.FreightSettlements = freightSettlements
	}
	if cityEngineSupportsOpenWorldCarrierRecovery(simulationVersion) {
		carrierRecovery, recoveryErr := loadCityOpenWorldCarrierRecoveryState(ctx, queryer, worldID)
		if recoveryErr != nil {
			return nil, recoveryErr
		}
		state.CarrierRecovery = carrierRecovery
	}
	if cityEngineSupportsOpenWorldCarrierCommerce(simulationVersion) {
		carrierCommerce, commerceErr := loadCityOpenWorldCarrierCommerceState(ctx, queryer, worldID)
		if commerceErr != nil {
			return nil, commerceErr
		}
		state.CarrierCommerce = carrierCommerce
	}
	return state, nil
}

func loadCityOpenWorldRuntimeDefinition(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	kind, code string,
) (*CityOpenWorldRuntimeDefinition, error) {
	item := &CityOpenWorldRuntimeDefinition{}
	err := queryer.QueryRowContext(ctx, `
SELECT definition_kind, code, definition_version, content_hash, visibility, payload
FROM city_open_world_runtime_definitions
WHERE world_id = $1 AND definition_kind = $2 AND code = $3`, worldID, kind, code).Scan(
		&item.Kind, &item.Code, &item.Version, &item.Hash, &item.Visibility, &item.Payload,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityOpenWorldRuntimeDefinitionNotFound.WithMetadata(map[string]string{"kind": kind, "code": code})
	}
	if err != nil {
		return nil, fmt.Errorf("load open-world runtime definition %s/%s: %w", kind, code, err)
	}
	return item, nil
}

func loadCityOpenWorldRuntimeDefinitions(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) ([]CityOpenWorldRuntimeDefinition, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT definition_kind, code, definition_version, content_hash, visibility, payload
FROM city_open_world_runtime_definitions
WHERE world_id = $1
ORDER BY definition_kind ASC, code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load open-world runtime definitions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldRuntimeDefinition, 0)
	for rows.Next() {
		item := CityOpenWorldRuntimeDefinition{}
		if err = rows.Scan(&item.Kind, &item.Code, &item.Version, &item.Hash, &item.Visibility, &item.Payload); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open-world runtime definitions: %w", err)
	}
	return items, nil
}

func loadCityOpenWorldActors(ctx context.Context, queryer citySQLQueryer, worldID int64) ([]CityOpenWorldActor, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT code, owner_user_id, actor_type_code, name, status, archetype_code,
       archetype_version, created_tick, updated_tick, version, metadata
FROM city_open_world_actors
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load open-world actors: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldActor, 0)
	for rows.Next() {
		item, scanErr := scanCityOpenWorldActor(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, *item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open-world actors: %w", err)
	}
	return items, nil
}

func scanCityOpenWorldActor(row cityScannable) (*CityOpenWorldActor, error) {
	item := &CityOpenWorldActor{}
	var ownerID sql.NullInt64
	var archetypeCode, archetypeVersion sql.NullString
	if err := row.Scan(
		&item.Code, &ownerID, &item.ActorTypeCode, &item.Name, &item.Status,
		&archetypeCode, &archetypeVersion, &item.CreatedTick, &item.UpdatedTick,
		&item.Version, &item.Metadata,
	); err != nil {
		return nil, err
	}
	item.OwnerUserID = nullInt64Pointer(ownerID)
	if archetypeCode.Valid {
		item.ArchetypeCode = cityOpenWorldStringPointer(archetypeCode.String)
		item.ArchetypeVersion = cityOpenWorldStringPointer(archetypeVersion.String)
	}
	return item, nil
}

func loadCityOpenWorldActorAttributes(
	ctx context.Context, queryer citySQLQueryer, worldID, actorID int64, actorCode string,
) ([]CityOpenWorldActorAttribute, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT attribute_code, value_units, experience_units, last_changed_tick, version, metadata
FROM city_open_world_actor_attributes
WHERE world_id = $1 AND actor_id = $2
ORDER BY attribute_code ASC`, worldID, actorID)
	if err != nil {
		return nil, fmt.Errorf("load open-world actor attributes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldActorAttribute, 0)
	for rows.Next() {
		item := CityOpenWorldActorAttribute{ActorCode: actorCode}
		if err = rows.Scan(&item.AttributeCode, &item.ValueUnits, &item.ExperienceUnits,
			&item.LastChangedTick, &item.Version, &item.Metadata); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open-world actor attributes: %w", err)
	}
	return items, nil
}

func loadCityOpenWorldAllActorAttributes(ctx context.Context, queryer citySQLQueryer, worldID int64) ([]CityOpenWorldActorAttribute, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT actor.code, value.attribute_code, value.value_units, value.experience_units,
       value.last_changed_tick, value.version, value.metadata
FROM city_open_world_actor_attributes value
JOIN city_open_world_actors actor ON actor.id = value.actor_id AND actor.world_id = value.world_id
WHERE value.world_id = $1
ORDER BY actor.code ASC, value.attribute_code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load open-world actor attributes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldActorAttribute, 0)
	for rows.Next() {
		item := CityOpenWorldActorAttribute{}
		if err = rows.Scan(&item.ActorCode, &item.AttributeCode, &item.ValueUnits, &item.ExperienceUnits,
			&item.LastChangedTick, &item.Version, &item.Metadata); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open-world actor attributes: %w", err)
	}
	return items, nil
}

func loadCityOpenWorldActorRoles(
	ctx context.Context, queryer citySQLQueryer, worldID, actorID int64, actorCode string,
) ([]CityOpenWorldActorRole, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT role_code, category_code, status, granted_tick, revoked_tick, version, metadata
FROM city_open_world_actor_roles
WHERE world_id = $1 AND actor_id = $2
ORDER BY category_code ASC, granted_tick ASC, role_code ASC`, worldID, actorID)
	if err != nil {
		return nil, fmt.Errorf("load open-world actor roles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldActorRole, 0)
	for rows.Next() {
		item := CityOpenWorldActorRole{ActorCode: actorCode}
		var revoked sql.NullInt64
		if err = rows.Scan(&item.RoleCode, &item.CategoryCode, &item.Status, &item.GrantedTick,
			&revoked, &item.Version, &item.Metadata); err != nil {
			return nil, err
		}
		item.RevokedTick = nullInt64Pointer(revoked)
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open-world actor roles: %w", err)
	}
	return items, nil
}

func loadCityOpenWorldAllActorRoles(ctx context.Context, queryer citySQLQueryer, worldID int64) ([]CityOpenWorldActorRole, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT actor.code, role.role_code, role.category_code, role.status, role.granted_tick,
       role.revoked_tick, role.version, role.metadata
FROM city_open_world_actor_roles role
JOIN city_open_world_actors actor ON actor.id = role.actor_id AND actor.world_id = role.world_id
WHERE role.world_id = $1
ORDER BY actor.code ASC, role.category_code ASC, role.granted_tick ASC, role.role_code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load open-world actor roles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldActorRole, 0)
	for rows.Next() {
		item := CityOpenWorldActorRole{}
		var revoked sql.NullInt64
		if err = rows.Scan(&item.ActorCode, &item.RoleCode, &item.CategoryCode, &item.Status,
			&item.GrantedTick, &revoked, &item.Version, &item.Metadata); err != nil {
			return nil, err
		}
		item.RevokedTick = nullInt64Pointer(revoked)
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open-world actor roles: %w", err)
	}
	return items, nil
}

func loadCityOpenWorldActorStatuses(
	ctx context.Context, queryer citySQLQueryer, worldID, actorID int64, actorCode string,
) ([]CityOpenWorldActorStatus, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT status.instance_code, status.status_code, status.lifecycle_status, status.intensity_units,
       status.stacks, status.granted_tick, status.expires_tick, status.ended_tick,
       fact.tick, fact.sequence, status.version, status.metadata
FROM city_open_world_actor_statuses status
LEFT JOIN city_open_world_runtime_facts fact
  ON fact.id = status.source_fact_id AND fact.world_id = status.world_id
WHERE status.world_id = $1 AND status.actor_id = $2
ORDER BY status.status_code ASC, status.instance_code ASC`, worldID, actorID)
	if err != nil {
		return nil, fmt.Errorf("load open-world actor statuses: %w", err)
	}
	return scanCityOpenWorldActorStatusRows(rows, actorCode)
}

func loadCityOpenWorldAllActorStatuses(ctx context.Context, queryer citySQLQueryer, worldID int64) ([]CityOpenWorldActorStatus, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT actor.code, status.instance_code, status.status_code, status.lifecycle_status,
       status.intensity_units, status.stacks, status.granted_tick, status.expires_tick,
       status.ended_tick, fact.tick, fact.sequence, status.version, status.metadata
FROM city_open_world_actor_statuses status
JOIN city_open_world_actors actor ON actor.id = status.actor_id AND actor.world_id = status.world_id
LEFT JOIN city_open_world_runtime_facts fact
  ON fact.id = status.source_fact_id AND fact.world_id = status.world_id
WHERE status.world_id = $1
ORDER BY actor.code ASC, status.status_code ASC, status.instance_code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load open-world actor statuses: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldActorStatus, 0)
	for rows.Next() {
		item := CityOpenWorldActorStatus{}
		var expires, ended, factTick, factSequence sql.NullInt64
		if err = rows.Scan(&item.ActorCode, &item.InstanceCode, &item.StatusCode, &item.Lifecycle,
			&item.IntensityUnits, &item.Stacks, &item.GrantedTick, &expires, &ended,
			&factTick, &factSequence, &item.Version, &item.Metadata); err != nil {
			return nil, err
		}
		item.ExpiresTick = nullInt64Pointer(expires)
		item.EndedTick = nullInt64Pointer(ended)
		if factTick.Valid {
			item.SourceFact = &CityOpenWorldRuntimeFactRef{Tick: factTick.Int64, Sequence: factSequence.Int64}
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open-world actor statuses: %w", err)
	}
	return items, nil
}

func scanCityOpenWorldActorStatusRows(rows *sql.Rows, actorCode string) ([]CityOpenWorldActorStatus, error) {
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldActorStatus, 0)
	for rows.Next() {
		item := CityOpenWorldActorStatus{ActorCode: actorCode}
		var expires, ended, factTick, factSequence sql.NullInt64
		if err := rows.Scan(&item.InstanceCode, &item.StatusCode, &item.Lifecycle,
			&item.IntensityUnits, &item.Stacks, &item.GrantedTick, &expires, &ended,
			&factTick, &factSequence, &item.Version, &item.Metadata); err != nil {
			return nil, err
		}
		item.ExpiresTick = nullInt64Pointer(expires)
		item.EndedTick = nullInt64Pointer(ended)
		if factTick.Valid {
			item.SourceFact = &CityOpenWorldRuntimeFactRef{Tick: factTick.Int64, Sequence: factSequence.Int64}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open-world actor statuses: %w", err)
	}
	return items, nil
}

func loadCityOpenWorldActorLocationByCode(
	ctx context.Context, queryer citySQLQueryer, worldID int64, actorCode string,
) (*CityOpenWorldActorLocation, error) {
	item := &CityOpenWorldActorLocation{ActorCode: actorCode}
	var buildingCode sql.NullString
	var factTick, factSequence sql.NullInt64
	err := queryer.QueryRowContext(ctx, `
SELECT location.space_kind, location.location_scope, location.building_code, location.floor_index,
       location.x, location.y, location.z, location.sector_x, location.sector_y,
       location.chunk_x, location.chunk_y, location.local_x, location.local_y,
       location.moved_tick, fact.tick, fact.sequence, location.version, location.metadata
FROM city_open_world_actor_locations location
JOIN city_open_world_actors actor ON actor.id = location.actor_id AND actor.world_id = location.world_id
LEFT JOIN city_open_world_runtime_facts fact
  ON fact.id = location.source_fact_id AND fact.world_id = location.world_id
WHERE location.world_id = $1 AND actor.code = $2`, worldID, actorCode).Scan(
		&item.SpaceKind, &item.LocationScope, &buildingCode, &item.FloorIndex,
		&item.X, &item.Y, &item.Z, &item.SectorX, &item.SectorY,
		&item.ChunkX, &item.ChunkY, &item.LocalX, &item.LocalY,
		&item.MovedTick, &factTick, &factSequence, &item.Version, &item.Metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load open-world actor location: %w", err)
	}
	if buildingCode.Valid {
		item.BuildingCode = cityOpenWorldStringPointer(buildingCode.String)
	}
	if factTick.Valid {
		item.SourceFact = &CityOpenWorldRuntimeFactRef{Tick: factTick.Int64, Sequence: factSequence.Int64}
	}
	return item, nil
}

func loadCityOpenWorldActorLocations(ctx context.Context, queryer citySQLQueryer, worldID int64) ([]CityOpenWorldActorLocation, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT actor.code, location.space_kind, location.location_scope, location.building_code,
       location.floor_index, location.x, location.y, location.z, location.sector_x,
       location.sector_y, location.chunk_x, location.chunk_y, location.local_x, location.local_y,
       location.moved_tick, fact.tick, fact.sequence, location.version, location.metadata
FROM city_open_world_actor_locations location
JOIN city_open_world_actors actor ON actor.id = location.actor_id AND actor.world_id = location.world_id
LEFT JOIN city_open_world_runtime_facts fact
  ON fact.id = location.source_fact_id AND fact.world_id = location.world_id
WHERE location.world_id = $1
ORDER BY actor.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load open-world actor locations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldActorLocation, 0)
	for rows.Next() {
		item := CityOpenWorldActorLocation{}
		var buildingCode sql.NullString
		var factTick, factSequence sql.NullInt64
		if err = rows.Scan(&item.ActorCode, &item.SpaceKind, &item.LocationScope, &buildingCode,
			&item.FloorIndex, &item.X, &item.Y, &item.Z, &item.SectorX, &item.SectorY,
			&item.ChunkX, &item.ChunkY, &item.LocalX, &item.LocalY, &item.MovedTick,
			&factTick, &factSequence, &item.Version, &item.Metadata); err != nil {
			return nil, err
		}
		if buildingCode.Valid {
			item.BuildingCode = cityOpenWorldStringPointer(buildingCode.String)
		}
		if factTick.Valid {
			item.SourceFact = &CityOpenWorldRuntimeFactRef{Tick: factTick.Int64, Sequence: factSequence.Int64}
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open-world actor locations: %w", err)
	}
	return items, nil
}

func loadCityOpenWorldActorControls(
	ctx context.Context, queryer citySQLQueryer, worldID, actorID int64, actorCode string,
) ([]CityOpenWorldActorControlGrant, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT code, user_id, capability, status, granted_by_user_id, granted_tick, revoked_tick,
       grant_fact.tick, grant_fact.sequence, revoke_fact.tick, revoke_fact.sequence,
       version, metadata
FROM city_open_world_actor_controls control
LEFT JOIN city_open_world_runtime_facts grant_fact
  ON grant_fact.id = control.grant_source_fact_id AND grant_fact.world_id = control.world_id
LEFT JOIN city_open_world_runtime_facts revoke_fact
  ON revoke_fact.id = control.revoke_source_fact_id AND revoke_fact.world_id = control.world_id
WHERE control.world_id = $1 AND control.actor_id = $2
ORDER BY control.code ASC`, worldID, actorID)
	if err != nil {
		return nil, fmt.Errorf("load open-world actor controls: %w", err)
	}
	return scanCityOpenWorldActorControlRows(rows, actorCode)
}

func loadCityOpenWorldAllActorControls(ctx context.Context, queryer citySQLQueryer, worldID int64) ([]CityOpenWorldActorControlGrant, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT actor.code, control.code, control.user_id, control.capability, control.status,
       control.granted_by_user_id, control.granted_tick, control.revoked_tick,
       grant_fact.tick, grant_fact.sequence, revoke_fact.tick, revoke_fact.sequence,
       control.version, control.metadata
FROM city_open_world_actor_controls control
JOIN city_open_world_actors actor ON actor.id = control.actor_id AND actor.world_id = control.world_id
LEFT JOIN city_open_world_runtime_facts grant_fact
  ON grant_fact.id = control.grant_source_fact_id AND grant_fact.world_id = control.world_id
LEFT JOIN city_open_world_runtime_facts revoke_fact
  ON revoke_fact.id = control.revoke_source_fact_id AND revoke_fact.world_id = control.world_id
WHERE control.world_id = $1
ORDER BY actor.code ASC, control.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load open-world actor controls: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldActorControlGrant, 0)
	for rows.Next() {
		item := CityOpenWorldActorControlGrant{}
		var revoked, grantTick, grantSequence, revokeTick, revokeSequence sql.NullInt64
		if err = rows.Scan(&item.ActorCode, &item.Code, &item.UserID, &item.Capability,
			&item.Status, &item.GrantedByUserID, &item.GrantedTick, &revoked,
			&grantTick, &grantSequence, &revokeTick, &revokeSequence, &item.Version, &item.Metadata); err != nil {
			return nil, err
		}
		item.RevokedTick = nullInt64Pointer(revoked)
		if grantTick.Valid {
			item.GrantSourceFact = &CityOpenWorldRuntimeFactRef{Tick: grantTick.Int64, Sequence: grantSequence.Int64}
		}
		if revokeTick.Valid {
			item.RevokeSourceFact = &CityOpenWorldRuntimeFactRef{Tick: revokeTick.Int64, Sequence: revokeSequence.Int64}
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open-world actor controls: %w", err)
	}
	return items, nil
}

func scanCityOpenWorldActorControlRows(rows *sql.Rows, actorCode string) ([]CityOpenWorldActorControlGrant, error) {
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldActorControlGrant, 0)
	for rows.Next() {
		item := CityOpenWorldActorControlGrant{ActorCode: actorCode}
		var revoked, grantTick, grantSequence, revokeTick, revokeSequence sql.NullInt64
		if err := rows.Scan(&item.Code, &item.UserID, &item.Capability, &item.Status,
			&item.GrantedByUserID, &item.GrantedTick, &revoked, &grantTick, &grantSequence,
			&revokeTick, &revokeSequence, &item.Version, &item.Metadata); err != nil {
			return nil, err
		}
		item.RevokedTick = nullInt64Pointer(revoked)
		if grantTick.Valid {
			item.GrantSourceFact = &CityOpenWorldRuntimeFactRef{Tick: grantTick.Int64, Sequence: grantSequence.Int64}
		}
		if revokeTick.Valid {
			item.RevokeSourceFact = &CityOpenWorldRuntimeFactRef{Tick: revokeTick.Int64, Sequence: revokeSequence.Int64}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open-world actor controls: %w", err)
	}
	return items, nil
}

func loadCityOpenWorldPortalStates(ctx context.Context, queryer citySQLQueryer, worldID int64) ([]CityOpenWorldPortalState, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT portal.code, portal.building_code, portal.portal_type, state.state_code,
       state.access_requirement, state.access_policy_hash, state.changed_tick,
       fact.tick, fact.sequence, state.version, state.metadata
FROM city_open_world_portal_states state
JOIN city_open_world_portals portal ON portal.world_id = state.world_id AND portal.code = state.portal_code
LEFT JOIN city_open_world_runtime_facts fact ON fact.id = state.source_fact_id AND fact.world_id = state.world_id
WHERE state.world_id = $1
ORDER BY portal.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load open-world portal states: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldPortalState, 0)
	for rows.Next() {
		item := CityOpenWorldPortalState{}
		var rawRequirement []byte
		var factTick, factSequence sql.NullInt64
		if err = rows.Scan(&item.PortalCode, &item.BuildingCode, &item.PortalType, &item.StateCode,
			&rawRequirement, &item.AccessPolicyHash, &item.ChangedTick, &factTick, &factSequence,
			&item.Version, &item.Metadata); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(rawRequirement, &item.AccessRequirement); err != nil {
			return nil, fmt.Errorf("decode open-world portal requirement: %w", err)
		}
		if factTick.Valid {
			item.SourceFact = &CityOpenWorldRuntimeFactRef{Tick: factTick.Int64, Sequence: factSequence.Int64}
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open-world portal states: %w", err)
	}
	return items, nil
}

func loadCityOpenWorldRuntimeFacts(
	ctx context.Context, queryer citySQLQueryer, worldID int64, tick *int64,
) ([]CityOpenWorldRuntimeFact, error) {
	query := `
SELECT fact.tick, fact.sequence, command.sequence, parent.tick, parent.sequence, actor.code,
       fact.fact_type, fact.definition_kind, fact.definition_code, fact.definition_version,
       fact.definition_hash, fact.payload
FROM city_open_world_runtime_facts fact
LEFT JOIN city_commands command ON command.id = fact.source_command_id AND command.world_id = fact.world_id
LEFT JOIN city_open_world_runtime_facts parent ON parent.id = fact.parent_fact_id AND parent.world_id = fact.world_id
LEFT JOIN city_open_world_actors actor ON actor.id = fact.actor_id AND actor.world_id = fact.world_id
WHERE fact.world_id = $1 AND fact.posted_at IS NOT NULL`
	args := []any{worldID}
	if tick != nil {
		query += ` AND fact.tick = $2`
		args = append(args, *tick)
	}
	query += ` ORDER BY fact.tick ASC, fact.sequence ASC`
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load open-world runtime facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldRuntimeFact, 0)
	for rows.Next() {
		item := CityOpenWorldRuntimeFact{}
		var commandSequence, parentTick, parentSequence sql.NullInt64
		var actorCode, definitionKind, definitionCode, definitionVersion, definitionHash sql.NullString
		if err = rows.Scan(&item.Tick, &item.Sequence, &commandSequence, &parentTick, &parentSequence,
			&actorCode, &item.FactType, &definitionKind, &definitionCode, &definitionVersion,
			&definitionHash, &item.Payload); err != nil {
			return nil, err
		}
		item.SourceCommandSequence = nullInt64Pointer(commandSequence)
		if parentTick.Valid {
			item.Parent = &CityOpenWorldRuntimeFactRef{Tick: parentTick.Int64, Sequence: parentSequence.Int64}
		}
		if actorCode.Valid {
			item.ActorCode = cityOpenWorldStringPointer(actorCode.String)
		}
		if definitionKind.Valid {
			item.DefinitionKind = cityOpenWorldStringPointer(definitionKind.String)
			item.DefinitionCode = cityOpenWorldStringPointer(definitionCode.String)
			item.DefinitionVersion = cityOpenWorldStringPointer(definitionVersion.String)
			item.DefinitionHash = cityOpenWorldStringPointer(definitionHash.String)
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open-world runtime facts: %w", err)
	}
	return items, nil
}

func loadCityOpenWorldRuntimeEffects(
	ctx context.Context, queryer citySQLQueryer, worldID int64, tick *int64,
) ([]CityOpenWorldRuntimeEffect, error) {
	query := `
SELECT effect.tick, effect.sequence, source_fact.tick, source_fact.sequence, effect.operation_index,
       effect.effect_type, target_actor.code, effect.target_key, effect.before_units,
       effect.delta_units, effect.after_units, effect.payload
FROM city_open_world_runtime_effects effect
JOIN city_open_world_runtime_facts source_fact ON source_fact.id = effect.source_fact_id AND source_fact.world_id = effect.world_id
LEFT JOIN city_open_world_actors target_actor ON target_actor.id = effect.target_actor_id AND target_actor.world_id = effect.world_id
WHERE effect.world_id = $1`
	args := []any{worldID}
	if tick != nil {
		query += ` AND effect.tick = $2`
		args = append(args, *tick)
	}
	query += ` ORDER BY effect.tick ASC, effect.sequence ASC`
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load open-world runtime effects: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldRuntimeEffect, 0)
	for rows.Next() {
		item := CityOpenWorldRuntimeEffect{ExecutorVersion: cityOpenWorldRuntimeExecutorVersion}
		var actorCode, targetKey sql.NullString
		var before, delta, after sql.NullInt64
		if err = rows.Scan(&item.Tick, &item.Sequence, &item.SourceFact.Tick, &item.SourceFact.Sequence,
			&item.OperationIndex, &item.EffectType, &actorCode, &targetKey, &before, &delta, &after, &item.Payload); err != nil {
			return nil, err
		}
		if actorCode.Valid {
			item.TargetActorCode = cityOpenWorldStringPointer(actorCode.String)
		}
		if targetKey.Valid {
			item.TargetKey = cityOpenWorldStringPointer(targetKey.String)
		}
		item.BeforeUnits = nullInt64Pointer(before)
		item.DeltaUnits = nullInt64Pointer(delta)
		item.AfterUnits = nullInt64Pointer(after)
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open-world runtime effects: %w", err)
	}
	return items, nil
}

func loadCityOpenWorldRuleCases(
	ctx context.Context, queryer citySQLQueryer, worldID int64, tick *int64,
) ([]CityOpenWorldRuleCase, error) {
	query := `
SELECT rule_case.code, rule_case.tick, rule_case.sequence, source_fact.tick, source_fact.sequence,
       consequence_fact.tick, consequence_fact.sequence, subject.code, rule_case.rule_code,
       rule_case.rule_version, rule_case.rule_hash, rule_case.category_code, rule_case.scope_kind,
       rule_case.scope_code, rule_case.status, rule_case.severity_units, rule_case.decision_code,
       rule_case.created_tick, rule_case.decided_tick, rule_case.closed_tick, rule_case.payload
FROM city_open_world_rule_cases rule_case
JOIN city_open_world_runtime_facts source_fact ON source_fact.id = rule_case.source_fact_id AND source_fact.world_id = rule_case.world_id
LEFT JOIN city_open_world_runtime_facts consequence_fact ON consequence_fact.id = rule_case.consequence_fact_id AND consequence_fact.world_id = rule_case.world_id
JOIN city_open_world_actors subject ON subject.id = rule_case.subject_actor_id AND subject.world_id = rule_case.world_id
WHERE rule_case.world_id = $1`
	args := []any{worldID}
	if tick != nil {
		query += ` AND rule_case.tick = $2`
		args = append(args, *tick)
	}
	query += ` ORDER BY rule_case.tick ASC, rule_case.sequence ASC`
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load open-world rule cases: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldRuleCase, 0)
	for rows.Next() {
		item := CityOpenWorldRuleCase{}
		var consequenceTick, consequenceSequence, decidedTick, closedTick sql.NullInt64
		var decisionCode sql.NullString
		if err = rows.Scan(&item.Code, &item.Tick, &item.Sequence, &item.SourceFact.Tick, &item.SourceFact.Sequence,
			&consequenceTick, &consequenceSequence, &item.SubjectActorCode, &item.RuleCode,
			&item.RuleVersion, &item.RuleHash, &item.CategoryCode, &item.ScopeKind, &item.ScopeCode,
			&item.Status, &item.SeverityUnits, &decisionCode, &item.CreatedTick, &decidedTick, &closedTick,
			&item.Payload); err != nil {
			return nil, err
		}
		if consequenceTick.Valid {
			item.ConsequenceFact = &CityOpenWorldRuntimeFactRef{Tick: consequenceTick.Int64, Sequence: consequenceSequence.Int64}
		}
		if decisionCode.Valid {
			item.DecisionCode = cityOpenWorldStringPointer(decisionCode.String)
		}
		item.DecidedTick = nullInt64Pointer(decidedTick)
		item.ClosedTick = nullInt64Pointer(closedTick)
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open-world rule cases: %w", err)
	}
	return items, nil
}

func loadCityOpenWorldRuntimeFactsForTick(ctx context.Context, queryer citySQLQueryer, worldID, tick int64) ([]CityOpenWorldRuntimeFact, error) {
	return loadCityOpenWorldRuntimeFacts(ctx, queryer, worldID, &tick)
}

func loadCityOpenWorldRuntimeEffectsForTick(ctx context.Context, queryer citySQLQueryer, worldID, tick int64) ([]CityOpenWorldRuntimeEffect, error) {
	return loadCityOpenWorldRuntimeEffects(ctx, queryer, worldID, &tick)
}

func loadCityOpenWorldRuleCasesForTick(ctx context.Context, queryer citySQLQueryer, worldID, tick int64) ([]CityOpenWorldRuleCase, error) {
	return loadCityOpenWorldRuleCases(ctx, queryer, worldID, &tick)
}

func cityOpenWorldStringPointer(value string) *string { return &value }

func cityOpenWorldUniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	items := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		items = append(items, value)
	}
	sort.Strings(items)
	return items
}
