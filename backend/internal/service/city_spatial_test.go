package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	"github.com/stretchr/testify/require"
)

func TestCitySpatialRuleSetServiceReturnsStableCopies(t *testing.T) {
	t.Parallel()
	cityService := &CityEconomyService{}

	items, err := cityService.ListSpatialRuleSets(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, cityspatial.DefaultRuleSetID, items[0].ID)

	first, err := cityService.GetSpatialRuleSet(context.Background(), 7, items[0].ID)
	require.NoError(t, err)
	require.Equal(t, items[0].ContentHash, first.ContentHash)
	first.Name = "mutated"
	first.Definitions[0].Name = "mutated"

	second, err := cityService.GetSpatialRuleSet(context.Background(), 7, items[0].ID)
	require.NoError(t, err)
	require.NotEqual(t, "mutated", second.Name)
	require.NotEqual(t, "mutated", second.Definitions[0].Name)
}

func TestCitySpatialCommandAndChunkPayloadValidationIsStrict(t *testing.T) {
	t.Parallel()
	payload, handled, err := normalizeCitySpatialCommand(
		CityCommandTypeSpatialGenerateChunk,
		json.RawMessage(`{"chunk_x":-1,"chunk_y":2,"z":0}`),
	)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, cityGenerateChunkPayload{ChunkX: -1, ChunkY: 2, Z: 0}, payload)

	_, handled, err = normalizeCitySpatialCommand(
		CityCommandTypeSpatialGenerateChunk,
		json.RawMessage(`{"chunk_x":0,"chunk_y":0,"z":0,"unknown":true}`),
	)
	require.True(t, handled)
	require.ErrorIs(t, err, ErrCityInvalidInput)
	_, handled, err = normalizeCitySpatialCommand(
		CityCommandTypeSpatialGenerateChunk,
		json.RawMessage(`{"chunk_x":0,"chunk_y":0,"z":128}`),
	)
	require.True(t, handled)
	require.ErrorIs(t, err, ErrCityInvalidInput)

	registry, err := cityspatial.DefaultRegistry()
	require.NoError(t, err)
	ruleSet, err := registry.Get(cityspatial.DefaultRuleSetID)
	require.NoError(t, err)
	binding, err := cityspatial.DefaultGeneratorBinding(CitySimulationVersionF7, 710042, ruleSet)
	require.NoError(t, err)
	overmap, err := cityspatial.GenerateDefaultOvermap(
		binding, []string{"central", "east", "harbor", "north", "south", "west"},
	)
	require.NoError(t, err)
	tile, found := spatialOvermapTileAt(overmap, 0, 0, 0)
	require.True(t, found)
	chunk, err := cityspatial.GenerateDefaultChunk(binding, ruleSet, tile)
	require.NoError(t, err)
	var decoded cityspatial.ChunkPayload
	canonical, err := decodeAndCanonicalizeCityChunkPayload(ruleSet, chunk.CanonicalPayload, &decoded)
	require.NoError(t, err)
	require.Equal(t, chunk.CanonicalPayload, canonical)

	trailing := append(append([]byte(nil), chunk.CanonicalPayload...), []byte(` {}`)...)
	_, err = decodeAndCanonicalizeCityChunkPayload(ruleSet, trailing, &decoded)
	require.ErrorIs(t, err, ErrCitySimulationInvariant)
}

func TestCitySpatialRuleSetServiceValidatesIdentityAndLookup(t *testing.T) {
	t.Parallel()
	cityService := &CityEconomyService{}

	_, err := cityService.ListSpatialRuleSets(context.Background(), 0)
	require.ErrorIs(t, err, ErrCityInvalidInput)
	_, err = cityService.GetSpatialRuleSet(context.Background(), 7, "")
	require.ErrorIs(t, err, ErrCityInvalidInput)
	_, err = cityService.GetSpatialRuleSet(context.Background(), 7, "   ")
	require.ErrorIs(t, err, ErrCityInvalidInput)
	_, err = cityService.GetSpatialRuleSet(context.Background(), 7, "does-not-exist")
	require.ErrorIs(t, err, ErrCitySpatialRuleSetNotFound)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = cityService.ListSpatialRuleSets(cancelled, 7)
	require.ErrorIs(t, err, context.Canceled)
}
