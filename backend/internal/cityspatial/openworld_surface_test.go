package cityspatial

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateOpenWorldSurfaceSectorMaterializesGlyphFacts(t *testing.T) {
	profile, err := WorldgenProfileByID(WorldgenProfileChinaMetropolitan)
	require.NoError(t, err)
	binding, err := DefaultOpenWorldgenBinding("city-openworld-v1", 8110042, profile)
	require.NoError(t, err)
	plan, err := GenerateWorldgenPlan(binding, profile, WorldgenBounds{
		MinimumChunkX: -4, MaximumChunkX: 4, MinimumChunkY: -4, MaximumChunkY: 4, Z: SurfaceZ,
	})
	require.NoError(t, err)
	sector, err := GenerateOpenWorldSurfaceSector(plan, WorldgenBounds{
		MinimumChunkX: -1, MaximumChunkX: 1, MinimumChunkY: -1, MaximumChunkY: 1, Z: SurfaceZ,
	})
	require.NoError(t, err)
	require.Len(t, sector.Chunks, 9)
	require.NotEmpty(t, sector.ContentHash)
	require.NotEmpty(t, sector.Buildings)
	require.NotEmpty(t, sector.Interiors)

	foundWall, foundDoor, foundFloor, foundFurniture := false, false, false, false
	for _, interior := range sector.Interiors {
		require.Equal(t, int32(0), interior.FloorIndex)
		require.Equal(t, SurfaceZ, interior.Z)
		require.Equal(t, DefaultWorldgenInteriorVersion, interior.LayoutVersion)
		require.NotEmpty(t, interior.ContentHash)
		for _, cell := range interior.Cells {
			foundFurniture = foundFurniture || cell.Kind == BuildingLayoutCellFurniture
		}
	}
	for _, chunk := range sector.Chunks {
		require.NoError(t, ValidateOpenWorldChunkPayload(chunk.Payload))
		require.NotEmpty(t, chunk.PayloadHash)
		for _, run := range chunk.Payload.TerrainRuns {
			foundFloor = foundFloor || run.DefinitionID == "terrain.floor"
		}
		for _, layer := range chunk.Payload.Layers {
			foundWall = foundWall || layer.DefinitionID == "structure.wall"
			foundDoor = foundDoor || layer.DefinitionID == "portal.door_open"
		}
	}
	require.True(t, foundWall)
	require.True(t, foundDoor)
	require.True(t, foundFloor)
	require.True(t, foundFurniture)

	second, err := GenerateOpenWorldSurfaceSector(plan, sector.Bounds)
	require.NoError(t, err)
	require.Equal(t, sector, second)
}

func TestGenerateOpenWorldSpawnSectorMaterializesDenseGroundInteriors(t *testing.T) {
	profile, err := WorldgenProfileByID(WorldgenProfileJapanMetropolitan)
	require.NoError(t, err)
	binding, err := DefaultOpenWorldgenBinding("city-openworld-v1", 9920041, profile)
	require.NoError(t, err)
	plan, err := GenerateWorldgenPlan(binding, profile, WorldgenBounds{
		MinimumChunkX: -8, MaximumChunkX: 7, MinimumChunkY: -8, MaximumChunkY: 7, Z: SurfaceZ,
	})
	require.NoError(t, err)
	require.NotEmpty(t, plan.Cities)

	spawn := plan.Cities[0].Center
	address, err := SplitWorldCoordinate(WorldCoordinate{X: spawn.X, Y: spawn.Y, Z: spawn.Z}, DefaultChunkSize)
	require.NoError(t, err)
	sectorX := openWorldTestFloorDiv(address.Chunk.X, 8)
	sectorY := openWorldTestFloorDiv(address.Chunk.Y, 8)
	sector, err := GenerateOpenWorldSurfaceSector(plan, WorldgenBounds{
		MinimumChunkX: sectorX * 8, MaximumChunkX: sectorX*8 + 7,
		MinimumChunkY: sectorY * 8, MaximumChunkY: sectorY*8 + 7,
		Z: SurfaceZ,
	})
	require.NoError(t, err)
	require.Len(t, sector.Chunks, 64)
	require.NotEmpty(t, sector.Interiors)

	floorCells, furnishingCells := 0, 0
	for _, interior := range sector.Interiors {
		for _, cell := range interior.Cells {
			if cell.Kind == BuildingLayoutCellFloor {
				floorCells++
			}
			if cell.Kind == BuildingLayoutCellFurniture {
				furnishingCells++
			}
		}
	}
	require.Positive(t, floorCells)
	require.Positive(t, furnishingCells)
}

func TestGenerateOpenWorldSurfaceSectorV2MaterializesEveryBuildingFloor(t *testing.T) {
	profile, err := WorldgenProfileByID(WorldgenProfileChinaMetropolitan)
	require.NoError(t, err)
	binding, err := DefaultOpenWorldgenBindingV2("city-openworld-v2", 8110042, profile)
	require.NoError(t, err)
	plan, err := GenerateWorldgenPlan(binding, profile, WorldgenBounds{
		MinimumChunkX: 0, MaximumChunkX: 31, MinimumChunkY: 0, MaximumChunkY: 31, Z: SurfaceZ,
	})
	require.NoError(t, err)
	require.NotEmpty(t, plan.Cities)

	spawn := plan.Cities[0].Center
	address, err := SplitWorldCoordinate(WorldCoordinate{X: spawn.X, Y: spawn.Y, Z: spawn.Z}, DefaultChunkSize)
	require.NoError(t, err)
	sectorX := openWorldTestFloorDiv(address.Chunk.X, 8)
	sectorY := openWorldTestFloorDiv(address.Chunk.Y, 8)
	sector, err := GenerateOpenWorldSurfaceSectorV2(plan, WorldgenBounds{
		MinimumChunkX: sectorX * 8, MaximumChunkX: sectorX*8 + 7,
		MinimumChunkY: sectorY * 8, MaximumChunkY: sectorY*8 + 7,
		Z: SurfaceZ,
	})
	require.NoError(t, err)

	expectedFloors := 0
	expectedPortals := 0
	for _, building := range sector.Buildings {
		expectedFloors += int(building.FloorCount)
		expectedPortals += int(building.FloorCount)
	}
	require.Positive(t, expectedFloors)
	require.Len(t, sector.Interiors, expectedFloors)
	require.Len(t, sector.Portals, expectedPortals)

	floorsByBuilding := make(map[string]map[int32]struct{})
	for _, interior := range sector.Interiors {
		if floorsByBuilding[interior.BuildingCode] == nil {
			floorsByBuilding[interior.BuildingCode] = make(map[int32]struct{})
		}
		floorsByBuilding[interior.BuildingCode][interior.FloorIndex] = struct{}{}
		require.Equal(t, interior.FloorIndex, interior.Z)
	}
	for _, building := range sector.Buildings {
		require.Len(t, floorsByBuilding[building.Code], int(building.FloorCount))
	}
	portalCodes := make(map[string]struct{}, len(sector.Portals))
	for _, portal := range sector.Portals {
		_, duplicate := portalCodes[portal.Code]
		require.False(t, duplicate)
		portalCodes[portal.Code] = struct{}{}
		require.True(t, portal.Bidirectional)
		hash, hashErr := ComputeOpenWorldPortalHash(portal)
		require.NoError(t, hashErr)
		require.Len(t, hash, 64)
		if portal.PortalType == "stairs" {
			require.Equal(t, portal.FromFloorIndex+1, portal.ToFloorIndex)
			require.Equal(t, portal.From.Z+1, portal.To.Z)
		}
	}

	// The surface remains a lightweight Z=0 projection even though vertical
	// facts are complete and independently hashable.
	for _, chunk := range sector.Chunks {
		require.Equal(t, SurfaceZ, chunk.Coordinate.Z)
	}
	replayed, err := GenerateOpenWorldSurfaceSectorV2(plan, sector.Bounds)
	require.NoError(t, err)
	require.Equal(t, sector, replayed)
}

func openWorldTestFloorDiv(value, divisor int64) int64 {
	quotient := value / divisor
	if value%divisor < 0 {
		quotient--
	}
	return quotient
}
