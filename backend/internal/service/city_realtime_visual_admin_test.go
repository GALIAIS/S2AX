package service

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestNormalizeCityRealtimeVisualPackInputRestrictsCurrentControlPlane(t *testing.T) {
	manifest := []byte(`{
  "schema_version": 1,
  "render_mode": "procedural_pixel_v1",
  "logical_tile_px": 16,
  "profile_palettes": {
    "default": {"ground": "#5f8259", "road": "#77736b"},
    "jp.metropolitan": {"ground": "#6b9468", "road": "#6d7370"}
  },
  "semantic_rules": {"terrain": ["grass", "road"]},
  "assets": []
}`)

	normalized, err := normalizeCityRealtimeVisualPackInput(
		"city-pixel-jp", "1.2.3", []string{"jp.metropolitan"}, manifest,
	)
	require.NoError(t, err)
	require.Equal(t, "city-pixel-jp", normalized.packID)
	require.JSONEq(t,
		`{"spatial_profile_ids":["jp.metropolitan"],"semantic_projection_versions":["city-realtime-semantic-pixel-v1"]}`,
		string(normalized.compatibility),
	)
	require.NotContains(t, string(normalized.manifest), "https://")

	_, err = normalizeCityRealtimeVisualPackInput(
		"city-pixel-invalid", "1.2.3", []string{"*", "jp.metropolitan"}, manifest,
	)
	require.ErrorIs(t, err, ErrCityInvalidInput)

	unsafeAssets := []byte(`{
  "schema_version": 1,
  "render_mode": "procedural_pixel_v1",
  "logical_tile_px": 16,
  "profile_palettes": {"default": {"ground": "#5f8259"}},
  "assets": ["atlas-not-deployable"]
}`)
	_, err = normalizeCityRealtimeVisualPackInput(
		"city-pixel-invalid", "1.2.3", []string{"*"}, unsafeAssets,
	)
	require.ErrorIs(t, err, ErrCityInvalidInput)
}

func TestNormalizeCityRealtimeVisualGenerationJobIsStructuredWithoutRawPrompt(t *testing.T) {
	normalized, err := normalizeCityRealtimeVisualGenerationJobInput(CityRealtimeVisualGenerationJobCreateInput{
		PackID: "city-pixel-jp", PackVersion: "1.2.3",
		AssetClass:   "building_exterior",
		SemanticTags: []string{"building.residential", "jp.metropolitan"},
		PixelWidth:   64, PixelHeight: 64, FrameCount: 4,
	})
	require.NoError(t, err)
	require.JSONEq(t, `{
  "schema_version": 1,
  "asset_class": "building_exterior",
  "semantic_tags": ["building.residential", "jp.metropolitan"],
  "pixel_width": 64,
  "pixel_height": 64,
  "frame_count": 4,
  "prompt_template_id": "city-pixel-asset-v1",
  "render_contract_version": "procedural_pixel_v1"
}`, string(normalized.requestSpec))
	require.NotContains(t, string(normalized.requestSpec), "http")
	require.NotContains(t, string(normalized.requestSpec), "data:")
	require.NotContains(t, string(normalized.requestSpec), "source_url")

	_, err = normalizeCityRealtimeVisualGenerationJobInput(CityRealtimeVisualGenerationJobCreateInput{
		PackID: "city-pixel-jp", PackVersion: "1.2.3", AssetClass: "building_exterior",
		SemanticTags: []string{"building.residential"}, PixelWidth: 63, PixelHeight: 64, FrameCount: 1,
	})
	require.ErrorIs(t, err, ErrCityInvalidInput)
}

func TestCityRealtimeVisualGenerationReviewTransitionsAreFailClosed(t *testing.T) {
	require.True(t, cityRealtimeVisualGenerationReviewAllowed("queued", "cancelled"))
	require.False(t, cityRealtimeVisualGenerationReviewAllowed("queued", "approved"))
	require.True(t, cityRealtimeVisualGenerationReviewAllowed("generated", "approved"))
	require.True(t, cityRealtimeVisualGenerationReviewAllowed("reviewing", "rejected"))
	require.False(t, cityRealtimeVisualGenerationReviewAllowed("approved", "cancelled"))
}

func TestCityRealtimeVisualPackSupportsReleasePolicy(t *testing.T) {
	compatibility := []byte(`{
  "spatial_profile_ids": ["*"],
  "semantic_projection_versions": ["city-realtime-semantic-pixel-v1"]
}`)
	require.True(t, cityRealtimeVisualPackSupportsReleasePolicy(compatibility, "*"))
	require.True(t, cityRealtimeVisualPackSupportsReleasePolicy(compatibility, "jp.metropolitan"))

	exactOnly := []byte(`{
  "spatial_profile_ids": ["jp.metropolitan"],
  "semantic_projection_versions": ["city-realtime-semantic-pixel-v1"]
}`)
	require.False(t, cityRealtimeVisualPackSupportsReleasePolicy(exactOnly, "*"))
	require.True(t, cityRealtimeVisualPackSupportsReleasePolicy(exactOnly, "jp.metropolitan"))
	require.False(t, cityRealtimeVisualPackSupportsReleasePolicy(exactOnly, "cn.metropolitan"))
}

func TestLoadCityRealtimeVisualReleasePackFailsClosedForRetiredPolicyTarget(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{})
	require.NoError(t, err)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT pack.status,")).
		WithArgs(cityRealtimeSemanticProjectionVersion, "jp.metropolitan").
		WillReturnRows(sqlmock.NewRows([]string{
			"status", "pack_id", "pack_version", "semantic_projection_version",
			"render_contract_version", "manifest_hash", "asset_set_hash", "compatibility",
		}).AddRow(
			"retired", "city-pixel-jp", "1.2.3", cityRealtimeSemanticProjectionVersion,
			cityRealtimeProceduralPixelRenderContract, strings.Repeat("a", 64), strings.Repeat("b", 64),
			`{"spatial_profile_ids":["jp.metropolitan"],"semantic_projection_versions":["city-realtime-semantic-pixel-v1"]}`,
		))

	_, err = loadCityRealtimeVisualReleasePack(context.Background(), tx, "jp.metropolitan")
	require.ErrorIs(t, err, ErrCitySimulationInvariant)
	mock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}
