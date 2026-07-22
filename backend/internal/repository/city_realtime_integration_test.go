//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

const integrationCityRealtimeProductionProfileID = "realtime-integration-production-v1"

type integrationCityRealtimeFeatureChecker struct{}

func (integrationCityRealtimeFeatureChecker) IsCitySimulationEnabled(context.Context) bool {
	return true
}

func (integrationCityRealtimeFeatureChecker) IsCityRealtimeSchedulerEnabled(context.Context) bool {
	return true
}

type integrationCityRealtimeClockAuthority struct {
	observation service.CityRealtimeClockObservation
	err         error
}

func (a *integrationCityRealtimeClockAuthority) Observe(
	context.Context,
	service.CityRealtimeClockProfile,
) (service.CityRealtimeClockObservation, error) {
	return a.observation, a.err
}

func ensureIntegrationCityRealtimeProductionProfile(t *testing.T, ctx context.Context) {
	t.Helper()
	_, err := integrationDB.ExecContext(ctx, `
INSERT INTO city_clock_profiles
    (id, version, profile_hash, source_clock_mode, deployment_scope, quantum_us,
     maximum_uncertainty_us, maximum_database_skew_us, pause_policy, calendar_policy,
     status, metadata)
VALUES ($1, '1.0.0', $2, 'system_ntp', 'production', 1000000,
        5000000, 30000000, 'freeze_elapsed_time_v1', 'timezone_elapsed_v1',
        'published', '{"schema_version":1,"purpose":"integration_only"}'::jsonb)
ON CONFLICT (id) DO NOTHING`, integrationCityRealtimeProductionProfileID, strings.Repeat("f", 64))
	require.NoError(t, err)
}

func TestCityRealtimeKernelCreatesSharedTemporalGenesisWithoutLegacyTicks(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	viewer := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-viewer-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	outsider := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-outsider-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})

	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(25_200_001)
	foundation, err := cityService.CreateWorld(service.WithCitySystemAdministrator(ctx), service.CityWorldCreateInput{
		OwnerUserID:       owner.ID,
		Name:              "Realtime Kernel " + suffix,
		Timezone:          "Asia/Shanghai",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV1,
	})
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionRealtimeV1, foundation.World.SimulationVersion)
	require.Equal(t, service.CityWorldStatusRunning, foundation.World.Status)
	worldID := foundation.World.ID

	clock, err := cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, int64(0), clock.WorldTime.ElapsedUS)
	require.Equal(t, "twf_000000000000", clock.TimelineCursor)
	require.Equal(t, "initializing", clock.WorldTime.ClockState)
	require.Equal(t, "frozen_test_clock", clock.WorldTime.SourceClockMode)
	require.Equal(t, "2000-01-01T08:00:00+08:00", clock.WorldTime.LocalTime.Format(time.RFC3339))

	frames, err := cityService.ListTemporalFrames(ctx, service.CityTemporalFrameListInput{
		UserID: owner.ID, WorldID: worldID, AfterFrameSequence: -1,
	})
	require.NoError(t, err)
	require.Len(t, frames.Items, 1)
	require.Equal(t, int64(0), frames.Items[0].FrameSequence)
	require.Equal(t, "genesis", frames.Items[0].FrameKind)
	require.NotNil(t, foundation.World.StateHash)
	require.Equal(t, *foundation.World.StateHash, frames.Items[0].StateHash)

	firstAdvance, err := cityService.AdvanceRealtimeDiagnosticWorld(service.WithCitySystemAdministrator(ctx), service.CityRealtimeDiagnosticAdvanceInput{
		WorldID:      worldID,
		EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(2*time.Second + 250*time.Millisecond),
	})
	require.NoError(t, err)
	require.True(t, firstAdvance.Advanced)
	require.NotNil(t, firstAdvance.Frame)
	require.Equal(t, int64(2_000_000), firstAdvance.CurrentWorldTimeUS)
	require.Equal(t, int64(1), firstAdvance.Frame.FrameSequence)
	require.Equal(t, "twf_000000000001", firstAdvance.TimelineCursor)
	require.Equal(t, int64(0), firstAdvance.Frame.WorldTimeFromUS)
	require.Equal(t, int64(2_000_000), firstAdvance.Frame.WorldTimeToUS)

	noOp, err := cityService.AdvanceRealtimeDiagnosticWorld(service.WithCitySystemAdministrator(ctx), service.CityRealtimeDiagnosticAdvanceInput{
		WorldID:      worldID,
		EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(2*time.Second + 500*time.Millisecond),
	})
	require.NoError(t, err)
	require.False(t, noOp.Advanced)
	require.Nil(t, noOp.Frame)
	require.Equal(t, int64(2_000_000), noOp.CurrentWorldTimeUS)

	secondAdvance, err := cityService.AdvanceRealtimeDiagnosticWorld(service.WithCitySystemAdministrator(ctx), service.CityRealtimeDiagnosticAdvanceInput{
		WorldID:      worldID,
		EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(3 * time.Second),
	})
	require.NoError(t, err)
	require.True(t, secondAdvance.Advanced)
	require.NotNil(t, secondAdvance.Frame)
	require.Equal(t, int64(3_000_000), secondAdvance.CurrentWorldTimeUS)
	require.Equal(t, int64(2), secondAdvance.Frame.FrameSequence)

	frames, err = cityService.ListTemporalFrames(ctx, service.CityTemporalFrameListInput{
		UserID: owner.ID, WorldID: worldID, AfterFrameSequence: -1,
	})
	require.NoError(t, err)
	require.Len(t, frames.Items, 3)
	require.Equal(t, []int64{0, 1, 2}, []int64{
		frames.Items[0].FrameSequence,
		frames.Items[1].FrameSequence,
		frames.Items[2].FrameSequence,
	})
	require.Equal(t, "diagnostic", frames.Items[1].FrameKind)
	require.Equal(t, "diagnostic", frames.Items[2].FrameKind)
	require.NotEqual(t, frames.Items[0].StateHash, frames.Items[1].StateHash)
	require.NotEqual(t, frames.Items[1].StateHash, frames.Items[2].StateHash)

	_, err = cityService.AddWorldMember(ctx, service.CityMemberAddInput{
		UserID: owner.ID, WorldID: worldID, Identity: viewer.Email, Role: service.CityMemberRoleViewer,
	})
	require.NoError(t, err)
	viewerClock, err := cityService.GetRealtimeClock(ctx, viewer.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, secondAdvance.TimelineCursor, viewerClock.TimelineCursor)
	require.Equal(t, secondAdvance.CurrentWorldTimeUS, viewerClock.WorldTime.ElapsedUS)
	_, err = cityService.GetRealtimeClock(ctx, outsider.ID, worldID)
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)

	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "realtime-legacy-command",
		CommandType: service.CityCommandTypeWorldRename, Payload: []byte(`{"name":"Invalid Legacy Command"}`),
	})
	require.ErrorIs(t, err, service.ErrCityRealtimeLegacyAPI)
	_, err = cityService.StepWorld(ctx, service.CityStepInput{
		UserID: owner.ID, WorldID: worldID, IdempotencyKey: "realtime-legacy-step",
	})
	require.ErrorIs(t, err, service.ErrCityRealtimeLegacyAPI)

	var legacyTickCount, legacyScheduleCount, realtimeScheduleCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM city_ticks WHERE world_id = $1`, worldID).Scan(&legacyTickCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM city_world_schedule_states WHERE world_id = $1`, worldID).Scan(&legacyScheduleCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM city_realtime_schedule_states WHERE world_id = $1`, worldID).Scan(&realtimeScheduleCount))
	require.Zero(t, legacyTickCount)
	require.Zero(t, legacyScheduleCount)
	require.Equal(t, 1, realtimeScheduleCount)
}

func TestCityRealtimeV2StaticWorldgenProvidesMemberSafeSharedProjection(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-v2-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	viewer := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-v2-viewer-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	outsider := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-v2-outsider-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})

	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(25_200_011)
	foundation, err := cityService.CreateWorld(service.WithCitySystemAdministrator(ctx), service.CityWorldCreateInput{
		OwnerUserID:       owner.ID,
		Name:              "Realtime Static World " + suffix,
		Timezone:          "Asia/Tokyo",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV2,
		StyleProfileID:    cityspatial.WorldgenProfileJapanMetropolitan,
	})
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionRealtimeV2, foundation.World.SimulationVersion)
	require.NotNil(t, foundation.World.StateHash)
	worldID := foundation.World.ID

	ownerProjection, err := cityService.GetRealtimeWorldProjection(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, service.CitySimulationVersionRealtimeV2, ownerProjection.TemporalEngineVersion)
	require.Equal(t, "twf_000000000000", ownerProjection.TimelineCursor)
	require.Equal(t, cityspatial.WorldgenProfileJapanMetropolitan, ownerProjection.Spatial.ProfileID)
	require.NotEmpty(t, ownerProjection.StaticProjectionHash)
	require.Equal(t, "city-pixel-core", ownerProjection.Visual.PackID)
	require.Equal(t, "1.0.0", ownerProjection.Visual.PackVersion)
	require.Equal(t, cityspatial.WorldgenProfileJapanMetropolitan, ownerProjection.Visual.SpatialProfileID)
	require.Equal(t, "city-realtime-semantic-pixel-v1", ownerProjection.Visual.SemanticProjectionVersion)
	require.Equal(t, "procedural_pixel_v1", ownerProjection.Visual.RenderContractVersion)
	require.Len(t, ownerProjection.Visual.ManifestHash, 64)
	require.Len(t, ownerProjection.Visual.AssetSetHash, 64)
	require.Len(t, ownerProjection.Visual.BindingHash, 64)
	require.True(t, ownerProjection.Viewer.CanManageWorld)
	require.True(t, ownerProjection.Viewer.CanViewSharedWorld)
	require.Equal(t, "member_safe_v1", ownerProjection.Viewer.RedactionPolicy)

	_, err = cityService.AddWorldMember(ctx, service.CityMemberAddInput{
		UserID: owner.ID, WorldID: worldID, Identity: viewer.Email, Role: service.CityMemberRoleViewer,
	})
	require.NoError(t, err)
	viewerProjection, err := cityService.GetRealtimeWorldProjection(ctx, viewer.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, ownerProjection.TimelineCursor, viewerProjection.TimelineCursor)
	require.Equal(t, ownerProjection.StaticProjectionHash, viewerProjection.StaticProjectionHash)
	require.Equal(t, ownerProjection.Visual, viewerProjection.Visual)
	require.Equal(t, service.CityMemberRoleViewer, viewerProjection.Viewer.MembershipRole)
	require.False(t, viewerProjection.Viewer.CanManageWorld)
	projectionJSON, err := json.Marshal(viewerProjection)
	require.NoError(t, err)
	require.NotContains(t, string(projectionJSON), owner.Email)
	require.NotContains(t, string(projectionJSON), viewer.Email)
	visualManifest, err := cityService.GetRealtimeVisualManifest(ctx, viewer.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, viewerProjection.Visual, visualManifest.Binding)
	require.JSONEq(t, `{
  "schema_version": 1,
  "render_mode": "procedural_pixel_v1",
  "logical_tile_px": 16,
  "profile_palettes": {
    "default": {
      "ground": "#5f8259", "soil": "#a57a50", "road": "#77736b", "water": "#3b6f97",
      "building_residential": "#b66f69", "building_commercial": "#d29a55", "building_industrial": "#8393a4",
      "structure": "#343332", "portal": "#e1bd66", "furniture": "#aa704a", "overlay": "#70b8aa"
    },
    "jp.metropolitan": {
      "ground": "#6b9468", "soil": "#b78c61", "road": "#6d7370", "water": "#4a83ad",
      "building_residential": "#bd7770", "building_commercial": "#d8a458", "building_industrial": "#8998aa",
      "structure": "#3a3835", "portal": "#eccb76", "furniture": "#b47a52", "overlay": "#75c3b4"
    },
    "cn.metropolitan": {
      "ground": "#6f8d61", "soil": "#a87c4e", "road": "#74716a", "water": "#437aa0",
      "building_residential": "#b76d62", "building_commercial": "#ce9250", "building_industrial": "#788e9f",
      "structure": "#393632", "portal": "#e0ba63", "furniture": "#a66e47", "overlay": "#70b5a1"
    }
  },
  "semantic_rules": {
    "terrain": ["deep_water", "water", "road", "floor", "soil", "sand", "grass"],
    "building_uses": ["residential", "commercial", "industrial"],
    "layers": ["structure", "portal", "furniture", "item", "entity", "field", "overlay"]
  },
  "assets": []
}`, string(visualManifest.Manifest))
	visualManifestJSON, err := json.Marshal(visualManifest)
	require.NoError(t, err)
	require.NotContains(t, string(visualManifestJSON), owner.Email)
	require.NotContains(t, string(visualManifestJSON), viewer.Email)
	_, err = cityService.GetRealtimeWorldProjection(ctx, outsider.ID, worldID)
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)
	_, err = cityService.GetRealtimeVisualManifest(ctx, outsider.ID, worldID)
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)

	var chunkX, chunkY int64
	var chunkZ int32
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT chunk_x, chunk_y, z
FROM city_realtime_spatial_chunks
WHERE world_id = $1
ORDER BY chunk_y ASC, chunk_x ASC
LIMIT 1`, worldID).Scan(&chunkX, &chunkY, &chunkZ))
	chunk, err := cityService.GetRealtimePixelChunk(ctx, service.CityRealtimePixelChunkInput{
		UserID: viewer.ID, WorldID: worldID, ChunkX: chunkX, ChunkY: chunkY, Z: chunkZ,
	})
	require.NoError(t, err)
	require.Equal(t, viewerProjection.TimelineCursor, chunk.TimelineCursor)
	require.Equal(t, viewerProjection.StaticProjectionHash, chunk.StaticProjectionHash)
	require.Equal(t, chunkX, chunk.Chunk.ChunkX)
	require.Equal(t, chunkY, chunk.Chunk.ChunkY)
	require.Equal(t, chunkZ, chunk.Chunk.Z)
	require.NotEmpty(t, chunk.Chunk.Payload)
	require.NotEmpty(t, chunk.Chunk.PayloadHash)

	patches, err := cityService.ListRealtimePatches(ctx, service.CityRealtimePatchListInput{
		UserID: viewer.ID, WorldID: worldID, AfterFrameSequence: -1,
	})
	require.NoError(t, err)
	require.True(t, patches.FullResyncRequired)
	require.Equal(t, int64(0), patches.CurrentFrameSequence)
	require.Equal(t, viewerProjection.StaticProjectionHash, patches.StaticProjectionHash)
	require.Len(t, patches.Items, 1)
	require.Equal(t, "genesis", patches.Items[0].FrameKind)

	var realtimeBindingCount, realtimeVisualBindingCount, realtimeChunkCount, legacyBindingCount, legacyTickCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM city_realtime_spatial_bindings WHERE world_id = $1`, worldID,
	).Scan(&realtimeBindingCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM city_realtime_spatial_chunks WHERE world_id = $1`, worldID,
	).Scan(&realtimeChunkCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM city_world_visual_bindings WHERE world_id = $1`, worldID,
	).Scan(&realtimeVisualBindingCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM city_open_world_bindings WHERE world_id = $1`, worldID,
	).Scan(&legacyBindingCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM city_ticks WHERE world_id = $1`, worldID,
	).Scan(&legacyTickCount))
	require.Equal(t, 1, realtimeBindingCount)
	require.Equal(t, 1, realtimeVisualBindingCount)
	require.Positive(t, realtimeChunkCount)
	require.Zero(t, legacyBindingCount)
	require.Zero(t, legacyTickCount)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_realtime_spatial_bindings
SET profile_id = 'tampered'
WHERE world_id = $1`, worldID)
	require.Error(t, err, "sealed realtime static worldgen must reject mutation after genesis")
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_world_visual_bindings
SET pack_id = 'tampered'
WHERE world_id = $1`, worldID)
	require.Error(t, err, "sealed realtime visual binding must reject mutation after genesis")

	legacySeed := int64(25_200_012)
	legacy, err := cityService.CreateWorld(service.WithCitySystemAdministrator(ctx), service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Realtime Kernel Only " + suffix,
		Timezone: "Asia/Shanghai", Seed: &legacySeed,
		SimulationVersion: service.CitySimulationVersionRealtimeV1,
	})
	require.NoError(t, err)
	_, err = cityService.GetRealtimeWorldProjection(ctx, owner.ID, legacy.World.ID)
	require.ErrorIs(t, err, service.ErrCityRealtimeStaticWorldRequired)
}

func TestCityRealtimeVisualReleasePolicyPinsOnlyNewWorldBindings(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-visual-policy-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)

	firstSeed := int64(25_200_071)
	first, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Realtime Visual Baseline " + suffix,
		Timezone: "Asia/Tokyo", Seed: &firstSeed,
		SimulationVersion: service.CitySimulationVersionRealtimeV2,
		StyleProfileID:    cityspatial.WorldgenProfileJapanMetropolitan,
	})
	require.NoError(t, err)
	before, err := cityService.GetRealtimeWorldProjection(ctx, owner.ID, first.World.ID)
	require.NoError(t, err)
	require.Equal(t, "city-pixel-core", before.Visual.PackID)

	packID := "city-pixel-release-" + suffix
	staging, err := cityService.CreateRealtimeVisualPack(adminCtx, service.CityRealtimeVisualPackCreateInput{
		ActorUserID: owner.ID,
		PackID:      packID, PackVersion: "1.0.0",
		SpatialProfileIDs: []string{cityspatial.WorldgenProfileJapanMetropolitan},
		Manifest: json.RawMessage(`{
  "schema_version": 1,
  "render_mode": "procedural_pixel_v1",
  "logical_tile_px": 16,
  "profile_palettes": {
    "default": {"ground": "#567d54", "road": "#696c68"},
    "jp.metropolitan": {"ground": "#70946c", "road": "#666e70"}
  },
  "semantic_rules": {"terrain": ["grass", "road"]},
  "assets": []
}`),
	})
	require.NoError(t, err)
	require.Equal(t, "staging", staging.Status)

	published, err := cityService.PublishRealtimeVisualPack(adminCtx, owner.ID, packID, "1.0.0")
	require.NoError(t, err)
	require.Equal(t, "published", published.Status)

	policy, err := cityService.SetRealtimeVisualReleasePolicy(adminCtx, service.CityRealtimeVisualReleasePolicySetInput{
		ActorUserID: owner.ID, SpatialProfileID: cityspatial.WorldgenProfileJapanMetropolitan,
		PackID: packID, PackVersion: "1.0.0",
	})
	require.NoError(t, err)
	require.Equal(t, packID, policy.PackID)

	secondSeed := int64(25_200_072)
	second, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID: owner.ID, Name: "Realtime Visual Policy " + suffix,
		Timezone: "Asia/Tokyo", Seed: &secondSeed,
		SimulationVersion: service.CitySimulationVersionRealtimeV2,
		StyleProfileID:    cityspatial.WorldgenProfileJapanMetropolitan,
	})
	require.NoError(t, err)
	after, err := cityService.GetRealtimeWorldProjection(ctx, owner.ID, second.World.ID)
	require.NoError(t, err)
	require.Equal(t, packID, after.Visual.PackID)
	require.Equal(t, "1.0.0", after.Visual.PackVersion)

	// The first shared world remains attached to its original immutable content
	// plane even though the current release policy has changed.
	oldAgain, err := cityService.GetRealtimeWorldProjection(ctx, owner.ID, first.World.ID)
	require.NoError(t, err)
	require.Equal(t, before.Visual, oldAgain.Visual)
	require.NotEqual(t, oldAgain.Visual.BindingHash, after.Visual.BindingHash)

	manifest, err := cityService.GetRealtimeVisualManifest(ctx, owner.ID, second.World.ID)
	require.NoError(t, err)
	require.Equal(t, after.Visual, manifest.Binding)
	memberJSON, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NotContains(t, string(memberJSON), "admin_staging")
	require.NotContains(t, string(memberJSON), "pending_review")

	events, err := cityService.ListRealtimeVisualReviewEvents(adminCtx, packID, "1.0.0", 20)
	require.NoError(t, err)
	require.Len(t, events, 3)
	require.Equal(t, "release_policy_assigned", events[0].EventType)
}

func TestCityRealtimeDueEventsCommitCanonicalFramesInStableBatches(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-due-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(25_200_002)
	foundation, err := cityService.CreateWorld(service.WithCitySystemAdministrator(ctx), service.CityWorldCreateInput{
		OwnerUserID:       owner.ID,
		Name:              "Realtime Due Events " + suffix,
		Timezone:          "Asia/Shanghai",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV1,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	clock, err := cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)

	_, err = cityService.ScheduleRealtimeDiagnosticDueEvent(ctx, service.CityRealtimeDiagnosticDueEventScheduleInput{
		WorldID: worldID, DueWorldTimeUS: 1_000_000, DedupKey: "diagnostic.alpha",
	})
	require.ErrorIs(t, err, service.ErrCityManagementRequired)

	first, err := cityService.ScheduleRealtimeDiagnosticDueEvent(service.WithCitySystemAdministrator(ctx), service.CityRealtimeDiagnosticDueEventScheduleInput{
		WorldID: worldID, DueWorldTimeUS: 1_000_000, DedupKey: "diagnostic.alpha",
		Payload: map[string]any{"message": "first"},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), first.Frame.FrameSequence)
	require.Equal(t, "pending", first.Status)

	_, err = cityService.ScheduleRealtimeDiagnosticDueEvent(service.WithCitySystemAdministrator(ctx), service.CityRealtimeDiagnosticDueEventScheduleInput{
		WorldID: worldID, DueWorldTimeUS: 1_000_000, DedupKey: "diagnostic.alpha",
	})
	require.ErrorIs(t, err, service.ErrCityRealtimeDueEventConflict)

	second, err := cityService.ScheduleRealtimeDiagnosticDueEvent(service.WithCitySystemAdministrator(ctx), service.CityRealtimeDiagnosticDueEventScheduleInput{
		WorldID: worldID, DueWorldTimeUS: 2_000_000, DedupKey: "diagnostic.beta",
		TemporalPhase: "post_schedule", Priority: 5,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), second.Frame.FrameSequence)
	third, err := cityService.ScheduleRealtimeDiagnosticDueEvent(service.WithCitySystemAdministrator(ctx), service.CityRealtimeDiagnosticDueEventScheduleInput{
		WorldID: worldID, DueWorldTimeUS: 2_000_000, DedupKey: "diagnostic.gamma",
		TemporalPhase: "pre_clock", Priority: -1,
	})
	require.NoError(t, err)
	require.Equal(t, int64(3), third.Frame.FrameSequence)

	_, err = cityService.AdvanceRealtimeDiagnosticWorld(service.WithCitySystemAdministrator(ctx), service.CityRealtimeDiagnosticAdvanceInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(2 * time.Second),
	})
	require.ErrorIs(t, err, service.ErrCityRealtimeDueEventPending)

	firstResolution, err := cityService.ProcessRealtimeDiagnosticDueEvents(service.WithCitySystemAdministrator(ctx), service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(1500 * time.Millisecond),
	})
	require.NoError(t, err)
	require.True(t, firstResolution.Resolved)
	require.Equal(t, 1, firstResolution.AppliedCount)
	require.Zero(t, firstResolution.RejectedCount)
	require.Equal(t, int64(1_000_000), firstResolution.CurrentWorldTimeUS)
	require.Equal(t, int64(4), firstResolution.Frame.FrameSequence)
	require.Equal(t, "due_event", firstResolution.Frame.FrameKind)

	noOp, err := cityService.ProcessRealtimeDiagnosticDueEvents(service.WithCitySystemAdministrator(ctx), service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(1900 * time.Millisecond),
	})
	require.NoError(t, err)
	require.False(t, noOp.Resolved)
	require.Nil(t, noOp.Frame)
	require.Equal(t, int64(1_000_000), noOp.CurrentWorldTimeUS)

	secondResolution, err := cityService.ProcessRealtimeDiagnosticDueEvents(service.WithCitySystemAdministrator(ctx), service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(2 * time.Second),
	})
	require.NoError(t, err)
	require.True(t, secondResolution.Resolved)
	require.Equal(t, 2, secondResolution.AppliedCount)
	require.Zero(t, secondResolution.RejectedCount)
	require.Equal(t, int64(2_000_000), secondResolution.CurrentWorldTimeUS)
	require.Equal(t, int64(5), secondResolution.Frame.FrameSequence)
	require.Equal(t, "twf_000000000005", secondResolution.TimelineCursor)

	unknown, err := cityService.ScheduleRealtimeDiagnosticDueEvent(service.WithCitySystemAdministrator(ctx), service.CityRealtimeDiagnosticDueEventScheduleInput{
		WorldID: worldID, EventType: "system.unimplemented", DueWorldTimeUS: 3_000_000,
		DedupKey: "diagnostic.unknown",
	})
	require.NoError(t, err)
	require.Equal(t, int64(6), unknown.Frame.FrameSequence)
	rejected, err := cityService.ProcessRealtimeDiagnosticDueEvents(service.WithCitySystemAdministrator(ctx), service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(3 * time.Second),
	})
	require.NoError(t, err)
	require.True(t, rejected.Resolved)
	require.Zero(t, rejected.AppliedCount)
	require.Equal(t, 1, rejected.RejectedCount)
	require.Equal(t, int64(7), rejected.Frame.FrameSequence)

	frames, err := cityService.ListTemporalFrames(ctx, service.CityTemporalFrameListInput{
		UserID: owner.ID, WorldID: worldID, AfterFrameSequence: -1,
	})
	require.NoError(t, err)
	require.Len(t, frames.Items, 8)
	require.Equal(t, []string{"genesis", "diagnostic", "diagnostic", "diagnostic", "due_event", "due_event", "diagnostic", "due_event"}, []string{
		frames.Items[0].FrameKind, frames.Items[1].FrameKind, frames.Items[2].FrameKind, frames.Items[3].FrameKind,
		frames.Items[4].FrameKind, frames.Items[5].FrameKind, frames.Items[6].FrameKind, frames.Items[7].FrameKind,
	})
	require.NotEqual(t, frames.Items[1].StateHash, frames.Items[4].StateHash)
	require.NotEqual(t, frames.Items[4].StateHash, frames.Items[5].StateHash)
	require.NotEqual(t, frames.Items[5].StateHash, frames.Items[7].StateHash)

	var nextDue sql.NullInt64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT next_due_at_world_time_us
FROM city_world_time_states
WHERE world_id = $1`, worldID).Scan(&nextDue))
	require.False(t, nextDue.Valid)

	rows, err := integrationDB.QueryContext(ctx, `
SELECT dedup_key, status, created_frame_sequence, resolved_frame_sequence
FROM city_due_events
WHERE world_id = $1
ORDER BY dedup_key ASC`, worldID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	type dueEventState struct {
		dedupKey string
		status   string
		created  int64
		resolved int64
	}
	states := make([]dueEventState, 0, 4)
	for rows.Next() {
		item := dueEventState{}
		require.NoError(t, rows.Scan(&item.dedupKey, &item.status, &item.created, &item.resolved))
		states = append(states, item)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []dueEventState{
		{dedupKey: "diagnostic.alpha", status: "applied", created: 1, resolved: 4},
		{dedupKey: "diagnostic.beta", status: "applied", created: 2, resolved: 5},
		{dedupKey: "diagnostic.gamma", status: "applied", created: 3, resolved: 5},
		{dedupKey: "diagnostic.unknown", status: "rejected", created: 6, resolved: 7},
	}, states)
	finalClock, err := cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, int64(3_000_000), finalClock.WorldTime.ElapsedUS)
	require.Equal(t, "twf_000000000007", finalClock.TimelineCursor)
}

func TestCityRealtimeProductionProfileRecoversThroughCanonicalSchedulerFrames(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	ensureIntegrationCityRealtimeProductionProfile(t, ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-production-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(25_200_003)

	_, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
		OwnerUserID:       owner.ID,
		Name:              "Unauthorized realtime " + suffix,
		Timezone:          "UTC",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV1,
		ClockProfileID:    integrationCityRealtimeProductionProfileID,
	})
	require.ErrorIs(t, err, service.ErrCityManagementRequired)

	foundation, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID:       owner.ID,
		Name:              "Production realtime " + suffix,
		Timezone:          "UTC",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV1,
		ClockProfileID:    integrationCityRealtimeProductionProfileID,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	clock, err := cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, integrationCityRealtimeProductionProfileID, clock.ClockProfileID)
	require.Equal(t, "initializing", clock.WorldTime.ClockState)

	scheduled, err := cityService.ScheduleRealtimeSystemDueEvent(adminCtx, service.CityRealtimeSystemDueEventScheduleInput{
		WorldID: worldID, DueWorldTimeUS: 1_000_000,
		DedupKey: "system.noop.bootstrap", Payload: map[string]any{"purpose": "scheduler-recovery-test"},
	})
	require.NoError(t, err)
	require.Equal(t, "lifecycle", scheduled.Frame.FrameKind)

	authority := &integrationCityRealtimeClockAuthority{err: service.ErrCityRealtimeClockUnsafe}
	scheduler := service.NewCityRealtimeScheduler(integrationDB, cityService, authority, time.Second, 1)
	scheduler.SetFeatureChecker(integrationCityRealtimeFeatureChecker{})

	unsafeReport, err := scheduler.ProcessDue(ctx)
	require.NoError(t, err)
	require.Equal(t, service.CityRealtimeSchedulerReport{Candidates: 1, Claimed: 1, ClockUnsafe: 1}, unsafeReport)
	var clockState, recoveryState string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT clock_state, recovery_state
FROM city_world_time_states
WHERE world_id = $1`, worldID).Scan(&clockState, &recoveryState))
	require.Equal(t, "unsafe", clockState)
	require.Equal(t, "held", recoveryState)

	// Retry/backoff is operational state, not canonical world state. Reset it
	// here so this integration test can exercise the recovery transition
	// immediately without sleeping through the production retry window.
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_realtime_schedule_states
SET retry_not_before = NULL, consecutive_failures = 0
WHERE world_id = $1`, worldID)
	require.NoError(t, err)
	authority.err = nil
	authority.observation = service.CityRealtimeClockObservation{
		NodeID:          "integration-ntp-node",
		SourceClockMode: "system_ntp",
		HealthState:     "healthy",
		EffectiveUTC:    clock.WorldTime.SourceEffectiveUTC.Add(3 * time.Second),
		UncertaintyUS:   0,
	}

	recoveryStartReport, err := scheduler.ProcessDue(ctx)
	require.NoError(t, err)
	require.Equal(t, service.CityRealtimeSchedulerReport{Candidates: 1, Claimed: 1, Recovered: 1}, recoveryStartReport)
	bootstrapRecoveryReport, err := scheduler.ProcessDue(ctx)
	require.NoError(t, err)
	require.Equal(t, service.CityRealtimeSchedulerReport{Candidates: 1, Claimed: 1, Processed: 1}, bootstrapRecoveryReport)
	scheduledRecoveryReport, err := scheduler.ProcessDue(ctx)
	require.NoError(t, err)
	require.Equal(t, service.CityRealtimeSchedulerReport{Candidates: 1, Claimed: 1, Processed: 1}, scheduledRecoveryReport)
	finalRecoveryReport, err := scheduler.ProcessDue(ctx)
	require.NoError(t, err)
	require.Equal(t, service.CityRealtimeSchedulerReport{Candidates: 1, Claimed: 1, Processed: 1}, finalRecoveryReport)

	finalClock, err := cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, int64(3_000_000), finalClock.WorldTime.ElapsedUS)
	require.Equal(t, "healthy", finalClock.WorldTime.ClockState)
	require.Equal(t, "idle", finalClock.WorldTime.RecoveryState)
	require.Nil(t, finalClock.WorldTime.CatchupTargetWorldTimeUS)

	var recoveryFrameCount, segmentCount, legacyTickCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_temporal_frames
WHERE world_id = $1 AND frame_kind = 'recovery'`, worldID).Scan(&recoveryFrameCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_world_clock_segments
WHERE world_id = $1`, worldID).Scan(&segmentCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_ticks
WHERE world_id = $1`, worldID).Scan(&legacyTickCount))
	require.Equal(t, 3, recoveryFrameCount)
	require.Equal(t, 2, segmentCount)
	require.Zero(t, legacyTickCount)
}

func TestCityRealtimeProductionBootstrapAndLifecycleFreezeElapsedTime(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	ensureIntegrationCityRealtimeProductionProfile(t, ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-lifecycle-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(25_200_004)
	foundation, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID:       owner.ID,
		Name:              "Realtime lifecycle " + suffix,
		Timezone:          "UTC",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV1,
		ClockProfileID:    integrationCityRealtimeProductionProfileID,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	clock, err := cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, "initializing", clock.WorldTime.ClockState)

	var genesisDueCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_due_events
WHERE world_id = $1
  AND dedup_key = 'realtime.bootstrap'
  AND due_world_time_us = 0
  AND status = 'pending'`, worldID).Scan(&genesisDueCount))
	require.Equal(t, 1, genesisDueCount)

	_, err = cityService.ScheduleRealtimeSystemDueEvent(adminCtx, service.CityRealtimeSystemDueEventScheduleInput{
		WorldID: worldID, DueWorldTimeUS: 3_000_000,
		DedupKey: "system.lifecycle.future", Payload: map[string]any{"purpose": "pause-preservation-test"},
	})
	require.NoError(t, err)

	observationAtPause := clock.WorldTime.SourceEffectiveUTC.Add(2 * time.Second)
	observation := service.CityRealtimeClockObservation{
		NodeID:          "integration-lifecycle-node",
		SourceClockMode: "system_ntp",
		HealthState:     "healthy",
		EffectiveUTC:    observationAtPause,
		UncertaintyUS:   0,
	}
	_, err = cityService.PauseRealtimeWorld(ctx, service.CityRealtimeLifecycleInput{
		WorldID: worldID, Observation: observation,
	})
	require.ErrorIs(t, err, service.ErrCityManagementRequired)

	paused, err := cityService.PauseRealtimeWorld(adminCtx, service.CityRealtimeLifecycleInput{
		WorldID: worldID, Observation: observation,
	})
	require.NoError(t, err)
	require.True(t, paused.Changed)
	require.Equal(t, service.CityWorldStatusPaused, paused.LifecycleStatus)
	require.Equal(t, int64(2_000_000), paused.CurrentWorldTimeUS)
	require.NotNil(t, paused.Frame)
	require.Equal(t, "lifecycle", paused.Frame.FrameKind)
	require.Equal(t, "administrative_pause", paused.Frame.PhaseSummary["reason"])

	var worldStatus, lifecycleStatus, clockState string
	var elapsedUS int64
	var activeSegmentClosedAt sql.NullTime
	var futureDueStatus string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT world.status, state.lifecycle_status, state.clock_state, state.current_world_time_us,
       segment.closed_at
FROM city_worlds world
JOIN city_world_time_states state ON state.world_id = world.id
JOIN city_world_clock_segments segment ON segment.id = state.current_clock_segment_id
WHERE world.id = $1`, worldID).Scan(
		&worldStatus, &lifecycleStatus, &clockState, &elapsedUS, &activeSegmentClosedAt,
	))
	require.Equal(t, service.CityWorldStatusPaused, worldStatus)
	require.Equal(t, service.CityWorldStatusPaused, lifecycleStatus)
	require.Equal(t, "healthy", clockState)
	require.Equal(t, int64(2_000_000), elapsedUS)
	require.True(t, activeSegmentClosedAt.Valid)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT status
FROM city_due_events
WHERE world_id = $1 AND dedup_key = 'system.lifecycle.future'`, worldID).Scan(&futureDueStatus))
	require.Equal(t, "pending", futureDueStatus)

	resumed, err := cityService.ResumeRealtimeWorld(adminCtx, service.CityRealtimeLifecycleInput{
		WorldID: worldID,
		Observation: service.CityRealtimeClockObservation{
			NodeID:          "integration-lifecycle-node",
			SourceClockMode: "system_ntp",
			HealthState:     "healthy",
			EffectiveUTC:    observationAtPause.Add(time.Second),
			UncertaintyUS:   0,
		},
	})
	require.NoError(t, err)
	require.True(t, resumed.Changed)
	require.Equal(t, service.CityWorldStatusRunning, resumed.LifecycleStatus)
	require.Equal(t, int64(2_000_000), resumed.CurrentWorldTimeUS)
	require.NotNil(t, resumed.Frame)
	require.Equal(t, int64(2_000_000), resumed.Frame.WorldTimeFromUS)
	require.Equal(t, int64(2_000_000), resumed.Frame.WorldTimeToUS)
	require.Equal(t, "administrative_resume", resumed.Frame.PhaseSummary["reason"])

	resumedAgain, err := cityService.ResumeRealtimeWorld(adminCtx, service.CityRealtimeLifecycleInput{
		WorldID: worldID,
		Observation: service.CityRealtimeClockObservation{
			NodeID:          "integration-lifecycle-node",
			SourceClockMode: "system_ntp",
			HealthState:     "healthy",
			EffectiveUTC:    observationAtPause.Add(2 * time.Second),
			UncertaintyUS:   0,
		},
	})
	require.NoError(t, err)
	require.False(t, resumedAgain.Changed)
	require.Equal(t, int64(2_000_000), resumedAgain.CurrentWorldTimeUS)

	health, err := cityService.GetRealtimeOperationalHealth(adminCtx, service.CityRealtimeOperationalHealthInput{WorldID: worldID})
	require.NoError(t, err)
	require.Len(t, health.Worlds, 1)
	require.Equal(t, worldID, health.Worlds[0].WorldID)
	require.Equal(t, service.CityWorldStatusRunning, health.Worlds[0].LifecycleStatus)
	require.Equal(t, "healthy", health.Worlds[0].ClockState)
	require.Equal(t, int64(2_000_000), health.Worlds[0].CurrentWorldTimeUS)
	require.NotEmpty(t, health.Nodes)

	_, err = cityService.GetRealtimeOperationalHealth(ctx, service.CityRealtimeOperationalHealthInput{WorldID: worldID})
	require.ErrorIs(t, err, service.ErrCityManagementRequired)
}

func TestCityRealtimeActorRuntimeProjectsSharedAnonymousPatrols(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-actor-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	viewer := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-actor-viewer-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	outsider := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-actor-outsider-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})

	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(25_200_081)
	foundation, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID:       owner.ID,
		Name:              "Realtime Actor Runtime " + suffix,
		Timezone:          "Asia/Tokyo",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV2,
		StyleProfileID:    cityspatial.WorldgenProfileJapanMetropolitan,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	var identityCount, stateCount, spawnCount, patrolCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_realtime_actor_identities WHERE world_id = $1`, worldID).Scan(&identityCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_realtime_actor_states WHERE world_id = $1`, worldID).Scan(&stateCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_realtime_actor_position_events
WHERE world_id = $1 AND event_kind = 'spawn'`, worldID).Scan(&spawnCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_due_events
WHERE world_id = $1 AND event_type = 'system.realtime.actor_patrol' AND status = 'pending'`, worldID).Scan(&patrolCount))
	require.Equal(t, 6, identityCount)
	require.Equal(t, identityCount, stateCount)
	require.Equal(t, identityCount, spawnCount)
	require.Equal(t, identityCount, patrolCount)

	var actorCode, initialStateHash, initialEventHash string
	var initialX, initialY int64
	var initialZ int32
	var initialRevision, initialFrame int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT state.actor_code, state.x, state.y, state.z, state.position_revision,
       state.last_frame_sequence, state.state_hash, state.event_chain_hash
FROM city_realtime_actor_states state
JOIN city_due_events event
  ON event.world_id = state.world_id
 AND event.aggregate_key = 'actor:' || state.actor_code
WHERE state.world_id = $1
  AND event.event_type = 'system.realtime.actor_patrol'
  AND event.status = 'pending'
ORDER BY event.due_world_time_us ASC, state.actor_code ASC
LIMIT 1`, worldID).Scan(
		&actorCode, &initialX, &initialY, &initialZ, &initialRevision,
		&initialFrame, &initialStateHash, &initialEventHash,
	))
	require.Equal(t, int64(1), initialRevision)
	require.Zero(t, initialFrame)

	initialAddress, err := cityspatial.SplitWorldCoordinate(
		cityspatial.WorldCoordinate{X: initialX, Y: initialY, Z: initialZ}, cityspatial.DefaultChunkSize,
	)
	require.NoError(t, err)
	initialWindow := service.CityRealtimeActorSnapshotInput{
		WorldID: worldID, MinimumChunkX: initialAddress.Chunk.X, MaximumChunkX: initialAddress.Chunk.X,
		MinimumChunkY: initialAddress.Chunk.Y, MaximumChunkY: initialAddress.Chunk.Y,
		Z: initialZ, Limit: 128,
	}
	initialWindow.UserID = owner.ID
	ownerBefore, err := cityService.GetRealtimeActors(ctx, initialWindow)
	require.NoError(t, err)
	require.NotEmpty(t, ownerBefore.Actors)

	_, err = cityService.AddWorldMember(ctx, service.CityMemberAddInput{
		UserID: owner.ID, WorldID: worldID, Identity: viewer.Email, Role: service.CityMemberRoleViewer,
	})
	require.NoError(t, err)
	initialWindow.UserID = viewer.ID
	viewerBefore, err := cityService.GetRealtimeActors(ctx, initialWindow)
	require.NoError(t, err)
	require.Equal(t, ownerBefore, viewerBefore)
	publicJSON, err := json.Marshal(viewerBefore)
	require.NoError(t, err)
	for _, privateField := range []string{"email", "username", "owner_user_id", "prompt", "model", "memory", owner.Email, viewer.Email} {
		require.NotContains(t, string(publicJSON), privateField)
	}
	initialWindow.UserID = outsider.ID
	_, err = cityService.GetRealtimeActors(ctx, initialWindow)
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)

	beforeProjection, err := cityService.GetRealtimeWorldProjection(ctx, owner.ID, worldID)
	require.NoError(t, err)
	clock, err := cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	processed, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(5 * time.Second),
	})
	require.NoError(t, err)
	require.True(t, processed.Resolved)
	require.Equal(t, 1, processed.AppliedCount)
	require.Zero(t, processed.RejectedCount)
	require.NotNil(t, processed.Frame)
	require.Equal(t, int64(1), processed.Frame.FrameSequence)
	require.Equal(t, 1, processed.Frame.PhaseSummary["actor_patrol_count"])

	var movedX, movedY int64
	var movedZ int32
	var movedRevision, movedFrame int64
	var movedStateHash, movedEventHash string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT x, y, z, position_revision, last_frame_sequence, state_hash, event_chain_hash
FROM city_realtime_actor_states
WHERE world_id = $1 AND actor_code = $2`, worldID, actorCode).Scan(
		&movedX, &movedY, &movedZ, &movedRevision, &movedFrame, &movedStateHash, &movedEventHash,
	))
	require.Equal(t, int64(2), movedRevision)
	require.Equal(t, int64(1), movedFrame)
	require.NotEqual(t, initialStateHash, movedStateHash)
	require.NotEqual(t, initialEventHash, movedEventHash)
	require.NotEqual(t, [3]int64{initialX, initialY, int64(initialZ)}, [3]int64{movedX, movedY, int64(movedZ)})

	movedAddress, err := cityspatial.SplitWorldCoordinate(
		cityspatial.WorldCoordinate{X: movedX, Y: movedY, Z: movedZ}, cityspatial.DefaultChunkSize,
	)
	require.NoError(t, err)
	movedWindow := service.CityRealtimeActorSnapshotInput{
		WorldID: worldID, MinimumChunkX: movedAddress.Chunk.X, MaximumChunkX: movedAddress.Chunk.X,
		MinimumChunkY: movedAddress.Chunk.Y, MaximumChunkY: movedAddress.Chunk.Y,
		Z: movedZ, Limit: 128,
	}
	movedWindow.UserID = owner.ID
	ownerAfter, err := cityService.GetRealtimeActors(ctx, movedWindow)
	require.NoError(t, err)
	movedWindow.UserID = viewer.ID
	viewerAfter, err := cityService.GetRealtimeActors(ctx, movedWindow)
	require.NoError(t, err)
	require.Equal(t, ownerAfter, viewerAfter)
	require.Equal(t, processed.TimelineCursor, ownerAfter.TimelineCursor)
	require.NotEqual(t, ownerBefore.TimelineCursor, ownerAfter.TimelineCursor)

	afterProjection, err := cityService.GetRealtimeWorldProjection(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Equal(t, beforeProjection.StaticProjectionHash, afterProjection.StaticProjectionHash)
	require.Equal(t, beforeProjection.Visual, afterProjection.Visual)

	var positionEventCount, nextPatrolCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_realtime_actor_position_events
WHERE world_id = $1 AND actor_code = $2`, worldID, actorCode).Scan(&positionEventCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_due_events
WHERE world_id = $1 AND aggregate_key = $2
  AND event_type = 'system.realtime.actor_patrol' AND status = 'pending'`, worldID, "actor:"+actorCode).Scan(&nextPatrolCount))
	require.Equal(t, 2, positionEventCount)
	require.Equal(t, 1, nextPatrolCount)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_realtime_actor_states
SET x = x + 1
WHERE world_id = $1 AND actor_code = $2`, worldID, actorCode)
	require.Error(t, err, "actor state mutations must be sealed inside an advancing temporal frame")
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_realtime_actor_identities
SET public_label = 'Tampered'
WHERE world_id = $1 AND actor_code = $2`, worldID, actorCode)
	require.Error(t, err, "actor identities are immutable after genesis")
}

func TestCityRealtimeAgentFoundationPinsPolicyAndAnonymousNPCTree(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-agent-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(25_200_082)
	foundation, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID:       owner.ID,
		Name:              "Realtime Agent Foundation " + suffix,
		Timezone:          "Asia/Tokyo",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV2,
		StyleProfileID:    cityspatial.WorldgenProfileJapanMetropolitan,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	var policyID, policyVersion, policyHash, bindingHash string
	var genesisFrameSequence int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT policy_id, policy_version, policy_hash, binding_hash, genesis_frame_sequence
FROM city_realtime_agent_world_bindings
WHERE world_id = $1`, worldID).Scan(
		&policyID, &policyVersion, &policyHash, &bindingHash, &genesisFrameSequence,
	))
	require.Equal(t, "city-realtime-agent-core", policyID)
	require.Equal(t, "1.0.0", policyVersion)
	require.Len(t, policyHash, 64)
	require.Len(t, bindingHash, 64)
	require.Zero(t, genesisFrameSequence)

	var policyStatus string
	var policyManifest []byte
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT status, manifest
FROM city_realtime_agent_policy_bundles
WHERE policy_id = $1 AND policy_version = $2`, policyID, policyVersion,
	).Scan(&policyStatus, &policyManifest))
	require.Equal(t, "published", policyStatus)
	for _, forbidden := range []string{"prompt", "provider", "api_key", "secret", "memory", "response"} {
		require.NotContains(t, strings.ToLower(string(policyManifest)), forbidden)
	}

	var totalAgents, rootAgents, managerAgents, npcAgents, ownedAgents, spawnEvents int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_realtime_agent_instances WHERE world_id = $1`, worldID).Scan(&totalAgents))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_realtime_agent_instances
WHERE world_id = $1 AND agent_code = 'system.root'`, worldID).Scan(&rootAgents))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_realtime_agent_instances
WHERE world_id = $1 AND agent_code = 'system.npc-manager'
  AND parent_agent_code = 'system.root'`, worldID).Scan(&managerAgents))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_realtime_agent_instances
WHERE world_id = $1 AND agent_subtype = 'character.npc'
  AND parent_agent_code = 'system.npc-manager'
  AND actor_code IS NOT NULL AND owner_user_id IS NULL`, worldID).Scan(&npcAgents))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_realtime_agent_instances
WHERE world_id = $1 AND owner_user_id IS NOT NULL`, worldID).Scan(&ownedAgents))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_realtime_agent_lifecycle_events
WHERE world_id = $1 AND event_type = 'spawn' AND event_sequence = 0`, worldID).Scan(&spawnEvents))
	require.Equal(t, 8, totalAgents)
	require.Equal(t, 1, rootAgents)
	require.Equal(t, 1, managerAgents)
	require.Equal(t, 6, npcAgents)
	require.Zero(t, ownedAgents)
	require.Equal(t, totalAgents, spawnEvents)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_realtime_agent_instances
SET lifecycle_status = 'suspended'
WHERE world_id = $1 AND agent_code = 'system.root'`, worldID)
	require.Error(t, err, "agent lifecycle state must only advance through a sealed frame")
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_realtime_agent_world_bindings
SET policy_id = 'city-tampered'
WHERE world_id = $1`, worldID)
	require.Error(t, err, "world policy binding must remain immutable after genesis")
	_, err = integrationDB.ExecContext(ctx, `
INSERT INTO city_realtime_agent_lifecycle_events
    (world_id, agent_code, event_sequence, frame_sequence, event_type,
     from_status, to_status, control_mode, reason_code, previous_event_hash,
     event_hash, metadata)
VALUES ($1, 'system.root', 1, 1, 'lifecycle', 'active', 'suspended', 'system',
        'tamper.attempt', NULL, $2, '{}'::jsonb)`, worldID, strings.Repeat("0", 64))
	require.Error(t, err, "agent lifecycle events must be append-only sealed frame facts")

	projection, err := cityService.GetRealtimeWorldProjection(ctx, owner.ID, worldID)
	require.NoError(t, err)
	projectionJSON, err := json.Marshal(projection)
	require.NoError(t, err)
	for _, forbidden := range []string{"owner_user_id", "prompt", "provider", "memory", owner.Email} {
		require.NotContains(t, string(projectionJSON), forbidden)
	}
}

func TestCityRealtimePlayerCharacterCreatesMovesAndKeepsOwnershipPrivate(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-character-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	viewer := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-character-viewer-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(25_200_083)
	foundation, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID:       owner.ID,
		Name:              "Realtime Player Character " + suffix,
		Timezone:          "Asia/Tokyo",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV2,
		StyleProfileID:    cityspatial.WorldgenProfileJapanMetropolitan,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	_, err = cityService.AddWorldMember(ctx, service.CityMemberAddInput{
		UserID: owner.ID, WorldID: worldID, Identity: viewer.Email, Role: service.CityMemberRoleViewer,
	})
	require.NoError(t, err)

	ownerBefore, err := cityService.GetRealtimeMyCharacter(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.True(t, ownerBefore.RuntimeReady)
	require.False(t, ownerBefore.Exists)
	viewerBefore, err := cityService.GetRealtimeMyCharacter(ctx, viewer.ID, worldID)
	require.NoError(t, err)
	require.True(t, viewerBefore.RuntimeReady)
	require.False(t, viewerBefore.Exists)

	createKey := "character-create-" + suffix
	created, err := cityService.CreateRealtimeCharacter(ctx, service.CityRealtimeCharacterCreateInput{
		UserID: owner.ID, WorldID: worldID, PublicLabel: "春日 花子", IdempotencyKey: createKey,
	})
	require.NoError(t, err)
	require.NotNil(t, created.Frame)
	require.Equal(t, int64(1), created.Frame.FrameSequence)
	require.Equal(t, "command", created.Frame.FrameKind)
	require.Equal(t, "character.create", created.Frame.PhaseSummary["command"])
	require.Equal(t, "春日 花子", created.Character.PublicLabel)
	require.Equal(t, "player.cobalt", created.Character.AppearanceVariant)
	require.True(t, strings.HasPrefix(created.Character.ActorCode, "character.player."))
	require.Len(t, strings.TrimPrefix(created.Character.ActorCode, "character.player."), 32)
	require.Equal(t, int64(1), created.Character.PositionRevision)
	require.Equal(t, int64(1), created.Character.LastFrameSequence)

	var actorKind, storedLabel, storedOwner, agentControl string
	var actorCount, agentCount, receiptCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT actor_kind, public_label
FROM city_realtime_actor_identities
WHERE world_id = $1 AND actor_code = $2`, worldID, created.Character.ActorCode).Scan(&actorKind, &storedLabel))
	require.Equal(t, "character", actorKind)
	require.Equal(t, created.Character.PublicLabel, storedLabel)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT owner_user_id::text, control_mode
FROM city_realtime_agent_instances
WHERE world_id = $1 AND actor_code = $2`, worldID, created.Character.ActorCode).Scan(&storedOwner, &agentControl))
	require.Equal(t, strconv.FormatInt(owner.ID, 10), storedOwner)
	require.Equal(t, "manual", agentControl)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_realtime_actor_identities WHERE world_id = $1`, worldID).Scan(&actorCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_realtime_agent_instances WHERE world_id = $1`, worldID).Scan(&agentCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_realtime_character_action_receipts WHERE world_id = $1`, worldID).Scan(&receiptCount))
	require.Equal(t, 7, actorCount)
	require.Equal(t, 9, agentCount)
	require.Equal(t, 1, receiptCount)

	replayedCreate, err := cityService.CreateRealtimeCharacter(ctx, service.CityRealtimeCharacterCreateInput{
		UserID: owner.ID, WorldID: worldID, PublicLabel: "春日 花子", IdempotencyKey: createKey,
	})
	require.NoError(t, err)
	require.Equal(t, created, replayedCreate)
	_, err = cityService.CreateRealtimeCharacter(ctx, service.CityRealtimeCharacterCreateInput{
		UserID: owner.ID, WorldID: worldID, PublicLabel: "不同请求", IdempotencyKey: createKey,
	})
	require.ErrorIs(t, err, service.ErrCityRealtimeCharacterIdempotencyConflict)
	_, err = cityService.CreateRealtimeCharacter(ctx, service.CityRealtimeCharacterCreateInput{
		UserID: owner.ID, WorldID: worldID, PublicLabel: "另一个角色", IdempotencyKey: "character-create-second-" + suffix,
	})
	require.ErrorIs(t, err, service.ErrCityRealtimeCharacterExists)

	ownerAfter, err := cityService.GetRealtimeMyCharacter(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.True(t, ownerAfter.Exists)
	require.NotNil(t, ownerAfter.Character)
	require.Equal(t, created.Character, *ownerAfter.Character)
	viewerAfter, err := cityService.GetRealtimeMyCharacter(ctx, viewer.ID, worldID)
	require.NoError(t, err)
	require.False(t, viewerAfter.Exists)
	require.Nil(t, viewerAfter.Character)

	nextX, nextY, nextZ := integrationRealtimeCharacterTraversableNeighbor(
		t, worldID, created.Character.X, created.Character.Y, created.Character.Z,
	)
	moveKey := "character-move-" + suffix
	moved, err := cityService.MoveRealtimeCharacter(ctx, service.CityRealtimeCharacterMoveInput{
		UserID: owner.ID, WorldID: worldID, X: nextX, Y: nextY, Z: nextZ, IdempotencyKey: moveKey,
	})
	require.NoError(t, err)
	require.NotNil(t, moved.Frame)
	require.Equal(t, int64(2), moved.Frame.FrameSequence)
	require.Equal(t, "character.move", moved.Frame.PhaseSummary["command"])
	require.Equal(t, int64(2), moved.Character.PositionRevision)
	require.Equal(t, int64(2), moved.Character.LastFrameSequence)
	require.Equal(t, [3]int64{nextX, nextY, int64(nextZ)}, [3]int64{moved.Character.X, moved.Character.Y, int64(moved.Character.Z)})

	replayedMove, err := cityService.MoveRealtimeCharacter(ctx, service.CityRealtimeCharacterMoveInput{
		UserID: owner.ID, WorldID: worldID, X: nextX, Y: nextY, Z: nextZ, IdempotencyKey: moveKey,
	})
	require.NoError(t, err)
	require.Equal(t, moved, replayedMove)
	_, err = cityService.MoveRealtimeCharacter(ctx, service.CityRealtimeCharacterMoveInput{
		UserID: owner.ID, WorldID: worldID, X: nextX + 1, Y: nextY, Z: nextZ, IdempotencyKey: moveKey,
	})
	require.ErrorIs(t, err, service.ErrCityRealtimeCharacterIdempotencyConflict)
	_, err = cityService.MoveRealtimeCharacter(ctx, service.CityRealtimeCharacterMoveInput{
		UserID: owner.ID, WorldID: worldID, X: nextX + 5, Y: nextY, Z: nextZ, IdempotencyKey: "character-move-invalid-" + suffix,
	})
	require.ErrorIs(t, err, service.ErrCityInvalidInput)

	address, err := cityspatial.SplitWorldCoordinate(cityspatial.WorldCoordinate{X: nextX, Y: nextY, Z: nextZ}, cityspatial.DefaultChunkSize)
	require.NoError(t, err)
	snapshot, err := cityService.GetRealtimeActors(ctx, service.CityRealtimeActorSnapshotInput{
		UserID: viewer.ID, WorldID: worldID,
		MinimumChunkX: address.Chunk.X, MaximumChunkX: address.Chunk.X,
		MinimumChunkY: address.Chunk.Y, MaximumChunkY: address.Chunk.Y,
		Z: nextZ, Limit: 128,
	})
	require.NoError(t, err)
	var publicCharacter *service.CityRealtimePublicActor
	for index := range snapshot.Actors {
		if snapshot.Actors[index].ActorCode == moved.Character.ActorCode {
			publicCharacter = &snapshot.Actors[index]
			break
		}
	}
	require.NotNil(t, publicCharacter)
	require.Equal(t, moved.Character.PublicLabel, publicCharacter.PublicLabel)
	require.Equal(t, [3]int64{nextX, nextY, int64(nextZ)}, [3]int64{publicCharacter.X, publicCharacter.Y, int64(publicCharacter.Z)})
	publicJSON, err := json.Marshal(snapshot)
	require.NoError(t, err)
	for _, forbidden := range []string{"owner_user_id", "agent_code", owner.Email, viewer.Email} {
		require.NotContains(t, string(publicJSON), forbidden)
	}

	_, err = integrationDB.ExecContext(ctx, `
INSERT INTO city_realtime_character_action_receipts
    (world_id, user_id, idempotency_key, actor_code, action_type,
     request_hash, frame_sequence, result_payload, result_hash)
VALUES ($1, $2, 'tamper-character-receipt', $3, 'character.move', $4, 2,
        '{"character":{},"frame":{}}'::jsonb, $4)`,
		worldID, owner.ID, moved.Character.ActorCode, strings.Repeat("0", 64),
	)
	require.Error(t, err, "character receipts must remain sealed-frame facts")
}

func TestCityRealtimeCharacterLifeActivitiesAreSealedPrivateAndAuditable(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-life-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	viewer := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-life-viewer-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(25_200_097)
	foundation, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID:       owner.ID,
		Name:              "Realtime Character Life " + suffix,
		Timezone:          "Asia/Tokyo",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV2,
		StyleProfileID:    cityspatial.WorldgenProfileJapanMetropolitan,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	_, err = cityService.AddWorldMember(ctx, service.CityMemberAddInput{
		UserID: owner.ID, WorldID: worldID, Identity: viewer.Email, Role: service.CityMemberRoleViewer,
	})
	require.NoError(t, err)

	var catalogID, catalogVersion string
	var bindingHash string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT catalog_id, catalog_version, binding_hash
FROM city_realtime_character_activity_world_bindings
WHERE world_id = $1`, worldID).Scan(&catalogID, &catalogVersion, &bindingHash))
	require.Equal(t, "city-realtime-character-core", catalogID)
	require.Equal(t, "1.3.0", catalogVersion)
	require.Len(t, bindingHash, 64)

	created, err := cityService.CreateRealtimeCharacter(ctx, service.CityRealtimeCharacterCreateInput{
		UserID: owner.ID, WorldID: worldID, PublicLabel: "森川 凛", ArchetypeCode: "resident.social",
		IdempotencyKey: "character-life-create-" + suffix,
	})
	require.NoError(t, err)
	require.NotNil(t, created.Life)
	require.Equal(t, int64(760), created.Life.EnergyMilli)
	require.Equal(t, int64(720), created.Life.SatietyMilli)
	require.Equal(t, int64(800), created.Life.CivicStandingMilli)
	require.Equal(t, int64(0), created.Life.CityCreditUnits)
	require.Len(t, created.Life.Inventory, 1)
	require.Equal(t, "item.food.ration", created.Life.Inventory[0].ItemCode)
	require.Equal(t, int64(2), created.Life.Inventory[0].Quantity)
	require.NotNil(t, created.Life.Progression)
	require.Equal(t, "resident.social", created.Life.Progression.ArchetypeCode)
	require.Equal(t, int64(0), created.Life.Progression.Revision)
	require.Len(t, created.Life.Progression.Attributes, 5)
	require.Len(t, created.Life.Progression.Roles, 1)
	require.Equal(t, "profession.resident", created.Life.Progression.Roles[0].Code)

	ownerCharacter, err := cityService.GetRealtimeMyCharacter(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.NotNil(t, ownerCharacter.Life)
	require.Len(t, ownerCharacter.AvailableArchetypes, 3)
	require.Len(t, ownerCharacter.AvailableActivities, 8)
	viewerCharacter, err := cityService.GetRealtimeMyCharacter(ctx, viewer.ID, worldID)
	require.NoError(t, err)
	require.False(t, viewerCharacter.Exists)
	require.Nil(t, viewerCharacter.Character)
	require.Nil(t, viewerCharacter.Life)
	require.Empty(t, viewerCharacter.AvailableActivities)

	workKey := "character-life-work-" + suffix
	worked, err := cityService.PerformRealtimeCharacterActivity(ctx, service.CityRealtimeCharacterActivityInput{
		UserID: owner.ID, WorldID: worldID, ActivityCode: "work.civic_shift", IdempotencyKey: workKey,
	})
	require.NoError(t, err)
	require.NotNil(t, worked.Frame)
	require.Equal(t, int64(2), worked.Frame.FrameSequence)
	require.NotNil(t, worked.Life)
	require.NotNil(t, worked.Activity)
	require.Equal(t, "work.civic_shift", worked.Activity.Code)
	require.Equal(t, "completed", worked.Activity.Outcome)
	require.True(t, worked.Activity.PublicVisibility)
	require.Equal(t, int64(24), worked.Activity.CityCreditDeltaUnits)
	require.Equal(t, "item.food.ration", worked.Activity.ItemCode)
	require.Equal(t, int64(1), worked.Activity.ItemQuantityDelta)
	require.Equal(t, int64(24), worked.Life.CityCreditUnits)
	require.Equal(t, int64(810), worked.Life.CivicStandingMilli)
	require.NotNil(t, worked.Life.Progression)
	require.Equal(t, int64(1), worked.Life.Progression.Revision)
	require.Equal(t, []service.CityRealtimeCharacterExperienceDelta{
		{AttributeCode: "communication", ExperienceUnits: 12},
		{AttributeCode: "discipline", ExperienceUnits: 24},
	}, worked.Activity.ExperienceDeltas)
	require.Len(t, worked.Life.Inventory, 1)
	require.Equal(t, int64(3), worked.Life.Inventory[0].Quantity)

	replayedWork, err := cityService.PerformRealtimeCharacterActivity(ctx, service.CityRealtimeCharacterActivityInput{
		UserID: owner.ID, WorldID: worldID, ActivityCode: "work.civic_shift", IdempotencyKey: workKey,
	})
	require.NoError(t, err)
	require.Equal(t, worked, replayedWork)
	_, err = cityService.PerformRealtimeCharacterActivity(ctx, service.CityRealtimeCharacterActivityInput{
		UserID: owner.ID, WorldID: worldID, ActivityCode: "rest.short", IdempotencyKey: workKey,
	})
	require.ErrorIs(t, err, service.ErrCityRealtimeCharacterIdempotencyConflict)
	_, err = cityService.PerformRealtimeCharacterActivity(ctx, service.CityRealtimeCharacterActivityInput{
		UserID: owner.ID, WorldID: worldID, ActivityCode: "rest.short", IdempotencyKey: "character-life-cooldown-" + suffix,
	})
	require.ErrorIs(t, err, service.ErrCityRealtimeCharacterActivityUnavailable)

	privateAfterWork, err := cityService.ListRealtimeMyCharacterEvents(ctx, service.CityRealtimeCharacterEventListInput{
		UserID: owner.ID, WorldID: worldID, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, privateAfterWork.Items, 1)
	require.Equal(t, "work.civic_shift", privateAfterWork.Items[0].ActivityCode)
	require.Equal(t, int64(24), privateAfterWork.Items[0].CityCreditDeltaUnits)
	_, err = cityService.ListRealtimeMyCharacterEvents(ctx, service.CityRealtimeCharacterEventListInput{
		UserID: viewer.ID, WorldID: worldID, Limit: 10,
	})
	require.ErrorIs(t, err, service.ErrCityRealtimeCharacterNotFound)
	publicAfterWork, err := cityService.ListRealtimePublicCharacterEvents(ctx, service.CityRealtimePublicCharacterEventListInput{
		UserID: viewer.ID, WorldID: worldID, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, publicAfterWork.Items, 1)
	require.Equal(t, "森川 凛", publicAfterWork.Items[0].PublicLabel)
	require.Equal(t, "work.civic_shift", publicAfterWork.Items[0].ActivityCode)
	publicJSON, err := json.Marshal(publicAfterWork)
	require.NoError(t, err)
	for _, forbidden := range []string{"energy_milli", "satiety_milli", "morale_milli", "city_credit", "inventory", "owner_user_id", "agent_code", owner.Email} {
		require.NotContains(t, string(publicJSON), forbidden)
	}

	clock, err := cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	processed, err := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
		WorldID: worldID, EffectiveUTC: clock.WorldTime.SourceEffectiveUTC.Add(5 * time.Second),
	})
	require.NoError(t, err)
	require.True(t, processed.Resolved)
	require.NotNil(t, processed.Frame)

	conducted, err := cityService.PerformRealtimeCharacterActivity(ctx, service.CityRealtimeCharacterActivityInput{
		UserID: owner.ID, WorldID: worldID, ActivityCode: "conduct.disruption", IdempotencyKey: "character-life-conduct-" + suffix,
	})
	require.NoError(t, err)
	require.NotNil(t, conducted.Activity)
	require.Equal(t, "penalized", conducted.Activity.Outcome)
	require.NotEmpty(t, conducted.Activity.LawCaseCode)
	require.Equal(t, int64(-12), conducted.Activity.CityCreditDeltaUnits)
	require.NotNil(t, conducted.Life)
	require.Equal(t, int64(12), conducted.Life.CityCreditUnits)
	require.Equal(t, int64(670), conducted.Life.CivicStandingMilli)

	privateEvents, err := cityService.ListRealtimeMyCharacterEvents(ctx, service.CityRealtimeCharacterEventListInput{
		UserID: owner.ID, WorldID: worldID, Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, privateEvents.Items, 1)
	require.Equal(t, "conduct.disruption", privateEvents.Items[0].ActivityCode)
	require.Equal(t, "rule.public_disruption", privateEvents.Items[0].LawRuleCode)
	require.Equal(t, "fine", privateEvents.Items[0].LawDisposition)
	require.Equal(t, int64(12), privateEvents.Items[0].LawPenaltyCreditUnits)
	require.NotNil(t, privateEvents.NextBeforeSequence)
	privateSecondPage, err := cityService.ListRealtimeMyCharacterEvents(ctx, service.CityRealtimeCharacterEventListInput{
		UserID: owner.ID, WorldID: worldID, BeforeSequence: *privateEvents.NextBeforeSequence, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, privateSecondPage.Items, 1)
	require.Equal(t, "work.civic_shift", privateSecondPage.Items[0].ActivityCode)

	publicEvents, err := cityService.ListRealtimePublicCharacterEvents(ctx, service.CityRealtimePublicCharacterEventListInput{
		UserID: viewer.ID, WorldID: worldID, Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, publicEvents.Items, 1)
	require.Equal(t, "conduct.disruption", publicEvents.Items[0].ActivityCode)
	require.Equal(t, "rule.public_disruption", publicEvents.Items[0].LawRuleCode)
	require.NotNil(t, publicEvents.NextCursor)
	publicSecondPage, err := cityService.ListRealtimePublicCharacterEvents(ctx, service.CityRealtimePublicCharacterEventListInput{
		UserID: viewer.ID, WorldID: worldID, BeforeCursor: *publicEvents.NextCursor, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, publicSecondPage.Items, 1)
	require.Equal(t, "work.civic_shift", publicSecondPage.Items[0].ActivityCode)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_realtime_character_profiles
SET energy_milli = 999
WHERE world_id = $1 AND actor_code = $2`, worldID, created.Character.ActorCode)
	require.Error(t, err, "life profiles must only advance through a sealed activity reducer")
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_realtime_character_inventory_stacks
SET quantity = quantity + 1
WHERE world_id = $1 AND actor_code = $2 AND item_code = 'item.food.ration'`, worldID, created.Character.ActorCode)
	require.Error(t, err, "inventory stacks must only advance through a sealed activity reducer")
	_, err = integrationDB.ExecContext(ctx, `
INSERT INTO city_realtime_character_activity_events
    (world_id, actor_code, event_sequence, frame_sequence, activity_code,
     category_code, outcome, public_visibility, energy_delta_milli,
     satiety_delta_milli, morale_delta_milli, civic_standing_delta_milli,
     city_credit_delta_units, item_quantity_delta, previous_event_hash, event_hash, metadata)
VALUES ($1, $2, 99, 99, 'rest.short', 'recovery', 'completed', FALSE,
        0, 0, 0, 0, 0, 0, $3, $3, '{}'::jsonb)`,
		worldID, created.Character.ActorCode, strings.Repeat("0", 64),
	)
	require.Error(t, err, "activity facts must reject unsealed direct inserts")
}

func TestCityRealtimeCharacterProgressionRolesRemainPrivateAndSealed(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-progression-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	viewer := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-progression-viewer-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(25_200_211)
	foundation, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID:       owner.ID,
		Name:              "Realtime Character Progression " + suffix,
		Timezone:          "Asia/Tokyo",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV2,
		StyleProfileID:    cityspatial.WorldgenProfileJapanMetropolitan,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	_, err = cityService.AddWorldMember(ctx, service.CityMemberAddInput{
		UserID: owner.ID, WorldID: worldID, Identity: viewer.Email, Role: service.CityMemberRoleViewer,
	})
	require.NoError(t, err)

	availableBeforeCreate, err := cityService.GetRealtimeMyCharacter(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Len(t, availableBeforeCreate.AvailableArchetypes, 3)

	created, err := cityService.CreateRealtimeCharacter(ctx, service.CityRealtimeCharacterCreateInput{
		UserID: owner.ID, WorldID: worldID, PublicLabel: "工藤 遥", ArchetypeCode: "resident.social",
		IdempotencyKey: "character-progression-create-" + suffix,
	})
	require.NoError(t, err)
	require.NotNil(t, created.Life)
	require.NotNil(t, created.Life.Progression)
	require.Equal(t, "resident.social", created.Life.Progression.ArchetypeCode)
	require.Len(t, created.Life.Progression.Attributes, 5)
	require.Len(t, created.Life.Progression.Roles, 1)

	var stateSchema, attributeCount, roleCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT p.state_schema_version,
       (SELECT COUNT(*) FROM city_realtime_character_attribute_states a WHERE a.world_id = p.world_id AND a.actor_code = p.actor_code),
       (SELECT COUNT(*) FROM city_realtime_character_role_assignments r WHERE r.world_id = p.world_id AND r.actor_code = p.actor_code)
FROM city_realtime_character_profiles p
WHERE p.world_id = $1 AND p.actor_code = $2`, worldID, created.Character.ActorCode).Scan(&stateSchema, &attributeCount, &roleCount))
	require.Equal(t, 3, stateSchema)
	require.Equal(t, 5, attributeCount)
	require.Equal(t, 1, roleCount)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_realtime_character_attribute_states
SET experience_units = 999
WHERE world_id = $1 AND actor_code = $2 AND attribute_code = 'discipline'`, worldID, created.Character.ActorCode)
	require.Error(t, err, "attribute experience must only advance through a sealed progression event")
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_realtime_character_role_assignments
SET role_code = 'profession.civic_aide'
WHERE world_id = $1 AND actor_code = $2 AND category_code = 'profession'`, worldID, created.Character.ActorCode)
	require.Error(t, err, "role assignments must only advance through a sealed role event")

	_, err = cityService.ChangeRealtimeCharacterRole(ctx, service.CityRealtimeCharacterRoleChangeInput{
		UserID: owner.ID, WorldID: worldID, RoleCode: "profession.civic_aide",
		IdempotencyKey: "character-progression-role-early-" + suffix,
	})
	require.ErrorIs(t, err, service.ErrCityRealtimeCharacterRoleUnavailable)

	worked, err := cityService.PerformRealtimeCharacterActivity(ctx, service.CityRealtimeCharacterActivityInput{
		UserID: owner.ID, WorldID: worldID, ActivityCode: "work.civic_shift",
		IdempotencyKey: "character-progression-work-" + suffix,
	})
	require.NoError(t, err)
	require.NotNil(t, worked.Activity)
	require.Equal(t, []service.CityRealtimeCharacterExperienceDelta{
		{AttributeCode: "communication", ExperienceUnits: 12},
		{AttributeCode: "discipline", ExperienceUnits: 24},
	}, worked.Activity.ExperienceDeltas)
	require.NotNil(t, worked.Life)
	require.NotNil(t, worked.Life.Progression)
	require.Equal(t, int64(1), worked.Life.Progression.Revision)

	var progressionEvents int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_realtime_character_progression_events
WHERE world_id = $1 AND actor_code = $2`, worldID, created.Character.ActorCode).Scan(&progressionEvents))
	require.Equal(t, 1, progressionEvents)

	public, err := cityService.ListRealtimePublicCharacterEvents(ctx, service.CityRealtimePublicCharacterEventListInput{
		UserID: viewer.ID, WorldID: worldID, Limit: 10,
	})
	require.NoError(t, err)
	publicJSON, err := json.Marshal(public)
	require.NoError(t, err)
	for _, forbidden := range []string{"progression", "archetype", "experience_units", "discipline", "resident.social"} {
		require.NotContains(t, string(publicJSON), forbidden)
	}
}

func TestCityRealtimePlayerCharacterTraversesSealedBuildingAndVerticalPortals(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-portal-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	viewer := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-portal-viewer-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(25_200_131)
	foundation, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID:       owner.ID,
		Name:              "Realtime Character Portals " + suffix,
		Timezone:          "Asia/Tokyo",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV2,
		StyleProfileID:    cityspatial.WorldgenProfileJapanMetropolitan,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID
	_, err = cityService.AddWorldMember(ctx, service.CityMemberAddInput{
		UserID: owner.ID, WorldID: worldID, Identity: viewer.Email, Role: service.CityMemberRoleViewer,
	})
	require.NoError(t, err)

	created, err := cityService.CreateRealtimeCharacter(ctx, service.CityRealtimeCharacterCreateInput{
		UserID: owner.ID, WorldID: worldID, PublicLabel: "佐藤 翔", IdempotencyKey: "character-portal-create-" + suffix,
	})
	require.NoError(t, err)
	portal, exteriorPath := integrationRealtimePathToMultilevelEntrance(
		t, worldID, created.Character.ActorCode,
		cityspatial.WorldCoordinate{X: created.Character.X, Y: created.Character.Y, Z: created.Character.Z},
	)

	lastCharacter := created.Character
	for index, step := range exteriorPath {
		moved, moveErr := cityService.MoveRealtimeCharacter(ctx, service.CityRealtimeCharacterMoveInput{
			UserID: owner.ID, WorldID: worldID, X: step.X, Y: step.Y, Z: step.Z,
			IdempotencyKey: fmt.Sprintf("character-portal-surface-%s-%03d", suffix, index),
		})
		require.NoError(t, moveErr)
		lastCharacter = moved.Character
	}
	require.Equal(t, [3]int64{portal.EntranceOutside.X, portal.EntranceOutside.Y, int64(portal.EntranceOutside.Z)},
		[3]int64{lastCharacter.X, lastCharacter.Y, int64(lastCharacter.Z)})

	enterKey := "character-portal-enter-" + suffix
	entered, err := cityService.TraverseRealtimeCharacterPortal(ctx, service.CityRealtimeCharacterPortalTraverseInput{
		UserID: owner.ID, WorldID: worldID, PortalCode: portal.EntranceCode, IdempotencyKey: enterKey,
	})
	require.NoError(t, err)
	require.Equal(t, "character.portal", entered.Frame.PhaseSummary["command"])
	require.Equal(t, float64(1), entered.Frame.PhaseSummary["character_portal"])
	require.Equal(t, "inside", entered.Character.MotionState)
	require.Equal(t, [3]int64{portal.EntranceInside.X, portal.EntranceInside.Y, int64(portal.EntranceInside.Z)},
		[3]int64{entered.Character.X, entered.Character.Y, int64(entered.Character.Z)})

	replayedEnter, err := cityService.TraverseRealtimeCharacterPortal(ctx, service.CityRealtimeCharacterPortalTraverseInput{
		UserID: owner.ID, WorldID: worldID, PortalCode: portal.EntranceCode, IdempotencyKey: enterKey,
	})
	require.NoError(t, err)
	require.Equal(t, entered, replayedEnter, "replaying a portal command must not append a second frame")

	insideProjection, err := cityService.GetRealtimeMyCharacter(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.NotNil(t, insideProjection.CurrentInterior)
	require.Equal(t, portal.BuildingCode, insideProjection.CurrentInterior.BuildingCode)
	require.Equal(t, int32(0), insideProjection.CurrentInterior.FloorIndex)
	require.Contains(t, integrationRealtimePortalCodes(insideProjection.AvailablePortals), portal.EntranceCode)
	viewerProjection, err := cityService.GetRealtimeMyCharacter(ctx, viewer.ID, worldID)
	require.NoError(t, err)
	require.False(t, viewerProjection.Exists)
	require.Empty(t, viewerProjection.AvailablePortals)
	require.Nil(t, viewerProjection.CurrentInterior)

	stairsPath := integrationRealtimeInteriorPath(
		t,
		insideProjection.CurrentInterior.Cells,
		cityspatial.WorldCoordinate{X: entered.Character.X, Y: entered.Character.Y, Z: entered.Character.Z},
		portal.StairsLower,
	)
	for index, step := range stairsPath {
		moved, moveErr := cityService.MoveRealtimeCharacter(ctx, service.CityRealtimeCharacterMoveInput{
			UserID: owner.ID, WorldID: worldID, X: step.X, Y: step.Y, Z: step.Z,
			IdempotencyKey: fmt.Sprintf("character-portal-interior-up-%s-%03d", suffix, index),
		})
		require.NoError(t, moveErr)
		lastCharacter = moved.Character
	}
	require.Equal(t, [3]int64{portal.StairsLower.X, portal.StairsLower.Y, int64(portal.StairsLower.Z)},
		[3]int64{lastCharacter.X, lastCharacter.Y, int64(lastCharacter.Z)})

	lowerProjection, err := cityService.GetRealtimeMyCharacter(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Contains(t, integrationRealtimePortalCodes(lowerProjection.AvailablePortals), portal.StairsCode)
	ascended, err := cityService.TraverseRealtimeCharacterPortal(ctx, service.CityRealtimeCharacterPortalTraverseInput{
		UserID: owner.ID, WorldID: worldID, PortalCode: portal.StairsCode, IdempotencyKey: "character-portal-ascend-" + suffix,
	})
	require.NoError(t, err)
	require.Equal(t, "inside", ascended.Character.MotionState)
	require.Equal(t, [3]int64{portal.StairsUpper.X, portal.StairsUpper.Y, int64(portal.StairsUpper.Z)},
		[3]int64{ascended.Character.X, ascended.Character.Y, int64(ascended.Character.Z)})

	upperProjection, err := cityService.GetRealtimeMyCharacter(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.NotNil(t, upperProjection.CurrentInterior)
	require.Equal(t, int32(1), upperProjection.CurrentInterior.FloorIndex)
	require.Contains(t, integrationRealtimePortalCodes(upperProjection.AvailablePortals), portal.StairsCode)
	descended, err := cityService.TraverseRealtimeCharacterPortal(ctx, service.CityRealtimeCharacterPortalTraverseInput{
		UserID: owner.ID, WorldID: worldID, PortalCode: portal.StairsCode, IdempotencyKey: "character-portal-descend-" + suffix,
	})
	require.NoError(t, err)
	require.Equal(t, [3]int64{portal.StairsLower.X, portal.StairsLower.Y, int64(portal.StairsLower.Z)},
		[3]int64{descended.Character.X, descended.Character.Y, int64(descended.Character.Z)})

	returnProjection, err := cityService.GetRealtimeMyCharacter(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.NotNil(t, returnProjection.CurrentInterior)
	exitPath := integrationRealtimeInteriorPath(
		t,
		returnProjection.CurrentInterior.Cells,
		cityspatial.WorldCoordinate{X: descended.Character.X, Y: descended.Character.Y, Z: descended.Character.Z},
		portal.EntranceInside,
	)
	for index, step := range exitPath {
		moved, moveErr := cityService.MoveRealtimeCharacter(ctx, service.CityRealtimeCharacterMoveInput{
			UserID: owner.ID, WorldID: worldID, X: step.X, Y: step.Y, Z: step.Z,
			IdempotencyKey: fmt.Sprintf("character-portal-interior-out-%s-%03d", suffix, index),
		})
		require.NoError(t, moveErr)
		lastCharacter = moved.Character
	}
	require.Equal(t, [3]int64{portal.EntranceInside.X, portal.EntranceInside.Y, int64(portal.EntranceInside.Z)},
		[3]int64{lastCharacter.X, lastCharacter.Y, int64(lastCharacter.Z)})

	exited, err := cityService.TraverseRealtimeCharacterPortal(ctx, service.CityRealtimeCharacterPortalTraverseInput{
		UserID: owner.ID, WorldID: worldID, PortalCode: portal.EntranceCode, IdempotencyKey: "character-portal-exit-" + suffix,
	})
	require.NoError(t, err)
	require.Equal(t, "walking", exited.Character.MotionState)
	require.Equal(t, [3]int64{portal.EntranceOutside.X, portal.EntranceOutside.Y, int64(portal.EntranceOutside.Z)},
		[3]int64{exited.Character.X, exited.Character.Y, int64(exited.Character.Z)})

	afterExit, err := cityService.GetRealtimeMyCharacter(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.Nil(t, afterExit.CurrentInterior)
	var portalEvents int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_realtime_actor_position_events
WHERE world_id = $1 AND actor_code = $2 AND event_kind = 'portal' AND portal_code IS NOT NULL`,
		worldID, exited.Character.ActorCode,
	).Scan(&portalEvents))
	require.Equal(t, 4, portalEvents)
}

func TestCityRealtimeCharacterMetabolismIsScheduledAndIndependentOfActivities(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	adminCtx := service.WithCitySystemAdministrator(ctx)
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	owner := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("realtime-metabolism-owner-%s@example.com", suffix), PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(25_200_111)
	foundation, err := cityService.CreateWorld(adminCtx, service.CityWorldCreateInput{
		OwnerUserID:       owner.ID,
		Name:              "Realtime Character Metabolism " + suffix,
		Timezone:          "Asia/Tokyo",
		Seed:              &seed,
		SimulationVersion: service.CitySimulationVersionRealtimeV2,
		StyleProfileID:    cityspatial.WorldgenProfileJapanMetropolitan,
	})
	require.NoError(t, err)
	worldID := foundation.World.ID

	created, err := cityService.CreateRealtimeCharacter(ctx, service.CityRealtimeCharacterCreateInput{
		UserID: owner.ID, WorldID: worldID, PublicLabel: "森川 律", IdempotencyKey: "character-metabolism-create-" + suffix,
	})
	require.NoError(t, err)
	require.NotNil(t, created.Life)
	require.Equal(t, int64(0), created.Life.MetabolismRevision)
	require.Equal(t, int64(0), created.Life.LastMetabolismWorldTimeUS)

	var firstDue, firstExpectedVersion int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT due_world_time_us, expected_version
FROM city_due_events
WHERE world_id = $1
  AND event_type = 'system.realtime.character_metabolism'
  AND aggregate_key = $2
  AND status = 'pending'`, worldID, "character:"+created.Character.ActorCode).Scan(&firstDue, &firstExpectedVersion))
	require.Equal(t, int64(60_000_000), firstDue)
	require.Equal(t, int64(0), firstExpectedVersion)

	// A player action increments the general profile revision but must not make
	// the pending passive event stale: expected_version is the separate
	// metabolism revision, not a browser-observable profile revision.
	worked, err := cityService.PerformRealtimeCharacterActivity(ctx, service.CityRealtimeCharacterActivityInput{
		UserID: owner.ID, WorldID: worldID, ActivityCode: "work.civic_shift", IdempotencyKey: "character-metabolism-work-" + suffix,
	})
	require.NoError(t, err)
	require.NotNil(t, worked.Life)
	require.Equal(t, int64(2), worked.Life.Revision)
	require.Equal(t, int64(0), worked.Life.MetabolismRevision)

	clock, err := cityService.GetRealtimeClock(ctx, owner.ID, worldID)
	require.NoError(t, err)
	// The frozen diagnostic profile intentionally rejects a source observation
	// more than 30 seconds away from PostgreSQL's clock.  The first metabolism
	// event is due at one minute, so wait until that source timestamp is inside
	// the trusted skew window instead of weakening the immutable profile just
	// for this integration test.
	time.Sleep(31 * time.Second)
	targetEffectiveUTC := clock.WorldTime.SourceEffectiveUTC.Add(time.Minute)
	metabolismApplied := false
	for attempt := 0; attempt < 128; attempt++ {
		processed, processErr := cityService.ProcessRealtimeDiagnosticDueEvents(adminCtx, service.CityRealtimeDiagnosticDueEventProcessInput{
			WorldID: worldID, EffectiveUTC: targetEffectiveUTC,
		})
		require.NoError(t, processErr)
		if !processed.Resolved {
			break
		}
		require.NotNil(t, processed.Frame)
		if count, ok := processed.Frame.PhaseSummary["character_metabolism_count"].(int); ok && count == 1 {
			metabolismApplied = true
			break
		}
	}
	require.True(t, metabolismApplied, "expected the first passive character metabolism due event to resolve")

	ownerCharacter, err := cityService.GetRealtimeMyCharacter(ctx, owner.ID, worldID)
	require.NoError(t, err)
	require.NotNil(t, ownerCharacter.Life)
	require.Equal(t, int64(3), ownerCharacter.Life.Revision)
	require.Equal(t, int64(1), ownerCharacter.Life.ActivityRevision)
	require.Equal(t, int64(0), ownerCharacter.Life.LawRevision)
	require.Equal(t, int64(1), ownerCharacter.Life.MetabolismRevision)
	require.Equal(t, int64(60_000_000), ownerCharacter.Life.LastMetabolismWorldTimeUS)
	require.Equal(t, int64(634), ownerCharacter.Life.EnergyMilli)
	require.Equal(t, int64(637), ownerCharacter.Life.SatietyMilli)
	require.Equal(t, int64(658), ownerCharacter.Life.MoraleMilli)

	var nextDue, nextExpectedVersion int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT due_world_time_us, expected_version
FROM city_due_events
WHERE world_id = $1
  AND event_type = 'system.realtime.character_metabolism'
  AND aggregate_key = $2
  AND status = 'pending'`, worldID, "character:"+created.Character.ActorCode).Scan(&nextDue, &nextExpectedVersion))
	require.Equal(t, int64(120_000_000), nextDue)
	require.Equal(t, int64(1), nextExpectedVersion)
}

type integrationRealtimeMultilevelPortal struct {
	EntranceCode    string
	StairsCode      string
	BuildingCode    string
	EntranceOutside cityspatial.WorldCoordinate
	EntranceInside  cityspatial.WorldCoordinate
	StairsLower     cityspatial.WorldCoordinate
	StairsUpper     cityspatial.WorldCoordinate
}

type integrationRealtimeNavigationCell struct {
	X int64
	Y int64
	Z int32
}

func integrationRealtimePathToMultilevelEntrance(
	t *testing.T,
	worldID int64,
	actorCode string,
	start cityspatial.WorldCoordinate,
) (integrationRealtimeMultilevelPortal, []cityspatial.WorldCoordinate) {
	t.Helper()
	rows, err := integrationDB.QueryContext(context.Background(), `
SELECT entrance.code, entrance.building_code,
       entrance.from_x, entrance.from_y, entrance.from_z,
       entrance.to_x, entrance.to_y, entrance.to_z,
       stairs.code,
       stairs.from_x, stairs.from_y, stairs.from_z,
       stairs.to_x, stairs.to_y, stairs.to_z
FROM city_realtime_spatial_portals entrance
JOIN city_realtime_spatial_portals stairs
  ON stairs.world_id = entrance.world_id
 AND stairs.building_code = entrance.building_code
WHERE entrance.world_id = $1
  AND entrance.portal_type = 'entrance'
  AND entrance.from_floor_index = 0
  AND entrance.to_floor_index = 0
  AND stairs.portal_type = 'stairs'
  AND stairs.from_floor_index = 0
  AND stairs.to_floor_index = 1
ORDER BY entrance.code ASC, stairs.code ASC`, worldID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	candidates := make([]integrationRealtimeMultilevelPortal, 0)
	for rows.Next() {
		portal := integrationRealtimeMultilevelPortal{}
		err = rows.Scan(
			&portal.EntranceCode, &portal.BuildingCode,
			&portal.EntranceOutside.X, &portal.EntranceOutside.Y, &portal.EntranceOutside.Z,
			&portal.EntranceInside.X, &portal.EntranceInside.Y, &portal.EntranceInside.Z,
			&portal.StairsCode,
			&portal.StairsLower.X, &portal.StairsLower.Y, &portal.StairsLower.Z,
			&portal.StairsUpper.X, &portal.StairsUpper.Y, &portal.StairsUpper.Z,
		)
		require.NoError(t, err)
		candidates = append(candidates, portal)
	}
	require.NoError(t, rows.Err())
	require.NotEmpty(t, candidates, "a V2 city needs at least one multi-floor building to exercise vertical portal traversal")

	walkable := integrationRealtimeSurfaceWalkableCells(t, worldID)
	occupied := integrationRealtimeOccupiedNavigationCells(t, worldID, actorCode)
	startCell := integrationRealtimeNavigationCell{X: start.X, Y: start.Y, Z: start.Z}
	delete(occupied, startCell)
	var selected integrationRealtimeMultilevelPortal
	var selectedPath []cityspatial.WorldCoordinate
	found := false
	for _, candidate := range candidates {
		goal := integrationRealtimeNavigationCell{X: candidate.EntranceOutside.X, Y: candidate.EntranceOutside.Y, Z: candidate.EntranceOutside.Z}
		if _, blocked := occupied[goal]; blocked || !walkable[goal] {
			continue
		}
		path, reachable := integrationRealtimeShortestNavigationPath(walkable, startCell, goal)
		if !reachable || (found && len(path) >= len(selectedPath)) {
			continue
		}
		selected = candidate
		selectedPath = path
		found = true
	}
	require.True(t, found, "expected a reachable exterior entrance for a multi-floor building")
	return selected, selectedPath
}

func integrationRealtimeSurfaceWalkableCells(
	t *testing.T,
	worldID int64,
) map[integrationRealtimeNavigationCell]bool {
	t.Helper()
	rows, err := integrationDB.QueryContext(context.Background(), `
SELECT chunk_x, chunk_y, z, payload
FROM city_realtime_spatial_chunks
WHERE world_id = $1 AND z = $2
ORDER BY chunk_y ASC, chunk_x ASC`, worldID, cityspatial.SurfaceZ)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	walkable := make(map[integrationRealtimeNavigationCell]bool)
	for rows.Next() {
		var chunkX, chunkY int64
		var z int32
		var raw []byte
		require.NoError(t, rows.Scan(&chunkX, &chunkY, &z, &raw))
		payload := cityspatial.OpenWorldChunkPayload{}
		require.NoError(t, json.Unmarshal(raw, &payload))
		require.NoError(t, cityspatial.ValidateOpenWorldChunkPayload(payload))
		structures := make(map[[2]int32]struct{})
		for _, layer := range payload.Layers {
			if layer.Kind == cityspatial.RuleKindStructure {
				structures[[2]int32{layer.X, layer.Y}] = struct{}{}
			}
		}
		for localY := 0; localY < payload.Height; localY++ {
			for localX := 0; localX < payload.Width; localX++ {
				if _, blocked := structures[[2]int32{int32(localX), int32(localY)}]; blocked {
					continue
				}
				terrain, found := integrationRealtimeTerrainDefinitionAt(payload.TerrainRuns, localY*payload.Width+localX)
				if !found || !integrationRealtimeSurfaceTerrainTraversable(terrain) {
					continue
				}
				walkable[integrationRealtimeNavigationCell{
					X: chunkX*int64(payload.Width) + int64(localX),
					Y: chunkY*int64(payload.Height) + int64(localY),
					Z: z,
				}] = true
			}
		}
	}
	require.NoError(t, rows.Err())

	interiorRows, err := integrationDB.QueryContext(context.Background(), `
SELECT cells
FROM city_realtime_spatial_building_interiors
WHERE world_id = $1 AND z = $2`, worldID, cityspatial.SurfaceZ)
	require.NoError(t, err)
	defer func() { _ = interiorRows.Close() }()
	for interiorRows.Next() {
		var raw []byte
		require.NoError(t, interiorRows.Scan(&raw))
		cells := []cityspatial.GeneratedWorldgenInteriorCell{}
		require.NoError(t, json.Unmarshal(raw, &cells))
		for _, cell := range cells {
			delete(walkable, integrationRealtimeNavigationCell{X: cell.X, Y: cell.Y, Z: cell.Z})
		}
	}
	require.NoError(t, interiorRows.Err())
	return walkable
}

func integrationRealtimeTerrainDefinitionAt(runs []cityspatial.TerrainRun, index int) (string, bool) {
	if index < 0 {
		return "", false
	}
	covered := 0
	for _, run := range runs {
		if index >= covered && index < covered+run.Length {
			return run.DefinitionID, true
		}
		covered += run.Length
	}
	return "", false
}

func integrationRealtimeSurfaceTerrainTraversable(definitionID string) bool {
	switch definitionID {
	case "terrain.road", "terrain.sidewalk", "terrain.grass", "terrain.ground", "terrain.soil":
		return true
	default:
		return false
	}
}

func integrationRealtimeOccupiedNavigationCells(
	t *testing.T,
	worldID int64,
	excludedActorCode string,
) map[integrationRealtimeNavigationCell]struct{} {
	t.Helper()
	rows, err := integrationDB.QueryContext(context.Background(), `
SELECT state.x, state.y, state.z
FROM city_realtime_actor_states state
JOIN city_realtime_actor_identities identity
  ON identity.world_id = state.world_id AND identity.actor_code = state.actor_code
WHERE state.world_id = $1
  AND state.actor_code <> $2
  AND identity.lifecycle_status = 'active'`, worldID, excludedActorCode)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	occupied := make(map[integrationRealtimeNavigationCell]struct{})
	for rows.Next() {
		cell := integrationRealtimeNavigationCell{}
		require.NoError(t, rows.Scan(&cell.X, &cell.Y, &cell.Z))
		occupied[cell] = struct{}{}
	}
	require.NoError(t, rows.Err())
	return occupied
}

func integrationRealtimeShortestNavigationPath(
	walkable map[integrationRealtimeNavigationCell]bool,
	start, goal integrationRealtimeNavigationCell,
) ([]cityspatial.WorldCoordinate, bool) {
	if !walkable[start] || !walkable[goal] {
		return nil, false
	}
	queue := []integrationRealtimeNavigationCell{start}
	visited := map[integrationRealtimeNavigationCell]bool{start: true}
	previous := make(map[integrationRealtimeNavigationCell]integrationRealtimeNavigationCell)
	for head := 0; head < len(queue); head++ {
		current := queue[head]
		if current == goal {
			reversed := make([]cityspatial.WorldCoordinate, 0)
			for cursor := goal; cursor != start; cursor = previous[cursor] {
				reversed = append(reversed, cityspatial.WorldCoordinate{X: cursor.X, Y: cursor.Y, Z: cursor.Z})
			}
			path := make([]cityspatial.WorldCoordinate, len(reversed))
			for index := range reversed {
				path[len(reversed)-1-index] = reversed[index]
			}
			return path, true
		}
		for _, neighbor := range []integrationRealtimeNavigationCell{
			{X: current.X + 1, Y: current.Y, Z: current.Z},
			{X: current.X - 1, Y: current.Y, Z: current.Z},
			{X: current.X, Y: current.Y + 1, Z: current.Z},
			{X: current.X, Y: current.Y - 1, Z: current.Z},
		} {
			if !walkable[neighbor] || visited[neighbor] {
				continue
			}
			visited[neighbor] = true
			previous[neighbor] = current
			queue = append(queue, neighbor)
		}
	}
	return nil, false
}

func integrationRealtimeInteriorPath(
	t *testing.T,
	cells []service.CityRealtimeCharacterInteriorCell,
	start, goal cityspatial.WorldCoordinate,
) []cityspatial.WorldCoordinate {
	t.Helper()
	walkable := make(map[integrationRealtimeNavigationCell]bool, len(cells))
	for _, cell := range cells {
		if cell.Traversable {
			walkable[integrationRealtimeNavigationCell{X: cell.X, Y: cell.Y, Z: cell.Z}] = true
		}
	}
	path, found := integrationRealtimeShortestNavigationPath(
		walkable,
		integrationRealtimeNavigationCell{X: start.X, Y: start.Y, Z: start.Z},
		integrationRealtimeNavigationCell{X: goal.X, Y: goal.Y, Z: goal.Z},
	)
	require.True(t, found, "expected a traversable path through the sealed interior")
	return path
}

func integrationRealtimePortalCodes(items []service.CityRealtimeCharacterPortalTransition) []string {
	codes := make([]string, 0, len(items))
	for _, item := range items {
		codes = append(codes, item.PortalCode)
	}
	return codes
}

func integrationRealtimeCharacterTraversableNeighbor(
	t *testing.T,
	worldID, x, y int64,
	z int32,
) (int64, int64, int32) {
	t.Helper()
	for _, candidate := range []cityspatial.WorldCoordinate{
		{X: x + 1, Y: y, Z: z}, {X: x - 1, Y: y, Z: z},
		{X: x, Y: y + 1, Z: z}, {X: x, Y: y - 1, Z: z},
	} {
		address, err := cityspatial.SplitWorldCoordinate(candidate, cityspatial.DefaultChunkSize)
		require.NoError(t, err)
		var raw []byte
		err = integrationDB.QueryRowContext(context.Background(), `
SELECT payload
FROM city_realtime_spatial_chunks
WHERE world_id = $1 AND chunk_x = $2 AND chunk_y = $3 AND z = $4`,
			worldID, address.Chunk.X, address.Chunk.Y, address.Chunk.Z,
		).Scan(&raw)
		if err != nil {
			continue
		}
		payload := cityspatial.OpenWorldChunkPayload{}
		if json.Unmarshal(raw, &payload) != nil || cityspatial.ValidateOpenWorldChunkPayload(payload) != nil {
			continue
		}
		blocked := false
		for _, layer := range payload.Layers {
			if layer.Kind == cityspatial.RuleKindStructure && layer.X == address.Local.X && layer.Y == address.Local.Y {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}
		cellIndex := int(address.Local.Y)*payload.Width + int(address.Local.X)
		covered := 0
		for _, run := range payload.TerrainRuns {
			if cellIndex >= covered && cellIndex < covered+run.Length {
				switch run.DefinitionID {
				case "terrain.road", "terrain.sidewalk", "terrain.grass", "terrain.ground", "terrain.soil":
					return candidate.X, candidate.Y, candidate.Z
				}
			}
			covered += run.Length
		}
	}
	t.Fatalf("no adjacent traversable realtime character cell at %d,%d,%d", x, y, z)
	return 0, 0, 0
}
