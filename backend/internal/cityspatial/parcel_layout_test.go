package cityspatial

import "testing"

import "github.com/stretchr/testify/require"

func TestGenerateParcelLayoutsIsDeterministicAndNeverOverwritesStructure(t *testing.T) {
	foundation := testLandFoundation(t)
	buildingLayouts, err := GenerateBuildingLayouts(foundation.Buildings, foundation.Portals)
	require.NoError(t, err)

	first, err := GenerateParcelLayouts(
		foundation.Parcels, foundation.Buildings, buildingLayouts, foundation.Portals, SurfaceZ,
	)
	require.NoError(t, err)
	second, err := GenerateParcelLayouts(
		foundation.Parcels, foundation.Buildings, buildingLayouts, foundation.Portals, SurfaceZ,
	)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.NotEmpty(t, first)

	parcelByCode := make(map[string]GeneratedParcel, len(foundation.Parcels))
	for _, parcel := range foundation.Parcels {
		parcelByCode[parcel.Code] = parcel
	}
	structure := make(map[buildingLayoutPoint]struct{})
	for _, layout := range buildingLayouts {
		for _, cell := range layout.Cells {
			if cell.Z == SurfaceZ {
				structure[buildingLayoutPoint{x: cell.X, y: cell.Y}] = struct{}{}
			}
		}
	}
	for _, layout := range first {
		parcel, found := parcelByCode[layout.ParcelCode]
		require.True(t, found)
		bounds, err := parcelLayoutWorldBounds(parcel.Geometry)
		require.NoError(t, err)
		for _, cell := range layout.Cells {
			require.Equal(t, SurfaceZ, cell.Z)
			require.GreaterOrEqual(t, cell.X, bounds.minX)
			require.LessOrEqual(t, cell.X, bounds.maxX)
			require.GreaterOrEqual(t, cell.Y, bounds.minY)
			require.LessOrEqual(t, cell.Y, bounds.maxY)
			_, overlap := structure[buildingLayoutPoint{x: cell.X, y: cell.Y}]
			require.Falsef(t, overlap, "parcel detail %s overwrote a building cell", layout.ParcelCode)
		}
	}
}
