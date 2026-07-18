package service

import (
	"context"
	"database/sql"
	"fmt"
)

type cityDevelopmentRecoveryFactKey struct {
	tick     int64
	sequence int64
}

func loadCityDevelopmentRecoveryFactIDs(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (map[cityDevelopmentRecoveryFactKey]int64, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT id, tick, sequence
FROM city_development_facts
WHERE world_id = $1
ORDER BY tick ASC, sequence ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city development recovery fact identities: %w", err)
	}
	identities := make(map[cityDevelopmentRecoveryFactKey]int64)
	for rows.Next() {
		var id, tick, sequence int64
		if err = rows.Scan(&id, &tick, &sequence); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan city development recovery fact identity: %w", err)
		}
		identities[cityDevelopmentRecoveryFactKey{tick: tick, sequence: sequence}] = id
	}
	if err = closeCityRows(rows, "iterate city development recovery fact identities"); err != nil {
		return nil, err
	}
	return identities, nil
}

func restoreCityDevelopmentProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	state *cityHashState,
	preservedFactIDs map[cityDevelopmentRecoveryFactKey]int64,
) (int, error) {
	if state == nil || state.Development == nil || !cityEngineSupportsDevelopment(state.SimulationVersion) {
		return 0, fmt.Errorf("recovery development state is unavailable")
	}
	development := state.Development
	if development.Profile.PolicyID != cityDevelopmentPolicyID ||
		development.Profile.PolicyVersion != cityDevelopmentPolicyVersion ||
		development.Profile.PolicyHash != cityDevelopmentPolicyHash ||
		development.Profile.BaselineHash != cityDevelopmentBaselineHash ||
		development.Profile.ProjectCount != int64(len(development.Projects)) ||
		development.Profile.FactCount != int64(len(development.Facts)) ||
		development.Profile.AdjustmentCount != int64(len(development.Adjustments)) ||
		development.Profile.Revision != int64(len(development.Facts))+1 {
		return 0, fmt.Errorf("recovery development profile is inconsistent")
	}
	count, err := clearCityDevelopmentProjection(ctx, tx, worldID)
	if err != nil {
		return 0, err
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_development_profiles
    (world_id, policy_id, policy_version, policy_hash, baseline_tick,
     project_count, fact_count, adjustment_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, '{}'::jsonb)`,
		worldID, development.Profile.PolicyID, development.Profile.PolicyVersion,
		development.Profile.PolicyHash, development.Profile.BaselineTick,
		development.Profile.ProjectCount, development.Profile.FactCount,
		development.Profile.AdjustmentCount, development.Profile.Revision); err != nil {
		return count, fmt.Errorf("restore city development profile: %w", err)
	}
	count++
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_development_baselines
    (world_id, tick, policy_hash, baseline_hash, metadata, posted_at)
VALUES ($1, $2, $3, $4, '{}'::jsonb, NOW())`,
		worldID, development.Profile.BaselineTick,
		development.Profile.PolicyHash, development.Profile.BaselineHash); err != nil {
		return count, fmt.Errorf("restore city development baseline: %w", err)
	}
	count++

	for _, project := range development.Projects {
		var districtID, parcelID, buildingID, developerEntityID int64
		if err := tx.QueryRowContext(ctx, `
SELECT district.id, parcel.id, building.id, developer.id
FROM city_districts district
JOIN city_parcels parcel
  ON parcel.world_id = district.world_id AND parcel.district_id = district.id AND parcel.code = $3
JOIN city_buildings building
  ON building.world_id = district.world_id AND building.district_id = district.id
 AND building.parcel_id = parcel.id AND building.code = $4
JOIN city_economic_entities developer
  ON developer.world_id = district.world_id AND developer.code = $5
WHERE district.world_id = $1 AND district.code = $2`,
			worldID, project.DistrictCode, project.ParcelCode, project.BuildingCode,
			project.DeveloperEntityCode).Scan(
			&districtID, &parcelID, &buildingID, &developerEntityID,
		); err != nil {
			return count, fmt.Errorf("resolve city development recovery identity %s: %w", project.Code, err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_development_projects
    (world_id, code, name, project_type, district_id, parcel_id, building_id,
     developer_entity_id, target_floor_count, target_quality_milli,
     added_floor_count, added_floor_area_sqm, added_capacity_units,
     quality_delta_milli, required_basic_material_units,
     required_capital_goods_units, required_labor_units, planned_duration_ticks,
     status, progress_milli, submitted_tick, reviewed_tick, started_tick,
     planned_completion_tick, completed_tick, cancelled_tick, version, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28::jsonb)`,
			worldID, project.Code, project.Name, project.ProjectType,
			districtID, parcelID, buildingID, developerEntityID,
			developmentNullableInt32(project.TargetFloorCount),
			developmentNullableInt64(project.TargetQualityMilli),
			project.AddedFloorCount, project.AddedFloorAreaSQM,
			project.AddedCapacityUnits, project.QualityDeltaMilli,
			project.RequiredBasicMaterialUnits, project.RequiredCapitalGoodsUnits,
			project.RequiredLaborUnits, project.PlannedDurationTicks,
			project.Status, project.ProgressMilli, project.SubmittedTick,
			developmentNullableInt64(project.ReviewedTick),
			developmentNullableInt64(project.StartedTick),
			developmentNullableInt64(project.PlannedCompletionTick),
			developmentNullableInt64(project.CompletedTick),
			developmentNullableInt64(project.CancelledTick),
			project.Version, []byte(project.Metadata)); err != nil {
			return count, fmt.Errorf("restore city development project %s: %w", project.Code, err)
		}
		count++
	}

	factIDs := make(map[string]int64)
	for _, fact := range development.Facts {
		var sourceCommandID any
		if fact.SourceCommandSequence != nil {
			var commandID int64
			if err := tx.QueryRowContext(ctx, `
SELECT id FROM city_commands WHERE world_id = $1 AND sequence = $2`,
				worldID, *fact.SourceCommandSequence).Scan(&commandID); err != nil {
				return count, fmt.Errorf("resolve city development source command: %w", err)
			}
			sourceCommandID = commandID
		}
		var factID int64
		preservedFactID := preservedFactIDs[cityDevelopmentRecoveryFactKey{
			tick: fact.Tick, sequence: fact.Sequence,
		}]
		if preservedFactID > 0 {
			err = tx.QueryRowContext(ctx, `
INSERT INTO city_development_facts
    (id, world_id, tick, sequence, project_code, source_command_id, fact_type,
     from_status, to_status, progress_before_milli, progress_after_milli,
     project_version_before, project_version_after, metadata, posted_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb, NOW())
RETURNING id`, preservedFactID, worldID, fact.Tick, fact.Sequence, fact.ProjectCode,
				sourceCommandID, fact.FactType, developmentNullableString(fact.FromStatus), fact.ToStatus,
				fact.ProgressBeforeMilli, fact.ProgressAfterMilli,
				fact.ProjectVersionBefore, fact.ProjectVersionAfter,
				[]byte(fact.Metadata)).Scan(&factID)
		} else {
			err = tx.QueryRowContext(ctx, `
INSERT INTO city_development_facts
    (world_id, tick, sequence, project_code, source_command_id, fact_type,
     from_status, to_status, progress_before_milli, progress_after_milli,
     project_version_before, project_version_after, metadata, posted_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb, NOW())
RETURNING id`, worldID, fact.Tick, fact.Sequence, fact.ProjectCode, sourceCommandID,
				fact.FactType, developmentNullableString(fact.FromStatus), fact.ToStatus,
				fact.ProgressBeforeMilli, fact.ProgressAfterMilli,
				fact.ProjectVersionBefore, fact.ProjectVersionAfter,
				[]byte(fact.Metadata)).Scan(&factID)
		}
		if err != nil {
			return count, fmt.Errorf("restore city development fact %d/%d: %w", fact.Tick, fact.Sequence, err)
		}
		if fact.FactType == CityDevelopmentFactCompleted {
			factIDs[fact.ProjectCode] = factID
		}
		count++
	}

	for _, adjustment := range development.Adjustments {
		factID, ok := factIDs[adjustment.ProjectCode]
		if !ok {
			return count, fmt.Errorf("recovery adjustment %s has no completion fact", adjustment.ProjectCode)
		}
		var buildingID, districtID int64
		if err := tx.QueryRowContext(ctx, `
SELECT building.id, district.id
FROM city_buildings building
JOIN city_districts district ON district.id = building.district_id
WHERE building.world_id = $1 AND building.code = $2 AND district.code = $3`,
			worldID, adjustment.BuildingCode, adjustment.DistrictCode).Scan(
			&buildingID, &districtID,
		); err != nil {
			return count, fmt.Errorf("resolve recovery building adjustment: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO city_building_adjustments
    (world_id, project_code, building_id, district_id, completion_fact_id,
     added_floor_count, added_top_z, added_floor_area_sqm,
     added_capacity_units, quality_delta_milli, completed_tick, metadata, posted_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, NOW())`,
			worldID, adjustment.ProjectCode, buildingID, districtID, factID,
			adjustment.AddedFloorCount, adjustment.AddedTopZ,
			adjustment.AddedFloorAreaSQM, adjustment.AddedCapacityUnits,
			adjustment.QualityDeltaMilli, adjustment.CompletedTick,
			[]byte(adjustment.Metadata)); err != nil {
			return count, fmt.Errorf("restore city building adjustment %s: %w", adjustment.ProjectCode, err)
		}
		count++
	}
	return count, nil
}

func clearCityDevelopmentProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) (int, error) {
	count := 0
	for _, statement := range []string{
		`DELETE FROM city_building_adjustments WHERE world_id = $1`,
		`DELETE FROM city_development_facts WHERE world_id = $1`,
		`DELETE FROM city_development_projects WHERE world_id = $1`,
		`DELETE FROM city_development_baselines WHERE world_id = $1`,
		`DELETE FROM city_development_profiles WHERE world_id = $1`,
	} {
		result, err := tx.ExecContext(ctx, statement, worldID)
		if err != nil {
			return count, fmt.Errorf("clear city development recovery projection: %w", err)
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return count, rowsErr
		}
		count += int(rows)
	}
	return count, nil
}
