package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	"github.com/stretchr/testify/require"
)

func TestGetOpenWorldBuildingInteriorAuthorizesAndVerifiesContentHash(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	})

	cells := []cityspatial.GeneratedWorldgenInteriorCell{
		{X: 10, Y: 20, Z: 0, Kind: cityspatial.BuildingLayoutCellFloor},
		{X: 11, Y: 20, Z: 0, Kind: cityspatial.BuildingLayoutCellDoor},
	}
	payload, err := json.Marshal(cells)
	require.NoError(t, err)
	interior := cityspatial.GeneratedWorldgenBuildingInterior{
		BuildingCode: "building_core_001", FloorIndex: 0, Z: 0,
		LayoutVersion: cityspatial.DefaultWorldgenInteriorVersion, LayoutStyle: "rowhouse", Cells: cells,
	}
	hash, err := cityspatial.ComputeWorldgenBuildingInteriorHash(&interior)
	require.NoError(t, err)

	mock.ExpectQuery("SELECT 1 FROM city_members").WithArgs(int64(9), int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(1))
	mock.ExpectQuery("SELECT building_code, floor_index, z, layout_version").
		WithArgs(int64(9), "building_core_001", int32(0)).
		WillReturnRows(sqlmock.NewRows([]string{
			"building_code", "floor_index", "z", "layout_version", "layout_style", "cells", "content_hash", "revision",
		}).AddRow("building_core_001", 0, 0, cityspatial.DefaultWorldgenInteriorVersion, "rowhouse", payload, hash, 1))

	service := NewCityEconomyService(db)
	actual, err := service.GetOpenWorldBuildingInterior(context.Background(), 41, 9, "building_core_001", 0)
	require.NoError(t, err)
	require.Equal(t, hash, actual.ContentHash)
	require.Equal(t, cells, actual.Cells)
}

func TestListOpenWorldBuildingPortalsAuthorizesAndVerifiesTopologyHash(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	})

	generated := cityspatial.GeneratedOpenWorldPortal{
		Code: "building_core_001.stairs.00.01", BuildingCode: "building_core_001", PortalType: "stairs",
		FromFloorIndex: 0, ToFloorIndex: 1,
		From: cityspatial.WorldgenPoint{X: 10, Y: 20, Z: 0},
		To:   cityspatial.WorldgenPoint{X: 11, Y: 20, Z: 1}, Bidirectional: true,
	}
	hash, err := cityspatial.ComputeOpenWorldPortalHash(generated)
	require.NoError(t, err)

	mock.ExpectQuery("SELECT 1 FROM city_members").WithArgs(int64(9), int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(1))
	mock.ExpectQuery("SELECT code, building_code, portal_type").
		WithArgs(int64(9), "building_core_001").
		WillReturnRows(sqlmock.NewRows([]string{
			"code", "building_code", "portal_type", "from_floor_index", "to_floor_index",
			"from_x", "from_y", "from_z", "to_x", "to_y", "to_z", "bidirectional", "topology_hash", "revision",
		}).AddRow(
			generated.Code, generated.BuildingCode, generated.PortalType, generated.FromFloorIndex, generated.ToFloorIndex,
			generated.From.X, generated.From.Y, generated.From.Z, generated.To.X, generated.To.Y, generated.To.Z,
			generated.Bidirectional, hash, 1,
		))

	service := NewCityEconomyService(db)
	items, err := service.ListOpenWorldBuildingPortals(context.Background(), 41, 9, "building_core_001")
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, hash, items[0].TopologyHash)
	require.Equal(t, generated.To, items[0].To)
}

func TestOpenWorldV2SectorCoordinatesUseStableFourByFourRegions(t *testing.T) {
	cases := []struct {
		sectorX int64
		sectorY int64
		regionX int64
		regionY int64
	}{
		{sectorX: 0, sectorY: 0, regionX: 0, regionY: 0},
		{sectorX: 3, sectorY: 3, regionX: 0, regionY: 0},
		{sectorX: 4, sectorY: 0, regionX: 1, regionY: 0},
		{sectorX: -1, sectorY: -1, regionX: -1, regionY: -1},
		{sectorX: -4, sectorY: -5, regionX: -1, regionY: -2},
	}
	for _, item := range cases {
		regionX, regionY := cityOpenWorldRegionForSector(item.sectorX, item.sectorY)
		require.Equal(t, item.regionX, regionX)
		require.Equal(t, item.regionY, regionY)
		bounds := cityOpenWorldRegionBounds(regionX, regionY)
		sectorBounds := cityOpenWorldSectorBounds(item.sectorX, item.sectorY)
		require.GreaterOrEqual(t, sectorBounds.MinimumChunkX, bounds.MinimumChunkX)
		require.LessOrEqual(t, sectorBounds.MaximumChunkX, bounds.MaximumChunkX)
		require.GreaterOrEqual(t, sectorBounds.MinimumChunkY, bounds.MinimumChunkY)
		require.LessOrEqual(t, sectorBounds.MaximumChunkY, bounds.MaximumChunkY)
	}
}

func TestNormalizeOpenWorldV2SectorMaterializationRejectsUnboundedCoordinates(t *testing.T) {
	payload, handled, err := normalizeCityOpenWorldCommand(
		CityCommandTypeOpenWorldSectorMaterialize,
		json.RawMessage(`{"sector_x":4,"sector_y":-5}`),
	)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, cityOpenWorldSectorMaterializePayload{SectorX: 4, SectorY: -5}, payload)

	_, handled, err = normalizeCityOpenWorldCommand(
		CityCommandTypeOpenWorldSectorMaterialize,
		json.RawMessage(`{"sector_x":1000001,"sector_y":0}`),
	)
	require.ErrorIs(t, err, ErrCityInvalidInput)
	require.True(t, handled)
}

func TestLoadOpenWorldSectorsForVerificationScopesExactlyOneRegion(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	})

	mock.ExpectQuery("FROM city_open_world_sectors").
		WithArgs(int64(7), int64(4), int64(7), int64(-4), int64(-1)).
		WillReturnRows(sqlmock.NewRows([]string{
			"sector_x", "sector_y", "epoch", "chunk_size", "sector_size_chunks", "status", "plan_hash",
			"content_hash", "generated_tick", "revision", "created_at", "updated_at",
		}))

	items, err := loadCityOpenWorldSectorsForVerification(context.Background(), db, 7, &cityOpenWorldVerificationScope{
		RegionX: 1,
		RegionY: -1,
	})
	require.NoError(t, err)
	require.Empty(t, items)
}

func TestVerifyOpenWorldMaterializedSectorRejectsWrongChunkOwner(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	})

	payload := cityspatial.OpenWorldChunkPayload{
		Format: cityspatial.OpenWorldChunkPayloadFormat,
		Width:  int(cityspatial.DefaultChunkSize),
		Height: int(cityspatial.DefaultChunkSize),
		TerrainRuns: []cityspatial.TerrainRun{{
			DefinitionID: "terrain.ground",
			Length:       int(cityspatial.DefaultChunkSize * cityspatial.DefaultChunkSize),
		}},
	}
	require.NoError(t, cityspatial.ValidateOpenWorldChunkPayload(payload))
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	chunk := cityspatial.GeneratedOpenWorldChunk{
		Coordinate: cityspatial.ChunkCoordinate{X: 0, Y: 0, Z: cityspatial.SurfaceZ},
		Payload:    payload, PayloadHash: cityOpenWorldPayloadHash(raw),
	}
	surface := &cityspatial.GeneratedOpenWorldSurfaceSector{
		Bounds: cityspatial.WorldgenBounds{
			MinimumChunkX: 0, MaximumChunkX: 0, MinimumChunkY: 0, MaximumChunkY: 0, Z: cityspatial.SurfaceZ,
		},
		Chunks: []cityspatial.GeneratedOpenWorldChunk{chunk},
	}

	mock.ExpectQuery("FROM city_open_world_chunks").
		WithArgs(int64(7), cityspatial.SurfaceZ, int64(0), int64(0), int64(0), int64(0)).
		WillReturnRows(sqlmock.NewRows([]string{
			"sector_x", "sector_y", "chunk_x", "chunk_y", "z", "payload", "payload_hash", "revision",
		}).AddRow(int64(1), int64(0), int64(0), int64(0), cityspatial.SurfaceZ, raw, chunk.PayloadHash, int64(1)))

	_, err = verifyCityOpenWorldMaterializedSector(context.Background(), db, 7, 0, 0, surface)
	require.ErrorIs(t, err, ErrCitySimulationInvariant)
}
