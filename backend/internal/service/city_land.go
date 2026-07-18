package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrCityLandStateNotFound = infraerrors.NotFound(
	"CITY_LAND_STATE_NOT_FOUND", "city land state not found",
)

type CityLandProfile struct {
	RuleSetID              string `json:"rule_set_id"`
	RuleSetVersion         string `json:"rule_set_version"`
	RuleSetHash            string `json:"rule_set_hash"`
	SpatialOvermapRootHash string `json:"spatial_overmap_root_hash"`
	NominalCellAreaSQM     int64  `json:"nominal_cell_area_sqm"`
	BaselineHash           string `json:"baseline_hash"`
	BaselineTick           int64  `json:"baseline_tick"`
	ZoningRuleCount        int64  `json:"zoning_rule_count"`
	ParcelCount            int64  `json:"parcel_count"`
	BuildingCount          int64  `json:"building_count"`
	UnitPoolCount          int64  `json:"unit_pool_count"`
	HousingAllocationCount int64  `json:"housing_allocation_count"`
	PortalCount            int64  `json:"portal_count"`
	Revision               int64  `json:"revision"`
}

type CityLandState struct {
	Profile            CityLandProfile                          `json:"profile"`
	ZoningRules        []cityspatial.LandZoningRule             `json:"zoning_rules"`
	Parcels            []cityspatial.GeneratedParcel            `json:"parcels"`
	Buildings          []cityspatial.GeneratedBuilding          `json:"buildings"`
	UnitPools          []cityspatial.GeneratedBuildingUnitPool  `json:"unit_pools"`
	HousingAllocations []cityspatial.GeneratedHousingAllocation `json:"housing_allocations"`
	Portals            []cityspatial.GeneratedBuildingPortal    `json:"portals"`
}

type CityLandQueryInput struct {
	UserID   int64
	WorldID  int64
	MinimumX int64
	MaximumX int64
	MinimumY int64
	MaximumY int64
	Z        int32
}

type cityLandHashState = CityLandState

func initializeCityLandFoundation(
	ctx context.Context,
	tx *sql.Tx,
	worldID, seed int64,
	simulationVersion string,
) error {
	if !cityEngineSupportsLand(simulationVersion) {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	spatialProfile, err := loadCitySpatialProfile(ctx, tx, worldID)
	if err != nil {
		return err
	}
	spatialGeneration, err := loadCitySpatialGenerationContext(
		ctx, tx, worldID, simulationVersion, seed, spatialProfile,
	)
	if err != nil {
		return err
	}
	districtIDs, cohortIDs, districts, err := loadCityLandGenerationSeeds(ctx, tx, worldID)
	if err != nil {
		return err
	}
	ruleSet, err := cityspatial.DefaultLandRuleSet()
	if err != nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "land_rule_set"}).WithCause(err)
	}
	landGeneratorVersion, err := cityLandGeneratorVersion(simulationVersion)
	if err != nil {
		return err
	}
	binding, err := cityspatial.DefaultLandGeneratorBinding(
		landGeneratorVersion, seed, spatialProfile.RuleSetHash, spatialProfile.OvermapRootHash, ruleSet,
	)
	if err != nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "land_generator_binding"}).WithCause(err)
	}
	foundation, err := cityspatial.GenerateDefaultLandFoundation(
		binding, ruleSet, spatialGeneration.overmap, districts,
	)
	if err != nil {
		return fmt.Errorf("generate city F7.3 land foundation: %w", err)
	}

	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_f7_initialize_world_id', $1, TRUE)`, strconv.FormatInt(worldID, 10)); err != nil {
		return fmt.Errorf("activate city F7.3 initialization write gate: %w", err)
	}
	var baselineTick int64
	if err = tx.QueryRowContext(ctx, `SELECT current_tick FROM city_worlds WHERE id = $1 FOR UPDATE`, worldID).
		Scan(&baselineTick); err != nil {
		return fmt.Errorf("lock city F7.3 world: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_land_profiles
    (world_id, rule_set_id, rule_set_version, rule_set_hash, spatial_overmap_root_hash,
     nominal_cell_area_sqm, baseline_hash, zoning_rule_count, parcel_count,
     building_count, unit_pool_count, housing_allocation_count, portal_count,
     revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, 1, '{}'::jsonb)`,
		worldID, ruleSet.ID, ruleSet.Version, ruleSet.ContentHash, spatialProfile.OvermapRootHash,
		ruleSet.NominalCellAreaSQM, foundation.BaselineHash, len(ruleSet.Rules), len(foundation.Parcels),
		len(foundation.Buildings), len(foundation.UnitPools), len(foundation.HousingAllocations),
		len(foundation.Portals)); err != nil {
		return fmt.Errorf("insert city F7.3 land profile: %w", err)
	}

	zoningStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_zoning_rules
    (world_id, code, name, primary_use, max_floor_area_ratio_milli,
     max_coverage_milli, max_floors, sqm_per_capacity_unit, status, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'active', '{}'::jsonb)`)
	if err != nil {
		return fmt.Errorf("prepare city F7.3 zoning rule insert: %w", err)
	}
	defer func() { _ = zoningStatement.Close() }()
	for _, rule := range ruleSet.Rules {
		if _, err = zoningStatement.ExecContext(ctx, worldID, rule.Code, rule.Name, rule.PrimaryUse,
			rule.MaxFloorAreaRatioMilli, rule.MaxCoverageMilli, rule.MaxFloors,
			rule.SQMPerCapacityUnit); err != nil {
			return fmt.Errorf("insert city F7.3 zoning rule %s: %w", rule.Code, err)
		}
	}

	parcelIDs := make(map[string]int64, len(foundation.Parcels))
	parcelStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_parcels
    (world_id, district_id, code, zone_code, chunk_x, chunk_y, z,
     local_min_x, local_min_y, local_max_x, local_max_y,
     area_sqm, developable_area_sqm, status, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, 'active', 1, '{}'::jsonb)
RETURNING id`)
	if err != nil {
		return fmt.Errorf("prepare city F7.3 parcel insert: %w", err)
	}
	defer func() { _ = parcelStatement.Close() }()
	for _, parcel := range foundation.Parcels {
		districtID, ok := districtIDs[parcel.DistrictCode]
		if !ok {
			return fmt.Errorf("city F7.3 parcel references unknown district %q", parcel.DistrictCode)
		}
		var parcelID int64
		err = parcelStatement.QueryRowContext(ctx, worldID, districtID, parcel.Code, parcel.ZoneCode,
			parcel.Geometry.ChunkX, parcel.Geometry.ChunkY, parcel.Geometry.Z,
			parcel.Geometry.LocalMinX, parcel.Geometry.LocalMinY,
			parcel.Geometry.LocalMaxX, parcel.Geometry.LocalMaxY,
			parcel.AreaSQM, parcel.DevelopableAreaSQM).Scan(&parcelID)
		if err != nil {
			return fmt.Errorf("insert city F7.3 parcel %s: %w", parcel.Code, err)
		}
		parcelIDs[parcel.Code] = parcelID
	}

	buildingIDs := make(map[string]int64, len(foundation.Buildings))
	buildingStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_buildings
    (world_id, district_id, parcel_id, code, primary_use, chunk_x, chunk_y, footprint_z,
     local_min_x, local_min_y, local_max_x, local_max_y, base_z, top_z, floor_count,
     footprint_area_sqm, floor_area_sqm, capacity_units, occupied_units, quality_milli,
     status, completed_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
        $16, $17, $18, $19, $20, 'active', $21, 1, '{}'::jsonb)
RETURNING id`)
	if err != nil {
		return fmt.Errorf("prepare city F7.3 building insert: %w", err)
	}
	defer func() { _ = buildingStatement.Close() }()
	for _, building := range foundation.Buildings {
		districtID, districtOK := districtIDs[building.DistrictCode]
		parcelID, parcelOK := parcelIDs[building.ParcelCode]
		if !districtOK || !parcelOK {
			return fmt.Errorf("city F7.3 building %s references unknown foundation identity", building.Code)
		}
		var buildingID int64
		err = buildingStatement.QueryRowContext(ctx, worldID, districtID, parcelID,
			building.Code, building.PrimaryUse, building.Footprint.ChunkX, building.Footprint.ChunkY,
			building.Footprint.Z, building.Footprint.LocalMinX, building.Footprint.LocalMinY,
			building.Footprint.LocalMaxX, building.Footprint.LocalMaxY,
			building.BaseZ, building.TopZ, building.FloorCount, building.FootprintAreaSQM,
			building.FloorAreaSQM, building.CapacityUnits, building.OccupiedUnits,
			building.QualityMilli, building.CompletedTick).Scan(&buildingID)
		if err != nil {
			return fmt.Errorf("insert city F7.3 building %s: %w", building.Code, err)
		}
		buildingIDs[building.Code] = buildingID
	}

	poolIDs := make(map[string]int64, len(foundation.UnitPools))
	poolStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_building_unit_pools
    (world_id, district_id, building_id, code, use_type, unit_count,
     occupied_unit_count, capacity_units_per_unit, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, '{}'::jsonb)
RETURNING id`)
	if err != nil {
		return fmt.Errorf("prepare city F7.3 unit pool insert: %w", err)
	}
	defer func() { _ = poolStatement.Close() }()
	for _, pool := range foundation.UnitPools {
		districtID, districtOK := districtIDs[pool.DistrictCode]
		buildingID, buildingOK := buildingIDs[pool.BuildingCode]
		if !districtOK || !buildingOK {
			return fmt.Errorf("city F7.3 pool %s references unknown foundation identity", pool.Code)
		}
		var poolID int64
		err = poolStatement.QueryRowContext(ctx, worldID, districtID, buildingID, pool.Code,
			pool.UseType, pool.UnitCount, pool.OccupiedUnitCount,
			pool.CapacityUnitsPerUnit).Scan(&poolID)
		if err != nil {
			return fmt.Errorf("insert city F7.3 unit pool %s: %w", pool.Code, err)
		}
		poolIDs[pool.Code] = poolID
	}

	allocationStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_housing_allocations
    (world_id, district_id, pool_id, cohort_id, cohort_key,
     allocated_units, status, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, 'active', 1, '{}'::jsonb)`)
	if err != nil {
		return fmt.Errorf("prepare city F7.3 housing allocation insert: %w", err)
	}
	defer func() { _ = allocationStatement.Close() }()
	for _, allocation := range foundation.HousingAllocations {
		districtID, districtOK := districtIDs[allocation.DistrictCode]
		poolID, poolOK := poolIDs[allocation.PoolCode]
		cohortID, cohortOK := cohortIDs[allocation.CohortKey]
		if !districtOK || !poolOK || !cohortOK {
			return fmt.Errorf("city F7.3 allocation %s references unknown foundation identity", allocation.CohortKey)
		}
		if _, err = allocationStatement.ExecContext(ctx, worldID, districtID, poolID, cohortID,
			allocation.CohortKey, allocation.AllocatedUnits); err != nil {
			return fmt.Errorf("insert city F7.3 housing allocation %s: %w", allocation.CohortKey, err)
		}
	}

	portalStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_building_portals
    (world_id, district_id, building_id, code, portal_type,
     from_x, from_y, from_z, to_x, to_y, to_z,
     bidirectional, status, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 'active', 1, '{}'::jsonb)`)
	if err != nil {
		return fmt.Errorf("prepare city F7.3 portal insert: %w", err)
	}
	defer func() { _ = portalStatement.Close() }()
	for _, portal := range foundation.Portals {
		districtID, districtOK := districtIDs[portal.DistrictCode]
		buildingID, buildingOK := buildingIDs[portal.BuildingCode]
		if !districtOK || !buildingOK {
			return fmt.Errorf("city F7.3 portal %s references unknown foundation identity", portal.Code)
		}
		if _, err = portalStatement.ExecContext(ctx, worldID, districtID, buildingID, portal.Code,
			portal.PortalType, portal.FromX, portal.FromY, portal.FromZ, portal.ToX, portal.ToY,
			portal.ToZ, portal.Bidirectional); err != nil {
			return fmt.Errorf("insert city F7.3 portal %s/%s: %w", portal.BuildingCode, portal.Code, err)
		}
	}

	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_land_baselines
    (world_id, tick, rule_set_hash, baseline_hash, zoning_rule_count, parcel_count,
     building_count, unit_pool_count, housing_allocation_count, portal_count,
     metadata, posted_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, '{}'::jsonb, NOW())`,
		worldID, baselineTick, ruleSet.ContentHash, foundation.BaselineHash, len(ruleSet.Rules),
		len(foundation.Parcels), len(foundation.Buildings), len(foundation.UnitPools),
		len(foundation.HousingAllocations), len(foundation.Portals)); err != nil {
		return fmt.Errorf("insert city F7.3 land baseline: %w", err)
	}
	return nil
}

func loadCityLandGenerationSeeds(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (map[string]int64, map[string]int64, []cityspatial.DistrictLandSeed, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT district.id, district.code, district.sort_order, district.area_units,
       district.developable_area_units, district.residential_capacity_units,
       district.commercial_capacity_units, district.industrial_capacity_units,
       cohort.id, entity.code, cohort.income_band, cohort.household_units
FROM city_districts district
LEFT JOIN city_household_cohorts cohort
  ON cohort.world_id = district.world_id AND cohort.district_id = district.id
LEFT JOIN city_economic_entities entity
  ON entity.world_id = cohort.world_id AND entity.id = cohort.entity_id
WHERE district.world_id = $1
ORDER BY district.sort_order ASC, district.code ASC,
         CASE cohort.income_band WHEN 'low' THEN 1 WHEN 'middle' THEN 2 WHEN 'high' THEN 3 ELSE 4 END,
         entity.code ASC`, worldID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load city F7.3 generation seeds: %w", err)
	}
	defer func() { _ = rows.Close() }()
	districtIDs := make(map[string]int64)
	cohortIDs := make(map[string]int64)
	districts := make([]cityspatial.DistrictLandSeed, 0, 6)
	districtIndex := make(map[string]int)
	for rows.Next() {
		var districtID, area, developable, residential, commercial, industrial int64
		var districtCode string
		var sortOrder int
		var cohortID sql.NullInt64
		var entityCode, incomeBand sql.NullString
		var householdUnits sql.NullInt64
		if err = rows.Scan(&districtID, &districtCode, &sortOrder, &area, &developable,
			&residential, &commercial, &industrial, &cohortID, &entityCode,
			&incomeBand, &householdUnits); err != nil {
			return nil, nil, nil, err
		}
		index, ok := districtIndex[districtCode]
		if !ok {
			index = len(districts)
			districtIndex[districtCode] = index
			districtIDs[districtCode] = districtID
			districts = append(districts, cityspatial.DistrictLandSeed{
				Code: districtCode, SortOrder: sortOrder, AreaSQM: area,
				DevelopableAreaSQM:       developable,
				ResidentialCapacityUnits: residential,
				CommercialCapacityUnits:  commercial,
				IndustrialCapacityUnits:  industrial,
				Households:               make([]cityspatial.HouseholdLandSeed, 0, 3),
			})
		}
		if cohortID.Valid {
			if !entityCode.Valid || !incomeBand.Valid || !householdUnits.Valid {
				return nil, nil, nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "land_household_seed"})
			}
			key := districtCode + "/" + entityCode.String + "/" + incomeBand.String
			if _, duplicate := cohortIDs[key]; duplicate {
				return nil, nil, nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "land_household_key"})
			}
			cohortIDs[key] = cohortID.Int64
			districts[index].Households = append(districts[index].Households, cityspatial.HouseholdLandSeed{
				EntityCode: entityCode.String, IncomeBand: incomeBand.String,
				HouseholdUnits: householdUnits.Int64,
			})
		}
	}
	if err = rows.Err(); err != nil {
		return nil, nil, nil, fmt.Errorf("iterate city F7.3 generation seeds: %w", err)
	}
	if len(districts) == 0 || len(cohortIDs) == 0 {
		return nil, nil, nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "land_generation_seeds"})
	}
	return districtIDs, cohortIDs, districts, nil
}

func loadCityLandHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	simulationVersion string,
	seed int64,
) (*cityLandHashState, error) {
	if !cityEngineSupportsLand(simulationVersion) {
		return nil, ErrCityLandStateNotFound
	}
	state := &cityLandHashState{
		ZoningRules:        make([]cityspatial.LandZoningRule, 0, 3),
		Parcels:            make([]cityspatial.GeneratedParcel, 0),
		Buildings:          make([]cityspatial.GeneratedBuilding, 0),
		UnitPools:          make([]cityspatial.GeneratedBuildingUnitPool, 0),
		HousingAllocations: make([]cityspatial.GeneratedHousingAllocation, 0),
		Portals:            make([]cityspatial.GeneratedBuildingPortal, 0),
	}
	var baselineRuleHash, baselineHash string
	err := queryer.QueryRowContext(ctx, `
SELECT profile.rule_set_id, profile.rule_set_version, profile.rule_set_hash,
       profile.spatial_overmap_root_hash, profile.nominal_cell_area_sqm,
       profile.baseline_hash, baseline.tick, profile.zoning_rule_count,
       profile.parcel_count, profile.building_count, profile.unit_pool_count,
       profile.housing_allocation_count, profile.portal_count, profile.revision,
       baseline.rule_set_hash, baseline.baseline_hash
FROM city_land_profiles profile
JOIN city_land_baselines baseline ON baseline.world_id = profile.world_id
WHERE profile.world_id = $1`, worldID).Scan(
		&state.Profile.RuleSetID, &state.Profile.RuleSetVersion, &state.Profile.RuleSetHash,
		&state.Profile.SpatialOvermapRootHash, &state.Profile.NominalCellAreaSQM,
		&state.Profile.BaselineHash, &state.Profile.BaselineTick,
		&state.Profile.ZoningRuleCount, &state.Profile.ParcelCount,
		&state.Profile.BuildingCount, &state.Profile.UnitPoolCount,
		&state.Profile.HousingAllocationCount, &state.Profile.PortalCount,
		&state.Profile.Revision, &baselineRuleHash, &baselineHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityLandStateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load city F7.3 land profile: %w", err)
	}
	if baselineRuleHash != state.Profile.RuleSetHash || baselineHash != state.Profile.BaselineHash {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "land_baseline_binding"})
	}

	ruleRows, err := queryer.QueryContext(ctx, `
SELECT code, name, primary_use, max_floor_area_ratio_milli,
       max_coverage_milli, max_floors, sqm_per_capacity_unit
FROM city_zoning_rules WHERE world_id = $1 ORDER BY code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city F7.3 zoning rules: %w", err)
	}
	for ruleRows.Next() {
		var rule cityspatial.LandZoningRule
		if err = ruleRows.Scan(&rule.Code, &rule.Name, &rule.PrimaryUse,
			&rule.MaxFloorAreaRatioMilli, &rule.MaxCoverageMilli,
			&rule.MaxFloors, &rule.SQMPerCapacityUnit); err != nil {
			_ = ruleRows.Close()
			return nil, err
		}
		state.ZoningRules = append(state.ZoningRules, rule)
	}
	if err = ruleRows.Err(); err != nil {
		_ = ruleRows.Close()
		return nil, err
	}
	_ = ruleRows.Close()

	parcelRows, err := queryer.QueryContext(ctx, `
SELECT parcel.code, district.code, parcel.zone_code, parcel.chunk_x, parcel.chunk_y,
       parcel.z, parcel.local_min_x, parcel.local_min_y, parcel.local_max_x,
       parcel.local_max_y, parcel.area_sqm, parcel.developable_area_sqm,
       parcel.status, parcel.version
FROM city_parcels parcel
JOIN city_districts district ON district.id = parcel.district_id
WHERE parcel.world_id = $1 ORDER BY parcel.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city F7.3 parcels: %w", err)
	}
	for parcelRows.Next() {
		var item cityspatial.GeneratedParcel
		if err = parcelRows.Scan(&item.Code, &item.DistrictCode, &item.ZoneCode,
			&item.Geometry.ChunkX, &item.Geometry.ChunkY, &item.Geometry.Z,
			&item.Geometry.LocalMinX, &item.Geometry.LocalMinY,
			&item.Geometry.LocalMaxX, &item.Geometry.LocalMaxY,
			&item.AreaSQM, &item.DevelopableAreaSQM, &item.Status, &item.Version); err != nil {
			_ = parcelRows.Close()
			return nil, err
		}
		state.Parcels = append(state.Parcels, item)
	}
	if err = parcelRows.Err(); err != nil {
		_ = parcelRows.Close()
		return nil, err
	}
	_ = parcelRows.Close()

	buildingRows, err := queryer.QueryContext(ctx, `
SELECT building.code, parcel.code, district.code, building.primary_use,
       building.chunk_x, building.chunk_y, building.footprint_z,
       building.local_min_x, building.local_min_y, building.local_max_x,
       building.local_max_y, building.base_z, building.top_z, building.floor_count,
       building.footprint_area_sqm, building.floor_area_sqm, building.capacity_units,
       building.occupied_units, building.quality_milli, building.status,
       building.completed_tick, building.version
FROM city_buildings building
JOIN city_parcels parcel ON parcel.id = building.parcel_id
JOIN city_districts district ON district.id = building.district_id
WHERE building.world_id = $1 ORDER BY building.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city F7.3 buildings: %w", err)
	}
	for buildingRows.Next() {
		var item cityspatial.GeneratedBuilding
		if err = buildingRows.Scan(&item.Code, &item.ParcelCode, &item.DistrictCode,
			&item.PrimaryUse, &item.Footprint.ChunkX, &item.Footprint.ChunkY,
			&item.Footprint.Z, &item.Footprint.LocalMinX, &item.Footprint.LocalMinY,
			&item.Footprint.LocalMaxX, &item.Footprint.LocalMaxY,
			&item.BaseZ, &item.TopZ, &item.FloorCount, &item.FootprintAreaSQM,
			&item.FloorAreaSQM, &item.CapacityUnits, &item.OccupiedUnits,
			&item.QualityMilli, &item.Status, &item.CompletedTick, &item.Version); err != nil {
			_ = buildingRows.Close()
			return nil, err
		}
		state.Buildings = append(state.Buildings, item)
	}
	if err = buildingRows.Err(); err != nil {
		_ = buildingRows.Close()
		return nil, err
	}
	_ = buildingRows.Close()

	poolRows, err := queryer.QueryContext(ctx, `
SELECT pool.code, building.code, district.code, pool.use_type, pool.unit_count,
       pool.occupied_unit_count, pool.capacity_units_per_unit, pool.version
FROM city_building_unit_pools pool
JOIN city_buildings building ON building.id = pool.building_id
JOIN city_districts district ON district.id = pool.district_id
WHERE pool.world_id = $1 ORDER BY pool.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city F7.3 unit pools: %w", err)
	}
	for poolRows.Next() {
		var item cityspatial.GeneratedBuildingUnitPool
		if err = poolRows.Scan(&item.Code, &item.BuildingCode, &item.DistrictCode,
			&item.UseType, &item.UnitCount, &item.OccupiedUnitCount,
			&item.CapacityUnitsPerUnit, &item.Version); err != nil {
			_ = poolRows.Close()
			return nil, err
		}
		state.UnitPools = append(state.UnitPools, item)
	}
	if err = poolRows.Err(); err != nil {
		_ = poolRows.Close()
		return nil, err
	}
	_ = poolRows.Close()

	allocationRows, err := queryer.QueryContext(ctx, `
SELECT pool.code, district.code, allocation.cohort_key,
       allocation.allocated_units, allocation.status, allocation.version
FROM city_housing_allocations allocation
JOIN city_building_unit_pools pool ON pool.id = allocation.pool_id
JOIN city_districts district ON district.id = allocation.district_id
WHERE allocation.world_id = $1
ORDER BY district.code ASC, allocation.cohort_key ASC, pool.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city F7.3 housing allocations: %w", err)
	}
	for allocationRows.Next() {
		var item cityspatial.GeneratedHousingAllocation
		if err = allocationRows.Scan(&item.PoolCode, &item.DistrictCode, &item.CohortKey,
			&item.AllocatedUnits, &item.Status, &item.Version); err != nil {
			_ = allocationRows.Close()
			return nil, err
		}
		state.HousingAllocations = append(state.HousingAllocations, item)
	}
	if err = allocationRows.Err(); err != nil {
		_ = allocationRows.Close()
		return nil, err
	}
	_ = allocationRows.Close()

	portalRows, err := queryer.QueryContext(ctx, `
SELECT portal.code, building.code, district.code, portal.portal_type,
       portal.from_x, portal.from_y, portal.from_z,
       portal.to_x, portal.to_y, portal.to_z,
       portal.bidirectional, portal.status, portal.version
FROM city_building_portals portal
JOIN city_buildings building ON building.id = portal.building_id
JOIN city_districts district ON district.id = portal.district_id
WHERE portal.world_id = $1
ORDER BY building.code ASC, portal.from_z ASC, portal.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city F7.3 portals: %w", err)
	}
	for portalRows.Next() {
		var item cityspatial.GeneratedBuildingPortal
		if err = portalRows.Scan(&item.Code, &item.BuildingCode, &item.DistrictCode,
			&item.PortalType, &item.FromX, &item.FromY, &item.FromZ,
			&item.ToX, &item.ToY, &item.ToZ, &item.Bidirectional,
			&item.Status, &item.Version); err != nil {
			_ = portalRows.Close()
			return nil, err
		}
		state.Portals = append(state.Portals, item)
	}
	if err = portalRows.Err(); err != nil {
		_ = portalRows.Close()
		return nil, err
	}
	_ = portalRows.Close()

	if int64(len(state.ZoningRules)) != state.Profile.ZoningRuleCount ||
		int64(len(state.Parcels)) != state.Profile.ParcelCount ||
		int64(len(state.Buildings)) != state.Profile.BuildingCount ||
		int64(len(state.UnitPools)) != state.Profile.UnitPoolCount ||
		int64(len(state.HousingAllocations)) != state.Profile.HousingAllocationCount ||
		int64(len(state.Portals)) != state.Profile.PortalCount {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "land_object_counts"})
	}

	defaultRuleSet, err := cityspatial.DefaultLandRuleSet()
	if err != nil || defaultRuleSet.ID != state.Profile.RuleSetID ||
		defaultRuleSet.Version != state.Profile.RuleSetVersion ||
		defaultRuleSet.ContentHash != state.Profile.RuleSetHash ||
		defaultRuleSet.NominalCellAreaSQM != state.Profile.NominalCellAreaSQM {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "land_rule_set_binding"}).WithCause(err)
	}
	defaultRuleSet.Rules = append([]cityspatial.LandZoningRule(nil), state.ZoningRules...)
	var spatialRuleHash, spatialRootHash string
	if err = queryer.QueryRowContext(ctx, `
SELECT rule_set_hash, overmap_root_hash FROM city_spatial_profiles WHERE world_id = $1`, worldID).
		Scan(&spatialRuleHash, &spatialRootHash); err != nil {
		return nil, fmt.Errorf("load city F7.3 spatial binding: %w", err)
	}
	if spatialRootHash != state.Profile.SpatialOvermapRootHash {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "land_spatial_root"})
	}
	landGeneratorVersion, err := cityLandGeneratorVersion(simulationVersion)
	if err != nil {
		return nil, err
	}
	binding, err := cityspatial.DefaultLandGeneratorBinding(
		landGeneratorVersion, seed, spatialRuleHash, spatialRootHash, defaultRuleSet,
	)
	if err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "land_generator_binding"}).WithCause(err)
	}
	computedHash, err := cityspatial.ComputeLandFoundationBaselineHash(&cityspatial.GeneratedLandFoundation{
		Binding: binding, RuleSet: *defaultRuleSet, Parcels: state.Parcels,
		Buildings: state.Buildings, UnitPools: state.UnitPools,
		HousingAllocations: state.HousingAllocations, Portals: state.Portals,
	})
	if err != nil || computedHash != state.Profile.BaselineHash {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "land_baseline_hash"}).WithCause(err)
	}
	return state, nil
}

func (s *CityEconomyService) GetLandState(
	ctx context.Context,
	input CityLandQueryInput,
) (*CityLandState, error) {
	if input.UserID <= 0 || input.WorldID <= 0 || input.MinimumX > input.MaximumX ||
		input.MinimumY > input.MaximumY || input.MaximumX-input.MinimumX+1 > citySpatialMaximumQueryAxis ||
		input.MaximumY-input.MinimumY+1 > citySpatialMaximumQueryAxis {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	profile, err := loadCitySpatialProfile(ctx, s.db, input.WorldID)
	if err != nil {
		return nil, err
	}
	if input.MinimumX < profile.MinimumChunkX || input.MaximumX > profile.MaximumChunkX ||
		input.MinimumY < profile.MinimumChunkY || input.MaximumY > profile.MaximumChunkY ||
		input.Z < profile.MinimumZ || input.Z > profile.MaximumZ {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "land_bounds"})
	}
	var version string
	var seed int64
	if err = s.db.QueryRowContext(ctx, `SELECT simulation_version, seed FROM city_worlds WHERE id = $1`, input.WorldID).
		Scan(&version, &seed); err != nil {
		return nil, fmt.Errorf("load city F7.3 world binding: %w", err)
	}
	full, err := loadCityLandHashState(ctx, s.db, input.WorldID, version, seed)
	if err != nil {
		return nil, err
	}
	if cityEngineSupportsDevelopment(version) {
		adjustments, adjustmentErr := loadAllCityBuildingAdjustments(ctx, s.db, input.WorldID)
		if adjustmentErr != nil {
			return nil, adjustmentErr
		}
		if adjustmentErr = applyCityBuildingAdjustments(full, adjustments); adjustmentErr != nil {
			return nil, adjustmentErr
		}
	}
	return filterCityLandState(full, input), nil
}

func loadAllCityBuildingAdjustments(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) ([]CityBuildingAdjustment, error) {
	rows, err := queryer.QueryContext(ctx, cityBuildingAdjustmentCanonicalSelect+`
WHERE adjustment.world_id = $1
ORDER BY building.code ASC, adjustment.project_code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load effective city building adjustments: %w", err)
	}
	items := make([]CityBuildingAdjustment, 0)
	for rows.Next() {
		item, scanErr := scanCityBuildingAdjustment(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan effective city building adjustment: %w", scanErr)
		}
		items = append(items, *item)
	}
	if err = closeCityRows(rows, "iterate effective city building adjustments"); err != nil {
		return nil, err
	}
	return items, nil
}

func applyCityBuildingAdjustments(
	state *CityLandState,
	adjustments []CityBuildingAdjustment,
) error {
	if state == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "effective_land_state"})
	}
	buildingIndexes := make(map[string]int, len(state.Buildings))
	for index := range state.Buildings {
		buildingIndexes[state.Buildings[index].Code] = index
	}
	poolIndexes := make(map[string]int, len(state.UnitPools))
	for index := range state.UnitPools {
		pool := state.UnitPools[index]
		if _, duplicate := poolIndexes[pool.BuildingCode]; duplicate {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{
				"field": "effective_land_unit_pool", "building_code": pool.BuildingCode,
			})
		}
		poolIndexes[pool.BuildingCode] = index
	}
	for _, adjustment := range adjustments {
		buildingIndex, ok := buildingIndexes[adjustment.BuildingCode]
		if !ok {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{
				"field": "effective_land_building", "building_code": adjustment.BuildingCode,
			})
		}
		building := &state.Buildings[buildingIndex]
		if building.DistrictCode != adjustment.DistrictCode || adjustment.AddedFloorCount < 0 ||
			adjustment.AddedTopZ < 0 || adjustment.AddedFloorAreaSQM < 0 ||
			adjustment.AddedCapacityUnits < 0 || adjustment.QualityDeltaMilli < 0 ||
			adjustment.AddedFloorCount != adjustment.AddedTopZ ||
			int64(building.FloorCount) > math.MaxInt32-int64(adjustment.AddedFloorCount) ||
			building.FloorAreaSQM > math.MaxInt64-adjustment.AddedFloorAreaSQM ||
			building.CapacityUnits > math.MaxInt64-adjustment.AddedCapacityUnits ||
			building.QualityMilli > math.MaxInt64-adjustment.QualityDeltaMilli ||
			building.Version == math.MaxInt64 {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{
				"field": "effective_land_adjustment", "project_code": adjustment.ProjectCode,
			})
		}
		previousTopZ := building.TopZ
		if int64(previousTopZ) > math.MaxInt32-int64(adjustment.AddedTopZ) {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{
				"field": "effective_land_top_z", "project_code": adjustment.ProjectCode,
			})
		}
		building.FloorCount += adjustment.AddedFloorCount
		building.TopZ += adjustment.AddedTopZ
		building.FloorAreaSQM += adjustment.AddedFloorAreaSQM
		building.CapacityUnits += adjustment.AddedCapacityUnits
		building.QualityMilli += adjustment.QualityDeltaMilli
		building.CompletedTick = maxInt64(building.CompletedTick, adjustment.CompletedTick)
		building.Version++

		poolIndex, hasPool := poolIndexes[adjustment.BuildingCode]
		if !hasPool {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{
				"field": "effective_land_unit_pool", "building_code": adjustment.BuildingCode,
			})
		}
		pool := &state.UnitPools[poolIndex]
		if pool.CapacityUnitsPerUnit <= 0 ||
			adjustment.AddedCapacityUnits%pool.CapacityUnitsPerUnit != 0 ||
			pool.UnitCount > math.MaxInt64-adjustment.AddedCapacityUnits/pool.CapacityUnitsPerUnit ||
			pool.Version == math.MaxInt64 {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{
				"field": "effective_land_unit_pool_adjustment", "project_code": adjustment.ProjectCode,
			})
		}
		pool.UnitCount += adjustment.AddedCapacityUnits / pool.CapacityUnitsPerUnit
		pool.Version++

		if adjustment.AddedTopZ > 0 {
			centerX := building.Footprint.ChunkX*cityspatial.DefaultChunkSize +
				int64((building.Footprint.LocalMinX+building.Footprint.LocalMaxX)/2)
			centerY := building.Footprint.ChunkY*cityspatial.DefaultChunkSize +
				int64((building.Footprint.LocalMinY+building.Footprint.LocalMaxY)/2)
			for level := previousTopZ; level < building.TopZ; level++ {
				state.Portals = append(state.Portals, cityspatial.GeneratedBuildingPortal{
					Code: fmt.Sprintf("%s_%s_stair_%03d_%03d",
						adjustment.ProjectCode, building.Code, level, level+1),
					BuildingCode: building.Code, DistrictCode: building.DistrictCode,
					PortalType: "stair", FromX: centerX, FromY: centerY, FromZ: level,
					ToX: centerX, ToY: centerY, ToZ: level + 1,
					Bidirectional: true, Status: "active", Version: building.Version,
				})
			}
		}
	}
	sort.Slice(state.Portals, func(i, j int) bool {
		left, right := state.Portals[i], state.Portals[j]
		if left.BuildingCode != right.BuildingCode {
			return left.BuildingCode < right.BuildingCode
		}
		if left.FromZ != right.FromZ {
			return left.FromZ < right.FromZ
		}
		return left.Code < right.Code
	})
	return nil
}

func filterCityLandState(full *CityLandState, input CityLandQueryInput) *CityLandState {
	result := &CityLandState{
		Profile:            full.Profile,
		ZoningRules:        append([]cityspatial.LandZoningRule(nil), full.ZoningRules...),
		Parcels:            make([]cityspatial.GeneratedParcel, 0),
		Buildings:          make([]cityspatial.GeneratedBuilding, 0),
		UnitPools:          make([]cityspatial.GeneratedBuildingUnitPool, 0),
		HousingAllocations: make([]cityspatial.GeneratedHousingAllocation, 0),
		Portals:            make([]cityspatial.GeneratedBuildingPortal, 0),
	}
	selectedBuildings := make(map[string]struct{})
	selectedParcels := make(map[string]struct{})
	for _, building := range full.Buildings {
		if building.Footprint.ChunkX < input.MinimumX || building.Footprint.ChunkX > input.MaximumX ||
			building.Footprint.ChunkY < input.MinimumY || building.Footprint.ChunkY > input.MaximumY ||
			input.Z < building.BaseZ || input.Z > building.TopZ {
			continue
		}
		result.Buildings = append(result.Buildings, building)
		selectedBuildings[building.Code] = struct{}{}
		selectedParcels[building.ParcelCode] = struct{}{}
	}
	for _, parcel := range full.Parcels {
		_, neededByBuilding := selectedParcels[parcel.Code]
		if !neededByBuilding && (input.Z != parcel.Geometry.Z ||
			parcel.Geometry.ChunkX < input.MinimumX || parcel.Geometry.ChunkX > input.MaximumX ||
			parcel.Geometry.ChunkY < input.MinimumY || parcel.Geometry.ChunkY > input.MaximumY) {
			continue
		}
		result.Parcels = append(result.Parcels, parcel)
	}
	selectedPools := make(map[string]struct{})
	for _, pool := range full.UnitPools {
		if _, ok := selectedBuildings[pool.BuildingCode]; !ok {
			continue
		}
		result.UnitPools = append(result.UnitPools, pool)
		selectedPools[pool.Code] = struct{}{}
	}
	for _, allocation := range full.HousingAllocations {
		if _, ok := selectedPools[allocation.PoolCode]; ok {
			result.HousingAllocations = append(result.HousingAllocations, allocation)
		}
	}
	for _, portal := range full.Portals {
		if _, ok := selectedBuildings[portal.BuildingCode]; ok &&
			(portal.FromZ == input.Z || portal.ToZ == input.Z) {
			result.Portals = append(result.Portals, portal)
		}
	}
	sort.Slice(result.Parcels, func(i, j int) bool { return result.Parcels[i].Code < result.Parcels[j].Code })
	return result
}
