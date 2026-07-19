package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
)

const (
	cityOpenWorldV5ScenarioVersion       = "1.0.0"
	cityOpenWorldV5NPCBehaviorVersion    = "1.0.0"
	cityOpenWorldV5MaximumBootstrapNPCs  = 16
	cityOpenWorldV5FacilityMetadataShape = 1
)

type cityOpenWorldV5FacilityRule struct {
	PrimaryUse       cityspatial.LandUse `json:"primary_use"`
	LayoutStyles     []string            `json:"layout_styles,omitempty"`
	FacilityType     string              `json:"facility_type"`
	CapacityPerFloor int64               `json:"capacity_per_floor"`
	NPCArchetype     string              `json:"npc_archetype"`
	BehaviorCode     string              `json:"behavior_code"`
}

type cityOpenWorldV5Scenario struct {
	ID            string                        `json:"id"`
	Version       string                        `json:"version"`
	ProfileID     string                        `json:"profile_id"`
	NPCBudget     int                           `json:"npc_budget"`
	FacilityRules []cityOpenWorldV5FacilityRule `json:"facility_rules"`
	ScenarioHash  string                        `json:"scenario_hash,omitempty"`
}

type cityOpenWorldV5FacilitySeed struct {
	ID               int64
	Code             string
	BuildingCode     string
	FacilityTypeCode string
	CapacityUnits    int64
	AnchorX          int64
	AnchorY          int64
	AnchorZ          int32
	PrimaryUse       cityspatial.LandUse
	LayoutStyle      string
	Rule             cityOpenWorldV5FacilityRule
	Metadata         json.RawMessage
}

// CityOpenWorldScenarioBinding seals the scenario catalogue selected for a
// world. It is immutable after genesis; country-specific composition lives in
// this data binding rather than in runtime branching.
type CityOpenWorldScenarioBinding struct {
	ScenarioID      string          `json:"scenario_id"`
	ScenarioVersion string          `json:"scenario_version"`
	ScenarioHash    string          `json:"scenario_hash"`
	ProfileID       string          `json:"profile_id"`
	ProfileVersion  string          `json:"profile_version"`
	ProfileHash     string          `json:"profile_hash"`
	Metadata        json.RawMessage `json:"metadata"`
}

// CityOpenWorldFacility is the durable operational projection for one static
// building. A facility changes through fact-backed reducers; the building
// topology itself remains immutable V3 data.
type CityOpenWorldFacility struct {
	Code             string                       `json:"code"`
	BuildingCode     string                       `json:"building_code"`
	FacilityTypeCode string                       `json:"facility_type_code"`
	State            string                       `json:"state"`
	CapacityUnits    int64                        `json:"capacity_units"`
	AnchorX          int64                        `json:"anchor_x"`
	AnchorY          int64                        `json:"anchor_y"`
	AnchorZ          int32                        `json:"anchor_z"`
	LastSettledTick  int64                        `json:"last_settled_tick"`
	SourceFact       *CityOpenWorldRuntimeFactRef `json:"source_fact,omitempty"`
	Version          int64                        `json:"version"`
	Metadata         json.RawMessage              `json:"metadata"`
}

// CityOpenWorldNPCProfile is intentionally separate from the Actor core so
// player-controlled characters never inherit autonomous scheduling fields.
type CityOpenWorldNPCProfile struct {
	ActorCode        string          `json:"actor_code"`
	BehaviorCode     string          `json:"behavior_code"`
	BehaviorVersion  string          `json:"behavior_version"`
	BehaviorHash     string          `json:"behavior_hash"`
	HomeFacilityCode *string         `json:"home_facility_code,omitempty"`
	WorkFacilityCode *string         `json:"work_facility_code,omitempty"`
	LODTier          string          `json:"lod_tier"`
	ScheduleOffset   int             `json:"schedule_offset"`
	NextActionTick   int64           `json:"next_action_tick"`
	LastActionTick   int64           `json:"last_action_tick"`
	BehaviorState    json.RawMessage `json:"behavior_state"`
	Version          int64           `json:"version"`
}

// CityOpenWorldNavigationIntent is a fact-backed goal. Position updates are
// still generated one deterministic step at a time by the V5 automatic loop.
type CityOpenWorldNavigationIntent struct {
	ActorCode           string                      `json:"actor_code"`
	IntentCode          string                      `json:"intent_code"`
	TargetSpaceKind     string                      `json:"target_space_kind"`
	TargetLocationScope string                      `json:"target_location_scope"`
	TargetBuildingCode  *string                     `json:"target_building_code,omitempty"`
	TargetFloorIndex    int32                       `json:"target_floor_index"`
	TargetX             int64                       `json:"target_x"`
	TargetY             int64                       `json:"target_y"`
	TargetZ             int32                       `json:"target_z"`
	Status              string                      `json:"status"`
	Priority            int                         `json:"priority"`
	MaximumSteps        int                         `json:"maximum_steps"`
	CompletedSteps      int                         `json:"completed_steps"`
	BlockedAttempts     int                         `json:"blocked_attempts"`
	NextAttemptTick     int64                       `json:"next_attempt_tick"`
	CreatedTick         int64                       `json:"created_tick"`
	UpdatedTick         int64                       `json:"updated_tick"`
	SourceFact          CityOpenWorldRuntimeFactRef `json:"source_fact"`
	Version             int64                       `json:"version"`
	Metadata            json.RawMessage             `json:"metadata"`
}

// CityOpenWorldSocialRuntimeState is included in V5 canonical snapshots.
// Empty slices are retained deliberately so replay verification can
// distinguish a missing projection from an empty, valid projection.
type CityOpenWorldSocialRuntimeState struct {
	Scenario          CityOpenWorldScenarioBinding    `json:"scenario"`
	Facilities        []CityOpenWorldFacility         `json:"facilities"`
	NPCProfiles       []CityOpenWorldNPCProfile       `json:"npc_profiles"`
	NavigationIntents []CityOpenWorldNavigationIntent `json:"navigation_intents"`
}

// builtInCityOpenWorldSocialRuntimeDefinitions extends the generic Actor
// contract rather than creating country-specific code paths.  The scenario
// binding selects facility composition and NPC mixes; these definitions only
// describe reusable roles, archetypes and activity mechanics.
func builtInCityOpenWorldSocialRuntimeDefinitions() ([]CityOpenWorldRuntimeDefinition, error) {
	always := WorldRequirementNode{Operator: WorldRequirementAll, Items: []WorldRequirementNode{}}
	seeds := []worldRuntimeDefinitionSeed{
		{WorldRuntimeDefinitionStatus, "civic_probation", "public", worldRuntimeStatusDefinition{
			NameKey: "openWorld.statuses.civicProbation", DescriptionKey: "openWorld.statuses.civicProbationDescription", MaximumStacks: 1,
		}},
		{WorldRuntimeDefinitionStatus, "access_restricted", "public", worldRuntimeStatusDefinition{
			NameKey: "openWorld.statuses.accessRestricted", DescriptionKey: "openWorld.statuses.accessRestrictedDescription", MaximumStacks: 8,
		}},
		{WorldRuntimeDefinitionRole, "employment.service", "public", worldRuntimeRoleDefinition{
			NameKey: "openWorld.roles.serviceWorker", DescriptionKey: "openWorld.roles.serviceWorkerDescription",
			CategoryCode: "employment", Requirements: always,
		}},
		{WorldRuntimeDefinitionRole, "employment.industry", "public", worldRuntimeRoleDefinition{
			NameKey: "openWorld.roles.industryWorker", DescriptionKey: "openWorld.roles.industryWorkerDescription",
			CategoryCode: "employment", Requirements: always,
		}},
		{WorldRuntimeDefinitionRole, "employment.civic", "public", worldRuntimeRoleDefinition{
			NameKey: "openWorld.roles.civicWorker", DescriptionKey: "openWorld.roles.civicWorkerDescription",
			CategoryCode: "employment", Requirements: always,
		}},
		{WorldRuntimeDefinitionArchetype, "npc.resident", "public", worldRuntimeArchetypeDefinition{
			NameKey: "openWorld.archetypes.npcResident", DescriptionKey: "openWorld.archetypes.npcResidentDescription",
			ActorTypeCode: "npc", InitialAttributes: map[string]int64{
				"vitality": 48000, "reasoning": 47000, "coordination": 47000, "communication": 48000, "discipline": 47000,
			}, InitialRoles: []string{"identity.resident"},
		}},
		{WorldRuntimeDefinitionArchetype, "npc.service_worker", "public", worldRuntimeArchetypeDefinition{
			NameKey: "openWorld.archetypes.npcServiceWorker", DescriptionKey: "openWorld.archetypes.npcServiceWorkerDescription",
			ActorTypeCode: "npc", InitialAttributes: map[string]int64{
				"vitality": 47000, "reasoning": 49000, "coordination": 50000, "communication": 58000, "discipline": 52000,
			}, InitialRoles: []string{"identity.resident", "employment.service"},
		}},
		{WorldRuntimeDefinitionArchetype, "npc.industrial_worker", "public", worldRuntimeArchetypeDefinition{
			NameKey: "openWorld.archetypes.npcIndustrialWorker", DescriptionKey: "openWorld.archetypes.npcIndustrialWorkerDescription",
			ActorTypeCode: "npc", InitialAttributes: map[string]int64{
				"vitality": 57000, "reasoning": 46000, "coordination": 54000, "communication": 43000, "discipline": 54000,
			}, InitialRoles: []string{"identity.resident", "employment.industry"},
		}},
		{WorldRuntimeDefinitionArchetype, "npc.civic_worker", "public", worldRuntimeArchetypeDefinition{
			NameKey: "openWorld.archetypes.npcCivicWorker", DescriptionKey: "openWorld.archetypes.npcCivicWorkerDescription",
			ActorTypeCode: "npc", InitialAttributes: map[string]int64{
				"vitality": 50000, "reasoning": 53000, "coordination": 50000, "communication": 56000, "discipline": 57000,
			}, InitialRoles: []string{"identity.resident", "employment.civic"},
		}},
	}
	definitions := make([]CityOpenWorldRuntimeDefinition, 0, len(seeds))
	for _, seed := range seeds {
		raw, err := json.Marshal(seed.payload)
		if err != nil {
			return nil, fmt.Errorf("marshal social runtime definition %s/%s: %w", seed.kind, seed.code, err)
		}
		sum := sha256.Sum256(raw)
		definition := CityOpenWorldRuntimeDefinition{
			Kind: seed.kind, Code: seed.code, Version: cityOpenWorldRuntimeCatalogVersion,
			Hash: hex.EncodeToString(sum[:]), Visibility: seed.visibility, Payload: raw,
		}
		if err := validateWorldRuntimeDefinition(definition); err != nil {
			return nil, fmt.Errorf("validate social runtime definition %s/%s: %w", seed.kind, seed.code, err)
		}
		definitions = append(definitions, definition)
	}
	return definitions, nil
}

func cityOpenWorldV5ScenarioForProfile(profileID string) (cityOpenWorldV5Scenario, error) {
	base := cityOpenWorldV5Scenario{
		Version:   cityOpenWorldV5ScenarioVersion,
		ProfileID: profileID,
		NPCBudget: 12,
		FacilityRules: []cityOpenWorldV5FacilityRule{
			{PrimaryUse: cityspatial.LandUseResidential, FacilityType: "residence", CapacityPerFloor: 4, NPCArchetype: "npc.resident", BehaviorCode: "npc.routine.resident"},
			{PrimaryUse: cityspatial.LandUseCommercial, FacilityType: "commerce", CapacityPerFloor: 8, NPCArchetype: "npc.service_worker", BehaviorCode: "npc.routine.service"},
			{PrimaryUse: cityspatial.LandUseIndustrial, FacilityType: "industry", CapacityPerFloor: 10, NPCArchetype: "npc.industrial_worker", BehaviorCode: "npc.routine.industry"},
		},
	}
	switch profileID {
	case cityspatial.WorldgenProfileJapanMetropolitan:
		base.ID = "scenario.jp.metropolitan"
		base.NPCBudget = 14
		base.FacilityRules = append([]cityOpenWorldV5FacilityRule{
			{PrimaryUse: cityspatial.LandUseCommercial, LayoutStyles: []string{"arcade", "shopfront"}, FacilityType: "commerce", CapacityPerFloor: 9, NPCArchetype: "npc.service_worker", BehaviorCode: "npc.routine.service"},
		}, base.FacilityRules...)
	case cityspatial.WorldgenProfileChinaMetropolitan:
		base.ID = "scenario.cn.metropolitan"
		base.NPCBudget = 16
		base.FacilityRules = append([]cityOpenWorldV5FacilityRule{
			{PrimaryUse: cityspatial.LandUseCommercial, LayoutStyles: []string{"tower", "arcade"}, FacilityType: "commerce", CapacityPerFloor: 12, NPCArchetype: "npc.service_worker", BehaviorCode: "npc.routine.service"},
			{PrimaryUse: cityspatial.LandUseResidential, LayoutStyles: []string{"tower", "courtyard"}, FacilityType: "residence", CapacityPerFloor: 6, NPCArchetype: "npc.resident", BehaviorCode: "npc.routine.resident"},
		}, base.FacilityRules...)
	case cityspatial.DefaultWorldgenProfileID:
		base.ID = "scenario.temperate.openworld"
	default:
		return cityOpenWorldV5Scenario{}, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "open_world_scenario_profile"})
	}
	canonical := struct {
		ID            string                        `json:"id"`
		Version       string                        `json:"version"`
		ProfileID     string                        `json:"profile_id"`
		NPCBudget     int                           `json:"npc_budget"`
		FacilityRules []cityOpenWorldV5FacilityRule `json:"facility_rules"`
	}{base.ID, base.Version, base.ProfileID, base.NPCBudget, base.FacilityRules}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return cityOpenWorldV5Scenario{}, fmt.Errorf("marshal open-world V5 scenario: %w", err)
	}
	sum := sha256.Sum256(raw)
	base.ScenarioHash = hex.EncodeToString(sum[:])
	return base, nil
}

func initializeCityOpenWorldV5SocialFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	binding, err := loadCityOpenWorldBinding(ctx, tx, worldID)
	if err != nil {
		return err
	}
	scenario, err := cityOpenWorldV5ScenarioForProfile(binding.ProfileID)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_scenario_bindings
    (world_id, scenario_id, scenario_version, scenario_hash, profile_id, profile_version, profile_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
		worldID, scenario.ID, scenario.Version, scenario.ScenarioHash,
		binding.ProfileID, binding.ProfileVersion, binding.ProfileHash,
		[]byte(`{"schema_version":1,"worldgen_contract":"1.5.0","npc_lod_contract":"1.0.0"}`),
	); err != nil {
		return fmt.Errorf("insert V5 open-world scenario binding: %w", err)
	}
	facilities, err := initializeCityOpenWorldV5Facilities(ctx, tx, worldID, scenario)
	if err != nil {
		return err
	}
	if err = initializeCityOpenWorldV5NPCs(ctx, tx, worldID, binding, scenario, facilities); err != nil {
		return err
	}
	return nil
}

func initializeCityOpenWorldV5Facilities(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	scenario cityOpenWorldV5Scenario,
) ([]cityOpenWorldV5FacilitySeed, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT code, primary_use, layout_style, floor_count, entrance_x, entrance_y, entrance_z
FROM city_open_world_buildings
WHERE world_id = $1
ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load V5 facility buildings: %w", err)
	}
	items := make([]cityOpenWorldV5FacilitySeed, 0)
	for rows.Next() {
		var buildingCode, primaryUse, layoutStyle string
		var floorCount int32
		var anchorX, anchorY int64
		var anchorZ int32
		if err = rows.Scan(&buildingCode, &primaryUse, &layoutStyle, &floorCount, &anchorX, &anchorY, &anchorZ); err != nil {
			return nil, fmt.Errorf("scan V5 facility building: %w", err)
		}
		rule, found := cityOpenWorldV5FacilityRuleForBuilding(scenario, cityspatial.LandUse(primaryUse), layoutStyle)
		if !found || floorCount <= 0 {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v5_facility_rule"})
		}
		capacity := int64(floorCount) * rule.CapacityPerFloor
		if capacity < 1 || capacity > 1_000_000 {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v5_facility_capacity"})
		}
		code := "facility." + buildingCode
		metadata, metadataErr := json.Marshal(map[string]any{
			"schema_version": cityOpenWorldV5FacilityMetadataShape,
			"primary_use":    primaryUse,
			"layout_style":   layoutStyle,
			"scenario_id":    scenario.ID,
		})
		if metadataErr != nil {
			return nil, metadataErr
		}
		item := cityOpenWorldV5FacilitySeed{
			Code: code, BuildingCode: buildingCode, FacilityTypeCode: rule.FacilityType,
			CapacityUnits: capacity, AnchorX: anchorX, AnchorY: anchorY, AnchorZ: anchorZ,
			PrimaryUse: cityspatial.LandUse(primaryUse), LayoutStyle: layoutStyle, Rule: rule, Metadata: metadata,
		}
		items = append(items, item)
	}
	if err = closeCityRows(rows, "iterate V5 facility buildings"); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v5_facilities"})
	}
	for index := range items {
		item := &items[index]
		if err = tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_facilities
    (world_id, code, building_code, facility_type_code, state, capacity_units,
     anchor_x, anchor_y, anchor_z, last_settled_tick, version, metadata)
VALUES ($1, $2, $3, $4, 'active', $5, $6, $7, $8, 0, 1, $9::jsonb)
RETURNING id`, worldID, item.Code, item.BuildingCode, item.FacilityTypeCode,
			item.CapacityUnits, item.AnchorX, item.AnchorY, item.AnchorZ, item.Metadata).Scan(&item.ID); err != nil {
			return nil, fmt.Errorf("insert V5 facility %s: %w", item.Code, err)
		}
	}
	return items, nil
}

func cityOpenWorldV5FacilityRuleForBuilding(
	scenario cityOpenWorldV5Scenario,
	primaryUse cityspatial.LandUse,
	layoutStyle string,
) (cityOpenWorldV5FacilityRule, bool) {
	for _, rule := range scenario.FacilityRules {
		if rule.PrimaryUse != primaryUse || len(rule.LayoutStyles) == 0 {
			continue
		}
		for _, style := range rule.LayoutStyles {
			if style == layoutStyle {
				return rule, true
			}
		}
	}
	for _, rule := range scenario.FacilityRules {
		if rule.PrimaryUse == primaryUse && len(rule.LayoutStyles) == 0 {
			return rule, true
		}
	}
	return cityOpenWorldV5FacilityRule{}, false
}

func initializeCityOpenWorldV5NPCs(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	binding *CityOpenWorldBinding,
	scenario cityOpenWorldV5Scenario,
	facilities []cityOpenWorldV5FacilitySeed,
) error {
	if binding == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v5_binding"})
	}
	limit := scenario.NPCBudget
	if limit > cityOpenWorldV5MaximumBootstrapNPCs {
		limit = cityOpenWorldV5MaximumBootstrapNPCs
	}
	if limit < 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v5_npc_budget"})
	}
	ordered := append([]cityOpenWorldV5FacilitySeed(nil), facilities...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Code < ordered[j].Code })
	created := 0
	for index := 0; index < limit; index++ {
		facility := ordered[index%len(ordered)]
		archetype, err := loadCityOpenWorldRuntimeDefinition(ctx, tx, worldID, WorldRuntimeDefinitionArchetype, facility.Rule.NPCArchetype)
		if err != nil {
			return err
		}
		archetypePayload, err := decodeWorldRuntimeDefinition[worldRuntimeArchetypeDefinition](archetype)
		if err != nil || archetypePayload.ActorTypeCode != "npc" {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v5_npc_archetype"}).WithCause(err)
		}
		actorCode := "npc." + strconv.FormatInt(int64(index+1), 10)
		metadata, err := json.Marshal(map[string]any{
			"origin": "genesis", "scenario_id": scenario.ID, "behavior_code": facility.Rule.BehaviorCode,
		})
		if err != nil {
			return err
		}
		var actorID int64
		if err = tx.QueryRowContext(ctx, `
INSERT INTO city_open_world_actors
    (world_id, code, owner_user_id, actor_type_code, name, status,
     archetype_code, archetype_version, created_tick, updated_tick, version, metadata)
VALUES ($1, $2, NULL, 'npc', $3, 'active', $4, $5, 0, 0, 1, $6::jsonb)
RETURNING id`, worldID, actorCode, "NPC "+strconv.FormatInt(int64(index+1), 10),
			archetype.Code, archetype.Version, metadata).Scan(&actorID); err != nil {
			return fmt.Errorf("insert V5 NPC %s: %w", actorCode, err)
		}
		if err = insertCityOpenWorldV5GenesisActorTraits(ctx, tx, worldID, actorID, archetypePayload); err != nil {
			return err
		}
		location, locationErr := findCityOpenWorldRuntimeNearbySpawnLocation(
			ctx, tx, worldID, actorID, actorCode, facility.AnchorX, facility.AnchorY, 24,
		)
		if locationErr != nil {
			return fmt.Errorf("place V5 NPC %s: %w", actorCode, locationErr)
		}
		if err = insertCityOpenWorldV5GenesisActorLocation(ctx, tx, worldID, actorID, location); err != nil {
			return err
		}
		behaviorHash := cityOpenWorldV5BehaviorHash(facility.Rule.BehaviorCode, scenario.ScenarioHash)
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_npc_profiles
    (world_id, actor_id, behavior_code, behavior_version, behavior_hash,
     home_facility_id, work_facility_id, lod_tier, schedule_offset,
     next_action_tick, last_action_tick, behavior_state, version)
VALUES ($1, $2, $3, $4, $5, NULL, $6, 'active', $7, 1, 0, $8::jsonb, 1)`,
			worldID, actorID, facility.Rule.BehaviorCode, cityOpenWorldV5NPCBehaviorVersion, behaviorHash,
			facility.ID, index%168, []byte(`{"schema_version":1,"mode":"routine"}`),
		); err != nil {
			return fmt.Errorf("insert V5 NPC profile %s: %w", actorCode, err)
		}
		created++
	}
	if created == 0 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_v5_npcs"})
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE city_open_world_runtime_profiles
SET actor_count = actor_count + $2, updated_at = NOW()
WHERE world_id = $1`, worldID, created); err != nil {
		return fmt.Errorf("update V5 runtime actor count: %w", err)
	}
	return nil
}

func insertCityOpenWorldV5GenesisActorTraits(
	ctx context.Context,
	tx *sql.Tx,
	worldID, actorID int64,
	archetype worldRuntimeArchetypeDefinition,
) error {
	attributeCodes := make([]string, 0, len(archetype.InitialAttributes))
	for code := range archetype.InitialAttributes {
		attributeCodes = append(attributeCodes, code)
	}
	sort.Strings(attributeCodes)
	for _, code := range attributeCodes {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_open_world_actor_attributes
    (world_id, actor_id, attribute_code, value_units, experience_units, last_changed_tick, version, metadata)
VALUES ($1, $2, $3, $4, 0, 0, 1, '{}'::jsonb)`, worldID, actorID, code, archetype.InitialAttributes[code]); err != nil {
			return fmt.Errorf("insert V5 NPC attribute %s: %w", code, err)
		}
	}
	roleCodes := append([]string(nil), archetype.InitialRoles...)
	sort.Strings(roleCodes)
	for _, code := range roleCodes {
		definition, err := loadCityOpenWorldRuntimeDefinition(ctx, tx, worldID, WorldRuntimeDefinitionRole, code)
		if err != nil {
			return err
		}
		role, err := decodeWorldRuntimeDefinition[worldRuntimeRoleDefinition](definition)
		if err != nil {
			return ErrCitySimulationInvariant.WithCause(err)
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_open_world_actor_roles
    (world_id, actor_id, role_code, category_code, status, granted_tick, version, metadata)
VALUES ($1, $2, $3, $4, 'active', 0, 1, '{}'::jsonb)`, worldID, actorID, code, role.CategoryCode); err != nil {
			return fmt.Errorf("insert V5 NPC role %s: %w", code, err)
		}
	}
	return nil
}

func cityOpenWorldV5BehaviorHash(behaviorCode, scenarioHash string) string {
	sum := sha256.Sum256([]byte(behaviorCode + "\x00" + scenarioHash + "\x00" + cityOpenWorldV5NPCBehaviorVersion))
	return hex.EncodeToString(sum[:])
}
