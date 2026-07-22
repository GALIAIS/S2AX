package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRealtimeVisualPackMigrationSeparatesImmutableContentFromWorldState(t *testing.T) {
	content, err := FS.ReadFile("255_city_realtime_visual_pack_foundation.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"create table if not exists city_visual_packs",
		"create table if not exists city_visual_assets",
		"create table if not exists city_visual_asset_variants",
		"create table if not exists city_visual_generation_jobs",
		"create table if not exists city_world_visual_bindings",
		"city-pixel-core",
		"procedural_pixel_v1",
		"city_realtime_visual_binding_write_enabled",
		"guard_city_realtime_visual_binding",
		"city_visual_manifest_is_safe",
		"city_visual_pack_asset_set_hash",
		"asset set hash mismatch",
		"immutable outside genesis initialization",
		"published city visual pack is immutable",
		"generation_required",
	} {
		require.Contains(t, sql, required)
	}

	// The content plane can never reuse or write legacy tick materialization.
	require.NotContains(t, sql, "city_open_world_")
}

func TestCityRealtimeVisualPackControlPlaneMigrationKeepsReleaseSelectionAudited(t *testing.T) {
	content, err := FS.ReadFile("256_city_realtime_visual_pack_control_plane.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	for _, required := range []string{
		"create table if not exists city_visual_pack_release_policies",
		"create table if not exists city_visual_pack_review_events",
		"guard_city_visual_pack_release_policy",
		"release policy requires a published pack",
		"renderer contract is not deployable",
		"city-pixel-core",
		"city_visual_packs_publication_review_guard",
		"generation jobs awaiting secure asset materialisation",
		"review events are append-only",
		"prompt|source_url|source_uri|storage_key|asset_url",
	} {
		require.Contains(t, sql, required)
	}

	// A release policy must not mutate existing shared-world visual bindings or
	// reintroduce legacy tick materialization as a content publication side effect.
	require.NotContains(t, sql, "insert into city_world_visual_bindings")
	require.NotContains(t, sql, "city_open_world_")
}
