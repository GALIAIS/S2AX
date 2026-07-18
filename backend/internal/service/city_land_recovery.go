package service

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"

	"github.com/Wei-Shaw/sub2api/internal/cityspatial"
)

func validateCityLandSnapshot(state cityHashState) error {
	if !cityEngineSupportsLand(state.SimulationVersion) || state.Spatial == nil || state.Land == nil {
		return fmt.Errorf("recovery F7.3 snapshot is missing land state")
	}
	land := state.Land
	if int64(len(land.ZoningRules)) != land.Profile.ZoningRuleCount ||
		int64(len(land.Parcels)) != land.Profile.ParcelCount ||
		int64(len(land.Buildings)) != land.Profile.BuildingCount ||
		int64(len(land.UnitPools)) != land.Profile.UnitPoolCount ||
		int64(len(land.HousingAllocations)) != land.Profile.HousingAllocationCount ||
		int64(len(land.Portals)) != land.Profile.PortalCount {
		return fmt.Errorf("recovery F7.3 land object counts do not match snapshot profile")
	}
	defaultRuleSet, err := cityspatial.DefaultLandRuleSet()
	if err != nil || land.Profile.RuleSetID != defaultRuleSet.ID ||
		land.Profile.RuleSetVersion != defaultRuleSet.Version ||
		land.Profile.RuleSetHash != defaultRuleSet.ContentHash ||
		land.Profile.NominalCellAreaSQM != defaultRuleSet.NominalCellAreaSQM ||
		!reflect.DeepEqual(land.ZoningRules, defaultRuleSet.Rules) {
		return fmt.Errorf("recovery F7.3 land rule set does not match its immutable binding")
	}
	if land.Profile.SpatialOvermapRootHash != state.Spatial.Overmap.RootHash {
		return fmt.Errorf("recovery F7.3 land baseline does not match spatial overmap")
	}
	landGeneratorVersion, err := cityLandGeneratorVersion(state.SimulationVersion)
	if err != nil {
		return err
	}
	binding, err := cityspatial.DefaultLandGeneratorBinding(
		landGeneratorVersion, state.Seed, state.Spatial.Profile.RuleSetHash,
		state.Spatial.Overmap.RootHash, defaultRuleSet,
	)
	if err != nil {
		return fmt.Errorf("recovery F7.3 land binding is invalid: %w", err)
	}
	computedHash, err := cityspatial.ComputeLandFoundationBaselineHash(&cityspatial.GeneratedLandFoundation{
		Binding: binding, RuleSet: *defaultRuleSet, Parcels: land.Parcels,
		Buildings: land.Buildings, UnitPools: land.UnitPools,
		HousingAllocations: land.HousingAllocations, Portals: land.Portals,
	})
	if err != nil || computedHash != land.Profile.BaselineHash {
		return fmt.Errorf("recovery F7.3 land baseline hash does not match snapshot")
	}
	return nil
}

func clearCityLandProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) (int, error) {
	count := 0
	for _, target := range []struct {
		label string
		query string
	}{
		{"city housing allocations", `DELETE FROM city_housing_allocations WHERE world_id = $1`},
		{"city building portals", `DELETE FROM city_building_portals WHERE world_id = $1`},
		{"city building unit pools", `DELETE FROM city_building_unit_pools WHERE world_id = $1`},
		{"city buildings", `DELETE FROM city_buildings WHERE world_id = $1`},
		{"city parcels", `DELETE FROM city_parcels WHERE world_id = $1`},
		{"city zoning rules", `DELETE FROM city_zoning_rules WHERE world_id = $1`},
		{"city land baselines", `DELETE FROM city_land_baselines WHERE world_id = $1`},
		{"city land profiles", `DELETE FROM city_land_profiles WHERE world_id = $1`},
	} {
		result, err := tx.ExecContext(ctx, target.query, worldID)
		if err != nil {
			return 0, fmt.Errorf("clear recovery %s: %w", target.label, err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("count cleared recovery %s: %w", target.label, err)
		}
		count += int(rows)
	}
	return count, nil
}

func restoreCityLandProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	state cityHashState,
) (int, error) {
	if err := validateCityLandSnapshot(state); err != nil {
		return 0, err
	}
	land := state.Land
	districtIDs, cohortIDs, _, err := loadCityLandGenerationSeeds(ctx, tx, worldID)
	if err != nil {
		return 0, err
	}
	count := 0
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_land_profiles
    (world_id, rule_set_id, rule_set_version, rule_set_hash, spatial_overmap_root_hash,
     nominal_cell_area_sqm, baseline_hash, zoning_rule_count, parcel_count,
     building_count, unit_pool_count, housing_allocation_count, portal_count,
     revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, '{}'::jsonb)`,
		worldID, land.Profile.RuleSetID, land.Profile.RuleSetVersion, land.Profile.RuleSetHash,
		land.Profile.SpatialOvermapRootHash, land.Profile.NominalCellAreaSQM,
		land.Profile.BaselineHash, land.Profile.ZoningRuleCount, land.Profile.ParcelCount,
		land.Profile.BuildingCount, land.Profile.UnitPoolCount,
		land.Profile.HousingAllocationCount, land.Profile.PortalCount,
		land.Profile.Revision); err != nil {
		return 0, fmt.Errorf("restore city F7.3 land profile: %w", err)
	}
	count++

	zoningStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_zoning_rules
    (world_id, code, name, primary_use, max_floor_area_ratio_milli,
     max_coverage_milli, max_floors, sqm_per_capacity_unit, status, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'active', '{}'::jsonb)`)
	if err != nil {
		return 0, fmt.Errorf("prepare recovery city zoning rules: %w", err)
	}
	defer func() { _ = zoningStatement.Close() }()
	for _, rule := range land.ZoningRules {
		if _, err = zoningStatement.ExecContext(ctx, worldID, rule.Code, rule.Name,
			rule.PrimaryUse, rule.MaxFloorAreaRatioMilli, rule.MaxCoverageMilli,
			rule.MaxFloors, rule.SQMPerCapacityUnit); err != nil {
			return 0, fmt.Errorf("restore city zoning rule %s: %w", rule.Code, err)
		}
		count++
	}

	parcelIDs := make(map[string]int64, len(land.Parcels))
	parcelStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_parcels
    (world_id, district_id, code, zone_code, chunk_x, chunk_y, z,
     local_min_x, local_min_y, local_max_x, local_max_y,
     area_sqm, developable_area_sqm, status, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, '{}'::jsonb)
RETURNING id`)
	if err != nil {
		return 0, fmt.Errorf("prepare recovery city parcels: %w", err)
	}
	defer func() { _ = parcelStatement.Close() }()
	for _, parcel := range land.Parcels {
		districtID, ok := districtIDs[parcel.DistrictCode]
		if !ok {
			return 0, fmt.Errorf("recovery parcel references unknown district %q", parcel.DistrictCode)
		}
		var parcelID int64
		err = parcelStatement.QueryRowContext(ctx, worldID, districtID, parcel.Code,
			parcel.ZoneCode, parcel.Geometry.ChunkX, parcel.Geometry.ChunkY, parcel.Geometry.Z,
			parcel.Geometry.LocalMinX, parcel.Geometry.LocalMinY,
			parcel.Geometry.LocalMaxX, parcel.Geometry.LocalMaxY,
			parcel.AreaSQM, parcel.DevelopableAreaSQM, parcel.Status, parcel.Version).Scan(&parcelID)
		if err != nil {
			return 0, fmt.Errorf("restore city parcel %s: %w", parcel.Code, err)
		}
		parcelIDs[parcel.Code] = parcelID
		count++
	}

	buildingIDs := make(map[string]int64, len(land.Buildings))
	buildingStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_buildings
    (world_id, district_id, parcel_id, code, primary_use, chunk_x, chunk_y, footprint_z,
     local_min_x, local_min_y, local_max_x, local_max_y, base_z, top_z, floor_count,
     footprint_area_sqm, floor_area_sqm, capacity_units, occupied_units, quality_milli,
     status, completed_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
        $16, $17, $18, $19, $20, $21, $22, $23, '{}'::jsonb)
RETURNING id`)
	if err != nil {
		return 0, fmt.Errorf("prepare recovery city buildings: %w", err)
	}
	defer func() { _ = buildingStatement.Close() }()
	for _, building := range land.Buildings {
		districtID, districtOK := districtIDs[building.DistrictCode]
		parcelID, parcelOK := parcelIDs[building.ParcelCode]
		if !districtOK || !parcelOK {
			return 0, fmt.Errorf("recovery building %s references unknown identity", building.Code)
		}
		var buildingID int64
		err = buildingStatement.QueryRowContext(ctx, worldID, districtID, parcelID,
			building.Code, building.PrimaryUse, building.Footprint.ChunkX,
			building.Footprint.ChunkY, building.Footprint.Z,
			building.Footprint.LocalMinX, building.Footprint.LocalMinY,
			building.Footprint.LocalMaxX, building.Footprint.LocalMaxY,
			building.BaseZ, building.TopZ, building.FloorCount,
			building.FootprintAreaSQM, building.FloorAreaSQM, building.CapacityUnits,
			building.OccupiedUnits, building.QualityMilli, building.Status,
			building.CompletedTick, building.Version).Scan(&buildingID)
		if err != nil {
			return 0, fmt.Errorf("restore city building %s: %w", building.Code, err)
		}
		buildingIDs[building.Code] = buildingID
		count++
	}

	poolIDs := make(map[string]int64, len(land.UnitPools))
	poolStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_building_unit_pools
    (world_id, district_id, building_id, code, use_type, unit_count,
     occupied_unit_count, capacity_units_per_unit, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, '{}'::jsonb)
RETURNING id`)
	if err != nil {
		return 0, fmt.Errorf("prepare recovery city unit pools: %w", err)
	}
	defer func() { _ = poolStatement.Close() }()
	for _, pool := range land.UnitPools {
		districtID, districtOK := districtIDs[pool.DistrictCode]
		buildingID, buildingOK := buildingIDs[pool.BuildingCode]
		if !districtOK || !buildingOK {
			return 0, fmt.Errorf("recovery pool %s references unknown identity", pool.Code)
		}
		var poolID int64
		err = poolStatement.QueryRowContext(ctx, worldID, districtID, buildingID,
			pool.Code, pool.UseType, pool.UnitCount, pool.OccupiedUnitCount,
			pool.CapacityUnitsPerUnit, pool.Version).Scan(&poolID)
		if err != nil {
			return 0, fmt.Errorf("restore city unit pool %s: %w", pool.Code, err)
		}
		poolIDs[pool.Code] = poolID
		count++
	}

	allocationStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_housing_allocations
    (world_id, district_id, pool_id, cohort_id, cohort_key,
     allocated_units, status, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, '{}'::jsonb)`)
	if err != nil {
		return 0, fmt.Errorf("prepare recovery city housing allocations: %w", err)
	}
	defer func() { _ = allocationStatement.Close() }()
	for _, allocation := range land.HousingAllocations {
		districtID, districtOK := districtIDs[allocation.DistrictCode]
		poolID, poolOK := poolIDs[allocation.PoolCode]
		cohortID, cohortOK := cohortIDs[allocation.CohortKey]
		if !districtOK || !poolOK || !cohortOK {
			return 0, fmt.Errorf("recovery allocation %s references unknown identity", allocation.CohortKey)
		}
		if _, err = allocationStatement.ExecContext(ctx, worldID, districtID, poolID,
			cohortID, allocation.CohortKey, allocation.AllocatedUnits,
			allocation.Status, allocation.Version); err != nil {
			return 0, fmt.Errorf("restore city housing allocation %s: %w", allocation.CohortKey, err)
		}
		count++
	}

	portalStatement, err := tx.PrepareContext(ctx, `
INSERT INTO city_building_portals
    (world_id, district_id, building_id, code, portal_type,
     from_x, from_y, from_z, to_x, to_y, to_z,
     bidirectional, status, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, '{}'::jsonb)`)
	if err != nil {
		return 0, fmt.Errorf("prepare recovery city portals: %w", err)
	}
	defer func() { _ = portalStatement.Close() }()
	for _, portal := range land.Portals {
		districtID, districtOK := districtIDs[portal.DistrictCode]
		buildingID, buildingOK := buildingIDs[portal.BuildingCode]
		if !districtOK || !buildingOK {
			return 0, fmt.Errorf("recovery portal %s references unknown identity", portal.Code)
		}
		if _, err = portalStatement.ExecContext(ctx, worldID, districtID, buildingID,
			portal.Code, portal.PortalType, portal.FromX, portal.FromY, portal.FromZ,
			portal.ToX, portal.ToY, portal.ToZ, portal.Bidirectional,
			portal.Status, portal.Version); err != nil {
			return 0, fmt.Errorf("restore city portal %s/%s: %w", portal.BuildingCode, portal.Code, err)
		}
		count++
	}

	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_land_baselines
    (world_id, tick, rule_set_hash, baseline_hash, zoning_rule_count, parcel_count,
     building_count, unit_pool_count, housing_allocation_count, portal_count,
     metadata, posted_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, '{}'::jsonb, NOW())`,
		worldID, land.Profile.BaselineTick, land.Profile.RuleSetHash,
		land.Profile.BaselineHash, land.Profile.ZoningRuleCount, land.Profile.ParcelCount,
		land.Profile.BuildingCount, land.Profile.UnitPoolCount,
		land.Profile.HousingAllocationCount, land.Profile.PortalCount); err != nil {
		return 0, fmt.Errorf("restore city F7.3 land baseline: %w", err)
	}
	count++
	if _, err = tx.ExecContext(ctx, `SELECT assert_city_land_foundation($1)`, worldID); err != nil {
		return 0, fmt.Errorf("validate recovered city F7.3 land foundation: %w", err)
	}
	return count, nil
}
