package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

type cityEnterpriseLocationRecoveryFactKey struct {
	tick     int64
	sequence int64
}

func loadCityEnterpriseLocationRecoveryFactIDs(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (map[cityEnterpriseLocationRecoveryFactKey]int64, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT id, tick, sequence
FROM city_enterprise_location_facts
WHERE world_id = $1
ORDER BY tick ASC, sequence ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city enterprise location recovery fact identities: %w", err)
	}
	identities := make(map[cityEnterpriseLocationRecoveryFactKey]int64)
	for rows.Next() {
		var id, tick, sequence int64
		if err = rows.Scan(&id, &tick, &sequence); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan city enterprise location recovery fact identity: %w", err)
		}
		identities[cityEnterpriseLocationRecoveryFactKey{tick: tick, sequence: sequence}] = id
	}
	if err = closeCityRows(rows, "iterate city enterprise location recovery fact identities"); err != nil {
		return nil, err
	}
	return identities, nil
}

func restoreCityEnterpriseLocationProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	state *cityHashState,
	preservedFactIDs map[cityEnterpriseLocationRecoveryFactKey]int64,
) (int, error) {
	if state == nil || state.EnterpriseLocation == nil ||
		!cityEngineSupportsEnterpriseLocation(state.SimulationVersion) {
		return 0, fmt.Errorf("recovery enterprise location state is unavailable")
	}
	location := state.EnterpriseLocation
	if location.Profile.PolicyID != cityEnterpriseLocationPolicyID ||
		location.Profile.PolicyVersion != cityEnterpriseLocationPolicyVersion ||
		location.Profile.PolicyHash != cityEnterpriseLocationPolicyHash ||
		location.Profile.SiteCount != int64(len(location.Sites)) ||
		location.Profile.FactCount != int64(len(location.Facts)) ||
		location.Profile.Revision != int64(len(location.Facts))+1 ||
		location.Profile.BaselineSiteCount < 0 ||
		location.Profile.BaselineSiteCount > location.Profile.SiteCount ||
		location.Profile.BaselineSiteCount != int64(len(location.BaselineSites)) {
		return 0, fmt.Errorf("recovery enterprise location profile is inconsistent")
	}
	baselineHash, err := cityEnterpriseLocationBaselineHash(location.BaselineSites)
	if err != nil || baselineHash != location.Profile.BaselineHash {
		return 0, fmt.Errorf("recovery enterprise location baseline hash is inconsistent")
	}

	count, err := clearCityEnterpriseLocationProjection(ctx, tx, worldID)
	if err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_enterprise_location_profiles
    (world_id, policy_id, policy_version, policy_hash, baseline_tick, baseline_hash,
     site_count, fact_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, '{"schema_version":1}'::jsonb)`,
		worldID, location.Profile.PolicyID, location.Profile.PolicyVersion,
		location.Profile.PolicyHash, location.Profile.BaselineTick,
		location.Profile.BaselineHash, location.Profile.SiteCount,
		location.Profile.FactCount, location.Profile.Revision); err != nil {
		return count, fmt.Errorf("restore city enterprise location profile: %w", err)
	}
	count++
	baselineMetadata, err := json.Marshal(struct {
		SchemaVersion int                  `json:"schema_version"`
		Sites         []CityEnterpriseSite `json:"sites"`
	}{SchemaVersion: 1, Sites: location.BaselineSites})
	if err != nil {
		return count, fmt.Errorf("marshal recovery enterprise location baseline: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_enterprise_location_baselines
    (world_id, tick, policy_hash, baseline_hash, site_count, metadata, posted_at)
VALUES ($1, $2, $3, $4, $5, $6::jsonb, NOW())`,
		worldID, location.Profile.BaselineTick, location.Profile.PolicyHash,
		location.Profile.BaselineHash, location.Profile.BaselineSiteCount,
		string(baselineMetadata)); err != nil {
		return count, fmt.Errorf("restore city enterprise location baseline: %w", err)
	}
	count++

	for _, site := range location.Sites {
		var firmID, districtID, buildingID, poolID int64
		if err = tx.QueryRowContext(ctx, `
SELECT firm.id, district.id, building.id, pool.id
FROM city_economic_entities firm
JOIN city_districts district
  ON district.world_id = firm.world_id AND district.code = $3
JOIN city_buildings building
  ON building.world_id = firm.world_id AND building.code = $4
 AND building.district_id = district.id
JOIN city_building_unit_pools pool
  ON pool.world_id = firm.world_id AND pool.code = $5
 AND pool.building_id = building.id AND pool.district_id = district.id
WHERE firm.world_id = $1 AND firm.code = $2 AND firm.entity_type = 'firm'`,
			worldID, site.FirmEntityCode, site.DistrictCode,
			site.BuildingCode, site.PoolCode).Scan(
			&firmID, &districtID, &buildingID, &poolID,
		); err != nil {
			return count, fmt.Errorf("resolve enterprise recovery site %s: %w", site.Code, err)
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_enterprise_sites
    (world_id, code, firm_entity_id, entity_type, district_id, building_id, pool_id,
     site_type, name, occupied_units, is_primary, status, opened_tick,
     last_changed_tick, closed_tick, version, metadata)
VALUES ($1, $2, $3, 'firm', $4, $5, $6, $7, $8, $9, $10, $11, $12,
        $13, $14, $15, $16::jsonb)`,
			worldID, site.Code, firmID, districtID, buildingID, poolID,
			site.SiteType, site.Name, site.OccupiedUnits, site.IsPrimary,
			site.Status, site.OpenedTick, site.LastChangedTick,
			enterpriseNullableInt64(site.ClosedTick), site.Version, string(site.Metadata)); err != nil {
			return count, fmt.Errorf("restore city enterprise site %s: %w", site.Code, err)
		}
		count++
	}

	for _, fact := range location.Facts {
		var sourceCommandID, firmID int64
		if err = tx.QueryRowContext(ctx, `
SELECT id FROM city_commands WHERE world_id = $1 AND sequence = $2`,
			worldID, fact.SourceCommandSequence).Scan(&sourceCommandID); err != nil {
			return count, fmt.Errorf("resolve city enterprise location source command: %w", err)
		}
		if err = tx.QueryRowContext(ctx, `
SELECT id FROM city_economic_entities
WHERE world_id = $1 AND code = $2 AND entity_type = 'firm'`,
			worldID, fact.FirmEntityCode).Scan(&firmID); err != nil {
			return count, fmt.Errorf("resolve city enterprise location fact firm: %w", err)
		}
		preservedID := preservedFactIDs[cityEnterpriseLocationRecoveryFactKey{
			tick: fact.Tick, sequence: fact.Sequence,
		}]
		insertPrefix := `
INSERT INTO city_enterprise_location_facts
    (world_id, tick, sequence, source_command_id, firm_entity_id, entity_type,
     site_code, fact_type, from_status, to_status, occupied_before_units,
     occupied_after_units, site_version_before, site_version_after, metadata, posted_at)
VALUES ($1, $2, $3, $4, $5, 'firm', $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb, NOW())`
		args := []any{
			worldID, fact.Tick, fact.Sequence, sourceCommandID, firmID,
			enterpriseNullableString(fact.SiteCode), fact.FactType,
			enterpriseNullableString(fact.FromStatus), enterpriseNullableString(fact.ToStatus),
			fact.OccupiedBeforeUnits, fact.OccupiedAfterUnits,
			fact.SiteVersionBefore, fact.SiteVersionAfter, string(fact.Metadata),
		}
		if preservedID > 0 {
			insertPrefix = `
INSERT INTO city_enterprise_location_facts
    (id, world_id, tick, sequence, source_command_id, firm_entity_id, entity_type,
     site_code, fact_type, from_status, to_status, occupied_before_units,
     occupied_after_units, site_version_before, site_version_after, metadata, posted_at)
VALUES ($1, $2, $3, $4, $5, $6, 'firm', $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb, NOW())`
			args = append([]any{preservedID}, args...)
		}
		if _, err = tx.ExecContext(ctx, insertPrefix, args...); err != nil {
			return count, fmt.Errorf("restore city enterprise location fact %d/%d: %w", fact.Tick, fact.Sequence, err)
		}
		count++
	}
	return count, nil
}

func clearCityEnterpriseLocationProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
) (int, error) {
	count := 0
	for _, statement := range []string{
		`DELETE FROM city_enterprise_location_facts WHERE world_id = $1`,
		`DELETE FROM city_enterprise_sites WHERE world_id = $1`,
		`DELETE FROM city_enterprise_location_baselines WHERE world_id = $1`,
		`DELETE FROM city_enterprise_location_profiles WHERE world_id = $1`,
	} {
		result, err := tx.ExecContext(ctx, statement, worldID)
		if err != nil {
			return count, fmt.Errorf("clear city enterprise location recovery projection: %w", err)
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return count, rowsErr
		}
		count += int(rows)
	}
	return count, nil
}

func enterpriseNullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func enterpriseNullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}
