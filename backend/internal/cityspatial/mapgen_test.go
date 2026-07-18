package cityspatial

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultMapgenIsDeterministicAndConnected(t *testing.T) {
	t.Parallel()
	registry, err := DefaultRegistry()
	require.NoError(t, err)
	ruleSet, err := registry.Get(DefaultRuleSetID)
	require.NoError(t, err)
	binding, err := DefaultGeneratorBinding("city-f7-v1", 710042, ruleSet)
	require.NoError(t, err)

	first, err := GenerateDefaultOvermap(binding, requiredDistrictCodes)
	require.NoError(t, err)
	second, err := GenerateDefaultOvermap(binding, append([]string(nil), requiredDistrictCodes...))
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Len(t, first.Tiles, 81)
	require.Equal(t, "fdf565491d2fed3079c6121dc108fc932c80ed7ff4a3d22e5cb3f5d64315730e", first.RootHash)

	tileByCoordinate := make(map[[2]int64]OvermapTile, len(first.Tiles))
	seenDistricts := make(map[string]bool)
	for _, tile := range first.Tiles {
		tileByCoordinate[[2]int64{tile.ChunkX, tile.ChunkY}] = tile
		seenDistricts[tile.DistrictCode] = true
	}
	for _, tile := range first.Tiles {
		if tile.RoadMask&ConnectionEast != 0 && tile.ChunkX < first.MaximumChunkX {
			require.NotZero(t, tileByCoordinate[[2]int64{tile.ChunkX + 1, tile.ChunkY}].RoadMask&ConnectionWest)
		}
	}
	for _, code := range requiredDistrictCodes {
		require.True(t, seenDistricts[code], code)
	}

	center := tileByCoordinate[[2]int64{0, 0}]
	chunk, err := GenerateDefaultChunk(binding, ruleSet, center)
	require.NoError(t, err)
	require.Equal(t, "4d52eff5a7c63bbda584aaa53223aca98da2c903d98f5fe3dd6e3c28ac537c13", chunk.PayloadHash)
	require.NotEmpty(t, chunk.CanonicalPayload)
	require.NoError(t, ValidateChunkPayload(ruleSet, chunk.Payload))
	covered := 0
	for _, run := range chunk.Payload.TerrainRuns {
		covered += run.Length
	}
	require.Equal(t, int(DefaultChunkSize*DefaultChunkSize), covered)

	changedBinding := binding
	changedBinding.WorldSeed++
	changed, err := GenerateDefaultOvermap(changedBinding, requiredDistrictCodes)
	require.NoError(t, err)
	require.NotEqual(t, first.SeedProof, changed.SeedProof)
}

func TestDefaultMapgenRejectsInvalidInputsAndPayloads(t *testing.T) {
	t.Parallel()
	registry, err := DefaultRegistry()
	require.NoError(t, err)
	ruleSet, err := registry.Get(DefaultRuleSetID)
	require.NoError(t, err)
	binding, err := DefaultGeneratorBinding("city-f7-v1", 9, ruleSet)
	require.NoError(t, err)

	_, err = GenerateDefaultOvermap(binding, []string{"central"})
	require.ErrorIs(t, err, ErrInvalidGenerationInput)
	_, err = DefaultGeneratorBinding("", 9, ruleSet)
	require.ErrorIs(t, err, ErrInvalidGenerationInput)
	_, err = DefaultGeneratorBinding("city-f7-v1", 0, ruleSet)
	require.ErrorIs(t, err, ErrInvalidGenerationInput)

	overmap, err := GenerateDefaultOvermap(binding, requiredDistrictCodes)
	require.NoError(t, err)
	tampered := overmap.Tiles[0]
	tampered.TileHash = "0" + tampered.TileHash[1:]
	_, err = GenerateDefaultChunk(binding, ruleSet, tampered)
	require.ErrorIs(t, err, ErrInvalidGenerationInput)

	invalid := ChunkPayload{
		Format: ChunkPayloadFormat, Width: 32, Height: 32,
		TerrainRuns: []TerrainRun{{DefinitionID: "terrain.grass", Length: 1023}},
		Furniture:   []FurnitureCell{},
	}
	require.ErrorIs(t, ValidateChunkPayload(ruleSet, invalid), ErrInvalidChunkPayload)
	invalid.TerrainRuns[0].Length = 1024
	invalid.Furniture = []FurnitureCell{
		{X: 2, Y: 2, DefinitionID: "furniture.tree"},
		{X: 1, Y: 2, DefinitionID: "furniture.tree"},
	}
	require.ErrorIs(t, ValidateChunkPayload(ruleSet, invalid), ErrInvalidChunkPayload)
}
