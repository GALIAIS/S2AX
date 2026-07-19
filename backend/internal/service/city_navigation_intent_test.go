package service

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestNormalizeWorldNavigationIntentCommands(t *testing.T) {
	value, handled, err := normalizeWorldRuntimeCommand(
		CityCommandTypeActorNavigationIntentSet,
		json.RawMessage(`{
            "actor_code":" ACTOR_0001 ",
            "destination":{"x":12,"y":-4,"z":1}
        }`),
	)
	require.NoError(t, err)
	require.True(t, handled)
	set := value.(worldNavigationIntentSetPayload)
	require.Equal(t, "actor_0001", set.ActorCode)
	require.Equal(t, CityNavigationCoordinate{X: 12, Y: -4, Z: 1}, set.Destination)
	require.Equal(t, 256, set.MaxSteps)
	require.Equal(t, WorldNavigationOnBlockedRetry, set.OnBlocked)

	value, handled, err = normalizeWorldRuntimeCommand(
		CityCommandTypeActorNavigationIntentCancel,
		json.RawMessage(`{"actor_code":"actor_0001"}`),
	)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, "actor_0001", value.(worldNavigationIntentCancelPayload).ActorCode)

	_, _, err = normalizeWorldRuntimeCommand(
		CityCommandTypeActorNavigationIntentSet,
		json.RawMessage(`{"actor_code":"actor_0001","destination":{"x":1,"y":2,"z":0},"priority":11}`),
	)
	require.Error(t, err)
	_, _, err = normalizeWorldRuntimeCommand(
		CityCommandTypeActorNavigationIntentSet,
		json.RawMessage(`{"actor_code":"actor_0001","destination":{"x":1,"y":2,"z":0},"on_blocked":"ignore"}`),
	)
	require.Error(t, err)
}

func TestAccrueWorldNavigationBudgetCapsWithoutOverflow(t *testing.T) {
	intent := WorldActorNavigationIntent{
		BudgetUnits: 50, BudgetGainUnits: 100, BudgetCapUnits: 400,
		UpdatedTick: 10,
	}
	value, err := accrueWorldNavigationBudget(intent, 12)
	require.NoError(t, err)
	require.Equal(t, int64(250), value)

	intent.BudgetUnits = 0
	intent.BudgetGainUnits = math.MaxInt64 - 1
	intent.BudgetCapUnits = math.MaxInt64
	value, err = accrueWorldNavigationBudget(intent, math.MaxInt64)
	require.NoError(t, err)
	require.Equal(t, int64(math.MaxInt64), value)

	_, err = accrueWorldNavigationBudget(intent, 9)
	require.Error(t, err)
}

func TestWorldNavigationReservationKeysAndRetryAreDeterministic(t *testing.T) {
	left := CityNavigationCoordinate{X: -2, Y: 4, Z: 0}
	right := CityNavigationCoordinate{X: -1, Y: 5, Z: 0}
	require.Equal(t, worldNavigationEdgeKey(left, right), worldNavigationEdgeKey(right, left))
	require.Equal(t, "-1:5:0", worldNavigationCoordinateKey(right))
	require.Equal(t, int64(1), worldNavigationRetryDelay(1, 8))
	require.Equal(t, int64(2), worldNavigationRetryDelay(4, 8))
	require.Equal(t, int64(8), worldNavigationRetryDelay(999, 8))
}

func TestSelectWorldNavigationIntentCandidatesIsStableFairAndBounded(t *testing.T) {
	profile := WorldNavigationProfile{
		MaximumIntentsPerTick: 2,
		FairnessAgingCap:      10,
	}
	record := func(actor string, priority, blocked int, created, updated, next int64, status string) worldNavigationIntentRecord {
		return worldNavigationIntentRecord{intent: WorldActorNavigationIntent{
			ActorCode: actor, Priority: priority, BlockedAttempts: blocked,
			CreatedTick: created, UpdatedTick: updated, NextAttemptTick: next,
			Status: status,
		}}
	}
	records := []worldNavigationIntentRecord{
		record("actor_b", 5, 2, 5, 20, 20, WorldNavigationIntentStatusBlocked),
		record("actor_a", -5, 1, 5, 0, 20, WorldNavigationIntentStatusActive),
		record("actor_c", 5, 1, 4, 20, 20, WorldNavigationIntentStatusActive),
		record("actor_future", 10, 9, 0, 20, 21, WorldNavigationIntentStatusActive),
		record("actor_arrived", 10, 9, 0, 0, 0, WorldNavigationIntentStatusArrived),
	}

	candidates, err := selectWorldNavigationIntentCandidates(records, profile, 20)
	require.NoError(t, err)
	require.Equal(t, []worldNavigationIntentCandidate{
		{actorCode: "actor_b", effectivePriority: 5, blockedAttempts: 2, createdTick: 5},
		{actorCode: "actor_c", effectivePriority: 5, blockedAttempts: 1, createdTick: 4},
	}, candidates)

	_, err = selectWorldNavigationIntentCandidates([]worldNavigationIntentRecord{
		record("actor_invalid", 0, 0, 1, 21, 20, WorldNavigationIntentStatusActive),
	}, profile, 20)
	require.Error(t, err)
}

func TestResolveWorldNavigationBlockedOutcomeCoversRetryCancelAndFailure(t *testing.T) {
	profile := WorldNavigationProfile{MaximumBlockedAttempts: 4, MaximumRetryDelayTicks: 8}
	base := WorldActorNavigationIntent{
		Status: WorldNavigationIntentStatusActive, OnBlocked: WorldNavigationOnBlockedRetry,
		NextAttemptTick: 10,
	}

	retried, factType := resolveWorldNavigationBlockedOutcome(
		base, profile, 10, CityNavigationBlockOccupied,
	)
	require.Equal(t, WorldRuntimeFactNavigationIntentBlocked, factType)
	require.Equal(t, WorldNavigationIntentStatusBlocked, retried.Status)
	require.Equal(t, 1, retried.BlockedAttempts)
	require.Equal(t, int64(11), retried.NextAttemptTick)
	require.Equal(t, CityNavigationBlockOccupied, *retried.LastReason)

	cancelBase := base
	cancelBase.OnBlocked = WorldNavigationOnBlockedCancel
	cancelled, factType := resolveWorldNavigationBlockedOutcome(
		cancelBase, profile, 10, CityNavigationBlockPortalClosed,
	)
	require.Equal(t, WorldRuntimeFactNavigationIntentCancelled, factType)
	require.Equal(t, WorldNavigationIntentStatusCancelled, cancelled.Status)
	require.Equal(t, int64(10), cancelled.NextAttemptTick)

	failed, factType := resolveWorldNavigationBlockedOutcome(
		base, profile, 10, CityNavigationBlockOutsideWorld,
	)
	require.Equal(t, WorldRuntimeFactNavigationIntentFailed, factType)
	require.Equal(t, WorldNavigationIntentStatusFailed, failed.Status)

	limitBase := base
	limitBase.BlockedAttempts = 3
	failed, factType = resolveWorldNavigationBlockedOutcome(
		limitBase, profile, 10, CityNavigationBlockUnreachable,
	)
	require.Equal(t, WorldRuntimeFactNavigationIntentFailed, factType)
	require.Equal(t, 4, failed.BlockedAttempts)
}

func TestWorldNavigationReservationConflictClassifiesTargetBeforeEdge(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	rows := func(target, edge bool) *sqlmock.Rows {
		return sqlmock.NewRows([]string{"target_exists", "edge_exists"}).AddRow(target, edge)
	}
	mock.ExpectQuery("SELECT EXISTS").WithArgs(int64(7), int64(12), "2:3:0", "1:3:0|2:3:0").
		WillReturnRows(rows(true, true))
	reason, err := worldNavigationReservationConflict(
		context.Background(), db, 7, 12, "2:3:0", "1:3:0|2:3:0",
	)
	require.NoError(t, err)
	require.Equal(t, WorldNavigationReasonReservationTarget, reason)

	mock.ExpectQuery("SELECT EXISTS").WithArgs(int64(7), int64(12), "3:3:0", "2:3:0|3:3:0").
		WillReturnRows(rows(false, true))
	reason, err = worldNavigationReservationConflict(
		context.Background(), db, 7, 12, "3:3:0", "2:3:0|3:3:0",
	)
	require.NoError(t, err)
	require.Equal(t, WorldNavigationReasonReservationEdge, reason)
	mock.ExpectClose()
	require.NoError(t, db.Close())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestReplayWorldNavigationIntentEffectsPreservesVersionedState(t *testing.T) {
	intents := make([]WorldActorNavigationIntent, 0)
	runtime := &worldRuntimeHashState{
		Actors: []WorldActor{{Code: "actor_0001"}}, NavigationIntents: &intents,
	}
	created := WorldActorNavigationIntent{
		ActorCode: "actor_0001", IntentCode: "navigation_intent_0001",
		Destination: CityNavigationCoordinate{X: 4, Y: 5, Z: 0},
		Status:      WorldNavigationIntentStatusActive, OnBlocked: WorldNavigationOnBlockedRetry,
		MaxSteps: 256, BudgetGainUnits: 100, BudgetCapUnits: 400,
		NextAttemptTick: 2, CreatedTick: 2, UpdatedTick: 2,
		SourceFact: WorldRuntimeFactRef{Tick: 2, Sequence: 1}, Version: 1,
		Metadata: json.RawMessage(`{ "schema_version" : 1 }`),
	}
	before, delta, after := int64(0), int64(1), int64(1)
	payload, err := json.Marshal(map[string]any{
		"schema_version": 1, "navigation_intent_before": nil,
		"navigation_intent_after": created,
	})
	require.NoError(t, err)
	effect := WorldEffectOperation{
		Tick: 2, Sequence: 1, SourceFact: created.SourceFact,
		OperationIndex: 1, EffectType: WorldRuntimeEffectNavigationIntentSet,
		ExecutorVersion: worldRuntimeNavigationIntentVersion,
		TargetActorCode: stringPointer(created.ActorCode), TargetKey: stringPointer("navigation.intent"),
		BeforeUnits: &before, DeltaUnits: &delta, AfterUnits: &after, Payload: payload,
	}
	require.NoError(t, replayWorldRuntimeEffect(runtime, effect))
	require.Equal(t, created.IntentCode, (*runtime.NavigationIntents)[0].IntentCode)

	updated := created
	updated.BudgetUnits = 100
	updated.UpdatedTick = 3
	updated.NextAttemptTick = 4
	updated.SourceFact = WorldRuntimeFactRef{Tick: 3, Sequence: 1}
	updated.Version = 2
	updated.Metadata = json.RawMessage(`{"schema_version":1}`)
	payload, err = json.Marshal(map[string]any{
		"schema_version": 1, "navigation_intent_before": created,
		"navigation_intent_after": updated,
	})
	require.NoError(t, err)
	before, delta, after = 1, 1, 2
	effect = WorldEffectOperation{
		Tick: 3, Sequence: 1, SourceFact: updated.SourceFact,
		OperationIndex: 1, EffectType: WorldRuntimeEffectNavigationIntentSet,
		ExecutorVersion: worldRuntimeNavigationIntentVersion,
		TargetActorCode: stringPointer(updated.ActorCode), TargetKey: stringPointer("navigation.intent"),
		BeforeUnits: &before, DeltaUnits: &delta, AfterUnits: &after, Payload: payload,
	}
	require.NoError(t, replayWorldRuntimeEffect(runtime, effect))
	require.Equal(t, int64(100), (*runtime.NavigationIntents)[0].BudgetUnits)
	require.Equal(t, int64(2), (*runtime.NavigationIntents)[0].Version)
}
