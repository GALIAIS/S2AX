package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
)

// Keep one synchronous verification request deliberately bounded.  A world
// can grow indefinitely, but an unbounded HTTP read transaction must not be
// allowed to monopolize the database or regenerate an arbitrary number of
// high-rise sectors.  Operations can verify each immutable region separately
// once a world grows beyond this guardrail.
const cityOpenWorldVerificationMaximumSectors = 256

// CityOpenWorldVerification is a read-only proof that persisted materialized
// facts still match the generator/version/profile binding sealed into a world.
// It does not "repair" facts: immutable maps are never silently rewritten.
type CityOpenWorldVerification struct {
	WorldID                int64     `json:"world_id"`
	SimulationVersion      string    `json:"simulation_version"`
	Scope                  string    `json:"scope"`
	RegionX                *int64    `json:"region_x,omitempty"`
	RegionY                *int64    `json:"region_y,omitempty"`
	CurrentTick            int64     `json:"current_tick"`
	StateHash              string    `json:"state_hash"`
	CanonicalStateVerified bool      `json:"canonical_state_verified"`
	RegionCount            int       `json:"region_count"`
	SectorCount            int       `json:"sector_count"`
	ChunkCount             int       `json:"chunk_count"`
	BuildingCount          int       `json:"building_count"`
	InteriorCount          int       `json:"interior_count"`
	PortalCount            int       `json:"portal_count"`
	VerifiedAt             time.Time `json:"verified_at"`
}

type cityOpenWorldVerificationCounts struct {
	chunks    int
	buildings int
	interiors int
	portals   int
}

type cityOpenWorldVerificationScope struct {
	RegionX int64
	RegionY int64
}

// VerifyOpenWorldMaterialization replays every already materialized sector
// through its immutable versioned generator and compares the complete stored
// projection.  This is the safe V2/V3 equivalent of projection recovery: the
// result is a proof or an invariant failure, never a hidden mutation.
func (s *CityEconomyService) VerifyOpenWorldMaterialization(
	ctx context.Context,
	userID, worldID int64,
) (*CityOpenWorldVerification, error) {
	return s.verifyOpenWorldMaterialization(ctx, userID, worldID, nil)
}

// VerifyOpenWorldRegionMaterialization is the bounded operational verifier
// for one immutable region.  It intentionally does not recalculate the
// whole-world canonical hash: that proof is only meaningful when every
// materialized fact participates in the same snapshot.
func (s *CityEconomyService) VerifyOpenWorldRegionMaterialization(
	ctx context.Context,
	userID, worldID, regionX, regionY int64,
) (*CityOpenWorldVerification, error) {
	return s.verifyOpenWorldMaterialization(ctx, userID, worldID, &cityOpenWorldVerificationScope{
		RegionX: regionX,
		RegionY: regionY,
	})
}

func (s *CityEconomyService) verifyOpenWorldMaterialization(
	ctx context.Context,
	userID, worldID int64,
	scope *cityOpenWorldVerificationScope,
) (*CityOpenWorldVerification, error) {
	if userID <= 0 || worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	if scope != nil && (!cityOpenWorldValidRegionCoordinate(scope.RegionX) || !cityOpenWorldValidRegionCoordinate(scope.RegionY)) {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "open_world_region"})
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin open-world verification snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = authorizeCityWorldRead(ctx, tx, userID, worldID); err != nil {
		return nil, err
	}
	var currentTick int64
	var storedStateHash sql.NullString
	if err = tx.QueryRowContext(ctx, `
SELECT current_tick, state_hash
FROM city_worlds WHERE id = $1`, worldID).Scan(&currentTick, &storedStateHash); err != nil {
		return nil, fmt.Errorf("load open-world verification world state: %w", err)
	}
	if !storedStateHash.Valid || storedStateHash.String == "" {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_state_hash"})
	}

	simulationVersion, binding, profile, _, err := loadCityOpenWorldRegionGenerator(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	regions, err := loadCityOpenWorldRegionsForVerification(ctx, tx, worldID, scope)
	if err != nil {
		return nil, err
	}
	sectors, err := loadCityOpenWorldSectorsForVerification(ctx, tx, worldID, scope)
	if err != nil {
		return nil, err
	}
	if len(regions) == 0 || len(sectors) == 0 {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_materialization"})
	}
	if scope == nil && len(sectors) > cityOpenWorldVerificationMaximumSectors {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "open_world_verification_sector_count"})
	}

	type regionKey struct{ x, y int64 }
	plans := make(map[regionKey]*cityspatial.GeneratedWorldgenPlan, len(regions))
	for _, region := range regions {
		if err = validateCityOpenWorldRegionFact(region); err != nil {
			return nil, err
		}
		key := regionKey{x: region.RegionX, y: region.RegionY}
		if _, duplicate := plans[key]; duplicate {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_region_duplicate"})
		}
		plan, generationErr := cityspatial.GenerateWorldgenPlan(binding, profile, cityOpenWorldRegionBounds(region.RegionX, region.RegionY))
		if generationErr != nil || plan.BaselineHash != region.PlanHash {
			if generationErr != nil {
				return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_region_plan"}).WithCause(generationErr)
			}
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_region_hash"})
		}
		plans[key] = plan
	}

	counts := cityOpenWorldVerificationCounts{}
	sectorsByRegion := make(map[regionKey]int, len(regions))
	for _, sector := range sectors {
		if err = validateCityOpenWorldSectorFact(sector); err != nil {
			return nil, err
		}
		regionX, regionY := cityOpenWorldRegionForSector(sector.SectorX, sector.SectorY)
		plan := plans[regionKey{x: regionX, y: regionY}]
		if plan == nil || sector.PlanHash != plan.BaselineHash {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_sector_region"})
		}
		sectorsByRegion[regionKey{x: regionX, y: regionY}]++
		surface, generationErr := cityOpenWorldSurfaceForVersion(
			simulationVersion, plan, cityOpenWorldSectorBounds(sector.SectorX, sector.SectorY),
		)
		if generationErr != nil {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_sector_surface"}).WithCause(generationErr)
		}
		if surface.PlanHash != sector.PlanHash || surface.ContentHash != sector.ContentHash {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_sector_hash"})
		}
		sectorCounts, verifyErr := verifyCityOpenWorldMaterializedSector(
			ctx, tx, worldID, sector.SectorX, sector.SectorY, surface,
		)
		if verifyErr != nil {
			return nil, verifyErr
		}
		counts.chunks += sectorCounts.chunks
		counts.buildings += sectorCounts.buildings
		counts.interiors += sectorCounts.interiors
		counts.portals += sectorCounts.portals
	}
	for key := range plans {
		if sectorsByRegion[key] == 0 {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_region_sector"})
		}
	}
	canonicalStateVerified := false
	if scope == nil {
		state, _, actualStateHash, canonicalErr := canonicalCityWorldState(ctx, tx, worldID)
		if canonicalErr != nil {
			return nil, canonicalErr
		}
		if state.SimulationVersion != simulationVersion || actualStateHash != storedStateHash.String {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_canonical_state"})
		}
		canonicalStateVerified = true
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit open-world verification snapshot: %w", err)
	}
	result := &CityOpenWorldVerification{
		WorldID: worldID, SimulationVersion: simulationVersion, Scope: "world", CurrentTick: currentTick,
		StateHash: storedStateHash.String, CanonicalStateVerified: canonicalStateVerified,
		RegionCount: len(regions), SectorCount: len(sectors),
		ChunkCount: counts.chunks, BuildingCount: counts.buildings, InteriorCount: counts.interiors,
		PortalCount: counts.portals, VerifiedAt: time.Now().UTC(),
	}
	if scope != nil {
		result.Scope = "region"
		result.RegionX = &scope.RegionX
		result.RegionY = &scope.RegionY
	}
	return result, nil
}

func validateCityOpenWorldRegionFact(region CityOpenWorldRegion) error {
	if !cityOpenWorldValidRegionCoordinate(region.RegionX) || !cityOpenWorldValidRegionCoordinate(region.RegionY) ||
		region.Epoch != 1 || region.ChunkSize != cityspatial.DefaultChunkSize ||
		region.RegionSizeChunks != cityOpenWorldRegionSizeChunks || region.Status != "generated" ||
		region.PlanHash == "" || region.GeneratedTick < 0 || region.Revision != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_region_fact"})
	}
	return nil
}

func validateCityOpenWorldSectorFact(sector CityOpenWorldSector) error {
	if !cityOpenWorldValidSectorCoordinate(sector.SectorX) || !cityOpenWorldValidSectorCoordinate(sector.SectorY) ||
		sector.Epoch != 1 || sector.ChunkSize != cityspatial.DefaultChunkSize ||
		sector.SectorSizeChunks != cityOpenWorldSectorSizeChunks || sector.Status != "generated" ||
		sector.PlanHash == "" || sector.ContentHash == "" || sector.GeneratedTick < 0 || sector.Revision != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_sector_fact"})
	}
	return nil
}

func cityOpenWorldValidSectorCoordinate(value int64) bool {
	return value >= -cityOpenWorldMaximumSectorAbs && value <= cityOpenWorldMaximumSectorAbs
}

func cityOpenWorldValidRegionCoordinate(value int64) bool {
	return value >= -cityOpenWorldMaximumSectorAbs/cityOpenWorldRegionsPerAxis &&
		value <= cityOpenWorldMaximumSectorAbs/cityOpenWorldRegionsPerAxis
}

func loadCityOpenWorldRegionsForVerification(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	scope *cityOpenWorldVerificationScope,
) ([]CityOpenWorldRegion, error) {
	if scope == nil {
		return loadCityOpenWorldRegions(ctx, queryer, worldID)
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT region_x, region_y, epoch, chunk_size, region_size_chunks, status,
       plan_hash, generated_tick, revision, created_at, updated_at
FROM city_open_world_regions
WHERE world_id = $1 AND region_x = $2 AND region_y = $3
ORDER BY epoch ASC`, worldID, scope.RegionX, scope.RegionY)
	if err != nil {
		return nil, fmt.Errorf("load scoped open-world region: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldRegion, 0, 1)
	for rows.Next() {
		var item CityOpenWorldRegion
		if err = rows.Scan(&item.RegionX, &item.RegionY, &item.Epoch, &item.ChunkSize,
			&item.RegionSizeChunks, &item.Status, &item.PlanHash, &item.GeneratedTick,
			&item.Revision, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan scoped open-world region: %w", err)
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate scoped open-world region: %w", err)
	}
	return items, nil
}

func loadCityOpenWorldSectorsForVerification(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	scope *cityOpenWorldVerificationScope,
) ([]CityOpenWorldSector, error) {
	if scope == nil {
		return loadCityOpenWorldSectors(ctx, queryer, worldID)
	}
	minimumSectorX := scope.RegionX * cityOpenWorldRegionsPerAxis
	minimumSectorY := scope.RegionY * cityOpenWorldRegionsPerAxis
	rows, err := queryer.QueryContext(ctx, `
SELECT sector_x, sector_y, epoch, chunk_size, sector_size_chunks, status, plan_hash,
       content_hash, generated_tick, revision, created_at, updated_at
FROM city_open_world_sectors
WHERE world_id = $1
  AND sector_x BETWEEN $2 AND $3
  AND sector_y BETWEEN $4 AND $5
ORDER BY epoch ASC, sector_y ASC, sector_x ASC`,
		worldID,
		minimumSectorX, minimumSectorX+cityOpenWorldRegionsPerAxis-1,
		minimumSectorY, minimumSectorY+cityOpenWorldRegionsPerAxis-1,
	)
	if err != nil {
		return nil, fmt.Errorf("load scoped open-world sectors: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]CityOpenWorldSector, 0, int(cityOpenWorldRegionsPerAxis*cityOpenWorldRegionsPerAxis))
	for rows.Next() {
		var item CityOpenWorldSector
		if err = rows.Scan(&item.SectorX, &item.SectorY, &item.Epoch, &item.ChunkSize, &item.SectorSizeChunks,
			&item.Status, &item.PlanHash, &item.ContentHash, &item.GeneratedTick, &item.Revision,
			&item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan scoped open-world sector: %w", err)
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate scoped open-world sector: %w", err)
	}
	return items, nil
}

func verifyCityOpenWorldMaterializedSector(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, sectorX, sectorY int64,
	surface *cityspatial.GeneratedOpenWorldSurfaceSector,
) (cityOpenWorldVerificationCounts, error) {
	if surface == nil {
		return cityOpenWorldVerificationCounts{}, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_surface"})
	}
	counts := cityOpenWorldVerificationCounts{}
	chunkRows, err := queryer.QueryContext(ctx, `
SELECT sector_x, sector_y, chunk_x, chunk_y, z, payload, payload_hash, revision
FROM city_open_world_chunks
WHERE world_id = $1 AND z = $2
  AND chunk_x BETWEEN $3 AND $4 AND chunk_y BETWEEN $5 AND $6
ORDER BY chunk_y ASC, chunk_x ASC`,
		worldID, cityspatial.SurfaceZ,
		surface.Bounds.MinimumChunkX, surface.Bounds.MaximumChunkX,
		surface.Bounds.MinimumChunkY, surface.Bounds.MaximumChunkY,
	)
	if err != nil {
		return counts, fmt.Errorf("load open-world sector chunks: %w", err)
	}
	defer func() { _ = chunkRows.Close() }()
	expectedChunks := make(map[string]cityspatial.GeneratedOpenWorldChunk, len(surface.Chunks))
	for _, chunk := range surface.Chunks {
		key := cityOpenWorldChunkKey(chunk.Coordinate.X, chunk.Coordinate.Y, chunk.Coordinate.Z)
		if _, duplicate := expectedChunks[key]; duplicate {
			return counts, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_generated_chunk"})
		}
		expectedChunks[key] = chunk
	}
	for chunkRows.Next() {
		var stored CityOpenWorldChunk
		var storedSectorX, storedSectorY int64
		var payload []byte
		if err = chunkRows.Scan(&storedSectorX, &storedSectorY, &stored.ChunkX, &stored.ChunkY, &stored.Z,
			&payload, &stored.PayloadHash, &stored.Revision); err != nil {
			return counts, fmt.Errorf("scan open-world sector chunk: %w", err)
		}
		if storedSectorX != sectorX || storedSectorY != sectorY ||
			json.Unmarshal(payload, &stored.Payload) != nil ||
			cityspatial.ValidateOpenWorldChunkPayload(stored.Payload) != nil {
			return counts, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_chunk_fact"})
		}
		canonicalPayload, marshalErr := json.Marshal(stored.Payload)
		if marshalErr != nil || cityOpenWorldPayloadHash(canonicalPayload) != stored.PayloadHash {
			return counts, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_chunk_hash"})
		}
		key := cityOpenWorldChunkKey(stored.ChunkX, stored.ChunkY, stored.Z)
		expected, found := expectedChunks[key]
		if !found || expected.PayloadHash != stored.PayloadHash || !reflect.DeepEqual(expected.Payload, stored.Payload) || stored.Revision != 1 {
			return counts, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_chunk_fact"})
		}
		delete(expectedChunks, key)
		counts.chunks++
	}
	if err = chunkRows.Err(); err != nil {
		return counts, fmt.Errorf("iterate open-world sector chunks: %w", err)
	}
	if len(expectedChunks) != 0 {
		return counts, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_chunk_count"})
	}

	// A building is physically owned by the sector containing its entrance.
	// The surface generator may include a footprint that crosses a sector edge,
	// but that neighboring sector must never write a second building, interior
	// stack, or portal topology row.
	ownedBuildingCodes := make(map[string]struct{})
	expectedBuildings := make(map[string]cityspatial.GeneratedWorldgenBuilding)
	for _, building := range surface.Buildings {
		ownerX, ownerY := cityOpenWorldSectorForWorldPoint(building.Entrance.X, building.Entrance.Y)
		if ownerX == sectorX && ownerY == sectorY {
			expectedBuildings[building.Code] = building
			ownedBuildingCodes[building.Code] = struct{}{}
		}
	}
	buildingRows, err := queryer.QueryContext(ctx, `
SELECT code, city_code, lot_code, primary_use, archetype_code, layout_style,
       floor_count, entrance_x, entrance_y, entrance_z, footprint, footprint_hash, revision
FROM city_open_world_buildings
WHERE world_id = $1 AND sector_x = $2 AND sector_y = $3 AND epoch = 1
ORDER BY code ASC`, worldID, sectorX, sectorY)
	if err != nil {
		return counts, fmt.Errorf("load open-world sector buildings: %w", err)
	}
	for buildingRows.Next() {
		var stored cityspatial.GeneratedWorldgenBuilding
		var footprint []byte
		var footprintHash string
		var revision int64
		if err = buildingRows.Scan(
			&stored.Code, &stored.CityCode, &stored.LotCode, &stored.PrimaryUse, &stored.ArchetypeCode,
			&stored.LayoutStyle, &stored.FloorCount, &stored.Entrance.X, &stored.Entrance.Y, &stored.Entrance.Z,
			&footprint, &footprintHash, &revision,
		); err != nil {
			_ = buildingRows.Close()
			return counts, fmt.Errorf("scan open-world sector building: %w", err)
		}
		if err = json.Unmarshal(footprint, &stored.Footprint); err != nil {
			_ = buildingRows.Close()
			return counts, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_building_fact"}).WithCause(err)
		}
		expected, found := expectedBuildings[stored.Code]
		if !found || revision != 1 || footprintHash != cityOpenWorldFootprintHash(stored.Footprint) || !reflect.DeepEqual(expected, stored) {
			_ = buildingRows.Close()
			return counts, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_building_fact"})
		}
		delete(expectedBuildings, stored.Code)
		counts.buildings++
	}
	if err = buildingRows.Err(); err != nil {
		_ = buildingRows.Close()
		return counts, fmt.Errorf("iterate open-world sector buildings: %w", err)
	}
	_ = buildingRows.Close()
	if len(expectedBuildings) != 0 {
		return counts, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_building_count"})
	}

	// Build the expected floor set from the immutable ownership rule rather
	// than from table rows, so missing or foreign floors are detected
	// deterministically even after the building comparison map is drained.
	expectedInteriors := make(map[string]cityspatial.GeneratedWorldgenBuildingInterior)
	for _, interior := range surface.Interiors {
		if _, owned := ownedBuildingCodes[interior.BuildingCode]; owned {
			expectedInteriors[cityOpenWorldInteriorKey(interior.BuildingCode, interior.FloorIndex)] = interior
		}
	}
	interiorRows, err := queryer.QueryContext(ctx, `
SELECT interior.building_code, interior.floor_index, interior.z, interior.layout_version,
       interior.layout_style, interior.cells, interior.content_hash, interior.revision
FROM city_open_world_building_interiors AS interior
JOIN city_open_world_buildings AS building
  ON building.world_id = interior.world_id AND building.code = interior.building_code
WHERE interior.world_id = $1 AND building.sector_x = $2 AND building.sector_y = $3 AND building.epoch = 1
ORDER BY interior.building_code ASC, interior.floor_index ASC`, worldID, sectorX, sectorY)
	if err != nil {
		return counts, fmt.Errorf("load open-world sector interiors: %w", err)
	}
	for interiorRows.Next() {
		var stored cityspatial.GeneratedWorldgenBuildingInterior
		var cells []byte
		var revision int64
		if err = interiorRows.Scan(&stored.BuildingCode, &stored.FloorIndex, &stored.Z, &stored.LayoutVersion,
			&stored.LayoutStyle, &cells, &stored.ContentHash, &revision); err != nil {
			_ = interiorRows.Close()
			return counts, fmt.Errorf("scan open-world sector interior: %w", err)
		}
		if err = json.Unmarshal(cells, &stored.Cells); err != nil {
			_ = interiorRows.Close()
			return counts, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_interior_fact"}).WithCause(err)
		}
		hash, hashErr := cityspatial.ComputeWorldgenBuildingInteriorHash(&stored)
		if hashErr != nil || hash != stored.ContentHash {
			_ = interiorRows.Close()
			if hashErr != nil {
				return counts, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_interior_hash"}).WithCause(hashErr)
			}
			return counts, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_interior_hash"})
		}
		expected, found := expectedInteriors[cityOpenWorldInteriorKey(stored.BuildingCode, stored.FloorIndex)]
		if !found || revision != 1 || !reflect.DeepEqual(expected, stored) {
			_ = interiorRows.Close()
			return counts, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_interior_fact"})
		}
		delete(expectedInteriors, cityOpenWorldInteriorKey(stored.BuildingCode, stored.FloorIndex))
		counts.interiors++
	}
	if err = interiorRows.Err(); err != nil {
		_ = interiorRows.Close()
		return counts, fmt.Errorf("iterate open-world sector interiors: %w", err)
	}
	_ = interiorRows.Close()
	if len(expectedInteriors) != 0 {
		return counts, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_interior_count"})
	}

	expectedPortals := make(map[string]cityspatial.GeneratedOpenWorldPortal)
	for _, portal := range surface.Portals {
		if _, owned := ownedBuildingCodes[portal.BuildingCode]; owned {
			expectedPortals[portal.Code] = portal
		}
	}
	portalRows, err := queryer.QueryContext(ctx, `
SELECT portal.code, portal.building_code, portal.portal_type,
       portal.from_floor_index, portal.to_floor_index,
       portal.from_x, portal.from_y, portal.from_z,
       portal.to_x, portal.to_y, portal.to_z,
       portal.bidirectional, portal.topology_hash, portal.revision
FROM city_open_world_portals AS portal
JOIN city_open_world_buildings AS building
  ON building.world_id = portal.world_id AND building.code = portal.building_code
WHERE portal.world_id = $1 AND building.sector_x = $2 AND building.sector_y = $3 AND building.epoch = 1
ORDER BY portal.code ASC`, worldID, sectorX, sectorY)
	if err != nil {
		return counts, fmt.Errorf("load open-world sector portals: %w", err)
	}
	for portalRows.Next() {
		var stored cityspatial.GeneratedOpenWorldPortal
		var topologyHash string
		var revision int64
		if err = portalRows.Scan(
			&stored.Code, &stored.BuildingCode, &stored.PortalType, &stored.FromFloorIndex, &stored.ToFloorIndex,
			&stored.From.X, &stored.From.Y, &stored.From.Z, &stored.To.X, &stored.To.Y, &stored.To.Z,
			&stored.Bidirectional, &topologyHash, &revision,
		); err != nil {
			_ = portalRows.Close()
			return counts, fmt.Errorf("scan open-world sector portal: %w", err)
		}
		hash, hashErr := cityspatial.ComputeOpenWorldPortalHash(stored)
		if hashErr != nil || hash != topologyHash {
			_ = portalRows.Close()
			if hashErr != nil {
				return counts, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_portal_hash"}).WithCause(hashErr)
			}
			return counts, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_portal_hash"})
		}
		expected, found := expectedPortals[stored.Code]
		if !found || revision != 1 || !reflect.DeepEqual(expected, stored) {
			_ = portalRows.Close()
			return counts, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_portal_fact"})
		}
		delete(expectedPortals, stored.Code)
		counts.portals++
	}
	if err = portalRows.Err(); err != nil {
		_ = portalRows.Close()
		return counts, fmt.Errorf("iterate open-world sector portals: %w", err)
	}
	_ = portalRows.Close()
	if len(expectedPortals) != 0 {
		return counts, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "open_world_portal_count"})
	}
	return counts, nil
}

func cityOpenWorldChunkKey(x, y int64, z int32) string {
	return fmt.Sprintf("%d/%d/%d", x, y, z)
}

func cityOpenWorldInteriorKey(buildingCode string, floorIndex int32) string {
	return fmt.Sprintf("%s/%d", buildingCode, floorIndex)
}
