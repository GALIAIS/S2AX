package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCitySpatialOvermapChunkDefinesVersionedImmutableFacts(t *testing.T) {
	content, err := FS.ReadFile("197_city_spatial_overmap_chunk.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"create table if not exists city_spatial_profiles",
		"create table if not exists city_overmap_tiles",
		"create table if not exists city_map_chunks",
		"create table if not exists city_spatial_mutations",
		"create table if not exists city_spatial_mutation_lines",
		"city_f7_initialization_write_enabled",
		"city_spatial_mutation_write_enabled",
		"guard_city_spatial_profile_projection",
		"guard_city_overmap_tile_projection",
		"guard_city_map_chunk_projection",
		"guard_city_spatial_mutation_write",
		"city_spatial_mutation_line_immutable_guard",
		"assert_city_spatial_mutation_committed",
		"assert_city_spatial_foundation",
		"city spatial mutation does not match its applied command",
		"mutation_row.metadata ->> 'generation_proof'",
		"(to_jsonb(new) ->> 'world_id')::bigint",
		"city-f7-v1",
		"f6_v3_to_f7_v1",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "update city_worlds\nset simulation_version")
}
