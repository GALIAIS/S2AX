package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// GetCityOpenWorldSocialRuntime returns the V5 scenario baseline and its live
// social projections in one authorized read. It intentionally does not expose
// a mutable world model: every future mutation still has to enter through a
// command and a fact-backed reducer.
func (s *CityEconomyService) GetCityOpenWorldSocialRuntime(
	ctx context.Context,
	userID, worldID int64,
) (*CityOpenWorldSocialRuntimeState, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, userID, worldID); err != nil {
		return nil, err
	}
	if err := ensureCityOpenWorldV5SocialRuntime(ctx, s.db, worldID); err != nil {
		return nil, err
	}
	return loadCityOpenWorldV5SocialRuntimeState(ctx, s.db, worldID)
}

func (s *CityEconomyService) ListCityOpenWorldFacilities(
	ctx context.Context,
	userID, worldID int64,
) ([]CityOpenWorldFacility, error) {
	state, err := s.GetCityOpenWorldSocialRuntime(ctx, userID, worldID)
	if err != nil {
		return nil, err
	}
	return state.Facilities, nil
}

func (s *CityEconomyService) ListCityOpenWorldNPCProfiles(
	ctx context.Context,
	userID, worldID int64,
) ([]CityOpenWorldNPCProfile, error) {
	state, err := s.GetCityOpenWorldSocialRuntime(ctx, userID, worldID)
	if err != nil {
		return nil, err
	}
	return state.NPCProfiles, nil
}

func (s *CityEconomyService) ListCityOpenWorldNavigationIntents(
	ctx context.Context,
	userID, worldID int64,
) ([]CityOpenWorldNavigationIntent, error) {
	state, err := s.GetCityOpenWorldSocialRuntime(ctx, userID, worldID)
	if err != nil {
		return nil, err
	}
	return state.NavigationIntents, nil
}

func ensureCityOpenWorldV5SocialRuntime(ctx context.Context, queryer citySQLQueryer, worldID int64) error {
	var runtimeID string
	err := queryer.QueryRowContext(ctx, `
SELECT runtime_id FROM city_open_world_runtime_profiles WHERE world_id = $1`, worldID).Scan(&runtimeID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCityOpenWorldRuntimeNotFound
	}
	if err != nil {
		return fmt.Errorf("load open-world social runtime identity: %w", err)
	}
	if runtimeID != cityOpenWorldSocialRuntimeID {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": runtimeID})
	}
	return nil
}

func loadCityOpenWorldV5SocialRuntimeState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*CityOpenWorldSocialRuntimeState, error) {
	state := &CityOpenWorldSocialRuntimeState{
		Facilities:        make([]CityOpenWorldFacility, 0),
		NPCProfiles:       make([]CityOpenWorldNPCProfile, 0),
		NavigationIntents: make([]CityOpenWorldNavigationIntent, 0),
	}
	if err := queryer.QueryRowContext(ctx, `
SELECT scenario_id, scenario_version, scenario_hash, profile_id, profile_version, profile_hash, metadata
FROM city_open_world_scenario_bindings
WHERE world_id = $1`, worldID).Scan(
		&state.Scenario.ScenarioID, &state.Scenario.ScenarioVersion, &state.Scenario.ScenarioHash,
		&state.Scenario.ProfileID, &state.Scenario.ProfileVersion, &state.Scenario.ProfileHash,
		&state.Scenario.Metadata,
	); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v5_scenario_binding"})
	} else if err != nil {
		return nil, fmt.Errorf("load open-world V5 scenario binding: %w", err)
	}
	facilities, err := loadCityOpenWorldV5Facilities(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	state.Facilities = facilities
	npcs, err := loadCityOpenWorldV5NPCProfiles(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	state.NPCProfiles = npcs
	intents, err := loadCityOpenWorldV5NavigationIntents(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	state.NavigationIntents = intents
	return state, nil
}

func loadCityOpenWorldV5Facilities(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) ([]CityOpenWorldFacility, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT facility.code, facility.building_code, facility.facility_type_code, facility.state,
       facility.capacity_units, facility.anchor_x, facility.anchor_y, facility.anchor_z,
       facility.last_settled_tick, source_fact.tick, source_fact.sequence,
       facility.version, facility.metadata
FROM city_open_world_facilities facility
LEFT JOIN city_open_world_runtime_facts source_fact
  ON source_fact.id = facility.source_fact_id AND source_fact.world_id = facility.world_id
WHERE facility.world_id = $1
ORDER BY facility.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load open-world V5 facilities: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldFacility, 0)
	for rows.Next() {
		item := CityOpenWorldFacility{}
		var factTick, factSequence sql.NullInt64
		if err = rows.Scan(
			&item.Code, &item.BuildingCode, &item.FacilityTypeCode, &item.State,
			&item.CapacityUnits, &item.AnchorX, &item.AnchorY, &item.AnchorZ,
			&item.LastSettledTick, &factTick, &factSequence, &item.Version, &item.Metadata,
		); err != nil {
			return nil, fmt.Errorf("scan open-world V5 facility: %w", err)
		}
		if factTick.Valid {
			item.SourceFact = &CityOpenWorldRuntimeFactRef{Tick: factTick.Int64, Sequence: factSequence.Int64}
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open-world V5 facilities: %w", err)
	}
	return items, nil
}

func loadCityOpenWorldV5NPCProfiles(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) ([]CityOpenWorldNPCProfile, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT actor.code, profile.behavior_code, profile.behavior_version, profile.behavior_hash,
       home.code, work.code, profile.lod_tier, profile.schedule_offset,
       profile.next_action_tick, profile.last_action_tick, profile.behavior_state, profile.version
FROM city_open_world_npc_profiles profile
JOIN city_open_world_actors actor
  ON actor.id = profile.actor_id AND actor.world_id = profile.world_id
LEFT JOIN city_open_world_facilities home
  ON home.id = profile.home_facility_id AND home.world_id = profile.world_id
LEFT JOIN city_open_world_facilities work
  ON work.id = profile.work_facility_id AND work.world_id = profile.world_id
WHERE profile.world_id = $1
ORDER BY actor.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load open-world V5 NPC profiles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldNPCProfile, 0)
	for rows.Next() {
		item := CityOpenWorldNPCProfile{}
		var home, work sql.NullString
		if err = rows.Scan(
			&item.ActorCode, &item.BehaviorCode, &item.BehaviorVersion, &item.BehaviorHash,
			&home, &work, &item.LODTier, &item.ScheduleOffset,
			&item.NextActionTick, &item.LastActionTick, &item.BehaviorState, &item.Version,
		); err != nil {
			return nil, fmt.Errorf("scan open-world V5 NPC profile: %w", err)
		}
		if home.Valid {
			item.HomeFacilityCode = cityOpenWorldStringPointer(home.String)
		}
		if work.Valid {
			item.WorkFacilityCode = cityOpenWorldStringPointer(work.String)
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open-world V5 NPC profiles: %w", err)
	}
	return items, nil
}

func loadCityOpenWorldV5NavigationIntents(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) ([]CityOpenWorldNavigationIntent, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT actor.code, intent.intent_code, intent.target_space_kind, intent.target_location_scope,
       intent.target_building_code, intent.target_floor_index, intent.target_x, intent.target_y,
       intent.target_z, intent.status, intent.priority, intent.maximum_steps, intent.completed_steps,
       intent.blocked_attempts, intent.next_attempt_tick, intent.created_tick, intent.updated_tick,
       source_fact.tick, source_fact.sequence, intent.version, intent.metadata
FROM city_open_world_actor_navigation_intents intent
JOIN city_open_world_actors actor
  ON actor.id = intent.actor_id AND actor.world_id = intent.world_id
JOIN city_open_world_runtime_facts source_fact
  ON source_fact.id = intent.source_fact_id AND source_fact.world_id = intent.world_id
WHERE intent.world_id = $1
ORDER BY actor.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load open-world V5 navigation intents: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldNavigationIntent, 0)
	for rows.Next() {
		item := CityOpenWorldNavigationIntent{}
		var buildingCode sql.NullString
		if err = rows.Scan(
			&item.ActorCode, &item.IntentCode, &item.TargetSpaceKind, &item.TargetLocationScope,
			&buildingCode, &item.TargetFloorIndex, &item.TargetX, &item.TargetY, &item.TargetZ,
			&item.Status, &item.Priority, &item.MaximumSteps, &item.CompletedSteps,
			&item.BlockedAttempts, &item.NextAttemptTick, &item.CreatedTick, &item.UpdatedTick,
			&item.SourceFact.Tick, &item.SourceFact.Sequence, &item.Version, &item.Metadata,
		); err != nil {
			return nil, fmt.Errorf("scan open-world V5 navigation intent: %w", err)
		}
		if buildingCode.Valid {
			item.TargetBuildingCode = cityOpenWorldStringPointer(buildingCode.String)
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open-world V5 navigation intents: %w", err)
	}
	return items, nil
}
