package cityspatial

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorldgenStyleCatalogIsStableAndStylesGenerateDifferentPlans(t *testing.T) {
	profiles, err := ListWorldgenProfiles()
	require.NoError(t, err)
	require.Len(t, profiles, 3)

	jp, err := WorldgenProfileByID(WorldgenProfileJapanMetropolitan)
	require.NoError(t, err)
	cn, err := WorldgenProfileByID(WorldgenProfileChinaMetropolitan)
	require.NoError(t, err)
	require.NotEqual(t, jp.ContentHash, cn.ContentHash)
	require.Less(t, jp.LocalStreetWidth, cn.LocalStreetWidth)

	jpBinding, err := DefaultOpenWorldgenBinding("city-openworld-v1", 8110042, jp)
	require.NoError(t, err)
	cnBinding, err := DefaultOpenWorldgenBinding("city-openworld-v1", 8110042, cn)
	require.NoError(t, err)
	require.NotEqual(t, jpBinding.SpatialRootHash, cnBinding.SpatialRootHash)

	bounds := WorldgenBounds{MinimumChunkX: -4, MaximumChunkX: 4, MinimumChunkY: -4, MaximumChunkY: 4, Z: SurfaceZ}
	jpPlan, err := GenerateWorldgenPlan(jpBinding, jp, bounds)
	require.NoError(t, err)
	cnPlan, err := GenerateWorldgenPlan(cnBinding, cn, bounds)
	require.NoError(t, err)
	require.NotEqual(t, jpPlan.BaselineHash, cnPlan.BaselineHash)
}
