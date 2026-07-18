package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	CityCommandTypeEnterpriseSiteOpen   = "enterprise.site.open"
	CityCommandTypeEnterpriseSiteResize = "enterprise.site.resize"
	CityCommandTypeEnterpriseSiteClose  = "enterprise.site.close"
	CityCommandTypeEnterpriseRelocate   = "enterprise.relocate"

	CityEnterpriseSiteHeadquarters = "headquarters"
	CityEnterpriseSiteOffice       = "office"
	CityEnterpriseSiteProduction   = "production"
	CityEnterpriseSiteWarehouse    = "warehouse"
	CityEnterpriseSiteRetail       = "retail"

	CityEnterpriseSiteStatusActive = "active"
	CityEnterpriseSiteStatusClosed = "closed"

	CityEnterpriseLocationFactOpened    = "opened"
	CityEnterpriseLocationFactResized   = "resized"
	CityEnterpriseLocationFactClosed    = "closed"
	CityEnterpriseLocationFactRelocated = "relocated"

	cityEnterpriseLocationPolicyID        = "sub2api-enterprise-location"
	cityEnterpriseLocationPolicyVersion   = "1.0.0"
	cityEnterpriseLocationPolicyCanonical = `{"capital_stock_per_warehouse_unit":10,"employee_units_per_office_unit":4,"employee_units_per_retail_unit":8,"id":"sub2api-enterprise-location","maximum_active_sites_per_firm":32,"production_capacity_per_production_unit":2,"site_types":{"headquarters":["commercial"],"office":["commercial"],"production":["industrial"],"retail":["commercial"],"warehouse":["industrial"]},"version":"1.0.0"}`
	cityEnterpriseLocationPolicyHash      = "b5ec620c0b3bbe81b564a59fe0c372bce97932b31d7d5af341fe62a2b362f39d"

	cityEnterpriseEmployeeUnitsPerOfficeUnit          int64 = 4
	cityEnterpriseProductionCapacityPerProductionUnit int64 = 2
	cityEnterpriseCapitalStockPerWarehouseUnit        int64 = 10
	cityEnterpriseEmployeeUnitsPerRetailUnit          int64 = 8
	cityEnterpriseMaximumActiveSitesPerFirm                 = 32
)

var (
	ErrCityEnterpriseLocationStateNotFound = infraerrors.NotFound(
		"CITY_ENTERPRISE_LOCATION_STATE_NOT_FOUND", "city enterprise location state not found",
	)
	errCityEnterprisePlacementInvalid  = errors.New("invalid enterprise placement input")
	errCityEnterprisePlacementCapacity = errors.New("insufficient enterprise placement capacity")
)

type CityEnterpriseLocationProfile struct {
	PolicyID          string `json:"policy_id"`
	PolicyVersion     string `json:"policy_version"`
	PolicyHash        string `json:"policy_hash"`
	BaselineTick      int64  `json:"baseline_tick"`
	BaselineHash      string `json:"baseline_hash"`
	BaselineSiteCount int64  `json:"baseline_site_count"`
	SiteCount         int64  `json:"site_count"`
	FactCount         int64  `json:"fact_count"`
	Revision          int64  `json:"revision"`
}

type CityEnterpriseSite struct {
	Code            string          `json:"code"`
	FirmEntityCode  string          `json:"firm_entity_code"`
	DistrictCode    string          `json:"district_code"`
	BuildingCode    string          `json:"building_code"`
	PoolCode        string          `json:"pool_code"`
	SiteType        string          `json:"site_type"`
	Name            string          `json:"name"`
	OccupiedUnits   int64           `json:"occupied_units"`
	IsPrimary       bool            `json:"is_primary"`
	Status          string          `json:"status"`
	OpenedTick      int64           `json:"opened_tick"`
	LastChangedTick int64           `json:"last_changed_tick"`
	ClosedTick      *int64          `json:"closed_tick,omitempty"`
	Version         int64           `json:"version"`
	Metadata        json.RawMessage `json:"metadata"`
}

type CityEnterpriseLocationFact struct {
	Tick                  int64           `json:"tick"`
	Sequence              int64           `json:"sequence"`
	SourceCommandSequence int64           `json:"source_command_sequence"`
	FirmEntityCode        string          `json:"firm_entity_code"`
	SiteCode              *string         `json:"site_code,omitempty"`
	FactType              string          `json:"fact_type"`
	FromStatus            *string         `json:"from_status,omitempty"`
	ToStatus              *string         `json:"to_status,omitempty"`
	OccupiedBeforeUnits   int64           `json:"occupied_before_units"`
	OccupiedAfterUnits    int64           `json:"occupied_after_units"`
	SiteVersionBefore     int64           `json:"site_version_before"`
	SiteVersionAfter      int64           `json:"site_version_after"`
	Metadata              json.RawMessage `json:"metadata"`
}

type CityEnterpriseLocationFactCursor struct {
	Tick     int64 `json:"tick"`
	Sequence int64 `json:"sequence"`
}

type CityEnterpriseLocationState struct {
	Profile       CityEnterpriseLocationProfile     `json:"profile"`
	BaselineSites []CityEnterpriseSite              `json:"baseline_sites"`
	Sites         []CityEnterpriseSite              `json:"sites"`
	Facts         []CityEnterpriseLocationFact      `json:"facts"`
	NextCursor    *CityEnterpriseLocationFactCursor `json:"next_cursor,omitempty"`
}

type cityEnterpriseLocationHashState = CityEnterpriseLocationState

type cityEnterpriseFirmLocationSnapshot struct {
	DistrictCode string `json:"district_code"`
	Version      int64  `json:"version"`
}

type cityEnterpriseLocationFactMetadata struct {
	SchemaVersion              int                                 `json:"schema_version"`
	Site                       *CityEnterpriseSite                 `json:"site,omitempty"`
	SitesBefore                []CityEnterpriseSite                `json:"sites_before,omitempty"`
	SitesAfter                 []CityEnterpriseSite                `json:"sites_after,omitempty"`
	FirmBefore                 *cityEnterpriseFirmLocationSnapshot `json:"firm_before,omitempty"`
	FirmAfter                  *cityEnterpriseFirmLocationSnapshot `json:"firm_after,omitempty"`
	ResourceOperationSequences []int64                             `json:"resource_operation_sequences,omitempty"`
	Reason                     string                              `json:"reason,omitempty"`
}

func isCityEnterpriseLocationCommand(commandType string) bool {
	switch commandType {
	case CityCommandTypeEnterpriseSiteOpen, CityCommandTypeEnterpriseSiteResize,
		CityCommandTypeEnterpriseSiteClose, CityCommandTypeEnterpriseRelocate:
		return true
	default:
		return false
	}
}

type cityEnterprisePlacementFirm struct {
	EntityID                int64
	Code                    string
	Name                    string
	DistrictID              int64
	DistrictCode            string
	EmployeeUnits           int64
	CapitalStockUnits       int64
	ProductionCapacityUnits int64
}

type cityEnterprisePlacementPool struct {
	PoolID             int64
	PoolCode           string
	BuildingID         int64
	BuildingCode       string
	DistrictID         int64
	DistrictCode       string
	UseType            string
	EffectiveUnitCount int64
}

type cityEnterpriseSitePlacement struct {
	FirmID     int64
	DistrictID int64
	BuildingID int64
	PoolID     int64
	Site       CityEnterpriseSite
}

func cityEnterpriseMinimumOccupiedUnits(
	siteType string,
	employeeUnits, capitalStockUnits, productionCapacityUnits int64,
) (int64, error) {
	if employeeUnits < 0 || capitalStockUnits < 0 || productionCapacityUnits < 0 {
		return 0, errCityEnterprisePlacementInvalid
	}
	var value, divisor int64
	switch siteType {
	case CityEnterpriseSiteHeadquarters, CityEnterpriseSiteOffice:
		value, divisor = employeeUnits, cityEnterpriseEmployeeUnitsPerOfficeUnit
	case CityEnterpriseSiteProduction:
		value, divisor = productionCapacityUnits, cityEnterpriseProductionCapacityPerProductionUnit
	case CityEnterpriseSiteWarehouse:
		value, divisor = capitalStockUnits, cityEnterpriseCapitalStockPerWarehouseUnit
	case CityEnterpriseSiteRetail:
		value, divisor = employeeUnits, cityEnterpriseEmployeeUnitsPerRetailUnit
	default:
		return 0, errCityEnterprisePlacementInvalid
	}
	if value == 0 {
		return 1, nil
	}
	if value > math.MaxInt64-(divisor-1) {
		return 0, errCityEnterprisePlacementInvalid
	}
	return (value + divisor - 1) / divisor, nil
}

func cityEnterpriseAllowedPoolUse(siteType string) (string, bool) {
	switch siteType {
	case CityEnterpriseSiteHeadquarters, CityEnterpriseSiteOffice, CityEnterpriseSiteRetail:
		return "commercial", true
	case CityEnterpriseSiteProduction, CityEnterpriseSiteWarehouse:
		return "industrial", true
	default:
		return "", false
	}
}

func planInitialCityEnterpriseSites(
	baselineTick int64,
	firms []cityEnterprisePlacementFirm,
	pools []cityEnterprisePlacementPool,
) ([]cityEnterpriseSitePlacement, error) {
	if baselineTick < 0 || len(firms) == 0 || len(pools) == 0 {
		return nil, errCityEnterprisePlacementInvalid
	}
	firms = append([]cityEnterprisePlacementFirm(nil), firms...)
	pools = append([]cityEnterprisePlacementPool(nil), pools...)
	sort.Slice(firms, func(i, j int) bool { return firms[i].Code < firms[j].Code })
	sort.Slice(pools, func(i, j int) bool { return pools[i].PoolCode < pools[j].PoolCode })

	firmCodes := make(map[string]struct{}, len(firms))
	remaining := make(map[int64]int64, len(pools))
	poolCodes := make(map[string]struct{}, len(pools))
	for _, pool := range pools {
		if pool.PoolID <= 0 || pool.BuildingID <= 0 || pool.DistrictID <= 0 ||
			pool.PoolCode == "" || pool.BuildingCode == "" || pool.DistrictCode == "" ||
			pool.EffectiveUnitCount < 0 || (pool.UseType != "commercial" && pool.UseType != "industrial") {
			return nil, errCityEnterprisePlacementInvalid
		}
		if _, duplicate := poolCodes[pool.PoolCode]; duplicate {
			return nil, errCityEnterprisePlacementInvalid
		}
		poolCodes[pool.PoolCode] = struct{}{}
		remaining[pool.PoolID] = pool.EffectiveUnitCount
	}

	placements := make([]cityEnterpriseSitePlacement, 0, len(firms)*2)
	for _, firm := range firms {
		if firm.EntityID <= 0 || firm.DistrictID <= 0 || firm.Code == "" || firm.Name == "" ||
			firm.DistrictCode == "" || firm.EmployeeUnits < 0 || firm.CapitalStockUnits < 0 ||
			firm.ProductionCapacityUnits <= 0 {
			return nil, errCityEnterprisePlacementInvalid
		}
		if _, duplicate := firmCodes[firm.Code]; duplicate {
			return nil, errCityEnterprisePlacementInvalid
		}
		firmCodes[firm.Code] = struct{}{}

		for _, requiredSite := range []struct {
			siteType string
			suffix   string
			name     string
		}{
			{CityEnterpriseSiteHeadquarters, "headquarters", firm.Name + " Headquarters"},
			{CityEnterpriseSiteProduction, "production", firm.Name + " Production Site"},
		} {
			requiredUnits, err := cityEnterpriseMinimumOccupiedUnits(
				requiredSite.siteType, firm.EmployeeUnits, firm.CapitalStockUnits,
				firm.ProductionCapacityUnits,
			)
			if err != nil {
				return nil, err
			}
			allowedUse, _ := cityEnterpriseAllowedPoolUse(requiredSite.siteType)
			poolIndex := -1
			for index := range pools {
				pool := pools[index]
				if pool.DistrictID == firm.DistrictID && pool.DistrictCode == firm.DistrictCode &&
					pool.UseType == allowedUse && remaining[pool.PoolID] >= requiredUnits {
					poolIndex = index
					break
				}
			}
			if poolIndex < 0 {
				return nil, fmt.Errorf("%w: firm=%s site_type=%s", errCityEnterprisePlacementCapacity, firm.Code, requiredSite.siteType)
			}
			pool := pools[poolIndex]
			remaining[pool.PoolID] -= requiredUnits
			placements = append(placements, cityEnterpriseSitePlacement{
				FirmID: firm.EntityID, DistrictID: pool.DistrictID,
				BuildingID: pool.BuildingID, PoolID: pool.PoolID,
				Site: CityEnterpriseSite{
					Code:           "site_" + firm.Code + "_" + requiredSite.suffix,
					FirmEntityCode: firm.Code, DistrictCode: pool.DistrictCode,
					BuildingCode: pool.BuildingCode, PoolCode: pool.PoolCode,
					SiteType: requiredSite.siteType, Name: requiredSite.name,
					OccupiedUnits: requiredUnits, IsPrimary: true,
					Status:     CityEnterpriseSiteStatusActive,
					OpenedTick: baselineTick, LastChangedTick: baselineTick,
					Version:  1,
					Metadata: json.RawMessage(`{"policy_hash":"` + cityEnterpriseLocationPolicyHash + `","schema_version":1,"source":"baseline"}`),
				},
			})
		}
	}
	return placements, nil
}

func cityEnterpriseLocationBaselineHash(sites []CityEnterpriseSite) (string, error) {
	sites = append([]CityEnterpriseSite(nil), sites...)
	sort.Slice(sites, func(i, j int) bool { return sites[i].Code < sites[j].Code })
	for index := range sites {
		var metadata any
		if err := json.Unmarshal(sites[index].Metadata, &metadata); err != nil {
			return "", fmt.Errorf("decode city enterprise location baseline metadata: %w", err)
		}
		normalized, err := json.Marshal(metadata)
		if err != nil {
			return "", fmt.Errorf("normalize city enterprise location baseline metadata: %w", err)
		}
		sites[index].Metadata = normalized
	}
	raw, err := json.Marshal(struct {
		PolicyHash string               `json:"policy_hash"`
		Sites      []CityEnterpriseSite `json:"sites"`
	}{PolicyHash: cityEnterpriseLocationPolicyHash, Sites: sites})
	if err != nil {
		return "", fmt.Errorf("marshal city enterprise location baseline: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func initializeCityEnterpriseLocationFoundation(ctx context.Context, tx *sql.Tx, worldID int64) error {
	if worldID <= 0 {
		return errCityEnterprisePlacementInvalid
	}
	if _, err := tx.ExecContext(
		ctx,
		`SELECT set_config('sub2api.city_f7_initialize_world_id', $1, TRUE)`,
		strconv.FormatInt(worldID, 10),
	); err != nil {
		return fmt.Errorf("activate city F7.5 initialization gate: %w", err)
	}

	var baselineTick, existingProfiles int64
	if err := tx.QueryRowContext(ctx, `
SELECT world.current_tick,
       (SELECT COUNT(*) FROM city_enterprise_location_profiles profile WHERE profile.world_id = world.id)
FROM city_worlds world
WHERE world.id = $1
FOR UPDATE`, worldID).Scan(&baselineTick, &existingProfiles); err != nil {
		return fmt.Errorf("lock city F7.5 world: %w", err)
	}
	if existingProfiles != 0 {
		return fmt.Errorf("city F7.5 enterprise location foundation already exists")
	}

	firmRows, err := tx.QueryContext(ctx, `
SELECT entity.id, entity.code, entity.name, district.id, district.code,
       firm.employee_units, firm.capital_stock_units, firm.production_capacity_units
FROM city_firm_states firm
JOIN city_economic_entities entity
  ON entity.id = firm.entity_id AND entity.world_id = firm.world_id
JOIN city_districts district
  ON district.id = firm.district_id AND district.world_id = firm.world_id
WHERE firm.world_id = $1 AND entity.entity_type = 'firm' AND entity.status = 'active'
ORDER BY entity.code ASC`, worldID)
	if err != nil {
		return fmt.Errorf("load city F7.5 firms: %w", err)
	}
	firms := make([]cityEnterprisePlacementFirm, 0)
	for firmRows.Next() {
		var firm cityEnterprisePlacementFirm
		if err = firmRows.Scan(
			&firm.EntityID, &firm.Code, &firm.Name, &firm.DistrictID, &firm.DistrictCode,
			&firm.EmployeeUnits, &firm.CapitalStockUnits, &firm.ProductionCapacityUnits,
		); err != nil {
			_ = firmRows.Close()
			return fmt.Errorf("scan city F7.5 firm: %w", err)
		}
		firms = append(firms, firm)
	}
	if err = closeCityRows(firmRows, "iterate city F7.5 firms"); err != nil {
		return err
	}

	poolRows, err := tx.QueryContext(ctx, `
SELECT pool.id, pool.code, building.id, building.code, district.id, district.code,
       pool.use_type,
       pool.unit_count + COALESCE(adjustment.added_capacity_units, 0) / pool.capacity_units_per_unit
FROM city_building_unit_pools pool
JOIN city_buildings building
  ON building.id = pool.building_id AND building.world_id = pool.world_id
JOIN city_districts district
  ON district.id = pool.district_id AND district.world_id = pool.world_id
LEFT JOIN LATERAL (
    SELECT COALESCE(SUM(value.added_capacity_units), 0)::BIGINT AS added_capacity_units
    FROM city_building_adjustments value
    WHERE value.world_id = pool.world_id AND value.building_id = pool.building_id
) adjustment ON TRUE
WHERE pool.world_id = $1 AND pool.use_type IN ('commercial', 'industrial')
  AND building.status = 'active'
ORDER BY pool.code ASC`, worldID)
	if err != nil {
		return fmt.Errorf("load city F7.5 building pools: %w", err)
	}
	pools := make([]cityEnterprisePlacementPool, 0)
	for poolRows.Next() {
		var pool cityEnterprisePlacementPool
		if err = poolRows.Scan(
			&pool.PoolID, &pool.PoolCode, &pool.BuildingID, &pool.BuildingCode,
			&pool.DistrictID, &pool.DistrictCode, &pool.UseType, &pool.EffectiveUnitCount,
		); err != nil {
			_ = poolRows.Close()
			return fmt.Errorf("scan city F7.5 building pool: %w", err)
		}
		pools = append(pools, pool)
	}
	if err = closeCityRows(poolRows, "iterate city F7.5 building pools"); err != nil {
		return err
	}

	placements, err := planInitialCityEnterpriseSites(baselineTick, firms, pools)
	if err != nil {
		return fmt.Errorf("plan city F7.5 enterprise placement: %w", err)
	}
	sites := make([]CityEnterpriseSite, 0, len(placements))
	for _, placement := range placements {
		sites = append(sites, placement.Site)
	}
	baselineHash, err := cityEnterpriseLocationBaselineHash(sites)
	if err != nil {
		return err
	}
	baselineMetadata, err := json.Marshal(struct {
		SchemaVersion int                  `json:"schema_version"`
		Sites         []CityEnterpriseSite `json:"sites"`
	}{SchemaVersion: 1, Sites: sites})
	if err != nil {
		return fmt.Errorf("marshal city F7.5 baseline metadata: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_enterprise_location_profiles
    (world_id, policy_id, policy_version, policy_hash, baseline_tick, baseline_hash,
     site_count, fact_count, revision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, 0, 1, '{"schema_version":1}'::jsonb)`,
		worldID, cityEnterpriseLocationPolicyID, cityEnterpriseLocationPolicyVersion,
		cityEnterpriseLocationPolicyHash, baselineTick, baselineHash, len(placements)); err != nil {
		return fmt.Errorf("insert city F7.5 enterprise location profile: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_enterprise_location_baselines
    (world_id, tick, policy_hash, baseline_hash, site_count, metadata)
VALUES ($1, $2, $3, $4, $5, $6::jsonb)`,
		worldID, baselineTick, cityEnterpriseLocationPolicyHash, baselineHash,
		len(placements), string(baselineMetadata)); err != nil {
		return fmt.Errorf("insert city F7.5 enterprise location baseline: %w", err)
	}
	for _, placement := range placements {
		site := placement.Site
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_enterprise_sites
    (world_id, code, firm_entity_id, entity_type, district_id, building_id, pool_id,
     site_type, name, occupied_units, is_primary, status, opened_tick,
     last_changed_tick, closed_tick, version, metadata)
VALUES ($1, $2, $3, 'firm', $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NULL, $14, $15::jsonb)`,
			worldID, site.Code, placement.FirmID, placement.DistrictID,
			placement.BuildingID, placement.PoolID, site.SiteType, site.Name,
			site.OccupiedUnits, site.IsPrimary, site.Status, site.OpenedTick,
			site.LastChangedTick, site.Version, string(site.Metadata)); err != nil {
			return fmt.Errorf("insert city F7.5 enterprise site %s: %w", site.Code, err)
		}
	}
	return nil
}

func loadCityEnterpriseLocationHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	simulationVersion string,
) (*cityEnterpriseLocationHashState, error) {
	if !cityEngineSupportsEnterpriseLocation(simulationVersion) {
		return nil, ErrCityEnterpriseLocationStateNotFound
	}
	state := &cityEnterpriseLocationHashState{
		BaselineSites: make([]CityEnterpriseSite, 0),
		Sites:         make([]CityEnterpriseSite, 0),
		Facts:         make([]CityEnterpriseLocationFact, 0),
	}
	var baselineMetadata []byte
	if err := queryer.QueryRowContext(ctx, `
SELECT profile.policy_id, profile.policy_version, profile.policy_hash,
       profile.baseline_tick, profile.baseline_hash, baseline.site_count,
       profile.site_count, profile.fact_count, profile.revision, baseline.metadata
FROM city_enterprise_location_profiles profile
JOIN city_enterprise_location_baselines baseline ON baseline.world_id = profile.world_id
WHERE profile.world_id = $1`, worldID).Scan(
		&state.Profile.PolicyID, &state.Profile.PolicyVersion, &state.Profile.PolicyHash,
		&state.Profile.BaselineTick, &state.Profile.BaselineHash,
		&state.Profile.BaselineSiteCount,
		&state.Profile.SiteCount, &state.Profile.FactCount, &state.Profile.Revision,
		&baselineMetadata,
	); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityEnterpriseLocationStateNotFound
	} else if err != nil {
		return nil, fmt.Errorf("load city enterprise location profile: %w", err)
	}
	var baseline struct {
		SchemaVersion int                  `json:"schema_version"`
		Sites         []CityEnterpriseSite `json:"sites"`
	}
	if err := json.Unmarshal(baselineMetadata, &baseline); err != nil {
		return nil, fmt.Errorf("decode city enterprise location baseline metadata: %w", err)
	}
	if baseline.SchemaVersion != 1 {
		return nil, fmt.Errorf("city enterprise location baseline metadata version is unsupported")
	}
	if baseline.Sites == nil {
		baseline.Sites = make([]CityEnterpriseSite, 0)
	}
	if int64(len(baseline.Sites)) != state.Profile.BaselineSiteCount {
		return nil, fmt.Errorf("city enterprise location baseline site count is inconsistent")
	}
	baselineHash, hashErr := cityEnterpriseLocationBaselineHash(baseline.Sites)
	if hashErr != nil || baselineHash != state.Profile.BaselineHash {
		return nil, fmt.Errorf("city enterprise location baseline hash is inconsistent")
	}
	state.BaselineSites = baseline.Sites

	siteRows, err := queryer.QueryContext(ctx, `
SELECT site.code, firm.code, district.code, building.code, pool.code,
       site.site_type, site.name, site.occupied_units, site.is_primary,
       site.status, site.opened_tick, site.last_changed_tick, site.closed_tick,
       site.version, site.metadata
FROM city_enterprise_sites site
JOIN city_economic_entities firm
  ON firm.id = site.firm_entity_id AND firm.world_id = site.world_id
JOIN city_districts district
  ON district.id = site.district_id AND district.world_id = site.world_id
JOIN city_buildings building
  ON building.id = site.building_id AND building.world_id = site.world_id
JOIN city_building_unit_pools pool
  ON pool.id = site.pool_id AND pool.world_id = site.world_id
WHERE site.world_id = $1
ORDER BY site.code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city enterprise sites: %w", err)
	}
	for siteRows.Next() {
		var site CityEnterpriseSite
		var closedTick sql.NullInt64
		if err = siteRows.Scan(
			&site.Code, &site.FirmEntityCode, &site.DistrictCode, &site.BuildingCode,
			&site.PoolCode, &site.SiteType, &site.Name, &site.OccupiedUnits,
			&site.IsPrimary, &site.Status, &site.OpenedTick, &site.LastChangedTick,
			&closedTick, &site.Version, &site.Metadata,
		); err != nil {
			_ = siteRows.Close()
			return nil, fmt.Errorf("scan city enterprise site: %w", err)
		}
		site.ClosedTick = nullableInt64Pointer(closedTick)
		state.Sites = append(state.Sites, site)
	}
	if err = closeCityRows(siteRows, "iterate city enterprise sites"); err != nil {
		return nil, err
	}

	factRows, err := queryer.QueryContext(ctx, cityEnterpriseLocationFactCanonicalSelect+`
WHERE fact.world_id = $1 AND fact.posted_at IS NOT NULL
ORDER BY fact.tick ASC, fact.sequence ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load city enterprise location facts: %w", err)
	}
	for factRows.Next() {
		fact, scanErr := scanCityEnterpriseLocationFact(factRows)
		if scanErr != nil {
			_ = factRows.Close()
			return nil, scanErr
		}
		state.Facts = append(state.Facts, *fact)
	}
	if err = closeCityRows(factRows, "iterate city enterprise location facts"); err != nil {
		return nil, err
	}
	return state, nil
}

const cityEnterpriseLocationFactCanonicalSelect = `
SELECT fact.tick, fact.sequence, command.sequence, firm.code, fact.site_code,
       fact.fact_type, fact.from_status, fact.to_status,
       fact.occupied_before_units, fact.occupied_after_units,
       fact.site_version_before, fact.site_version_after, fact.metadata
FROM city_enterprise_location_facts fact
JOIN city_commands command
  ON command.id = fact.source_command_id AND command.world_id = fact.world_id
JOIN city_economic_entities firm
  ON firm.id = fact.firm_entity_id AND firm.world_id = fact.world_id
`

func scanCityEnterpriseLocationFact(row cityScannable) (*CityEnterpriseLocationFact, error) {
	fact := &CityEnterpriseLocationFact{}
	var siteCode, fromStatus, toStatus sql.NullString
	if err := row.Scan(
		&fact.Tick, &fact.Sequence, &fact.SourceCommandSequence, &fact.FirmEntityCode,
		&siteCode, &fact.FactType, &fromStatus, &toStatus,
		&fact.OccupiedBeforeUnits, &fact.OccupiedAfterUnits,
		&fact.SiteVersionBefore, &fact.SiteVersionAfter, &fact.Metadata,
	); err != nil {
		return nil, fmt.Errorf("scan city enterprise location fact: %w", err)
	}
	fact.SiteCode = nullStringPointer(siteCode)
	fact.FromStatus = nullStringPointer(fromStatus)
	fact.ToStatus = nullStringPointer(toStatus)
	return fact, nil
}

func loadCityEnterpriseLocationFactsForTick(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
) ([]CityEnterpriseLocationFact, error) {
	rows, err := queryer.QueryContext(ctx, cityEnterpriseLocationFactCanonicalSelect+`
WHERE fact.world_id = $1 AND fact.tick = $2 AND fact.posted_at IS NOT NULL
ORDER BY fact.sequence ASC`, worldID, tick)
	if err != nil {
		return nil, fmt.Errorf("load city enterprise location facts for tick: %w", err)
	}
	items := make([]CityEnterpriseLocationFact, 0)
	for rows.Next() {
		item, scanErr := scanCityEnterpriseLocationFact(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		items = append(items, *item)
	}
	if err = closeCityRows(rows, "iterate city enterprise location facts for tick"); err != nil {
		return nil, err
	}
	return items, nil
}

func replayCityEnterpriseLocationFacts(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, tick int64,
	state *cityHashState,
) error {
	if state == nil || state.EnterpriseLocation == nil ||
		!cityEngineSupportsEnterpriseLocation(state.SimulationVersion) {
		return fmt.Errorf("city enterprise location replay state is unavailable")
	}
	facts, err := loadCityEnterpriseLocationFactsForTick(ctx, queryer, worldID, tick)
	if err != nil {
		return err
	}
	for index := range facts {
		fact := facts[index]
		if fact.Tick != tick || fact.Sequence != int64(index+1) {
			return fmt.Errorf("city enterprise location fact sequence is not contiguous")
		}
		if err = applyCityEnterpriseLocationFactToHashState(state, fact); err != nil {
			return err
		}
	}
	return nil
}

func applyCityEnterpriseLocationFactToHashState(
	state *cityHashState,
	fact CityEnterpriseLocationFact,
) error {
	location := state.EnterpriseLocation
	if location == nil || fact.FirmEntityCode == "" {
		return fmt.Errorf("city enterprise location fact has no canonical target")
	}
	var metadata cityEnterpriseLocationFactMetadata
	if err := json.Unmarshal(fact.Metadata, &metadata); err != nil {
		return fmt.Errorf("decode city enterprise location fact metadata: %w", err)
	}
	if metadata.SchemaVersion != 1 {
		return fmt.Errorf("city enterprise location fact metadata version is unsupported")
	}

	switch fact.FactType {
	case CityEnterpriseLocationFactOpened:
		if fact.SiteCode == nil || fact.ToStatus == nil || metadata.Site == nil || metadata.Site.Code != *fact.SiteCode ||
			metadata.Site.FirmEntityCode != fact.FirmEntityCode ||
			metadata.Site.Status != CityEnterpriseSiteStatusActive ||
			metadata.Site.OccupiedUnits != fact.OccupiedAfterUnits ||
			metadata.Site.Version != fact.SiteVersionAfter ||
			metadata.Site.OpenedTick != fact.Tick ||
			findCityEnterpriseSite(location.Sites, metadata.Site.Code) >= 0 {
			return fmt.Errorf("opened city enterprise location fact is inconsistent")
		}
		location.Sites = append(location.Sites, *metadata.Site)
		location.Profile.SiteCount++
	case CityEnterpriseLocationFactResized, CityEnterpriseLocationFactClosed:
		if fact.SiteCode == nil || fact.FromStatus == nil || fact.ToStatus == nil ||
			metadata.Site == nil || metadata.Site.Code != *fact.SiteCode ||
			metadata.Site.FirmEntityCode != fact.FirmEntityCode {
			return fmt.Errorf("city enterprise location fact site metadata is inconsistent")
		}
		siteIndex := findCityEnterpriseSite(location.Sites, *fact.SiteCode)
		if siteIndex < 0 {
			return fmt.Errorf("city enterprise location fact references an unknown site")
		}
		before := location.Sites[siteIndex]
		if before.Status != *fact.FromStatus ||
			before.Version != fact.SiteVersionBefore ||
			before.OccupiedUnits != fact.OccupiedBeforeUnits ||
			metadata.Site.Version != fact.SiteVersionAfter ||
			metadata.Site.Status != *fact.ToStatus ||
			metadata.Site.LastChangedTick != fact.Tick {
			return fmt.Errorf("city enterprise location fact before-state is inconsistent")
		}
		if fact.FactType == CityEnterpriseLocationFactResized &&
			metadata.Site.OccupiedUnits != fact.OccupiedAfterUnits {
			return fmt.Errorf("resized city enterprise location fact occupancy is inconsistent")
		}
		if fact.FactType == CityEnterpriseLocationFactClosed &&
			(metadata.Site.ClosedTick == nil || *metadata.Site.ClosedTick != fact.Tick ||
				metadata.Site.OccupiedUnits != before.OccupiedUnits) {
			return fmt.Errorf("closed city enterprise location fact lifecycle is inconsistent")
		}
		location.Sites[siteIndex] = *metadata.Site
	case CityEnterpriseLocationFactRelocated:
		if metadata.FirmBefore == nil || metadata.FirmAfter == nil ||
			metadata.FirmBefore.DistrictCode == metadata.FirmAfter.DistrictCode ||
			metadata.FirmAfter.Version != metadata.FirmBefore.Version+1 ||
			len(metadata.SitesBefore) != 2 || len(metadata.SitesAfter) != 4 {
			return fmt.Errorf("relocated city enterprise location fact metadata is inconsistent")
		}
		for index, sequence := range metadata.ResourceOperationSequences {
			if sequence <= 0 || index > 0 && sequence <= metadata.ResourceOperationSequences[index-1] {
				return fmt.Errorf("relocated city enterprise resource operation sequence is inconsistent")
			}
		}
		firmIndex := findCityEnterpriseFirm(state.Physical.Firms, fact.FirmEntityCode)
		if firmIndex < 0 || state.Physical.Firms[firmIndex].DistrictCode != metadata.FirmBefore.DistrictCode ||
			state.Physical.Firms[firmIndex].Version != metadata.FirmBefore.Version {
			return fmt.Errorf("relocated city enterprise location firm before-state is inconsistent")
		}
		beforeCodes := make(map[string]CityEnterpriseSite, len(metadata.SitesBefore))
		for _, before := range metadata.SitesBefore {
			siteIndex := findCityEnterpriseSite(location.Sites, before.Code)
			if before.FirmEntityCode != fact.FirmEntityCode || siteIndex < 0 ||
				location.Sites[siteIndex].Status != before.Status ||
				location.Sites[siteIndex].Version != before.Version ||
				location.Sites[siteIndex].OccupiedUnits != before.OccupiedUnits ||
				location.Sites[siteIndex].DistrictCode != metadata.FirmBefore.DistrictCode ||
				!before.IsPrimary {
				return fmt.Errorf("relocated city enterprise location before-site is inconsistent")
			}
			beforeCodes[before.Code] = before
		}
		newHeadquarters, newProduction, newSiteCount := 0, 0, 0
		for _, site := range metadata.SitesAfter {
			if site.FirmEntityCode != fact.FirmEntityCode || site.LastChangedTick != fact.Tick {
				return fmt.Errorf("relocated city enterprise location site is inconsistent")
			}
			siteIndex := findCityEnterpriseSite(location.Sites, site.Code)
			if siteIndex < 0 {
				if site.Status != CityEnterpriseSiteStatusActive || site.Version != 1 ||
					site.OpenedTick != fact.Tick || site.DistrictCode != metadata.FirmAfter.DistrictCode ||
					!site.IsPrimary {
					return fmt.Errorf("relocated city enterprise location new-site is inconsistent")
				}
				switch site.SiteType {
				case CityEnterpriseSiteHeadquarters:
					newHeadquarters++
				case CityEnterpriseSiteProduction:
					newProduction++
				default:
					return fmt.Errorf("relocated city enterprise location new-site type is inconsistent")
				}
				location.Sites = append(location.Sites, site)
				location.Profile.SiteCount++
				newSiteCount++
			} else {
				before, ok := beforeCodes[site.Code]
				if !ok || site.Status != CityEnterpriseSiteStatusClosed ||
					site.Version != before.Version+1 || site.ClosedTick == nil ||
					*site.ClosedTick != fact.Tick || site.OccupiedUnits != before.OccupiedUnits {
					return fmt.Errorf("relocated city enterprise location closed-site is inconsistent")
				}
				location.Sites[siteIndex] = site
			}
		}
		if newSiteCount != 2 || newHeadquarters != 1 || newProduction != 1 {
			return fmt.Errorf("relocated city enterprise location replacement set is incomplete")
		}
		state.Physical.Firms[firmIndex].DistrictCode = metadata.FirmAfter.DistrictCode
		state.Physical.Firms[firmIndex].Version = metadata.FirmAfter.Version
	default:
		return fmt.Errorf("unsupported city enterprise location fact type %q", fact.FactType)
	}
	sort.Slice(location.Sites, func(i, j int) bool { return location.Sites[i].Code < location.Sites[j].Code })
	location.Facts = append(location.Facts, fact)
	location.Profile.FactCount++
	location.Profile.Revision++
	return nil
}

func findCityEnterpriseSite(sites []CityEnterpriseSite, code string) int {
	for index := range sites {
		if sites[index].Code == code {
			return index
		}
	}
	return -1
}

func findCityEnterpriseFirm(firms []cityHashFirm, code string) int {
	for index := range firms {
		if firms[index].EntityCode == code {
			return index
		}
	}
	return -1
}
