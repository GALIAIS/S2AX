//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCitySimulationTickKernelIsDeterministicIdempotentAndSerialized(t *testing.T) {
	isolateIntegrationData(t)
	ctx := context.Background()
	client := testEntClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	ownerA := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("city-tick-a-%s@example.com", suffix),
		PasswordHash: "integration-test-password",
	})
	ownerB := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("city-tick-b-%s@example.com", suffix),
		PasswordHash: "integration-test-password",
	})
	outsider := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("city-tick-outsider-%s@example.com", suffix),
		PasswordHash: "integration-test-password",
	})
	cityService := service.NewCityEconomyService(integrationDB)
	seed := int64(424242)

	createWorld := func(ownerID int64) *service.CityWorldFoundation {
		foundation, err := cityService.CreateWorld(ctx, service.CityWorldCreateInput{
			OwnerUserID: ownerID,
			Name:        "Deterministic Seed City",
			Timezone:    "Asia/Shanghai",
			Seed:        &seed,
			MonetaryUnit: service.CityMonetaryUnitCreateInput{
				Code: "city_credit", Name: "City Credit", Symbol: "CC",
			},
		})
		require.NoError(t, err)
		return foundation
	}
	worldA := createWorld(ownerA.ID)
	worldB := createWorld(ownerB.ID)

	expectedZero := int64(0)
	submitInitialCommands := func(ownerID, worldID int64) []*service.CityCommand {
		inputs := []service.CityCommandSubmitInput{
			{
				UserID: ownerID, WorldID: worldID, IdempotencyKey: "city-rename-0",
				CommandType: service.CityCommandTypeWorldRename,
				Payload:     json.RawMessage(`{"name":"Harbor City"}`), ExpectedWorldTick: &expectedZero,
			},
			{
				UserID: ownerID, WorldID: worldID, IdempotencyKey: "city-speed-0",
				CommandType: service.CityCommandTypeWorldSetSpeed,
				Payload:     json.RawMessage(`{"speed_milli":1250}`), ExpectedWorldTick: &expectedZero,
			},
			{
				UserID: ownerID, WorldID: worldID, IdempotencyKey: "city-resume-0",
				CommandType: service.CityCommandTypeWorldResume,
				Payload:     json.RawMessage(`{}`), ExpectedWorldTick: &expectedZero,
			},
		}
		commands := make([]*service.CityCommand, 0, len(inputs))
		for _, input := range inputs {
			command, err := cityService.SubmitCommand(ctx, input)
			require.NoError(t, err)
			commands = append(commands, command)
		}
		return commands
	}
	commandsA := submitInitialCommands(ownerA.ID, worldA.World.ID)
	_ = submitInitialCommands(ownerB.ID, worldB.World.ID)
	for index, command := range commandsA {
		require.Equal(t, int64(index+1), command.Sequence)
		require.Equal(t, service.CityCommandStatusPending, command.Status)
	}

	replayedCommand, err := cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: ownerA.ID, WorldID: worldA.World.ID, IdempotencyKey: "city-rename-0",
		CommandType: service.CityCommandTypeWorldRename,
		Payload:     json.RawMessage(`{"name":"Harbor City"}`), ExpectedWorldTick: &expectedZero,
	})
	require.NoError(t, err)
	require.Equal(t, commandsA[0].ID, replayedCommand.ID)
	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: ownerA.ID, WorldID: worldA.World.ID, IdempotencyKey: "city-rename-0",
		CommandType: service.CityCommandTypeWorldRename,
		Payload:     json.RawMessage(`{"name":"Different City"}`), ExpectedWorldTick: &expectedZero,
	})
	require.ErrorIs(t, err, service.ErrCityCommandConflict)

	stepA1, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerA.ID, WorldID: worldA.World.ID,
		IdempotencyKey: "city-step-0", ExpectedWorldTick: &expectedZero,
	})
	require.NoError(t, err)
	stepB1, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerB.ID, WorldID: worldB.World.ID,
		IdempotencyKey: "city-step-0", ExpectedWorldTick: &expectedZero,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), stepA1.Tick.Tick)
	require.Equal(t, 3, stepA1.Tick.CommandCount)
	require.Equal(t, 3, stepA1.Tick.AppliedCommandCount)
	require.Zero(t, stepA1.Tick.RejectedCommandCount)
	require.Len(t, stepA1.Events, 4)
	require.Equal(t, stepA1.Tick.StateHash, stepB1.Tick.StateHash)
	require.Equal(t, stepA1.Tick.PRNGProof, stepB1.Tick.PRNGProof)
	require.Equal(t, service.CityTickEpoch().Add(time.Hour), stepA1.Tick.SimulatedTo)
	for _, command := range stepA1.Commands {
		require.Equal(t, service.CityCommandStatusApplied, command.Status)
		require.Equal(t, int64(1), *command.ProcessedTick)
	}

	updatedWorld, err := cityService.GetWorld(ctx, ownerA.ID, worldA.World.ID)
	require.NoError(t, err)
	require.Equal(t, "Harbor City", updatedWorld.World.Name)
	require.Equal(t, service.CityWorldStatusRunning, updatedWorld.World.Status)
	require.InDelta(t, 1.25, updatedWorld.World.SpeedMultiplier, 0.0001)
	require.Equal(t, int64(1), updatedWorld.World.CurrentTick)
	require.Equal(t, stepA1.Tick.StateHash, *updatedWorld.World.StateHash)
	require.Equal(t, service.CityTickEpoch().Add(time.Hour), *updatedWorld.World.SimulatedAt)

	replayedStep, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerA.ID, WorldID: worldA.World.ID,
		IdempotencyKey: "city-step-0", ExpectedWorldTick: &expectedZero,
	})
	require.NoError(t, err)
	require.Equal(t, stepA1.Tick.ID, replayedStep.Tick.ID)
	require.Equal(t, stepA1.Tick.StateHash, replayedStep.Tick.StateHash)
	require.Equal(t, stepA1.Events[0].ID, replayedStep.Events[0].ID)
	_, err = cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerA.ID, WorldID: worldA.World.ID, IdempotencyKey: "city-step-0",
	})
	require.ErrorIs(t, err, service.ErrCityStepIdempotencyConflict)

	_, err = cityService.SubmitCommand(ctx, service.CityCommandSubmitInput{
		UserID: ownerA.ID, WorldID: worldA.World.ID, IdempotencyKey: "stale-command-0",
		CommandType: service.CityCommandTypeWorldPause,
		Payload:     json.RawMessage(`{}`), ExpectedWorldTick: &expectedZero,
	})
	require.ErrorIs(t, err, service.ErrCityExpectedTickConflict)
	_, err = cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerA.ID, WorldID: worldA.World.ID,
		IdempotencyKey: "city-step-conflict", ExpectedWorldTick: &expectedZero,
	})
	require.ErrorIs(t, err, service.ErrCityExpectedTickConflict)

	page, err := cityService.ListEvents(ctx, service.CityEventListInput{
		UserID: ownerA.ID, WorldID: worldA.World.ID, Limit: 2,
	})
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	require.NotNil(t, page.NextCursor)
	nextPage, err := cityService.ListEvents(ctx, service.CityEventListInput{
		UserID: ownerA.ID, WorldID: worldA.World.ID, Limit: 10,
		AfterTick: page.NextCursor.Tick, AfterSequence: page.NextCursor.Sequence,
	})
	require.NoError(t, err)
	require.Len(t, nextPage.Items, 2)
	_, err = cityService.ListEvents(ctx, service.CityEventListInput{
		UserID: outsider.ID, WorldID: worldA.World.ID,
	})
	require.ErrorIs(t, err, service.ErrCityWorldNotFound)
	_, err = cityService.GetCommand(ctx, outsider.ID, worldA.World.ID, commandsA[0].ID)
	require.ErrorIs(t, err, service.ErrCityCommandNotFound)

	expectedOne := int64(1)
	var waitGroup sync.WaitGroup
	concurrentResults := make(chan *service.CityStepResult, 2)
	concurrentErrors := make(chan error, 2)
	for range 2 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
				UserID: ownerA.ID, WorldID: worldA.World.ID,
				IdempotencyKey: "city-step-1", ExpectedWorldTick: &expectedOne,
			})
			concurrentResults <- result
			concurrentErrors <- stepErr
		}()
	}
	waitGroup.Wait()
	close(concurrentResults)
	close(concurrentErrors)
	for stepErr := range concurrentErrors {
		require.NoError(t, stepErr)
	}
	var replayedTickIDs []int64
	for result := range concurrentResults {
		require.NotNil(t, result)
		replayedTickIDs = append(replayedTickIDs, result.Tick.ID)
	}
	require.Len(t, replayedTickIDs, 2)
	require.Equal(t, replayedTickIDs[0], replayedTickIDs[1])

	stepB2, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerB.ID, WorldID: worldB.World.ID,
		IdempotencyKey: "city-step-1", ExpectedWorldTick: &expectedOne,
	})
	require.NoError(t, err)
	stepA2, err := cityService.StepWorld(ctx, service.CityStepInput{
		UserID: ownerA.ID, WorldID: worldA.World.ID,
		IdempotencyKey: "city-step-1", ExpectedWorldTick: &expectedOne,
	})
	require.NoError(t, err)
	require.Equal(t, stepA2.Tick.StateHash, stepB2.Tick.StateHash)
	require.Equal(t, stepA2.Tick.PRNGProof, stepB2.Tick.PRNGProof)
	var tickCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM city_ticks WHERE world_id = $1`, worldA.World.ID).Scan(&tickCount))
	require.Equal(t, 2, tickCount)

	distinctResults := make(chan *service.CityStepResult, 2)
	distinctErrors := make(chan error, 2)
	for _, key := range []string{"city-step-2-a", "city-step-2-b"} {
		waitGroup.Add(1)
		go func(requestID string) {
			defer waitGroup.Done()
			result, stepErr := cityService.StepWorld(ctx, service.CityStepInput{
				UserID: ownerA.ID, WorldID: worldA.World.ID, IdempotencyKey: requestID,
			})
			distinctResults <- result
			distinctErrors <- stepErr
		}(key)
	}
	waitGroup.Wait()
	close(distinctResults)
	close(distinctErrors)
	for stepErr := range distinctErrors {
		require.NoError(t, stepErr)
	}
	var serializedTicks []int64
	for result := range distinctResults {
		require.NotNil(t, result)
		serializedTicks = append(serializedTicks, result.Tick.Tick)
	}
	sort.Slice(serializedTicks, func(i, j int) bool { return serializedTicks[i] < serializedTicks[j] })
	require.Equal(t, []int64{3, 4}, serializedTicks)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_events SET event_type = 'city.event.tampered'
WHERE world_id = $1 AND tick = 1 AND sequence = 1`, worldA.World.ID)
	require.ErrorContains(t, err, "immutable facts")
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_ticks SET duration_ms = duration_ms + 1
WHERE world_id = $1 AND tick = 1`, worldA.World.ID)
	require.ErrorContains(t, err, "immutable facts")
	_, err = integrationDB.ExecContext(ctx, `
UPDATE city_commands SET payload = '{"name":"Tampered"}'::jsonb
WHERE id = $1`, commandsA[0].ID)
	require.ErrorContains(t, err, "identity and intent are immutable")

	invalidSummaryTx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	invalidHash := strings.Repeat("0", 64)
	_, err = invalidSummaryTx.ExecContext(ctx, `
INSERT INTO city_ticks
    (world_id, tick, step_request_id, request_fingerprint, initiated_by_user_id,
     simulation_version, state_hash, prng_proof, simulated_from, simulated_to,
     command_count, applied_command_count, rejected_command_count, event_count)
VALUES ($1, 999, 'invalid-summary', $2, $3, $4, $2, $2, $5, $6, 0, 0, 0, 1)`,
		worldA.World.ID, invalidHash, ownerA.ID, service.CitySimulationVersionV1,
		service.CityTickEpoch(), service.CityTickEpoch().Add(time.Hour))
	require.NoError(t, err)
	require.ErrorContains(t, invalidSummaryTx.Commit(), "event summary does not match")
}
