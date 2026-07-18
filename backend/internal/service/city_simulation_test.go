package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeCityCommandCanonicalizesIntent(t *testing.T) {
	expectedTick := int64(7)
	first, err := normalizeCityCommand(
		" WORLD.RENAME ",
		json.RawMessage(`{"name":"  Harbor City  "}`),
		&expectedTick,
	)
	require.NoError(t, err)
	second, err := normalizeCityCommand(
		CityCommandTypeWorldRename,
		json.RawMessage("{\n\t\"name\": \"Harbor City\"\n}"),
		&expectedTick,
	)
	require.NoError(t, err)
	require.JSONEq(t, `{"name":"Harbor City"}`, string(first.payload))
	require.Equal(t, first.fingerprint, second.fingerprint)

	_, err = normalizeCityCommand(
		CityCommandTypeWorldRename,
		json.RawMessage(`{"name":"Harbor City","owner_id":1}`),
		&expectedTick,
	)
	require.ErrorIs(t, err, ErrCityInvalidInput)

	_, err = normalizeCityCommand(
		CityCommandTypeWorldSetSpeed,
		json.RawMessage(`{"speed_milli":1.5}`),
		&expectedTick,
	)
	require.ErrorIs(t, err, ErrCityInvalidInput)

	_, err = normalizeCityCommand(
		CityCommandTypeWorldSetSpeed,
		json.RawMessage(`{"speed_milli":1000,"speed_milli":2000}`),
		&expectedTick,
	)
	require.ErrorIs(t, err, ErrCityInvalidInput)
}

func TestCityTickFailureAuditOnlyTracksEngineFailures(t *testing.T) {
	require.False(t, shouldRecordCityTickFailure(nil))
	require.False(t, shouldRecordCityTickFailure(ErrCityExpectedTickConflict))
	require.False(t, shouldRecordCityTickFailure(ErrCityStepIdempotencyConflict))
	require.False(t, shouldRecordCityTickFailure(context.Canceled))
	require.False(t, shouldRecordCityTickFailure(context.DeadlineExceeded))
	require.True(t, shouldRecordCityTickFailure(ErrCitySimulationInvariant))
	require.True(t, shouldRecordCityTickFailure(errors.New("database unavailable")))
}

func TestDeriveCityRandomHexIsVersionedAndStable(t *testing.T) {
	first := deriveCityRandomHex(CitySimulationVersionV1, 42, 9, "population", 3)
	require.Len(t, first, 64)
	require.Equal(t, first, deriveCityRandomHex(CitySimulationVersionV1, 42, 9, "population", 3))
	require.NotEqual(t, first, deriveCityRandomHex(CitySimulationVersionV1, 42, 9, "population", 4))
	require.NotEqual(t, first, deriveCityRandomHex("city-f2-v1", 42, 9, "population", 3))
}
