//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// TestCityOpenWorldV5SocialGenesis verifies the first social-world contract
// against PostgreSQL. It proves that V5 persists a scenario, facility graph,
// autonomous NPC baseline and canonical state without falling back to F7
// spatial tables.
func TestCityOpenWorldV5SocialGenesis(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v5-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(9510001)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V5 Social", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV5,
		StyleProfileID:    "jp.metropolitan",
		SpawnPolicy:       "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	catalog, err := cityService.GetCityOpenWorldRuntimeCatalog(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "sub2api-city-open-world-social-runtime", catalog.Profile.RuntimeID)
	require.Equal(t, "2.0.0", catalog.Profile.RuntimeVersion)
	require.Equal(t, "2.0.0", catalog.Profile.CatalogVersion)
	definitionCodes := make(map[string]struct{}, len(catalog.Definitions))
	for _, definition := range catalog.Definitions {
		definitionCodes[definition.Code] = struct{}{}
	}
	for _, code := range []string{"npc", "npc.resident", "npc.service_worker", "employment.service"} {
		_, found := definitionCodes[code]
		require.True(t, found, code)
	}

	social, err := cityService.GetCityOpenWorldSocialRuntime(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "scenario.jp.metropolitan", social.Scenario.ScenarioID)
	require.NotEmpty(t, social.Scenario.ScenarioHash)
	require.NotEmpty(t, social.Facilities)
	require.Len(t, social.NPCProfiles, 14)
	require.Empty(t, social.NavigationIntents)

	seenNPCs := make(map[string]struct{}, len(social.NPCProfiles))
	for _, profile := range social.NPCProfiles {
		require.NotEmpty(t, profile.ActorCode)
		require.NotEmpty(t, profile.BehaviorHash)
		require.Equal(t, "active", profile.LODTier)
		_, duplicate := seenNPCs[profile.ActorCode]
		require.False(t, duplicate, profile.ActorCode)
		seenNPCs[profile.ActorCode] = struct{}{}
	}
	var actorCount, locationCount, duplicateLocationCount int
	err = integrationDB.QueryRowContext(ctx, `
SELECT
    (SELECT COUNT(*) FROM city_open_world_actors WHERE world_id = $1 AND actor_type_code = 'npc'),
    (SELECT COUNT(*) FROM city_open_world_actor_locations WHERE world_id = $1),
    (SELECT COUNT(*) FROM (
        SELECT space_kind, location_scope, floor_index, x, y, z
        FROM city_open_world_actor_locations
        WHERE world_id = $1
        GROUP BY space_kind, location_scope, floor_index, x, y, z
        HAVING COUNT(*) > 1
    ) duplicates)`, worldID).Scan(&actorCount, &locationCount, &duplicateLocationCount)
	require.NoError(t, err)
	require.Equal(t, 14, actorCount)
	require.Equal(t, actorCount, locationCount)
	require.Zero(t, duplicateLocationCount)

	verification, err := cityService.VerifyOpenWorldMaterialization(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionOpenWorldV5, verification.SimulationVersion)
	require.True(t, verification.CanonicalStateVerified)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_open_world_facilities SET state = 'closed' WHERE world_id = $1`, worldID)
	require.Error(t, err)
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_open_world_npc_profiles SET lod_tier = 'dormant' WHERE world_id = $1`, worldID)
	require.Error(t, err)
}

// TestCityOpenWorldV5NavigationIntentLifecycle proves the V5 navigation
// contract stays command/fact driven: a replacement can be cancelled before
// the automatic loop sees it, while a stable target is completed by that loop
// without a client-side position write.
func TestCityOpenWorldV5NavigationIntentLifecycle(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v5-navigation-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(9520001)
	foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Open World V5 Navigation", Seed: &seed,
		SimulationVersion: service.CitySimulationVersionOpenWorldV5,
		StyleProfileID:    "cn.metropolitan",
		SpawnPolicy:       "city_center",
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	participant := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("open-world-v5-navigation-player-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	_, err = cityService.AddWorldMember(ctx, service.CityMemberAddInput{
		UserID: owner.ID, WorldID: worldID, Identity: participant.Email, Role: service.CityMemberRolePlanner,
	})
	require.NoError(t, err)

	marshalPayload := func(value any) json.RawMessage {
		raw, marshalErr := json.Marshal(value)
		require.NoError(t, marshalErr)
		return raw
	}
	submit := func(expectedTick int64, key, commandType string, payload json.RawMessage) {
		_, submitErr := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
			UserID: participant.ID, WorldID: worldID, IdempotencyKey: key,
			CommandType: commandType, Payload: payload, ExpectedWorldTick: &expectedTick,
		})
		require.NoError(t, submitErr)
	}
	step := func(expectedTick int64, key string, expectedCommands int) *service.CityStepResult {
		result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
			UserID: owner.ID, WorldID: worldID, IdempotencyKey: key, ExpectedWorldTick: &expectedTick,
		})
		require.NoError(t, stepErr)
		require.Len(t, result.Commands, expectedCommands)
		for _, command := range result.Commands {
			require.Equal(t, service.CityCommandStatusApplied, command.Status, "%+v", command)
		}
		return result
	}

	submit(0, "v5-create-character", service.CityCommandTypeOpenWorldActorCreate, marshalPayload(map[string]any{
		"archetype_code": "urban_apprentice", "name": "Navigator",
	}))
	created := step(0, "v5-create-character-step", 1)
	require.Len(t, created.OpenWorldRuntimeFacts, 1)
	require.Equal(t, service.CityOpenWorldRuntimeFactActorCreated, created.OpenWorldRuntimeFacts[0].FactType)
	actors, err := cityService.ListCityOpenWorldActors(ctx, participant.ID, worldID)
	require.NoError(t, err)
	require.Len(t, actors, 1)
	require.NotNil(t, actors[0].Location)
	actorCode := actors[0].Code
	location := *actors[0].Location

	navigationPayload := cityOpenWorldV5NavigationPayload(actorCode, location)

	// Cancellation runs after the set command during the same tick, so no
	// automatic movement is allowed to race ahead of the explicit cancel.
	submit(1, "v5-navigation-set-cancel", service.CityCommandTypeOpenWorldActorNavigationSet, marshalPayload(navigationPayload))
	submit(1, "v5-navigation-cancel", service.CityCommandTypeOpenWorldActorNavigationCancel, marshalPayload(map[string]any{"actor_code": actorCode}))
	cancelled := step(1, "v5-navigation-cancel-step", 2)
	require.Len(t, cancelled.OpenWorldRuntimeFacts, 2)
	require.Equal(t, service.CityOpenWorldRuntimeFactNavigationCreated, cancelled.OpenWorldRuntimeFacts[0].FactType)
	require.Equal(t, service.CityOpenWorldRuntimeFactNavigationCancelled, cancelled.OpenWorldRuntimeFacts[1].FactType)
	intents, err := cityService.ListCityOpenWorldNavigationIntents(ctx, participant.ID, worldID)
	require.NoError(t, err)
	require.Len(t, intents, 1)
	require.Equal(t, "cancelled", intents[0].Status)
	require.Equal(t, int64(2), intents[0].Version)

	// Select a real unoccupied neighboring cell from the persisted map. This
	// makes the automatic reducer prove an actual one-cell movement rather than
	// merely accepting a same-cell target.
	target := findCityOpenWorldV5AdjacentPassableTarget(t, ctx, cityService, participant.ID, worldID, actorCode, location)
	navigationPayload = cityOpenWorldV5NavigationPayload(actorCode, target)
	submit(2, "v5-navigation-set-arrive", service.CityCommandTypeOpenWorldActorNavigationSet, marshalPayload(navigationPayload))
	arrived := step(2, "v5-navigation-arrive-step", 1)
	require.Len(t, arrived.OpenWorldRuntimeFacts, 2)
	require.Equal(t, service.CityOpenWorldRuntimeFactNavigationReplaced, arrived.OpenWorldRuntimeFacts[0].FactType)
	require.Equal(t, service.CityOpenWorldRuntimeFactNavigationArrived, arrived.OpenWorldRuntimeFacts[1].FactType)
	intents, err = cityService.ListCityOpenWorldNavigationIntents(ctx, participant.ID, worldID)
	require.NoError(t, err)
	require.Len(t, intents, 1)
	require.Equal(t, "arrived", intents[0].Status)
	require.Equal(t, 1, intents[0].CompletedSteps)
	require.Equal(t, int64(4), intents[0].Version)
	state, err := cityService.GetCityOpenWorldActorState(ctx, participant.ID, worldID, actorCode)
	require.NoError(t, err)
	require.NotNil(t, state.Actor.Location)
	require.Equal(t, target.X, state.Actor.Location.X)
	require.Equal(t, target.Y, state.Actor.Location.Y)

	verification, err := cityService.VerifyOpenWorldMaterialization(ctx, participant.ID, worldID)
	require.NoError(t, err)
	require.True(t, verification.CanonicalStateVerified)
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_open_world_actor_navigation_intents
SET status = 'active'
WHERE world_id = $1`, worldID)
	require.Error(t, err)
}

func cityOpenWorldV5NavigationPayload(actorCode string, location service.CityOpenWorldActorLocation) map[string]any {
	payload := map[string]any{
		"actor_code": actorCode, "space_kind": location.SpaceKind,
		"floor_index": location.FloorIndex, "x": location.X, "y": location.Y, "z": location.Z,
		"priority": 7,
	}
	if location.BuildingCode != nil {
		payload["building_code"] = *location.BuildingCode
	}
	return payload
}

func findCityOpenWorldV5AdjacentPassableTarget(
	t *testing.T,
	ctx context.Context,
	cityService *service.CityEconomyService,
	userID, worldID int64,
	actorCode string,
	current service.CityOpenWorldActorLocation,
) service.CityOpenWorldActorLocation {
	t.Helper()
	require.Equal(t, "surface", current.SpaceKind)
	worldMap, err := cityService.GetOpenWorldMap(ctx, service.CityOpenWorldMapInput{
		UserID: userID, WorldID: worldID,
		MinimumX: current.X - 1, MaximumX: current.X + 1,
		MinimumY: current.Y - 1, MaximumY: current.Y + 1,
		Z: cityspatial.SurfaceZ,
	})
	require.NoError(t, err)
	registry, err := cityspatial.DefaultRegistry()
	require.NoError(t, err)
	ruleSet, err := registry.Get(worldMap.Binding.RuleSetID)
	require.NoError(t, err)
	definitions := make(map[string]cityspatial.Definition, len(ruleSet.Definitions))
	for _, definition := range ruleSet.Definitions {
		definitions[definition.ID] = definition
	}
	for _, offset := range [][2]int64{{-1, -1}, {0, -1}, {1, -1}, {-1, 0}, {1, 0}, {-1, 1}, {0, 1}, {1, 1}} {
		candidate := current
		candidate.X += offset[0]
		candidate.Y += offset[1]
		if !cityOpenWorldV5SurfaceCellPassable(worldMap, definitions, candidate.X, candidate.Y) {
			continue
		}
		var occupied bool
		err = integrationDB.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM city_open_world_actor_locations
    WHERE world_id = $1 AND actor_id <> $2 AND space_kind = 'surface'
      AND location_scope = 'surface' AND floor_index = 0 AND x = $3 AND y = $4 AND z = 0
)`, worldID, actorIDForOpenWorldActor(t, ctx, worldID, actorCode), candidate.X, candidate.Y).Scan(&occupied)
		require.NoError(t, err)
		if !occupied {
			return candidate
		}
	}
	t.Fatalf("no unoccupied passable V5 navigation target near %d,%d", current.X, current.Y)
	return service.CityOpenWorldActorLocation{}
}

func cityOpenWorldV5SurfaceCellPassable(
	worldMap *service.CityOpenWorldMap,
	definitions map[string]cityspatial.Definition,
	x, y int64,
) bool {
	address, err := cityspatial.SplitWorldCoordinate(
		cityspatial.WorldCoordinate{X: x, Y: y, Z: cityspatial.SurfaceZ}, cityspatial.DefaultChunkSize,
	)
	if err != nil {
		return false
	}
	for _, chunk := range worldMap.Chunks {
		if chunk.ChunkX != address.Chunk.X || chunk.ChunkY != address.Chunk.Y || chunk.Z != cityspatial.SurfaceZ {
			continue
		}
		index := int(address.Local.Y)*chunk.Payload.Width + int(address.Local.X)
		terrainID, found := cityOpenWorldV5TerrainAt(chunk.Payload.TerrainRuns, index)
		if !found || !cityOpenWorldV5DefinitionPassable(definitions[terrainID]) {
			return false
		}
		for _, layer := range chunk.Payload.Layers {
			if layer.X != address.Local.X || layer.Y != address.Local.Y {
				continue
			}
			if layer.Kind != cityspatial.RuleKindStructure && layer.Kind != cityspatial.RuleKindFurniture && layer.Kind != cityspatial.RuleKindTerrain {
				continue
			}
			if !cityOpenWorldV5DefinitionPassable(definitions[layer.DefinitionID]) {
				return false
			}
		}
		return true
	}
	return false
}

func cityOpenWorldV5TerrainAt(runs []cityspatial.TerrainRun, index int) (string, bool) {
	position := 0
	for _, run := range runs {
		if index < position+run.Length {
			return run.DefinitionID, true
		}
		position += run.Length
	}
	return "", false
}

func cityOpenWorldV5DefinitionPassable(definition cityspatial.Definition) bool {
	for _, flag := range definition.Flags {
		if flag == "passable" {
			return true
		}
	}
	return false
}

func actorIDForOpenWorldActor(t *testing.T, ctx context.Context, worldID int64, actorCode string) int64 {
	t.Helper()
	var actorID int64
	err := integrationDB.QueryRowContext(ctx, `
SELECT id FROM city_open_world_actors WHERE world_id = $1 AND code = $2`, worldID, actorCode).Scan(&actorID)
	require.NoError(t, err)
	return actorID
}
