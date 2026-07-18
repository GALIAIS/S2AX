package service

import (
	"context"
	"fmt"
	"strings"
)

const (
	cityEnterpriseLocationDefaultFactLimit = 50
	cityEnterpriseLocationMaximumFactLimit = 200
)

type CityEnterpriseLocationQueryInput struct {
	UserID        int64
	WorldID       int64
	FirmCode      string
	DistrictCode  string
	SiteType      string
	Status        string
	AfterTick     int64
	AfterSequence int64
	Limit         int
}

type CityEnterpriseFirmOption struct {
	EntityID                int64  `json:"entity_id"`
	EntityCode              string `json:"entity_code"`
	EntityName              string `json:"entity_name"`
	DistrictCode            string `json:"district_code"`
	EmployeeUnits           int64  `json:"employee_units"`
	CapitalStockUnits       int64  `json:"capital_stock_units"`
	ProductionCapacityUnits int64  `json:"production_capacity_units"`
	ActiveSiteCount         int64  `json:"active_site_count"`
}

type CityEnterprisePoolAvailability struct {
	Code               string `json:"code"`
	BuildingCode       string `json:"building_code"`
	DistrictCode       string `json:"district_code"`
	UseType            string `json:"use_type"`
	EffectiveUnitCount int64  `json:"effective_unit_count"`
	OccupiedUnitCount  int64  `json:"occupied_unit_count"`
	AvailableUnitCount int64  `json:"available_unit_count"`
}

type CityEnterpriseLocationOverview struct {
	Profile       CityEnterpriseLocationProfile     `json:"profile"`
	BaselineSites []CityEnterpriseSite              `json:"baseline_sites"`
	Sites         []CityEnterpriseSite              `json:"sites"`
	Facts         []CityEnterpriseLocationFact      `json:"facts"`
	Firms         []CityEnterpriseFirmOption        `json:"firms"`
	Pools         []CityEnterprisePoolAvailability  `json:"pools"`
	NextCursor    *CityEnterpriseLocationFactCursor `json:"next_cursor,omitempty"`
}

func (s *CityEconomyService) GetEnterpriseLocationState(
	ctx context.Context,
	input CityEnterpriseLocationQueryInput,
) (*CityEnterpriseLocationOverview, error) {
	input.FirmCode = strings.ToLower(strings.TrimSpace(input.FirmCode))
	input.DistrictCode = strings.ToLower(strings.TrimSpace(input.DistrictCode))
	input.SiteType = strings.ToLower(strings.TrimSpace(input.SiteType))
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if input.UserID <= 0 || input.WorldID <= 0 || input.AfterTick < 0 || input.AfterSequence < 0 ||
		(input.FirmCode != "" && !cityEnterpriseLongCodePattern.MatchString(input.FirmCode)) ||
		(input.DistrictCode != "" && !cityEnterpriseLongCodePattern.MatchString(input.DistrictCode)) {
		return nil, ErrCityInvalidInput
	}
	if input.SiteType != "" {
		if _, valid := cityEnterpriseAllowedPoolUse(input.SiteType); !valid {
			return nil, ErrCityInvalidInput
		}
	}
	if input.Status != "" && input.Status != CityEnterpriseSiteStatusActive &&
		input.Status != CityEnterpriseSiteStatusClosed {
		return nil, ErrCityInvalidInput
	}
	if input.Limit <= 0 {
		input.Limit = cityEnterpriseLocationDefaultFactLimit
	}
	if input.Limit > cityEnterpriseLocationMaximumFactLimit {
		return nil, ErrCityInvalidInput
	}
	if err := authorizeCityWorldRead(ctx, s.db, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	var simulationVersion string
	if err := s.db.QueryRowContext(ctx,
		`SELECT simulation_version FROM city_worlds WHERE id = $1`, input.WorldID,
	).Scan(&simulationVersion); err != nil {
		return nil, ErrCityEnterpriseLocationStateNotFound
	}
	state, err := loadCityEnterpriseLocationHashState(ctx, s.db, input.WorldID, simulationVersion)
	if err != nil {
		return nil, err
	}

	filteredSites := make([]CityEnterpriseSite, 0, len(state.Sites))
	matchingSiteCodes := make(map[string]struct{})
	for _, site := range state.Sites {
		if input.FirmCode != "" && site.FirmEntityCode != input.FirmCode ||
			input.DistrictCode != "" && site.DistrictCode != input.DistrictCode ||
			input.SiteType != "" && site.SiteType != input.SiteType ||
			input.Status != "" && site.Status != input.Status {
			continue
		}
		filteredSites = append(filteredSites, site)
		matchingSiteCodes[site.Code] = struct{}{}
	}

	filteredFacts := make([]CityEnterpriseLocationFact, 0, input.Limit+1)
	for _, fact := range state.Facts {
		if fact.Tick < input.AfterTick || fact.Tick == input.AfterTick && fact.Sequence <= input.AfterSequence {
			continue
		}
		if input.FirmCode != "" && fact.FirmEntityCode != input.FirmCode {
			continue
		}
		if input.DistrictCode != "" || input.SiteType != "" || input.Status != "" {
			if fact.SiteCode != nil {
				if _, matches := matchingSiteCodes[*fact.SiteCode]; !matches {
					continue
				}
			} else if input.FirmCode == "" {
				continue
			}
		}
		filteredFacts = append(filteredFacts, fact)
		if len(filteredFacts) > input.Limit {
			break
		}
	}
	firms, pools, err := loadCityEnterpriseLocationQueryDimensions(ctx, s, input.WorldID)
	if err != nil {
		return nil, err
	}
	result := &CityEnterpriseLocationOverview{
		Profile: state.Profile, BaselineSites: state.BaselineSites,
		Sites: filteredSites, Facts: filteredFacts, Firms: firms, Pools: pools,
	}
	if len(result.Facts) > input.Limit {
		result.Facts = result.Facts[:input.Limit]
		last := result.Facts[len(result.Facts)-1]
		result.NextCursor = &CityEnterpriseLocationFactCursor{Tick: last.Tick, Sequence: last.Sequence}
	}
	return result, nil
}

func loadCityEnterpriseLocationQueryDimensions(
	ctx context.Context,
	s *CityEconomyService,
	worldID int64,
) ([]CityEnterpriseFirmOption, []CityEnterprisePoolAvailability, error) {
	firmRows, err := s.db.QueryContext(ctx, `
SELECT entity.id, entity.code, entity.name, district.code,
       firm.employee_units, firm.capital_stock_units, firm.production_capacity_units,
       COUNT(site.id) FILTER (WHERE site.status = 'active')::BIGINT
FROM city_firm_states firm
JOIN city_economic_entities entity
  ON entity.id = firm.entity_id AND entity.world_id = firm.world_id
JOIN city_districts district
  ON district.id = firm.district_id AND district.world_id = firm.world_id
LEFT JOIN city_enterprise_sites site
  ON site.world_id = firm.world_id AND site.firm_entity_id = firm.entity_id
WHERE firm.world_id = $1 AND entity.status = 'active'
GROUP BY entity.id, entity.code, entity.name, district.code,
         firm.employee_units, firm.capital_stock_units, firm.production_capacity_units
ORDER BY entity.code ASC`, worldID)
	if err != nil {
		return nil, nil, fmt.Errorf("list city enterprise firms: %w", err)
	}
	firms := make([]CityEnterpriseFirmOption, 0)
	for firmRows.Next() {
		var item CityEnterpriseFirmOption
		if err = firmRows.Scan(
			&item.EntityID, &item.EntityCode, &item.EntityName, &item.DistrictCode,
			&item.EmployeeUnits, &item.CapitalStockUnits,
			&item.ProductionCapacityUnits, &item.ActiveSiteCount,
		); err != nil {
			_ = firmRows.Close()
			return nil, nil, fmt.Errorf("scan city enterprise firm option: %w", err)
		}
		firms = append(firms, item)
	}
	if err = closeCityRows(firmRows, "iterate city enterprise firm options"); err != nil {
		return nil, nil, err
	}

	poolRows, err := s.db.QueryContext(ctx, `
SELECT pool.code, building.code, district.code, pool.use_type,
       pool.unit_count + COALESCE(adjustment.added_capacity, 0) / pool.capacity_units_per_unit,
       COALESCE(occupied.units, 0)::BIGINT
FROM city_building_unit_pools pool
JOIN city_buildings building
  ON building.id = pool.building_id AND building.world_id = pool.world_id
JOIN city_parcels parcel
  ON parcel.id = building.parcel_id AND parcel.world_id = building.world_id
JOIN city_districts district
  ON district.id = pool.district_id AND district.world_id = pool.world_id
LEFT JOIN LATERAL (
    SELECT COALESCE(SUM(value.added_capacity_units), 0)::BIGINT AS added_capacity
    FROM city_building_adjustments value
    WHERE value.world_id = pool.world_id AND value.building_id = pool.building_id
) adjustment ON TRUE
LEFT JOIN LATERAL (
    SELECT COALESCE(SUM(site.occupied_units), 0)::BIGINT AS units
    FROM city_enterprise_sites site
    WHERE site.world_id = pool.world_id AND site.pool_id = pool.id AND site.status = 'active'
) occupied ON TRUE
WHERE pool.world_id = $1 AND pool.use_type IN ('commercial', 'industrial')
  AND building.status = 'active' AND parcel.status = 'active'
ORDER BY district.sort_order ASC, pool.use_type ASC, pool.code ASC`, worldID)
	if err != nil {
		return nil, nil, fmt.Errorf("list city enterprise pool availability: %w", err)
	}
	pools := make([]CityEnterprisePoolAvailability, 0)
	for poolRows.Next() {
		var item CityEnterprisePoolAvailability
		if err = poolRows.Scan(
			&item.Code, &item.BuildingCode, &item.DistrictCode, &item.UseType,
			&item.EffectiveUnitCount, &item.OccupiedUnitCount,
		); err != nil {
			_ = poolRows.Close()
			return nil, nil, fmt.Errorf("scan city enterprise pool availability: %w", err)
		}
		item.AvailableUnitCount = item.EffectiveUnitCount - item.OccupiedUnitCount
		pools = append(pools, item)
	}
	if err = closeCityRows(poolRows, "iterate city enterprise pool availability"); err != nil {
		return nil, nil, err
	}
	return firms, pools, nil
}
